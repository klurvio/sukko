package orchestration

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Toniq-Labs/odin-ws/internal/broadcast"
)

// =============================================================================
// KafkaPoolMetrics Tests
// =============================================================================

func TestKafkaPoolMetrics_Fields(t *testing.T) {
	t.Parallel()
	metrics := KafkaPoolMetrics{
		MessagesRouted:  100,
		MessagesDropped: 5,
		RoutingErrors:   2,
	}

	if metrics.MessagesRouted != 100 {
		t.Errorf("MessagesRouted: got %d, want 100", metrics.MessagesRouted)
	}
	if metrics.MessagesDropped != 5 {
		t.Errorf("MessagesDropped: got %d, want 5", metrics.MessagesDropped)
	}
	if metrics.RoutingErrors != 2 {
		t.Errorf("RoutingErrors: got %d, want 2", metrics.RoutingErrors)
	}
}

// =============================================================================
// Subject Formatting Tests
// =============================================================================

func TestKafkaPool_SubjectFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		subject   string
		eventType string
		expected  string
	}{
		{"BTC", "trade", "BTC.trade"},
		{"ETH", "orderbook", "ETH.orderbook"},
		{"SOL", "ticker", "SOL.ticker"},
		{"DOGE", "trade", "DOGE.trade"},
	}

	for _, tt := range tests {
		t.Run(tt.subject+"_"+tt.eventType, func(t *testing.T) {
			t.Parallel()
			result := fmt.Sprintf("%s.%s", tt.subject, tt.eventType)
			if result != tt.expected {
				t.Errorf("Subject: got %s, want %s", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Metrics Atomic Counter Tests
// =============================================================================

func TestKafkaPool_AtomicCounters(t *testing.T) {
	t.Parallel()
	var messagesRouted atomic.Uint64
	var messagesDropped atomic.Uint64
	var routingErrors atomic.Uint64

	// Test increments
	messagesRouted.Add(1)
	messagesRouted.Add(1)
	if messagesRouted.Load() != 2 {
		t.Errorf("messagesRouted: got %d, want 2", messagesRouted.Load())
	}

	messagesDropped.Add(5)
	if messagesDropped.Load() != 5 {
		t.Errorf("messagesDropped: got %d, want 5", messagesDropped.Load())
	}

	routingErrors.Add(3)
	if routingErrors.Load() != 3 {
		t.Errorf("routingErrors: got %d, want 3", routingErrors.Load())
	}
}

func TestKafkaPool_AtomicCounters_Concurrent(t *testing.T) {
	t.Parallel()
	var counter atomic.Uint64
	const numGoroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				counter.Add(1)
			}
		}()
	}

	wg.Wait()

	expected := uint64(numGoroutines * opsPerGoroutine)
	if counter.Load() != expected {
		t.Errorf("Counter: got %d, want %d", counter.Load(), expected)
	}
}

// =============================================================================
// RouteMessage Behavior Tests (without real Kafka)
// =============================================================================

func TestKafkaPool_RouteMessage_SubjectCreation(t *testing.T) {
	t.Parallel()
	// Simulate the routeMessage logic
	subject := "BTC"
	eventType := "trade"
	message := []byte(`{"price":"100.50"}`)

	broadcastSubject := fmt.Sprintf("%s.%s", subject, eventType)
	broadcastMsg := &broadcast.Message{
		Subject: broadcastSubject,
		Payload: message,
	}

	// Verify subject format
	if broadcastMsg.Subject != "BTC.trade" {
		t.Errorf("Subject: got %s, want BTC.trade", broadcastMsg.Subject)
	}

	// Verify message is preserved
	if string(broadcastMsg.Payload) != `{"price":"100.50"}` {
		t.Errorf("Payload: got %s, want {\"price\":\"100.50\"}", broadcastMsg.Payload)
	}
}

func TestKafkaPool_RouteMessage_MultipleSubjects(t *testing.T) {
	t.Parallel()
	entities := []struct {
		subject   string
		eventType string
	}{
		{"BTC", "trade"},
		{"ETH", "orderbook"},
		{"SOL", "ticker"},
	}

	messages := make([]*broadcast.Message, 0)

	for _, entity := range entities {
		broadcastSubject := fmt.Sprintf("%s.%s", entity.subject, entity.eventType)
		msg := &broadcast.Message{
			Subject: broadcastSubject,
			Payload: []byte(`{}`),
		}
		messages = append(messages, msg)
	}

	// Verify all messages have unique subjects
	subjects := make(map[string]bool)
	for _, msg := range messages {
		if subjects[msg.Subject] {
			t.Errorf("Duplicate subject: %s", msg.Subject)
		}
		subjects[msg.Subject] = true
	}

	if len(subjects) != 3 {
		t.Errorf("Expected 3 unique subjects, got %d", len(subjects))
	}
}

// =============================================================================
// GetMetrics Tests
// =============================================================================

func TestKafkaPool_GetMetrics_Structure(t *testing.T) {
	t.Parallel()
	// Test the metrics structure
	pool := &KafkaConsumerPool{}

	// Set some metrics
	pool.messagesRouted.Store(1000)
	pool.messagesDropped.Store(50)
	pool.routingErrors.Store(10)

	metrics := pool.GetMetrics()

	if metrics.MessagesRouted != 1000 {
		t.Errorf("MessagesRouted: got %d, want 1000", metrics.MessagesRouted)
	}
	if metrics.MessagesDropped != 50 {
		t.Errorf("MessagesDropped: got %d, want 50", metrics.MessagesDropped)
	}
	if metrics.RoutingErrors != 10 {
		t.Errorf("RoutingErrors: got %d, want 10", metrics.RoutingErrors)
	}
}

func TestKafkaPool_GetMetrics_Initial(t *testing.T) {
	t.Parallel()
	pool := &KafkaConsumerPool{}

	metrics := pool.GetMetrics()

	// Initial metrics should all be zero
	if metrics.MessagesRouted != 0 {
		t.Errorf("Initial MessagesRouted: got %d, want 0", metrics.MessagesRouted)
	}
	if metrics.MessagesDropped != 0 {
		t.Errorf("Initial MessagesDropped: got %d, want 0", metrics.MessagesDropped)
	}
	if metrics.RoutingErrors != 0 {
		t.Errorf("Initial RoutingErrors: got %d, want 0", metrics.RoutingErrors)
	}
}

func TestKafkaPool_GetMetrics_Concurrent(t *testing.T) {
	t.Parallel()
	pool := &KafkaConsumerPool{}

	const numReaders = 50
	const numWriters = 50
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(numReaders + numWriters)

	// Concurrent writers
	for range numWriters {
		go func() {
			defer wg.Done()
			for range ops {
				pool.messagesRouted.Add(1)
			}
		}()
	}

	// Concurrent readers
	for range numReaders {
		go func() {
			defer wg.Done()
			for range ops {
				_ = pool.GetMetrics()
			}
		}()
	}

	wg.Wait()

	// Verify final count
	metrics := pool.GetMetrics()
	expected := uint64(numWriters * ops)
	if metrics.MessagesRouted != expected {
		t.Errorf("MessagesRouted: got %d, want %d", metrics.MessagesRouted, expected)
	}
}

// =============================================================================
// KafkaPoolConfig Tests
// =============================================================================

func TestKafkaPoolConfig_Fields(t *testing.T) {
	t.Parallel()
	cfg := KafkaPoolConfig{
		Brokers:       []string{"localhost:9092", "localhost:9093"},
		ConsumerGroup: "test-group",
	}

	if len(cfg.Brokers) != 2 {
		t.Errorf("Brokers length: got %d, want 2", len(cfg.Brokers))
	}
	if cfg.ConsumerGroup != "test-group" {
		t.Errorf("ConsumerGroup: got %s, want test-group", cfg.ConsumerGroup)
	}
}
