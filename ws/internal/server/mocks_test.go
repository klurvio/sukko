package server

import (
	"net"
	"sync"
	"time"
)

// =============================================================================
// Test Mocks (internal to shared package tests)
// =============================================================================

// testMockLogger captures log messages for testing.
type testMockLogger struct {
	mu       sync.Mutex
	messages []testLogMessage
}

type testLogMessage struct {
	level   string
	message string
	fields  map[string]any
	err     error
}

func newTestMockLogger() *testMockLogger {
	return &testMockLogger{messages: make([]testLogMessage, 0)}
}

func (m *testMockLogger) Debug() LogEvent                { return &testMockLogEvent{logger: m, level: "debug"} }
func (m *testMockLogger) Info() LogEvent                 { return &testMockLogEvent{logger: m, level: "info"} }
func (m *testMockLogger) Warn() LogEvent                 { return &testMockLogEvent{logger: m, level: "warn"} }
func (m *testMockLogger) Error() LogEvent                { return &testMockLogEvent{logger: m, level: "error"} }
func (m *testMockLogger) Printf(format string, v ...any) {}

func (m *testMockLogger) messageCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

func (m *testMockLogger) getMessages() []testLogMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]testLogMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

// testMockLogEvent implements LogEvent for testing.
type testMockLogEvent struct {
	logger *testMockLogger
	level  string
	fields map[string]any
	err    error
}

func (e *testMockLogEvent) Err(err error) LogEvent {
	e.err = err
	return e
}

func (e *testMockLogEvent) Int64(key string, val int64) LogEvent {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *testMockLogEvent) Int(key string, val int) LogEvent {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *testMockLogEvent) Str(key string, val string) LogEvent {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *testMockLogEvent) Interface(key string, val any) LogEvent {
	if e.fields == nil {
		e.fields = make(map[string]any)
	}
	e.fields[key] = val
	return e
}

func (e *testMockLogEvent) Msg(msg string) {
	e.logger.mu.Lock()
	defer e.logger.mu.Unlock()
	e.logger.messages = append(e.logger.messages, testLogMessage{
		level:   e.level,
		message: msg,
		fields:  e.fields,
		err:     e.err,
	})
}

// testMockClock provides controllable time for testing.
type testMockClock struct {
	mu         sync.Mutex
	now        time.Time
	tickers    []*testMockTicker
	afterChans []chan time.Time
}

func newTestMockClock(t ...time.Time) *testMockClock {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if len(t) > 0 && !t[0].IsZero() {
		now = t[0]
	}
	return &testMockClock{
		now:        now,
		tickers:    make([]*testMockTicker, 0),
		afterChans: make([]chan time.Time, 0),
	}
}

func (c *testMockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testMockClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	ticker := &testMockTicker{
		c:        make(chan time.Time, 1),
		duration: d,
		stopped:  false,
	}
	c.tickers = append(c.tickers, ticker)
	return ticker
}

func (c *testMockClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.afterChans = append(c.afterChans, ch)
	return ch
}

func (c *testMockClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	tickers := c.tickers
	afterChans := c.afterChans
	c.afterChans = make([]chan time.Time, 0)
	c.mu.Unlock()

	for _, t := range tickers {
		if !t.stopped {
			select {
			case t.c <- now:
			default:
			}
		}
	}

	for _, ch := range afterChans {
		select {
		case ch <- now:
		default:
		}
	}
}

// testMockTicker implements Ticker for testing.
type testMockTicker struct {
	c        chan time.Time
	duration time.Duration
	stopped  bool
}

func (t *testMockTicker) C() <-chan time.Time {
	return t.c
}

func (t *testMockTicker) Stop() {
	t.stopped = true
}

// testMockRateLimiter provides controllable rate limiting for testing.
type testMockRateLimiter struct {
	mu          sync.Mutex
	allowAll    bool
	allowCount  int
	blockedIDs  map[int64]bool
	callHistory []int64
}

func newTestMockRateLimiter() *testMockRateLimiter {
	return &testMockRateLimiter{
		allowAll:    true,
		blockedIDs:  make(map[int64]bool),
		callHistory: make([]int64, 0),
	}
}

func (m *testMockRateLimiter) CheckLimit(clientID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callHistory = append(m.callHistory, clientID)

	if m.blockedIDs[clientID] {
		return false
	}

	if m.allowAll {
		return true
	}

	if m.allowCount > 0 {
		m.allowCount--
		return true
	}

	return false
}

func (m *testMockRateLimiter) setAllowCount(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowAll = false
	m.allowCount = count
}

func (m *testMockRateLimiter) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.callHistory)
}

// testMockAuditLogger captures audit events for testing.
type testMockAuditLogger struct {
	mu     sync.Mutex
	events []testAuditEvent
}

type testAuditEvent struct {
	level    string
	event    string
	message  string
	metadata map[string]any
}

func newTestMockAuditLogger() *testMockAuditLogger {
	return &testMockAuditLogger{events: make([]testAuditEvent, 0)}
}

func (m *testMockAuditLogger) Warning(event, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, testAuditEvent{
		level:    "warning",
		event:    event,
		message:  message,
		metadata: metadata,
	})
}

func (m *testMockAuditLogger) Info(event, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, testAuditEvent{
		level:    "info",
		event:    event,
		message:  message,
		metadata: metadata,
	})
}

func (m *testMockAuditLogger) Critical(event, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, testAuditEvent{
		level:    "critical",
		event:    event,
		message:  message,
		metadata: metadata,
	})
}

func (m *testMockAuditLogger) eventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func (m *testMockAuditLogger) hasEvent(eventName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.event == eventName {
			return true
		}
	}
	return false
}

// =============================================================================
// Mock Connection for WebSocket Testing
// =============================================================================

// testMockConn implements net.Conn for testing WebSocket frame handling.
type testMockConn struct {
	mu           sync.Mutex
	readBuf      []byte
	readPos      int
	readErr      error
	writeBuf     []byte
	writeErr     error
	closed       bool
	deadlines    []time.Time
	readDeadline time.Time
}

func newTestMockConn() *testMockConn {
	return &testMockConn{
		readBuf:   make([]byte, 0),
		writeBuf:  make([]byte, 0),
		deadlines: make([]time.Time, 0),
	}
}

func (c *testMockConn) Read(b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.readErr != nil {
		return 0, c.readErr
	}

	if c.readPos >= len(c.readBuf) {
		return 0, c.readErr
	}

	n = copy(b, c.readBuf[c.readPos:])
	c.readPos += n
	return n, nil
}

func (c *testMockConn) Write(b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writeErr != nil {
		return 0, c.writeErr
	}

	c.writeBuf = append(c.writeBuf, b...)
	return len(b), nil
}

func (c *testMockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *testMockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (c *testMockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (c *testMockConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlines = append(c.deadlines, t)
	return nil
}

func (c *testMockConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.deadlines = append(c.deadlines, t)
	return nil
}

func (c *testMockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// Helper methods for testing

func (c *testMockConn) setReadData(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readBuf = data
	c.readPos = 0
}

func (c *testMockConn) setReadError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readErr = err
}

func (c *testMockConn) getWrittenData() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]byte, len(c.writeBuf))
	copy(result, c.writeBuf)
	return result
}

func (c *testMockConn) getDeadlineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.deadlines)
}

func (c *testMockConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}
