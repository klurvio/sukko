package orchestration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/server/broadcast"
	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
)

// =============================================================================
// Mock Implementations
// =============================================================================

// mockTenantRegistry implements kafka.TenantRegistry for testing
type mockTenantRegistry struct {
	sharedTopics     []string
	dedicatedTenants []kafka.TenantTopics
	err              error
	callCount        int64
	mu               sync.Mutex
}

func (m *mockTenantRegistry) GetSharedTenantTopics(_ context.Context, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.sharedTopics, nil
}

func (m *mockTenantRegistry) GetDedicatedTenants(_ context.Context, _ string) ([]kafka.TenantTopics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.dedicatedTenants, nil
}

// mockBroadcastBus implements broadcast.Bus for testing
type mockBroadcastBus struct {
	messages     []*broadcast.Message
	publishCount int64
	mu           sync.Mutex
}

func (m *mockBroadcastBus) Publish(msg *broadcast.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.publishCount++
}

func (m *mockBroadcastBus) Subscribe() <-chan *broadcast.Message {
	return make(chan *broadcast.Message, 100)
}

func (m *mockBroadcastBus) Run() {
	// Not used in tests
}

func (m *mockBroadcastBus) Shutdown() {
	// Not used in tests
}

func (m *mockBroadcastBus) ShutdownWithContext(_ context.Context) {
	// Not used in tests
}

func (m *mockBroadcastBus) IsHealthy() bool {
	return true
}

func (m *mockBroadcastBus) GetMetrics() broadcast.Metrics {
	return broadcast.Metrics{
		Type:    "mock",
		Healthy: true,
	}
}

func (m *mockBroadcastBus) getPublishCount() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.publishCount
}

// mockResourceGuard implements kafka.ResourceGuard for testing
type mockResourceGuard struct{}

func (m *mockResourceGuard) AllowKafkaMessage(_ context.Context) (bool, time.Duration) {
	return true, 0
}

func (m *mockResourceGuard) ShouldPauseKafka() bool {
	return false
}

// =============================================================================
// MultiTenantPoolConfig Tests
// =============================================================================

func TestMultiTenantPoolConfig_Validation(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	tests := []struct {
		name      string
		config    MultiTenantPoolConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid config",
			config: MultiTenantPoolConfig{
				Brokers:       []string{"localhost:9092"},
				Namespace:     "prod",
				Registry:      registry,
				BroadcastBus:  bus,
				ResourceGuard: guard,
				Logger:        logger,
			},
			wantErr: false,
		},
		{
			name: "missing brokers",
			config: MultiTenantPoolConfig{
				Namespace:     "prod",
				Registry:      registry,
				BroadcastBus:  bus,
				ResourceGuard: guard,
				Logger:        logger,
			},
			wantErr:   true,
			errSubstr: "broker",
		},
		{
			name: "missing namespace",
			config: MultiTenantPoolConfig{
				Brokers:       []string{"localhost:9092"},
				Registry:      registry,
				BroadcastBus:  bus,
				ResourceGuard: guard,
				Logger:        logger,
			},
			wantErr:   true,
			errSubstr: "namespace",
		},
		{
			name: "missing registry",
			config: MultiTenantPoolConfig{
				Brokers:       []string{"localhost:9092"},
				Namespace:     "prod",
				BroadcastBus:  bus,
				ResourceGuard: guard,
				Logger:        logger,
			},
			wantErr:   true,
			errSubstr: "registry",
		},
		{
			name: "missing broadcast bus",
			config: MultiTenantPoolConfig{
				Brokers:       []string{"localhost:9092"},
				Namespace:     "prod",
				Registry:      registry,
				ResourceGuard: guard,
				Logger:        logger,
			},
			wantErr:   true,
			errSubstr: "broadcast bus",
		},
		{
			name: "missing resource guard",
			config: MultiTenantPoolConfig{
				Brokers:      []string{"localhost:9092"},
				Namespace:    "prod",
				Registry:     registry,
				BroadcastBus: bus,
				Logger:       logger,
			},
			wantErr:   true,
			errSubstr: "resource guard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewMultiTenantConsumerPool(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestMultiTenantPool_DefaultRefreshInterval(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	config := MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
		// RefreshInterval not set - should use default
	}

	pool, err := NewMultiTenantConsumerPool(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pool.refreshInterval != 60*time.Second {
		t.Errorf("refreshInterval: got %v, want 60s", pool.refreshInterval)
	}
}

func TestMultiTenantPool_CustomRefreshInterval(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	config := MultiTenantPoolConfig{
		Brokers:         []string{"localhost:9092"},
		Namespace:       "prod",
		Registry:        registry,
		BroadcastBus:    bus,
		ResourceGuard:   guard,
		Logger:          logger,
		RefreshInterval: 30 * time.Second,
	}

	pool, err := NewMultiTenantConsumerPool(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pool.refreshInterval != 30*time.Second {
		t.Errorf("refreshInterval: got %v, want 30s", pool.refreshInterval)
	}
}

// =============================================================================
// MultiTenantPoolMetrics Tests
// =============================================================================

func TestMultiTenantPoolMetrics_Fields(t *testing.T) {
	t.Parallel()
	metrics := MultiTenantPoolMetrics{
		MessagesRouted:   100,
		MessagesDropped:  5,
		TopicsSubscribed: 10,
		DedicatedCount:   3,
		RefreshCount:     50,
		RefreshErrors:    2,
		LastRefresh:      time.Now(),
	}

	if metrics.MessagesRouted != 100 {
		t.Errorf("MessagesRouted: got %d, want 100", metrics.MessagesRouted)
	}
	if metrics.MessagesDropped != 5 {
		t.Errorf("MessagesDropped: got %d, want 5", metrics.MessagesDropped)
	}
	if metrics.TopicsSubscribed != 10 {
		t.Errorf("TopicsSubscribed: got %d, want 10", metrics.TopicsSubscribed)
	}
	if metrics.DedicatedCount != 3 {
		t.Errorf("DedicatedCount: got %d, want 3", metrics.DedicatedCount)
	}
	if metrics.RefreshCount != 50 {
		t.Errorf("RefreshCount: got %d, want 50", metrics.RefreshCount)
	}
	if metrics.RefreshErrors != 2 {
		t.Errorf("RefreshErrors: got %d, want 2", metrics.RefreshErrors)
	}
}

func TestMultiTenantPool_GetMetrics_Initial(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	pool, err := NewMultiTenantConsumerPool(MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := pool.GetMetrics()

	// Initial metrics should all be zero
	if metrics.MessagesRouted != 0 {
		t.Errorf("Initial MessagesRouted: got %d, want 0", metrics.MessagesRouted)
	}
	if metrics.MessagesDropped != 0 {
		t.Errorf("Initial MessagesDropped: got %d, want 0", metrics.MessagesDropped)
	}
	if metrics.TopicsSubscribed != 0 {
		t.Errorf("Initial TopicsSubscribed: got %d, want 0", metrics.TopicsSubscribed)
	}
	if metrics.DedicatedCount != 0 {
		t.Errorf("Initial DedicatedCount: got %d, want 0", metrics.DedicatedCount)
	}
}

// =============================================================================
// RouteMessage Tests
// =============================================================================

func TestMultiTenantPool_RouteMessage(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	pool, err := NewMultiTenantConsumerPool(MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test routing
	pool.routeMessage("BTC.trade", []byte(`{"price":"100.50"}`))

	if bus.getPublishCount() != 1 {
		t.Errorf("publishCount: got %d, want 1", bus.getPublishCount())
	}

	metrics := pool.GetMetrics()
	if metrics.MessagesRouted != 1 {
		t.Errorf("MessagesRouted: got %d, want 1", metrics.MessagesRouted)
	}
}

func TestMultiTenantPool_RouteMessage_Concurrent(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	pool, err := NewMultiTenantConsumerPool(MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numGoroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				pool.routeMessage("BTC.trade", []byte(`{}`))
			}
		}()
	}

	wg.Wait()

	expected := uint64(numGoroutines * opsPerGoroutine)
	metrics := pool.GetMetrics()
	if metrics.MessagesRouted != expected {
		t.Errorf("MessagesRouted: got %d, want %d", metrics.MessagesRouted, expected)
	}
}

// =============================================================================
// Atomic Counter Tests
// =============================================================================

func TestMultiTenantPool_AtomicCounters(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	pool, err := NewMultiTenantConsumerPool(MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test atomic increments
	pool.messagesRouted.Add(100)
	pool.messagesDropped.Add(5)
	pool.refreshCount.Add(10)
	pool.refreshErrors.Add(2)

	metrics := pool.GetMetrics()
	if metrics.MessagesRouted != 100 {
		t.Errorf("MessagesRouted: got %d, want 100", metrics.MessagesRouted)
	}
	if metrics.MessagesDropped != 5 {
		t.Errorf("MessagesDropped: got %d, want 5", metrics.MessagesDropped)
	}
	if metrics.RefreshCount != 10 {
		t.Errorf("RefreshCount: got %d, want 10", metrics.RefreshCount)
	}
	if metrics.RefreshErrors != 2 {
		t.Errorf("RefreshErrors: got %d, want 2", metrics.RefreshErrors)
	}
}

// =============================================================================
// Consumer Group Naming Tests
// =============================================================================

func TestMultiTenantPool_ConsumerGroupNaming(t *testing.T) {
	t.Parallel()
	tests := []struct {
		namespace string
		tenantID  string
		isShared  bool
		expected  string
	}{
		{"prod", "", true, "odin-shared-prod"},
		{"dev", "", true, "odin-shared-dev"},
		{"prod", "acme", false, "odin-acme-prod"},
		{"dev", "bigcorp", false, "odin-bigcorp-dev"},
		{"staging", "tenant123", false, "odin-tenant123-staging"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			var result string
			if tt.isShared {
				result = "odin-shared-" + tt.namespace
			} else {
				result = "odin-" + tt.tenantID + "-" + tt.namespace
			}

			if result != tt.expected {
				t.Errorf("ConsumerGroup: got %s, want %s", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// GetSharedConsumer Tests
// =============================================================================

func TestMultiTenantPool_GetSharedConsumer_Initial(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	pool, err := NewMultiTenantConsumerPool(MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Before start, shared consumer should be nil
	consumer := pool.GetSharedConsumer()
	if consumer != nil {
		t.Error("GetSharedConsumer: expected nil before Start")
	}
}

// =============================================================================
// Topic Tracking Tests
// =============================================================================

func TestMultiTenantPool_TopicTracking(t *testing.T) {
	t.Parallel()
	logger := zerolog.New(nil).Level(zerolog.Disabled)
	registry := &mockTenantRegistry{
		sharedTopics: []string{
			"prod.acme.trade",
			"prod.acme.liquidity",
			"prod.bigcorp.trade",
		},
	}
	bus := &mockBroadcastBus{}
	guard := &mockResourceGuard{}

	pool, err := NewMultiTenantConsumerPool(MultiTenantPoolConfig{
		Brokers:       []string{"localhost:9092"},
		Namespace:     "prod",
		Registry:      registry,
		BroadcastBus:  bus,
		ResourceGuard: guard,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify initial state
	if len(pool.sharedTopics) != 0 {
		t.Errorf("Initial sharedTopics: got %d, want 0", len(pool.sharedTopics))
	}
}

// =============================================================================
// Ensure interface implementations
// =============================================================================

var _ kafka.TenantRegistry = (*mockTenantRegistry)(nil)
var _ broadcast.Bus = (*mockBroadcastBus)(nil)
var _ kafka.ResourceGuard = (*mockResourceGuard)(nil)
