package kafka

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	kafkautil "github.com/klurvio/sukko/internal/shared/kafka"
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

func (m *mockResourceGuard) AllowKafkaMessage(_ context.Context) (bool, time.Duration) {
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
	broadcast := func(_ string, _ []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{},
		ConsumerGroup: "test-group",
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
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
	broadcast := func(_ string, _ []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "",
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
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
	broadcast := func(_ string, _ []byte) {}

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
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
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
	broadcast := func(_ string, _ []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
		Logger:        &logger,
		Broadcast:     broadcast,
		ResourceGuard: nil,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no resource guard")
	}
}

func TestNewConsumer_NoLogger(t *testing.T) {
	t.Parallel()
	guard := newMockResourceGuard()
	broadcast := func(_ string, _ []byte) {}

	cfg := ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
		Logger:        nil,
		Broadcast:     broadcast,
		ResourceGuard: guard,
	}

	_, err := NewConsumer(cfg)
	if err == nil {
		t.Error("NewConsumer should fail with no logger")
	}
}

// =============================================================================
// ConsumerGroup Field Tests
// =============================================================================

func TestConsumer_ConsumerGroupField(t *testing.T) {
	t.Parallel()
	// Verify consumerGroup is stored on the struct for metrics labels.
	// NewConsumer() requires a real Kafka broker, so we test the field directly.
	consumer := &Consumer{consumerGroup: "sukko-shared-dev"}

	if consumer.consumerGroup != "sukko-shared-dev" {
		t.Errorf("consumerGroup = %q, want %q", consumer.consumerGroup, "sukko-shared-dev")
	}
}

func TestConsumer_ConsumerGroupField_Empty(t *testing.T) {
	t.Parallel()
	// Empty consumer group should not panic (metrics will use empty string label)
	consumer := &Consumer{consumerGroup: ""}

	if consumer.consumerGroup != "" {
		t.Errorf("consumerGroup = %q, want empty", consumer.consumerGroup)
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
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
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
		Topics:        []string{kafkautil.BuildTopicName("test", "sukko", "trade")},
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

	consumer.incrementProcessed("test-topic")
	consumer.incrementProcessed("test-topic")
	consumer.incrementProcessed("test-topic")

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

	consumer.incrementDropped("test-topic")
	consumer.incrementDropped("test-topic")
	consumer.incrementDropped("test-topic")
	consumer.incrementDropped("test-topic")

	_, _, dropped := consumer.GetMetrics()
	if dropped != 4 {
		t.Errorf("dropped = %d, want 4", dropped)
	}
}

func TestConsumer_GetDroppedCount(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementDropped("test-topic")
	consumer.incrementDropped("test-topic")

	count := consumer.getDroppedCount()
	if count != 2 {
		t.Errorf("getDroppedCount() = %d, want 2", count)
	}
}

func TestConsumer_IncrementProcessed_MultipleTopics(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementProcessed("sukko.dev.trade")
	consumer.incrementProcessed("sukko.dev.liquidity")
	consumer.incrementProcessed("sukko.dev.trade")

	processed, _, _ := consumer.GetMetrics()
	if processed != 3 {
		t.Errorf("processed = %d, want 3", processed)
	}
}

func TestConsumer_IncrementDropped_MultipleTopics(t *testing.T) {
	t.Parallel()
	consumer := &Consumer{}

	consumer.incrementDropped("sukko.dev.trade")
	consumer.incrementDropped("sukko.dev.liquidity")

	_, _, dropped := consumer.GetMetrics()
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
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
				consumer.incrementProcessed("test-topic")
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
				consumer.incrementDropped("test-topic")
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
// PrepareMessage Tests
// =============================================================================

func TestConsumer_PrepareMessage_IncludesTopic(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()

	consumer := &Consumer{
		logger:        &logger,
		resourceGuard: guard,
		ctx:           context.Background(),
		consumerGroup: "sukko-shared-dev",
	}

	record := &kgo.Record{
		Topic: "sukko.dev.trade",
		Key:   []byte("BTC.trade"),
		Value: []byte(`{"price":"50000"}`),
	}

	msg := consumer.prepareMessage(record)
	if msg == nil {
		t.Fatal("prepareMessage returned nil")
	}

	if msg.topic != "sukko.dev.trade" {
		t.Errorf("msg.topic = %q, want %q", msg.topic, "sukko.dev.trade")
	}
	if msg.subject != "BTC.trade" {
		t.Errorf("msg.subject = %q, want %q", msg.subject, "BTC.trade")
	}
	if string(msg.message) != `{"price":"50000"}` {
		t.Errorf("msg.message = %q, want %q", msg.message, `{"price":"50000"}`)
	}
}

func TestConsumer_PrepareMessage_EmptyKey_ReturnNil(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()

	consumer := &Consumer{
		logger:        &logger,
		resourceGuard: guard,
		ctx:           context.Background(),
		consumerGroup: "test-group",
	}

	record := &kgo.Record{
		Topic: "sukko.dev.trade",
		Key:   []byte(""),
		Value: []byte(`{"price":"50000"}`),
	}

	msg := consumer.prepareMessage(record)
	if msg != nil {
		t.Error("prepareMessage should return nil for empty key")
	}

	_, failed, _ := consumer.GetMetrics()
	if failed != 1 {
		t.Errorf("failed = %d, want 1 (should increment on empty key)", failed)
	}
}

func TestConsumer_PrepareMessage_RateLimited_DropsWithTopic(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()
	guard := newMockResourceGuard()
	guard.allowKafka = false

	consumer := &Consumer{
		logger:        &logger,
		resourceGuard: guard,
		ctx:           context.Background(),
		consumerGroup: "test-group",
	}

	record := &kgo.Record{
		Topic: "sukko.dev.trade",
		Key:   []byte("BTC.trade"),
		Value: []byte(`{"price":"50000"}`),
	}

	msg := consumer.prepareMessage(record)
	if msg != nil {
		t.Error("prepareMessage should return nil when rate limited")
	}

	_, _, dropped := consumer.GetMetrics()
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

// =============================================================================
// TokenEvent Tests
// =============================================================================

func TestTokenEvent_Fields(t *testing.T) {
	t.Parallel()
	event := TokenEvent{
		Type:      kafkautil.EventTradeExecuted,
		Timestamp: 1234567890,
		Data: map[string]any{
			"price":  "100.50",
			"volume": "1000",
		},
	}

	if event.Type != kafkautil.EventTradeExecuted {
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
		Topic:     kafkautil.BuildTopicName("test", "sukko", "trade"),
		Partition: 0,
		Offset:    12345,
		Subject:   "BTC.trade",
		Data:      []byte(`{"price":"100.50"}`),
	}

	if msg.Topic != kafkautil.BuildTopicName("test", "sukko", "trade") {
		t.Errorf("Topic = %s, want %s", msg.Topic, kafkautil.BuildTopicName("test", "sukko", "trade"))
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
// PauseFetchTopics Tests
// =============================================================================

func TestConsumer_PauseFetchTopics_EmptyTopics(t *testing.T) {
	t.Parallel()
	// Empty topics should be a no-op and not panic with nil client
	consumer := &Consumer{}
	consumer.PauseFetchTopics()
}

func TestConsumer_PauseFetchTopics_EmptySlice(t *testing.T) {
	t.Parallel()
	// Explicit empty slice should also be a no-op
	consumer := &Consumer{}
	consumer.PauseFetchTopics([]string{}...)
}

// =============================================================================
// AddConsumeTopics Tests
// =============================================================================

func TestConsumer_AddConsumeTopics_EmptyTopics(t *testing.T) {
	t.Parallel()
	// Empty topics should be a no-op and not panic with nil client
	consumer := &Consumer{}
	consumer.AddConsumeTopics()
}

func TestConsumer_AddConsumeTopics_EmptySlice(t *testing.T) {
	t.Parallel()
	// Explicit empty slice should also be a no-op
	consumer := &Consumer{}
	consumer.AddConsumeTopics([]string{}...)
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
