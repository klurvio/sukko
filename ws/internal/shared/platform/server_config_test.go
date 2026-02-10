package platform

import (
	"strings"
	"testing"
	"time"
)

// newValidServerConfig returns a server config with all valid defaults for testing.
func newValidServerConfig() *ServerConfig {
	return &ServerConfig{
		Addr:                       ":3002",
		KafkaBrokers:               "localhost:19092",
		ConsumerGroup:              "test-group",
		MemoryLimit:                512 * 1024 * 1024,
		MaxConnections:             1000,
		MaxKafkaRate:               1000,
		MaxBroadcastRate:           100,
		MaxGoroutines:              1000,
		ConnectionRateLimitEnabled: true,
		ConnRateLimitIPBurst:       10,
		ConnRateLimitIPRate:        1.0,
		ConnRateLimitGlobalBurst:   300,
		ConnRateLimitGlobalRate:    50.0,
		CPURejectThreshold:         75.0,
		CPURejectThresholdLower:    65.0,
		CPUPauseThreshold:          80.0,
		CPUPauseThresholdLower:     70.0,
		TCPListenBacklog:           2048,
		HTTPReadTimeout:            15 * time.Second,
		HTTPWriteTimeout:           15 * time.Second,
		HTTPIdleTimeout:            60 * time.Second,
		MetricsInterval:            15 * time.Second,
		CPUPollInterval:            1 * time.Second,
		LogLevel:                   "info",
		LogFormat:                  "json",
		ValkeyAddrs:                []string{"localhost:26379"},
		ValkeyMasterName:           "mymaster",
		ValkeyChannel:              "ws.broadcast",
		ValkeyDB:                   0,
		ClientSendBufferSize:       512,
		SlowClientMaxAttempts:      3,
		// Multi-tenant consumer (required)
		ProvisioningDatabaseURL:    "postgres://user:pass@localhost:5432/provisioning?sslmode=disable",
		TopicRefreshInterval:       60 * time.Second,
		ProvisioningDBMaxOpenConns: 5,
		// WebSocket ping/pong (required)
		PongWait:   60 * time.Second,
		PingPeriod: 45 * time.Second,
		WriteWait:  5 * time.Second,
	}
}

func TestServerConfig_Validate_Valid(t *testing.T) {
	t.Parallel()
	cfg := newValidServerConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}
}

func TestServerConfig_Validate_EmptyAddr(t *testing.T) {
	t.Parallel()
	cfg := newValidServerConfig()
	cfg.Addr = ""

	err := cfg.Validate()
	if err == nil {
		t.Error("Should error on empty Addr")
	}
	if !strings.Contains(err.Error(), "WS_ADDR") {
		t.Errorf("Error should mention WS_ADDR: %v", err)
	}
}

func TestServerConfig_Validate_MaxConnections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int
		shouldError bool
	}{
		{"zero", 0, true},
		{"negative", -1, true},
		{"one", 1, false},
		{"large", 100000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MaxConnections = tt.value
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

func TestServerConfig_Validate_CPUThresholds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		reject      float64
		rejectLower float64
		pause       float64
		pauseLower  float64
		shouldError bool
		errorField  string
	}{
		{"valid defaults", 75.0, 65.0, 80.0, 70.0, false, ""},
		{"reject negative", -1.0, 65.0, 80.0, 70.0, true, "WS_CPU_REJECT_THRESHOLD"},
		{"reject > 100", 101.0, 65.0, 80.0, 70.0, true, "WS_CPU_REJECT_THRESHOLD"},
		{"pause < reject", 80.0, 70.0, 75.0, 65.0, true, "WS_CPU_PAUSE_THRESHOLD"},
		{"reject lower >= upper", 75.0, 75.0, 80.0, 70.0, true, "WS_CPU_REJECT_THRESHOLD_LOWER"},
		{"pause lower >= upper", 75.0, 65.0, 80.0, 80.0, true, "WS_CPU_PAUSE_THRESHOLD_LOWER"},
		{"pause lower negative", 75.0, 65.0, 80.0, -1.0, true, "WS_CPU_PAUSE_THRESHOLD_LOWER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.CPURejectThreshold = tt.reject
			cfg.CPURejectThresholdLower = tt.rejectLower
			cfg.CPUPauseThreshold = tt.pause
			cfg.CPUPauseThresholdLower = tt.pauseLower

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

func TestServerConfig_Validate_LogLevel(t *testing.T) {
	t.Parallel()
	validLevels := []string{"debug", "info", "warn", "error"}
	invalidLevels := []string{"DEBUG", "INFO", "invalid", "", "trace"}

	for _, level := range validLevels {
		t.Run("valid_"+level, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.LogLevel = level
			if err := cfg.Validate(); err != nil {
				t.Errorf("%s should be valid: %v", level, err)
			}
		})
	}

	for _, level := range invalidLevels {
		t.Run("invalid_"+level, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
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

func TestServerConfig_Validate_LogFormat(t *testing.T) {
	t.Parallel()
	validFormats := []string{"json", "text", "pretty"}
	invalidFormats := []string{"JSON", "xml", "", "console"}

	for _, format := range validFormats {
		t.Run("valid_"+format, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.LogFormat = format
			if err := cfg.Validate(); err != nil {
				t.Errorf("%s should be valid: %v", format, err)
			}
		})
	}

	for _, format := range invalidFormats {
		t.Run("invalid_"+format, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.LogFormat = format
			err := cfg.Validate()
			if err == nil {
				t.Errorf("%s should be invalid", format)
			}
		})
	}
}

func TestServerConfig_Validate_Valkey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		addrs       []string
		db          int
		channel     string
		shouldError bool
		errorField  string
	}{
		{"valid", []string{"localhost:26379"}, 0, "ws.broadcast", false, ""},
		{"multiple addrs", []string{"host1:26379", "host2:26379", "host3:26379"}, 0, "ws.broadcast", false, ""},
		{"empty addrs", []string{}, 0, "ws.broadcast", true, "VALKEY_ADDRS"},
		{"negative db", []string{"localhost:26379"}, -1, "ws.broadcast", true, "VALKEY_DB"},
		{"empty channel", []string{"localhost:26379"}, 0, "", true, "VALKEY_CHANNEL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.ValkeyAddrs = tt.addrs
			cfg.ValkeyDB = tt.db
			cfg.ValkeyChannel = tt.channel

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if !strings.Contains(err.Error(), tt.errorField) {
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

func TestServerConfig_Validate_ClientSendBufferSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		size        int
		shouldError bool
	}{
		{"min valid", 64, false},
		{"default", 512, false},
		{"large", 1024, false},
		{"max valid", 4096, false},
		{"too small", 63, true},
		{"too large", 4097, true},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.ClientSendBufferSize = tt.size
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

func TestServerConfig_Validate_CPUPollInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		pollInterval    time.Duration
		metricsInterval time.Duration
		shouldError     bool
		errorField      string
	}{
		{"valid", 1 * time.Second, 15 * time.Second, false, ""},
		{"equal to metrics", 15 * time.Second, 15 * time.Second, false, ""},
		{"minimum 100ms", 100 * time.Millisecond, 15 * time.Second, false, ""},
		{"too fast", 50 * time.Millisecond, 15 * time.Second, true, "CPU_POLL_INTERVAL"},
		{"greater than metrics", 20 * time.Second, 15 * time.Second, true, "CPU_POLL_INTERVAL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.CPUPollInterval = tt.pollInterval
			cfg.MetricsInterval = tt.metricsInterval

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if !strings.Contains(err.Error(), tt.errorField) {
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

func TestServerConfig_Validate_SlowClientMaxAttempts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int
		shouldError bool
	}{
		{"min valid", 1, false},
		{"default", 3, false},
		{"max valid", 10, false},
		{"zero", 0, true},
		{"negative", -1, true},
		{"too large", 11, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.SlowClientMaxAttempts = tt.value
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if !strings.Contains(err.Error(), "WS_SLOW_CLIENT_MAX_ATTEMPTS") {
					t.Errorf("Error should mention WS_SLOW_CLIENT_MAX_ATTEMPTS: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestServerConfig_Validate_HysteresisGap(t *testing.T) {
	t.Parallel()
	// Test that hysteresis gap (upper - lower) is reasonable

	// Valid: 10% gap
	cfg := newValidServerConfig()
	cfg.CPURejectThreshold = 75.0
	cfg.CPURejectThresholdLower = 65.0
	if err := cfg.Validate(); err != nil {
		t.Errorf("10%% gap should be valid: %v", err)
	}

	// Valid: 1% gap (minimal)
	cfg.CPURejectThreshold = 75.0
	cfg.CPURejectThresholdLower = 74.0
	if err := cfg.Validate(); err != nil {
		t.Errorf("1%% gap should be valid: %v", err)
	}

	// Invalid: 0% gap (lower == upper)
	cfg.CPURejectThreshold = 75.0
	cfg.CPURejectThresholdLower = 75.0
	if err := cfg.Validate(); err == nil {
		t.Error("0%% gap (lower == upper) should be invalid")
	}

	// Invalid: negative gap (lower > upper)
	cfg.CPURejectThreshold = 75.0
	cfg.CPURejectThresholdLower = 76.0
	if err := cfg.Validate(); err == nil {
		t.Error("Negative gap (lower > upper) should be invalid")
	}
}

func TestServerConfig_Validate_PingPong(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pongWait    time.Duration
		pingPeriod  time.Duration
		shouldError bool
		errorField  string
	}{
		{
			name:        "valid_60s_45s",
			pongWait:    60 * time.Second,
			pingPeriod:  45 * time.Second,
			shouldError: false,
		},
		{
			name:        "valid_120s_90s",
			pongWait:    120 * time.Second,
			pingPeriod:  90 * time.Second,
			shouldError: false,
		},
		{
			name:        "pongWait_too_small",
			pongWait:    MinPongWait - 1*time.Second, // Below minimum
			pingPeriod:  MinPingPeriod,
			shouldError: true,
			errorField:  "WS_PONG_WAIT",
		},
		{
			name:        "pingPeriod_too_small",
			pongWait:    60 * time.Second,
			pingPeriod:  MinPingPeriod - 1*time.Second, // Below minimum
			shouldError: true,
			errorField:  "WS_PING_PERIOD",
		},
		{
			name:        "pingPeriod_equals_pongWait",
			pongWait:    60 * time.Second,
			pingPeriod:  60 * time.Second,
			shouldError: true,
			errorField:  "WS_PING_PERIOD",
		},
		{
			name:        "pingPeriod_exceeds_pongWait",
			pongWait:    60 * time.Second,
			pingPeriod:  90 * time.Second,
			shouldError: true,
			errorField:  "WS_PING_PERIOD",
		},
		{
			name:        "minimum_valid_values",
			pongWait:    MinPongWait,
			pingPeriod:  MinPingPeriod,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := newValidServerConfig()
			cfg.PongWait = tt.pongWait
			cfg.PingPeriod = tt.pingPeriod

			err := cfg.Validate()

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorField)
				} else if !strings.Contains(err.Error(), tt.errorField) {
					t.Errorf("expected error containing %q, got %q", tt.errorField, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestServerConfig_Validate_WriteWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		writeWait   time.Duration
		shouldError bool
	}{
		{
			name:        "valid_5s",
			writeWait:   5 * time.Second,
			shouldError: false,
		},
		{
			name:        "valid_10s",
			writeWait:   10 * time.Second,
			shouldError: false,
		},
		{
			name:        "valid_minimum_1s",
			writeWait:   1 * time.Second,
			shouldError: false,
		},
		{
			name:        "valid_maximum_30s",
			writeWait:   30 * time.Second,
			shouldError: false,
		},
		{
			name:        "too_small",
			writeWait:   500 * time.Millisecond,
			shouldError: true,
		},
		{
			name:        "too_large",
			writeWait:   31 * time.Second,
			shouldError: true,
		},
		{
			name:        "zero",
			writeWait:   0,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := newValidServerConfig()
			cfg.WriteWait = tt.writeWait

			err := cfg.Validate()

			if tt.shouldError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), "WS_WRITE_WAIT") {
					t.Errorf("expected error containing WS_WRITE_WAIT, got %q", err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}
