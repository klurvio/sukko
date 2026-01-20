package platform

import (
	"strings"
	"testing"
	"time"
)

// newValidGatewayConfig returns a gateway config with all valid defaults for testing.
func newValidGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		Port:                3000,
		ReadTimeout:         15 * time.Second,
		WriteTimeout:        15 * time.Second,
		IdleTimeout:         60 * time.Second,
		BackendURL:          "ws://localhost:3001/ws",
		DialTimeout:         10 * time.Second,
		MessageTimeout:      60 * time.Second,
		AuthEnabled:         true,
		JWTSecret:           "test-secret-key-at-least-32-bytes!!",
		PublicPatterns:      []string{"*.trade"},
		UserScopedPatterns:  []string{"balances.{principal}"},
		GroupScopedPatterns: []string{"community.{group_id}"},
		RateLimitEnabled:    true,
		RateLimitBurst:      100,
		RateLimitRate:       10.0,
		LogLevel:            "info",
		LogFormat:           "json",
		Environment:         "test",
	}
}

func TestGatewayConfig_Validate_Valid(t *testing.T) {
	cfg := newValidGatewayConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}
}

func TestGatewayConfig_Validate_AuthDisabled(t *testing.T) {
	cfg := newValidGatewayConfig()
	cfg.AuthEnabled = false
	cfg.JWTSecret = "" // Should be OK when auth disabled

	if err := cfg.Validate(); err != nil {
		t.Errorf("Auth disabled config should not error: %v", err)
	}
}

func TestGatewayConfig_Validate_Port(t *testing.T) {
	tests := []struct {
		name        string
		port        int
		shouldError bool
	}{
		{"valid min", 1, false},
		{"valid max", 65535, false},
		{"valid common", 3000, false},
		{"zero", 0, true},
		{"negative", -1, true},
		{"too large", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.Port = tt.port
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_JWTSecret(t *testing.T) {
	tests := []struct {
		name        string
		authEnabled bool
		secret      string
		shouldError bool
		errorField  string
	}{
		{"valid 32+ chars", true, "test-secret-key-at-least-32-bytes!!", false, ""},
		{"exactly 32 chars", true, "12345678901234567890123456789012", false, ""},
		{"empty when auth enabled", true, "", true, "JWT_SECRET"},
		{"too short when auth enabled", true, "short", true, "JWT_SECRET"},
		{"31 chars", true, "1234567890123456789012345678901", true, "JWT_SECRET"},
		{"empty when auth disabled", false, "", false, ""},
		{"short when auth disabled", false, "short", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.AuthEnabled = tt.authEnabled
			cfg.JWTSecret = tt.secret

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorField != "" && !strings.Contains(err.Error(), tt.errorField) {
					t.Errorf("Error should mention %s: %v", tt.errorField, err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestGatewayConfig_Validate_BackendURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		shouldError bool
	}{
		{"valid ws", "ws://localhost:3001/ws", false},
		{"valid wss", "wss://example.com/ws", false},
		{"valid with port", "ws://127.0.0.1:8080/websocket", false},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.BackendURL = tt.url
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_PublicPatterns(t *testing.T) {
	tests := []struct {
		name        string
		patterns    []string
		shouldError bool
	}{
		{"single pattern", []string{"*.trade"}, false},
		{"multiple patterns", []string{"*.trade", "*.liquidity", "odin.*"}, false},
		{"empty slice", []string{}, true},
		{"nil slice", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.PublicPatterns = tt.patterns
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_LogLevel(t *testing.T) {
	validLevels := []string{"debug", "info", "warn", "error"}
	invalidLevels := []string{"DEBUG", "INFO", "invalid", "", "trace"}

	for _, level := range validLevels {
		t.Run("valid_"+level, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.LogLevel = level
			if err := cfg.Validate(); err != nil {
				t.Errorf("%s should be valid: %v", level, err)
			}
		})
	}

	for _, level := range invalidLevels {
		t.Run("invalid_"+level, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.LogLevel = level
			err := cfg.Validate()
			if err == nil {
				t.Errorf("%s should be invalid", level)
			}
			if !strings.Contains(err.Error(), "LOG_LEVEL") {
				t.Errorf("Error should mention LOG_LEVEL: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_LogFormat(t *testing.T) {
	validFormats := []string{"json", "text", "pretty"}
	invalidFormats := []string{"JSON", "xml", "", "console"}

	for _, format := range validFormats {
		t.Run("valid_"+format, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.LogFormat = format
			if err := cfg.Validate(); err != nil {
				t.Errorf("%s should be valid: %v", format, err)
			}
		})
	}

	for _, format := range invalidFormats {
		t.Run("invalid_"+format, func(t *testing.T) {
			cfg := newValidGatewayConfig()
			cfg.LogFormat = format
			err := cfg.Validate()
			if err == nil {
				t.Errorf("%s should be invalid", format)
			}
		})
	}
}
