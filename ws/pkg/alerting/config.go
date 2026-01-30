package alerting

import (
	"os"
	"strconv"
	"time"
)

// Config holds configuration for creating alerters.
type Config struct {
	// Enabled determines if alerting is active.
	Enabled bool

	// MinLevel is the minimum level that triggers alerts.
	MinLevel Level

	// Slack configuration
	SlackWebhookURL string
	SlackChannel    string
	SlackUsername   string

	// Service context
	ServiceName string
	Environment string

	// Rate limiting
	RateLimitWindow time.Duration
	RateLimitMax    int

	// ConsoleEnabled outputs alerts to console (for development).
	ConsoleEnabled bool
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		MinLevel:        WARNING,
		SlackUsername:   "AlertBot",
		RateLimitWindow: 5 * time.Minute,
		RateLimitMax:    3,
		ConsoleEnabled:  false,
	}
}

// ConfigFromEnv creates a Config from environment variables.
//
// Environment variables:
//   - ALERT_ENABLED: "true" to enable alerting
//   - ALERT_MIN_LEVEL: Minimum level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
//   - ALERT_SLACK_WEBHOOK_URL: Slack webhook URL
//   - ALERT_SLACK_CHANNEL: Slack channel (e.g., "#alerts")
//   - ALERT_SLACK_USERNAME: Slack username (default: "AlertBot")
//   - ALERT_SERVICE_NAME: Service name for context
//   - ALERT_ENVIRONMENT: Environment name (e.g., "production")
//   - ALERT_RATE_LIMIT_WINDOW: Rate limit window (e.g., "5m")
//   - ALERT_RATE_LIMIT_MAX: Max alerts per window (e.g., "3")
//   - ALERT_CONSOLE_ENABLED: "true" to also output to console
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	if os.Getenv("ALERT_ENABLED") == "true" {
		cfg.Enabled = true
	}

	if level := os.Getenv("ALERT_MIN_LEVEL"); level != "" {
		cfg.MinLevel = ParseLevel(level)
	}

	if url := os.Getenv("ALERT_SLACK_WEBHOOK_URL"); url != "" {
		cfg.SlackWebhookURL = url
	}

	if channel := os.Getenv("ALERT_SLACK_CHANNEL"); channel != "" {
		cfg.SlackChannel = channel
	}

	if username := os.Getenv("ALERT_SLACK_USERNAME"); username != "" {
		cfg.SlackUsername = username
	}

	if service := os.Getenv("ALERT_SERVICE_NAME"); service != "" {
		cfg.ServiceName = service
	}

	if env := os.Getenv("ALERT_ENVIRONMENT"); env != "" {
		cfg.Environment = env
	} else if env := os.Getenv("ENVIRONMENT"); env != "" {
		cfg.Environment = env
	}

	if window := os.Getenv("ALERT_RATE_LIMIT_WINDOW"); window != "" {
		if d, err := time.ParseDuration(window); err == nil {
			cfg.RateLimitWindow = d
		}
	}

	if maxStr := os.Getenv("ALERT_RATE_LIMIT_MAX"); maxStr != "" {
		if n, err := strconv.Atoi(maxStr); err == nil {
			cfg.RateLimitMax = n
		}
	}

	if os.Getenv("ALERT_CONSOLE_ENABLED") == "true" {
		cfg.ConsoleEnabled = true
	}

	return cfg
}

// NewFromEnv creates an Alerter from environment variables.
// Returns a NoopAlerter if alerting is disabled.
func NewFromEnv() Alerter {
	return NewFromConfig(ConfigFromEnv())
}

// NewFromConfig creates an Alerter from a Config.
// Returns a NoopAlerter if alerting is disabled.
func NewFromConfig(cfg Config) Alerter {
	if !cfg.Enabled {
		return &NoopAlerter{}
	}

	var alerters []Alerter

	// Add Slack alerter if configured
	if cfg.SlackWebhookURL != "" {
		slack := NewSlackAlerterWithConfig(SlackConfig{
			WebhookURL:  cfg.SlackWebhookURL,
			Channel:     cfg.SlackChannel,
			Username:    cfg.SlackUsername,
			ServiceName: cfg.ServiceName,
			Environment: cfg.Environment,
		})
		alerters = append(alerters, slack)
	}

	// Add console alerter if enabled
	if cfg.ConsoleEnabled {
		alerters = append(alerters, NewConsoleAlerter())
	}

	// If no alerters configured, return noop
	if len(alerters) == 0 {
		return &NoopAlerter{}
	}

	// Combine alerters
	var alerter Alerter
	if len(alerters) == 1 {
		alerter = alerters[0]
	} else {
		alerter = NewMultiAlerter(alerters...)
	}

	// Wrap with rate limiting
	return NewRateLimitedAlerterWithConfig(alerter, RateLimitConfig{
		Window: cfg.RateLimitWindow,
		Max:    cfg.RateLimitMax,
	})
}
