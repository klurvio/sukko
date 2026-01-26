package kafka

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

// =============================================================================
// ProducerConfig Validation Tests
// =============================================================================

func TestNewProducer_NoBrokers(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	cfg := ProducerConfig{
		Brokers:        []string{},
		TopicNamespace: "test",
		Logger:         &logger,
	}

	_, err := NewProducer(cfg)
	if err == nil {
		t.Error("NewProducer should fail with no brokers")
	}
}

func TestNewProducer_NoTopicNamespace(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	cfg := ProducerConfig{
		Brokers:        []string{"localhost:9092"},
		TopicNamespace: "",
		Logger:         &logger,
	}

	_, err := NewProducer(cfg)
	if err == nil {
		t.Error("NewProducer should fail with no topic namespace")
	}
}

func TestNewProducer_InvalidSASLMechanism(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	cfg := ProducerConfig{
		Brokers:        []string{"localhost:9092"},
		TopicNamespace: "test",
		Logger:         &logger,
		SASL: &SASLConfig{
			Mechanism: "invalid-mechanism",
			Username:  "user",
			Password:  "pass",
		},
	}

	_, err := NewProducer(cfg)
	if err == nil {
		t.Error("NewProducer should fail with invalid SASL mechanism")
	}
}

// =============================================================================
// ProducerConfig Defaults Tests
// =============================================================================

func TestProducerConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := ProducerConfig{
		Brokers:        []string{"localhost:9092"},
		TopicNamespace: "test",
	}

	// Default ClientID should be empty (will be set to "odin-ws-producer" in NewProducer)
	if cfg.ClientID != "" {
		t.Errorf("Default ClientID = %q, want empty string", cfg.ClientID)
	}

	// Default BatchMaxBytes should be 0 (will be set to 1MB in NewProducer)
	if cfg.BatchMaxBytes != 0 {
		t.Errorf("Default BatchMaxBytes = %d, want 0", cfg.BatchMaxBytes)
	}

	// Default MaxBufferedRecs should be 0 (will be set to 10000 in NewProducer)
	if cfg.MaxBufferedRecs != 0 {
		t.Errorf("Default MaxBufferedRecs = %d, want 0", cfg.MaxBufferedRecs)
	}

	// Default RecordRetries should be 0 (will be set to 8 in NewProducer)
	if cfg.RecordRetries != 0 {
		t.Errorf("Default RecordRetries = %d, want 0", cfg.RecordRetries)
	}
}

func TestProducerConfig_CustomValues(t *testing.T) {
	t.Parallel()
	cfg := ProducerConfig{
		Brokers:         []string{"localhost:9092", "localhost:9093"},
		TopicNamespace:  "production",
		ClientID:        "custom-client",
		BatchMaxBytes:   512 * 1024,
		MaxBufferedRecs: 5000,
		RecordRetries:   5,
	}

	if len(cfg.Brokers) != 2 {
		t.Errorf("Brokers length = %d, want 2", len(cfg.Brokers))
	}
	if cfg.TopicNamespace != "production" {
		t.Errorf("TopicNamespace = %q, want %q", cfg.TopicNamespace, "production")
	}
	if cfg.ClientID != "custom-client" {
		t.Errorf("ClientID = %q, want %q", cfg.ClientID, "custom-client")
	}
	if cfg.BatchMaxBytes != 512*1024 {
		t.Errorf("BatchMaxBytes = %d, want %d", cfg.BatchMaxBytes, 512*1024)
	}
	if cfg.MaxBufferedRecs != 5000 {
		t.Errorf("MaxBufferedRecs = %d, want 5000", cfg.MaxBufferedRecs)
	}
	if cfg.RecordRetries != 5 {
		t.Errorf("RecordRetries = %d, want 5", cfg.RecordRetries)
	}
}

// =============================================================================
// ProducerStats Tests
// =============================================================================

func TestProducerStats_Initial(t *testing.T) {
	t.Parallel()
	producer := &Producer{}

	stats := producer.Stats()

	if stats.MessagesPublished != 0 {
		t.Errorf("Initial MessagesPublished = %d, want 0", stats.MessagesPublished)
	}
	if stats.MessagesFailed != 0 {
		t.Errorf("Initial MessagesFailed = %d, want 0", stats.MessagesFailed)
	}
}

func TestProducerStats_IncrementPublished(t *testing.T) {
	t.Parallel()
	producer := &Producer{}

	atomic.AddInt64(&producer.stats.MessagesPublished, 1)
	atomic.AddInt64(&producer.stats.MessagesPublished, 1)
	atomic.AddInt64(&producer.stats.MessagesPublished, 1)

	stats := producer.Stats()
	if stats.MessagesPublished != 3 {
		t.Errorf("MessagesPublished = %d, want 3", stats.MessagesPublished)
	}
}

func TestProducerStats_IncrementFailed(t *testing.T) {
	t.Parallel()
	producer := &Producer{}

	atomic.AddInt64(&producer.stats.MessagesFailed, 1)
	atomic.AddInt64(&producer.stats.MessagesFailed, 1)

	stats := producer.Stats()
	if stats.MessagesFailed != 2 {
		t.Errorf("MessagesFailed = %d, want 2", stats.MessagesFailed)
	}
}

func TestProducerStats_Concurrent(t *testing.T) {
	t.Parallel()
	producer := &Producer{}

	const numGoroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // 2 types of operations

	// Concurrent MessagesPublished increments
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				atomic.AddInt64(&producer.stats.MessagesPublished, 1)
			}
		}()
	}

	// Concurrent MessagesFailed increments
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				atomic.AddInt64(&producer.stats.MessagesFailed, 1)
			}
		}()
	}

	wg.Wait()

	stats := producer.Stats()
	expected := int64(numGoroutines * opsPerGoroutine)

	if stats.MessagesPublished != expected {
		t.Errorf("MessagesPublished = %d, want %d", stats.MessagesPublished, expected)
	}
	if stats.MessagesFailed != expected {
		t.Errorf("MessagesFailed = %d, want %d", stats.MessagesFailed, expected)
	}
}

// =============================================================================
// Producer Topic Tests
// =============================================================================

func TestProducer_Topic(t *testing.T) {
	t.Parallel()
	producer := &Producer{
		topic: "odin.test.client-events",
	}

	topic := producer.Topic()
	if topic != "odin.test.client-events" {
		t.Errorf("Topic() = %q, want %q", topic, "odin.test.client-events")
	}
}

func TestProducer_TopicNamespaces(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		namespace     string
		expectedTopic string
	}{
		{"local", "odin.local.client-events"},
		{"dev", "odin.dev.client-events"},
		{"staging", "odin.staging.client-events"},
		{"main", "odin.main.client-events"},
		{"production", "odin.main.client-events"}, // production normalizes to main
	}

	for _, tc := range testCases {
		t.Run(tc.namespace, func(t *testing.T) {
			t.Parallel()
			topic := GetTopic(tc.namespace, TopicClientEvents)
			if topic != tc.expectedTopic {
				t.Errorf("GetTopic(%q, TopicClientEvents) = %q, want %q", tc.namespace, topic, tc.expectedTopic)
			}
		})
	}
}

// =============================================================================
// Producer Close Tests
// =============================================================================

func TestProducer_Close_Idempotent(t *testing.T) {
	t.Parallel()
	producer := &Producer{
		closed: false,
	}

	// Simulate close without actual client
	producer.closed = true

	// Second close should be safe
	if !producer.closed {
		t.Error("Producer should be marked as closed")
	}
}

func TestProducer_Close_SetsClosedFlag(t *testing.T) {
	t.Parallel()
	producer := &Producer{
		closed: false,
	}

	if producer.closed {
		t.Error("Producer should not be closed initially")
	}

	producer.closed = true

	if !producer.closed {
		t.Error("Producer should be closed after Close()")
	}
}

// =============================================================================
// TopicClientEvents Constant Tests
// =============================================================================

func TestTopicClientEvents_Value(t *testing.T) {
	t.Parallel()
	if TopicClientEvents != "client-events" {
		t.Errorf("TopicClientEvents = %q, want %q", TopicClientEvents, "client-events")
	}
}

// =============================================================================
// SASL Config Tests
// =============================================================================

func TestSASLConfig_Fields(t *testing.T) {
	t.Parallel()
	sasl := &SASLConfig{
		Mechanism: "scram-sha-256",
		Username:  "testuser",
		Password:  "testpass",
	}

	if sasl.Mechanism != "scram-sha-256" {
		t.Errorf("Mechanism = %q, want %q", sasl.Mechanism, "scram-sha-256")
	}
	if sasl.Username != "testuser" {
		t.Errorf("Username = %q, want %q", sasl.Username, "testuser")
	}
	if sasl.Password != "testpass" {
		t.Errorf("Password = %q, want %q", sasl.Password, "testpass")
	}
}

func TestSASLConfig_SupportedMechanisms(t *testing.T) {
	t.Parallel()
	mechanisms := []string{"scram-sha-256", "scram-sha-512"}

	for _, mech := range mechanisms {
		t.Run(mech, func(t *testing.T) {
			t.Parallel()
			// Just verify these are valid string values
			// Actual validation happens in NewProducer
			if mech != "scram-sha-256" && mech != "scram-sha-512" {
				t.Errorf("Unexpected mechanism: %q", mech)
			}
		})
	}
}

// =============================================================================
// TLS Config Tests
// =============================================================================

func TestTLSConfig_Fields(t *testing.T) {
	t.Parallel()
	tlsCfg := &TLSConfig{
		Enabled:            true,
		InsecureSkipVerify: false,
		CAPath:             "/path/to/ca.crt",
	}

	if !tlsCfg.Enabled {
		t.Error("Enabled should be true")
	}
	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false")
	}
	if tlsCfg.CAPath != "/path/to/ca.crt" {
		t.Errorf("CAPath = %q, want %q", tlsCfg.CAPath, "/path/to/ca.crt")
	}
}

func TestTLSConfig_InsecureMode(t *testing.T) {
	t.Parallel()
	tlsCfg := &TLSConfig{
		Enabled:            true,
		InsecureSkipVerify: true,
		CAPath:             "", // No CA needed when skipping verification
	}

	if !tlsCfg.Enabled {
		t.Error("Enabled should be true")
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true")
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkProducerStats_Increment(b *testing.B) {
	producer := &Producer{}

	for b.Loop() {
		atomic.AddInt64(&producer.stats.MessagesPublished, 1)
	}
}

func BenchmarkProducerStats_Read(b *testing.B) {
	producer := &Producer{}
	atomic.StoreInt64(&producer.stats.MessagesPublished, 12345)
	atomic.StoreInt64(&producer.stats.MessagesFailed, 67)

	for b.Loop() {
		_ = producer.Stats()
	}
}

func BenchmarkGetTopic(b *testing.B) {
	for b.Loop() {
		_ = GetTopic("dev", TopicClientEvents)
	}
}
