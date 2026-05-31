package orchestration

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/server/broadcast"
	"github.com/klurvio/sukko/internal/server/kafka"
	kafkashared "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/logging"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
	"github.com/klurvio/sukko/internal/shared/types"
)

// MsgTopicAutoRemoved is logged when handleBrokerDeletedTopic removes a topic from the pool.
const MsgTopicAutoRemoved = "topic removed from pool — deleted at broker"

// consumerEntry is used to stop consumers outside of mu (Constitution VII: no I/O while holding a mutex).
type consumerEntry struct {
	tenantID string
	consumer *kafka.Consumer
}

// MultiTenantConsumerPool manages Kafka consumers for multi-tenant deployments.
// It supports two consumer modes:
//
//   - Shared: All shared-mode tenants use a single consumer group ({env}-shared-consumer).
//     Topics are discovered via TenantRegistry and hot-added without rebalance.
//
//   - Dedicated: Each dedicated-mode tenant gets its own consumer group
//     ({env}-{tenant_id}-consumer) for complete isolation.
//
// Topic Discovery:
//
//	Instead of regex subscriptions (which cause O(m) memory, O(m×p) CPU, and rebalance storms),
//	we use explicit topic lists with periodic refresh from the provisioning database.
//	New topics are added via AddConsumeTopics() which avoids rebalance.
//
// Architecture:
//
//	MultiTenantConsumerPool
//	├── SharedConsumer (explicit topics, 60-second refresh)
//	│   └── Consumer Group: {env}-shared-consumer
//	│   └── Topics: queried from TenantRegistry
//	│
//	└── DedicatedConsumers (per-tenant isolation)
//	    └── Consumer Group: {env}-{tenant_id}-consumer
//	    └── Topics: from provisioning DB for that tenant
//
// Thread Safety: All public methods are safe for concurrent use.
type MultiTenantConsumerPool struct {
	config       MultiTenantPoolConfig
	registry     types.TenantRegistry
	broadcastBus broadcast.Bus
	logger       zerolog.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.RWMutex
	metrics      pkgmetrics.PoolMetrics

	// topicMu protects sharedTopics/blockedSharedTopics/dedicatedTopics. Lock ordering: mu THEN topicMu (never reversed).
	topicMu sync.Mutex

	// Shared consumer for all shared-mode tenants
	sharedConsumer      *kafka.Consumer
	sharedTopics        map[string]bool     // Currently subscribed topics (protected by topicMu)
	blockedSharedTopics map[string]struct{} // Broker-deleted topics suppressed from re-subscription (protected by topicMu)

	// Dedicated consumers per tenant (tenant_id -> consumer)
	dedicatedConsumers map[string]*kafka.Consumer
	dedicatedTopics    map[string]string // Reverse index: topic -> tenantID (protected by topicMu)

	// consumerFactory creates consumers; replaced in tests to avoid real broker connections.
	consumerFactory func(kafka.ConsumerConfig) (*kafka.Consumer, error)

	// Refresh management
	refreshInterval time.Duration
	refreshCh       chan struct{} // Signal channel for on-demand refresh
	lastRefresh     time.Time

	// Internal metrics (also exposed via Prometheus if callback is set)
	messagesRouted   atomic.Uint64
	messagesDropped  atomic.Uint64
	topicsSubscribed atomic.Uint64
	dedicatedCount   atomic.Uint64
	refreshCount     atomic.Uint64
	refreshErrors    atomic.Uint64
}

// MultiTenantPoolConfig configures the multi-tenant consumer pool.
type MultiTenantPoolConfig struct {
	// Brokers is the list of Kafka/Redpanda broker addresses (required)
	Brokers []string

	// Namespace is the topic namespace (e.g., "prod", "dev")
	// Used for topic prefixes
	Namespace string

	// Environment is the deployment environment (e.g., "dev", "stg", "prod").
	// Used for consumer group naming: {env}-shared-consumer, {env}-{tenant}-consumer.
	// If empty, falls back to Namespace for backward compatibility.
	Environment string

	// Registry provides tenant topic information from the provisioning database
	Registry types.TenantRegistry

	// BroadcastBus receives consumed messages for distribution to WebSocket clients
	BroadcastBus broadcast.Bus

	// ResourceGuard provides rate limiting and CPU-based backpressure
	ResourceGuard kafka.ResourceGuard

	// Logger for structured logging
	Logger zerolog.Logger

	// RefreshInterval controls how often to check for new tenant topics
	// Default: 60 seconds
	RefreshInterval time.Duration

	// Consumer batch tuning
	KafkaBatchSize    int
	KafkaBatchTimeout time.Duration

	// Consumer transport tuning
	KafkaFetchMaxWait              time.Duration
	KafkaFetchMinBytes             int32
	KafkaFetchMaxBytes             int32
	KafkaSessionTimeout            time.Duration
	KafkaRebalanceTimeout          time.Duration
	KafkaReplayFetchMaxBytes       int32
	KafkaBackpressureCheckInterval time.Duration

	// Partition-revoke commit tuning
	KafkaCommitOnRevokeTimeout time.Duration // max time for CommitMarkedOffsets in revoke callback
	KafkaAutoCommitInterval    time.Duration // background auto-commit interval (0 = franz-go default)

	// Security configuration for managed Kafka/Redpanda services
	SASL *kafka.SASLConfig
	TLS  *kafka.TLSConfig

	// Metrics is an optional callback for reporting pool metrics to Prometheus.
	// If nil, metrics are still tracked internally via GetMetrics().
	Metrics pkgmetrics.PoolMetrics
}

// NewMultiTenantConsumerPool creates a new multi-tenant consumer pool.
// The pool starts without any topics; call Start() to begin topic discovery and consumption.
func NewMultiTenantConsumerPool(config MultiTenantPoolConfig) (*MultiTenantConsumerPool, error) {
	if len(config.Brokers) == 0 {
		return nil, errors.New("at least one broker is required")
	}
	if config.Namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if config.Registry == nil {
		return nil, errors.New("tenant registry is required")
	}
	if config.BroadcastBus == nil {
		return nil, errors.New("broadcast bus is required")
	}
	if config.ResourceGuard == nil {
		return nil, errors.New("resource guard is required")
	}

	// Environment fallback: use Namespace if Environment not set
	if config.Environment == "" {
		config.Environment = config.Namespace
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel stored in pool.cancelFn and called during Stop

	pool := &MultiTenantConsumerPool{
		config:              config,
		registry:            config.Registry,
		broadcastBus:        config.BroadcastBus,
		logger:              config.Logger.With().Str("component", "multitenant-pool").Logger(),
		ctx:                 ctx,
		cancel:              cancel,
		metrics:             config.Metrics,
		sharedTopics:        make(map[string]bool),
		blockedSharedTopics: make(map[string]struct{}),
		dedicatedConsumers:  make(map[string]*kafka.Consumer),
		dedicatedTopics:     make(map[string]string),
		consumerFactory:     kafka.NewConsumer,
		refreshInterval:     config.RefreshInterval,
		refreshCh:           make(chan struct{}, 1), // Buffered to avoid blocking senders
	}

	pool.logger.Info().
		Str("namespace", config.Namespace).
		Dur("refresh_interval", config.RefreshInterval).
		Msg("Multi-tenant consumer pool initialized")

	return pool, nil
}

// Start performs initial topic discovery and begins the periodic refresh loop.
func (p *MultiTenantConsumerPool) Start() error {
	p.logger.Info().Msg("Starting multi-tenant consumer pool")

	// Do NOT call consumer.Start() here — updateSharedConsumer/updateDedicatedConsumers do it; calling twice double-starts the goroutine.
	if err := p.refreshTopics(p.ctx); err != nil {
		return fmt.Errorf("initial topic discovery failed: %w", err)
	}

	// Snapshot under locks — consumer poll goroutines run concurrently and mutate sharedTopics.
	sharedCount := func() int {
		p.topicMu.Lock()
		defer p.topicMu.Unlock()
		return len(p.sharedTopics)
	}()
	dedicatedCount := func() int {
		p.mu.RLock()
		defer p.mu.RUnlock()
		return len(p.dedicatedConsumers)
	}()

	// Start refresh loop
	p.wg.Go(p.refreshLoop)

	p.logger.Info().
		Int("shared_topics", sharedCount).
		Int("dedicated_consumers", dedicatedCount).
		Msg("Multi-tenant consumer pool started")

	return nil
}

// refreshLoop periodically checks for new tenant topics.
// Also listens for on-demand refresh signals from RefreshTopics().
func (p *MultiTenantConsumerPool) refreshLoop() {
	defer logging.RecoverPanic(p.logger, "refreshLoop", nil)

	ticker := time.NewTicker(p.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.refreshTopics(p.ctx); err != nil {
				p.refreshErrors.Add(1)
				p.logger.Error().
					Err(err).
					Msg("Topic refresh failed (periodic)")
			}
		case <-p.refreshCh:
			if err := p.refreshTopics(p.ctx); err != nil {
				p.refreshErrors.Add(1)
				p.logger.Error().
					Err(err).
					Msg("Topic refresh failed (on-demand)")
			}
		}
	}
}

// RefreshTopics triggers an on-demand topic refresh.
// Non-blocking: if a refresh is already pending, this is a no-op.
func (p *MultiTenantConsumerPool) RefreshTopics() {
	select {
	case p.refreshCh <- struct{}{}:
		p.logger.Debug().Msg("On-demand topic refresh triggered")
	default:
		// Refresh already pending, skip
	}
}

// refreshTopics queries the registry for current tenant topics and updates consumers.
func (p *MultiTenantConsumerPool) refreshTopics(ctx context.Context) error {
	p.refreshCount.Add(1)

	// Get shared tenant topics
	sharedTopics, err := p.registry.GetSharedTenantTopics(ctx, p.config.Namespace)
	if err != nil {
		if p.metrics != nil {
			p.metrics.OnRefresh(false, 0, 0)
		}
		return fmt.Errorf("failed to get shared tenant topics: %w", err)
	}

	// Get dedicated tenants
	dedicatedTenants, err := p.registry.GetDedicatedTenants(ctx, p.config.Namespace)
	if err != nil {
		if p.metrics != nil {
			p.metrics.OnRefresh(false, 0, 0)
		}
		return fmt.Errorf("failed to get dedicated tenants: %w", err)
	}

	p.mu.Lock()

	p.lastRefresh = time.Now()

	if err := p.updateSharedConsumer(ctx, sharedTopics); err != nil {
		p.mu.Unlock()
		if p.metrics != nil {
			p.metrics.OnRefresh(false, 0, 0)
		}
		return fmt.Errorf("failed to update shared consumer: %w", err)
	}

	var toStop []consumerEntry
	if err := p.updateDedicatedConsumers(ctx, dedicatedTenants, &toStop); err != nil {
		p.mu.Unlock()
		if p.metrics != nil {
			p.metrics.OnRefresh(false, 0, 0)
		}
		return fmt.Errorf("failed to update dedicated consumers: %w", err)
	}

	topicsCount := func() int {
		p.topicMu.Lock()
		defer p.topicMu.Unlock()
		return len(p.sharedTopics)
	}()
	dedicatedCount := len(p.dedicatedConsumers)

	p.mu.Unlock()

	// Stop deprovisioned consumers outside mu (Constitution VII: no I/O while holding a mutex).
	for _, entry := range toStop {
		if err := entry.consumer.Stop(); err != nil {
			p.logger.Error().
				Err(err).
				Str(kafka.LogFieldTenantID, entry.tenantID).
				Msg("Error stopping dedicated consumer")
		}
	}

	p.topicsSubscribed.Store(uint64(topicsCount)) //nolint:gosec // len() is always non-negative
	p.dedicatedCount.Store(uint64(dedicatedCount))

	if p.metrics != nil {
		p.metrics.OnRefresh(true, topicsCount, dedicatedCount)
	}

	return nil
}

// updateSharedConsumer creates or updates the shared consumer for shared-mode tenants.
func (p *MultiTenantConsumerPool) updateSharedConsumer(_ context.Context, topics []string) error {
	// Build topic set from registry.
	newTopics := make(map[string]bool, len(topics))
	for _, topic := range topics {
		newTopics[topic] = true
	}

	// Single lock acquisition: eliminates TOCTOU window with concurrent handleBrokerDeletedTopic.
	var currentTopics map[string]bool
	func() {
		p.topicMu.Lock()
		defer p.topicMu.Unlock()
		maps.DeleteFunc(p.blockedSharedTopics, func(t string, _ struct{}) bool { return !newTopics[t] })
		for t := range p.blockedSharedTopics {
			delete(newTopics, t)
		}
		currentTopics = maps.Clone(p.sharedTopics)
	}()

	// Find new topics to add
	var toAdd []string
	for topic := range newTopics {
		if !currentTopics[topic] {
			toAdd = append(toAdd, topic)
		}
	}

	// Find topics to remove (tenant deprovisioned)
	var toRemove []string
	for topic := range currentTopics {
		if !newTopics[topic] {
			toRemove = append(toRemove, topic)
		}
	}

	if p.sharedConsumer == nil && len(newTopics) == 0 && len(topics) > 0 {
		p.logger.Info().Int("blocked_topics", len(topics)).Msg("all shared topics broker-deleted; awaiting deprovisioning")
		return nil
	}

	// Create shared consumer if this is the first time (using filtered newTopics).
	if p.sharedConsumer == nil && len(newTopics) > 0 {
		filteredTopics := make([]string, 0, len(newTopics))
		for t := range newTopics {
			filteredTopics = append(filteredTopics, t)
		}
		consumer, err := p.consumerFactory(kafka.ConsumerConfig{
			Brokers:       p.config.Brokers,
			ConsumerGroup: p.config.Environment + "-shared-consumer",
			Topics:        filteredTopics,
			Logger:        &p.logger,
			Broadcast:     p.routeMessage,
			ResourceGuard: p.config.ResourceGuard,
			SASL:          p.config.SASL,
			TLS:           p.config.TLS,
			BatchSize:     p.config.KafkaBatchSize,
			BatchTimeout:  p.config.KafkaBatchTimeout,
			// Transport tuning
			FetchMaxWait:              p.config.KafkaFetchMaxWait,
			FetchMinBytes:             p.config.KafkaFetchMinBytes,
			FetchMaxBytes:             p.config.KafkaFetchMaxBytes,
			SessionTimeout:            p.config.KafkaSessionTimeout,
			RebalanceTimeout:          p.config.KafkaRebalanceTimeout,
			ReplayFetchMaxBytes:       p.config.KafkaReplayFetchMaxBytes,
			BackpressureCheckInterval: p.config.KafkaBackpressureCheckInterval,
			// Partition-revoke commit tuning
			CommitOnRevokeTimeout: p.config.KafkaCommitOnRevokeTimeout,
			AutoCommitInterval:    p.config.KafkaAutoCommitInterval,
			ConsumerType:          kafka.ConsumerTypeKindShared,
			OnUnknownTopic: func(topic string) {
				p.handleBrokerDeletedTopic(topic, kafka.ConsumerTypeKindShared)
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create shared consumer: %w", err)
		}

		if err := consumer.Start(); err != nil {
			return fmt.Errorf("failed to start shared consumer: %w", err)
		}

		// Assign only after successful start to prevent a zombie consumer on Start() failure.
		p.sharedConsumer = consumer
		func() {
			p.topicMu.Lock()
			defer p.topicMu.Unlock()
			p.sharedTopics = newTopics
		}()

		p.logger.Info().
			Strs("topics", filteredTopics).
			Msg("Created shared consumer with initial topics")
		return nil
	}

	// Add new topics incrementally (no rebalance)
	if len(toAdd) > 0 {
		p.logger.Info().
			Strs("topics", toAdd).
			Msg("Adding new topics to shared consumer")

		// AddConsumeTopics (KIP-429) hot-adds without triggering a rebalance.
		p.sharedConsumer.AddConsumeTopics(toAdd...)

		func() {
			p.topicMu.Lock()
			defer p.topicMu.Unlock()
			for _, topic := range toAdd {
				if _, blocked := p.blockedSharedTopics[topic]; !blocked {
					p.sharedTopics[topic] = true
				}
			}
		}()
	}

	if len(toRemove) > 0 {
		p.logger.Info().
			Strs("topics", toRemove).
			Msg("Topics removed from shared consumer (tenant deprovisioned)")
		if p.sharedConsumer != nil {
			p.sharedConsumer.PauseFetchTopics(toRemove...)
		}
		func() {
			p.topicMu.Lock()
			defer p.topicMu.Unlock()
			for _, topic := range toRemove {
				delete(p.sharedTopics, topic)
			}
		}()
	}

	return nil
}

// updateDedicatedConsumers creates or removes dedicated consumers.
// Deprovisioned consumers are appended to toStop; callers must Stop() them outside mu.
//
//nolint:unparam // error return is for interface consistency and future error handling
func (p *MultiTenantConsumerPool) updateDedicatedConsumers(_ context.Context, tenants []types.TenantTopics, toStop *[]consumerEntry) error {
	// Build tenant set
	activeTenants := make(map[string]bool, len(tenants))
	for _, t := range tenants {
		activeTenants[t.TenantID] = true
	}

	// Collect consumers for deprovisioned tenants; stop them outside mu (caller's responsibility).
	for tenantID, consumer := range p.dedicatedConsumers {
		if activeTenants[tenantID] {
			continue
		}
		p.logger.Info().
			Str(kafka.LogFieldTenantID, tenantID).
			Msg("Scheduling stop of dedicated consumer for deprovisioned tenant")
		*toStop = append(*toStop, consumerEntry{tenantID: tenantID, consumer: consumer})
		delete(p.dedicatedConsumers, tenantID)
		func() {
			p.topicMu.Lock()
			defer p.topicMu.Unlock()
			maps.DeleteFunc(p.dedicatedTopics, func(_, v string) bool { return v == tenantID })
		}()
	}

	// Create consumers for new dedicated tenants
	for _, tenant := range tenants {
		if _, exists := p.dedicatedConsumers[tenant.TenantID]; exists {
			continue // Already have a consumer for this tenant
		}

		if len(tenant.Topics) == 0 {
			continue // No topics for this tenant
		}

		tenantID := tenant.TenantID
		consumer, err := p.consumerFactory(kafka.ConsumerConfig{
			Brokers:       p.config.Brokers,
			ConsumerGroup: fmt.Sprintf("%s-%s-consumer", p.config.Environment, tenant.TenantID),
			Topics:        tenant.Topics,
			Logger:        &p.logger,
			Broadcast:     p.routeMessage,
			ResourceGuard: p.config.ResourceGuard,
			SASL:          p.config.SASL,
			TLS:           p.config.TLS,
			BatchSize:     p.config.KafkaBatchSize,
			BatchTimeout:  p.config.KafkaBatchTimeout,
			// Transport tuning
			FetchMaxWait:              p.config.KafkaFetchMaxWait,
			FetchMinBytes:             p.config.KafkaFetchMinBytes,
			FetchMaxBytes:             p.config.KafkaFetchMaxBytes,
			SessionTimeout:            p.config.KafkaSessionTimeout,
			RebalanceTimeout:          p.config.KafkaRebalanceTimeout,
			ReplayFetchMaxBytes:       p.config.KafkaReplayFetchMaxBytes,
			BackpressureCheckInterval: p.config.KafkaBackpressureCheckInterval,
			// Partition-revoke commit tuning
			CommitOnRevokeTimeout: p.config.KafkaCommitOnRevokeTimeout,
			AutoCommitInterval:    p.config.KafkaAutoCommitInterval,
			ConsumerType:          kafka.ConsumerTypeKindDedicated,
			OnUnknownTopic: func(topic string) {
				p.handleBrokerDeletedTopic(topic, kafka.ConsumerTypeKindDedicated)
			},
		})
		if err != nil {
			p.logger.Error().
				Err(err).
				Str(kafka.LogFieldTenantID, tenant.TenantID).
				Msg("Failed to create dedicated consumer")
			continue
		}

		// Start the consumer immediately
		if err := consumer.Start(); err != nil {
			p.logger.Error().
				Err(err).
				Str(kafka.LogFieldTenantID, tenant.TenantID).
				Msg("Failed to start dedicated consumer")
			continue
		}

		p.dedicatedConsumers[tenantID] = consumer

		func() {
			p.topicMu.Lock()
			defer p.topicMu.Unlock()
			for _, t := range tenant.Topics {
				p.dedicatedTopics[t] = tenantID
			}
		}()

		p.logger.Info().
			Str(kafka.LogFieldTenantID, tenant.TenantID).
			Strs("topics", tenant.Topics).
			Msg("Created dedicated consumer for tenant")
	}

	return nil
}

// routeMessage is called by consumers for each message.
// It publishes the message to the BroadcastBus for distribution to WebSocket clients.
// topicName, partition, offset are used by the history writer to populate stream metadata.
func (p *MultiTenantConsumerPool) routeMessage(subject string, message []byte, topicName string, partition int32, offset int64) {
	p.messagesRouted.Add(1)

	// Report to Prometheus via callback
	if p.metrics != nil {
		p.metrics.OnMessageRouted()
	}

	tenantID, _, err := kafkashared.ExtractTenantID(topicName, p.config.Namespace)
	if err != nil {
		p.logger.Warn().Err(err).Str("topic", topicName).Msg("routeMessage: could not extract tenant/channel from topic name")
	}
	bareChannel := strings.TrimPrefix(subject, tenantID+".")
	streamID := fmt.Sprintf("%d-%d", partition, offset)

	p.broadcastBus.Publish(&broadcast.Message{
		Subject:  subject,
		Payload:  message,
		TenantID: tenantID,
		StreamID: streamID,
		Channel:  bareChannel,
	})

	// Log periodic metrics (sample every Nth message to avoid log spam)
	const logRoutingMetricsInterval = 1000
	if routed := p.messagesRouted.Load(); routed%logRoutingMetricsInterval == 0 {
		p.logger.Debug().
			Uint64("routed", routed).
			Uint64("dropped", p.messagesDropped.Load()).
			Msg("Multi-tenant pool routing metrics")
	}
}

// handleBrokerDeletedTopic removes a broker-deleted topic from the pool and blocks re-subscription until deprovisioned.
func (p *MultiTenantConsumerPool) handleBrokerDeletedTopic(topic, consumerType string) {
	func() {
		p.topicMu.Lock()
		defer p.topicMu.Unlock()
		if _, ok := p.sharedTopics[topic]; ok {
			delete(p.sharedTopics, topic)
			p.blockedSharedTopics[topic] = struct{}{}
		} else {
			delete(p.dedicatedTopics, topic)
		}
	}()
	p.logger.Info().
		Str(kafka.LabelTopic, topic).
		Str(kafka.LabelConsumerType, consumerType).
		Msg(MsgTopicAutoRemoved)
}

// Stop gracefully shuts down the consumer pool.
func (p *MultiTenantConsumerPool) Stop() error {
	p.logger.Info().Msg("Stopping multi-tenant consumer pool")

	// Cancel context to stop refresh loop
	p.cancel()

	// Wait for refresh loop to stop
	p.wg.Wait()

	// Snapshot under mu (fast map reads only); refreshLoop is dead after wg.Wait(), so no concurrent mutations.
	var toStop []consumerEntry
	func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.sharedConsumer != nil {
			toStop = append(toStop, consumerEntry{tenantID: kafka.ConsumerTypeKindShared, consumer: p.sharedConsumer})
		}
		for tenantID, c := range p.dedicatedConsumers {
			toStop = append(toStop, consumerEntry{tenantID: tenantID, consumer: c})
		}
	}()

	var errs []error
	for _, entry := range toStop {
		if err := entry.consumer.Stop(); err != nil {
			p.logger.Error().
				Err(err).
				Str(kafka.LogFieldTenantID, entry.tenantID).
				Msg("Error stopping consumer")
			errs = append(errs, fmt.Errorf("stop %s: %w", entry.tenantID, err))
		}
	}
	p.logger.Info().
		Uint64("total_routed", p.messagesRouted.Load()).
		Uint64("total_dropped", p.messagesDropped.Load()).
		Uint64("refresh_count", p.refreshCount.Load()).
		Uint64("refresh_errors", p.refreshErrors.Load()).
		Msg("Multi-tenant consumer pool stopped")

	return errors.Join(errs...)
}

// GetMetrics returns current pool metrics.
func (p *MultiTenantConsumerPool) GetMetrics() MultiTenantPoolMetrics {
	lastRefresh := func() time.Time {
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.lastRefresh
	}()

	return MultiTenantPoolMetrics{
		MessagesRouted:   p.messagesRouted.Load(),
		MessagesDropped:  p.messagesDropped.Load(),
		TopicsSubscribed: p.topicsSubscribed.Load(),
		DedicatedCount:   p.dedicatedCount.Load(),
		RefreshCount:     p.refreshCount.Load(),
		RefreshErrors:    p.refreshErrors.Load(),
		LastRefresh:      lastRefresh,
	}
}

// GetSharedConsumer returns the shared consumer for replay operations.
// Returns nil if the pool hasn't been started or if there are no shared topics.
func (p *MultiTenantConsumerPool) GetSharedConsumer() *kafka.Consumer {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sharedConsumer
}

// MultiTenantPoolMetrics contains metrics for the consumer pool.
type MultiTenantPoolMetrics struct {
	MessagesRouted   uint64
	MessagesDropped  uint64
	TopicsSubscribed uint64
	DedicatedCount   uint64
	RefreshCount     uint64
	RefreshErrors    uint64
	LastRefresh      time.Time
}
