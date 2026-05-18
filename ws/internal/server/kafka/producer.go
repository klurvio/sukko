// Package kafka provides Kafka/Redpanda integration for the WebSocket server.
// This file contains the Producer for publishing client messages to Kafka.
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"github.com/twmb/franz-go/pkg/sasl/scram"

	kafkautil "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/license"
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

// RoutingSnapshotProvider provides per-tenant routing snapshots used by the producer
// to resolve channel → topic(s) mappings at publish time.
type RoutingSnapshotProvider interface {
	GetRoutingSnapshot(tenantID string) (provapi.TenantRoutingSnapshot, bool)
}

// ProducerConfig holds configuration for creating a Kafka producer.
type ProducerConfig struct {
	Brokers        []string        // Kafka/Redpanda broker addresses (required)
	TopicNamespace string          // Topic namespace: "local", "dev", "staging", "prod" (required)
	ClientID       string          // Client ID for Kafka connection (optional, defaults to "sukko-producer-{hostname}")
	Logger         *zerolog.Logger // Structured logger (optional)

	// Security configuration for managed Kafka/Redpanda services
	SASL *SASLConfig // SASL authentication (nil = no auth, for local dev)
	TLS  *TLSConfig  // TLS encryption (nil = no TLS, for local dev)

	// Routing — resolves channel → topic(s) using per-tenant routing rules.
	// When nil the producer falls back to community mode (last channel segment).
	RulesProvider RoutingSnapshotProvider

	// Fanout — delivers records to multiple topics concurrently.
	// When nil a direct synchronous produce is used (single-topic path only).
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

	// Routing and fanout dependencies (nil = community fallback)
	rulesProvider RoutingSnapshotProvider
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

	// Build client options
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(clientID),
		kgo.RetryBackoffFn(func(attempt int) time.Duration {
			// Exponential backoff: 100ms, 200ms, 400ms, 800ms, ...
			return time.Duration(100*(1<<attempt)) * time.Millisecond
		}),
	}

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

	// Add SASL authentication if configured
	if cfg.SASL != nil {
		mechanism := scram.Auth{
			User: cfg.SASL.Username,
			Pass: cfg.SASL.Password,
		}

		switch cfg.SASL.Mechanism {
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(mechanism.AsSha256Mechanism()))
			if cfg.Logger != nil {
				cfg.Logger.Info().
					Str("mechanism", "SCRAM-SHA-256").
					Str("username", cfg.SASL.Username).
					Msg("Kafka producer SASL authentication enabled")
			}
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(mechanism.AsSha512Mechanism()))
			if cfg.Logger != nil {
				cfg.Logger.Info().
					Str("mechanism", "SCRAM-SHA-512").
					Str("username", cfg.SASL.Username).
					Msg("Kafka producer SASL authentication enabled")
			}
		default:
			cancel()
			return nil, fmt.Errorf("unsupported SASL mechanism: %s (use scram-sha-256 or scram-sha-512)", cfg.SASL.Mechanism)
		}
	}

	// Add TLS encryption if configured
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, //nolint:gosec // Controlled by configuration for dev/testing environments
			MinVersion:         tls.VersionTLS12,
		}

		// Load CA certificate if provided
		if cfg.TLS.CAPath != "" {
			caCert, err := os.ReadFile(cfg.TLS.CAPath)
			if err != nil {
				cancel()
				return nil, fmt.Errorf("failed to read CA certificate from %s: %w", cfg.TLS.CAPath, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				cancel()
				return nil, fmt.Errorf("failed to parse CA certificate from %s", cfg.TLS.CAPath)
			}
			tlsCfg.RootCAs = caCertPool
		}

		opts = append(opts, kgo.DialTLSConfig(tlsCfg))
		if cfg.Logger != nil {
			cfg.Logger.Info().
				Bool("insecure_skip_verify", cfg.TLS.InsecureSkipVerify).
				Str("ca_path", cfg.TLS.CAPath).
				Msg("Kafka producer TLS encryption enabled")
		}
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
// The message is published to the appropriate category topic based on channel:
//   - Channel format: {tenant}.{identifier}.{category} (internal/mapped)
//   - Topic: {namespace}.{tenant}.{category}
//   - Key: full channel string (for partitioning - same channel = same partition = ordering)
//   - Value: raw JSON from client (no parsing/validation)
//   - Headers: metadata (client_id, source, timestamp)
//
// The channel must already be in internal format (tenant-prefixed) after gateway mapping.
// Example: channel "acme.BTC.trade" → topic "prod.acme.trade", key "acme.BTC.trade"
func (p *Producer) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
	closed := func() bool {
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.closed
	}()
	if closed {
		return ErrProducerClosed
	}

	// Execute through circuit breaker
	_, err := p.circuitBreaker.Execute(func() (any, error) {
		return nil, p.doPublish(ctx, clientID, channel, data)
	})

	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return fmt.Errorf("%w: kafka circuit breaker open", ErrServiceUnavailable)
	}

	if err != nil {
		return fmt.Errorf("circuit breaker execute: %w", err)
	}
	return nil
}

// doPublish performs the actual Kafka publish operation.
func (p *Producer) doPublish(ctx context.Context, clientID int64, channel string, data []byte) error {
	tenant, err := extractTenant(channel)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}

	topics, dlqReason := p.resolveTargets(channel, tenant)

	baseRecord := &kgo.Record{
		Key:   []byte(channel),
		Value: data,
		Headers: []kgo.RecordHeader{
			{Key: HeaderClientID, Value: []byte(strconv.FormatInt(clientID, 10))},
			{Key: HeaderSource, Value: []byte(SourceWSClient)},
			{Key: HeaderTimestamp, Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
			{Key: HeaderChannel, Value: []byte(channel)},
		},
	}

	if dlqReason != "" {
		// No matching rule — route to dead-letter topic.
		dlqTopic := kafkautil.BuildTopicName(p.topicNamespace, tenant, routing.DeadLetterTopicSuffix)
		dlqRec := *baseRecord // struct copy — independent from baseRecord
		dlqRec.Topic = dlqTopic
		dlqRec.Headers = append(slices.Clone(baseRecord.Headers), kgo.RecordHeader{Key: HeaderReason, Value: []byte(dlqReason)})
		results := p.client.ProduceSync(ctx, &dlqRec)
		if results.FirstErr() != nil {
			p.stats.MessagesFailed.Add(1)
			return fmt.Errorf("kafka dlq produce failed: %w", results.FirstErr())
		}
		p.stats.MessagesPublished.Add(1)
		return nil
	}

	n := len(topics)
	if n > 1 && p.fanout == nil {
		// Fanout pool not configured — multi-topic rule degrades to first topic only.
		if p.logger != nil {
			p.logger.Warn().
				Str("channel", channel).
				Int("rule_topics", n).
				Msg("fanout pool not configured: multi-topic rule delivering to first topic only")
		}
	}
	if n == 1 || p.fanout == nil {
		// Fast path: single topic or no fanout pool configured.
		rec := *baseRecord // struct copy — independent from baseRecord
		rec.Topic = topics[0]
		results := p.client.ProduceSync(ctx, &rec)
		if err := results.FirstErr(); err != nil {
			p.stats.MessagesFailed.Add(1)
			if p.logger != nil {
				p.logger.Error().Err(err).Str("channel", channel).Str("topic", topics[0]).Msg("kafka produce failed")
			}
			return fmt.Errorf("kafka produce failed: %w", err)
		}
		p.stats.MessagesPublished.Add(1)
		if p.logger != nil {
			p.logger.Debug().
				Int64("client_id", clientID).
				Str("channel", channel).
				Str("topic", topics[0]).
				Int("data_size", len(data)).
				Msg("Published message to Kafka")
		}
		return nil
	}

	// Fan-out path: submit to multiple topics via the fanout pool.
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
		dlqTopic := kafkautil.BuildTopicName(p.topicNamespace, tenant, routing.DeadLetterTopicSuffix)
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
					p.logger.Warn().Str("tenant", tenant).Msg("DLQ queue full — dropping failed fanout record")
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

// resolveTargets returns the Kafka topic(s) for a channel given the tenant's routing rules.
// Falls back to community mode (last channel segment) when routing is unavailable.
func (p *Producer) resolveTargets(channel, tenant string) (topics []string, dlqReason string) {
	if p.rulesProvider != nil {
		snap, ok := p.rulesProvider.GetRoutingSnapshot(tenant)
		if ok && license.EditionHasFeature(snap.Edition, license.ChannelTopicRouting) {
			for _, rule := range snap.Rules {
				matched, err := routing.MatchRoutingPattern(rule.Pattern, channel)
				if err != nil || !matched {
					continue
				}
				fullTopics := make([]string, len(rule.Topics))
				for i, suffix := range rule.Topics {
					fullTopics[i] = kafkautil.BuildTopicName(p.topicNamespace, tenant, suffix)
				}
				return fullTopics, ""
			}
			// No rule matched — dead letter.
			return nil, ReasonNoRoutingRuleMatched
		}
	}

	// Community fallback: derive topic from last channel segment.
	suffix := channel[strings.LastIndex(channel, ".")+1:]
	return []string{kafkautil.BuildTopicName(p.topicNamespace, tenant, suffix)}, ""
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
