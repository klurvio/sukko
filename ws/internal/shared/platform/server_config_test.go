package platform

import (
	"strings"
	"testing"
	"time"
)

// newValidServerConfig returns a server config with all valid defaults for testing.
func newValidServerConfig() *ServerConfig {
	return &ServerConfig{
		BaseConfig: BaseConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			Environment: "local",
		},
		Addr:                        ":3002",
		NumShards:                   1,
		BasePort:                    3002,
		LBAddr:                      ":3005",
		RateLimitBurstMultiplier:    2,
		MessageBackend:              "direct",
		KafkaBrokers:                "localhost:19092",
		MemoryLimit:                 512 * 1024 * 1024, // 512MB
		MaxConnections:              1000,
		MaxKafkaMessagesPerSec:      1000,
		MaxBroadcastsPerSec:         100,
		MaxGoroutines:               1000,
		ConnectionRateLimitEnabled:  true,
		ConnRateLimitIPBurst:        100,
		ConnRateLimitIPRate:         100.0,
		ConnRateLimitGlobalBurst:    300,
		ConnRateLimitGlobalRate:     50.0,
		CPURejectThreshold:          75.0,
		CPURejectThresholdLower:     65.0, // Explicit: non-zero skips auto-compute
		CPUPauseThreshold:           80.0,
		CPUPauseThresholdLower:      70.0, // Explicit: non-zero skips auto-compute
		CPUEWMABeta:                 0.8,
		TCPListenBacklog:            2048,
		HTTPReadTimeout:             15 * time.Second,
		HTTPWriteTimeout:            15 * time.Second,
		HTTPIdleTimeout:             60 * time.Second,
		MetricsInterval:             15 * time.Second,
		CPUPollInterval:             1 * time.Second,
		BroadcastType:               "nats",
		NATSURLs:                    []string{"nats://localhost:4222"},
		NATSSubject:                 "ws.broadcast",
		ClientSendBufferSize:        512,
		SlowClientMaxAttempts:       3,
		MemoryWarningPercent:        80,
		MemoryCriticalPercent:       90,
		BufferHighSaturationPercent: 90,
		BufferPopulationWarnPercent: 25,
		// Topic refresh interval (required for kafka/nats backends)
		TopicRefreshInterval: 60 * time.Second,
		// NATS JetStream defaults (needed when tests switch to nats backend)
		NATSJetStreamReplicas: 1,
		NATSJetStreamMaxAge:   24 * time.Hour,
		// Kafka topic defaults (needed when tests switch to kafka backend)
		KafkaDefaultPartitions:        1,
		KafkaDefaultReplicationFactor: 1,
		// Provisioning gRPC (required for topic discovery)
		ProvisioningGRPCAddr:  "localhost:9090",
		GRPCReconnectDelay:    1 * time.Second,
		GRPCReconnectMaxDelay: 30 * time.Second,
		// WebSocket ping/pong (required)
		PongWait:   60 * time.Second,
		PingPeriod: 45 * time.Second,
		WriteWait:  5 * time.Second,
		// Handler timeouts
		ReplayTimeout:        5 * time.Second,
		PublishTimeout:       5 * time.Second,
		MaxReplayMessages:    100,
		TopicCreationTimeout: 30 * time.Second,
		// Orchestration
		ShardDialTimeout:           10 * time.Second,
		ShardMessageTimeout:        60 * time.Second,
		MetricsAggregationInterval: 5 * time.Second,
		// Shutdown
		ShutdownGracePeriod:   30 * time.Second,
		ShutdownCheckInterval: 1 * time.Second,
		// Internal monitoring
		MetricsCollectInterval: 2 * time.Second,
		MemoryMonitorInterval:  30 * time.Second,
		BufferSampleInterval:   10 * time.Second,
		BufferMaxSamples:       100,
		// Connection rate limiter internals
		ConnRateLimitIPTTL:           5 * time.Minute,
		ConnRateLimitCleanupInterval: 1 * time.Minute,
		// Broadcast bus
		BroadcastBufferSize:      1024,
		BroadcastShutdownTimeout: 5 * time.Second,
		// NATS broadcast tuning
		NATSReconnectBufSize:    5 * 1024 * 1024,
		NATSPingInterval:        10 * time.Second,
		NATSMaxPingsOutstanding: 3,
		NATSReconnectWait:       2 * time.Second,
		NATSMaxReconnects:       -1,
		NATSHealthCheckInterval: 10 * time.Second,
		NATSFlushTimeout:        5 * time.Second,
		// Valkey broadcast tuning
		ValkeyPoolSize:                50,
		ValkeyMinIdleConns:            10,
		ValkeyDialTimeout:             5 * time.Second,
		ValkeyReadTimeout:             3 * time.Second,
		ValkeyWriteTimeout:            3 * time.Second,
		ValkeyMaxRetries:              3,
		ValkeyMinRetryBackoff:         100 * time.Millisecond,
		ValkeyMaxRetryBackoff:         1 * time.Second,
		ValkeyPublishTimeout:          100 * time.Millisecond,
		ValkeyStartupPingTimeout:      5 * time.Second,
		ValkeyReconnectInitialBackoff: 100 * time.Millisecond,
		ValkeyReconnectMaxBackoff:     30 * time.Second,
		ValkeyReconnectMaxAttempts:    10,
		ValkeyHealthCheckInterval:     10 * time.Second,
		ValkeyHealthCheckTimeout:      5 * time.Second,
		// Kafka consumer tuning
		KafkaBatchSize:                 50,
		KafkaBatchTimeout:              10 * time.Millisecond,
		KafkaFetchMaxWait:              500 * time.Millisecond,
		KafkaFetchMinBytes:             1,
		KafkaFetchMaxBytes:             10485760,
		KafkaSessionTimeout:            30 * time.Second,
		KafkaRebalanceTimeout:          60 * time.Second,
		KafkaReplayFetchMaxBytes:       5242880,
		KafkaBackpressureCheckInterval: 100 * time.Millisecond,
		// Kafka producer tuning
		KafkaProducerBatchMaxBytes:      1048576,
		KafkaProducerMaxBufferedRecords: 10000,
		KafkaProducerRecordRetries:      8,
		KafkaProducerCBTimeout:          30 * time.Second,
		KafkaProducerCBMaxFailures:      5,
		KafkaProducerCBHalfOpenReqs:     1,
		KafkaProducerTopicCacheTTL:      30 * time.Second,
		// JetStream backend tuning
		JetStreamReconnectWait:   2 * time.Second,
		JetStreamMaxDeliver:      3,
		JetStreamAckWait:         30 * time.Second,
		JetStreamRefreshTimeout:  30 * time.Second,
		JetStreamRefreshInterval: 60 * time.Second,
		JetStreamReplayFetchWait: 2 * time.Second,
		JetStreamMaxAge:          24 * time.Hour,
		// Valkey config
		ValkeyMasterName: "mymaster",
		ValkeyChannel:    "ws.broadcast",
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

func TestServerConfig_Validate_BasePortPlusShards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		basePort  int
		numShards int
		wantErr   bool
	}{
		{"valid_3002_3shards", 3002, 3, false},
		{"valid_65533_3shards", 65533, 3, false},
		{"overflow_65534_3shards", 65534, 3, true},
		{"overflow_65535_2shards", 65535, 2, true},
		{"edge_65535_1shard", 65535, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.BasePort = tt.basePort
			cfg.NumShards = tt.numShards

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("Expected error for port overflow")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "exceeds max port") {
				t.Errorf("Error should mention port overflow: %v", err)
			}
		})
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

func TestServerConfig_Validate_CPUEWMABeta(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       float64
		shouldError bool
	}{
		{"valid default", 0.8, false},
		{"valid low", 0.1, false},
		{"valid high", 0.99, false},
		{"zero", 0.0, true},
		{"one", 1.0, true},
		{"negative", -0.5, true},
		{"greater than one", 1.5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.CPUEWMABeta = tt.value
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if tt.shouldError && err != nil && !strings.Contains(err.Error(), "WS_CPU_EWMA_BETA") {
				t.Errorf("Error should mention WS_CPU_EWMA_BETA: %v", err)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
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
	validFormats := []string{"json", "pretty"}
	invalidFormats := []string{"JSON", "xml", "text", "", "console"}

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
			cfg.BroadcastType = "valkey"
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

func TestServerConfig_Validate_BroadcastType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		bType       string
		shouldError bool
	}{
		{"nats", "nats", false},
		{"valkey", "valkey", false},
		{"redis", "redis", false},
		{"empty", "", true},
		{"invalid", "rabbitmq", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.BroadcastType = tt.bType
			if tt.bType == "valkey" || tt.bType == "redis" {
				cfg.ValkeyAddrs = []string{"localhost:26379"}
				cfg.ValkeyMasterName = "mymaster"
				cfg.ValkeyChannel = "ws.broadcast"
			}
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

func TestServerConfig_Normalize_HysteresisAutoCompute(t *testing.T) {
	t.Parallel()

	// When lower = 0 (sentinel), Normalize auto-computes as upper - 10
	cfg := newValidServerConfig()
	cfg.CPURejectThreshold = 60.0
	cfg.CPURejectThresholdLower = 0 // sentinel
	cfg.CPUPauseThreshold = 70.0
	cfg.CPUPauseThresholdLower = 0 // sentinel

	cfg.Normalize()

	if err := cfg.Validate(); err != nil {
		t.Errorf("Auto-compute should produce valid config: %v", err)
	}
	if cfg.CPURejectThresholdLower != 50.0 {
		t.Errorf("CPURejectThresholdLower should be 50.0 (auto: 60-10), got %.1f", cfg.CPURejectThresholdLower)
	}
	if cfg.CPUPauseThresholdLower != 60.0 {
		t.Errorf("CPUPauseThresholdLower should be 60.0 (auto: 70-10), got %.1f", cfg.CPUPauseThresholdLower)
	}

	// When lower is explicitly set (non-zero), Normalize does NOT override
	cfg2 := newValidServerConfig()
	cfg2.CPURejectThreshold = 75.0
	cfg2.CPURejectThresholdLower = 60.0 // explicit
	cfg2.CPUPauseThreshold = 80.0
	cfg2.CPUPauseThresholdLower = 65.0 // explicit

	cfg2.Normalize()

	if err := cfg2.Validate(); err != nil {
		t.Errorf("Explicit lower thresholds should be valid: %v", err)
	}
	if cfg2.CPURejectThresholdLower != 60.0 {
		t.Errorf("Explicit CPURejectThresholdLower should remain 60.0, got %.1f", cfg2.CPURejectThresholdLower)
	}
	if cfg2.CPUPauseThresholdLower != 65.0 {
		t.Errorf("Explicit CPUPauseThresholdLower should remain 65.0, got %.1f", cfg2.CPUPauseThresholdLower)
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

func TestServerConfig_Validate_MessageBackend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		backend     string
		jsURLs      string
		shouldError bool
		errorField  string
	}{
		{"direct", "direct", "", false, ""},
		{"kafka", "kafka", "", false, ""},
		{"nats with urls", "nats", "nats://localhost:4222", false, ""},
		{"nats without urls", "nats", "", true, "NATS_JETSTREAM_URLS"},
		{"invalid backend", "redis", "", true, "MESSAGE_BACKEND"},
		{"empty backend", "", "", true, "MESSAGE_BACKEND"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MessageBackend = tt.backend
			cfg.NATSJetStreamURLs = tt.jsURLs

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

func TestServerConfig_Validate_NATSJetStreamBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		replicas   int
		maxAge     time.Duration
		wantErr    bool
		errContain string
	}{
		{"valid defaults", 1, 24 * time.Hour, false, ""},
		{"valid 3 replicas", 3, 1 * time.Hour, false, ""},
		{"zero replicas", 0, 24 * time.Hour, true, "NATS_JETSTREAM_REPLICAS"},
		{"negative replicas", -1, 24 * time.Hour, true, "NATS_JETSTREAM_REPLICAS"},
		{"zero max age", 1, 0, true, "NATS_JETSTREAM_MAX_AGE"},
		{"too small max age", 1, 30 * time.Second, true, "NATS_JETSTREAM_MAX_AGE"},
		{"min valid max age", 1, 1 * time.Minute, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MessageBackend = "nats"
			cfg.NATSJetStreamURLs = "nats://localhost:4222"
			cfg.NATSJetStreamReplicas = tt.replicas
			cfg.NATSJetStreamMaxAge = tt.maxAge

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				} else if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error should contain %q, got: %v", tt.errContain, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_TopicRefreshInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backend  string
		interval time.Duration
		wantErr  bool
	}{
		{"kafka valid", "kafka", 60 * time.Second, false},
		{"nats valid", "nats", 30 * time.Second, false},
		{"direct skips validation", "direct", 0, false},
		{"kafka zero", "kafka", 0, true},
		{"kafka too small", "kafka", 500 * time.Millisecond, true},
		{"kafka min valid", "kafka", 1 * time.Second, false},
		{"nats zero", "nats", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MessageBackend = tt.backend
			cfg.TopicRefreshInterval = tt.interval
			if tt.backend == "nats" {
				cfg.NATSJetStreamURLs = "nats://localhost:4222"
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error")
			} else if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_KafkaTopicDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		partitions        int
		replicationFactor int
		shouldError       bool
		errorField        string
	}{
		{"valid defaults", 1, 1, false, ""},
		{"valid higher values", 6, 3, false, ""},
		{"zero partitions", 0, 1, true, "KAFKA_DEFAULT_PARTITIONS"},
		{"negative partitions", -1, 1, true, "KAFKA_DEFAULT_PARTITIONS"},
		{"zero replication", 1, 0, true, "KAFKA_DEFAULT_REPLICATION_FACTOR"},
		{"negative replication", 1, -1, true, "KAFKA_DEFAULT_REPLICATION_FACTOR"},
		{"both zero", 0, 0, true, "KAFKA_DEFAULT_PARTITIONS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MessageBackend = "kafka"
			cfg.KafkaDefaultPartitions = tt.partitions
			cfg.KafkaDefaultReplicationFactor = tt.replicationFactor

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

func TestServerConfig_Validate_KafkaTopicDefaults_SkippedForDirect(t *testing.T) {
	t.Parallel()
	// Zero partitions with direct backend should NOT error (validation is skipped)
	cfg := newValidServerConfig()
	cfg.MessageBackend = "direct"
	cfg.KafkaDefaultPartitions = 0
	cfg.KafkaDefaultReplicationFactor = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("Kafka topic defaults with direct backend should not error: %v", err)
	}
}

func TestServerConfig_Validate_KafkaSASL_OnlyWithKafkaBackend(t *testing.T) {
	t.Parallel()

	// SASL enabled with direct backend should NOT error (SASL validation is skipped)
	cfg := newValidServerConfig()
	cfg.MessageBackend = "direct"
	cfg.KafkaSASLEnabled = true
	// Deliberately leave SASL fields empty
	if err := cfg.Validate(); err != nil {
		t.Errorf("SASL with direct backend should not error: %v", err)
	}

	// SASL enabled with kafka backend SHOULD error (missing credentials)
	cfg2 := newValidServerConfig()
	cfg2.MessageBackend = "kafka"
	cfg2.KafkaSASLEnabled = true
	cfg2.KafkaSASLMechanism = "scram-sha-256"
	// Missing username/password
	if err := cfg2.Validate(); err == nil {
		t.Error("SASL with kafka backend and missing credentials should error")
	}
}

func TestServerConfig_Validate_MemoryLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int64
		shouldError bool
	}{
		{"valid 512MB", 512 * 1024 * 1024, false},
		{"valid 64MB min", 64 * 1024 * 1024, false},
		{"too small", 32 * 1024 * 1024, true},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MemoryLimit = tt.value
			err := cfg.Validate()
			if tt.shouldError && err == nil {
				t.Error("Should error")
			}
			if tt.shouldError && err != nil && !strings.Contains(err.Error(), "WS_MEMORY_LIMIT") {
				t.Errorf("Error should mention WS_MEMORY_LIMIT: %v", err)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_RateLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		maxKafka      int
		maxBroadcast  int
		maxGoroutines int
		shouldError   bool
		errorContains string
	}{
		{"valid defaults", 1000, 100, 1000, false, ""},
		{"zero kafka rate", 0, 100, 1000, true, "WS_MAX_KAFKA_RATE"},
		{"negative kafka rate", -1, 100, 1000, true, "WS_MAX_KAFKA_RATE"},
		{"zero broadcast rate", 1000, 0, 1000, true, "WS_MAX_BROADCAST_RATE"},
		{"goroutines too low", 1000, 100, 50, true, "WS_MAX_GOROUTINES"},
		{"goroutines min valid", 1000, 100, 100, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MaxKafkaMessagesPerSec = tt.maxKafka
			cfg.MaxBroadcastsPerSec = tt.maxBroadcast
			cfg.MaxGoroutines = tt.maxGoroutines

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should mention %s: %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_ConnRateLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		enabled       bool
		ipBurst       int
		ipRate        float64
		globalBurst   int
		globalRate    float64
		shouldError   bool
		errorContains string
	}{
		{"valid enabled", true, 100, 100.0, 300, 50.0, false, ""},
		{"disabled skips checks", false, 0, 0, 0, 0, false, ""},
		{"zero ip burst", true, 0, 100.0, 300, 50.0, true, "CONN_RATE_LIMIT_IP_BURST"},
		{"zero ip rate", true, 100, 0, 300, 50.0, true, "CONN_RATE_LIMIT_IP_RATE"},
		{"zero global burst", true, 100, 100.0, 0, 50.0, true, "CONN_RATE_LIMIT_GLOBAL_BURST"},
		{"zero global rate", true, 100, 100.0, 300, 0, true, "CONN_RATE_LIMIT_GLOBAL_RATE"},
		{"negative ip rate", true, 100, -1.0, 300, 50.0, true, "CONN_RATE_LIMIT_IP_RATE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.ConnectionRateLimitEnabled = tt.enabled
			cfg.ConnRateLimitIPBurst = tt.ipBurst
			cfg.ConnRateLimitIPRate = tt.ipRate
			cfg.ConnRateLimitGlobalBurst = tt.globalBurst
			cfg.ConnRateLimitGlobalRate = tt.globalRate

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error should mention %s: %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_TCPListenBacklog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int
		shouldError bool
	}{
		{"valid default", 2048, false},
		{"zero disables", 0, false},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.TCPListenBacklog = tt.value
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

func TestServerConfig_Validate_HTTPTimeouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		readTimeout  time.Duration
		writeTimeout time.Duration
		idleTimeout  time.Duration
		shouldError  bool
		errorField   string
	}{
		{"valid defaults", 15 * time.Second, 15 * time.Second, 60 * time.Second, false, ""},
		{"read too small", 500 * time.Millisecond, 15 * time.Second, 60 * time.Second, true, "HTTP_READ_TIMEOUT"},
		{"read too large", 121 * time.Second, 15 * time.Second, 60 * time.Second, true, "HTTP_READ_TIMEOUT"},
		{"write too small", 15 * time.Second, 0, 60 * time.Second, true, "HTTP_WRITE_TIMEOUT"},
		{"write too large", 15 * time.Second, 121 * time.Second, 60 * time.Second, true, "HTTP_WRITE_TIMEOUT"},
		{"idle too small", 15 * time.Second, 15 * time.Second, 0, true, "HTTP_IDLE_TIMEOUT"},
		{"idle too large", 15 * time.Second, 15 * time.Second, 301 * time.Second, true, "HTTP_IDLE_TIMEOUT"},
		{"min read", 1 * time.Second, 15 * time.Second, 60 * time.Second, false, ""},
		{"max read", 120 * time.Second, 15 * time.Second, 60 * time.Second, false, ""},
		{"max idle", 15 * time.Second, 15 * time.Second, 300 * time.Second, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.HTTPReadTimeout = tt.readTimeout
			cfg.HTTPWriteTimeout = tt.writeTimeout
			cfg.HTTPIdleTimeout = tt.idleTimeout

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if tt.errorField != "" && !strings.Contains(err.Error(), tt.errorField) {
					t.Errorf("Error should mention %s: %v", tt.errorField, err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_MetricsInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		interval    time.Duration
		shouldError bool
	}{
		{"valid default", 15 * time.Second, false},
		{"valid 1s", 1 * time.Second, false},
		{"too small", 500 * time.Millisecond, true},
		{"zero", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.MetricsInterval = tt.interval
			// CPUPollInterval must be <= MetricsInterval
			if tt.interval > 0 {
				cfg.CPUPollInterval = tt.interval
			}

			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error")
				} else if !strings.Contains(err.Error(), "METRICS_INTERVAL") {
					t.Errorf("Error should mention METRICS_INTERVAL: %v", err)
				}
			} else if err != nil {
				t.Errorf("Should not error: %v", err)
			}
		})
	}
}

func TestServerConfig_Validate_ProdOverrideBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		environment string
		override    string
		shouldError bool
	}{
		{"prod_with_override", "prod", "dev", true},
		{"prod_no_override", "prod", "", false},
		{"dev_with_override", "dev", "prod", false},
		{"dev_no_override", "dev", "", false},
		{"PROD_uppercase", "PROD", "dev", true},
		{"prod_whitespace", " prod ", "dev", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.Environment = tt.environment
			cfg.KafkaTopicNamespaceOverride = tt.override
			err := cfg.Validate()
			if tt.shouldError {
				if err == nil {
					t.Error("Should error on prod override")
				} else if !strings.Contains(err.Error(), "KAFKA_TOPIC_NAMESPACE_OVERRIDE") {
					t.Errorf("Error should mention KAFKA_TOPIC_NAMESPACE_OVERRIDE: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Should not error: %v", err)
				}
			}
		})
	}
}

func TestServerConfig_Validate_NewDurationFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setField      func(*ServerConfig)
		errorContains string
	}{
		{"ReplayTimeout", func(c *ServerConfig) { c.ReplayTimeout = 0 }, "WS_REPLAY_TIMEOUT"},
		{"PublishTimeout", func(c *ServerConfig) { c.PublishTimeout = 0 }, "WS_PUBLISH_TIMEOUT"},
		{"TopicCreationTimeout", func(c *ServerConfig) { c.TopicCreationTimeout = 0 }, "WS_TOPIC_CREATION_TIMEOUT"},
		{"ShardDialTimeout", func(c *ServerConfig) { c.ShardDialTimeout = 0 }, "WS_SHARD_DIAL_TIMEOUT"},
		{"ShardMessageTimeout", func(c *ServerConfig) { c.ShardMessageTimeout = 0 }, "WS_SHARD_MESSAGE_TIMEOUT"},
		{"MetricsAggregationInterval", func(c *ServerConfig) { c.MetricsAggregationInterval = 0 }, "WS_METRICS_AGGREGATION_INTERVAL"},
		{"ShutdownGracePeriod", func(c *ServerConfig) { c.ShutdownGracePeriod = 0 }, "WS_SHUTDOWN_GRACE_PERIOD"},
		{"ShutdownCheckInterval", func(c *ServerConfig) { c.ShutdownCheckInterval = 0 }, "WS_SHUTDOWN_CHECK_INTERVAL"},
		{"MetricsCollectInterval", func(c *ServerConfig) { c.MetricsCollectInterval = 0 }, "WS_METRICS_COLLECT_INTERVAL"},
		{"MemoryMonitorInterval", func(c *ServerConfig) { c.MemoryMonitorInterval = 0 }, "WS_MEMORY_MONITOR_INTERVAL"},
		{"BufferSampleInterval", func(c *ServerConfig) { c.BufferSampleInterval = 0 }, "WS_BUFFER_SAMPLE_INTERVAL"},
		{"ConnRateLimitIPTTL", func(c *ServerConfig) { c.ConnRateLimitIPTTL = 0 }, "CONN_RATE_LIMIT_IP_TTL"},
		{"ConnRateLimitCleanupInterval", func(c *ServerConfig) { c.ConnRateLimitCleanupInterval = 0 }, "CONN_RATE_LIMIT_CLEANUP_INTERVAL"},
		{"BroadcastShutdownTimeout", func(c *ServerConfig) { c.BroadcastShutdownTimeout = 0 }, "BROADCAST_SHUTDOWN_TIMEOUT"},
		{"NATSPingInterval", func(c *ServerConfig) { c.NATSPingInterval = 0 }, "NATS_PING_INTERVAL"},
		{"NATSReconnectWait", func(c *ServerConfig) { c.NATSReconnectWait = 0 }, "NATS_RECONNECT_WAIT"},
		{"NATSHealthCheckInterval", func(c *ServerConfig) { c.NATSHealthCheckInterval = 0 }, "NATS_HEALTH_CHECK_INTERVAL"},
		{"NATSFlushTimeout", func(c *ServerConfig) { c.NATSFlushTimeout = 0 }, "NATS_FLUSH_TIMEOUT"},
		{"KafkaBatchTimeout", func(c *ServerConfig) { c.KafkaBatchTimeout = 0 }, "KAFKA_BATCH_TIMEOUT"},
		{"KafkaFetchMaxWait", func(c *ServerConfig) { c.KafkaFetchMaxWait = 0 }, "KAFKA_FETCH_MAX_WAIT"},
		{"KafkaSessionTimeout", func(c *ServerConfig) { c.KafkaSessionTimeout = 0 }, "KAFKA_SESSION_TIMEOUT"},
		{"KafkaRebalanceTimeout", func(c *ServerConfig) { c.KafkaRebalanceTimeout = 0 }, "KAFKA_REBALANCE_TIMEOUT"},
		{"KafkaBackpressureCheckInterval", func(c *ServerConfig) { c.KafkaBackpressureCheckInterval = 0 }, "KAFKA_BACKPRESSURE_CHECK_INTERVAL"},
		{"KafkaProducerCBTimeout", func(c *ServerConfig) { c.KafkaProducerCBTimeout = 0 }, "KAFKA_PRODUCER_CB_TIMEOUT"},
		{"KafkaProducerTopicCacheTTL", func(c *ServerConfig) { c.KafkaProducerTopicCacheTTL = 0 }, "KAFKA_PRODUCER_TOPIC_CACHE_TTL"},
		{"JetStreamReconnectWait", func(c *ServerConfig) { c.JetStreamReconnectWait = 0 }, "JETSTREAM_RECONNECT_WAIT"},
		{"JetStreamAckWait", func(c *ServerConfig) { c.JetStreamAckWait = 0 }, "JETSTREAM_ACK_WAIT"},
		{"JetStreamRefreshTimeout", func(c *ServerConfig) { c.JetStreamRefreshTimeout = 0 }, "JETSTREAM_REFRESH_TIMEOUT"},
		{"JetStreamRefreshInterval", func(c *ServerConfig) { c.JetStreamRefreshInterval = 0 }, "JETSTREAM_REFRESH_INTERVAL"},
		{"JetStreamReplayFetchWait", func(c *ServerConfig) { c.JetStreamReplayFetchWait = 0 }, "JETSTREAM_REPLAY_FETCH_WAIT"},
		{"JetStreamMaxAge", func(c *ServerConfig) { c.JetStreamMaxAge = 0 }, "JETSTREAM_MAX_AGE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			tt.setField(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected error for zero %s", tt.name)
			} else if !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("Error should contain %q: %v", tt.errorContains, err)
			}
		})
	}
}

func TestServerConfig_Validate_NewIntFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setField      func(*ServerConfig)
		errorContains string
	}{
		{"MaxReplayMessages", func(c *ServerConfig) { c.MaxReplayMessages = 0 }, "WS_MAX_REPLAY_MESSAGES"},
		{"BufferMaxSamples", func(c *ServerConfig) { c.BufferMaxSamples = 0 }, "WS_BUFFER_MAX_SAMPLES"},
		{"BroadcastBufferSize", func(c *ServerConfig) { c.BroadcastBufferSize = 0 }, "BROADCAST_BUFFER_SIZE"},
		{"NATSReconnectBufSize", func(c *ServerConfig) { c.NATSReconnectBufSize = 0 }, "NATS_RECONNECT_BUF_SIZE"},
		{"NATSMaxPingsOutstanding", func(c *ServerConfig) { c.NATSMaxPingsOutstanding = 0 }, "NATS_MAX_PINGS_OUTSTANDING"},
		{"KafkaBatchSize", func(c *ServerConfig) { c.KafkaBatchSize = 0 }, "KAFKA_BATCH_SIZE"},
		{"KafkaFetchMinBytes", func(c *ServerConfig) { c.KafkaFetchMinBytes = 0 }, "KAFKA_FETCH_MIN_BYTES"},
		{"KafkaFetchMaxBytes", func(c *ServerConfig) { c.KafkaFetchMaxBytes = 0 }, "KAFKA_FETCH_MAX_BYTES"},
		{"KafkaReplayFetchMaxBytes", func(c *ServerConfig) { c.KafkaReplayFetchMaxBytes = 0 }, "KAFKA_REPLAY_FETCH_MAX_BYTES"},
		{"KafkaProducerBatchMaxBytes", func(c *ServerConfig) { c.KafkaProducerBatchMaxBytes = 0 }, "KAFKA_PRODUCER_BATCH_MAX_BYTES"},
		{"KafkaProducerMaxBufferedRecords", func(c *ServerConfig) { c.KafkaProducerMaxBufferedRecords = 0 }, "KAFKA_PRODUCER_MAX_BUFFERED_RECORDS"},
		{"KafkaProducerRecordRetries", func(c *ServerConfig) { c.KafkaProducerRecordRetries = 0 }, "KAFKA_PRODUCER_RECORD_RETRIES"},
		{"KafkaProducerCBMaxFailures", func(c *ServerConfig) { c.KafkaProducerCBMaxFailures = 0 }, "KAFKA_PRODUCER_CB_MAX_FAILURES"},
		{"KafkaProducerCBHalfOpenReqs", func(c *ServerConfig) { c.KafkaProducerCBHalfOpenReqs = 0 }, "KAFKA_PRODUCER_CB_HALF_OPEN_REQS"},
		{"JetStreamMaxDeliver", func(c *ServerConfig) { c.JetStreamMaxDeliver = 0 }, "JETSTREAM_MAX_DELIVER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			tt.setField(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected error for zero %s", tt.name)
			} else if !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("Error should contain %q: %v", tt.errorContains, err)
			}
		})
	}
}

func TestServerConfig_Validate_PercentageFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setField      func(*ServerConfig)
		errorContains string
	}{
		{"MemoryWarningPercent zero", func(c *ServerConfig) { c.MemoryWarningPercent = 0 }, "WS_MEMORY_WARNING_PERCENT"},
		{"MemoryWarningPercent 101", func(c *ServerConfig) { c.MemoryWarningPercent = 101 }, "WS_MEMORY_WARNING_PERCENT"},
		{"MemoryCriticalPercent zero", func(c *ServerConfig) { c.MemoryCriticalPercent = 0 }, "WS_MEMORY_CRITICAL_PERCENT"},
		{"MemoryCriticalPercent 101", func(c *ServerConfig) { c.MemoryCriticalPercent = 101 }, "WS_MEMORY_CRITICAL_PERCENT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			tt.setField(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected error for %s", tt.name)
			} else if !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("Error should contain %q: %v", tt.errorContains, err)
			}
		})
	}
}

func TestServerConfig_Validate_NATSMaxReconnects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		value       int
		shouldError bool
	}{
		{"unlimited (-1)", -1, false},
		{"zero", 0, false},
		{"positive", 10, false},
		{"invalid (-2)", -2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newValidServerConfig()
			cfg.NATSMaxReconnects = tt.value
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
