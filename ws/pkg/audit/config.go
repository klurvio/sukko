package audit

import (
	"io"
	"os"

	"github.com/Toniq-Labs/odin-ws/pkg/alerting"
)

// Config holds configuration for creating an audit logger.
type Config struct {
	// MinLevel is the minimum level that gets logged.
	// Default: INFO
	MinLevel alerting.Level

	// Writer is the output destination for logs.
	// Default: os.Stdout
	Writer io.Writer

	// Alerter is an optional alerter for WARNING+ events.
	// If nil, no alerts are sent.
	Alerter alerting.Alerter
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		MinLevel: alerting.INFO,
		Writer:   os.Stdout,
		Alerter:  nil,
	}
}

// ConfigFromEnv creates a Config from environment variables.
//
// Environment variables:
//   - AUDIT_MIN_LEVEL: Minimum level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
//   - AUDIT_ENABLED: "true" to enable (default: true)
//
// Note: Alerter must be set programmatically via SetAlerter or Config.Alerter
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	if level := os.Getenv("AUDIT_MIN_LEVEL"); level != "" {
		cfg.MinLevel = alerting.ParseLevel(level)
	}

	return cfg
}

// NewFromEnv creates an audit Logger from environment variables.
func NewFromEnv() *Logger {
	return New(ConfigFromEnv())
}

// NewFromEnvWithAlerter creates an audit Logger from environment variables
// with the provided alerter for WARNING+ events.
func NewFromEnvWithAlerter(alerter alerting.Alerter) *Logger {
	cfg := ConfigFromEnv()
	cfg.Alerter = alerter
	return New(cfg)
}
