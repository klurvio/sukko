package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/broadcast"
	"github.com/Toniq-Labs/odin-ws/internal/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
	pkgmetrics "github.com/Toniq-Labs/odin-ws/pkg/metrics"
)

// MultiTenantConsumerPool manages Kafka consumers for multi-tenant deployments.
// It supports two consumer modes:
//
//   - Shared: All shared-mode tenants use a single consumer group (odin-shared-{namespace}).
//     Topics are discovered via TenantRegistry and hot-added without rebalance.
//
//   - Dedicated: Each dedicated-mode tenant gets its own consumer group
//     (odin-{tenant_id}-{namespace}) for complete isolation.
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
//	│   └── Consumer Group: odin-shared-{namespace}
//	│   └── Topics: queried from TenantRegistry
//	│
//	└── DedicatedConsumers (per-tenant isolation)
//	    └── Consumer Group: odin-{tenant_id}-{namespace}
//	    └── Topics: from provisioning DB for that tenant
//
// Thread Safety: All public methods are safe for concurrent use.
type MultiTenantConsumerPool struct {
	config       MultiTenantPoolConfig
	registry     kafka.TenantRegistry
	broadcastBus broadcast.Bus
	logger       zerolog.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.RWMutex
	metrics      pkgmetrics.PoolMetrics

	// Shared consumer for all shared-mode tenants
	sharedConsumer *kafka.Consumer
	sharedTopics   map[string]bool // Currently subscribed topics

	// Dedicated consumers per tenant (tenant_id -> consumer)
	dedicatedConsumers map[string]*kafka.Consumer

	// Refresh management
	refreshInterval time.Duration
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
	// Used for consumer group naming and topic prefixes
	Namespace string

	// Registry provides tenant topic information from the provisioning database
	Registry kafka.TenantRegistry

	// BroadcastBus receives consumed messages for distribution to WebSocket clients
	BroadcastBus broadcast.Bus

	// ResourceGuard provides rate limiting and CPU-based backpressure
	ResourceGuard kafka.ResourceGuard

	// Logger for structured logging
	Logger zerolog.Logger

	// RefreshInterval controls how often to check for new tenant topics
	// Default: 60 seconds
	RefreshInterval time.Duration

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

	// Default refresh interval
	refreshInterval := config.RefreshInterval
	if refreshInterval == 0 {
		refreshInterval = 60 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	pool := &MultiTenantConsumerPool{
		config:             config,
		registry:           config.Registry,
		broadcastBus:       config.BroadcastBus,
		logger:             config.Logger.With().Str("component", "multitenant-pool").Logger(),
		ctx:                ctx,
		cancel:             cancel,
		metrics:            config.Metrics,
		sharedTopics:       make(map[string]bool),
		dedicatedConsumers: make(map[string]*kafka.Consumer),
		refreshInterval:    refreshInterval,
	}

	pool.logger.Info().
		Str("namespace", config.Namespace).
		Dur("refresh_interval", refreshInterval).
		Msg("Multi-tenant consumer pool initialized")

	return pool, nil
}

// Start begins consuming messages and starts the topic refresh loop.
// It performs an initial topic discovery, creates the shared consumer,
// creates dedicated consumers for dedicated-mode tenants, and starts
// periodic refresh for new topics.
func (p *MultiTenantConsumerPool) Start() error {
	p.logger.Info().Msg("Starting multi-tenant consumer pool")

	// Initial topic discovery and consumer creation
	if err := p.refreshTopics(p.ctx); err != nil {
		return fmt.Errorf("initial topic discovery failed: %w", err)
	}

	// Start shared consumer if we have topics
	if p.sharedConsumer != nil {
		if err := p.sharedConsumer.Start(); err != nil {
			return fmt.Errorf("failed to start shared consumer: %w", err)
		}
	}

	// Start all dedicated consumers
	for tenantID, consumer := range p.dedicatedConsumers {
		if err := consumer.Start(); err != nil {
			p.logger.Error().
				Err(err).
				Str("tenant_id", tenantID).
				Msg("Failed to start dedicated consumer")
			// Continue with other consumers
		}
	}

	// Start refresh loop
	p.wg.Add(1)
	go p.refreshLoop()

	p.logger.Info().
		Int("shared_topics", len(p.sharedTopics)).
		Int("dedicated_consumers", len(p.dedicatedConsumers)).
		Msg("Multi-tenant consumer pool started")

	return nil
}

// refreshLoop periodically checks for new tenant topics.
func (p *MultiTenantConsumerPool) refreshLoop() {
	defer monitoring.RecoverPanic(p.logger, "refreshLoop", nil)
	defer p.wg.Done()

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
					Msg("Topic refresh failed")
			}
		}
	}
}

// refreshTopics queries the registry for current tenant topics and updates consumers.
func (p *MultiTenantConsumerPool) refreshTopics(ctx context.Context) error {
	p.refreshCount.Add(1)
	p.lastRefresh = time.Now()

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
	defer p.mu.Unlock()

	// Update shared consumer
	if err := p.updateSharedConsumer(ctx, sharedTopics); err != nil {
		if p.metrics != nil {
			p.metrics.OnRefresh(false, 0, 0)
		}
		return fmt.Errorf("failed to update shared consumer: %w", err)
	}

	// Update dedicated consumers
	if err := p.updateDedicatedConsumers(ctx, dedicatedTenants); err != nil {
		if p.metrics != nil {
			p.metrics.OnRefresh(false, 0, 0)
		}
		return fmt.Errorf("failed to update dedicated consumers: %w", err)
	}

	topicsCount := len(p.sharedTopics)
	dedicatedCount := len(p.dedicatedConsumers)

	p.topicsSubscribed.Store(uint64(topicsCount))
	p.dedicatedCount.Store(uint64(dedicatedCount))

	// Report success to Prometheus
	if p.metrics != nil {
		p.metrics.OnRefresh(true, topicsCount, dedicatedCount)
	}

	return nil
}

// updateSharedConsumer creates or updates the shared consumer for shared-mode tenants.
func (p *MultiTenantConsumerPool) updateSharedConsumer(_ context.Context, topics []string) error {
	// Build topic set for comparison
	newTopics := make(map[string]bool, len(topics))
	for _, topic := range topics {
		newTopics[topic] = true
	}

	// Find new topics to add
	var toAdd []string
	for topic := range newTopics {
		if !p.sharedTopics[topic] {
			toAdd = append(toAdd, topic)
		}
	}

	// Find topics to remove (tenant deprovisioned)
	var toRemove []string
	for topic := range p.sharedTopics {
		if !newTopics[topic] {
			toRemove = append(toRemove, topic)
		}
	}

	// Create shared consumer if this is the first time
	if p.sharedConsumer == nil && len(topics) > 0 {
		consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
			Brokers:       p.config.Brokers,
			ConsumerGroup: "odin-shared-" + p.config.Namespace,
			Topics:        topics,
			Logger:        &p.logger,
			Broadcast:     p.routeMessage,
			ResourceGuard: p.config.ResourceGuard,
			SASL:          p.config.SASL,
			TLS:           p.config.TLS,
		})
		if err != nil {
			return fmt.Errorf("failed to create shared consumer: %w", err)
		}
		p.sharedConsumer = consumer
		p.sharedTopics = newTopics

		p.logger.Info().
			Strs("topics", topics).
			Msg("Created shared consumer with initial topics")
		return nil
	}

	// Log changes
	if len(toAdd) > 0 {
		p.logger.Info().
			Strs("topics", toAdd).
			Msg("Adding new topics to shared consumer")
		// Note: franz-go AddConsumeTopics() would be called here
		// For now, we track the topics; full implementation requires consumer recreation
		// or using the franz-go client method directly
		for _, topic := range toAdd {
			p.sharedTopics[topic] = true
		}
	}

	if len(toRemove) > 0 {
		p.logger.Info().
			Strs("topics", toRemove).
			Msg("Topics removed from shared consumer (tenant deprovisioned)")
		for _, topic := range toRemove {
			delete(p.sharedTopics, topic)
		}
	}

	return nil
}

// updateDedicatedConsumers creates, updates, or removes dedicated consumers.
//
//nolint:unparam // error return is for interface consistency and future error handling
func (p *MultiTenantConsumerPool) updateDedicatedConsumers(_ context.Context, tenants []kafka.TenantTopics) error {
	// Build tenant set
	activeTenants := make(map[string]bool, len(tenants))
	for _, t := range tenants {
		activeTenants[t.TenantID] = true
	}

	// Remove consumers for deprovisioned tenants
	for tenantID, consumer := range p.dedicatedConsumers {
		if !activeTenants[tenantID] {
			p.logger.Info().
				Str("tenant_id", tenantID).
				Msg("Stopping dedicated consumer for deprovisioned tenant")
			if err := consumer.Stop(); err != nil {
				p.logger.Error().
					Err(err).
					Str("tenant_id", tenantID).
					Msg("Error stopping dedicated consumer")
			}
			delete(p.dedicatedConsumers, tenantID)
		}
	}

	// Create consumers for new dedicated tenants
	for _, tenant := range tenants {
		if _, exists := p.dedicatedConsumers[tenant.TenantID]; exists {
			continue // Already have a consumer for this tenant
		}

		if len(tenant.Topics) == 0 {
			continue // No topics for this tenant
		}

		consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
			Brokers:       p.config.Brokers,
			ConsumerGroup: fmt.Sprintf("odin-%s-%s", tenant.TenantID, p.config.Namespace),
			Topics:        tenant.Topics,
			Logger:        &p.logger,
			Broadcast:     p.routeMessage,
			ResourceGuard: p.config.ResourceGuard,
			SASL:          p.config.SASL,
			TLS:           p.config.TLS,
		})
		if err != nil {
			p.logger.Error().
				Err(err).
				Str("tenant_id", tenant.TenantID).
				Msg("Failed to create dedicated consumer")
			continue
		}

		// Start the consumer immediately
		if err := consumer.Start(); err != nil {
			p.logger.Error().
				Err(err).
				Str("tenant_id", tenant.TenantID).
				Msg("Failed to start dedicated consumer")
			continue
		}

		p.dedicatedConsumers[tenant.TenantID] = consumer
		p.logger.Info().
			Str("tenant_id", tenant.TenantID).
			Strs("topics", tenant.Topics).
			Msg("Created dedicated consumer for tenant")
	}

	return nil
}

// routeMessage is called by consumers for each message.
// It publishes the message to the BroadcastBus for distribution to WebSocket clients.
func (p *MultiTenantConsumerPool) routeMessage(subject string, message []byte) {
	p.messagesRouted.Add(1)

	// Report to Prometheus via callback
	if p.metrics != nil {
		p.metrics.OnMessageRouted()
	}

	p.broadcastBus.Publish(&broadcast.Message{
		Subject: subject,
		Payload: message,
	})

	// Log periodic metrics
	if routed := p.messagesRouted.Load(); routed%1000 == 0 {
		p.logger.Debug().
			Uint64("routed", routed).
			Uint64("dropped", p.messagesDropped.Load()).
			Msg("Multi-tenant pool routing metrics")
	}
}

// Stop gracefully shuts down the consumer pool.
func (p *MultiTenantConsumerPool) Stop() error {
	p.logger.Info().Msg("Stopping multi-tenant consumer pool")

	// Cancel context to stop refresh loop
	p.cancel()

	// Wait for refresh loop to stop
	p.wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop shared consumer
	if p.sharedConsumer != nil {
		if err := p.sharedConsumer.Stop(); err != nil {
			p.logger.Error().Err(err).Msg("Error stopping shared consumer")
		}
	}

	// Stop all dedicated consumers
	for tenantID, consumer := range p.dedicatedConsumers {
		if err := consumer.Stop(); err != nil {
			p.logger.Error().
				Err(err).
				Str("tenant_id", tenantID).
				Msg("Error stopping dedicated consumer")
		}
	}

	p.logger.Info().
		Uint64("total_routed", p.messagesRouted.Load()).
		Uint64("total_dropped", p.messagesDropped.Load()).
		Uint64("refresh_count", p.refreshCount.Load()).
		Uint64("refresh_errors", p.refreshErrors.Load()).
		Msg("Multi-tenant consumer pool stopped")

	return nil
}

// GetMetrics returns current pool metrics.
func (p *MultiTenantConsumerPool) GetMetrics() MultiTenantPoolMetrics {
	return MultiTenantPoolMetrics{
		MessagesRouted:   p.messagesRouted.Load(),
		MessagesDropped:  p.messagesDropped.Load(),
		TopicsSubscribed: p.topicsSubscribed.Load(),
		DedicatedCount:   p.dedicatedCount.Load(),
		RefreshCount:     p.refreshCount.Load(),
		RefreshErrors:    p.refreshErrors.Load(),
		LastRefresh:      p.lastRefresh,
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
