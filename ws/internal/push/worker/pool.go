// Package worker provides a bounded worker pool for dispatching push notification
// jobs to platform-specific providers (Web Push, FCM, APNs). It implements
// backpressure via a bounded job channel and handles provider-specific errors
// like expired subscriptions and rate limiting.
package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/push/provider"
	"github.com/klurvio/sukko/internal/push/repository"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// PoolConfig configures the worker pool.
type PoolConfig struct {
	// WorkerCount is the number of concurrent worker goroutines.
	WorkerCount int

	// QueueSize is the capacity of the bounded job channel.
	// When full, Enqueue blocks — providing backpressure to the consumer.
	QueueSize int

	// Providers maps platform names to their Provider implementations.
	// Keys: "web", "android", "ios", "dryrun".
	Providers map[string]provider.Provider

	// Repo is the subscription repository for stale subscription cleanup.
	Repo repository.SubscriptionRepository

	// MaxRetries is the maximum number of retries for rate-limited requests.
	MaxRetries int

	// Logger for structured logging.
	Logger zerolog.Logger
}

// Prometheus metrics for push job processing.
var (
	jobsEnqueued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "push_jobs_enqueued_total",
		Help: "Total number of push jobs enqueued for delivery.",
	})

	jobsDispatched = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "push_jobs_dispatched_total",
		Help: "Total number of push jobs dispatched to providers.",
	}, []string{"platform"})

	jobsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "push_jobs_failed_total",
		Help: "Total number of push jobs that failed delivery.",
	}, []string{"platform", "error_type"})

	dispatchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "push_dispatch_duration_seconds",
		Help:    "Duration of push notification dispatch to providers.",
		Buckets: prometheus.DefBuckets,
	}, []string{"platform"})
)

// Pool is a bounded worker pool that dispatches push notification jobs
// to platform-specific providers. Workers read from a shared job channel,
// providing natural load balancing and backpressure.
//
// Thread Safety: All public methods are safe for concurrent use.
type Pool struct {
	jobs       chan provider.PushJob
	providers  map[string]provider.Provider
	repo       repository.SubscriptionRepository
	maxRetries int
	logger     zerolog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewPool creates a new worker pool. Call Start to launch worker goroutines.
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.WorkerCount < 1 {
		return nil, errors.New("worker count must be >= 1")
	}
	if cfg.QueueSize < 1 {
		return nil, errors.New("queue size must be >= 1")
	}
	if len(cfg.Providers) == 0 {
		return nil, errors.New("at least one provider is required")
	}
	if cfg.Repo == nil {
		return nil, errors.New("subscription repository is required")
	}

	return &Pool{
		jobs:       make(chan provider.PushJob, cfg.QueueSize),
		providers:  cfg.Providers,
		repo:       cfg.Repo,
		maxRetries: cfg.MaxRetries,
		logger:     cfg.Logger,
	}, nil
}

// Start initializes the worker pool context. Must be called before StartWorkers.
func (p *Pool) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)
}

// StartWorkers launches n worker goroutines. Must be called after Start.
func (p *Pool) StartWorkers(n int) {
	for range n {
		p.wg.Go(p.worker)
	}

	p.logger.Info().
		Int("workers", n).
		Int("queue_size", cap(p.jobs)).
		Msg("Push worker pool workers launched")
}

// Enqueue adds a push job to the work queue. If the queue is full, Enqueue
// blocks until space is available or the context is canceled — providing
// backpressure to the consumer.
func (p *Pool) Enqueue(job provider.PushJob) {
	select {
	case p.jobs <- job:
		jobsEnqueued.Inc()
	case <-p.ctx.Done():
	}
}

// EnqueueBatch adds multiple push jobs to the work queue.
func (p *Pool) EnqueueBatch(jobs []provider.PushJob) {
	for _, job := range jobs {
		p.Enqueue(job)
	}
}

// Stop gracefully shuts down the worker pool. It cancels the context,
// closes the job channel (draining remaining jobs), and waits for all
// workers to finish.
func (p *Pool) Stop() {
	if p.cancel != nil {
		p.cancel()
	}

	// Close the jobs channel so workers drain remaining jobs and exit.
	close(p.jobs)
	p.wg.Wait()

	p.logger.Info().Msg("Push worker pool stopped")
}

// worker is the main loop for a single worker goroutine.
func (p *Pool) worker() {
	defer logging.RecoverPanic(p.logger, "push_worker", nil)

	for job := range p.jobs {
		p.dispatch(job)
	}
}

// dispatch sends a single push job to the appropriate provider and handles errors.
func (p *Pool) dispatch(job provider.PushJob) {
	prov, ok := p.providers[job.Platform]
	if !ok {
		p.logger.Error().
			Str("platform", job.Platform).
			Str("tenant_id", job.TenantID).
			Msg("No provider registered for platform")
		jobsFailed.WithLabelValues(job.Platform, "no_provider").Inc()
		return
	}

	start := time.Now()
	err := p.sendWithRetry(prov, job)
	elapsed := time.Since(start)
	dispatchDuration.WithLabelValues(job.Platform).Observe(elapsed.Seconds())

	if err == nil {
		jobsDispatched.WithLabelValues(job.Platform).Inc()
		return
	}

	// Handle expired subscriptions — remove stale token from storage
	if errors.Is(err, provider.ErrSubscriptionExpired) {
		p.logger.Info().
			Str("tenant_id", job.TenantID).
			Str("platform", job.Platform).
			Str("channel", job.Channel).
			Msg("Subscription expired, removing from storage")

		token := job.Token
		if token == "" {
			token = job.Endpoint // Web Push uses endpoint as identifier
		}
		if delErr := p.repo.DeleteByToken(p.ctx, job.TenantID, token); delErr != nil {
			p.logger.Error().
				Err(delErr).
				Str("tenant_id", job.TenantID).
				Msg("Failed to delete expired subscription")
		}
		jobsFailed.WithLabelValues(job.Platform, "expired").Inc()
		return
	}

	// All other errors
	p.logger.Error().
		Err(err).
		Str("tenant_id", job.TenantID).
		Str("platform", job.Platform).
		Str("channel", job.Channel).
		Msg("Push notification delivery failed")
	jobsFailed.WithLabelValues(job.Platform, "send_error").Inc()
}

// sendWithRetry attempts to send a push job with exponential backoff for rate limiting.
func (p *Pool) sendWithRetry(prov provider.Provider, job provider.PushJob) error {
	var lastErr error
	backoff := 1 * time.Second

	for attempt := range p.maxRetries + 1 {
		lastErr = prov.Send(p.ctx, job)
		if lastErr == nil {
			return nil
		}

		// Only retry on rate limiting
		if !errors.Is(lastErr, provider.ErrRateLimited) {
			return lastErr
		}

		// Don't sleep after the last attempt
		if attempt == p.maxRetries {
			break
		}

		p.logger.Debug().
			Int("attempt", attempt+1).
			Dur("backoff", backoff).
			Str("platform", job.Platform).
			Msg("Push provider rate limited, retrying")

		select {
		case <-time.After(backoff):
		case <-p.ctx.Done():
			return p.ctx.Err()
		}

		// Exponential backoff: 1s, 2s, 4s, ...
		backoff *= 2
		if backoff > 16*time.Second {
			backoff = 16 * time.Second
		}
	}

	jobsFailed.WithLabelValues(job.Platform, "rate_limited").Inc()
	return lastErr
}
