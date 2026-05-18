package kafka

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/klurvio/sukko/internal/shared/logging"
)

// fanoutJob represents a single topic write within a multi-topic fan-out.
// successCh and failCh are shared across all jobs for the same message so the
// aggregator can collect exactly N results (one per topic).
type fanoutJob struct {
	topic     string
	record    *kgo.Record
	channel   string
	tenant    string
	successCh chan<- string
	failCh    chan<- string
}

// FanoutPool writes records to multiple Kafka topics concurrently and reports
// per-topic success or failure back to the caller via result channels.
type FanoutPool struct {
	handoff chan fanoutJob
	dlq     *DLQPool
	client  *kgo.Client
	logger  zerolog.Logger
	workers int

	writeFailedCounter *prometheus.CounterVec
	droppedCounter     *prometheus.CounterVec
}

// NewFanoutPool creates a fan-out worker pool. workers controls concurrency;
// queueSize is the depth of the handoff channel.
func NewFanoutPool(workers, queueSize int, client *kgo.Client, dlq *DLQPool, logger zerolog.Logger) *FanoutPool {
	return &FanoutPool{
		handoff: make(chan fanoutJob, queueSize),
		dlq:     dlq,
		client:  client,
		logger:  logger.With().Str("component", "fanout_pool").Logger(),
		workers: workers,
		writeFailedCounter: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: MetricFanoutWriteFailedTotal,
			Help: "Total fan-out topic writes that failed",
		}, []string{LabelTenant, LabelTopic}),
		droppedCounter: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: MetricFanoutDroppedTotal,
			Help: "Total fan-out jobs dropped because the queue was full",
		}, []string{LabelTenant}),
	}
}

// Start launches p.workers goroutines to drain the fanout job queue.
func (p *FanoutPool) Start(ctx context.Context, wg *sync.WaitGroup) {
	for range p.workers {
		wg.Go(func() {
			defer logging.RecoverPanic(p.logger, "fanout_worker", nil)
			p.runWorker(ctx)
		})
	}
}

// Submit enqueues a fanout job. Returns false if the queue is full (caller
// should treat the topic as failed and route to DLQ immediately).
func (p *FanoutPool) Submit(job fanoutJob) bool {
	select {
	case p.handoff <- job:
		return true
	default:
		if p.droppedCounter != nil {
			p.droppedCounter.WithLabelValues(job.tenant).Inc()
		}
		return false
	}
}

func (p *FanoutPool) runWorker(ctx context.Context) {
	for {
		select {
		case job := <-p.handoff:
			p.process(ctx, job)
		case <-ctx.Done():
			return
		}
	}
}

func (p *FanoutPool) process(ctx context.Context, job fanoutJob) {
	// Copy headers into a new slice so per-topic mutations don't race.
	// HeaderChannel is already stamped on the base record by the producer.
	headers := make([]kgo.RecordHeader, len(job.record.Headers))
	copy(headers, job.record.Headers)
	rec := &kgo.Record{
		Topic:   job.topic,
		Key:     job.record.Key,
		Value:   job.record.Value,
		Headers: headers,
	}

	results := p.client.ProduceSync(ctx, rec)
	if results.FirstErr() == nil {
		job.successCh <- job.topic
		return
	}

	if p.writeFailedCounter != nil {
		p.writeFailedCounter.WithLabelValues(job.tenant, job.topic).Inc()
	}

	if ctx.Err() != nil {
		// Context canceled — increment dropped counter and skip DLQ submission.
		if p.droppedCounter != nil {
			p.droppedCounter.WithLabelValues(job.tenant).Inc()
		}
		job.failCh <- job.topic
		return
	}

	job.failCh <- job.topic
}
