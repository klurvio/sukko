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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/sony/gobreaker/v2"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	kafkautil "github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/shared/protocol"
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

// ProducerConfig holds configuration for creating a Kafka producer.
type ProducerConfig struct {
	Brokers        []string        // Kafka/Redpanda broker addresses (required)
	TopicNamespace string          // Topic namespace: "local", "dev", "staging", "prod" (required)
	ClientID       string          // Client ID for Kafka connection (optional, defaults to "odin-ws-producer-{hostname}")
	Logger         *zerolog.Logger // Structured logger (optional)

	// Security configuration for managed Kafka/Redpanda services
	SASL *SASLConfig // SASL authentication (nil = no auth, for local dev)
	TLS  *TLSConfig  // TLS encryption (nil = no TLS, for local dev)

	// Topic validation (optional - if nil, topics are not validated)
	TenantRegistry types.TenantRegistry // Registry to validate provisioned topics

	// Circuit breaker settings (optional - sensible defaults provided)
	CircuitBreakerTimeout      time.Duration // Time before half-open (default: 30s)
	CircuitBreakerMaxFailures  uint32        // Consecutive failures to trip (default: 5)
	CircuitBreakerHalfOpenReqs uint32        // Requests allowed in half-open (default: 1)

	// Topic cache settings
	TopicCacheTTL time.Duration // TTL for provisioned topic cache (default: 60s)

	// DefaultTenantID is used when channel has no tenant prefix.
	// For channels like "BTC.trade" (2 parts), prepends this tenant.
	// When empty, channels must be in full internal format (3+ parts).
	DefaultTenantID string

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
	client          *kgo.Client
	topicNamespace  string
	defaultTenantID string
	logger          *zerolog.Logger
	stats           ProducerStats
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	closed          bool

	// Circuit breaker for Kafka connection
	circuitBreaker *gobreaker.CircuitBreaker[any]

	// Topic validation
	tenantRegistry    types.TenantRegistry
	provisionedTopics map[string]bool
	topicCacheMu      sync.RWMutex
	topicCacheExpiry  time.Time
	topicCacheTTL     time.Duration
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
	// Example: "odin-ws-producer-odin-ws-7f8d9c4b5-abc123" in Kubernetes
	clientID := cfg.ClientID
	if clientID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		clientID = "odin-ws-producer-" + hostname
	}
	// Producer tuning (values provided by env-parsed config)
	batchMaxBytes := cfg.BatchMaxBytes
	maxBufferedRecs := cfg.MaxBufferedRecs
	recordRetries := cfg.RecordRetries

	// Circuit breaker settings (values provided by env-parsed config)
	cbTimeout := cfg.CircuitBreakerTimeout
	cbMaxFailures := cfg.CircuitBreakerMaxFailures
	cbHalfOpenReqs := cfg.CircuitBreakerHalfOpenReqs

	// Topic cache TTL (value provided by env-parsed config)
	topicCacheTTL := cfg.TopicCacheTTL

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
		client:            client,
		topicNamespace:    strings.ToLower(strings.TrimSpace(cfg.TopicNamespace)),
		defaultTenantID:   cfg.DefaultTenantID,
		logger:            cfg.Logger,
		ctx:               ctx,
		cancel:            cancel,
		circuitBreaker:    cb,
		tenantRegistry:    cfg.TenantRegistry,
		provisionedTopics: make(map[string]bool),
		topicCacheTTL:     topicCacheTTL,
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
	// Extract tenant/category to build topic.
	// When defaultTenantID is set, channels with fewer than 3 parts use the default tenant.
	var tenant, category string
	var err error
	if p.defaultTenantID != "" {
		tenant, category, err = p.parseChannelWithDefault(channel)
	} else {
		tenant, category, err = parseChannel(channel)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidChannel, err)
	}

	topic := kafkautil.BuildTopicName(p.topicNamespace, tenant, category)

	// Validate topic is provisioned (if registry is configured)
	if p.tenantRegistry != nil && !p.isTopicProvisioned(ctx, topic) {
		return fmt.Errorf("%w: %s", ErrTopicNotProvisioned, topic)
	}

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(channel),
		Value: data,
		Headers: []kgo.RecordHeader{
			{Key: "client_id", Value: []byte(strconv.FormatInt(clientID, 10))},
			{Key: "source", Value: []byte("ws-client")},
			{Key: "timestamp", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
		},
	}

	// Use synchronous produce for reliability (client expects ack)
	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		p.stats.MessagesFailed.Add(1)
		if p.logger != nil {
			p.logger.Error().
				Err(err).
				Int64("client_id", clientID).
				Str("channel", channel).
				Str("topic", topic).
				Msg("Failed to publish message to Kafka")
		}
		return fmt.Errorf("kafka produce failed: %w", err)
	}

	p.stats.MessagesPublished.Add(1)

	if p.logger != nil {
		p.logger.Debug().
			Int64("client_id", clientID).
			Str("channel", channel).
			Str("topic", topic).
			Int("data_size", len(data)).
			Msg("Published message to Kafka")
	}

	return nil
}

// parseChannel extracts tenant and category from an internal channel.
// Channel format: {tenant}.{identifier}.{category} (minimum 3 parts)
//
// Example:
//
//	"acme.BTC.trade" → tenant: "acme", category: "trade"
//	"acme.user123.balances" → tenant: "acme", category: "balances"
//	"acme.group.chat.community" → tenant: "acme", category: "community"
func parseChannel(channel string) (tenant, category string, err error) {
	parts := strings.Split(channel, ".")

	// Must be mapped format: tenant.identifier.category (3+ parts)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("channel must have at least 3 parts (tenant.identifier.category), got %d", len(parts))
	}

	tenant = parts[0]
	category = parts[len(parts)-1]

	if tenant == "" {
		return "", "", errors.New("tenant (first segment) cannot be empty")
	}
	if category == "" {
		return "", "", errors.New("category (last segment) cannot be empty")
	}

	return tenant, category, nil
}

// parseChannelWithDefault extracts tenant and category, using default tenant if needed.
// Handles channels with fewer than 3 parts by prepending the default tenant.
//
// Examples with DefaultTenantID = "odin":
//
//	"odin.BTC.trade"  → tenant: "odin", category: "trade" (3+ parts: explicit tenant)
//	"BTC.trade"       → tenant: "odin", category: "trade" (2 parts: use default)
//	"trade"           → tenant: "odin", category: "trade" (1 part: use default)
func (p *Producer) parseChannelWithDefault(channel string) (tenant, category string, err error) {
	parts := strings.Split(channel, ".")

	switch len(parts) {
	case 0:
		return "", "", errors.New("channel cannot be empty")
	case 1:
		// Just category: "trade" → use default tenant
		if parts[0] == "" {
			return "", "", errors.New("channel cannot be empty")
		}
		return p.defaultTenantID, parts[0], nil
	case 2:
		// identifier.category: "BTC.trade" → use default tenant
		if parts[0] == "" || parts[1] == "" {
			return "", "", errors.New("channel parts cannot be empty")
		}
		return p.defaultTenantID, parts[1], nil
	default:
		// 3+ parts: tenant.identifier.category — delegate to standard parser
		return parseChannel(channel)
	}
}

// isTopicProvisioned checks if a topic exists in the cache or registry.
// Uses a TTL-based cache to avoid querying the registry on every publish.
func (p *Producer) isTopicProvisioned(ctx context.Context, topic string) bool {
	// Check cache first
	p.topicCacheMu.RLock()
	if time.Now().Before(p.topicCacheExpiry) {
		if provisioned, ok := p.provisionedTopics[topic]; ok {
			p.topicCacheMu.RUnlock()
			return provisioned
		}
	}
	p.topicCacheMu.RUnlock()

	// Cache miss or expired - refresh cache
	p.refreshTopicCache(ctx)

	// Check again after refresh
	p.topicCacheMu.RLock()
	defer p.topicCacheMu.RUnlock()
	return p.provisionedTopics[topic]
}

// refreshTopicCache refreshes the provisioned topics cache from the registry.
func (p *Producer) refreshTopicCache(ctx context.Context) {
	p.topicCacheMu.Lock()
	defer p.topicCacheMu.Unlock()

	// Double-check after acquiring write lock
	if time.Now().Before(p.topicCacheExpiry) {
		return
	}

	// Query shared topics
	sharedTopics, err := p.tenantRegistry.GetSharedTenantTopics(ctx, p.topicNamespace)
	if err != nil {
		if p.logger != nil {
			p.logger.Warn().Err(err).Msg("Failed to refresh shared topic cache")
		}
		return
	}

	// Query dedicated tenants
	dedicatedTenants, err := p.tenantRegistry.GetDedicatedTenants(ctx, p.topicNamespace)
	if err != nil {
		if p.logger != nil {
			p.logger.Warn().Err(err).Msg("Failed to refresh dedicated topic cache")
		}
		return
	}

	// Rebuild cache
	newCache := make(map[string]bool)
	for _, topic := range sharedTopics {
		newCache[topic] = true
	}
	for _, tenant := range dedicatedTenants {
		for _, topic := range tenant.Topics {
			newCache[topic] = true
		}
	}

	p.provisionedTopics = newCache
	p.topicCacheExpiry = time.Now().Add(p.topicCacheTTL)

	if p.logger != nil {
		p.logger.Debug().
			Int("topic_count", len(newCache)).
			Dur("ttl", p.topicCacheTTL).
			Msg("Refreshed provisioned topic cache")
	}
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
	p.mu.RLock()
	defer p.mu.RUnlock()
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
