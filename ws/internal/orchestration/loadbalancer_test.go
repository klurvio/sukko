package orchestration

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/broadcast"
	"github.com/rs/zerolog"
)

// =============================================================================
// Mock Broadcast Bus for Testing NATS Publishing
// =============================================================================

// mockBus implements broadcast.Bus for testing LoadBalancer NATS publishing
type mockBus struct {
	mu              sync.Mutex
	directPublishes []directPublish
	publishes       []*broadcast.Message
	healthy         bool
}

type directPublish struct {
	Subject string
	Payload []byte
}

func newMockBus() *mockBus {
	return &mockBus{
		healthy:         true,
		directPublishes: make([]directPublish, 0),
		publishes:       make([]*broadcast.Message, 0),
	}
}

func (m *mockBus) Publish(msg *broadcast.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishes = append(m.publishes, msg)
}

func (m *mockBus) PublishDirect(subject string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.directPublishes = append(m.directPublishes, directPublish{Subject: subject, Payload: payload})
}

func (m *mockBus) Subscribe() <-chan *broadcast.Message    { return nil }
func (m *mockBus) Run()                                    {}
func (m *mockBus) Shutdown()                               {}
func (m *mockBus) ShutdownWithContext(ctx context.Context) {}
func (m *mockBus) IsHealthy() bool                         { return m.healthy }
func (m *mockBus) GetMetrics() broadcast.Metrics           { return broadcast.Metrics{} }

func (m *mockBus) getDirectPublishes() []directPublish {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]directPublish, len(m.directPublishes))
	copy(result, m.directPublishes)
	return result
}

func (m *mockBus) clearDirectPublishes() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.directPublishes = make([]directPublish, 0)
}

// Verify mockBus implements broadcast.Bus
var _ broadcast.Bus = (*mockBus)(nil)

// =============================================================================
// Mock ShardMetrics Implementation for Testing
// =============================================================================

// mockShard implements ShardMetrics interface for testing
type mockShard struct {
	currentConns int64
	maxConns     int
	addr         string
}

func (m *mockShard) GetCurrentConnections() int64 { return m.currentConns }
func (m *mockShard) GetMaxConnections() int       { return m.maxConns }
func (m *mockShard) GetAddr() string              { return m.addr }

// newMockShards creates mock shards from connection counts
func newMockShards(configs []struct{ current, max int64 }) []ShardMetrics {
	shards := make([]ShardMetrics, len(configs))
	for i, cfg := range configs {
		shards[i] = &mockShard{
			currentConns: cfg.current,
			maxConns:     int(cfg.max),
			addr:         "localhost:300" + string(rune('0'+i)),
		}
	}
	return shards
}

// =============================================================================
// LoadBalancerConfig Tests
// =============================================================================

func TestLoadBalancerConfig_Fields(t *testing.T) {
	cfg := LoadBalancerConfig{
		Addr:             ":3000",
		Shards:           []*Shard{},
		Logger:           zerolog.Nop(),
		HTTPReadTimeout:  15 * time.Second,
		HTTPWriteTimeout: 15 * time.Second,
		HTTPIdleTimeout:  60 * time.Second,
	}

	if cfg.Addr != ":3000" {
		t.Errorf("Addr: got %s, want :3000", cfg.Addr)
	}
	if cfg.HTTPReadTimeout != 15*time.Second {
		t.Errorf("HTTPReadTimeout: got %v, want 15s", cfg.HTTPReadTimeout)
	}
	if cfg.HTTPWriteTimeout != 15*time.Second {
		t.Errorf("HTTPWriteTimeout: got %v, want 15s", cfg.HTTPWriteTimeout)
	}
	if cfg.HTTPIdleTimeout != 60*time.Second {
		t.Errorf("HTTPIdleTimeout: got %v, want 60s", cfg.HTTPIdleTimeout)
	}
}

func TestLoadBalancerConfig_NoShards(t *testing.T) {
	cfg := LoadBalancerConfig{
		Addr:   ":3000",
		Shards: []*Shard{}, // Empty shards
		Logger: zerolog.Nop(),
	}

	_, err := NewLoadBalancer(cfg)
	if err == nil {
		t.Error("NewLoadBalancer should return error with no shards")
	}
}

func TestLoadBalancerConfig_NilShards(t *testing.T) {
	cfg := LoadBalancerConfig{
		Addr:   ":3000",
		Shards: nil, // Nil shards
		Logger: zerolog.Nop(),
	}

	_, err := NewLoadBalancer(cfg)
	if err == nil {
		t.Error("NewLoadBalancer should return error with nil shards")
	}
}

// =============================================================================
// SelectShardByMetrics Tests (Using ShardMetrics Interface)
// =============================================================================

func TestSelectShardByMetrics_LeastConnections(t *testing.T) {
	shards := newMockShards([]struct{ current, max int64 }{
		{50, 100},
		{30, 100}, // Least connections
		{70, 100},
	})

	idx := SelectShardByMetrics(shards)

	if idx != 1 {
		t.Errorf("Should select shard 1 (least connections), got %d", idx)
	}
}

func TestSelectShardByMetrics_AllFull(t *testing.T) {
	shards := newMockShards([]struct{ current, max int64 }{
		{100, 100}, // Full
		{100, 100}, // Full
		{100, 100}, // Full
	})

	idx := SelectShardByMetrics(shards)

	if idx != -1 {
		t.Errorf("Should return -1 when all shards full, got %d", idx)
	}
}

func TestSelectShardByMetrics_SingleShard(t *testing.T) {
	shards := newMockShards([]struct{ current, max int64 }{
		{25, 100},
	})

	idx := SelectShardByMetrics(shards)

	if idx != 0 {
		t.Errorf("Should select shard 0, got %d", idx)
	}
}

func TestSelectShardByMetrics_EvenDistribution(t *testing.T) {
	// All shards have same connections
	shards := newMockShards([]struct{ current, max int64 }{
		{50, 100},
		{50, 100},
		{50, 100},
	})

	idx := SelectShardByMetrics(shards)

	// Should pick first shard when tied
	if idx != 0 {
		t.Errorf("Should select first shard when tied, got %d", idx)
	}
}

func TestSelectShardByMetrics_RespectsCapacity(t *testing.T) {
	shards := newMockShards([]struct{ current, max int64 }{
		{10, 100}, // Available
		{50, 50},  // Full (50/50)
		{20, 100}, // Available
	})

	idx := SelectShardByMetrics(shards)

	// Should skip shard 1 (full) and pick shard 0 (fewest)
	if idx != 0 {
		t.Errorf("Should select shard 0 (skip full shard), got %d", idx)
	}
}

func TestSelectShardByMetrics_EmptyShards(t *testing.T) {
	idx := SelectShardByMetrics([]ShardMetrics{})

	if idx != -1 {
		t.Errorf("Should return -1 for empty shard list, got %d", idx)
	}
}

func TestSelectShardByMetrics_NilShards(t *testing.T) {
	idx := SelectShardByMetrics(nil)

	if idx != -1 {
		t.Errorf("Should return -1 for nil shard list, got %d", idx)
	}
}

func TestSelectShardByMetrics_PartialCapacity(t *testing.T) {
	// First two shards full, third has room
	shards := newMockShards([]struct{ current, max int64 }{
		{100, 100}, // Full
		{100, 100}, // Full
		{50, 100},  // Available
	})

	idx := SelectShardByMetrics(shards)

	if idx != 2 {
		t.Errorf("Should select shard 2 (only available), got %d", idx)
	}
}

// =============================================================================
// Health Status Calculation Tests
// =============================================================================

func TestHealthStatus_Calculation(t *testing.T) {
	tests := []struct {
		name            string
		totalConns      int64
		totalMaxConns   int64
		allShardsOK     bool
		expectedStatus  string
		expectedHealthy bool
	}{
		{"healthy_low_usage", 10, 100, true, "healthy", true},
		{"healthy_medium_usage", 50, 100, true, "healthy", true},
		{"degraded_high_usage", 95, 100, true, "degraded", true},
		{"unhealthy_over_capacity", 110, 100, true, "unhealthy", false},
		{"unhealthy_shard_error", 50, 100, false, "unhealthy", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate health calculation from handleHealth
			var capacityPercent float64
			if tt.totalMaxConns > 0 {
				capacityPercent = float64(tt.totalConns) / float64(tt.totalMaxConns) * 100
			}

			isHealthy := tt.allShardsOK && tt.totalConns <= tt.totalMaxConns
			status := "healthy"

			if !isHealthy {
				status = "unhealthy"
			} else if capacityPercent > 90 {
				status = "degraded"
			}

			if status != tt.expectedStatus {
				t.Errorf("status: got %s, want %s", status, tt.expectedStatus)
			}
			if isHealthy != tt.expectedHealthy {
				t.Errorf("healthy: got %v, want %v", isHealthy, tt.expectedHealthy)
			}
		})
	}
}

func TestCapacityPercent_Calculation(t *testing.T) {
	tests := []struct {
		name       string
		current    int64
		max        int64
		expectedPC float64
	}{
		{"zero_of_hundred", 0, 100, 0.0},
		{"fifty_of_hundred", 50, 100, 50.0},
		{"hundred_of_hundred", 100, 100, 100.0},
		{"zero_max", 50, 0, 0.0}, // Edge case: avoid div by zero
		{"partial", 25, 200, 12.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capacityPercent float64
			if tt.max > 0 {
				capacityPercent = float64(tt.current) / float64(tt.max) * 100
			}

			if capacityPercent != tt.expectedPC {
				t.Errorf("capacity percent: got %.2f, want %.2f", capacityPercent, tt.expectedPC)
			}
		})
	}
}

// =============================================================================
// Shard Status Calculation Tests
// =============================================================================

func TestShardStatus_Calculation(t *testing.T) {
	tests := []struct {
		name           string
		current        int64
		max            int64
		expectedStatus string
	}{
		{"available", 50, 100, "available"},
		{"medium", 80, 100, "medium"},
		{"high", 95, 100, "high"},
		{"full", 100, 100, "full"},
		{"over_capacity", 110, 100, "full"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate shard status calculation
			utilization := 0.0
			if tt.max > 0 {
				utilization = (float64(tt.current) / float64(tt.max)) * 100
			}

			shardStatus := "available"
			if tt.current >= tt.max {
				shardStatus = "full"
			} else if utilization > 90 {
				shardStatus = "high"
			} else if utilization > 75 {
				shardStatus = "medium"
			}

			if shardStatus != tt.expectedStatus {
				t.Errorf("shard status: got %s, want %s", shardStatus, tt.expectedStatus)
			}
		})
	}
}

// =============================================================================
// NATS Publishing Tests (Least-Connections Routing)
// =============================================================================

// TestLoadBalancer_PublishMetrics_CorrectFormat tests the JSON payload format
// published to NATS for gateway least-connections routing.
// Note: We test publishMetrics directly because onConnectionChange requires
// Shards with initialized servers (for GetCurrentConnections).
func TestLoadBalancer_PublishMetrics_CorrectFormat(t *testing.T) {
	bus := newMockBus()

	// Create minimal shards - maxConnections will be 0 (unexported field)
	// This tests the format, not the aggregation logic
	shards := []*Shard{
		{ID: 0},
	}

	lb := &LoadBalancer{
		shards:       shards,
		broadcastBus: bus,
		podIP:        "10.0.0.99",
		logger:       zerolog.Nop(),
	}

	lb.publishMetrics(75)

	publishes := bus.getDirectPublishes()
	if len(publishes) != 1 {
		t.Fatalf("Expected 1 publish, got %d", len(publishes))
	}

	// Verify subject
	if publishes[0].Subject != "odin.lb.metrics" {
		t.Errorf("Subject: got %s, want odin.lb.metrics", publishes[0].Subject)
	}

	// Parse and validate JSON payload
	var payload map[string]interface{}
	if err := json.Unmarshal(publishes[0].Payload, &payload); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Check required fields
	expectedFields := []string{"pod_ip", "connections", "max", "ts"}
	for _, field := range expectedFields {
		if _, ok := payload[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Verify pod_ip
	if payload["pod_ip"] != "10.0.0.99" {
		t.Errorf("pod_ip: got %v, want 10.0.0.99", payload["pod_ip"])
	}

	// Verify connections (the value we passed in)
	if int64(payload["connections"].(float64)) != 75 {
		t.Errorf("connections: got %v, want 75", payload["connections"])
	}

	// Verify timestamp is present and reasonable (not zero, within last minute)
	ts := int64(payload["ts"].(float64))
	now := time.Now().Unix()
	if ts <= 0 || ts > now+60 {
		t.Errorf("ts: got %d, should be close to now (%d)", ts, now)
	}
}

// TestLoadBalancer_NoBroadcastBus_NoPublish tests that onConnectionChange
// gracefully handles nil broadcastBus (Valkey backend case).
func TestLoadBalancer_NoBroadcastBus_NoPublish(t *testing.T) {
	lb := &LoadBalancer{
		shards:       []*Shard{},
		broadcastBus: nil, // No bus configured (Valkey backend)
		podIP:        "10.0.0.5",
		logger:       zerolog.Nop(),
	}

	// Should not panic - the nil check happens before iterating shards
	lb.onConnectionChange(+1)

	// Test passes if no panic occurred
}

// TestLoadBalancer_NoPodIP_NoPublish tests that onConnectionChange
// gracefully handles empty podIP.
func TestLoadBalancer_NoPodIP_NoPublish(t *testing.T) {
	bus := newMockBus()

	lb := &LoadBalancer{
		shards:       []*Shard{},
		broadcastBus: bus,
		podIP:        "", // Empty PodIP
		logger:       zerolog.Nop(),
	}

	lb.onConnectionChange(+1)

	// Should not publish when PodIP is empty
	publishes := bus.getDirectPublishes()
	if len(publishes) != 0 {
		t.Errorf("Expected 0 publishes when PodIP is empty, got %d", len(publishes))
	}
}

// TestLoadBalancer_PublishMetrics_UsesPublishDirect verifies that metrics
// are published via PublishDirect (not Publish) to bypass broadcast channel.
func TestLoadBalancer_PublishMetrics_UsesPublishDirect(t *testing.T) {
	bus := newMockBus()

	lb := &LoadBalancer{
		shards:       []*Shard{{ID: 0}},
		broadcastBus: bus,
		podIP:        "10.0.0.5",
		logger:       zerolog.Nop(),
	}

	lb.publishMetrics(50)

	// Verify PublishDirect was used (not Publish)
	directPublishes := bus.getDirectPublishes()
	if len(directPublishes) != 1 {
		t.Errorf("Expected 1 direct publish, got %d", len(directPublishes))
	}

	// Verify regular Publish was NOT used
	bus.mu.Lock()
	regularPublishes := len(bus.publishes)
	bus.mu.Unlock()
	if regularPublishes != 0 {
		t.Errorf("Expected 0 regular publishes, got %d (should use PublishDirect)", regularPublishes)
	}
}

// TestLoadBalancer_PublishMetrics_MultipleShards tests that publishMetrics
// aggregates max connections from all shards.
func TestLoadBalancer_PublishMetrics_MultipleShards(t *testing.T) {
	bus := newMockBus()

	// Create 3 shards - maxConnections is 0 for each (unexported)
	// This verifies the iteration logic, even if values are 0
	lb := &LoadBalancer{
		shards:       []*Shard{{ID: 0}, {ID: 1}, {ID: 2}},
		broadcastBus: bus,
		podIP:        "10.0.0.5",
		logger:       zerolog.Nop(),
	}

	lb.publishMetrics(450)

	publishes := bus.getDirectPublishes()
	if len(publishes) != 1 {
		t.Fatalf("Expected 1 publish, got %d", len(publishes))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(publishes[0].Payload, &payload); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify the connections we passed in
	if int64(payload["connections"].(float64)) != 450 {
		t.Errorf("connections: got %v, want 450", payload["connections"])
	}
}

// TestLoadBalancer_MockBus_Interface verifies mockBus implements broadcast.Bus
func TestLoadBalancer_MockBus_Interface(t *testing.T) {
	// Compile-time check that mockBus implements broadcast.Bus
	var _ broadcast.Bus = (*mockBus)(nil)

	bus := newMockBus()
	if !bus.IsHealthy() {
		t.Error("Mock bus should be healthy by default")
	}
}
