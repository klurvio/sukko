package logging

import (
	"runtime/debug"

	"github.com/rs/zerolog"
)

// LogError logs an error with full context and optional stack trace.
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

// LogErrorWithStack logs an error with stack trace.
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
