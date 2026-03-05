package platform

import (
	"strings"
	"testing"
	"time"
)

// newValidGatewayConfig returns a gateway config with all valid defaults for testing.
func newValidGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		Port:                  3000,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
		BackendURL:            "ws://localhost:3001/ws",
		DialTimeout:           10 * time.Second,
		MessageTimeout:        60 * time.Second,
		AuthEnabled:           false,  // Disabled by default for tests
		DefaultTenantID:       "sukko", // Required when auth disabled
		ProvisioningGRPCAddr:  "localhost:9090",
		GRPCReconnectDelay:    1 * time.Second,
		GRPCReconnectMaxDelay: 30 * time.Second,
		RequireTenantID:       true,
		PublicPatterns:        []string{"*.trade"},
		UserScopedPatterns:    []string{"balances.{principal}"},
		GroupScopedPatterns:   []string{"community.{group_id}"},
		RateLimitEnabled:      true,
		RateLimitBurst:        100,
		RateLimitRate:         10.0,
		AuthRefreshRateInterval: 30 * time.Second,
		OIDCKeyfuncCacheTTL:    1 * time.Hour,
		JWKSFetchTimeout:       10 * time.Second,
		JWKSRefreshInterval:    1 * time.Hour,
		LogLevel:               "info",
		LogFormat:              "json",
		Environment:            "test",
	}
}

func TestGatewayConfig_Validate_Valid(t *testing.T) {
	t.Parallel()
	cfg := newValidGatewayConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}
}

func TestGatewayConfig_Validate_AuthDisabled(t *testing.T) {
	t.Parallel()
	cfg := newValidGatewayConfig()
	cfg.AuthEnabled = false
	cfg.ProvisioningGRPCAddr = "" // Should be OK when auth disabled

	if err := cfg.Validate(); err != nil {
		t.Errorf("Auth disabled config should not error: %v", err)
	}
}

func TestGatewayConfig_Validate_AuthDisabled_RequiresDefaultTenantID(t *testing.T) {
	t.Parallel()
	cfg := newValidGatewayConfig()
	cfg.AuthEnabled = false
	cfg.DefaultTenantID = "" // Empty should fail

	err := cfg.Validate()
	if err == nil {
		t.Error("Should error when auth disabled without DefaultTenantID")
	}
	if !strings.Contains(err.Error(), "DEFAULT_TENANT_ID") {
		t.Errorf("Error should mention DEFAULT_TENANT_ID: %v", err)
	}
}

func TestGatewayConfig_Validate_AuthEnabled_RequiresGRPC(t *testing.T) {
	t.Parallel()
	cfg := newValidGatewayConfig()
	cfg.AuthEnabled = true
	cfg.ProvisioningGRPCAddr = "" // Missing gRPC addr

	err := cfg.Validate()
	if err == nil {
		t.Error("Should error when auth enabled without gRPC addr")
	}
	if !strings.Contains(err.Error(), "PROVISIONING_GRPC_ADDR") {
		t.Errorf("Error should mention PROVISIONING_GRPC_ADDR: %v", err)
	}
}

func TestGatewayConfig_Validate_AuthEnabled_WithGRPC(t *testing.T) {
	t.Parallel()
	cfg := newValidGatewayConfig()
	cfg.AuthEnabled = true
	cfg.ProvisioningGRPCAddr = "localhost:9090"

	if err := cfg.Validate(); err != nil {
		t.Errorf("Auth enabled with gRPC addr should not error: %v", err)
	}
}

func TestGatewayConfig_Validate_Port(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestGatewayConfig_Validate_GRPCReconnectSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		delay         time.Duration
		maxDelay      time.Duration
		shouldError   bool
		errorContains string
	}{
		{"valid defaults", 1 * time.Second, 30 * time.Second, false, ""},
		{"delay too small", 50 * time.Millisecond, 30 * time.Second, true, "PROVISIONING_GRPC_RECONNECT_DELAY"},
		{"max delay < delay", 5 * time.Second, 1 * time.Second, true, "PROVISIONING_GRPC_RECONNECT_MAX_DELAY"},
		{"equal values", 5 * time.Second, 5 * time.Second, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.GRPCReconnectDelay = tt.delay
			cfg.GRPCReconnectMaxDelay = tt.maxDelay
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should contain %q: %v", tt.errorContains, err)
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
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
	tests := []struct {
		name        string
		patterns    []string
		shouldError bool
	}{
		{"single pattern", []string{"*.trade"}, false},
		{"multiple patterns", []string{"*.trade", "*.liquidity", "sukko.*"}, false},
		{"empty slice", []string{}, true},
		{"nil slice", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	validLevels := []string{"debug", "info", "warn", "error"}
	invalidLevels := []string{"DEBUG", "INFO", "invalid", "", "trace"}

	for _, level := range validLevels {
		t.Run("valid_"+level, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.LogLevel = level
			if err := cfg.Validate(); err != nil {
				t.Errorf("%s should be valid: %v", level, err)
			}
		})
	}

	for _, level := range invalidLevels {
		t.Run("invalid_"+level, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	validFormats := []string{"json", "text", "pretty"}
	invalidFormats := []string{"JSON", "xml", "", "console"}

	for _, format := range validFormats {
		t.Run("valid_"+format, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.LogFormat = format
			if err := cfg.Validate(); err != nil {
				t.Errorf("%s should be valid: %v", format, err)
			}
		})
	}

	for _, format := range invalidFormats {
		t.Run("invalid_"+format, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.LogFormat = format
			err := cfg.Validate()
			if err == nil {
				t.Errorf("%s should be invalid", format)
			}
		})
	}
}
