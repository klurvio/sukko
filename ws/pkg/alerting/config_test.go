package alerting

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("Default should be disabled")
	}
	if cfg.MinLevel != WARNING {
		t.Errorf("Default min level should be WARNING, got %s", cfg.MinLevel)
	}
	if cfg.SlackUsername != "AlertBot" {
		t.Errorf("Default username should be AlertBot, got %s", cfg.SlackUsername)
	}
	if cfg.RateLimitWindow != 5*time.Minute {
		t.Errorf("Default rate limit window should be 5m, got %v", cfg.RateLimitWindow)
	}
	if cfg.RateLimitMax != 3 {
		t.Errorf("Default rate limit max should be 3, got %d", cfg.RateLimitMax)
	}
}

func TestConfigFromEnv(t *testing.T) {
	// Use t.Setenv which automatically restores env vars after test
	// Note: t.Setenv calls t.Parallel() internally so we don't call it here
	t.Setenv("ALERT_ENABLED", "true")
	t.Setenv("ALERT_MIN_LEVEL", "ERROR")
	t.Setenv("ALERT_SLACK_WEBHOOK_URL", "https://hooks.slack.com/test")
	t.Setenv("ALERT_SLACK_CHANNEL", "#test-alerts")
	t.Setenv("ALERT_SLACK_USERNAME", "TestBot")
	t.Setenv("ALERT_SERVICE_NAME", "ws-server")
	t.Setenv("ALERT_ENVIRONMENT", "staging")
	t.Setenv("ALERT_RATE_LIMIT_WINDOW", "10m")
	t.Setenv("ALERT_RATE_LIMIT_MAX", "5")
	t.Setenv("ALERT_CONSOLE_ENABLED", "true")

	cfg := ConfigFromEnv()

	if !cfg.Enabled {
		t.Error("Should be enabled")
	}
	if cfg.MinLevel != ERROR {
		t.Errorf("MinLevel should be ERROR, got %s", cfg.MinLevel)
	}
	if cfg.SlackWebhookURL != "https://hooks.slack.com/test" {
		t.Errorf("SlackWebhookURL mismatch: %s", cfg.SlackWebhookURL)
	}
	if cfg.SlackChannel != "#test-alerts" {
		t.Errorf("SlackChannel mismatch: %s", cfg.SlackChannel)
	}
	if cfg.SlackUsername != "TestBot" {
		t.Errorf("SlackUsername mismatch: %s", cfg.SlackUsername)
	}
	if cfg.ServiceName != "ws-server" {
		t.Errorf("ServiceName mismatch: %s", cfg.ServiceName)
	}
	if cfg.Environment != "staging" {
		t.Errorf("Environment mismatch: %s", cfg.Environment)
	}
	if cfg.RateLimitWindow != 10*time.Minute {
		t.Errorf("RateLimitWindow should be 10m, got %v", cfg.RateLimitWindow)
	}
	if cfg.RateLimitMax != 5 {
		t.Errorf("RateLimitMax should be 5, got %d", cfg.RateLimitMax)
	}
	if !cfg.ConsoleEnabled {
		t.Error("ConsoleEnabled should be true")
	}
}

func TestConfigFromEnv_FallbackToEnvironment(t *testing.T) {
	// Clear ALERT_ENVIRONMENT and set ENVIRONMENT
	t.Setenv("ALERT_ENVIRONMENT", "")
	t.Setenv("ENVIRONMENT", "production")

	cfg := ConfigFromEnv()

	if cfg.Environment != "production" {
		t.Errorf("Should fall back to ENVIRONMENT, got %s", cfg.Environment)
	}
}

func TestNewFromEnv_Disabled(t *testing.T) {
	t.Setenv("ALERT_ENABLED", "false")

	alerter := NewFromEnv()

	// Should be NoopAlerter
	if _, ok := alerter.(*NoopAlerter); !ok {
		t.Errorf("Disabled alerter should be NoopAlerter, got %T", alerter)
	}
}

func TestNewFromConfig_NoopWhenDisabled(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Enabled = false

	alerter := NewFromConfig(cfg)

	if _, ok := alerter.(*NoopAlerter); !ok {
		t.Errorf("Should return NoopAlerter when disabled, got %T", alerter)
	}
}

func TestNewFromConfig_NoopWhenNoAlertersConfigured(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Enabled = true
	// No webhook URL, no console

	alerter := NewFromConfig(cfg)

	if _, ok := alerter.(*NoopAlerter); !ok {
		t.Errorf("Should return NoopAlerter when no alerters configured, got %T", alerter)
	}
}

func TestNewFromConfig_SlackOnly(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.SlackWebhookURL = "https://hooks.slack.com/test"

	alerter := NewFromConfig(cfg)

	// Should be rate limited slack alerter
	if _, ok := alerter.(*RateLimitedAlerter); !ok {
		t.Errorf("Should return RateLimitedAlerter, got %T", alerter)
	}
}

func TestNewFromConfig_ConsoleOnly(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ConsoleEnabled = true

	alerter := NewFromConfig(cfg)

	// Should be rate limited console alerter
	if _, ok := alerter.(*RateLimitedAlerter); !ok {
		t.Errorf("Should return RateLimitedAlerter, got %T", alerter)
	}
}

func TestNewFromConfig_MultipleAlerters(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.SlackWebhookURL = "https://hooks.slack.com/test"
	cfg.ConsoleEnabled = true

	alerter := NewFromConfig(cfg)

	// Should be rate limited multi alerter
	if rl, ok := alerter.(*RateLimitedAlerter); ok {
		if _, ok := rl.alerter.(*MultiAlerter); !ok {
			t.Errorf("Underlying alerter should be MultiAlerter, got %T", rl.alerter)
		}
	} else {
		t.Errorf("Should return RateLimitedAlerter, got %T", alerter)
	}
}
