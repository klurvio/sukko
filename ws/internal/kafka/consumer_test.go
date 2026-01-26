package kafka

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// =============================================================================
// Mock ResourceGuard for Testing
// =============================================================================

type mockResourceGuard struct {
	allowKafka   bool
	shouldPause  bool
	waitDuration time.Duration
	allowCount   atomic.Int64
	pauseCount   atomic.Int64
}

func newMockResourceGuard() *mockResourceGuard {
	return &mockResourceGuard{
		allowKafka:  true,
		shouldPause: false,
	}
}

func (m *mockResourceGuard) AllowKafkaMessage(ctx context.Context) (bool, time.Duration) {
	m.allowCount.Add(1)
	return m.allowKafka, m.waitDuration
}

func (m *mockResourceGuard) ShouldPauseKafka() bool {
	m.pauseCount.Add(1)
	return m.shouldPause
}

// =============================================================================
// ConsumerConfig Validation Tests
// =============================================================================

func TestNewConsumer_NoBrokers(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()
	broadcast := func(subject string, message []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{},
		ConsumerGroup: "test-group",
		Topics:        []string{GetRefinedTopic("test", TopicBaseTrade)},
		Logger:        &logger,
		Broadcast:     broadcast,
		ResourceGuard: guard,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no brokers")
	}
}

func TestNewConsumer_NoConsumerGroup(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()
	broadcast := func(subject string, message []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "",
		Topics:        []string{GetRefinedTopic("test", TopicBaseTrade)},
		Logger:        &logger,
		Broadcast:     broadcast,
		ResourceGuard: guard,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no consumer group")
	}
}

func TestNewConsumer_NoTopics(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()
	broadcast := func(subject string, message []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{},
		Logger:        &logger,
		Broadcast:     broadcast,
		ResourceGuard: guard,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no topics")
	}
}

func TestNewConsumer_NoBroadcast(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{GetRefinedTopic("test", TopicBaseTrade)},
		Logger:        &logger,
		Broadcast:     nil,
		ResourceGuard: guard,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no broadcast function")
	}
}

func TestNewConsumer_NoResourceGuard(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	broadcast := func(subject string, message []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{GetRefinedTopic("test", TopicBaseTrade)},
		Logger:        &logger,
		Broadcast:     broadcast,
		ResourceGuard: nil,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no resource guard")
	}
}

// =============================================================================
// ConsumerConfig Fields Tests
// =============================================================================

func TestConsumerConfig_BatchDefaults(t *testing.T) {
	t.Parallel()
	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{GetRefinedTopic("test", TopicBaseTrade)},
	}

	// Default BatchSize should be 0 (will be set to 50 in NewConsumer)
	if cfg.BatchSize != 0 {
		t.Errorf("Default BatchSize = %d, want 0", cfg.BatchSize)
	}

	// Default BatchTimeout should be 0 (will be set to 10ms in NewConsumer)
	if cfg.BatchTimeout != 0 {
		t.Errorf("Default BatchTimeout = %v, want 0", cfg.BatchTimeout)
	}
}

func TestConsumerConfig_CustomBatch(t *testing.T) {
	t.Parallel()
	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{GetRefinedTopic("test", TopicBaseTrade)},
		BatchSize:     100,
		BatchTimeout:  20 * time.Millisecond,
	}

	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want 100", cfg.BatchSize)
	}
	if cfg.BatchTimeout != 20*time.Millisecond {
		t.Errorf("BatchTimeout = %v, want 20ms", cfg.BatchTimeout)
	}
}

// =============================================================================
// Consumer Metrics Tests
// =============================================================================

func TestConsumer_GetMetrics_Initial(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	processed, failed, dropped := consumer.GetMetrics()

	if processed != 0 {
		t.Errorf("Initial processed = %d, want 0", processed)
	}
	if failed != 0 {
		t.Errorf("Initial failed = %d, want 0", failed)
	}
	if dropped != 0 {
		t.Errorf("Initial dropped = %d, want 0", dropped)
	}
}

func TestConsumer_IncrementProcessed(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementProcessed()
	consumer.incrementProcessed()
	consumer.incrementProcessed()

	processed, _, _ := consumer.GetMetrics()
	if processed != 3 {
		t.Errorf("processed = %d, want 3", processed)
	}
}

func TestConsumer_IncrementFailed(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementFailed()
	consumer.incrementFailed()

	_, failed, _ := consumer.GetMetrics()
	if failed != 2 {
		t.Errorf("failed = %d, want 2", failed)
	}
}

func TestConsumer_IncrementDropped(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementDropped()
	consumer.incrementDropped()
	consumer.incrementDropped()
	consumer.incrementDropped()

	_, _, dropped := consumer.GetMetrics()
	if dropped != 4 {
		t.Errorf("dropped = %d, want 4", dropped)
	}
}

func TestConsumer_GetDroppedCount(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementDropped()
	consumer.incrementDropped()

	count := consumer.getDroppedCount()
	if count != 2 {
		t.Errorf("getDroppedCount() = %d, want 2", count)
	}
}

func TestConsumer_IncrementBatches(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementBatches()
	consumer.incrementBatches()
	consumer.incrementBatches()

	count := consumer.getBatchCount()
	if count != 3 {
		t.Errorf("getBatchCount() = %d, want 3", count)
	}
}

func TestConsumer_Metrics_Concurrent(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	const numGoroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // 3 types of operations

	// Concurrent incrementProcessed
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				consumer.incrementProcessed()
			}
		}()
	}

	// Concurrent incrementFailed
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				consumer.incrementFailed()
			}
		}()
	}

	// Concurrent incrementDropped
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				consumer.incrementDropped()
			}
		}()
	}

	wg.Wait()

	processed, failed, dropped := consumer.GetMetrics()
	expected := uint64(numGoroutines * opsPerGoroutine)

	if processed != expected {
		t.Errorf("processed = %d, want %d", processed, expected)
	}
	if failed != expected {
		t.Errorf("failed = %d, want %d", failed, expected)
	}
	if dropped != expected {
		t.Errorf("dropped = %d, want %d", dropped, expected)
	}
}

// =============================================================================
// TokenEvent Tests
// =============================================================================

func TestTokenEvent_Fields(t *testing.T) {
	t.Parallel()
	event := TokenEvent{
		Type:      EventTradeExecuted,
		Timestamp: 1234567890,
		Data: map[string]any{
			"price":  "100.50",
			"volume": "1000",
		},
	}

	if event.Type != EventTradeExecuted {
		t.Errorf("Type = %s, want TRADE_EXECUTED", event.Type)
	}
	if event.Timestamp != 1234567890 {
		t.Errorf("Timestamp = %d, want 1234567890", event.Timestamp)
	}
	if event.Data["price"] != "100.50" {
		t.Errorf("Data[price] = %v, want 100.50", event.Data["price"])
	}
}

// =============================================================================
// ReplayMessage Tests
// =============================================================================

func TestReplayMessage_Fields(t *testing.T) {
	t.Parallel()
	msg := ReplayMessage{
		Topic:     GetRefinedTopic("test", TopicBaseTrade),
		Partition: 0,
		Offset:    12345,
		Subject:   "BTC.trade",
		Data:      []byte(`{"price":"100.50"}`),
	}

	if msg.Topic != GetRefinedTopic("test", TopicBaseTrade) {
		t.Errorf("Topic = %s, want %s", msg.Topic, GetRefinedTopic("test", TopicBaseTrade))
	}
	if msg.Partition != 0 {
		t.Errorf("Partition = %d, want 0", msg.Partition)
	}
	if msg.Offset != 12345 {
		t.Errorf("Offset = %d, want 12345", msg.Offset)
	}
	if msg.Subject != "BTC.trade" {
		t.Errorf("Subject = %s, want BTC.trade", msg.Subject)
	}
	if string(msg.Data) != `{"price":"100.50"}` {
		t.Errorf("Data = %s, want {\"price\":\"100.50\"}", msg.Data)
	}
}

// =============================================================================
// BroadcastFunc Tests
// =============================================================================

func TestBroadcastFunc_Signature(t *testing.T) {
	t.Parallel()
	var calls []struct {
		subject string
		message []byte
	}

	var broadcast BroadcastFunc = func(subject string, message []byte) {
		calls = append(calls, struct {
			subject string
			message []byte
		}{subject, message})
	}

	broadcast("BTC.trade", []byte(`{"test":true}`))
	broadcast("ETH.liquidity", []byte(`{"pool":"abc"}`))

	if len(calls) != 2 {
		t.Fatalf("Expected 2 calls, got %d", len(calls))
	}

	if calls[0].subject != "BTC.trade" {
		t.Errorf("calls[0].subject = %s, want BTC.trade", calls[0].subject)
	}

	if calls[1].subject != "ETH.liquidity" {
		t.Errorf("calls[1].subject = %s, want ETH.liquidity", calls[1].subject)
	}
}

// =============================================================================
// ResourceGuard Interface Tests
// =============================================================================

func TestResourceGuard_MockImplementation(t *testing.T) {
	t.Parallel()
	guard := newMockResourceGuard()

	// Default should allow
	allow, duration := guard.AllowKafkaMessage(context.Background())
	if !allow {
		t.Error("Default should allow Kafka messages")
	}
	if duration != 0 {
		t.Errorf("Default wait duration = %v, want 0", duration)
	}

	// Default should not pause
	if guard.ShouldPauseKafka() {
		t.Error("Default should not pause Kafka")
	}

	// Test deny mode
	guard.allowKafka = false
	guard.waitDuration = 100 * time.Millisecond

	allow, duration = guard.AllowKafkaMessage(context.Background())
	if allow {
		t.Error("Should deny when allowKafka=false")
	}
	if duration != 100*time.Millisecond {
		t.Errorf("Wait duration = %v, want 100ms", duration)
	}

	// Test pause mode
	guard.shouldPause = true
	if !guard.ShouldPauseKafka() {
		t.Error("Should pause when shouldPause=true")
	}
}
