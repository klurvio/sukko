package logging

import (
	"runtime/debug"

	"github.com/rs/zerolog"
)

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

// LogPanic logs a recovered panic with full stack trace.
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

		event.Msg("GOROUTINE PANIC RECOVERED - This would have crashed the server!")
	}
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
