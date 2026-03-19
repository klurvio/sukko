package platform

import (
	"strings"
	"testing"
	"time"
)

// newValidGatewayConfig returns a gateway config with all valid defaults for testing.
func newValidGatewayConfig() *GatewayConfig {
	return &GatewayConfig{
		BaseConfig: BaseConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			Environment: "test",
		},
		AuthConfig: AuthConfig{
			AuthEnabled: false, // Disabled by default for tests
		},
		ProvisioningClientConfig: ProvisioningClientConfig{
			ProvisioningGRPCAddr:  "localhost:9090",
			GRPCReconnectDelay:    1 * time.Second,
			GRPCReconnectMaxDelay: 30 * time.Second,
		},
		Port:                         3000,
		ReadTimeout:                  15 * time.Second,
		WriteTimeout:                 15 * time.Second,
		IdleTimeout:                  60 * time.Second,
		BackendURL:                   "ws://localhost:3001/ws",
		DialTimeout:                  10 * time.Second,
		MessageTimeout:               60 * time.Second,
		DefaultTenantID:              "sukko", // Required when auth disabled
		RequireTenantID:              true,
		PublicPatterns:               []string{"*.trade"},
		UserScopedPatterns:           []string{"balances.{principal}"},
		GroupScopedPatterns:          []string{"community.{group_id}"},
		MaxFrameSize:                 1048576,
		RateLimitEnabled:             true,
		RateLimitBurst:               100,
		RateLimitRate:                10.0,
		PublishRateLimit:             10.0,
		PublishBurst:                 100,
		MaxPublishSize:               65536,
		TenantConnectionLimitEnabled: true,
		DefaultTenantConnectionLimit: 1000,
		AuthRefreshRateInterval:      30 * time.Second,
		AuthValidationTimeout:        5 * time.Second,
		ShutdownTimeout:              30 * time.Second,
		IssuerCacheTTL:               5 * time.Minute,
		ChannelRulesCacheTTL:         1 * time.Minute,
		RegistryQueryTimeout:         5 * time.Second,
		OIDCKeyfuncCacheTTL:          1 * time.Hour,
		JWKSFetchTimeout:             10 * time.Second,
		JWKSRefreshInterval:          1 * time.Hour,
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
			cfg.AuthEnabled = true   // GRPC validation runs when auth is enabled
			cfg.DefaultTenantID = "" // Not required when auth is enabled
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
	validFormats := []string{"json", "pretty"}
	invalidFormats := []string{"JSON", "xml", "text", "", "console"}

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

func TestGatewayConfig_Validate_MaxFrameSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		size        int
		shouldError bool
	}{
		{"valid default 1MB", 1048576, false},
		{"valid min 1KB", 1024, false},
		{"valid max 10MB", 10 * 1024 * 1024, false},
		{"zero is too small", 0, true},
		{"too small", 512, true},
		{"too large", 10*1024*1024 + 1, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.MaxFrameSize = tt.size
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

func TestGatewayConfig_Validate_HTTPTimeouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		field         string
		value         time.Duration
		shouldError   bool
		errorContains string
	}{
		{"read_valid", "read", 15 * time.Second, false, ""},
		{"read_zero", "read", 0, true, "GATEWAY_READ_TIMEOUT"},
		{"read_too_large", "read", 121 * time.Second, true, "GATEWAY_READ_TIMEOUT"},
		{"write_valid", "write", 15 * time.Second, false, ""},
		{"write_zero", "write", 0, true, "GATEWAY_WRITE_TIMEOUT"},
		{"write_too_large", "write", 121 * time.Second, true, "GATEWAY_WRITE_TIMEOUT"},
		{"idle_valid", "idle", 60 * time.Second, false, ""},
		{"idle_zero", "idle", 0, true, "GATEWAY_IDLE_TIMEOUT"},
		{"idle_too_large", "idle", 301 * time.Second, true, "GATEWAY_IDLE_TIMEOUT"},
		{"idle_max", "idle", 300 * time.Second, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			switch tt.field {
			case "read":
				cfg.ReadTimeout = tt.value
			case "write":
				cfg.WriteTimeout = tt.value
			case "idle":
				cfg.IdleTimeout = tt.value
			}
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should contain %q: %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_DialTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       time.Duration
		shouldError bool
	}{
		{"valid", 10 * time.Second, false},
		{"valid min", 1 * time.Second, false},
		{"valid max", 60 * time.Second, false},
		{"zero", 0, true},
		{"too large", 61 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.DialTimeout = tt.value
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

func TestGatewayConfig_Validate_MessageTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       time.Duration
		shouldError bool
	}{
		{"valid default", 60 * time.Second, false},
		{"valid min", 1 * time.Second, false},
		{"valid max", 300 * time.Second, false},
		{"zero", 0, true},
		{"negative", -1 * time.Second, true},
		{"too large", 301 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.MessageTimeout = tt.value
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

func TestGatewayConfig_Validate_RateLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		enabled     bool
		burst       int
		rate        float64
		shouldError bool
	}{
		{"enabled valid", true, 100, 10.0, false},
		{"enabled burst zero", true, 0, 10.0, true},
		{"enabled rate zero", true, 100, 0, true},
		{"enabled rate negative", true, 100, -1.0, true},
		{"disabled zero burst ok", false, 0, 0, false},
		{"disabled zero rate ok", false, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.RateLimitEnabled = tt.enabled
			cfg.RateLimitBurst = tt.burst
			cfg.RateLimitRate = tt.rate
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

func TestGatewayConfig_Validate_PublishSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		rateLimit     float64
		burst         int
		maxSize       int
		shouldError   bool
		errorContains string
	}{
		{"valid defaults", 10.0, 100, 65536, false, ""},
		{"burst zero rejected", 10.0, 0, 65536, true, "GATEWAY_PUBLISH_BURST"},
		{"rate zero rejected", 0, 100, 65536, true, "GATEWAY_PUBLISH_RATE_LIMIT"},
		{"rate negative", -1.0, 100, 65536, true, "GATEWAY_PUBLISH_RATE_LIMIT"},
		{"size too small", 10.0, 100, 512, true, "GATEWAY_MAX_PUBLISH_SIZE"},
		{"size too large", 10.0, 100, 10*1024*1024 + 1, true, "GATEWAY_MAX_PUBLISH_SIZE"},
		{"size valid min", 10.0, 100, 1024, false, ""},
		{"size valid max", 10.0, 100, 10 * 1024 * 1024, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.PublishRateLimit = tt.rateLimit
			cfg.PublishBurst = tt.burst
			cfg.MaxPublishSize = tt.maxSize
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should contain %q: %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_JWKSRefreshInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		enabled     bool
		interval    time.Duration
		shouldError bool
	}{
		{"enabled valid", true, 1 * time.Hour, false},
		{"enabled min valid", true, 1 * time.Minute, false},
		{"enabled too small", true, 30 * time.Second, true},
		{"disabled zero ok", false, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.MultiIssuerOIDCEnabled = tt.enabled
			cfg.JWKSRefreshInterval = tt.interval
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

func TestGatewayConfig_Validate_TenantConnectionLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		enabled     bool
		limit       int
		shouldError bool
	}{
		{"enabled valid", true, 1000, false},
		{"enabled min valid", true, 1, false},
		{"enabled zero", true, 0, true},
		{"enabled negative", true, -1, true},
		{"disabled zero ok", false, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.TenantConnectionLimitEnabled = tt.enabled
			cfg.DefaultTenantConnectionLimit = tt.limit
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

func TestGatewayConfig_Validate_AuthValidationTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		timeout     time.Duration
		shouldError bool
	}{
		{"valid", 5 * time.Second, false},
		{"zero", 0, true},
		{"negative", -1 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.AuthValidationTimeout = tt.timeout
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			} else if tt.shouldError && !strings.Contains(err.Error(), "GATEWAY_AUTH_VALIDATION_TIMEOUT") {
				t.Errorf("Error should mention GATEWAY_AUTH_VALIDATION_TIMEOUT: %v", err)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_ShutdownTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		timeout     time.Duration
		shouldError bool
	}{
		{"valid", 30 * time.Second, false},
		{"zero", 0, true},
		{"negative", -1 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			cfg.ShutdownTimeout = tt.timeout
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			} else if tt.shouldError && !strings.Contains(err.Error(), "GATEWAY_SHUTDOWN_TIMEOUT") {
				t.Errorf("Error should mention GATEWAY_SHUTDOWN_TIMEOUT: %v", err)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_CacheTTLAndRegistryTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setField      func(*GatewayConfig)
		shouldError   bool
		errorContains string
	}{
		{"issuer cache TTL valid", func(c *GatewayConfig) { c.IssuerCacheTTL = 5 * time.Minute }, false, ""},
		{"issuer cache TTL zero", func(c *GatewayConfig) { c.IssuerCacheTTL = 0 }, true, "GATEWAY_ISSUER_CACHE_TTL"},
		{"channel rules cache TTL valid", func(c *GatewayConfig) { c.ChannelRulesCacheTTL = 1 * time.Minute }, false, ""},
		{"channel rules cache TTL zero", func(c *GatewayConfig) { c.ChannelRulesCacheTTL = 0 }, true, "GATEWAY_CHANNEL_RULES_CACHE_TTL"},
		{"registry query timeout valid", func(c *GatewayConfig) { c.RegistryQueryTimeout = 5 * time.Second }, false, ""},
		{"registry query timeout zero", func(c *GatewayConfig) { c.RegistryQueryTimeout = 0 }, true, "GATEWAY_REGISTRY_QUERY_TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidGatewayConfig()
			tt.setField(cfg)
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should contain %q: %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}
