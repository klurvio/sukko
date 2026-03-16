// Package kafkabackend provides the Kafka/Redpanda implementation of the
// MessageBackend interface. It wraps the existing multi-tenant consumer pool
// and producer for full persistence, offset-based replay, and tenant isolation.
package kafkabackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/Toniq-Labs/odin-ws/internal/server/backend"
	"github.com/Toniq-Labs/odin-ws/internal/server/broadcast"
	"github.com/Toniq-Labs/odin-ws/internal/server/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/server/limits"
	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/server/orchestration"
	kafkautil "github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	"github.com/Toniq-Labs/odin-ws/internal/shared/provapi"
)

// backendName identifies this backend in metrics labels and logging.
const backendName = "kafka"

// KafkaBackend wraps the existing Kafka/Redpanda consumer pool and producer
// behind the MessageBackend interface. It provides full persistence,
// offset-based replay, and multi-tenant consumer isolation.
type KafkaBackend struct {
	pool          *orchestration.MultiTenantConsumerPool
	producer      *kafka.Producer
	adminClient   *kadm.Client
	kgoClient     *kgo.Client // underlying kgo client for admin (closed separately)
	topicRegistry *provapi.StreamTopicRegistry
	logger        zerolog.Logger
	healthy       atomic.Bool
	wg            sync.WaitGroup // tracks in-flight topic update goroutines

	defaultPartitions        int
	defaultReplicationFactor int
	namespace                string        // topic namespace for registry queries
	topicCreationTimeout     time.Duration // timeout for admin topic creation
}

// Config contains all configuration for the Kafka backend.
// Fields are populated by the wiring code in main.go from the top-level server config.
type Config struct {
	// Kafka/Redpanda broker addresses
	Brokers []string

	// Topic namespace (e.g., "prod", "dev")
	Namespace string

	// Deployment environment (e.g., "dev", "stg", "prod")
	Environment string

	// SASL authentication
	SASLEnabled   bool
	SASLMechanism string
	SASLUsername  string
	SASLPassword  string

	// TLS encryption
	TLSEnabled  bool
	TLSInsecure bool
	TLSCAPath   string

	// Consumer toggle (false = connection-only mode for loadtesting)
	KafkaConsumerEnabled bool

	// Topic refresh interval for periodic discovery
	TopicRefreshInterval time.Duration

	// Default tenant ID for channels without tenant prefix
	DefaultTenantID string

	// TopicCreationTimeout is the timeout for admin topic creation operations.
	TopicCreationTimeout time.Duration

	// Kafka admin topic defaults
	DefaultPartitions        int
	DefaultReplicationFactor int
	DefaultRetentionMs       int64

	// ResourceGuard fields
	MaxKafkaMessagesPerSec   int
	MaxBroadcastsPerSec      int
	RateLimitBurstMultiplier int
	CPUPauseThreshold        float64
	CPUPauseThresholdLower   float64
	CPURejectThreshold       float64
	CPURejectThresholdLower  float64

	// Broadcast bus for message distribution
	BroadcastBus broadcast.Bus

	// Logger is the structured logger passed by the caller.
	Logger zerolog.Logger

	// Kafka producer tuning
	ProducerBatchMaxBytes             int
	ProducerMaxBufferedRecs           int
	ProducerRecordRetries             int
	ProducerCircuitBreakerTimeout     time.Duration
	ProducerCircuitBreakerMaxFailures int
	ProducerCircuitBreakerHalfOpen    int
	ProducerTopicCacheTTL             time.Duration

	// Kafka consumer batch tuning
	KafkaBatchSize    int
	KafkaBatchTimeout time.Duration

	// Kafka consumer transport tuning
	KafkaFetchMaxWait              time.Duration
	KafkaFetchMinBytes             int32
	KafkaFetchMaxBytes             int32
	KafkaSessionTimeout            time.Duration
	KafkaRebalanceTimeout          time.Duration
	KafkaReplayFetchMaxBytes       int32
	KafkaBackpressureCheckInterval time.Duration

	// Provisioning gRPC connection
	ProvisioningGRPCAddr  string
	GRPCReconnectDelay    time.Duration
	GRPCReconnectMaxDelay time.Duration
}

// New creates a new Kafka backend with the provided configuration.
// It creates the ResourceGuard, StreamTopicRegistry, MultiTenantConsumerPool,
// and Producer. Returns an error on any initialization failure (fail fast).
func New(cfg Config) (*KafkaBackend, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka backend: at least one broker is required")
	}
	if cfg.KafkaConsumerEnabled && cfg.BroadcastBus == nil {
		return nil, errors.New("kafka backend: broadcast bus is required when consumer is enabled")
	}

	logger := cfg.Logger.With().Str("component", "kafka-backend").Logger()

	// Resolve topic namespace
	topicNamespace := kafkautil.ResolveNamespace("", cfg.Environment)
	if cfg.Namespace != "" {
		topicNamespace = kafkautil.ResolveNamespace(cfg.Namespace, cfg.Environment)
	}

	// Build SASL config if enabled
	var saslConfig *kafka.SASLConfig
	if cfg.SASLEnabled {
		saslConfig = &kafka.SASLConfig{
			Mechanism: cfg.SASLMechanism,
			Username:  cfg.SASLUsername,
			Password:  cfg.SASLPassword,
		}
	}

	// Build TLS config if enabled
	var tlsConfig *kafka.TLSConfig
	if cfg.TLSEnabled {
		tlsConfig = &kafka.TLSConfig{
			Enabled:            true,
			InsecureSkipVerify: cfg.TLSInsecure,
			CAPath:             cfg.TLSCAPath,
		}
	}

	defaultPartitions := max(cfg.DefaultPartitions, 1)
	defaultReplicationFactor := max(cfg.DefaultReplicationFactor, 1)

	kb := &KafkaBackend{
		logger:                   logger,
		defaultPartitions:        defaultPartitions,
		defaultReplicationFactor: defaultReplicationFactor,
		namespace:                topicNamespace,
		topicCreationTimeout:     cfg.TopicCreationTimeout,
	}

	// Create Kafka admin client for on-demand topic creation (uses same brokers/auth)
	adminKgoOpts, err := buildKgoOpts(cfg.Brokers, saslConfig, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("kafka backend: build admin options: %w", err)
	}
	adminKgoClient, err := kgo.NewClient(adminKgoOpts...)
	if err != nil {
		return nil, fmt.Errorf("kafka backend: create admin client: %w", err)
	}
	kb.kgoClient = adminKgoClient
	kb.adminClient = kadm.NewClient(adminKgoClient)

	// Create Kafka producer for client message publishing
	producerLogger := cfg.Logger

	kb.producer, err = kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                    cfg.Brokers,
		TopicNamespace:             topicNamespace,
		Logger:                     &producerLogger,
		SASL:                       saslConfig,
		TLS:                        tlsConfig,
		DefaultTenantID:            cfg.DefaultTenantID,
		BatchMaxBytes:              int32(min(cfg.ProducerBatchMaxBytes, math.MaxInt32)), //nolint:gosec // Bounds validated in ServerConfig.Validate()
		MaxBufferedRecs:            cfg.ProducerMaxBufferedRecs,
		RecordRetries:              cfg.ProducerRecordRetries,
		CircuitBreakerTimeout:      cfg.ProducerCircuitBreakerTimeout,
		CircuitBreakerMaxFailures:  uint32(min(cfg.ProducerCircuitBreakerMaxFailures, math.MaxUint32)), //nolint:gosec // Bounds validated in ServerConfig.Validate()
		CircuitBreakerHalfOpenReqs: uint32(min(cfg.ProducerCircuitBreakerHalfOpen, math.MaxUint32)),    //nolint:gosec // Bounds validated in ServerConfig.Validate()
		TopicCacheTTL:              cfg.ProducerTopicCacheTTL,
	})
	if err != nil {
		kb.kgoClient.Close()
		return nil, fmt.Errorf("kafka backend: create producer: %w", err)
	}

	logger.Info().
		Str("namespace", topicNamespace).
		Msg("Kafka producer initialized")

	if cfg.KafkaConsumerEnabled {
		// Create resource guard for CPU brake (shared across pool)
		poolLogger := cfg.Logger
		resourceGuard := limits.NewResourceGuard(limits.ResourceGuardConfig{
			MaxKafkaMessagesPerSec:   cfg.MaxKafkaMessagesPerSec,
			MaxBroadcastsPerSec:      cfg.MaxBroadcastsPerSec,
			RateLimitBurstMultiplier: cfg.RateLimitBurstMultiplier,
			CPUPauseThreshold:        cfg.CPUPauseThreshold,
			CPUPauseThresholdLower:   cfg.CPUPauseThresholdLower,
			CPURejectThreshold:       cfg.CPURejectThreshold,
			CPURejectThresholdLower:  cfg.CPURejectThresholdLower,
		}, poolLogger, &atomic.Int64{})

		// Create gRPC stream-backed topic registry for tenant topic discovery
		kb.topicRegistry, err = provapi.NewStreamTopicRegistry(provapi.StreamTopicRegistryConfig{
			GRPCAddr:          cfg.ProvisioningGRPCAddr,
			Namespace:         topicNamespace,
			ReconnectDelay:    cfg.GRPCReconnectDelay,
			ReconnectMaxDelay: cfg.GRPCReconnectMaxDelay,
			MetricPrefix:      "ws",
			Logger:            poolLogger,
		})
		if err != nil {
			_ = kb.producer.Close() // Best-effort cleanup; already returning constructor error
			kb.kgoClient.Close()
			return nil, fmt.Errorf("kafka backend: create topic registry: %w", err)
		}

		// Create multi-tenant consumer pool
		kb.pool, err = orchestration.NewMultiTenantConsumerPool(orchestration.MultiTenantPoolConfig{
			Brokers:           cfg.Brokers,
			Namespace:         topicNamespace,
			Environment:       strings.ToLower(strings.TrimSpace(cfg.Environment)),
			Registry:          kb.topicRegistry,
			BroadcastBus:      cfg.BroadcastBus,
			ResourceGuard:     resourceGuard,
			Logger:            poolLogger,
			SASL:              saslConfig,
			TLS:               tlsConfig,
			RefreshInterval:   cfg.TopicRefreshInterval,
			KafkaBatchSize:    cfg.KafkaBatchSize,
			KafkaBatchTimeout: cfg.KafkaBatchTimeout,
			// Consumer transport tuning
			KafkaFetchMaxWait:              cfg.KafkaFetchMaxWait,
			KafkaFetchMinBytes:             cfg.KafkaFetchMinBytes,
			KafkaFetchMaxBytes:             cfg.KafkaFetchMaxBytes,
			KafkaSessionTimeout:            cfg.KafkaSessionTimeout,
			KafkaRebalanceTimeout:          cfg.KafkaRebalanceTimeout,
			KafkaReplayFetchMaxBytes:       cfg.KafkaReplayFetchMaxBytes,
			KafkaBackpressureCheckInterval: cfg.KafkaBackpressureCheckInterval,
			Metrics:                        &metrics.MultiTenantPoolMetricsAdapter{},
		})
		if err != nil {
			_ = kb.topicRegistry.Close() // Best-effort cleanup; already returning constructor error
			_ = kb.producer.Close()      // Best-effort cleanup; already returning constructor error
			kb.kgoClient.Close()
			return nil, fmt.Errorf("kafka backend: create consumer pool: %w", err)
		}

		// Wire gRPC stream topic registry to trigger on-demand pool refresh
		// and on-demand topic creation via kadm
		kb.topicRegistry.SetOnUpdate(func() {
			// Run asynchronously to avoid blocking the gRPC stream receive goroutine.
			// ensureTopicsExist has its own 30s timeout; RefreshTopics is non-blocking.
			kb.wg.Go(func() {
				defer logging.RecoverPanic(kb.logger, "topic_update", nil)
				kb.ensureTopicsExist()
				kb.pool.RefreshTopics()
			})
		})

		logger.Info().
			Bool("consumer_enabled", true).
			Msg("Kafka consumer pool created (gRPC topic streaming)")
	} else {
		logger.Info().
			Bool("consumer_enabled", false).
			Msg("Kafka consumer DISABLED — connection-only mode for loadtesting")
	}

	return kb, nil
}

// Start begins the Kafka backend's consumption loop.
// For the consumer pool, this starts topic discovery and message consumption.
func (kb *KafkaBackend) Start(_ context.Context) error {
	if kb.pool != nil {
		if err := kb.pool.Start(); err != nil {
			return fmt.Errorf("kafka backend start: %w", err)
		}
		metrics.SetKafkaConnected(true)
		kb.logger.Info().Msg("Kafka consumer pool started")
	}

	kb.healthy.Store(true)
	metrics.SetBackendHealthy(backendName, true)
	return nil
}

// Publish sends a client-published message through the Kafka producer.
func (kb *KafkaBackend) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
	if channel == "" {
		return fmt.Errorf("%w: channel is required", backend.ErrPublishFailed)
	}
	if kb.producer == nil {
		return fmt.Errorf("%w: kafka producer not initialized", backend.ErrPublishFailed)
	}
	start := time.Now()
	err := kb.producer.Publish(ctx, clientID, channel, data)
	metrics.RecordBackendPublishLatency(backendName, time.Since(start).Seconds())
	if err != nil {
		metrics.RecordBackendPublishError(backendName)
		return fmt.Errorf("kafka backend publish: %w", err)
	}
	metrics.RecordBackendPublish(backendName)
	return nil
}

// Replay returns messages from the specified offsets for client reconnection.
// Delegates to the shared consumer's ReplayFromOffsets method.
func (kb *KafkaBackend) Replay(ctx context.Context, req backend.ReplayRequest) ([]backend.ReplayMessage, error) {
	metrics.RecordBackendReplayRequest(backendName)

	if kb.pool == nil {
		return nil, nil
	}

	sharedConsumer := kb.pool.GetSharedConsumer()
	if sharedConsumer == nil {
		return nil, nil
	}

	kafkaMessages, err := sharedConsumer.ReplayFromOffsets(ctx, req.Positions, req.MaxMessages, req.Subscriptions)
	if err != nil {
		return nil, fmt.Errorf("kafka replay: %w", err)
	}

	// Convert kafka.ReplayMessage → backend.ReplayMessage
	messages := make([]backend.ReplayMessage, len(kafkaMessages))
	for i, m := range kafkaMessages {
		messages[i] = backend.ReplayMessage{
			Subject: m.Subject,
			Data:    m.Data,
		}
	}

	metrics.RecordBackendReplayMessages(backendName, len(messages))
	return messages, nil
}

// IsHealthy returns true if the Kafka backend is operational.
func (kb *KafkaBackend) IsHealthy() bool {
	return kb.healthy.Load()
}

// Shutdown gracefully stops the Kafka backend.
// Stops pool, closes producer, closes topic registry (continues on individual failures per Constitution IV).
func (kb *KafkaBackend) Shutdown(ctx context.Context) error {
	kb.healthy.Store(false)
	metrics.SetBackendHealthy(backendName, false)

	// Close topic registry FIRST to stop gRPC stream and prevent new SetOnUpdate callbacks.
	// This must happen before wg.Wait to prevent wg.Add after wg.Wait returns.
	if kb.topicRegistry != nil {
		if err := kb.topicRegistry.Close(); err != nil {
			kb.logger.Error().Err(err).Msg("Error closing topic registry")
		}
	}

	// Wait for in-flight topic update goroutines to finish
	done := make(chan struct{})
	go func() {
		defer logging.RecoverPanic(kb.logger, "shutdown_wait", nil)
		kb.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Topic update goroutines exited cleanly
	case <-ctx.Done():
		kb.logger.Warn().Msg("Shutdown timed out waiting for topic update goroutines")
	}

	var errs []error

	// Stop consumer pool
	if kb.pool != nil {
		if err := kb.pool.Stop(); err != nil {
			kb.logger.Error().Err(err).Msg("Error stopping consumer pool")
			errs = append(errs, fmt.Errorf("stop consumer pool: %w", err))
		}
	}

	// Close producer
	if kb.producer != nil {
		if err := kb.producer.Close(); err != nil {
			kb.logger.Error().Err(err).Msg("Error closing Kafka producer")
			errs = append(errs, fmt.Errorf("close producer: %w", err))
		}
	}

	// Close admin kgo client
	if kb.kgoClient != nil {
		kb.kgoClient.Close()
	}

	metrics.SetKafkaConnected(false)
	kb.logger.Info().Msg("Kafka backend shut down")

	return errors.Join(errs...)
}

// ensureTopicsExist reads from the topic registry and creates missing Kafka topics
// via the kadm admin client. TopicAlreadyExists is treated as a no-op.
func (kb *KafkaBackend) ensureTopicsExist() {
	if kb.adminClient == nil || kb.topicRegistry == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), kb.topicCreationTimeout)
	defer cancel()

	// Collect all topics from registry
	sharedTopics, err := kb.topicRegistry.GetSharedTenantTopics(ctx, kb.namespace)
	if err != nil {
		kb.logger.Error().Err(err).Msg("Failed to get shared topics for topic creation")
		return
	}

	dedicatedTenants, err := kb.topicRegistry.GetDedicatedTenants(ctx, kb.namespace)
	if err != nil {
		kb.logger.Error().Err(err).Msg("Failed to get dedicated tenants for topic creation")
		return
	}

	var allTopics []string
	allTopics = append(allTopics, sharedTopics...)
	for _, dt := range dedicatedTenants {
		allTopics = append(allTopics, dt.Topics...)
	}

	if len(allTopics) == 0 {
		return
	}

	// Create topics (kadm handles TopicAlreadyExists gracefully)
	partitions := int32(kb.defaultPartitions)               //nolint:gosec // G115: partition count is validated at config load time and always small
	replicationFactor := int16(kb.defaultReplicationFactor) //nolint:gosec // G115: replication factor is validated at config load time and always small

	resp, err := kb.adminClient.CreateTopics(ctx, partitions, replicationFactor, nil, allTopics...)
	if err != nil {
		kb.logger.Error().Err(err).Msg("Failed to create topics")
		return
	}

	for _, topic := range resp.Sorted() {
		if topic.Err != nil {
			// kadm returns *kadm.TopicError — check for "already exists"
			if isTopicAlreadyExistsError(topic.Err) {
				continue
			}
			kb.logger.Error().
				Err(topic.Err).
				Str("topic", topic.Topic).
				Msg("Failed to create topic")
		} else {
			kb.logger.Info().
				Str("topic", topic.Topic).
				Msg("Created Kafka topic")
		}
	}
}

// isTopicAlreadyExistsError checks if the error indicates the topic already exists.
func isTopicAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, kerr.TopicAlreadyExists)
}

// SplitBrokers splits a comma-separated broker string into individual addresses.
func SplitBrokers(brokers string) []string {
	var result []string
	for b := range strings.SplitSeq(brokers, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// buildKgoOpts builds common kgo client options for brokers, SASL, and TLS.
func buildKgoOpts(brokers []string, sasl *kafka.SASLConfig, tlsCfg *kafka.TLSConfig) ([]kgo.Opt, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
	}

	// Add SASL authentication if configured
	if sasl != nil {
		mechanism := scram.Auth{
			User: sasl.Username,
			Pass: sasl.Password,
		}
		switch sasl.Mechanism {
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(mechanism.AsSha256Mechanism()))
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(mechanism.AsSha512Mechanism()))
		default:
			return nil, fmt.Errorf("unsupported SASL mechanism: %s (use scram-sha-256 or scram-sha-512)", sasl.Mechanism)
		}
	}

	// Add TLS encryption if configured
	if tlsCfg != nil && tlsCfg.Enabled {
		tc := &tls.Config{
			InsecureSkipVerify: tlsCfg.InsecureSkipVerify, //nolint:gosec // Controlled by configuration for dev/testing environments
			MinVersion:         tls.VersionTLS12,
		}
		if tlsCfg.CAPath != "" {
			caCert, err := os.ReadFile(tlsCfg.CAPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate from %s: %w", tlsCfg.CAPath, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA certificate from %s", tlsCfg.CAPath)
			}
			tc.RootCAs = caCertPool
		}
		opts = append(opts, kgo.DialTLSConfig(tc))
	}

	return opts, nil
}

// Compile-time interface check.
var _ backend.MessageBackend = (*KafkaBackend)(nil)
