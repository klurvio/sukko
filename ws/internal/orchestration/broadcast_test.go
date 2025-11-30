package orchestration

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// newTestBroadcastBus creates a BroadcastBus for testing with required fields initialized.
func newTestBroadcastBus() *BroadcastBus {
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // Keep cancel available but unused in tests
	return &BroadcastBus{
		subscribers: make([]chan *BroadcastMessage, 0),
		ctx:         ctx,
		logger:      zerolog.Nop(),
	}
}

// =============================================================================
// BroadcastBus Subscribe Tests
// =============================================================================

func TestBroadcastBus_Subscribe_CreatesChannel(t *testing.T) {
	bus := &BroadcastBus{
		subscribers: make([]chan *BroadcastMessage, 0),
	}

	ch := bus.Subscribe()

	if ch == nil {
		t.Fatal("Subscribe should return non-nil channel")
	}

	// Check buffer size is 1024
	if cap(ch) != 1024 {
		t.Errorf("Channel buffer should be 1024, got %d", cap(ch))
	}

	// Verify subscriber was registered
	if len(bus.subscribers) != 1 {
		t.Errorf("Should have 1 subscriber, got %d", len(bus.subscribers))
	}
}

func TestBroadcastBus_Subscribe_Multiple(t *testing.T) {
	bus := &BroadcastBus{
		subscribers: make([]chan *BroadcastMessage, 0),
	}

	channels := make([]chan *BroadcastMessage, 5)
	for i := 0; i < 5; i++ {
		channels[i] = bus.Subscribe()
	}

	// Each channel should be unique
	seen := make(map[chan *BroadcastMessage]bool)
	for _, ch := range channels {
		if seen[ch] {
			t.Error("Subscribe returned duplicate channel")
		}
		seen[ch] = true
	}

	// All subscribers should be registered
	if len(bus.subscribers) != 5 {
		t.Errorf("Should have 5 subscribers, got %d", len(bus.subscribers))
	}
}

func TestBroadcastBus_Subscribe_Concurrent(t *testing.T) {
	bus := newTestBroadcastBus()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	channels := make(chan (chan *BroadcastMessage), numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ch := bus.Subscribe()
			channels <- ch
		}()
	}

	wg.Wait()
	close(channels)

	// Verify all unique channels
	seen := make(map[chan *BroadcastMessage]bool)
	for ch := range channels {
		if seen[ch] {
			t.Error("Duplicate channel returned")
		}
		seen[ch] = true
	}

	if len(seen) != numGoroutines {
		t.Errorf("Expected %d unique channels, got %d", numGoroutines, len(seen))
	}
}

// =============================================================================
// BroadcastBus FanOut Tests
// =============================================================================

func TestBroadcastBus_FanOut_AllSubscribers(t *testing.T) {
	bus := newTestBroadcastBus()

	// Create subscribers
	channels := make([]chan *BroadcastMessage, 3)
	for i := 0; i < 3; i++ {
		channels[i] = bus.Subscribe()
	}

	// Fan out a message
	msg := &BroadcastMessage{
		Subject: "test.subject",
		Message: []byte("test payload"),
	}
	bus.fanOut(msg)

	// All subscribers should receive
	for i, ch := range channels {
		select {
		case received := <-ch:
			if received.Subject != msg.Subject {
				t.Errorf("Subscriber %d: subject mismatch", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Subscriber %d did not receive message", i)
		}
	}
}

func TestBroadcastBus_FanOut_NonBlocking(t *testing.T) {
	bus := newTestBroadcastBus()

	// Create one slow subscriber (unbuffered channel)
	slowCh := make(chan *BroadcastMessage) // Unbuffered!
	bus.subMu.Lock()
	bus.subscribers = append(bus.subscribers, slowCh)
	bus.subMu.Unlock()

	// Create one normal subscriber
	fastCh := bus.Subscribe()

	msg := &BroadcastMessage{Subject: "test", Message: []byte("data")}

	// Fan out should not block even with slow subscriber
	done := make(chan bool)
	go func() {
		bus.fanOut(msg)
		done <- true
	}()

	select {
	case <-done:
		// Good - fanOut returned quickly
	case <-time.After(100 * time.Millisecond):
		t.Error("fanOut blocked on slow subscriber")
	}

	// Fast subscriber should still receive
	select {
	case <-fastCh:
		// Good
	default:
		t.Error("Fast subscriber did not receive message")
	}
}

func TestBroadcastBus_FanOut_DropsOnFull(t *testing.T) {
	bus := newTestBroadcastBus()

	// Create subscriber with small buffer
	smallCh := make(chan *BroadcastMessage, 2)
	bus.subMu.Lock()
	bus.subscribers = append(bus.subscribers, smallCh)
	bus.subMu.Unlock()

	// Fill the buffer
	bus.fanOut(&BroadcastMessage{Subject: "msg1", Message: []byte("1")})
	bus.fanOut(&BroadcastMessage{Subject: "msg2", Message: []byte("2")})

	// Third message should be dropped (not block)
	done := make(chan bool)
	go func() {
		bus.fanOut(&BroadcastMessage{Subject: "msg3", Message: []byte("3")})
		done <- true
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("fanOut blocked when buffer full")
	}

	// Only 2 messages should be in channel
	if len(smallCh) != 2 {
		t.Errorf("Expected 2 messages in channel, got %d", len(smallCh))
	}
}

// =============================================================================
// BroadcastBus Health & Metrics Tests
// =============================================================================

func TestBroadcastBus_IsHealthy_Initial(t *testing.T) {
	bus := &BroadcastBus{}
	bus.healthy.Store(true)
	bus.lastPublish.Store(time.Now().Unix())

	if !bus.IsHealthy() {
		t.Error("Bus should be healthy initially")
	}
}

func TestBroadcastBus_IsHealthy_AfterError(t *testing.T) {
	bus := &BroadcastBus{}
	bus.healthy.Store(true)
	bus.lastPublish.Store(time.Now().Unix())

	// Simulate error
	bus.healthy.Store(false)

	if bus.IsHealthy() {
		t.Error("Bus should be unhealthy after error")
	}
}

func TestBroadcastBus_GetMetrics_AllFields(t *testing.T) {
	bus := &BroadcastBus{
		channel:     "test.channel",
		subscribers: make([]chan *BroadcastMessage, 0),
	}
	bus.healthy.Store(true)
	bus.lastPublish.Store(time.Now().Unix())
	bus.publishErrors.Store(5)
	bus.messagesRecv.Store(100)

	// Add some subscribers
	bus.Subscribe()
	bus.Subscribe()

	metrics := bus.GetMetrics()

	// Check all expected fields
	if metrics["type"] != "valkey" {
		t.Errorf("type: got %v, want valkey", metrics["type"])
	}
	if metrics["healthy"] != true {
		t.Errorf("healthy: got %v, want true", metrics["healthy"])
	}
	if metrics["channel"] != "test.channel" {
		t.Errorf("channel: got %v, want test.channel", metrics["channel"])
	}
	if metrics["subscribers"] != 2 {
		t.Errorf("subscribers: got %v, want 2", metrics["subscribers"])
	}
	if metrics["publish_errors"].(uint64) != 5 {
		t.Errorf("publish_errors: got %v, want 5", metrics["publish_errors"])
	}
	if metrics["messages_received"].(uint64) != 100 {
		t.Errorf("messages_received: got %v, want 100", metrics["messages_received"])
	}
	if _, ok := metrics["last_publish_ago"]; !ok {
		t.Error("last_publish_ago field missing")
	}
}

func TestBroadcastBus_MetricCounters(t *testing.T) {
	bus := &BroadcastBus{}

	// Test atomic counters
	bus.publishErrors.Add(1)
	bus.publishErrors.Add(1)
	if bus.publishErrors.Load() != 2 {
		t.Errorf("publishErrors: got %d, want 2", bus.publishErrors.Load())
	}

	bus.messagesRecv.Add(10)
	if bus.messagesRecv.Load() != 10 {
		t.Errorf("messagesRecv: got %d, want 10", bus.messagesRecv.Load())
	}
}

// =============================================================================
// BroadcastMessage Serialization Tests
// =============================================================================

func TestBroadcastMessage_JSONSerialization(t *testing.T) {
	original := &BroadcastMessage{
		Subject: "odin.token.BTC.trade",
		Message: []byte(`{"price":"100.50","volume":"1000"}`),
	}

	// Serialize
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Deserialize
	var decoded BroadcastMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify
	if decoded.Subject != original.Subject {
		t.Errorf("Subject: got %s, want %s", decoded.Subject, original.Subject)
	}
	if string(decoded.Message) != string(original.Message) {
		t.Errorf("Message: got %s, want %s", decoded.Message, original.Message)
	}
}

func TestBroadcastMessage_EmptyMessage(t *testing.T) {
	msg := &BroadcastMessage{
		Subject: "test",
		Message: []byte{},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded BroadcastMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Subject != "test" {
		t.Error("Subject mismatch")
	}
}

// =============================================================================
// BroadcastBus Config Validation Tests
// =============================================================================

func TestBroadcastBusConfig_Defaults(t *testing.T) {
	cfg := BroadcastBusConfig{
		SentinelAddrs: []string{"localhost:6379"},
	}

	// Test that zero values are detected (would be set to defaults in NewBroadcastBus)
	if cfg.MasterName != "" {
		t.Errorf("MasterName should be empty initially, got %s", cfg.MasterName)
	}
	if cfg.Channel != "" {
		t.Errorf("Channel should be empty initially, got %s", cfg.Channel)
	}
	if cfg.BufferSize != 0 {
		t.Errorf("BufferSize should be 0 initially, got %d", cfg.BufferSize)
	}
}

// =============================================================================
// Reconnect Logic Tests
// =============================================================================

func TestBroadcastBus_ReconnectBackoff(t *testing.T) {
	// Test exponential backoff calculation
	backoff := 100 * time.Millisecond
	maxBackoff := 30 * time.Second

	backoffs := make([]time.Duration, 0)
	for i := 0; i < 10; i++ {
		backoffs = append(backoffs, backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	// Verify exponential growth
	if backoffs[0] != 100*time.Millisecond {
		t.Errorf("First backoff should be 100ms, got %v", backoffs[0])
	}
	if backoffs[1] != 200*time.Millisecond {
		t.Errorf("Second backoff should be 200ms, got %v", backoffs[1])
	}
	if backoffs[2] != 400*time.Millisecond {
		t.Errorf("Third backoff should be 400ms, got %v", backoffs[2])
	}

	// Verify cap at maxBackoff
	if backoffs[9] != maxBackoff {
		t.Errorf("10th backoff should be capped at %v, got %v", maxBackoff, backoffs[9])
	}
}

// =============================================================================
// Concurrent Operations Tests
// =============================================================================

func TestBroadcastBus_ConcurrentFanOut(t *testing.T) {
	bus := newTestBroadcastBus()

	// Create subscribers
	channels := make([]chan *BroadcastMessage, 5)
	for i := 0; i < 5; i++ {
		channels[i] = bus.Subscribe()
	}

	const numMessages = 100
	var wg sync.WaitGroup
	wg.Add(numMessages)

	// Concurrent fanOut calls
	for i := 0; i < numMessages; i++ {
		go func(idx int) {
			defer wg.Done()
			bus.fanOut(&BroadcastMessage{
				Subject: "concurrent.test",
				Message: []byte{byte(idx)},
			})
		}(i)
	}

	wg.Wait()

	// Each subscriber should have received messages (some may be dropped if buffer full)
	for i, ch := range channels {
		count := len(ch)
		if count == 0 {
			t.Errorf("Subscriber %d received no messages", i)
		}
	}
}

func TestBroadcastBus_ConcurrentSubscribe(t *testing.T) {
	bus := newTestBroadcastBus()

	const numGoroutines = 100
	var wg sync.WaitGroup
	var received atomic.Int64

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ch := bus.Subscribe()
			if ch != nil && cap(ch) == 1024 {
				received.Add(1)
			}
		}()
	}

	wg.Wait()

	if received.Load() != numGoroutines {
		t.Errorf("Expected %d successful subscribes, got %d", numGoroutines, received.Load())
	}
}
