package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/shared/alerting"
)

// =============================================================================
// Test Helpers
// =============================================================================

// testLogger creates a Logger that writes to a buffer for testing.
func testLogger(minLevel alerting.Level) (*Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := New(Config{
		MinLevel: minLevel,
		Writer:   buf,
	})
	return logger, buf
}

// mockAlerter implements alerting.Alerter for testing.
type mockAlerter struct {
	mu     sync.Mutex
	alerts []struct {
		level    alerting.Level
		message  string
		metadata map[string]any
	}
}

func (m *mockAlerter) Alert(level alerting.Level, message string, metadata map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, struct {
		level    alerting.Level
		message  string
		metadata map[string]any
	}{level, message, metadata})
}

func (m *mockAlerter) getAlerts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.alerts)
}

// =============================================================================
// New Tests
// =============================================================================

func TestNew(t *testing.T) {
	t.Parallel()
	logger := New(Config{MinLevel: alerting.INFO})

	if logger == nil {
		t.Fatal("New should return non-nil")
	}
	if logger.minLevel != alerting.INFO {
		t.Errorf("minLevel: got %s, want INFO", logger.minLevel)
	}
	if logger.alerter != nil {
		t.Error("alerter should be nil by default")
	}
}

func TestNewWithLevel(t *testing.T) {
	t.Parallel()
	levels := []alerting.Level{alerting.DEBUG, alerting.INFO, alerting.WARNING, alerting.ERROR, alerting.CRITICAL}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()
			logger := NewWithLevel(level)
			if logger.minLevel != level {
				t.Errorf("minLevel: got %s, want %s", logger.minLevel, level)
			}
		})
	}
}

func TestNewWithAlerter(t *testing.T) {
	t.Parallel()
	alerter := &mockAlerter{}
	logger := NewWithAlerter(alerting.WARNING, alerter)

	if logger.minLevel != alerting.WARNING {
		t.Errorf("minLevel: got %s, want WARNING", logger.minLevel)
	}
	if logger.alerter == nil {
		t.Error("alerter should be set")
	}
}

// =============================================================================
// Log Tests
// =============================================================================

func TestLogger_Log_OutputsJSON(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	logger.Log(Event{
		Level:   alerting.INFO,
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

func TestLogger_Log_SetsTimestamp(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	// Log event without timestamp
	logger.Log(Event{
		Level:   alerting.INFO,
		Event:   "TestEvent",
		Message: "Test message",
	})

	output := buf.String()

	// Should have timestamp set
	if !strings.Contains(output, "timestamp") {
		t.Errorf("Output should contain timestamp: %s", output)
	}
}

func TestLogger_Log_PreservesTimestamp(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	fixedTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	logger.Log(Event{
		Level:     alerting.INFO,
		Timestamp: fixedTime,
		Event:     "TestEvent",
		Message:   "Test message",
	})

	output := buf.String()

	if !strings.Contains(output, "2025-06-15") {
		t.Errorf("Should preserve provided timestamp: %s", output)
	}
}

func TestLogger_Log_RespectsMinLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		minLevel  alerting.Level
		logLevel  alerting.Level
		shouldLog bool
	}{
		{"DEBUG logs DEBUG", alerting.DEBUG, alerting.DEBUG, true},
		{"DEBUG logs INFO", alerting.DEBUG, alerting.INFO, true},
		{"DEBUG logs WARNING", alerting.DEBUG, alerting.WARNING, true},
		{"INFO skips DEBUG", alerting.INFO, alerting.DEBUG, false},
		{"INFO logs INFO", alerting.INFO, alerting.INFO, true},
		{"INFO logs ERROR", alerting.INFO, alerting.ERROR, true},
		{"WARNING skips DEBUG", alerting.WARNING, alerting.DEBUG, false},
		{"WARNING skips INFO", alerting.WARNING, alerting.INFO, false},
		{"WARNING logs WARNING", alerting.WARNING, alerting.WARNING, true},
		{"ERROR logs only ERROR and above", alerting.ERROR, alerting.WARNING, false},
		{"ERROR logs ERROR", alerting.ERROR, alerting.ERROR, true},
		{"ERROR logs CRITICAL", alerting.ERROR, alerting.CRITICAL, true},
		{"CRITICAL logs only CRITICAL", alerting.CRITICAL, alerting.ERROR, false},
		{"CRITICAL logs CRITICAL", alerting.CRITICAL, alerting.CRITICAL, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, buf := testLogger(tt.minLevel)

			logger.Log(Event{
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
// SetAlerter Tests
// =============================================================================

func TestLogger_SetAlerter(t *testing.T) {
	t.Parallel()
	logger := NewWithLevel(alerting.INFO)
	alerter := &mockAlerter{}

	logger.SetAlerter(alerter)

	if logger.alerter == nil {
		t.Error("alerter should be set")
	}
}

func TestLogger_AlertsOnWarningAndAbove(t *testing.T) {
	t.Parallel()
	logger, _ := testLogger(alerting.DEBUG)
	alerter := &mockAlerter{}
	logger.SetAlerter(alerter)

	// DEBUG should NOT trigger alert
	logger.Debug("TestEvent", "debug message", nil)
	if alerter.getAlerts() != 0 {
		t.Error("DEBUG should not trigger alert")
	}

	// INFO should NOT trigger alert
	logger.Info("TestEvent", "info message", nil)
	if alerter.getAlerts() != 0 {
		t.Error("INFO should not trigger alert")
	}

	// WARNING should trigger alert
	logger.Warning("TestEvent", "warning message", nil)
	if alerter.getAlerts() != 1 {
		t.Errorf("WARNING should trigger alert, got %d alerts", alerter.getAlerts())
	}

	// ERROR should trigger alert
	logger.Error("TestEvent", "error message", nil)
	if alerter.getAlerts() != 2 {
		t.Errorf("ERROR should trigger alert, got %d alerts", alerter.getAlerts())
	}

	// CRITICAL should trigger alert
	logger.Critical("TestEvent", "critical message", nil)
	if alerter.getAlerts() != 3 {
		t.Errorf("CRITICAL should trigger alert, got %d alerts", alerter.getAlerts())
	}
}

// =============================================================================
// Convenience Method Tests
// =============================================================================

func TestLogger_Debug(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.DEBUG)

	logger.Debug("TestEvent", "debug message", map[string]any{"key": "value"})

	output := buf.String()
	if !strings.Contains(output, `"level":"DEBUG"`) {
		t.Errorf("Should contain DEBUG level: %s", output)
	}
	if !strings.Contains(output, `"event":"TestEvent"`) {
		t.Errorf("Should contain event: %s", output)
	}
}

func TestLogger_Info(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	logger.Info("TestEvent", "info message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"INFO"`) {
		t.Errorf("Should contain INFO level: %s", output)
	}
}

func TestLogger_Warning(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	logger.Warning("TestEvent", "warning message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"WARNING"`) {
		t.Errorf("Should contain WARNING level: %s", output)
	}
}

func TestLogger_Error(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	logger.Error("TestEvent", "error message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Should contain ERROR level: %s", output)
	}
}

func TestLogger_Critical(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.INFO)

	logger.Critical("TestEvent", "critical message", nil)

	output := buf.String()
	if !strings.Contains(output, `"level":"CRITICAL"`) {
		t.Errorf("Should contain CRITICAL level: %s", output)
	}
}

// =============================================================================
// WithClientID Tests
// =============================================================================

func TestLogger_WithClientID(t *testing.T) {
	t.Parallel()
	logger, _ := testLogger(alerting.DEBUG)
	clientLogger := logger.WithClientID(12345)

	if clientLogger == nil {
		t.Fatal("WithClientID should return non-nil")
	}
	if clientLogger.clientID != 12345 {
		t.Errorf("clientID: got %d, want 12345", clientLogger.clientID)
	}
	if clientLogger.logger != logger {
		t.Error("logger should reference parent")
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestLogger_ConcurrentLogging(t *testing.T) {
	t.Parallel()
	logger, buf := testLogger(alerting.DEBUG)
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
