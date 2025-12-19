package testutil

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adred-codev/odin-ws/internal/monitoring"
)

// MockSystemMonitor provides controllable CPU/memory/goroutine values for testing.
type MockSystemMonitor struct {
	mu          sync.RWMutex
	cpuPercent  float64
	memoryBytes int64
	goroutines  int
}

// NewMockSystemMonitor creates a new mock with default safe values.
func NewMockSystemMonitor() *MockSystemMonitor {
	return &MockSystemMonitor{
		cpuPercent:  10.0,
		memoryBytes: 100 * 1024 * 1024, // 100MB
		goroutines:  100,
	}
}

// SetCPU sets the CPU percentage returned by GetCPUPercent.
func (m *MockSystemMonitor) SetCPU(percent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cpuPercent = percent
}

// SetMemory sets the memory bytes returned by GetMemoryBytes.
func (m *MockSystemMonitor) SetMemory(bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memoryBytes = bytes
}

// SetGoroutines sets the goroutine count returned by GetGoroutines.
func (m *MockSystemMonitor) SetGoroutines(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goroutines = count
}

func (m *MockSystemMonitor) GetCPUPercent() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cpuPercent
}

func (m *MockSystemMonitor) GetMemoryBytes() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryBytes
}

func (m *MockSystemMonitor) GetGoroutines() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.goroutines
}

func (m *MockSystemMonitor) GetCPUAllocation() float64 {
	return 1.0
}

func (m *MockSystemMonitor) GetMetrics() monitoring.SystemMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return monitoring.SystemMetrics{
		CPUPercent:  m.cpuPercent,
		MemoryBytes: m.memoryBytes,
		MemoryMB:    float64(m.memoryBytes) / (1024 * 1024),
		Goroutines:  m.goroutines,
	}
}

// MockResourceGuard provides controllable rate limiting for testing.
type MockResourceGuard struct {
	AllowKafka    bool
	ShouldPause   bool
	WaitDuration  time.Duration
	AllowedCount  atomic.Int64
	RejectedCount atomic.Int64
}

// NewMockResourceGuard creates a permissive mock (allows everything by default).
func NewMockResourceGuard() *MockResourceGuard {
	return &MockResourceGuard{
		AllowKafka:   true,
		ShouldPause:  false,
		WaitDuration: 0,
	}
}

func (m *MockResourceGuard) AllowKafkaMessage(ctx context.Context) (bool, time.Duration) {
	if m.AllowKafka {
		m.AllowedCount.Add(1)
		return true, 0
	}
	m.RejectedCount.Add(1)
	return false, m.WaitDuration
}

func (m *MockResourceGuard) ShouldPauseKafka() bool {
	return m.ShouldPause
}

// MockBroadcastFunc captures broadcast calls for verification.
type MockBroadcastFunc struct {
	mu       sync.Mutex
	Calls    []BroadcastCall
	Callback func(tokenID, eventType string, message []byte)
}

// BroadcastCall records a single broadcast invocation.
type BroadcastCall struct {
	TokenID   string
	EventType string
	Message   []byte
}

// NewMockBroadcastFunc creates a mock that captures all broadcast calls.
func NewMockBroadcastFunc() *MockBroadcastFunc {
	return &MockBroadcastFunc{
		Calls: make([]BroadcastCall, 0),
	}
}

// Func returns the BroadcastFunc to pass to consumers.
func (m *MockBroadcastFunc) Func() func(tokenID, eventType string, message []byte) {
	return func(tokenID, eventType string, message []byte) {
		m.mu.Lock()
		m.Calls = append(m.Calls, BroadcastCall{
			TokenID:   tokenID,
			EventType: eventType,
			Message:   message,
		})
		m.mu.Unlock()

		if m.Callback != nil {
			m.Callback(tokenID, eventType, message)
		}
	}
}

// CallCount returns the number of times the broadcast function was called.
func (m *MockBroadcastFunc) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

// GetCalls returns a copy of all recorded calls.
func (m *MockBroadcastFunc) GetCalls() []BroadcastCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]BroadcastCall, len(m.Calls))
	copy(result, m.Calls)
	return result
}

// Reset clears all recorded calls.
func (m *MockBroadcastFunc) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = m.Calls[:0]
}

// MockConn implements net.Conn for testing WebSocket operations.
type MockConn struct {
	ReadData    []byte
	ReadErr     error
	WriteData   []byte
	WrittenData []byte
	WriteErr    error
	Closed      bool
	LocalAddr_  net.Addr
	RemoteAddr_ net.Addr
	mu          sync.Mutex
}

// NewMockConn creates a mock connection with default addresses.
func NewMockConn() *MockConn {
	return &MockConn{
		LocalAddr_:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3004},
		RemoteAddr_: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 54321},
	}
}

func (m *MockConn) Read(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ReadErr != nil {
		return 0, m.ReadErr
	}
	n = copy(b, m.ReadData)
	return n, nil
}

func (m *MockConn) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WriteErr != nil {
		return 0, m.WriteErr
	}
	m.WrittenData = append(m.WrittenData, b...)
	return len(b), nil
}

func (m *MockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
	return nil
}

func (m *MockConn) LocalAddr() net.Addr {
	return m.LocalAddr_
}

func (m *MockConn) RemoteAddr() net.Addr {
	return m.RemoteAddr_
}

func (m *MockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *MockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *MockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// GetWrittenData returns all data written to the connection.
func (m *MockConn) GetWrittenData() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]byte, len(m.WrittenData))
	copy(result, m.WrittenData)
	return result
}

// IsClosed returns whether Close() was called.
func (m *MockConn) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Closed
}

// MockAlerter captures alert calls for testing.
type MockAlerter struct {
	mu     sync.Mutex
	Alerts []AlertCall
}

// AlertCall records a single alert invocation.
type AlertCall struct {
	Level    monitoring.AuditLevel
	Message  string
	Metadata map[string]any
}

// NewMockAlerter creates a mock alerter.
func NewMockAlerter() *MockAlerter {
	return &MockAlerter{
		Alerts: make([]AlertCall, 0),
	}
}

func (m *MockAlerter) Alert(level monitoring.AuditLevel, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Alerts = append(m.Alerts, AlertCall{
		Level:    level,
		Message:  message,
		Metadata: metadata,
	})
}

// AlertCount returns the number of alerts recorded.
func (m *MockAlerter) AlertCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Alerts)
}

// =============================================================================
// Multi-Package Mocks (for multi-core testing)
// =============================================================================

// MockShard provides controllable shard behavior for LoadBalancer testing.
type MockShard struct {
	id             int
	currentConns   atomic.Int64
	maxConnections int
	availableSlots atomic.Int32
	advertiseAddr  string
	cpuPercent     float64
	memoryMB       float64
}

// NewMockShard creates a mock shard with default values.
func NewMockShard(id int, maxConns int) *MockShard {
	s := &MockShard{
		id:             id,
		maxConnections: maxConns,
		advertiseAddr:  fmt.Sprintf("localhost:%d", 3000+id),
		cpuPercent:     10.0,
		memoryMB:       100.0,
	}
	s.availableSlots.Store(int32(maxConns))
	return s
}

func (m *MockShard) GetCurrentConnections() int64 {
	return m.currentConns.Load()
}

func (m *MockShard) GetMaxConnections() int {
	return m.maxConnections
}

func (m *MockShard) GetAvailableSlots() int {
	return int(m.availableSlots.Load())
}

func (m *MockShard) GetAddr() string {
	return m.advertiseAddr
}

func (m *MockShard) GetSystemStats() (float64, float64) {
	return m.cpuPercent, m.memoryMB
}

func (m *MockShard) TryAcquireSlot() bool {
	for {
		current := m.availableSlots.Load()
		if current <= 0 {
			return false
		}
		if m.availableSlots.CompareAndSwap(current, current-1) {
			m.currentConns.Add(1)
			return true
		}
	}
}

func (m *MockShard) ReleaseSlot() {
	m.availableSlots.Add(1)
	m.currentConns.Add(-1)
}

// SetConnections sets the current connection count for testing.
func (m *MockShard) SetConnections(count int64) {
	m.currentConns.Store(count)
	m.availableSlots.Store(int32(m.maxConnections - int(count)))
}

// SetSystemStats sets CPU and memory values.
func (m *MockShard) SetSystemStats(cpu, mem float64) {
	m.cpuPercent = cpu
	m.memoryMB = mem
}

// MockBroadcastBus provides controllable broadcast bus for Shard/KafkaPool testing.
type MockBroadcastBus struct {
	mu            sync.Mutex
	PublishCalls  []MockBroadcastMessage
	subscribers   []chan *MockBroadcastMessage
	healthy       atomic.Bool
	publishErrors atomic.Uint64
	messagesRecv  atomic.Uint64
}

// MockBroadcastMessage mirrors orchestration.BroadcastMessage for testing.
type MockBroadcastMessage struct {
	Subject string
	Message []byte
}

// NewMockBroadcastBus creates a mock broadcast bus.
func NewMockBroadcastBus() *MockBroadcastBus {
	bus := &MockBroadcastBus{
		PublishCalls: make([]MockBroadcastMessage, 0),
		subscribers:  make([]chan *MockBroadcastMessage, 0),
	}
	bus.healthy.Store(true)
	return bus
}

// Publish records the publish call.
func (m *MockBroadcastBus) Publish(msg *MockBroadcastMessage) {
	m.mu.Lock()
	m.PublishCalls = append(m.PublishCalls, *msg)
	// Fan out to subscribers
	for _, sub := range m.subscribers {
		select {
		case sub <- msg:
		default:
			// Channel full, drop
		}
	}
	m.mu.Unlock()
}

// Subscribe returns a channel for receiving broadcast messages.
func (m *MockBroadcastBus) Subscribe() chan *MockBroadcastMessage {
	subCh := make(chan *MockBroadcastMessage, 1024)
	m.mu.Lock()
	m.subscribers = append(m.subscribers, subCh)
	m.mu.Unlock()
	return subCh
}

// GetPublishCount returns the number of publish calls.
func (m *MockBroadcastBus) GetPublishCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.PublishCalls)
}

// GetPublishCalls returns all recorded publish calls.
func (m *MockBroadcastBus) GetPublishCalls() []MockBroadcastMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]MockBroadcastMessage, len(m.PublishCalls))
	copy(result, m.PublishCalls)
	return result
}

// IsHealthy returns the health status.
func (m *MockBroadcastBus) IsHealthy() bool {
	return m.healthy.Load()
}

// SetHealthy sets the health status.
func (m *MockBroadcastBus) SetHealthy(healthy bool) {
	m.healthy.Store(healthy)
}

// GetMetrics returns mock metrics.
func (m *MockBroadcastBus) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"type":              "mock",
		"healthy":           m.IsHealthy(),
		"channel":           "test.channel",
		"subscribers":       len(m.subscribers),
		"publish_errors":    m.publishErrors.Load(),
		"messages_received": m.messagesRecv.Load(),
		"last_publish_ago":  0.0,
	}
}

// Reset clears all recorded calls.
func (m *MockBroadcastBus) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PublishCalls = m.PublishCalls[:0]
}

// =============================================================================
// Interfaces Mocks (for wsserver.interfaces.go)
// =============================================================================

// MockLogger captures log messages for testing.
// Implements wsserver.Logger interface.
type MockLogger struct {
	mu       sync.Mutex
	Messages []LogMessage
}

// LogMessage records a single log event.
type LogMessage struct {
	Level   string
	Message string
	Fields  map[string]any
	Error   error
}

// NewMockLogger creates a mock logger.
func NewMockLogger() *MockLogger {
	return &MockLogger{
		Messages: make([]LogMessage, 0),
	}
}

func (m *MockLogger) Debug() MockLogEventInterface { return &MockLogEvent{logger: m, level: "debug"} }
func (m *MockLogger) Info() MockLogEventInterface  { return &MockLogEvent{logger: m, level: "info"} }
func (m *MockLogger) Warn() MockLogEventInterface  { return &MockLogEvent{logger: m, level: "warn"} }
func (m *MockLogger) Error() MockLogEventInterface { return &MockLogEvent{logger: m, level: "error"} }

// MockLogEventInterface is the interface returned by MockLogger methods.
// This allows MockLogger to implement wsserver.Logger interface.
type MockLogEventInterface interface {
	Err(err error) MockLogEventInterface
	Int64(key string, val int64) MockLogEventInterface
	Int(key string, val int) MockLogEventInterface
	Str(key string, val string) MockLogEventInterface
	Interface(key string, val any) MockLogEventInterface
	Msg(msg string)
}

func (m *MockLogger) Printf(format string, v ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Messages = append(m.Messages, LogMessage{
		Level:   "printf",
		Message: fmt.Sprintf(format, v...),
		Fields:  nil,
	})
}

// GetMessages returns a copy of all recorded log messages.
func (m *MockLogger) GetMessages() []LogMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]LogMessage, len(m.Messages))
	copy(result, m.Messages)
	return result
}

// MessageCount returns the number of log messages recorded.
func (m *MockLogger) MessageCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Messages)
}

// HasMessage checks if any log message contains the given substring.
func (m *MockLogger) HasMessage(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.Messages {
		if contains(msg.Message, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Reset clears all recorded log messages.
func (m *MockLogger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Messages = m.Messages[:0]
}

// MockLogEvent implements wsserver.LogEvent for testing.
type MockLogEvent struct {
	logger *MockLogger
	level  string
	fields map[string]any
	err    error
}

func (e *MockLogEvent) Err(err error) MockLogEventInterface {
	e.err = err
	return e
}

func (e *MockLogEvent) Int64(key string, val int64) MockLogEventInterface {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *MockLogEvent) Int(key string, val int) MockLogEventInterface {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *MockLogEvent) Str(key string, val string) MockLogEventInterface {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *MockLogEvent) Interface(key string, val any) MockLogEventInterface {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *MockLogEvent) Msg(msg string) {
	e.logger.mu.Lock()
	defer e.logger.mu.Unlock()
	e.logger.Messages = append(e.logger.Messages, LogMessage{
		Level:   e.level,
		Message: msg,
		Fields:  e.fields,
		Error:   e.err,
	})
}

// MockClock provides controllable time for testing.
// Implements wsserver.Clock interface.
type MockClock struct {
	mu         sync.Mutex
	now        time.Time
	tickers    []*MockTicker
	afterChans []chan time.Time
}

// NewMockClock creates a mock clock starting at the given time.
// If zero time is provided, uses a fixed time for determinism.
func NewMockClock(t ...time.Time) *MockClock {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if len(t) > 0 && !t[0].IsZero() {
		now = t[0]
	}
	return &MockClock{
		now:        now,
		tickers:    make([]*MockTicker, 0),
		afterChans: make([]chan time.Time, 0),
	}
}

func (c *MockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// MockTickerInterface is returned by MockClock.NewTicker.
// This allows MockClock to implement wsserver.Clock interface.
type MockTickerInterface interface {
	C() <-chan time.Time
	Stop()
}

func (c *MockClock) NewTicker(d time.Duration) MockTickerInterface {
	c.mu.Lock()
	defer c.mu.Unlock()
	ticker := &MockTicker{
		c:        make(chan time.Time, 1),
		duration: d,
		stopped:  false,
	}
	c.tickers = append(c.tickers, ticker)
	return ticker
}

func (c *MockClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.afterChans = append(c.afterChans, ch)
	return ch
}

// Advance moves the clock forward by the given duration.
// Triggers any tickers that have accumulated enough time.
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	tickers := c.tickers
	afterChans := c.afterChans
	c.afterChans = make([]chan time.Time, 0) // Clear after chans
	c.mu.Unlock()

	// Trigger tickers
	for _, t := range tickers {
		if !t.stopped {
			select {
			case t.c <- now:
			default:
			}
		}
	}

	// Trigger after channels
	for _, ch := range afterChans {
		select {
		case ch <- now:
		default:
		}
	}
}

// Set sets the clock to a specific time.
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Tick manually triggers all tickers (for fine-grained control).
func (c *MockClock) Tick() {
	c.mu.Lock()
	now := c.now
	tickers := c.tickers
	c.mu.Unlock()

	for _, t := range tickers {
		if !t.stopped {
			select {
			case t.c <- now:
			default:
			}
		}
	}
}

// MockTicker implements wsserver.Ticker for testing.
type MockTicker struct {
	c        chan time.Time
	duration time.Duration
	stopped  bool
}

func (t *MockTicker) C() <-chan time.Time {
	return t.c
}

func (t *MockTicker) Stop() {
	t.stopped = true
}

// MockMetricsRecorder captures metrics for testing.
// Implements wsserver.MetricsRecorder interface.
type MockMetricsRecorder struct {
	mu               sync.Mutex
	MessagesSent     int64
	BytesSent        int64
	MessagesReceived int64
	BytesReceived    int64
	RateLimitedCount int64
	Disconnects      []DisconnectRecord
}

// DisconnectRecord records a single disconnect event.
type DisconnectRecord struct {
	Reason      string
	InitiatedBy string
	Duration    time.Duration
}

// NewMockMetricsRecorder creates a mock metrics recorder.
func NewMockMetricsRecorder() *MockMetricsRecorder {
	return &MockMetricsRecorder{
		Disconnects: make([]DisconnectRecord, 0),
	}
}

func (m *MockMetricsRecorder) RecordMessageSent(count, bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MessagesSent += count
	m.BytesSent += bytes
}

func (m *MockMetricsRecorder) RecordMessageReceived(count, bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MessagesReceived += count
	m.BytesReceived += bytes
}

func (m *MockMetricsRecorder) IncrementRateLimited() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RateLimitedCount++
}

func (m *MockMetricsRecorder) RecordDisconnect(reason, initiatedBy string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Disconnects = append(m.Disconnects, DisconnectRecord{
		Reason:      reason,
		InitiatedBy: initiatedBy,
		Duration:    duration,
	})
}

// GetMetrics returns a snapshot of all metrics.
func (m *MockMetricsRecorder) GetMetrics() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]int64{
		"messages_sent":     m.MessagesSent,
		"bytes_sent":        m.BytesSent,
		"messages_received": m.MessagesReceived,
		"bytes_received":    m.BytesReceived,
		"rate_limited":      m.RateLimitedCount,
		"disconnects":       int64(len(m.Disconnects)),
	}
}

// Reset clears all recorded metrics.
func (m *MockMetricsRecorder) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MessagesSent = 0
	m.BytesSent = 0
	m.MessagesReceived = 0
	m.BytesReceived = 0
	m.RateLimitedCount = 0
	m.Disconnects = m.Disconnects[:0]
}

// MockRateLimiter provides controllable rate limiting for testing.
// Implements wsserver.RateLimiter interface.
type MockRateLimiter struct {
	mu          sync.Mutex
	AllowAll    bool           // If true, always allows
	AllowCount  int            // Number of calls to allow before blocking
	BlockedIDs  map[int64]bool // Specific IDs to block
	CallHistory []int64        // Record of all CheckLimit calls
}

// NewMockRateLimiter creates a permissive mock rate limiter.
func NewMockRateLimiter() *MockRateLimiter {
	return &MockRateLimiter{
		AllowAll:    true,
		BlockedIDs:  make(map[int64]bool),
		CallHistory: make([]int64, 0),
	}
}

func (m *MockRateLimiter) CheckLimit(clientID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallHistory = append(m.CallHistory, clientID)

	// Check specific blocked IDs first
	if m.BlockedIDs[clientID] {
		return false
	}

	// If AllowAll is set, always allow
	if m.AllowAll {
		return true
	}

	// Otherwise, decrement AllowCount
	if m.AllowCount > 0 {
		m.AllowCount--
		return true
	}

	return false
}

// Block marks a specific client ID as blocked.
func (m *MockRateLimiter) Block(clientID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BlockedIDs[clientID] = true
}

// Unblock removes a client ID from the blocked list.
func (m *MockRateLimiter) Unblock(clientID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.BlockedIDs, clientID)
}

// SetAllowCount sets the number of calls to allow (when AllowAll is false).
func (m *MockRateLimiter) SetAllowCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AllowAll = false
	m.AllowCount = count
}

// GetCallCount returns the number of CheckLimit calls made.
func (m *MockRateLimiter) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CallHistory)
}

// Reset clears all state.
func (m *MockRateLimiter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AllowAll = true
	m.AllowCount = 0
	m.BlockedIDs = make(map[int64]bool)
	m.CallHistory = m.CallHistory[:0]
}

// MockAuditLogger captures audit events for testing.
// Implements wsserver.AuditLogger interface.
type MockAuditLogger struct {
	mu     sync.Mutex
	Events []AuditEvent
}

// AuditEvent records a single audit event.
type AuditEvent struct {
	Level    string
	Event    string
	Message  string
	Metadata map[string]any
}

// NewMockAuditLogger creates a mock audit logger.
func NewMockAuditLogger() *MockAuditLogger {
	return &MockAuditLogger{
		Events: make([]AuditEvent, 0),
	}
}

func (m *MockAuditLogger) Warning(event, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, AuditEvent{
		Level:    "warning",
		Event:    event,
		Message:  message,
		Metadata: metadata,
	})
}

func (m *MockAuditLogger) Info(event, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, AuditEvent{
		Level:    "info",
		Event:    event,
		Message:  message,
		Metadata: metadata,
	})
}

func (m *MockAuditLogger) Critical(event, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, AuditEvent{
		Level:    "critical",
		Event:    event,
		Message:  message,
		Metadata: metadata,
	})
}

// GetEvents returns a copy of all recorded events.
func (m *MockAuditLogger) GetEvents() []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]AuditEvent, len(m.Events))
	copy(result, m.Events)
	return result
}

// EventCount returns the number of events recorded.
func (m *MockAuditLogger) EventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Events)
}

// HasEvent checks if any event has the given event name.
func (m *MockAuditLogger) HasEvent(eventName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.Events {
		if e.Event == eventName {
			return true
		}
	}
	return false
}

// Reset clears all recorded events.
func (m *MockAuditLogger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = m.Events[:0]
}

// MockListenerFactory creates mock listeners for testing.
// Implements wsserver.ListenerFactory interface.
type MockListenerFactory struct {
	Listener net.Listener
	Error    error
}

func (m *MockListenerFactory) Listen(network, addr string) (net.Listener, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Listener, nil
}
