package platform

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// Config holds all server configuration
// Tags:
//
//	env: Environment variable name
//	envDefault: Default value if not set
//	required: Must be provided (no default)
type Config struct {
	// Server basics
	Addr          string `env:"WS_ADDR" envDefault:":3002"`
	KafkaBrokers  string `env:"KAFKA_BROKERS" envDefault:"localhost:19092"`
	ConsumerGroup string `env:"KAFKA_CONSUMER_GROUP" envDefault:"ws-server-group"`

	// Resource limits (from container)
	CPULimit    float64 `env:"WS_CPU_LIMIT" envDefault:"1.0"`
	MemoryLimit int64   `env:"WS_MEMORY_LIMIT" envDefault:"536870912"` // 512MB

	// Capacity
	MaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"500"`

	// Rate limiting
	MaxKafkaRate     int `env:"WS_MAX_KAFKA_RATE" envDefault:"1000"` // Kafka message consumption rate
	MaxBroadcastRate int `env:"WS_MAX_BROADCAST_RATE" envDefault:"20"`
	MaxGoroutines    int `env:"WS_MAX_GOROUTINES" envDefault:"1000"`

	// Connection rate limiting (DoS protection)
	ConnectionRateLimitEnabled bool    `env:"CONN_RATE_LIMIT_ENABLED" envDefault:"true"`
	ConnRateLimitIPBurst       int     `env:"CONN_RATE_LIMIT_IP_BURST" envDefault:"10"`
	ConnRateLimitIPRate        float64 `env:"CONN_RATE_LIMIT_IP_RATE" envDefault:"1.0"`
	ConnRateLimitGlobalBurst   int     `env:"CONN_RATE_LIMIT_GLOBAL_BURST" envDefault:"300"`
	ConnRateLimitGlobalRate    float64 `env:"CONN_RATE_LIMIT_GLOBAL_RATE" envDefault:"50.0"`

	// CPU Safety Thresholds (Container-Aware) with Hysteresis
	//
	// These thresholds are relative to CONTAINER CPU ALLOCATION, not host CPU.
	// The system uses container-aware cgroup measurement when running in Docker/K8s.
	//
	// Example with 1.0 CPU allocation (docker: cpus: "1.0"):
	//   - 75% = reject when using 0.75 of allocated 1.0 CPU
	//   - Container can use up to 100% before being throttled by cgroup
	//
	// Example with 4.0 CPU allocation (docker: cpus: "4.0"):
	//   - 75% = reject when using 3.0 of allocated 4.0 CPUs
	//   - Container can use up to 400% (4.0 cores) before throttling
	//
	// In non-containerized environments, falls back to host CPU percentage.
	//
	// Hysteresis prevents rapid oscillation when CPU hovers near thresholds.
	// Uses two thresholds: upper (to enter state) and lower (to exit state).
	//
	// Example with default values (reject: 75% upper, 65% lower):
	//   - CPU rises to 76% → start rejecting connections
	//   - CPU drops to 70% → still rejecting (in deadband)
	//   - CPU drops to 64% → stop rejecting, accept connections
	//   - CPU rises to 70% → still accepting (in deadband)
	//
	// The 10% gap is the "hysteresis band" that provides stability.
	//
	CPURejectThreshold      float64 `env:"WS_CPU_REJECT_THRESHOLD" envDefault:"75.0"`       // Upper: start rejecting above this %
	CPURejectThresholdLower float64 `env:"WS_CPU_REJECT_THRESHOLD_LOWER" envDefault:"65.0"` // Lower: stop rejecting below this %
	CPUPauseThreshold       float64 `env:"WS_CPU_PAUSE_THRESHOLD" envDefault:"80.0"`        // Upper: pause Kafka above this %
	CPUPauseThresholdLower  float64 `env:"WS_CPU_PAUSE_THRESHOLD_LOWER" envDefault:"70.0"`  // Lower: resume Kafka below this %

	// TCP/Network Tuning (Burst Tolerance)
	//
	// These settings improve tolerance to connection bursts by increasing buffers and timeouts.
	// Defaults are conservative - increase for high-burst workloads.
	//
	// REVERT: Set TCP_LISTEN_BACKLOG=0 to disable custom backlog (use Go defaults)
	//         Set HTTP_*_TIMEOUT to lower values if needed
	//
	TCPListenBacklog  int           `env:"TCP_LISTEN_BACKLOG" envDefault:"2048"` // TCP accept queue size (0 = Go default ~128)
	HTTPReadTimeout   time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`   // HTTP server read timeout
	HTTPWriteTimeout  time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`  // HTTP server write timeout
	HTTPIdleTimeout   time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`   // HTTP server idle timeout

	// Monitoring
	MetricsInterval time.Duration `env:"METRICS_INTERVAL" envDefault:"15s"`

	// CPU Polling Interval (for protection decisions)
	// Separate from MetricsInterval to allow faster CPU spike detection
	// while keeping full metrics reporting (memory, goroutines) at a reasonable interval.
	//
	// CPU polling is used by:
	// - ShouldPauseKafka() - backpressure when CPU > 80%
	// - ShouldAcceptConnection() - reject connections when CPU > 75%
	//
	// Trade-off: 1s polling = 0.1% CPU overhead, but 15x faster spike detection
	CPUPollInterval time.Duration `env:"CPU_POLL_INTERVAL" envDefault:"1s"`

	// Logging
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// Environment
	Environment string `env:"ENVIRONMENT" envDefault:"development"`

	// Redis Sentinel Configuration (for BroadcastBus)
	// Supports both self-hosted Sentinel (3 addresses) and GCP Memorystore (1 address)
	RedisSentinelAddrs []string `env:"REDIS_SENTINEL_ADDRS" envSeparator:"," required:"true"`
	RedisMasterName    string   `env:"REDIS_MASTER_NAME" envDefault:"mymaster"`
	RedisPassword      string   `env:"REDIS_PASSWORD"`
	RedisDB            int      `env:"REDIS_DB" envDefault:"0"`
	RedisChannel       string   `env:"REDIS_CHANNEL" envDefault:"ws.broadcast"`

	// Client Send Buffer Size
	// Controls the per-client send channel buffer (memory vs slow-client tolerance trade-off)
	//
	// Buffer sizing at 125 msg/sec broadcast rate (25 msg/sec × 5 channel subscriptions):
	// - 512 slots: ~256KB/client, 4.1s buffer, ~3.5GB heap at 13.5K clients
	// - 1024 slots: ~512KB/client, 8.2s buffer, ~6.9GB heap at 13.5K clients
	//
	// Trade-off:
	// - Smaller buffer = less memory = less GC pressure = lower CPU spikes
	// - Larger buffer = more tolerance for slow clients and network hiccups
	//
	// Default: 512 (reduced from 1024 to cut heap size by 50%, reduce GC pressure)
	// Production guidance: Start with 512, increase to 768 or 1024 if cascade disconnects occur
	ClientSendBufferSize int `env:"WS_CLIENT_SEND_BUFFER_SIZE" envDefault:"512"`
}

// LoadConfig reads configuration from .env file and environment variables
// Priority: ENV vars > .env file > defaults
//
// Optional logger parameter for structured logging. If nil, logs to stdout.
func LoadConfig(logger *zerolog.Logger) (*Config, error) {
	// Load .env file (optional - OK if it doesn't exist)
	// In production (Docker), we use environment variables directly
	// In development, .env file provides convenience
	if err := godotenv.Load(); err != nil {
		// Only log, don't fail - we can run without .env file
		if logger != nil {
			logger.Info().Msg("No .env file found (using environment variables only)")
		} else {
			fmt.Println("Info: No .env file found (using environment variables only)")
		}
	} else {
		if logger != nil {
			logger.Info().Msg("Loaded configuration from .env file")
		}
	}

	cfg := &Config{}

	// Parse environment variables into struct
	// This validates types and applies defaults
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validation
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	if logger != nil {
		logger.Info().Msg("Configuration loaded and validated successfully")
	}

	return cfg, nil
}

// Validate checks configuration for errors
func (c *Config) Validate() error {
	// Required fields (no sensible defaults)
	if c.Addr == "" {
		return fmt.Errorf("WS_ADDR is required")
	}

	// Range checks
	if c.MaxConnections < 1 {
		return fmt.Errorf("WS_MAX_CONNECTIONS must be > 0, got %d", c.MaxConnections)
	}
	if c.CPURejectThreshold < 0 || c.CPURejectThreshold > 100 {
		return fmt.Errorf("WS_CPU_REJECT_THRESHOLD must be 0-100, got %.1f", c.CPURejectThreshold)
	}
	if c.CPURejectThresholdLower < 0 || c.CPURejectThresholdLower > 100 {
		return fmt.Errorf("WS_CPU_REJECT_THRESHOLD_LOWER must be 0-100, got %.1f", c.CPURejectThresholdLower)
	}
	if c.CPUPauseThreshold < 0 || c.CPUPauseThreshold > 100 {
		return fmt.Errorf("WS_CPU_PAUSE_THRESHOLD must be 0-100, got %.1f", c.CPUPauseThreshold)
	}
	if c.CPUPauseThresholdLower < 0 || c.CPUPauseThresholdLower > 100 {
		return fmt.Errorf("WS_CPU_PAUSE_THRESHOLD_LOWER must be 0-100, got %.1f", c.CPUPauseThresholdLower)
	}

	// Logical checks
	if c.CPUPauseThreshold < c.CPURejectThreshold {
		return fmt.Errorf("WS_CPU_PAUSE_THRESHOLD (%.1f) must be >= WS_CPU_REJECT_THRESHOLD (%.1f)",
			c.CPUPauseThreshold, c.CPURejectThreshold)
	}

	// Hysteresis validation: lower must be < upper
	if c.CPURejectThresholdLower >= c.CPURejectThreshold {
		return fmt.Errorf("WS_CPU_REJECT_THRESHOLD_LOWER (%.1f) must be < WS_CPU_REJECT_THRESHOLD (%.1f)",
			c.CPURejectThresholdLower, c.CPURejectThreshold)
	}
	if c.CPUPauseThresholdLower >= c.CPUPauseThreshold {
		return fmt.Errorf("WS_CPU_PAUSE_THRESHOLD_LOWER (%.1f) must be < WS_CPU_PAUSE_THRESHOLD (%.1f)",
			c.CPUPauseThresholdLower, c.CPUPauseThreshold)
	}

	// Enum checks
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error (got: %s)", c.LogLevel)
	}

	validLogFormats := map[string]bool{"json": true, "text": true, "pretty": true}
	if !validLogFormats[c.LogFormat] {
		return fmt.Errorf("LOG_FORMAT must be one of: json, text, pretty (got: %s)", c.LogFormat)
	}

	// Redis configuration validation
	if len(c.RedisSentinelAddrs) == 0 {
		return fmt.Errorf("REDIS_SENTINEL_ADDRS is required (at least one address)")
	}
	if c.RedisDB < 0 {
		return fmt.Errorf("REDIS_DB must be >= 0, got %d", c.RedisDB)
	}
	if c.RedisChannel == "" {
		return fmt.Errorf("REDIS_CHANNEL cannot be empty")
	}

	// Client send buffer size validation
	if c.ClientSendBufferSize < 64 {
		return fmt.Errorf("WS_CLIENT_SEND_BUFFER_SIZE must be >= 64, got %d", c.ClientSendBufferSize)
	}
	if c.ClientSendBufferSize > 4096 {
		return fmt.Errorf("WS_CLIENT_SEND_BUFFER_SIZE must be <= 4096 (4096 = ~2MB per client), got %d", c.ClientSendBufferSize)
	}

	// CPU poll interval validation
	if c.CPUPollInterval < 100*time.Millisecond {
		return fmt.Errorf("CPU_POLL_INTERVAL must be >= 100ms, got %v", c.CPUPollInterval)
	}
	if c.CPUPollInterval > c.MetricsInterval {
		return fmt.Errorf("CPU_POLL_INTERVAL (%v) should be <= METRICS_INTERVAL (%v)", c.CPUPollInterval, c.MetricsInterval)
	}

	return nil
}

// Print logs configuration for debugging (human-readable format)
// For production, use LogConfig() with structured logging
func (c *Config) Print() {
	fmt.Println("=== Server Configuration ===")
	fmt.Printf("Environment:     %s\n", c.Environment)
	fmt.Printf("Address:         %s\n", c.Addr)
	fmt.Printf("Kafka Brokers:   %s\n", c.KafkaBrokers)
	fmt.Printf("Consumer Group:  %s\n", c.ConsumerGroup)
	fmt.Println("\n=== Resource Limits ===")
	fmt.Printf("CPU Limit:       %.1f cores\n", c.CPULimit)
	fmt.Printf("Memory Limit:    %d MB\n", c.MemoryLimit/(1024*1024))
	fmt.Printf("Max Connections: %d\n", c.MaxConnections)
	fmt.Println("\n=== Rate Limits ===")
	fmt.Printf("Kafka Messages:  %d/sec\n", c.MaxKafkaRate)
	fmt.Printf("Broadcasts:      %d/sec\n", c.MaxBroadcastRate)
	fmt.Printf("Max Goroutines:  %d\n", c.MaxGoroutines)
	fmt.Println("\n=== Connection Rate Limiting (DoS Protection) ===")
	fmt.Printf("Enabled:         %v\n", c.ConnectionRateLimitEnabled)
	fmt.Printf("IP Burst:        %d connections\n", c.ConnRateLimitIPBurst)
	fmt.Printf("IP Rate:         %.1f conn/sec\n", c.ConnRateLimitIPRate)
	fmt.Printf("Global Burst:    %d connections\n", c.ConnRateLimitGlobalBurst)
	fmt.Printf("Global Rate:     %.1f conn/sec\n", c.ConnRateLimitGlobalRate)
	fmt.Println("\n=== Safety Thresholds (with Hysteresis) ===")
	fmt.Printf("CPU Reject (upper):    %.1f%%\n", c.CPURejectThreshold)
	fmt.Printf("CPU Reject (lower):    %.1f%%\n", c.CPURejectThresholdLower)
	fmt.Printf("CPU Reject Band:       %.1f%%\n", c.CPURejectThreshold-c.CPURejectThresholdLower)
	fmt.Printf("CPU Pause (upper):     %.1f%%\n", c.CPUPauseThreshold)
	fmt.Printf("CPU Pause (lower):     %.1f%%\n", c.CPUPauseThresholdLower)
	fmt.Printf("CPU Pause Band:        %.1f%%\n", c.CPUPauseThreshold-c.CPUPauseThresholdLower)
	fmt.Println("\n=== TCP/Network Tuning ===")
	fmt.Printf("Listen Backlog:  %d\n", c.TCPListenBacklog)
	fmt.Printf("Read Timeout:    %s\n", c.HTTPReadTimeout)
	fmt.Printf("Write Timeout:   %s\n", c.HTTPWriteTimeout)
	fmt.Printf("Idle Timeout:    %s\n", c.HTTPIdleTimeout)
	fmt.Println("\n=== Logging ===")
	fmt.Printf("Level:           %s\n", c.LogLevel)
	fmt.Printf("Format:          %s\n", c.LogFormat)
	fmt.Println("\n=== Redis (BroadcastBus) ===")
	fmt.Printf("Sentinel Addrs:  %v\n", c.RedisSentinelAddrs)
	fmt.Printf("Master Name:     %s\n", c.RedisMasterName)
	fmt.Printf("Channel:         %s\n", c.RedisChannel)
	fmt.Printf("Database:        %d\n", c.RedisDB)
	fmt.Println("\n=== Client Buffers ===")
	fmt.Printf("Send Buffer:     %d slots (~%dKB/client)\n", c.ClientSendBufferSize, c.ClientSendBufferSize/2)
	fmt.Println("\n=== Monitoring ===")
	fmt.Printf("Metrics Interval: %s\n", c.MetricsInterval)
	fmt.Printf("CPU Poll Interval: %s\n", c.CPUPollInterval)
	fmt.Println("============================")
}

// LogConfig logs configuration using structured logging (Loki-compatible)
func (c *Config) LogConfig(logger zerolog.Logger) {
	logger.Info().
		Str("environment", c.Environment).
		Str("addr", c.Addr).
		Str("kafka_brokers", c.KafkaBrokers).
		Str("consumer_group", c.ConsumerGroup).
		Float64("cpu_limit", c.CPULimit).
		Int64("memory_limit_mb", c.MemoryLimit/(1024*1024)).
		Int("max_connections", c.MaxConnections).
		Int("max_kafka_rate", c.MaxKafkaRate).
		Int("max_broadcast_rate", c.MaxBroadcastRate).
		Int("max_goroutines", c.MaxGoroutines).
		Bool("conn_rate_limit_enabled", c.ConnectionRateLimitEnabled).
		Int("conn_rate_limit_ip_burst", c.ConnRateLimitIPBurst).
		Float64("conn_rate_limit_ip_rate", c.ConnRateLimitIPRate).
		Int("conn_rate_limit_global_burst", c.ConnRateLimitGlobalBurst).
		Float64("conn_rate_limit_global_rate", c.ConnRateLimitGlobalRate).
		Float64("cpu_reject_threshold", c.CPURejectThreshold).
		Float64("cpu_reject_threshold_lower", c.CPURejectThresholdLower).
		Float64("cpu_pause_threshold", c.CPUPauseThreshold).
		Float64("cpu_pause_threshold_lower", c.CPUPauseThresholdLower).
		Int("tcp_listen_backlog", c.TCPListenBacklog).
		Dur("http_read_timeout", c.HTTPReadTimeout).
		Dur("http_write_timeout", c.HTTPWriteTimeout).
		Dur("http_idle_timeout", c.HTTPIdleTimeout).
		Dur("metrics_interval", c.MetricsInterval).
		Dur("cpu_poll_interval", c.CPUPollInterval).
		Str("log_level", c.LogLevel).
		Str("log_format", c.LogFormat).
		Strs("redis_sentinel_addrs", c.RedisSentinelAddrs).
		Str("redis_master_name", c.RedisMasterName).
		Str("redis_channel", c.RedisChannel).
		Int("redis_db", c.RedisDB).
		Int("client_send_buffer_size", c.ClientSendBufferSize).
		Msg("Server configuration loaded")
}
