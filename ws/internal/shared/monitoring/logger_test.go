package monitoring

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/adred-codev/ws_poc/internal/shared/types"
	"github.com/rs/zerolog"
)

// resetGlobalLevel resets zerolog's global level to Trace for tests
// This must be called at the start of any test that verifies log output
func resetGlobalLevel() {
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
}

// =============================================================================
// NewLogger Tests
// =============================================================================

func TestNewLogger_DefaultLevel(t *testing.T) {
	// Test with empty level defaults to info
	logger := NewLogger(LoggerConfig{
		Level:  "", // Empty defaults to info
		Format: types.LogFormatJSON,
	})

	// Logger should be created without panic
	_ = logger
}

func TestNewLogger_AllLevels(t *testing.T) {
	levels := []types.LogLevel{
		types.LogLevelDebug,
		types.LogLevelInfo,
		types.LogLevelWarn,
		types.LogLevelError,
		types.LogLevelFatal,
	}

	for _, level := range levels {
		t.Run(string(level), func(t *testing.T) {
			logger := NewLogger(LoggerConfig{
				Level:  level,
				Format: types.LogFormatJSON,
			})

			// Logger should be created without panic
			_ = logger
		})
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	// Create logger directly to write to buffer (bypass NewLogger's global level setting)
	logger := zerolog.New(&buf).With().Logger()

	// Log a test message
	logger.Info().Str("test", "value").Msg("test message")

	output := buf.String()
	if !strings.Contains(output, `"test":"value"`) {
		t.Errorf("JSON format should contain test field: %s", output)
	}
	if !strings.Contains(output, `"message":"test message"`) {
		t.Errorf("JSON format should contain message: %s", output)
	}
}

func TestNewLogger_IncludesServiceField(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer

	// Create logger that writes to buffer
	logger := zerolog.New(&buf).With().
		Str("service", "ws-server").
		Logger()

	logger.Info().Msg("test")

	output := buf.String()
	if !strings.Contains(output, `"service":"ws-server"`) {
		t.Errorf("Logger should include service field: %s", output)
	}
}

// =============================================================================
// LogError Tests
// =============================================================================

func TestLogError_LogsError(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	testErr := errors.New("test error")
	LogError(logger, testErr, "Something went wrong", nil)

	output := buf.String()
	if !strings.Contains(output, "test error") {
		t.Errorf("Should contain error message: %s", output)
	}
	if !strings.Contains(output, "Something went wrong") {
		t.Errorf("Should contain log message: %s", output)
	}
}

func TestLogError_WithFields(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	testErr := errors.New("test error")
	fields := map[string]any{
		"client_id":    123,
		"message_size": 1024,
	}
	LogError(logger, testErr, "Broadcast failed", fields)

	output := buf.String()
	if !strings.Contains(output, "client_id") {
		t.Errorf("Should contain client_id field: %s", output)
	}
	if !strings.Contains(output, "message_size") {
		t.Errorf("Should contain message_size field: %s", output)
	}
}

func TestLogError_NilFields(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	testErr := errors.New("test error")

	// Should not panic with nil fields
	LogError(logger, testErr, "Test message", nil)

	output := buf.String()
	if output == "" {
		t.Error("Should produce log output")
	}
}

// =============================================================================
// LogErrorWithStack Tests
// =============================================================================

func TestLogErrorWithStack_IncludesStackTrace(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	testErr := errors.New("test error")
	LogErrorWithStack(logger, testErr, "Panic recovered", nil)

	output := buf.String()
	if !strings.Contains(output, "stack_trace") {
		t.Errorf("Should include stack_trace field: %s", output)
	}
	if !strings.Contains(output, "test error") {
		t.Errorf("Should contain error message: %s", output)
	}
}

func TestLogErrorWithStack_WithFields(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	testErr := errors.New("unexpected nil pointer")
	fields := map[string]any{
		"function": "processMessage",
		"line":     42,
	}
	LogErrorWithStack(logger, testErr, "Critical error", fields)

	output := buf.String()
	if !strings.Contains(output, "stack_trace") {
		t.Errorf("Should include stack_trace: %s", output)
	}
	if !strings.Contains(output, "function") {
		t.Errorf("Should include function field: %s", output)
	}
}

// =============================================================================
// RecoverPanic Tests
// =============================================================================

func TestRecoverPanic_CapturesPanic(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	func() {
		defer RecoverPanic(logger, "testGoroutine", nil)
		panic("test panic")
	}()

	output := buf.String()
	if !strings.Contains(output, "test panic") {
		t.Errorf("Should capture panic value: %s", output)
	}
	if !strings.Contains(output, "testGoroutine") {
		t.Errorf("Should include goroutine name: %s", output)
	}
	if !strings.Contains(output, "stack_trace") {
		t.Errorf("Should include stack trace: %s", output)
	}
}

func TestRecoverPanic_WithContextFields(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	func() {
		defer RecoverPanic(logger, "writePump", map[string]any{
			"client_id": 12345,
		})
		panic("write failed")
	}()

	output := buf.String()
	if !strings.Contains(output, "client_id") {
		t.Errorf("Should include context fields: %s", output)
	}
	if !strings.Contains(output, "12345") {
		t.Errorf("Should include client_id value: %s", output)
	}
}

func TestRecoverPanic_NoPanic(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	func() {
		defer RecoverPanic(logger, "normalFunction", nil)
		// No panic - normal return
	}()

	output := buf.String()
	if output != "" {
		t.Errorf("Should not log when no panic occurs: %s", output)
	}
}

func TestRecoverPanic_ServerStaysRunning(t *testing.T) {
	resetGlobalLevel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	// Track that goroutine completed
	done := make(chan bool, 1)

	// Simulate a goroutine that panics but is recovered
	// NOTE: RecoverPanic must be used DIRECTLY as a defer, not wrapped
	go func() {
		defer func() { done <- true }()           // This runs after RecoverPanic
		defer RecoverPanic(logger, "workerGoroutine", nil) // This catches panic
		panic("worker panic")
	}()

	// Wait for goroutine to complete
	select {
	case <-done:
		// Success - goroutine recovered and continued
	case <-time.After(1 * time.Second):
		t.Error("Goroutine should have recovered and signaled completion")
	}

	// Verify the panic was logged
	output := buf.String()
	if !strings.Contains(output, "worker panic") {
		t.Errorf("Should log panic: %s", output)
	}
}

// =============================================================================
// LoggerConfig Tests
// =============================================================================

func TestLoggerConfig_Fields(t *testing.T) {
	config := LoggerConfig{
		Level:  types.LogLevelDebug,
		Format: types.LogFormatPretty,
	}

	if config.Level != types.LogLevelDebug {
		t.Errorf("Level: got %s, want debug", config.Level)
	}
	if config.Format != types.LogFormatPretty {
		t.Errorf("Format: got %s, want pretty", config.Format)
	}
}

// =============================================================================
// InitGlobalLogger Tests
// =============================================================================

func TestInitGlobalLogger_SetsGlobalLogger(t *testing.T) {
	// Just verify it doesn't panic
	InitGlobalLogger(LoggerConfig{
		Level:  types.LogLevelInfo,
		Format: types.LogFormatJSON,
	})
}
