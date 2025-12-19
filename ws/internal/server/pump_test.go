package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adred-codev/odin-ws/internal/types"
	"github.com/rs/zerolog"
)

// =============================================================================
// PumpConfig Tests
// =============================================================================

func TestDefaultPumpConfig(t *testing.T) {
	config := DefaultPumpConfig()

	if config.PongWait != 30*time.Second {
		t.Errorf("PongWait: got %v, want 30s", config.PongWait)
	}
	if config.WriteWait != 5*time.Second {
		t.Errorf("WriteWait: got %v, want 5s", config.WriteWait)
	}
	if config.PingPeriod != 27*time.Second {
		t.Errorf("PingPeriod: got %v, want 27s", config.PingPeriod)
	}
}

func TestPumpConfig_PingLessThanPong(t *testing.T) {
	config := DefaultPumpConfig()

	if config.PingPeriod >= config.PongWait {
		t.Errorf("PingPeriod (%v) should be less than PongWait (%v)", config.PingPeriod, config.PongWait)
	}
}

// =============================================================================
// NewPump Tests
// =============================================================================

func TestNewPump_NilDependencies(t *testing.T) {
	config := DefaultPumpConfig()
	stats := &types.Stats{}

	pump := NewPump(config, nil, zerolog.Logger{}, nil, nil, stats, nil)

	if pump == nil {
		t.Fatal("NewPump returned nil")
	}
	if pump.Stats != stats { //nolint:staticcheck // SA5011 false positive - t.Fatal stops execution
		t.Error("Stats not set correctly")
	}
}

func TestNewPump_AllDependencies(t *testing.T) {
	config := DefaultPumpConfig()
	mockLogger := newTestMockLogger()
	mockRateLimiter := newTestMockRateLimiter()
	mockAuditLogger := newTestMockAuditLogger()
	mockClock := newTestMockClock()
	stats := &types.Stats{}

	pump := NewPump(
		config,
		mockLogger,
		zerolog.Logger{},
		mockRateLimiter,
		mockAuditLogger,
		stats,
		mockClock,
	)

	if pump == nil {
		t.Fatal("NewPump returned nil")
	}
	if pump.Logger == nil {
		t.Error("Logger not set")
	}
	if pump.RateLimiter == nil {
		t.Error("RateLimiter not set")
	}
	if pump.AuditLogger == nil {
		t.Error("AuditLogger not set")
	}
	if pump.Clock == nil {
		t.Error("Clock not set")
	}
}

// =============================================================================
// CreateRateLimitErrorMessage Tests
// =============================================================================

func TestCreateRateLimitErrorMessage_Format(t *testing.T) {
	msg := CreateRateLimitErrorMessage()

	var parsed map[string]any
	if err := json.Unmarshal(msg, &parsed); err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	if parsed["type"] != "error" {
		t.Errorf("type: got %v, want error", parsed["type"])
	}
	if parsed["code"] != "RATE_LIMIT_EXCEEDED" {
		t.Errorf("code: got %v, want RATE_LIMIT_EXCEEDED", parsed["code"])
	}
	if _, ok := parsed["message"]; !ok {
		t.Error("message field missing")
	}
}

func TestCreateRateLimitErrorMessage_Consistency(t *testing.T) {
	msg1 := CreateRateLimitErrorMessage()
	msg2 := CreateRateLimitErrorMessage()

	if string(msg1) != string(msg2) {
		t.Error("CreateRateLimitErrorMessage should return consistent output")
	}
}

// =============================================================================
// Pump.handleRateLimitExceeded Tests
// =============================================================================

func TestPump_HandleRateLimitExceeded_LogsWarning(t *testing.T) {
	mockLogger := newTestMockLogger()
	mockAuditLogger := newTestMockAuditLogger()
	stats := &types.Stats{}

	pump := &Pump{
		Logger:      mockLogger,
		AuditLogger: mockAuditLogger,
		Stats:       stats,
	}

	client := &Client{
		id:   12345,
		send: make(chan []byte, 10),
	}

	pump.handleRateLimitExceeded(client)

	if mockLogger.messageCount() == 0 {
		t.Error("Expected log message for rate limit")
	}

	messages := mockLogger.getMessages()
	foundWarning := false
	for _, msg := range messages {
		if msg.level == "warn" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("Expected warning level log")
	}
}

func TestPump_HandleRateLimitExceeded_AuditLog(t *testing.T) {
	mockLogger := newTestMockLogger()
	mockAuditLogger := newTestMockAuditLogger()
	stats := &types.Stats{}

	pump := &Pump{
		Logger:      mockLogger,
		AuditLogger: mockAuditLogger,
		Stats:       stats,
	}

	client := &Client{
		id:   12345,
		send: make(chan []byte, 10),
	}

	pump.handleRateLimitExceeded(client)

	if mockAuditLogger.eventCount() == 0 {
		t.Error("Expected audit log event")
	}

	if !mockAuditLogger.hasEvent("ClientRateLimited") {
		t.Error("Expected ClientRateLimited audit event")
	}
}

func TestPump_HandleRateLimitExceeded_SendsError(t *testing.T) {
	mockLogger := newTestMockLogger()
	stats := &types.Stats{}

	pump := &Pump{
		Logger: mockLogger,
		Stats:  stats,
	}

	client := &Client{
		id:   12345,
		send: make(chan []byte, 10),
	}

	pump.handleRateLimitExceeded(client)

	select {
	case msg := <-client.send:
		var parsed map[string]any
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("Failed to parse sent message: %v", err)
		}
		if parsed["code"] != "RATE_LIMIT_EXCEEDED" {
			t.Errorf("Expected RATE_LIMIT_EXCEEDED, got %v", parsed["code"])
		}
	default:
		t.Error("Expected error message to be sent to client")
	}
}

func TestPump_HandleRateLimitExceeded_FullBuffer(t *testing.T) {
	mockLogger := newTestMockLogger()
	stats := &types.Stats{}

	pump := &Pump{
		Logger: mockLogger,
		Stats:  stats,
	}

	client := &Client{
		id:   12345,
		send: make(chan []byte, 1),
	}
	client.send <- []byte("existing message")

	done := make(chan bool)
	go func() {
		pump.handleRateLimitExceeded(client)
		done <- true
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("handleRateLimitExceeded blocked on full buffer")
	}
}

func TestPump_HandleRateLimitExceeded_UpdatesStats(t *testing.T) {
	mockLogger := newTestMockLogger()
	stats := &types.Stats{}

	pump := &Pump{
		Logger: mockLogger,
		Stats:  stats,
	}

	client := &Client{
		id:   12345,
		send: make(chan []byte, 10),
	}

	initialCount := stats.RateLimitedMessages

	pump.handleRateLimitExceeded(client)

	if stats.RateLimitedMessages != initialCount+1 {
		t.Errorf("RateLimitedMessages: got %d, want %d", stats.RateLimitedMessages, initialCount+1)
	}
}

// =============================================================================
// Pump.now Tests
// =============================================================================

func TestPump_Now_WithMockClock(t *testing.T) {
	fixedTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mockClock := newTestMockClock(fixedTime)

	pump := &Pump{Clock: mockClock}

	result := pump.now()

	if !result.Equal(fixedTime) {
		t.Errorf("now(): got %v, want %v", result, fixedTime)
	}
}

func TestPump_Now_WithNilClock(t *testing.T) {
	pump := &Pump{Clock: nil}

	before := time.Now()
	result := pump.now()
	after := time.Now()

	if result.Before(before) || result.After(after) {
		t.Errorf("now() without clock should return current time")
	}
}

func TestPump_Now_ClockAdvance(t *testing.T) {
	mockClock := newTestMockClock()
	pump := &Pump{Clock: mockClock}

	t1 := pump.now()
	mockClock.advance(5 * time.Second)
	t2 := pump.now()

	diff := t2.Sub(t1)
	if diff != 5*time.Second {
		t.Errorf("Time difference: got %v, want 5s", diff)
	}
}

// =============================================================================
// Pump.newTicker Tests
// =============================================================================

func TestPump_NewTicker_WithMockClock(t *testing.T) {
	mockClock := newTestMockClock()
	pump := &Pump{Clock: mockClock}

	ticker := pump.newTicker(1 * time.Second)

	if ticker == nil {
		t.Fatal("newTicker returned nil")
	}

	if ticker.C() == nil {
		t.Error("Ticker channel is nil")
	}

	ticker.Stop()
}

func TestPump_NewTicker_WithNilClock(t *testing.T) {
	pump := &Pump{Clock: nil}

	ticker := pump.newTicker(1 * time.Second)

	if ticker == nil {
		t.Fatal("newTicker returned nil")
	}

	ticker.Stop()
}

// =============================================================================
// Adapter Tests
// =============================================================================

func TestZerologAdapter_ImplementsLogger(t *testing.T) {
	var _ Logger = (*ZerologAdapter)(nil)
}

func TestAuditLoggerAdapter_ImplementsAuditLogger(t *testing.T) {
	var _ AuditLogger = (*AuditLoggerAdapter)(nil)
}

func TestRateLimiterAdapter_ImplementsRateLimiter(t *testing.T) {
	var _ RateLimiter = (*RateLimiterAdapter)(nil)
}

func TestRealClock_ImplementsClock(t *testing.T) {
	var _ Clock = (*RealClock)(nil)
}

func TestRealTicker_ImplementsTicker(t *testing.T) {
	var _ Ticker = (*RealTicker)(nil)
}

// =============================================================================
// RealClock Tests
// =============================================================================

func TestRealClock_Now(t *testing.T) {
	clock := &RealClock{}

	before := time.Now()
	result := clock.Now()
	after := time.Now()

	if result.Before(before) || result.After(after) {
		t.Error("RealClock.Now() should return current time")
	}
}

func TestRealClock_NewTicker(t *testing.T) {
	clock := &RealClock{}

	ticker := clock.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	if ticker == nil {
		t.Fatal("NewTicker returned nil")
	}

	select {
	case <-ticker.C():
		// Good
	case <-time.After(200 * time.Millisecond):
		t.Error("Ticker did not fire")
	}
}

func TestRealClock_After(t *testing.T) {
	clock := &RealClock{}

	ch := clock.After(10 * time.Millisecond)

	select {
	case <-ch:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("After did not fire")
	}
}

// =============================================================================
// Concurrent Safety Tests
// =============================================================================

func TestPump_HandleRateLimitExceeded_Concurrent(t *testing.T) {
	mockLogger := newTestMockLogger()
	stats := &types.Stats{}

	pump := &Pump{
		Logger: mockLogger,
		Stats:  stats,
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			client := &Client{
				id:   int64(id),
				send: make(chan []byte, 10),
			}
			pump.handleRateLimitExceeded(client)
		}(i)
	}

	wg.Wait()

	if stats.RateLimitedMessages != numGoroutines {
		t.Errorf("RateLimitedMessages: got %d, want %d", stats.RateLimitedMessages, numGoroutines)
	}
}

// =============================================================================
// Integration-style Tests
// =============================================================================

func TestPump_RateLimitingFlow(t *testing.T) {
	mockLogger := newTestMockLogger()
	mockRateLimiter := newTestMockRateLimiter()
	mockAuditLogger := newTestMockAuditLogger()
	stats := &types.Stats{}

	mockRateLimiter.setAllowCount(5)

	pump := &Pump{
		Logger:      mockLogger,
		RateLimiter: mockRateLimiter,
		AuditLogger: mockAuditLogger,
		Stats:       stats,
	}

	clientID := int64(12345)
	blockedCount := 0
	for range 10 {
		if !pump.RateLimiter.CheckLimit(clientID) {
			blockedCount++
		}
	}

	if blockedCount != 5 {
		t.Errorf("blockedCount: got %d, want 5", blockedCount)
	}

	if mockRateLimiter.getCallCount() != 10 {
		t.Errorf("Rate limiter call count: got %d, want 10", mockRateLimiter.getCallCount())
	}
}

// =============================================================================
// ZerologAdapter Tests
// =============================================================================

func TestZerologAdapter_Methods_ReturnValidEvents(t *testing.T) {
	// Create a zerolog logger that writes to a buffer so we can verify output
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	adapter := NewZerologAdapter(logger)

	// Test Debug level - should return a valid event that can be chained and emit output
	adapter.Debug().Str("test", "debug_value").Msg("debug message")

	// Test Info level
	adapter.Info().Str("test", "info_value").Msg("info message")

	// Test Warn level
	adapter.Warn().Str("test", "warn_value").Msg("warn message")

	// Test Error level
	adapter.Error().Str("test", "error_value").Msg("error message")

	// Verify output was written (zerolog writes JSON)
	output := buf.String()

	// Verify all levels produced output
	if !strings.Contains(output, "debug message") {
		t.Error("Debug() did not produce output")
	}
	if !strings.Contains(output, "info message") {
		t.Error("Info() did not produce output")
	}
	if !strings.Contains(output, "warn message") {
		t.Error("Warn() did not produce output")
	}
	if !strings.Contains(output, "error message") {
		t.Error("Error() did not produce output")
	}

	// Verify fields were included
	if !strings.Contains(output, "debug_value") {
		t.Error("Debug().Str() field not in output")
	}
}

func TestZerologEventAdapter_Chaining_ProducesOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	adapter := NewZerologAdapter(logger)

	// Test method chaining with all supported methods
	adapter.Info().
		Str("key", "value").
		Int("num", 42).
		Int64("big", 1234567890).
		Interface("data", map[string]int{"a": 1}).
		Msg("test message")

	output := buf.String()

	// Verify the chained call produced output
	if len(output) == 0 {
		t.Error("Chained method call did not produce output")
	}

	// Verify the message was included
	if !strings.Contains(output, "test message") {
		t.Errorf("Output missing message: %s", output)
	}

	// Verify all fields were included in output (zerolog writes JSON)
	if !strings.Contains(output, "\"key\":\"value\"") {
		t.Errorf("Output missing Str field: %s", output)
	}
	if !strings.Contains(output, "\"num\":42") {
		t.Errorf("Output missing Int field: %s", output)
	}
	if !strings.Contains(output, "\"big\":1234567890") {
		t.Errorf("Output missing Int64 field: %s", output)
	}
	// Interface fields are JSON-encoded
	if !strings.Contains(output, "\"data\":{\"a\":1}") {
		t.Errorf("Output missing Interface field: %s", output)
	}
}
