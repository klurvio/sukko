// Package kafka provides Kafka/Redpanda integration for the WebSocket server.
// This file contains the Producer for publishing client messages to Kafka.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/sony/gobreaker/v2"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/klurvio/sukko/internal/server/backend"
	kafkashared "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/protocol"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/routing"
)

// Re-export sentinel errors from protocol package for backward compatibility.
// These aliases allow existing code that imports kafka.ErrXxx to continue working.
var (
	ErrInvalidChannel      = protocol.ErrInvalidChannel
	ErrTopicNotProvisioned = protocol.ErrTopicNotProvisioned
	ErrServiceUnavailable  = protocol.ErrServiceUnavailable
)

// ErrProducerClosed is defined locally — it's a producer concern, not a shared protocol type.
var ErrProducerClosed = errors.New("producer is closed")

// ProducerStats holds metrics for the producer.
type ProducerStats struct {
	MessagesPublished atomic.Int64 // Successfully published messages
	MessagesFailed    atomic.Int64 // Failed publish attempts
}

// RoutingSnapshotProvider provides per-tenant routing snapshots used to resolve
// channel → topic(s) mappings at publish time. The consumer implements against this
// narrow view; do NOT widen it (§XVIII — producer-specific needs go on RoutingRulesSource).
type RoutingSnapshotProvider interface {
	GetRoutingSnapshot(tenantID string) (provapi.TenantRoutingSnapshot, bool)
}

// RoutingRulesSource is the producer's view of the routing provider: snapshot lookup plus
// readiness. SnapshotReceived() distinguishes "no rules synced yet" (server degraded,
// retryable → 503) from "tenant has no rules" (reject → 409). StreamChannelRulesProvider
// satisfies it.
type RoutingRulesSource interface {
	RoutingSnapshotProvider
	SnapshotReceived() bool
}

// ProducerConfig holds configuration for creating a Kafka producer.
type ProducerConfig struct {
	Brokers        []string        // Kafka/Redpanda broker addresses (required)
	TopicNamespace string          // Topic namespace: "local", "dev", "staging", "prod" (required)
	ClientID       string          // Client ID for Kafka connection (optional, defaults to "sukko-producer-{hostname}")
	Logger         *zerolog.Logger // Structured logger (optional)

	// Security configuration for managed Kafka/Redpanda services
	SASL *kafkashared.SASLConfig // SASL authentication (nil = no auth, for local dev)
	TLS  *kafkashared.TLSConfig  // TLS encryption (nil = no TLS, for local dev)

	// Routing — resolves channel → topic(s) using per-tenant routing rules.
	// Required in kafka mode (kafkabackend.New rejects nil). Publish rejects when the
	// provider yields no applicable rule — there is no community fallback.
	RulesProvider RoutingRulesSource

	// Edition returns the server's current license edition, checked at publish time to
	// gate ChannelTopicRouting. Required in kafka mode (kafkabackend.New rejects nil).
	Edition func() license.Edition

	// Fanout — delivers records to multiple topics concurrently.
	// When nil, multi-topic rules are rejected (single-topic uses the direct produce path).
	// P1a: always nil; pool construction lands in P1b (#179).
	Fanout *FanoutPool

	// Circuit breaker settings (optional - sensible defaults provided)
	CircuitBreakerTimeout      time.Duration // Time before half-open (default: 30s)
	CircuitBreakerMaxFailures  uint32        // Consecutive failures to trip (default: 5)
	CircuitBreakerHalfOpenReqs uint32        // Requests allowed in half-open (default: 1)

	// Producer tuning (optional - sensible defaults provided)
	BatchMaxBytes   int32         // Max batch size in bytes (default: 1MB)
	MaxBufferedRecs int           // Max buffered records (default: 10000)
	RecordRetries   int           // Number of retries (default: 8)
	ProduceTimeout  time.Duration // Timeout for produce operations (default: 10s)
}

// Producer wraps franz-go client for publishing to Kafka/Redpanda.
// It provides a simple interface for WebSocket clients to publish messages.
//
// Thread Safety: All public methods are safe for concurrent use.
//
// Lifecycle: Create with NewProducer, use Publish to send messages, Close when done.
type Producer struct {
	client         *kgo.Client
	topicNamespace string
	logger         *zerolog.Logger
	stats          ProducerStats
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
	closed         bool

	// Circuit breaker for Kafka connection
	circuitBreaker *gobreaker.CircuitBreaker[any]

	// Routing dependencies (validated non-nil by kafkabackend.New in production).
	rulesProvider RoutingRulesSource
	edition       func() license.Edition
	fanout        *FanoutPool
}

// NewProducer creates a new Kafka producer with the provided configuration.
// It validates required fields and initializes the franz-go client with
// optimal settings for client message publishing.
//
// Returns an error if any required field is missing or client creation fails.
func NewProducer(cfg ProducerConfig) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("at least one broker is required")
	}
	if cfg.TopicNamespace == "" {
		return nil, errors.New("topic namespace is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Set defaults
	// Auto-generate client ID with hostname for per-instance observability
	// Example: "sukko-producer-sukko-7f8d9c4b5-abc123" in Kubernetes
	clientID := cfg.ClientID
	if clientID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		clientID = "sukko-producer-" + hostname
	}
	// Producer tuning (values provided by env-parsed config)
	batchMaxBytes := cfg.BatchMaxBytes
	maxBufferedRecs := cfg.MaxBufferedRecs
	recordRetries := cfg.RecordRetries

	// Circuit breaker settings (values provided by env-parsed config)
	cbTimeout := cfg.CircuitBreakerTimeout
	cbMaxFailures := cfg.CircuitBreakerMaxFailures
	cbHalfOpenReqs := cfg.CircuitBreakerHalfOpenReqs

	// Build base client options (SeedBrokers, SASL, TLS)
	opts, err := kafkashared.BuildKgoOpts(cfg.Brokers, cfg.SASL, cfg.TLS)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to build kafka options: %w", err)
	}

	// Producer-specific options
	opts = append(opts,
		kgo.ClientID(clientID),
		kgo.RetryBackoffFn(func(attempt int) time.Duration {
			// Exponential backoff: 100ms, 200ms, 400ms, 800ms, ...
			return time.Duration(100*(1<<attempt)) * time.Millisecond
		}),
	)

	// Producer tuning (apply only if non-zero; omitting uses franz-go defaults)
	if batchMaxBytes > 0 {
		opts = append(opts, kgo.ProducerBatchMaxBytes(batchMaxBytes))
	}
	if maxBufferedRecs > 0 {
		opts = append(opts, kgo.MaxBufferedRecords(maxBufferedRecs))
	}
	if recordRetries > 0 {
		opts = append(opts, kgo.RecordRetries(recordRetries))
	}

	// Create franz-go client
	client, err := kgo.NewClient(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create kafka producer client: %w", err)
	}

	// Create circuit breaker for Kafka connection
	cbSettings := gobreaker.Settings{
		Name:        "kafka-producer",
		MaxRequests: cbHalfOpenReqs,
		Interval:    0, // No periodic reset
		Timeout:     cbTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cbMaxFailures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			if cfg.Logger != nil {
				cfg.Logger.Warn().
					Str("name", name).
					Str("from", from.String()).
					Str("to", to.String()).
					Msg("Circuit breaker state changed")
			}
		},
	}
	cb := gobreaker.NewCircuitBreaker[any](cbSettings)

	producer := &Producer{
		client:         client,
		topicNamespace: strings.ToLower(strings.TrimSpace(cfg.TopicNamespace)),
		logger:         cfg.Logger,
		ctx:            ctx,
		cancel:         cancel,
		circuitBreaker: cb,
		rulesProvider:  cfg.RulesProvider,
		edition:        cfg.Edition,
		fanout:         cfg.Fanout,
	}

	if cfg.Logger != nil {
		cfg.Logger.Info().
			Strs("brokers", cfg.Brokers).
			Str("namespace", producer.topicNamespace).
			Str("client_id", clientID).
			Dur("circuit_breaker_timeout", cbTimeout).
			Msg("Kafka producer initialized")
	}

	return producer, nil
}

// Publish sends a client message to Kafka.
//
// The target topic(s) come from the tenant's per-tenant routing rules (resolveTargets) —
// there is NO convention/last-segment fallback. The record uses:
//   - Key: full channel string (for partitioning — same channel = same partition = ordering)
//   - Value: raw JSON from the client (no parsing/validation)
//   - Headers: metadata (client_id, source, timestamp)
//
// The channel must already be in internal format (tenant-prefixed) after gateway mapping.
//
// Callers branch on these sentinels (via errors.Is):
//   - backend.ErrNoMatchingRoute    — rules present, none match this channel (reject → 409;
//     wraps ErrPublishNotRoutable, so the check below it also fires)
//   - backend.ErrPublishNotRoutable — no rules provisioned / routing unavailable (reject → 409)
//   - ErrInvalidChannel             — malformed channel (→ 400)
//   - ErrServiceUnavailable         — rules not yet synced or breaker open (retryable → 503)
//   - other (e.g. ErrPublishFailed) — genuine produce failure (→ 500)
//
// A rule with no configured topics is treated as a non-match (skipped); if no rule matches,
// the publish is REJECTED (ErrNoMatchingRoute) — never silently dead-lettered (C1 revised,
// #179; ratified FR-014).
func (p *Producer) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
	closed := func() bool {
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.closed
	}()
	if closed {
		return ErrProducerClosed
	}

	// Resolve routing BEFORE the circuit breaker. Rejects and the not-synced/unavailable
	// signal are pure in-memory decisions with no Kafka I/O — they MUST NOT count toward
	// breaker failure state (§IX: otherwise one rules-less tenant's retries open the shared
	// breaker and 503 every tenant). Only genuine produce attempts enter the breaker.
	tenant, err := extractTenant(channel)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}
	topics, resolveErr := p.resolveTargets(channel, tenant)
	if resolveErr != nil {
		return resolveErr
	}
	// Multi-topic rule without a fanout pool is a server-capability gap (P1a defers fanout
	// to P1b). Reject it HERE, before the breaker — like the other in-memory decisions above,
	// it has no Kafka I/O and MUST NOT count toward breaker failure state (§IX: otherwise a
	// tenant with a valid multi-topic rule opens the shared breaker and 503s every tenant).
	if len(topics) > 1 && p.fanout == nil {
		p.stats.MessagesFailed.Add(1)
		if p.logger != nil {
			p.logger.Error().
				Str("channel", channel).
				Int("rule_topics", len(topics)).
				Msg("multi-topic routing rule requires the fanout pool (not configured)")
		}
		return fmt.Errorf("%w: multi-topic routing requires the fanout pool", backend.ErrPublishFailed)
	}

	_, cbErr := p.circuitBreaker.Execute(func() (any, error) {
		return nil, p.doProduce(ctx, clientID, channel, tenant, topics, data)
	})

	if errors.Is(cbErr, gobreaker.ErrOpenState) || errors.Is(cbErr, gobreaker.ErrTooManyRequests) {
		return fmt.Errorf("%w: kafka circuit breaker open", ErrServiceUnavailable)
	}

	if cbErr != nil {
		return fmt.Errorf("circuit breaker execute: %w", cbErr)
	}
	return nil
}

// doProduce performs the actual Kafka produce for a pre-resolved plan (topics already
// computed by resolveTargets, guaranteed non-empty — no-match rejects before this point).
// It runs inside the circuit breaker, so only genuine produce failures count toward breaker
// state.
func (p *Producer) doProduce(ctx context.Context, clientID int64, channel, tenant string, topics []string, data []byte) error {
	baseRecord := &kgo.Record{
		Key:   []byte(channel),
		Value: data,
		Headers: []kgo.RecordHeader{
			{Key: HeaderClientID, Value: []byte(strconv.FormatInt(clientID, 10))},
			{Key: kafkashared.HeaderSource, Value: []byte(SourceWSClient)},
			{Key: kafkashared.HeaderTimestamp, Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
			{Key: kafkashared.HeaderChannel, Value: []byte(channel)},
		},
	}

	// Single-topic: synchronous, error-propagating, breaker-protected — regardless of whether
	// a fanout pool exists (the pool is for multi-topic fan-out only). Multi-topic with a nil
	// pool is already rejected before the breaker in Publish, so here len(topics) > 1 implies
	// p.fanout != nil.
	if len(topics) == 1 {
		rec := *baseRecord // struct copy — independent from baseRecord
		rec.Topic = topics[0]
		results := p.client.ProduceSync(ctx, &rec)
		if err := results.FirstErr(); err != nil {
			p.stats.MessagesFailed.Add(1)
			if p.logger != nil {
				p.logger.Error().Err(err).Str("channel", channel).Str(LabelTopic, topics[0]).Msg("kafka produce failed")
			}
			return fmt.Errorf("kafka produce failed: %w", err)
		}
		p.stats.MessagesPublished.Add(1)
		if p.logger != nil {
			p.logger.Debug().
				Int64("client_id", clientID).
				Str("channel", channel).
				Str(LabelTopic, topics[0]).
				Int("data_size", len(data)).
				Msg("Published message to Kafka")
		}
		return nil
	}

	// Fan-out path: submit to multiple topics via the fanout pool (reachable only once the
	// pool is wired in P1b; P1a rejects multi-topic-with-nil-pool before the breaker).
	n := len(topics)
	successCh := make(chan string, n)
	failCh := make(chan string, n)

	var succeeded, failed []string
	for _, topic := range topics {
		job := fanoutJob{
			topic:     topic,
			record:    baseRecord,
			channel:   channel,
			tenant:    tenant,
			successCh: successCh,
			failCh:    failCh,
		}
		if !p.fanout.Submit(job) {
			// Queue full — count as immediate failure so aggregator gets N results.
			failCh <- topic
		}
	}

	// Collect exactly N results. ctx.Done() guards against fanout workers exiting
	// mid-flight (e.g., shutdown) before all jobs are processed.
	for range n {
		select {
		case t := <-successCh:
			succeeded = append(succeeded, t)
		case t := <-failCh:
			failed = append(failed, t)
		case <-ctx.Done():
			return fmt.Errorf("fanout canceled: %w", ctx.Err())
		}
	}

	if len(failed) > 0 {
		dlqTopic := kafkashared.BuildTopicName(p.topicNamespace, tenant, routing.DeadLetterTopicSuffix)
		dlqRec := &kgo.Record{
			Topic: dlqTopic,
			Key:   baseRecord.Key,
			Value: baseRecord.Value,
			Headers: append(slices.Clone(baseRecord.Headers),
				kgo.RecordHeader{Key: HeaderReason, Value: []byte(ReasonFanoutTopicWriteFailed)},
				kgo.RecordHeader{Key: HeaderFailedTopics, Value: []byte(strings.Join(failed, ","))},
				kgo.RecordHeader{Key: HeaderSucceededTopics, Value: []byte(strings.Join(succeeded, ","))},
			),
		}
		if p.fanout.dlq != nil {
			if !p.fanout.dlq.TrySubmit(dlqJob{record: dlqRec, tenant: tenant, reason: ReasonFanoutTopicWriteFailed}) {
				if p.logger != nil {
					p.logger.Warn().Str(logging.LogKeyTenantSlug, tenant).Msg("DLQ queue full — dropping failed fanout record")
				}
			}
		}
	}

	if len(succeeded) > 0 {
		p.stats.MessagesPublished.Add(1)
	} else {
		p.stats.MessagesFailed.Add(1)
	}
	return nil
}

// resolveTargets computes the produce plan for a channel from the tenant's routing rules.
// There is NO community fallback (deleted in #179) and NO silent dead-letter on no-match
// (C1 revised, #179): a channel with no applicable rule is REJECTED, never silently routed
// to a convention-derived or dead-letter topic. This restores ratified FR-013/FR-014.
//
// Outcomes (checked in this order — not-synced MUST precede tenant-absent so a degraded
// server returns a retryable 503, not a misleading 409):
//   - err != nil      → REJECT (ErrPublishNotRoutable / ErrNoMatchingRoute) or UNAVAILABLE
//     (ErrServiceUnavailable); do NOT produce
//   - topics (len>=1) → produce to these rule topic(s)
//
// The edition is read from the server-global license accessor, NOT snap.Edition: the routing
// snapshot and the license state arrive on independent streams, so snap.Edition can lag the
// live license (zero-value on a fresh snapshot, or license/rules boot-order skew). The
// accessor is always current (#179).
func (p *Producer) resolveTargets(channel, tenant string) (topics []string, err error) {
	if p.rulesProvider == nil {
		return nil, fmt.Errorf("%w: routing rules provider not configured", backend.ErrPublishNotRoutable)
	}
	if !p.rulesProvider.SnapshotReceived() {
		// The rules stream has not delivered its first snapshot — the server is degraded
		// (cold start / provisioning outage), not the tenant misconfigured. Retryable.
		return nil, fmt.Errorf("%w: routing rules not yet synced", ErrServiceUnavailable)
	}
	if p.edition == nil || !license.EditionHasFeature(p.edition(), license.ChannelTopicRouting) {
		// License anomaly: kafka mode is Pro-gated, so ChannelTopicRouting should always be
		// present. If not, fail closed and surface it loudly.
		if p.logger != nil {
			p.logger.Error().Str(logging.LogKeyTenantSlug, tenant).
				Msg("channel-topic-routing feature unavailable on the produce path (license anomaly)")
		}
		return nil, fmt.Errorf("%w: channel-topic routing not licensed", backend.ErrPublishNotRoutable)
	}

	snap, ok := p.rulesProvider.GetRoutingSnapshot(tenant)
	if !ok || len(snap.Rules) == 0 {
		return nil, fmt.Errorf("%w: no routing rules provisioned for tenant", backend.ErrPublishNotRoutable)
	}

	for _, rule := range snap.Rules {
		matched, matchErr := routing.MatchRoutingPattern(rule.Pattern, channel)
		if matchErr != nil {
			// Patterns are validated at provisioning time, so a match error here means the
			// snapshot is corrupt or skewed — skip the rule (it can't route reliably) but
			// surface it: silently dropping a provisioned rule is an operator-invisible gap.
			if p.logger != nil {
				p.logger.Warn().Err(matchErr).
					Str(logging.LogKeyTenantSlug, tenant).
					Str("pattern", rule.Pattern).
					Str("channel", channel).
					Msg("routing rule pattern failed to match; skipping rule")
			}
			continue
		}
		if !matched {
			continue
		}
		if len(rule.Topics) == 0 {
			continue // zero-topic rule — treat as no-match (defense in depth §II)
		}
		// Dedupe: duplicate topics within one rule would double-deliver.
		fullTopics := make([]string, 0, len(rule.Topics))
		seen := make(map[string]struct{}, len(rule.Topics))
		for _, suffix := range rule.Topics {
			name := kafkashared.BuildTopicName(p.topicNamespace, tenant, suffix)
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			fullTopics = append(fullTopics, name)
		}
		return fullTopics, nil
	}
	// Rules present but none matched — REJECT (C1 revised, #179; ratified FR-014). The WS
	// handler emits protocol.ErrCodeNoMatchingRoute; the gateway maps it to 409. This is a
	// synchronous reject, NOT a silent dead-letter (§III/§XV): a rules-bearing tenant
	// publishing to an unmatched channel gets an immediate, actionable error.
	return nil, fmt.Errorf("%w for channel %q", backend.ErrNoMatchingRoute, channel)
}

// extractTenant returns the tenant ID from an internal channel (first segment).
// Channel format: {tenant}.{rest...} (minimum 2 parts).
func extractTenant(channel string) (string, error) {
	dot := strings.IndexByte(channel, '.')
	if dot <= 0 {
		return "", fmt.Errorf("channel must have at least 2 dot-separated parts, got %q", channel)
	}
	tenant := channel[:dot]
	if tenant == "" {
		return "", errors.New("tenant (first segment) cannot be empty")
	}
	return tenant, nil
}

// Stats returns a snapshot of the current producer statistics.
func (p *Producer) Stats() ProducerStatsSnapshot {
	return ProducerStatsSnapshot{
		MessagesPublished: p.stats.MessagesPublished.Load(),
		MessagesFailed:    p.stats.MessagesFailed.Load(),
	}
}

// ProducerStatsSnapshot is a plain snapshot of producer statistics (safe to copy).
type ProducerStatsSnapshot struct {
	MessagesPublished int64
	MessagesFailed    int64
}

// Namespace returns the topic namespace this producer uses.
func (p *Producer) Namespace() string {
	return p.topicNamespace
}

// CircuitBreakerState returns the current state of the circuit breaker.
func (p *Producer) CircuitBreakerState() gobreaker.State {
	return p.circuitBreaker.State()
}

// Close gracefully shuts down the producer.
// It flushes any buffered messages and closes the Kafka client.
func (p *Producer) Close() error {
	alreadyClosed := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.closed {
			return true
		}
		p.closed = true
		return false
	}()
	if alreadyClosed {
		return nil
	}

	p.cancel()
	p.client.Close()

	if p.logger != nil {
		stats := p.Stats()
		p.logger.Info().
			Int64("messages_published", stats.MessagesPublished).
			Int64("messages_failed", stats.MessagesFailed).
			Msg("Kafka producer closed")
	}

	return nil
}
