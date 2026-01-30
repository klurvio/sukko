package audit

import (
	"testing"

	"github.com/Toniq-Labs/odin-ws/pkg/alerting"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	if cfg.MinLevel != alerting.INFO {
		t.Errorf("Default min level should be INFO, got %s", cfg.MinLevel)
	}
	if cfg.Writer == nil {
		t.Error("Default writer should not be nil")
	}
	if cfg.Alerter != nil {
		t.Error("Default alerter should be nil")
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("AUDIT_MIN_LEVEL", "ERROR")

	cfg := ConfigFromEnv()

	if cfg.MinLevel != alerting.ERROR {
		t.Errorf("MinLevel should be ERROR, got %s", cfg.MinLevel)
	}
}

func TestConfigFromEnv_DefaultLevel(t *testing.T) {
	// Don't set AUDIT_MIN_LEVEL
	t.Setenv("AUDIT_MIN_LEVEL", "")

	cfg := ConfigFromEnv()

	if cfg.MinLevel != alerting.INFO {
		t.Errorf("Default MinLevel should be INFO, got %s", cfg.MinLevel)
	}
}

func TestConfigFromEnv_AllLevels(t *testing.T) {
	tests := []struct {
		envValue string
		expected alerting.Level
	}{
		{"DEBUG", alerting.DEBUG},
		{"INFO", alerting.INFO},
		{"WARNING", alerting.WARNING},
		{"ERROR", alerting.ERROR},
		{"CRITICAL", alerting.CRITICAL},
		{"debug", alerting.DEBUG},
		{"warning", alerting.WARNING},
	}

	for _, tt := range tests {
		t.Run(tt.envValue, func(t *testing.T) {
			t.Setenv("AUDIT_MIN_LEVEL", tt.envValue)

			cfg := ConfigFromEnv()

			if cfg.MinLevel != tt.expected {
				t.Errorf("MinLevel for %s: got %s, want %s", tt.envValue, cfg.MinLevel, tt.expected)
			}
		})
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv("AUDIT_MIN_LEVEL", "WARNING")

	logger := NewFromEnv()

	if logger == nil {
		t.Fatal("NewFromEnv should return non-nil")
	}
	if logger.minLevel != alerting.WARNING {
		t.Errorf("minLevel: got %s, want WARNING", logger.minLevel)
	}
}

func TestNewFromEnvWithAlerter(t *testing.T) {
	t.Setenv("AUDIT_MIN_LEVEL", "ERROR")

	alerter := &mockAlerter{}
	logger := NewFromEnvWithAlerter(alerter)

	if logger == nil {
		t.Fatal("NewFromEnvWithAlerter should return non-nil")
	}
	if logger.minLevel != alerting.ERROR {
		t.Errorf("minLevel: got %s, want ERROR", logger.minLevel)
	}
	if logger.alerter == nil {
		t.Error("alerter should be set")
	}
}
