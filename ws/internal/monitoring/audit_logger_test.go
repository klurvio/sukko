package monitoring

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// AuditLevel Tests
// =============================================================================

func TestAuditLevel_Constants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level    AuditLevel
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARNING, "WARNING"},
		{ERROR, "ERROR"},
		{CRITICAL, "CRITICAL"},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			t.Parallel()
			if string(tt.level) != tt.expected {
				t.Errorf("AuditLevel: got %s, want %s", tt.level, tt.expected)
			}
		})
	}
}

// =============================================================================
// AuditEvent Tests
// =============================================================================

func TestAuditEvent_JSONMarshaling(t *testing.T) {
	t.Parallel()
	clientID := int64(12345)
	event := AuditEvent{
		Level:     WARNING,
		Timestamp: time.Date(2025, 10, 3, 12, 0, 0, 0, time.UTC),
		Event:     "SlowClientDisconnected",
		ClientID:  &clientID,
		Message:   "Client disconnected due to slow consumption",
		Metadata: map[string]any{
			"pending_messages": 500,
			"buffer_full":      true,
		},
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal AuditEvent: %v", err)
	}

	output := string(jsonBytes)

	// Verify required fields
	if !strings.Contains(output, `"level":"WARNING"`) {
		t.Errorf("JSON should contain level: %s", output)
	}
	if !strings.Contains(output, `"event":"SlowClientDisconnected"`) {
		t.Errorf("JSON should contain event: %s", output)
	}
	if !strings.Contains(output, `"client_id":12345`) {
		t.Errorf("JSON should contain client_id: %s", output)
	}
	if !strings.Contains(output, `"message"`) {
		t.Errorf("JSON should contain message: %s", output)
	}
}

func TestAuditEvent_OmitsNilClientID(t *testing.T) {
	t.Parallel()
	event := AuditEvent{
		Level:   INFO,
		Event:   "ServerStarted",
		Message: "Server started successfully",
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal AuditEvent: %v", err)
	}

	output := string(jsonBytes)

	// client_id should be omitted when nil
	if strings.Contains(output, "client_id") {
		t.Errorf("JSON should omit nil client_id: %s", output)
	}
}

func TestAuditEvent_OmitsEmptyMetadata(t *testing.T) {
	t.Parallel()
	event := AuditEvent{
		Level:   INFO,
		Event:   "ServerStarted",
		Message: "Server started successfully",
	}

	jsonBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal AuditEvent: %v", err)
	}

	output := string(jsonBytes)

	// metadata should be omitted when nil
	if strings.Contains(output, "metadata") {
		t.Errorf("JSON should omit nil metadata: %s", output)
	}
}

// =============================================================================
// NewAuditLogger Tests
// =============================================================================

func TestNewAuditLogger_DefaultSettings(t *testing.T) {
	t.Parallel()
	logger := NewAuditLogger(INFO)

	if logger == nil {
		t.Fatal("NewAuditLogger should return non-nil")
	}
	if logger.minLevel != INFO {
		t.Errorf("minLevel: got %s, want INFO", logger.minLevel)
	}
	if logger.alerter != nil {
		t.Error("alerter should be nil by default")
	}
}

func TestNewAuditLogger_AllLevels(t *testing.T) {
	t.Parallel()
	levels := []AuditLevel{DEBUG, INFO, WARNING, ERROR, CRITICAL}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			logger := NewAuditLogger(level)
			if logger.minLevel != level {
				t.Errorf("minLevel: got %s, want %s", logger.minLevel, level)
			}
		})
	}
}

// =============================================================================
// AuditLogger.Log Tests
// =============================================================================

// testAuditLogger creates an AuditLogger that writes to a buffer for testing
func testAuditLogger(minLevel AuditLevel) (*AuditLogger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := &AuditLogger{
		logger:   log.New(buf, "", 0),
		minLevel: minLevel,
	}
	return logger, buf
}

func TestAuditLogger_Log_OutputsJSON(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(INFO)

	logger.Log(AuditEvent{
		Level:   INFO,
		Event:   "TestEvent",
		Message: "Test message",
	})

	output := buf.String()

	// Should be valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &result); err != nil {
		t.Errorf("Output should be valid JSON: %v (output: %s)", err, output)
	}
}

func TestAuditLogger_Log_SetsTimestamp(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(INFO)

	// Log event without timestamp
	logger.Log(AuditEvent{
		Level:   INFO,
		Event:   "TestEvent",
		Message: "Test message",
	})

	output := buf.String()

	// Should have timestamp set
	if !strings.Contains(output, "timestamp") {
		t.Errorf("Output should contain timestamp: %s", output)
	}
}

func TestAuditLogger_Log_RespectsMinLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		minLevel  AuditLevel
		logLevel  AuditLevel
		shouldLog bool
	}{
		{"DEBUG logs DEBUG", DEBUG, DEBUG, true},
		{"DEBUG logs INFO", DEBUG, INFO, true},
		{"DEBUG logs WARNING", DEBUG, WARNING, true},
		{"INFO skips DEBUG", INFO, DEBUG, false},
		{"INFO logs INFO", INFO, INFO, true},
		{"INFO logs ERROR", INFO, ERROR, true},
		{"WARNING skips DEBUG", WARNING, DEBUG, false},
		{"WARNING skips INFO", WARNING, INFO, false},
		{"WARNING logs WARNING", WARNING, WARNING, true},
		{"ERROR logs only ERROR and above", ERROR, WARNING, false},
		{"ERROR logs ERROR", ERROR, ERROR, true},
		{"ERROR logs CRITICAL", ERROR, CRITICAL, true},
		{"CRITICAL logs only CRITICAL", CRITICAL, ERROR, false},
		{"CRITICAL logs CRITICAL", CRITICAL, CRITICAL, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := testAuditLogger(tt.minLevel)

			logger.Log(AuditEvent{
				Level:   tt.logLevel,
				Event:   "TestEvent",
				Message: "Test message",
			})

			hasOutput := buf.Len() > 0
			if hasOutput != tt.shouldLog {
				t.Errorf("minLevel=%s, logLevel=%s: hasOutput=%v, want=%v",
					tt.minLevel, tt.logLevel, hasOutput, tt.shouldLog)
			}
		})
	}
}

// =============================================================================
// AuditLogger.SetAlerter Tests
// =============================================================================

// mockAlerter implements Alerter for testing
type mockAlerter struct {
	mu     sync.Mutex
	alerts []struct {
		level    AuditLevel
		message  string
		metadata map[string]any
	}
}

func (m *mockAlerter) Alert(level AuditLevel, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, struct {
		level    AuditLevel
		message  string
		metadata map[string]any
	}{level, message, metadata})
}

func (m *mockAlerter) getAlerts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.alerts)
}

func TestAuditLogger_SetAlerter(t *testing.T) {
	t.Parallel()
	logger := NewAuditLogger(INFO)
	alerter := &mockAlerter{}

	logger.SetAlerter(alerter)

	if logger.alerter == nil {
		t.Error("alerter should be set")
	}
}

func TestAuditLogger_AlertsOnWarningAndAbove(t *testing.T) {
	t.Parallel()
	logger, _ := testAuditLogger(DEBUG)
	alerter := &mockAlerter{}
	logger.SetAlerter(alerter)

	// DEBUG should NOT trigger alert
	logger.Debug("TestEvent", "debug message", nil)
	time.Sleep(10 * time.Millisecond) // Give time for async processing
	if alerter.getAlerts() != 0 {
		t.Error("DEBUG should not trigger alert")
	}

	// INFO should NOT trigger alert
	logger.Info("TestEvent", "info message", nil)
	time.Sleep(10 * time.Millisecond)
	if alerter.getAlerts() != 0 {
		t.Error("INFO should not trigger alert")
	}

	// WARNING should trigger alert
	logger.Warning("TestEvent", "warning message", nil)
	time.Sleep(10 * time.Millisecond)
	if alerter.getAlerts() != 1 {
		t.Errorf("WARNING should trigger alert, got %d alerts", alerter.getAlerts())
	}

	// ERROR should trigger alert
	logger.Error("TestEvent", "error message", nil)
	time.Sleep(10 * time.Millisecond)
	if alerter.getAlerts() != 2 {
		t.Errorf("ERROR should trigger alert, got %d alerts", alerter.getAlerts())
	}

	// CRITICAL should trigger alert
	logger.Critical("TestEvent", "critical message", nil)
	time.Sleep(10 * time.Millisecond)
	if alerter.getAlerts() != 3 {
		t.Errorf("CRITICAL should trigger alert, got %d alerts", alerter.getAlerts())
	}
}

// =============================================================================
// Convenience Method Tests
// =============================================================================

func TestAuditLogger_Debug(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(DEBUG)

	logger.Debug("TestEvent", "debug message", map[string]any{"key": "value"})

	output := buf.String()
	if !strings.Contains(output, `"level":"DEBUG"`) {
		t.Errorf("Should contain DEBUG level: %s", output)
	}
	if !strings.Contains(output, `"event":"TestEvent"`) {
		t.Errorf("Should contain event: %s", output)
	}
}

func TestAuditLogger_Info(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(INFO)

	logger.Info("TestEvent", "info message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"INFO"`) {
		t.Errorf("Should contain INFO level: %s", output)
	}
}

func TestAuditLogger_Warning(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(INFO)

	logger.Warning("TestEvent", "warning message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"WARNING"`) {
		t.Errorf("Should contain WARNING level: %s", output)
	}
}

func TestAuditLogger_Error(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(INFO)

	logger.Error("TestEvent", "error message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Should contain ERROR level: %s", output)
	}
}

func TestAuditLogger_Critical(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(INFO)

	logger.Critical("TestEvent", "critical message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"CRITICAL"`) {
		t.Errorf("Should contain CRITICAL level: %s", output)
	}
}

// =============================================================================
// WithClientID Tests
// =============================================================================

func TestAuditLogger_WithClientID(t *testing.T) {
	t.Parallel()
	logger, _ := testAuditLogger(DEBUG)
	clientLogger := logger.WithClientID(12345)

	if clientLogger == nil {
		t.Fatal("WithClientID should return non-nil")
	}
	if clientLogger.clientID != 12345 {
		t.Errorf("clientID: got %d, want 12345", clientLogger.clientID)
	}
	if clientLogger.auditLogger != logger {
		t.Error("auditLogger should reference parent")
	}
}

// =============================================================================
// ClientLogger Tests
// =============================================================================

func TestClientLogger_IncludesClientID(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(DEBUG)
	clientLogger := logger.WithClientID(99999)

	clientLogger.Info("TestEvent", "test message", nil)

	output := buf.String()
	if !strings.Contains(output, `"client_id":99999`) {
		t.Errorf("Should contain client_id: %s", output)
	}
}

func TestClientLogger_AllLevels(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(DEBUG)
	clientLogger := logger.WithClientID(12345)

	tests := []struct {
		name     string
		logFunc  func(event, message string, metadata map[string]any)
		expected string
	}{
		{"Debug", clientLogger.Debug, `"level":"DEBUG"`},
		{"Info", clientLogger.Info, `"level":"INFO"`},
		{"Warning", clientLogger.Warning, `"level":"WARNING"`},
		{"Error", clientLogger.Error, `"level":"ERROR"`},
		{"Critical", clientLogger.Critical, `"level":"CRITICAL"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf.Reset()
			tt.logFunc("TestEvent", "test message", nil)

			output := buf.String()
			if !strings.Contains(output, tt.expected) {
				t.Errorf("Should contain %s: %s", tt.expected, output)
			}
			if !strings.Contains(output, `"client_id":12345`) {
				t.Errorf("Should contain client_id: %s", output)
			}
		})
	}
}

func TestClientLogger_WithMetadata(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(DEBUG)
	clientLogger := logger.WithClientID(12345)

	clientLogger.Info("TestEvent", "test message", map[string]any{
		"custom_field": "custom_value",
	})

	output := buf.String()
	if !strings.Contains(output, `"custom_field"`) {
		t.Errorf("Should contain metadata: %s", output)
	}
}

// =============================================================================
// shouldLog Tests
// =============================================================================

func TestShouldLog_LevelHierarchy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		minLevel  AuditLevel
		logLevel  AuditLevel
		shouldLog bool
	}{
		{DEBUG, DEBUG, true},
		{DEBUG, INFO, true},
		{DEBUG, WARNING, true},
		{DEBUG, ERROR, true},
		{DEBUG, CRITICAL, true},

		{INFO, DEBUG, false},
		{INFO, INFO, true},
		{INFO, WARNING, true},
		{INFO, ERROR, true},
		{INFO, CRITICAL, true},

		{WARNING, DEBUG, false},
		{WARNING, INFO, false},
		{WARNING, WARNING, true},
		{WARNING, ERROR, true},
		{WARNING, CRITICAL, true},

		{ERROR, DEBUG, false},
		{ERROR, INFO, false},
		{ERROR, WARNING, false},
		{ERROR, ERROR, true},
		{ERROR, CRITICAL, true},

		{CRITICAL, DEBUG, false},
		{CRITICAL, INFO, false},
		{CRITICAL, WARNING, false},
		{CRITICAL, ERROR, false},
		{CRITICAL, CRITICAL, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.minLevel)+"_"+string(tt.logLevel), func(t *testing.T) {
			t.Parallel()
			logger := &AuditLogger{minLevel: tt.minLevel}
			result := logger.shouldLog(tt.logLevel)
			if result != tt.shouldLog {
				t.Errorf("minLevel=%s, logLevel=%s: got %v, want %v",
					tt.minLevel, tt.logLevel, result, tt.shouldLog)
			}
		})
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestAuditLogger_ConcurrentLogging(t *testing.T) {
	t.Parallel()
	logger, buf := testAuditLogger(DEBUG)
	var wg sync.WaitGroup

	// Multiple goroutines logging concurrently
	for i := range 100 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			logger.Info("ConcurrentEvent", "message", map[string]any{
				"goroutine": id,
			})
		}(i)
	}

	wg.Wait()

	// All logs should be written
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 100 {
		t.Errorf("Expected 100 log lines, got %d", len(lines))
	}
}
