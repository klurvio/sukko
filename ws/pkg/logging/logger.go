package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// LoggerConfig holds logger configuration.
type LoggerConfig struct {
	Level       LogLevel  // Minimum log level
	Format      LogFormat // Output format
	ServiceName string    // Service name for log identification (e.g., "ws-server", "ws-gateway")
}

// NewLogger creates a structured logger configured for Loki integration.
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
	case LogLevelDebug:
		level = zerolog.DebugLevel
	case LogLevelInfo:
		level = zerolog.InfoLevel
	case LogLevelWarn:
		level = zerolog.WarnLevel
	case LogLevelError:
		level = zerolog.ErrorLevel
	case LogLevelFatal:
		level = zerolog.FatalLevel
	default:
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Set format
	if config.Format == LogFormatPretty {
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

// InitGlobalLogger initializes the global logger.
// This should be called once at application startup.
func InitGlobalLogger(config LoggerConfig) {
	logger := NewLogger(config)
	log.Logger = logger
}
