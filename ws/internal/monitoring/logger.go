// Package monitoring provides observability components for the WebSocket server
// including structured logging, Prometheus metrics, alerting, and system resource
// monitoring with cgroup-aware CPU/memory tracking.
//
// Key components:
//   - Logger: Structured JSON logging with Loki integration
//   - MetricsCollector: Prometheus metrics for connections, messages, latency
//   - AuditLogger: Audit trail for security-relevant events
//   - SystemMonitor: Real-time CPU and memory monitoring for resource guards
package monitoring

import (
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Toniq-Labs/odin-ws/internal/types"
)

// LoggerConfig holds logger configuration.
//
// Deprecated: Use pkg/logging.LoggerConfig instead.
type LoggerConfig struct {
	Level       types.LogLevel  // Minimum log level
	Format      types.LogFormat // Output format
	ServiceName string          // Service name for log identification (e.g., "ws-server", "ws-gateway")
}

// NewLogger creates a structured logger configured for Loki integration.
//
// Deprecated: Use pkg/logging.NewLogger instead.
//
// Features:
//   - Structured JSON output (Loki-compatible)
//   - Stack traces on errors
//   - Contextual fields for filtering
//   - Timestamp in RFC3339 format
//   - Caller information for debugging
//   - Configurable service name for multi-service deployments
//
// Example:
//
//	logger := NewLogger(LoggerConfig{
//	    Level:       LogLevelInfo,
//	    Format:      LogFormatJSON,
//	    ServiceName: "ws-gateway",
//	})
//	logger.Info().
//	    Str("component", "server").
//	    Int("connections", 100).
//	    Msg("Server started")
func NewLogger(config LoggerConfig) zerolog.Logger {
	var output io.Writer = os.Stdout

	// Set log level
	var level zerolog.Level
	switch config.Level {
	case types.LogLevelDebug:
		level = zerolog.DebugLevel
	case types.LogLevelInfo:
		level = zerolog.InfoLevel
	case types.LogLevelWarn:
		level = zerolog.WarnLevel
	case types.LogLevelError:
		level = zerolog.ErrorLevel
	case types.LogLevelFatal:
		level = zerolog.FatalLevel
	default:
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Set format
	if config.Format == types.LogFormatPretty {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	// Determine service name (default to "ws-server" for backwards compatibility)
	serviceName := config.ServiceName
	if serviceName == "" {
		serviceName = "ws-server"
	}

	// Create logger with timestamp and caller info
	logger := zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Str("service", serviceName).
		Logger()

	return logger
}

// LogError logs an error with full context and optional stack trace
//
// Parameters:
//   - logger: The zerolog logger
//   - err: The error to log
//   - msg: Human-readable message
//   - fields: Additional context fields (key-value pairs)
//
// Example:
//
//	LogError(logger, err, "Failed to broadcast", map[string]any{
//	    "client_id": client.id,
//	    "message_size": len(data),
//	})
func LogError(logger zerolog.Logger, err error, msg string, fields map[string]any) {
	event := logger.Error().Err(err)

	// Add all context fields
	for k, v := range fields {
		event = event.Interface(k, v)
	}

	event.Msg(msg)
}

// LogErrorWithStack logs an error with stack trace
//
// Use this for unexpected errors, panics recovered, or critical failures
// where you need to understand the full call stack.
//
// Example:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        LogErrorWithStack(logger, fmt.Errorf("panic: %v", r), "Worker panic recovered", nil)
//	    }
//	}()
func LogErrorWithStack(logger zerolog.Logger, err error, msg string, fields map[string]any) {
	stack := string(debug.Stack())

	event := logger.Error().Err(err).Str("stack_trace", stack)

	// Add all context fields
	for k, v := range fields {
		event = event.Interface(k, v)
	}

	event.Msg(msg)
}

// LogPanic logs a recovered panic with full stack trace
//
// Use in defer recover() blocks to log panics before re-panicking or gracefully handling.
//
// Example:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        LogPanic(logger, r, "Worker goroutine panic", map[string]any{
//	            "worker_id": id,
//	        })
//	    }
//	}()
func LogPanic(logger zerolog.Logger, panicValue any, msg string, fields map[string]any) {
	stack := string(debug.Stack())

	event := logger.Fatal().
		Interface("panic_value", panicValue).
		Str("stack_trace", stack)

	// Add all context fields
	for k, v := range fields {
		event = event.Interface(k, v)
	}

	event.Msg(msg)
}

// RecoverPanic is a helper for goroutine panic recovery that logs but doesn't exit.
//
// Deprecated: Use pkg/logging.RecoverPanic instead. This function remains for backward
// compatibility and will be removed when internal/monitoring is deleted.
//
// CRITICAL: Use this in ALL goroutine defer blocks to catch panics that would otherwise
// crash the entire process. This is for DEBUGGING - logs panic details but keeps server running.
//
// Example:
//
//	go func() {
//	    defer logging.RecoverPanic(logger, "writePump", map[string]any{"client_id": id})
//	    // ... goroutine work ...
//	}()
func RecoverPanic(logger zerolog.Logger, goroutineName string, fields map[string]any) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())

		// Use Error instead of Fatal so we log but don't exit
		// This lets us see WHICH goroutine panicked and WHY
		event := logger.Error().
			Str("goroutine", goroutineName).
			Interface("panic_value", r).
			Str("stack_trace", stack).
			Str("recovery_mode", "captured_panic_continuing_execution")

		// Add all context fields
		for k, v := range fields {
			event = event.Interface(k, v)
		}

		event.Msg("🚨 GOROUTINE PANIC RECOVERED - This would have crashed the server!")
	}
}

// InitGlobalLogger initializes the global logger
// This should be called once at application startup
func InitGlobalLogger(config LoggerConfig) {
	logger := NewLogger(config)
	log.Logger = logger
}

// PanicLogger is the minimal interface required for panic recovery logging.
type PanicLogger interface {
	Error() PanicLogEvent
}

// PanicLogEvent is the minimal log event interface for panic recovery.
type PanicLogEvent interface {
	Str(key string, val string) PanicLogEvent
	Interface(key string, val any) PanicLogEvent
	Msg(msg string)
}

// RecoverPanicAny is like RecoverPanic but accepts any logger implementing PanicLogger.
// Use this for testable code with injected loggers.
func RecoverPanicAny(logger PanicLogger, goroutineName string, fields map[string]any) {
	if r := recover(); r != nil {
		if logger == nil {
			// No logger, just swallow the panic in tests
			return
		}

		stack := string(debug.Stack())

		event := logger.Error().
			Str("goroutine", goroutineName).
			Interface("panic_value", r).
			Str("stack_trace", stack).
			Str("recovery_mode", "captured_panic_continuing_execution")

		for k, v := range fields {
			event = event.Interface(k, v)
		}

		event.Msg("GOROUTINE PANIC RECOVERED - This would have crashed the server!")
	}
}
