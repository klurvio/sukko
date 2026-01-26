package monitoring

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Toniq-Labs/odin-ws/internal/types"
)

// =============================================================================
// Constant Tests
// =============================================================================

func TestErrorSeverityConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"Warning", ErrorSeverityWarning, "warning"},
		{"Critical", ErrorSeverityCritical, "critical"},
		{"Fatal", ErrorSeverityFatal, "fatal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("ErrorSeverity%s: got %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestErrorTypeConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"Kafka", ErrorTypeKafka, "kafka"},
		{"Broadcast", ErrorTypeBroadcast, "broadcast"},
		{"Serialization", ErrorTypeSerialization, "serialization"},
		{"Connection", ErrorTypeConnection, "connection"},
		{"Health", ErrorTypeHealth, "health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("ErrorType%s: got %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestDisconnectReasonConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"ReadError", DisconnectReasonReadError, "read_error"},
		{"WriteTimeout", DisconnectReasonWriteTimeout, "write_timeout"},
		{"PingTimeout", DisconnectReasonPingTimeout, "ping_timeout"},
		{"RateLimitExceeded", DisconnectReasonRateLimitExceeded, "rate_limit_exceeded"},
		{"ServerShutdown", DisconnectReasonServerShutdown, "server_shutdown"},
		{"ClientInitiated", DisconnectReasonClientInitiated, "client_initiated"},
		{"SubscriptionError", DisconnectReasonSubscriptionError, "subscription_error"},
		{"SendChannelClosed", DisconnectReasonSendChannelClosed, "send_channel_closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("DisconnectReason%s: got %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestDisconnectInitiatedByConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"Client", DisconnectInitiatedByClient, "client"},
		{"Server", DisconnectInitiatedByServer, "server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("DisconnectInitiatedBy%s: got %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestDropReasonConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"SendTimeout", DropReasonSendTimeout, "send_timeout"},
		{"BufferFull", DropReasonBufferFull, "buffer_full"},
		{"ClientDisconnected", DropReasonClientDisconnected, "client_disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("DropReason%s: got %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// =============================================================================
// Update Function Tests (verify they don't panic)
// =============================================================================

func TestUpdateMessageMetrics_IncrementsCounters(t *testing.T) {
	t.Parallel()
	// Get baseline values
	sentBefore := testutil.ToFloat64(messagesSent)
	receivedBefore := testutil.ToFloat64(messagesReceived)

	// Update with known values
	UpdateMessageMetrics(100, 50)

	// Verify counters incremented correctly
	sentAfter := testutil.ToFloat64(messagesSent)
	receivedAfter := testutil.ToFloat64(messagesReceived)

	if sentAfter-sentBefore != 100 {
		t.Errorf("messagesSent: got delta %f, want 100", sentAfter-sentBefore)
	}
	if receivedAfter-receivedBefore != 50 {
		t.Errorf("messagesReceived: got delta %f, want 50", receivedAfter-receivedBefore)
	}

	// Zero values should not change counters
	sentBefore = sentAfter
	receivedBefore = receivedAfter
	UpdateMessageMetrics(0, 0)

	if testutil.ToFloat64(messagesSent) != sentBefore {
		t.Error("messagesSent should not change with zero input")
	}
	if testutil.ToFloat64(messagesReceived) != receivedBefore {
		t.Error("messagesReceived should not change with zero input")
	}

	// Negative values should not change counters (guard in implementation)
	UpdateMessageMetrics(-1, -1)
	if testutil.ToFloat64(messagesSent) != sentBefore {
		t.Error("messagesSent should not change with negative input")
	}
}

func TestUpdateBytesMetrics_IncrementsCounters(t *testing.T) {
	t.Parallel()
	// Get baseline values
	sentBefore := testutil.ToFloat64(bytesSent)
	receivedBefore := testutil.ToFloat64(bytesReceived)

	// Update with known values
	UpdateBytesMetrics(1024, 512)

	// Verify counters incremented correctly
	sentAfter := testutil.ToFloat64(bytesSent)
	receivedAfter := testutil.ToFloat64(bytesReceived)

	if sentAfter-sentBefore != 1024 {
		t.Errorf("bytesSent: got delta %f, want 1024", sentAfter-sentBefore)
	}
	if receivedAfter-receivedBefore != 512 {
		t.Errorf("bytesReceived: got delta %f, want 512", receivedAfter-receivedBefore)
	}

	// Zero values should not change counters
	sentBefore = sentAfter
	UpdateBytesMetrics(0, 0)
	if testutil.ToFloat64(bytesSent) != sentBefore {
		t.Error("bytesSent should not change with zero input")
	}
}

func TestIncrementSlowClientDisconnects_IncrementsCounter(t *testing.T) {
	t.Parallel()
	// Get baseline value
	before := testutil.ToFloat64(slowClientsDisconnected)

	// Increment 10 times
	for range 10 {
		IncrementSlowClientDisconnects()
	}

	// Verify counter incremented correctly
	after := testutil.ToFloat64(slowClientsDisconnected)
	delta := after - before

	if delta != 10 {
		t.Errorf("slowClientsDisconnected: got delta %f, want 10", delta)
	}
}

func TestIncrementRateLimitedMessages_NoPanic(t *testing.T) {
	t.Parallel()
	for range 10 {
		IncrementRateLimitedMessages()
	}
}

func TestIncrementReplayRequests_NoPanic(t *testing.T) {
	t.Parallel()
	for range 10 {
		IncrementReplayRequests()
	}
}

func TestIncrementConnectionRateLimit_NoPanic(t *testing.T) {
	t.Parallel()
	IncrementConnectionRateLimit("per_ip")
	IncrementConnectionRateLimit("global")
	IncrementConnectionRateLimit("unknown")
}

func TestIncrementKafkaMessages_NoPanic(t *testing.T) {
	t.Parallel()
	for range 10 {
		IncrementKafkaMessages()
	}
}

func TestIncrementKafkaDropped_NoPanic(t *testing.T) {
	t.Parallel()
	for range 10 {
		IncrementKafkaDropped()
	}
}

func TestUpdateCapacityMetrics_NoPanic(t *testing.T) {
	t.Parallel()
	UpdateCapacityMetrics(10000, 75.0)
	UpdateCapacityMetrics(0, 0)
	UpdateCapacityMetrics(100000, 90.0)
}

func TestIncrementCapacityRejection_NoPanic(t *testing.T) {
	t.Parallel()
	IncrementCapacityRejection("cpu_limit")
	IncrementCapacityRejection("memory_limit")
	IncrementCapacityRejection("connection_limit")
}

func TestUpdateCapacityHeadroom_NoPanic(t *testing.T) {
	t.Parallel()
	UpdateCapacityHeadroom(25.0, 50.0)
	UpdateCapacityHeadroom(0.0, 0.0)
	UpdateCapacityHeadroom(100.0, 100.0)
}

// =============================================================================
// Record Error Tests
// =============================================================================

func TestRecordError_AllTypes(t *testing.T) {
	t.Parallel()
	errorTypes := []string{
		ErrorTypeKafka,
		ErrorTypeBroadcast,
		ErrorTypeSerialization,
		ErrorTypeConnection,
		ErrorTypeHealth,
	}
	severities := []string{
		ErrorSeverityWarning,
		ErrorSeverityCritical,
		ErrorSeverityFatal,
	}

	for _, et := range errorTypes {
		for _, sev := range severities {
			t.Run(et+"_"+sev, func(t *testing.T) {
				t.Parallel()
				// Should not panic
				RecordError(et, sev)
			})
		}
	}
}

func TestRecordKafkaError_AllSeverities(t *testing.T) {
	t.Parallel()
	RecordKafkaError(ErrorSeverityWarning)
	RecordKafkaError(ErrorSeverityCritical)
	RecordKafkaError(ErrorSeverityFatal)
}

func TestRecordBroadcastError_AllSeverities(t *testing.T) {
	t.Parallel()
	RecordBroadcastError(ErrorSeverityWarning)
	RecordBroadcastError(ErrorSeverityCritical)
	RecordBroadcastError(ErrorSeverityFatal)
}

func TestRecordSerializationError_AllSeverities(t *testing.T) {
	t.Parallel()
	RecordSerializationError(ErrorSeverityWarning)
	RecordSerializationError(ErrorSeverityCritical)
	RecordSerializationError(ErrorSeverityFatal)
}

func TestRecordConnectionError_AllSeverities(t *testing.T) {
	t.Parallel()
	RecordConnectionError(ErrorSeverityWarning)
	RecordConnectionError(ErrorSeverityCritical)
	RecordConnectionError(ErrorSeverityFatal)
}

// =============================================================================
// Record Disconnect Tests
// =============================================================================

func TestRecordDisconnect_AllReasons(t *testing.T) {
	t.Parallel()
	reasons := []string{
		DisconnectReasonReadError,
		DisconnectReasonWriteTimeout,
		DisconnectReasonPingTimeout,
		DisconnectReasonRateLimitExceeded,
		DisconnectReasonServerShutdown,
		DisconnectReasonClientInitiated,
		DisconnectReasonSubscriptionError,
		DisconnectReasonSendChannelClosed,
	}
	initiators := []string{
		DisconnectInitiatedByClient,
		DisconnectInitiatedByServer,
	}

	for _, reason := range reasons {
		for _, initiator := range initiators {
			t.Run(reason+"_"+initiator, func(t *testing.T) {
				t.Parallel()
				// Should not panic
				RecordDisconnect(reason, initiator, 5*time.Minute)
			})
		}
	}
}

func TestRecordDisconnectWithStats_UpdatesStats(t *testing.T) {
	t.Parallel()
	stats := &types.Stats{
		DisconnectsByReason: make(map[string]int64),
	}

	RecordDisconnectWithStats(stats, DisconnectReasonReadError, DisconnectInitiatedByClient, 1*time.Minute)
	RecordDisconnectWithStats(stats, DisconnectReasonReadError, DisconnectInitiatedByClient, 2*time.Minute)
	RecordDisconnectWithStats(stats, DisconnectReasonWriteTimeout, DisconnectInitiatedByServer, 3*time.Minute)

	stats.DisconnectsMu.RLock()
	defer stats.DisconnectsMu.RUnlock()

	if stats.DisconnectsByReason[DisconnectReasonReadError] != 2 {
		t.Errorf("ReadError count: got %d, want 2", stats.DisconnectsByReason[DisconnectReasonReadError])
	}
	if stats.DisconnectsByReason[DisconnectReasonWriteTimeout] != 1 {
		t.Errorf("WriteTimeout count: got %d, want 1", stats.DisconnectsByReason[DisconnectReasonWriteTimeout])
	}
}

// =============================================================================
// Record Dropped Broadcast Tests
// =============================================================================

func TestRecordDroppedBroadcast_AllReasons(t *testing.T) {
	t.Parallel()
	channels := []string{"trades", "orders", "quotes", "system"}
	reasons := []string{
		DropReasonSendTimeout,
		DropReasonBufferFull,
		DropReasonClientDisconnected,
	}

	for _, channel := range channels {
		for _, reason := range reasons {
			t.Run(channel+"_"+reason, func(t *testing.T) {
				t.Parallel()
				// Should not panic
				RecordDroppedBroadcast(channel, reason)
			})
		}
	}
}

func TestRecordDroppedBroadcastWithStats_UpdatesStats(t *testing.T) {
	t.Parallel()
	stats := &types.Stats{
		DroppedBroadcastsByChannel: make(map[string]int64),
	}

	RecordDroppedBroadcastWithStats(stats, "trades", DropReasonBufferFull)
	RecordDroppedBroadcastWithStats(stats, "trades", DropReasonBufferFull)
	RecordDroppedBroadcastWithStats(stats, "orders", DropReasonSendTimeout)

	stats.DropsMu.RLock()
	defer stats.DropsMu.RUnlock()

	if stats.DroppedBroadcastsByChannel["trades"] != 2 {
		t.Errorf("trades drops: got %d, want 2", stats.DroppedBroadcastsByChannel["trades"])
	}
	if stats.DroppedBroadcastsByChannel["orders"] != 1 {
		t.Errorf("orders drops: got %d, want 1", stats.DroppedBroadcastsByChannel["orders"])
	}
}

// =============================================================================
// Record Client Buffer Tests
// =============================================================================

func TestRecordSlowClientAttempt_NoPanic(t *testing.T) {
	t.Parallel()
	RecordSlowClientAttempt(1)
	RecordSlowClientAttempt(5)
	RecordSlowClientAttempt(10)
	RecordSlowClientAttempt(20)
}

func TestRecordClientBufferSize_NoPanic(t *testing.T) {
	t.Parallel()
	RecordClientBufferSize(0, 512)
	RecordClientBufferSize(256, 512)
	RecordClientBufferSize(512, 512)
}

func TestRecordClientBufferSizeWithStats_UpdatesStats(t *testing.T) {
	t.Parallel()
	stats := &types.Stats{
		BufferSaturationSamples: make([]int, 0, 100),
	}

	RecordClientBufferSizeWithStats(stats, 256, 512) // 50%
	RecordClientBufferSizeWithStats(stats, 512, 512) // 100%
	RecordClientBufferSizeWithStats(stats, 0, 512)   // 0%

	stats.BuffersMu.RLock()
	defer stats.BuffersMu.RUnlock()

	if len(stats.BufferSaturationSamples) != 3 {
		t.Errorf("Sample count: got %d, want 3", len(stats.BufferSaturationSamples))
	}

	// Verify percentages
	if stats.BufferSaturationSamples[0] != 50 {
		t.Errorf("First sample: got %d, want 50", stats.BufferSaturationSamples[0])
	}
	if stats.BufferSaturationSamples[1] != 100 {
		t.Errorf("Second sample: got %d, want 100", stats.BufferSaturationSamples[1])
	}
	if stats.BufferSaturationSamples[2] != 0 {
		t.Errorf("Third sample: got %d, want 0", stats.BufferSaturationSamples[2])
	}
}

func TestRecordClientBufferSizeWithStats_SlidingWindow(t *testing.T) {
	t.Parallel()
	stats := &types.Stats{
		BufferSaturationSamples: make([]int, 0, 100),
	}

	// Add more than 100 samples (0-105 inclusive = 106 samples)
	for i := range 106 {
		RecordClientBufferSizeWithStats(stats, i, 512)
	}

	stats.BuffersMu.RLock()
	defer stats.BuffersMu.RUnlock()

	// Should only have 100 samples (sliding window)
	if len(stats.BufferSaturationSamples) != 100 {
		t.Errorf("Sample count: got %d, want 100", len(stats.BufferSaturationSamples))
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestRecordDisconnectWithStats_Concurrent(t *testing.T) {
	t.Parallel()
	stats := &types.Stats{
		DisconnectsByReason: make(map[string]int64),
	}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reason := DisconnectReasonReadError
			if id%2 == 0 {
				reason = DisconnectReasonWriteTimeout
			}
			RecordDisconnectWithStats(stats, reason, DisconnectInitiatedByServer, time.Minute)
		}(i)
	}

	wg.Wait()

	stats.DisconnectsMu.RLock()
	total := stats.DisconnectsByReason[DisconnectReasonReadError] +
		stats.DisconnectsByReason[DisconnectReasonWriteTimeout]
	stats.DisconnectsMu.RUnlock()

	if total != 100 {
		t.Errorf("Total disconnects: got %d, want 100", total)
	}
}

func TestRecordDroppedBroadcastWithStats_Concurrent(t *testing.T) {
	t.Parallel()
	stats := &types.Stats{
		DroppedBroadcastsByChannel: make(map[string]int64),
	}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			channel := "trades"
			if id%2 == 0 {
				channel = "orders"
			}
			RecordDroppedBroadcastWithStats(stats, channel, DropReasonBufferFull)
		}(i)
	}

	wg.Wait()

	stats.DropsMu.RLock()
	total := stats.DroppedBroadcastsByChannel["trades"] +
		stats.DroppedBroadcastsByChannel["orders"]
	stats.DropsMu.RUnlock()

	if total != 100 {
		t.Errorf("Total drops: got %d, want 100", total)
	}
}

// =============================================================================
// MetricsCollector Tests
// =============================================================================

type mockServerMetrics struct {
	config        types.ServerConfig
	stats         *types.Stats
	kafkaConsumer any
}

func (m *mockServerMetrics) GetConfig() types.ServerConfig {
	return m.config
}

func (m *mockServerMetrics) GetStats() *types.Stats {
	return m.stats
}

func (m *mockServerMetrics) GetKafkaConsumer() any {
	return m.kafkaConsumer
}

func TestNewMetricsCollector(t *testing.T) {
	t.Parallel()
	mock := &mockServerMetrics{
		config: types.ServerConfig{
			MaxConnections:  10000,
			MetricsInterval: time.Second,
		},
		stats: &types.Stats{},
	}

	collector := NewMetricsCollector(mock)

	if collector == nil {
		t.Fatal("NewMetricsCollector should return non-nil")
	}
	if collector.server != mock {
		t.Error("server should be set")
	}
	if collector.stopChan == nil {
		t.Error("stopChan should be initialized")
	}
}

//nolint:paralleltest // shares global Prometheus metrics
func TestMetricsCollector_StartStop(_ *testing.T) {
	var connCount int64 = 500
	mock := &mockServerMetrics{
		config: types.ServerConfig{
			MaxConnections:  10000,
			MetricsInterval: 50 * time.Millisecond, // Fast for testing
		},
		stats: &types.Stats{
			CurrentConnections: connCount,
		},
	}

	collector := NewMetricsCollector(mock)

	// Start should not block
	collector.Start()

	// Give it time to collect at least once
	time.Sleep(100 * time.Millisecond)

	// Stop should not block or panic
	collector.Stop()
}

//nolint:paralleltest // shares global Prometheus metrics
func TestMetricsCollector_CollectsStats(t *testing.T) {
	var connCount int64 = 250
	mock := &mockServerMetrics{
		config: types.ServerConfig{
			MaxConnections:  5000,
			MetricsInterval: 50 * time.Millisecond,
		},
		stats: &types.Stats{
			CurrentConnections: connCount,
		},
	}

	collector := NewMetricsCollector(mock)

	// Update connection count before starting collector
	atomic.StoreInt64(&mock.stats.CurrentConnections, 500)

	collector.Start()
	time.Sleep(100 * time.Millisecond)
	collector.Stop()

	// NOTE: connectionsActive is now set by LoadBalancer.aggregateMetrics()
	// to fix multi-shard overwrite bug, so we don't test it here

	// Verify max connections was set
	maxConns := testutil.ToFloat64(connectionsMax)
	if maxConns != 5000 {
		t.Errorf("connectionsMax: got %f, want 5000", maxConns)
	}

	// Verify goroutines gauge was updated (should be > 0)
	goroutines := testutil.ToFloat64(goroutinesActive)
	if goroutines <= 0 {
		t.Errorf("goroutinesActive: got %f, should be > 0", goroutines)
	}
}

//nolint:paralleltest // shares global Prometheus metrics
func TestMetricsCollector_KafkaStatus_NoConsumer(_ *testing.T) {
	mock := &mockServerMetrics{
		config: types.ServerConfig{
			MaxConnections:  10000,
			MetricsInterval: 50 * time.Millisecond,
		},
		stats:         &types.Stats{},
		kafkaConsumer: nil, // No Kafka consumer
	}

	collector := NewMetricsCollector(mock)
	collector.Start()
	time.Sleep(100 * time.Millisecond)
	collector.Stop()
}

func TestMetricsCollector_KafkaStatus_WithConsumer(t *testing.T) {
	t.Parallel()
	mock := &mockServerMetrics{
		config: types.ServerConfig{
			MaxConnections:  10000,
			MetricsInterval: 50 * time.Millisecond,
		},
		stats:         &types.Stats{},
		kafkaConsumer: struct{}{}, // Non-nil consumer
	}

	collector := NewMetricsCollector(mock)
	collector.Start()
	time.Sleep(100 * time.Millisecond)
	collector.Stop()
}
