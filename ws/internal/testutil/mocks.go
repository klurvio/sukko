// Package testutil provides testing utilities, mocks, and fixtures for the WS server.
package testutil

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adred-codev/ws_poc/internal/shared/monitoring"
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
	id              int
	currentConns    atomic.Int64
	maxConnections  int
	availableSlots  atomic.Int32
	advertiseAddr   string
	cpuPercent      float64
	memoryMB        float64
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

// MockBroadcastMessage mirrors multi.BroadcastMessage for testing.
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
