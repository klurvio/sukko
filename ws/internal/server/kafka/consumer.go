// Package kafka provides Kafka/Redpanda consumer integration with resource-aware
// message processing. It supports batching, rate limiting, and CPU-based backpressure
// to prevent message loss during high load.
//
// Key features:
//   - Franz-go based consumer with consumer group support
//   - Message batching for reduced overhead (default: 50 messages, 10ms timeout)
//   - Integration with ResourceGuard for CPU-based throttling
//   - Offset replay for client reconnection scenarios
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/klurvio/sukko/internal/server/metrics"
	kafkautil "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/routing"
)

// consumerRoutingMetrics holds Prometheus metrics shared across all Consumer instances.
// Registered once at package level to avoid duplicate registration panics when the
// orchestration pool creates one Consumer per active tenant.
var consumerRoutingMetrics = struct {
	deadLetterCounter     *prometheus.CounterVec
	malformedTopicCounter prometheus.Counter
	once                  sync.Once
}{}

// TokenEvent represents an event from Redpanda
type TokenEvent struct {
	Type      kafkautil.EventType `json:"type"`
	Timestamp int64               `json:"timestamp"`
	Data      map[string]any      `json:"data"`
}

// BroadcastFunc is called when a message is received
// Parameters: subject (Kafka Key = broadcast channel, e.g., "sukko.BTC.trade"), messageJSON
type BroadcastFunc func(subject string, message []byte)

// ResourceGuard interface for rate limiting and CPU emergency brake
type ResourceGuard interface {
	AllowKafkaMessage(ctx context.Context) (allow bool, waitDuration time.Duration)
	ShouldPauseKafka() bool
}

// SASLConfig holds SASL authentication configuration for Kafka
type SASLConfig struct {
	Mechanism string // "scram-sha-256" or "scram-sha-512"
	Username  string
	Password  string
}

// TLSConfig holds TLS encryption configuration for Kafka
type TLSConfig struct {
	Enabled            bool
	InsecureSkipVerify bool   // Skip server certificate verification (not for production)
	CAPath             string // Path to CA certificate file (optional, uses system CA pool if empty)
}

// Consumer wraps franz-go client for consuming from Kafka/Redpanda.
// It provides three-layer protection against overload:
//  1. Rate limiting - caps consumption at configured rate (e.g., 25 msg/sec)
//  2. CPU emergency brake - pauses when CPU exceeds threshold (e.g., 80%)
//  3. Direct broadcast - non-blocking message delivery to clients
//
// Thread Safety: All public methods are safe for concurrent use.
//
// Lifecycle: Create with NewConsumer, start with Start, stop with Stop.
type Consumer struct {
	client        *kgo.Client     // Franz-go Kafka client
	logger        *zerolog.Logger // Structured logger
	broadcast     BroadcastFunc   // Callback for message delivery to clients
	resourceGuard ResourceGuard   // Rate limiting and CPU brake
	ctx           context.Context // Root context for consumer goroutines
	cancel        context.CancelFunc
	wg            sync.WaitGroup // Tracks consumer goroutines

	// Batching configuration - reduces overhead by processing multiple messages together
	batchSize    int           // Max messages per batch (default: 50)
	batchTimeout time.Duration // Max time to wait for batch (default: 10ms)
	batchEnabled bool          // Enable batching (default: true for performance)

	// Transport tuning (stored from config for replay consumer and backpressure)
	fetchMaxWait              time.Duration
	fetchMinBytes             int32
	fetchMaxBytes             int32
	replayFetchMaxBytes       int32
	backpressureCheckInterval time.Duration

	// Routing — resolves channel from header or key based on edition.
	namespace     string
	rulesProvider RoutingSnapshotProvider
	dlq           *DLQPool

	// Metrics - lock-free atomic counters (Constitution VII)
	messagesProcessed atomic.Uint64 // Successfully broadcast messages
	messagesFailed    atomic.Uint64 // Messages with invalid format
	messagesDropped   atomic.Uint64 // Rate limited or CPU paused
	batchesSent       atomic.Uint64 // Number of batches sent
	consumerGroup     string        // Consumer group ID for metrics labels

	deadLetterCounter     *prometheus.CounterVec
	malformedTopicCounter prometheus.Counter
}

// ConsumerConfig holds configuration for creating a Kafka consumer.
// Required fields: Brokers, ConsumerGroup, Topics, Logger, Broadcast, ResourceGuard.
type ConsumerConfig struct {
	Brokers       []string        // Kafka/Redpanda broker addresses (required)
	ConsumerGroup string          // Consumer group ID for offset tracking (required)
	Topics        []string        // Topics to consume from (required)
	Logger        *zerolog.Logger // Structured logger for consumer events
	Broadcast     BroadcastFunc   // Callback for message delivery (required)
	ResourceGuard ResourceGuard   // Rate limiting and CPU brake (required)

	// Security configuration for managed Kafka/Redpanda services
	SASL *SASLConfig // SASL authentication (nil = no auth, for local dev)
	TLS  *TLSConfig  // TLS encryption (nil = no TLS, for local dev)

	// Optional batching configuration - improves throughput by reducing per-message overhead
	BatchSize    int           // Max messages per batch (default: 50, 0 = disabled)
	BatchTimeout time.Duration // Max wait for batch (default: 10ms)

	// Transport tuning (source of truth: envDefault tags in ServerConfig)
	FetchMaxWait     time.Duration // Max time broker waits before responding (default: 500ms)
	FetchMinBytes    int32         // Min bytes before broker responds (default: 1)
	FetchMaxBytes    int32         // Max bytes per fetch response (default: 10MB)
	SessionTimeout   time.Duration // Consumer group heartbeat timeout (default: 30s)
	RebalanceTimeout time.Duration // Max time for rebalance to complete (default: 60s)

	// Replay tuning
	ReplayFetchMaxBytes int32 // Max bytes for replay fetch (default: 5MB)

	// Backpressure tuning
	BackpressureCheckInterval time.Duration // CPU brake poll interval (default: 100ms)

	// Routing — optional; when set, channel extraction uses header and edition awareness.
	Namespace     string                  // Topic namespace (e.g. "prod") — used to build DLQ topic names.
	RulesProvider RoutingSnapshotProvider // Routing snapshot provider (nil = Community fallback always).
	DLQ           *DLQPool                // Dead-letter queue pool (nil = drop on invalid channel).
}

// NewConsumer creates a new Kafka consumer with the provided configuration.
// It validates required fields and initializes the franz-go client with
// optimal settings for trading workloads (500ms fetch wait, 10MB max fetch).
//
// The consumer starts at the latest offset (no historical replay on creation).
// Use Start to begin consuming and Stop for graceful shutdown.
//
// Returns an error if any required field is missing or client creation fails.
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("at least one broker is required")
	}
	if cfg.ConsumerGroup == "" {
		return nil, errors.New("consumer group is required")
	}
	if len(cfg.Topics) == 0 {
		return nil, errors.New("at least one topic is required")
	}
	if cfg.Broadcast == nil {
		return nil, errors.New("broadcast function is required")
	}
	if cfg.ResourceGuard == nil {
		return nil, errors.New("resource guard is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("logger is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Build client options
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topics...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()), // Start from latest
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
			cfg.Logger.Info().
				Interface("partitions", assigned).
				Msg("Partitions assigned")
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
			cfg.Logger.Info().
				Interface("partitions", revoked).
				Msg("Partitions revoked")
		}),
	}

	// Transport tuning (apply only if non-zero; omitting uses franz-go defaults)
	if cfg.FetchMaxWait > 0 {
		opts = append(opts, kgo.FetchMaxWait(cfg.FetchMaxWait))
	}
	if cfg.FetchMinBytes > 0 {
		opts = append(opts, kgo.FetchMinBytes(cfg.FetchMinBytes))
	}
	if cfg.FetchMaxBytes > 0 {
		opts = append(opts, kgo.FetchMaxBytes(cfg.FetchMaxBytes))
	}
	if cfg.SessionTimeout > 0 {
		opts = append(opts, kgo.SessionTimeout(cfg.SessionTimeout))
	}
	if cfg.RebalanceTimeout > 0 {
		opts = append(opts, kgo.RebalanceTimeout(cfg.RebalanceTimeout))
	}

	// Add SASL authentication if configured
	if cfg.SASL != nil {
		var mechanism = scram.Auth{
			User: cfg.SASL.Username,
			Pass: cfg.SASL.Password,
		}

		switch cfg.SASL.Mechanism {
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(mechanism.AsSha256Mechanism()))
			cfg.Logger.Info().
				Str("mechanism", "SCRAM-SHA-256").
				Str("username", cfg.SASL.Username).
				Msg("Kafka SASL authentication enabled")
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(mechanism.AsSha512Mechanism()))
			cfg.Logger.Info().
				Str("mechanism", "SCRAM-SHA-512").
				Str("username", cfg.SASL.Username).
				Msg("Kafka SASL authentication enabled")
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
		cfg.Logger.Info().
			Bool("insecure_skip_verify", cfg.TLS.InsecureSkipVerify).
			Str("ca_path", cfg.TLS.CAPath).
			Msg("Kafka TLS encryption enabled")
	}

	// Create franz-go client with all options
	client, err := kgo.NewClient(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	// Batching configuration (values provided by env-parsed config)
	batchSize := cfg.BatchSize
	batchTimeout := cfg.BatchTimeout
	batchEnabled := batchSize > 1

	// Backpressure check interval (validated > 0 by platform config layer)
	backpressureInterval := cfg.BackpressureCheckInterval

	consumer := &Consumer{
		client:        client,
		logger:        cfg.Logger,
		broadcast:     cfg.Broadcast,
		resourceGuard: cfg.ResourceGuard,
		ctx:           ctx,
		cancel:        cancel,
		batchSize:     batchSize,
		batchTimeout:  batchTimeout,
		batchEnabled:  batchEnabled,
		consumerGroup: cfg.ConsumerGroup,

		// Transport tuning (stored for replay consumer and backpressure)
		fetchMaxWait:              cfg.FetchMaxWait,
		fetchMinBytes:             cfg.FetchMinBytes,
		fetchMaxBytes:             cfg.FetchMaxBytes,
		replayFetchMaxBytes:       cfg.ReplayFetchMaxBytes,
		backpressureCheckInterval: backpressureInterval,

		// Routing
		namespace:     cfg.Namespace,
		rulesProvider: cfg.RulesProvider,
		dlq:           cfg.DLQ,
	}

	// Register routing Prometheus metrics once across all Consumer instances.
	// The orchestration pool creates one Consumer per active tenant, so
	// per-instance promauto calls would panic on the second registration.
	consumerRoutingMetrics.once.Do(func() {
		consumerRoutingMetrics.deadLetterCounter = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: MetricDeadLetterTotal,
			Help: "Total records routed to the dead-letter topic",
		}, []string{LabelTenant, LabelReason})
		consumerRoutingMetrics.malformedTopicCounter = promauto.NewCounter(prometheus.CounterOpts{
			Name: MetricMalformedTopicTotal,
			Help: "Total records dropped due to malformed topic name",
		})
	})
	consumer.deadLetterCounter = consumerRoutingMetrics.deadLetterCounter
	consumer.malformedTopicCounter = consumerRoutingMetrics.malformedTopicCounter

	if batchEnabled {
		cfg.Logger.Info().
			Int("batch_size", batchSize).
			Dur("batch_timeout", batchTimeout).
			Msg("Kafka message batching enabled")
	}

	return consumer, nil
}

// Start begins consuming messages from Kafka in a background goroutine.
// It returns immediately after launching the consumer loop.
// Call Stop to gracefully shut down the consumer.
func (c *Consumer) Start() error {
	c.logger.Info().Msg("Starting Kafka consumer")

	// Start consumer loop
	c.wg.Go(c.consumeLoop)

	return nil
}

// Stop gracefully stops the consumer and waits for pending messages to complete.
// It cancels the consumer context, waits for the consume loop to exit, then
// closes the Kafka client. Final metrics are logged before returning.
func (c *Consumer) Stop() error {
	c.logger.Info().Msg("Stopping Kafka consumer")

	// Cancel context to stop consume loop
	c.cancel()

	// Wait for consumer loop to finish
	c.wg.Wait()

	// Close client
	c.client.Close()

	c.logger.Info().
		Uint64("messages_processed", c.messagesProcessed.Load()).
		Uint64("messages_failed", c.messagesFailed.Load()).
		Msg("Kafka consumer stopped")

	return nil
}

// AddConsumeTopics dynamically adds topics to the consumer without triggering
// a full consumer group rebalance. This uses franz-go's AddConsumeTopics()
// which implements incremental cooperative rebalancing (KIP-429, Kafka 2.4+).
//
// Benefits over recreating the consumer:
//   - No stop-the-world rebalance (5-30s pause avoided)
//   - Existing partition assignments remain stable
//   - New topics are assigned incrementally
//   - Zero message loss during topic addition
//
// Thread Safety: Safe for concurrent use.
func (c *Consumer) AddConsumeTopics(topics ...string) {
	if len(topics) == 0 {
		return
	}

	c.client.AddConsumeTopics(topics...)

	c.logger.Info().
		Strs("topics", topics).
		Msg("Added topics to consumer (incremental assignment)")
}

// PauseFetchTopics stops the consumer from fetching the given topics.
// This is used to stop consuming deprovisioned tenant topics without
// triggering a rebalance. Paused topics persist until the consumer restarts.
//
// Note: franz-go does not support removing topics from a group consumer.
// PauseFetchTopics is the best available alternative — it eliminates
// fetch bandwidth waste while keeping the consumer group stable.
//
// Thread Safety: Safe for concurrent use.
func (c *Consumer) PauseFetchTopics(topics ...string) {
	if len(topics) == 0 {
		return
	}

	c.client.PauseFetchTopics(topics...)

	c.logger.Info().
		Strs("topics", topics).
		Msg("Paused fetching from deprovisioned topics")
}

// consumeLoop continuously polls for messages
func (c *Consumer) consumeLoop() {
	// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
	defer logging.RecoverPanic(*c.logger, "consumeLoop", nil)

	// If batching is disabled, use the old one-by-one processing
	if !c.batchEnabled {
		c.consumeLoopUnbatched()
		return
	}

	// Batching enabled: accumulate messages and flush periodically
	batch := make([]preparedMessage, 0, c.batchSize)
	flushTimer := time.NewTimer(c.batchTimeout)
	defer flushTimer.Stop()

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		// Send all messages in batch
		for _, msg := range batch {
			c.broadcast(msg.subject, msg.message)
			c.incrementProcessed(msg.topic)
		}

		c.incrementBatches()

		// Log batching metrics periodically (only when debug enabled)
		// IMPORTANT: Guard with level check to avoid expensive mutex lock in getBatchCount()
		// Cost when disabled: ~1ns (level check) vs ~100ns (modulo + mutex lock)
		if c.logger.GetLevel() <= zerolog.DebugLevel {
			if batches := c.getBatchCount(); batches%100 == 0 {
				c.logger.Debug().
					Uint64("batches_sent", batches).
					Int("last_batch_size", len(batch)).
					Msg("Kafka batching metrics")
			}
		}

		// Clear batch
		batch = batch[:0]
		flushTimer.Reset(c.batchTimeout)
	}

	for {
		select {
		case <-c.ctx.Done():
			// Flush remaining messages before exit
			flushBatch()
			return

		case <-flushTimer.C:
			// Timeout: flush accumulated batch
			flushBatch()

		default:
			// Poll for messages
			fetches := c.client.PollFetches(c.ctx)

			// Check for errors
			if errs := fetches.Errors(); len(errs) > 0 {
				for _, err := range errs {
					c.logger.Error().
						Err(err.Err).
						Str("topic", err.Topic).
						Int32("partition", err.Partition).
						Msg("Fetch error")
				}
			}

			// Accumulate records into batch
			fetches.EachRecord(func(record *kgo.Record) {
				msg := c.prepareMessage(record)
				if msg != nil {
					batch = append(batch, *msg)

					// Flush if batch is full
					if len(batch) >= c.batchSize {
						flushBatch()
					}
				}
			})
		}
	}
}

// consumeLoopUnbatched is the original implementation without batching (for backward compatibility)
func (c *Consumer) consumeLoopUnbatched() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Poll for messages
			fetches := c.client.PollFetches(c.ctx)

			// Check for errors
			if errs := fetches.Errors(); len(errs) > 0 {
				for _, err := range errs {
					c.logger.Error().
						Err(err.Err).
						Str("topic", err.Topic).
						Int32("partition", err.Partition).
						Msg("Fetch error")
				}
			}

			// Process records one by one
			fetches.EachRecord(func(record *kgo.Record) {
				c.processRecord(record)
			})
		}
	}
}

// preparedMessage holds a validated, ready-to-broadcast Kafka message.
type preparedMessage struct {
	topic   string
	subject string
	message []byte
}

// prepareMessage validates and prepares a message for batching
// Returns nil if message should be dropped (rate limited, invalid, etc.)
func (c *Consumer) prepareMessage(record *kgo.Record) *preparedMessage {
	// ============================================================================
	// LAYER 1: RATE LIMITING
	// ============================================================================
	allow, waitDuration := c.resourceGuard.AllowKafkaMessage(c.ctx)
	if !allow {
		c.incrementDropped(record.Topic)

		// Log every 100th drop to avoid log spam
		dropped := c.getDroppedCount()
		if dropped%100 == 0 {
			c.logger.Warn().
				Uint64("dropped_count", dropped).
				Dur("would_wait", waitDuration).
				Str("topic", record.Topic).
				Msg("Kafka rate limit exceeded - dropping messages")
		}
		return nil
	}

	// ============================================================================
	// LAYER 2: CPU EMERGENCY BRAKE - BACKPRESSURE MODE (zero message loss!)
	// ============================================================================
	// If CPU is critically high (>80% by default), WAIT instead of dropping
	// This creates natural backpressure - messages are never lost
	if c.resourceGuard.ShouldPauseKafka() {
		pauseStart := time.Now()
		pauseLogged := false

		// Wait until CPU recovers (with context cancellation support)
		for c.resourceGuard.ShouldPauseKafka() {
			if !pauseLogged {
				c.logger.Warn().
					Str("topic", record.Topic).
					Msg("CPU emergency brake - entering backpressure mode (waiting for CPU recovery)")
				pauseLogged = true
			}

			select {
			case <-c.ctx.Done():
				return nil // Context canceled, exit gracefully
			case <-time.After(c.backpressureCheckInterval):
				// Check CPU again
			}
		}

		if pauseLogged {
			c.logger.Info().
				Dur("pause_duration", time.Since(pauseStart)).
				Str("topic", record.Topic).
				Msg("CPU emergency brake released - resuming consumption")
		}
	}

	channel, reason, malformed := extractChannel(record, c.rulesProvider)
	if malformed {
		if c.malformedTopicCounter != nil {
			c.malformedTopicCounter.Inc()
		}
		c.incrementFailed()
		return nil
	}
	if reason != "" {
		c.routeToDLQ(record, reason)
		c.incrementFailed()
		return nil
	}

	return &preparedMessage{
		topic:   record.Topic,
		subject: channel,
		message: record.Value,
	}
}

// processRecord handles a single Kafka record with three-layer protection
//
// LAYER 1: Rate limiting (caps consumption at configured rate, e.g., 25 msg/sec)
// LAYER 2: CPU emergency brake (pauses when CPU exceeds threshold, e.g., 80%)
// LAYER 3: Direct broadcast (worker pool removed - broadcast is already non-blocking)
//
// This achieves 12K connections @ 30% CPU with minimal overhead.
// Without these protections, Kafka consumer blocks synchronously, causing plateau at 2.2K connections.
func (c *Consumer) processRecord(record *kgo.Record) {
	// ============================================================================
	// LAYER 1: RATE LIMITING
	// ============================================================================
	// Check if we're allowed to process this message based on configured rate limit
	// If rate limit exceeded, drop message and let Kafka handle redelivery
	allow, waitDuration := c.resourceGuard.AllowKafkaMessage(c.ctx)
	if !allow {
		c.incrementDropped(record.Topic)

		// Log every 100th drop to avoid log spam
		dropped := c.getDroppedCount()
		if dropped%100 == 0 {
			c.logger.Warn().
				Uint64("dropped_count", dropped).
				Dur("would_wait", waitDuration).
				Str("topic", record.Topic).
				Msg("Kafka rate limit exceeded - dropping messages")
		}
		return
	}

	// ============================================================================
	// LAYER 2: CPU EMERGENCY BRAKE - BACKPRESSURE MODE (zero message loss!)
	// ============================================================================
	// If CPU is critically high (>80% by default), WAIT instead of dropping
	// This creates natural backpressure:
	// - Consumer pauses, Kafka retains messages
	// - When CPU recovers (drops below lower threshold), processing resumes
	// - Zero messages are lost - critical for trading platforms!
	//
	// Trade-off: Consumer lag increases during high CPU, but all messages are delivered
	if c.resourceGuard.ShouldPauseKafka() {
		pauseStart := time.Now()
		pauseLogged := false

		// Wait until CPU recovers (with context cancellation support)
		for c.resourceGuard.ShouldPauseKafka() {
			// Log once when entering pause state
			if !pauseLogged {
				c.logger.Warn().
					Str("topic", record.Topic).
					Msg("CPU emergency brake - entering backpressure mode (waiting for CPU recovery)")
				pauseLogged = true
			}

			select {
			case <-c.ctx.Done():
				// Context canceled (shutdown), exit gracefully
				return
			case <-time.After(c.backpressureCheckInterval):
				// Check CPU again after short wait
			}
		}

		// Log recovery
		if pauseLogged {
			c.logger.Info().
				Dur("pause_duration", time.Since(pauseStart)).
				Str("topic", record.Topic).
				Msg("CPU emergency brake released - resuming consumption")
		}
	}

	channel, reason, malformed := extractChannel(record, c.rulesProvider)
	if malformed {
		if c.malformedTopicCounter != nil {
			c.malformedTopicCounter.Inc()
		}
		c.incrementFailed()
		return
	}
	if reason != "" {
		c.routeToDLQ(record, reason)
		c.incrementFailed()
		return
	}

	c.broadcast(channel, record.Value)
	c.incrementProcessed(record.Topic)

	c.logger.Debug().
		Str("channel", channel).
		Str("topic", record.Topic).
		Msg("Consumed Kafka message")
}

// routeToDLQ submits a record to the dead-letter queue using a non-blocking send.
// If the DLQ is nil or the channel is full, the record is dropped and counted.
func (c *Consumer) routeToDLQ(record *kgo.Record, reason string) {
	topicParts := strings.SplitN(record.Topic, ".", 3)
	tenant := ""
	if len(topicParts) >= 2 {
		tenant = topicParts[1]
	}

	if c.deadLetterCounter != nil {
		c.deadLetterCounter.WithLabelValues(tenant, reason).Inc()
	}

	if c.dlq == nil {
		return
	}

	dlqTopic := kafkautil.BuildTopicName(c.namespace, tenant, routing.DeadLetterTopicSuffix)
	dlqRec := &kgo.Record{
		Topic: dlqTopic,
		Key:   record.Key,
		Value: record.Value,
		Headers: append(slices.Clone(record.Headers),
			kgo.RecordHeader{Key: HeaderReason, Value: []byte(reason)},
		),
	}
	// Non-blocking submit; drop silently if queue full (deadLetterCounter already incremented above).
	c.dlq.TrySubmit(dlqJob{record: dlqRec, tenant: tenant, reason: reason})
}

// findHeader returns the value of the first header matching key, or nil.
func findHeader(record *kgo.Record, key string) []byte {
	for _, h := range record.Headers {
		if h.Key == key {
			return h.Value
		}
	}
	return nil
}

// extractChannel resolves the broadcast channel from a Kafka record.
// Returns (channel, reason, malformed):
//   - malformed=true: topic has fewer than 3 segments; drop silently, no DLQ.
//   - reason!="": channel is invalid/missing; caller should route to DLQ.
//   - otherwise: channel is valid.
func extractChannel(record *kgo.Record, rulesProvider RoutingSnapshotProvider) (channel, reason string, malformed bool) {
	topicParts := strings.SplitN(record.Topic, ".", 3)
	if len(topicParts) < 3 {
		return "", "", true
	}
	tenant := topicParts[1]

	var edition = license.Community
	if rulesProvider != nil {
		if snap, ok := rulesProvider.GetRoutingSnapshot(tenant); ok {
			edition = snap.Edition
		}
	}

	headerVal := findHeader(record, HeaderChannel)

	if headerVal == nil {
		if license.EditionHasFeature(edition, license.ChannelTopicRouting) {
			return "", ReasonMissingChannelHeader, false
		}
		// Community fallback: channel from record.Key.
		ch := string(record.Key)
		if ch == "" || !strings.Contains(ch, ".") {
			return "", ReasonInvalidChannelKey, false
		}
		return ch, "", false
	}

	// Header present: validate it's a well-formed channel (at least two segments)
	// then check tenant prefix matches topic tenant.
	ch := string(headerVal)
	dot := strings.IndexByte(ch, '.')
	if dot <= 0 {
		return "", ReasonInvalidChannelKey, false
	}
	if ch[:dot] != tenant {
		return "", ReasonTenantPrefixMismatch, false
	}
	return ch, "", false
}

// GetMetrics returns current consumer metrics: messages processed successfully,
// messages that failed validation, and messages dropped due to rate limiting.
// Thread-safe for concurrent access.
func (c *Consumer) GetMetrics() (processed, failed, dropped uint64) {
	return c.messagesProcessed.Load(), c.messagesFailed.Load(), c.messagesDropped.Load()
}

func (c *Consumer) incrementProcessed(topic string) {
	c.messagesProcessed.Add(1)
	metrics.IncrementKafkaMessages(topic, c.consumerGroup)
}

func (c *Consumer) incrementFailed() {
	c.messagesFailed.Add(1)
}

func (c *Consumer) incrementDropped(topic string) {
	c.messagesDropped.Add(1)
	metrics.IncrementKafkaDropped(topic, c.consumerGroup)
}

func (c *Consumer) getDroppedCount() uint64 {
	return c.messagesDropped.Load()
}

func (c *Consumer) incrementBatches() {
	c.batchesSent.Add(1)
}

func (c *Consumer) getBatchCount() uint64 {
	return c.batchesSent.Load()
}

// ReplayMessage represents a message replayed from Kafka
type ReplayMessage struct {
	Topic     string
	Partition int32
	Offset    int64
	Subject   string // The Kafka Key = broadcast subject
	Data      []byte
}

// ReplayFromOffsets creates a temporary consumer and replays messages from specified offsets
// This is used for client reconnection to retrieve missed messages
//
// Parameters:
//   - ctx: Context for cancellation (should have timeout, e.g. 5 seconds)
//   - topicOffsets: Map of topic -> starting offset to replay from
//   - maxMessages: Maximum number of messages to replay (prevents overwhelming client)
//   - subscriptions: Filter to only include messages for these channels (e.g. ["BTC.trade", "ETH.trade"])
//
// Returns:
//   - []ReplayMessage: Messages from [startOffset → current] filtered by subscriptions
//   - error: If consumer creation or replay fails
//
// Implementation notes:
//   - Creates isolated consumer (doesn't affect main consumer group)
//   - Reads from specific offsets to current position
//   - Filters by client subscriptions (only return relevant messages)
//   - Respects context timeout (typically 5 seconds for reconnect)
//   - Closes temporary consumer after replay
//
// Usage example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	messages, err := consumer.ReplayFromOffsets(ctx, map[string]int64{
//	    "sukko.trades": 12345,  // Start from offset 12345
//	    "sukko.liquidity": 67890,
//	}, 100, []string{"BTC.trade", "ETH.trade"})
func (c *Consumer) ReplayFromOffsets(
	ctx context.Context,
	topicOffsets map[string]int64,
	maxMessages int,
	subscriptions []string,
) ([]ReplayMessage, error) {
	if len(topicOffsets) == 0 {
		return nil, errors.New("at least one topic offset is required")
	}

	c.logger.Info().
		Interface("topic_offsets", topicOffsets).
		Int("max_messages", maxMessages).
		Strs("subscriptions", subscriptions).
		Msg("Starting Kafka replay from offsets")

	// Extract topics from offsets map
	topics := make([]string, 0, len(topicOffsets))
	for topic := range topicOffsets {
		topics = append(topics, topic)
	}

	// Create temporary consumer for replay (isolated from main consumer group)
	// Use unique consumer group to avoid affecting main consumption
	tempGroupID := fmt.Sprintf("replay-%d", time.Now().UnixNano())

	// Build offset map for kgo
	startOffsets := make(map[string]map[int32]kgo.Offset)
	for topic, offset := range topicOffsets {
		startOffsets[topic] = map[int32]kgo.Offset{
			-1: kgo.NewOffset().At(offset), // -1 means all partitions
		}
	}

	brokers, ok := c.client.OptValue(kgo.SeedBrokers).([]string)
	if !ok || len(brokers) == 0 {
		return nil, errors.New("replay: unable to extract broker addresses from consumer")
	}

	replayOpts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(tempGroupID),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumePartitions(startOffsets), // Start from specified offsets
	}
	if c.fetchMaxWait > 0 {
		replayOpts = append(replayOpts, kgo.FetchMaxWait(c.fetchMaxWait))
	}
	if c.fetchMinBytes > 0 {
		replayOpts = append(replayOpts, kgo.FetchMinBytes(c.fetchMinBytes))
	}
	if c.replayFetchMaxBytes > 0 {
		replayOpts = append(replayOpts, kgo.FetchMaxBytes(c.replayFetchMaxBytes))
	} else if c.fetchMaxBytes > 0 {
		replayOpts = append(replayOpts, kgo.FetchMaxBytes(c.fetchMaxBytes))
	}

	tempClient, err := kgo.NewClient(replayOpts...)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create replay consumer")
		return nil, fmt.Errorf("failed to create replay consumer: %w", err)
	}
	defer tempClient.Close()

	// Create subscription filter set for O(1) lookup
	subSet := make(map[string]struct{}, len(subscriptions))
	for _, sub := range subscriptions {
		subSet[sub] = struct{}{}
	}

	// Collect replayed messages
	var messages []ReplayMessage
	messagesRead := 0

	// Poll for messages until we reach current position or max messages
	for messagesRead < maxMessages {
		select {
		case <-ctx.Done():
			c.logger.Warn().
				Int("messages_read", messagesRead).
				Msg("Replay canceled by context timeout")
			return messages, fmt.Errorf("replay canceled: %w", ctx.Err())
		default:
		}

		fetches := tempClient.PollFetches(ctx)
		if fetches.IsClientClosed() {
			break
		}

		// Check for errors
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, err := range errs {
				c.logger.Warn().
					Err(err.Err).
					Str("topic", err.Topic).
					Int32("partition", err.Partition).
					Msg("Replay fetch error")
			}
		}

		// No more records, we've reached current position
		if fetches.NumRecords() == 0 {
			break
		}

		// Process records
		fetches.EachRecord(func(record *kgo.Record) {
			if messagesRead >= maxMessages {
				return // Stop if we've hit the limit
			}

			// Parse message to get subject
			msg := c.prepareMessage(record)
			if msg == nil {
				return
			}

			// Filter by subscriptions (only return messages client is subscribed to)
			// The subject IS the channel (e.g., "BTC.trade", "BTC.balances.user123")
			if len(subSet) > 0 {
				if _, subscribed := subSet[msg.subject]; !subscribed {
					return // Skip messages for unsubscribed channels
				}
			}

			// Add to replay results
			messages = append(messages, ReplayMessage{
				Topic:     record.Topic,
				Partition: record.Partition,
				Offset:    record.Offset,
				Subject:   msg.subject,
				Data:      msg.message,
			})
			messagesRead++
		})
	}

	c.logger.Info().
		Int("messages_replayed", len(messages)).
		Int("messages_read", messagesRead).
		Msg("Kafka replay completed")

	return messages, nil
}
