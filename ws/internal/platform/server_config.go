// Package platform provides environment configuration and container resource
// detection. It handles loading configuration from environment variables and
// detecting cgroup-based resource limits for containers.
//
// Key features:
//   - Environment variable parsing with defaults via caarlos0/env
//   - Optional .env file loading via godotenv
//   - Cgroup v1/v2 CPU and memory limit detection
//   - Automatic GOMAXPROCS configuration via automaxprocs
package platform

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// ServerConfig holds all server configuration
// Tags:
//
//	env: Environment variable name
//	envDefault: Default value if not set
//	required: Must be provided (no default)
type ServerConfig struct {
	// Server basics
	Addr          string `env:"WS_ADDR" envDefault:":3002"`
	KafkaBrokers  string `env:"KAFKA_BROKERS" envDefault:"localhost:19092"`
	ConsumerGroup string `env:"KAFKA_CONSUMER_GROUP" envDefault:"ws-server-group"`

	// Kafka Security - SASL Authentication
	//
	// For connecting to managed Kafka/Redpanda services that require authentication.
	// When KafkaSASLEnabled=false (default), connects without authentication (local dev).
	//
	// Supported mechanisms:
	// - scram-sha-256: SCRAM-SHA-256 (recommended, widely supported)
	// - scram-sha-512: SCRAM-SHA-512 (stronger, less common)
	KafkaSASLEnabled   bool   `env:"KAFKA_SASL_ENABLED" envDefault:"false"`
	KafkaSASLMechanism string `env:"KAFKA_SASL_MECHANISM"` // scram-sha-256 or scram-sha-512
	KafkaSASLUsername  string `env:"KAFKA_SASL_USERNAME"`
	KafkaSASLPassword  string `env:"KAFKA_SASL_PASSWORD"`

	// Kafka Security - TLS Encryption
	//
	// For encrypted connections to Kafka. Required for most managed services.
	// When KafkaTLSEnabled=false (default), connects without TLS (local dev).
	//
	// KafkaTLSInsecure: Skip server certificate verification (NOT for production)
	// KafkaTLSCAPath: Path to CA certificate for server verification
	KafkaTLSEnabled  bool   `env:"KAFKA_TLS_ENABLED" envDefault:"false"`
	KafkaTLSInsecure bool   `env:"KAFKA_TLS_INSECURE" envDefault:"false"`
	KafkaTLSCAPath   string `env:"KAFKA_TLS_CA_PATH"`

	// Resource limits
	// Note: CPU limit is detected automatically via automaxprocs reading cgroup
	// WS_CPU_LIMIT env var is only used by Docker to set the container limit
	MemoryLimit int64 `env:"WS_MEMORY_LIMIT" envDefault:"536870912"` // 512MB

	// Capacity
	MaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"500"`

	// Rate limiting
	MaxKafkaRate     int `env:"WS_MAX_KAFKA_RATE" envDefault:"1000"` // Kafka message consumption rate
	MaxBroadcastRate int `env:"WS_MAX_BROADCAST_RATE" envDefault:"20"`
	MaxGoroutines    int `env:"WS_MAX_GOROUTINES" envDefault:"100000"`

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
	// Example with default values (reject: 60% upper, 50% lower):
	//   - CPU rises to 61% → start rejecting connections
	//   - CPU drops to 55% → still rejecting (in deadband)
	//   - CPU drops to 49% → stop rejecting, accept connections
	//   - CPU rises to 55% → still accepting (in deadband)
	//
	// The 10% gap is the "hysteresis band" that provides stability.
	//
	CPURejectThreshold      float64 `env:"WS_CPU_REJECT_THRESHOLD" envDefault:"60.0"`       // Upper: start rejecting above this %
	CPURejectThresholdLower float64 `env:"WS_CPU_REJECT_THRESHOLD_LOWER" envDefault:"50.0"` // Lower: stop rejecting below this %
	CPUPauseThreshold       float64 `env:"WS_CPU_PAUSE_THRESHOLD" envDefault:"70.0"`        // Upper: pause Kafka above this %
	CPUPauseThresholdLower  float64 `env:"WS_CPU_PAUSE_THRESHOLD_LOWER" envDefault:"60.0"`  // Lower: resume Kafka below this %

	// TCP/Network Tuning (Burst Tolerance)
	//
	// These settings improve tolerance to connection bursts by increasing buffers and timeouts.
	// Defaults are conservative - increase for high-burst workloads.
	//
	// REVERT: Set TCP_LISTEN_BACKLOG=0 to disable custom backlog (use Go defaults)
	//         Set HTTP_*_TIMEOUT to lower values if needed
	//
	TCPListenBacklog int           `env:"TCP_LISTEN_BACKLOG" envDefault:"2048"` // TCP accept queue size (0 = Go default ~128)
	HTTPReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`   // HTTP server read timeout
	HTTPWriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`  // HTTP server write timeout
	HTTPIdleTimeout  time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`   // HTTP server idle timeout

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

	// KafkaTopicNamespace overrides ENVIRONMENT for Kafka topic naming only.
	// If empty, defaults to normalized ENVIRONMENT value.
	// Valid values: local, dev, staging, main
	//
	// Use cases:
	//   - Set to "main" in develop environment to consume from production topics
	//   - Keeps logs/metrics accurate (shows "develop" not "main")
	//
	// See ws/internal/kafka/config.go for detailed documentation.
	KafkaTopicNamespace string `env:"KAFKA_TOPIC_NAMESPACE" envDefault:""`

	// NOTE: Authentication is now handled by ws-gateway
	// ws-server is a dumb broadcaster with network-level security via NetworkPolicy

	// Valkey Configuration (for BroadcastBus when BROADCAST_TYPE=valkey)
	// Supports both self-hosted Sentinel (3 addresses) and single instance (1 address)
	ValkeyAddrs      []string `env:"VALKEY_ADDRS" envSeparator:","`
	ValkeyMasterName string   `env:"VALKEY_MASTER_NAME" envDefault:"mymaster"`
	ValkeyPassword   string   `env:"VALKEY_PASSWORD"`
	ValkeyDB         int      `env:"VALKEY_DB" envDefault:"0"`
	ValkeyChannel    string   `env:"VALKEY_CHANNEL" envDefault:"ws.broadcast"`

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

	// Broadcast Bus Configuration
	//
	// BroadcastType: Backend for inter-instance messaging ("valkey" or "nats")
	// - valkey: Redis-compatible Pub/Sub (default, ~1-2ms latency)
	// - nats: NATS Core Pub/Sub (sub-millisecond latency, fire-and-forget)
	//
	// When switching backends:
	// 1. Set BROADCAST_TYPE to the new backend
	// 2. Configure the corresponding backend settings (VALKEY_* or NATS_*)
	// 3. Restart all instances to use the same backend
	BroadcastType string `env:"BROADCAST_TYPE" envDefault:"valkey"`

	// NATS Configuration (for BroadcastBus when BROADCAST_TYPE=nats)
	//
	// NATSURLs: Comma-separated NATS server URLs
	// - Single: nats://localhost:4222
	// - Cluster: nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222
	//
	// NATSClusterMode: Enable cluster features (automatic failover, load balancing)
	// - false: Local development (single server)
	// - true: Production (cluster of NATS servers)
	//
	// NATSSubject: NATS subject for broadcasting (default: ws.broadcast)
	//
	// Authentication (choose one):
	// - NATSToken: Token-based auth (preferred for production)
	// - NATSUser + NATSPassword: User/password auth (alternative)
	NATSURLs        []string `env:"NATS_URLS" envSeparator:","`
	NATSClusterMode bool     `env:"NATS_CLUSTER_MODE" envDefault:"false"`
	NATSSubject     string   `env:"NATS_SUBJECT" envDefault:"ws.broadcast"`
	NATSToken       string   `env:"NATS_TOKEN"`
	NATSUser        string   `env:"NATS_USER"`
	NATSPassword    string   `env:"NATS_PASSWORD"`

	// Multi-Tenant Consumer Configuration (Required)
	//
	// The server uses MultiTenantConsumerPool which:
	// - Queries the provisioning database for tenant topics
	// - Manages consumer groups dynamically based on tenant consumer_type:
	//   - Shared tenants: odin-shared-{namespace} consumer group
	//   - Dedicated tenants: odin-{tenant_id}-{namespace} consumer group
	//
	// ProvisioningDatabaseURL: PostgreSQL connection string for multi-tenant topic registry
	// Required - server will fail to start if not set or DB is unreachable
	// Example: postgres://user:pass@host:5432/provisioning?sslmode=require
	ProvisioningDatabaseURL string `env:"PROVISIONING_DATABASE_URL,required"`

	// TopicRefreshInterval: How often to query the database for new tenant topics
	// Lower values = faster topic discovery, higher DB load
	// Default: 60s (good balance for most deployments)
	TopicRefreshInterval time.Duration `env:"TOPIC_REFRESH_INTERVAL" envDefault:"60s"`

	// Database Connection Pool (for provisioning database when multi-tenant enabled)
	ProvisioningDBMaxOpenConns    int           `env:"PROVISIONING_DB_MAX_OPEN_CONNS" envDefault:"5"`
	ProvisioningDBMaxIdleConns    int           `env:"PROVISIONING_DB_MAX_IDLE_CONNS" envDefault:"2"`
	ProvisioningDBConnMaxLifetime time.Duration `env:"PROVISIONING_DB_CONN_MAX_LIFETIME" envDefault:"5m"`
	ProvisioningDBConnMaxIdleTime time.Duration `env:"PROVISIONING_DB_CONN_MAX_IDLE_TIME" envDefault:"1m"`
}

// LoadServerConfig reads server configuration from .env file and environment variables
// Priority: ENV vars > .env file > defaults
//
// Optional logger parameter for structured logging. If nil, logs to stdout.
func LoadServerConfig(logger *zerolog.Logger) (*ServerConfig, error) {
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

	cfg := &ServerConfig{}

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

// Validate checks server configuration for errors
func (c *ServerConfig) Validate() error {
	// Required fields (no sensible defaults)
	if c.Addr == "" {
		return errors.New("WS_ADDR is required")
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

	// Broadcast bus configuration validation
	// Empty defaults to "valkey" (same as envDefault in struct tag)
	validBroadcastTypes := map[string]bool{"valkey": true, "redis": true, "nats": true, "": true}
	if !validBroadcastTypes[c.BroadcastType] {
		return fmt.Errorf("BROADCAST_TYPE must be one of: valkey, nats (got: %s)", c.BroadcastType)
	}

	// Valkey-specific validation (when BROADCAST_TYPE=valkey or redis)
	if c.BroadcastType == "valkey" || c.BroadcastType == "redis" || c.BroadcastType == "" {
		if len(c.ValkeyAddrs) == 0 {
			return fmt.Errorf("VALKEY_ADDRS is required when BROADCAST_TYPE=%s", c.BroadcastType)
		}
		if c.ValkeyDB < 0 {
			return fmt.Errorf("VALKEY_DB must be >= 0, got %d", c.ValkeyDB)
		}
		if c.ValkeyChannel == "" {
			return errors.New("VALKEY_CHANNEL cannot be empty")
		}
	}

	// NATS-specific validation (when BROADCAST_TYPE=nats)
	if c.BroadcastType == "nats" {
		if len(c.NATSURLs) == 0 {
			return errors.New("NATS_URLS is required when BROADCAST_TYPE=nats")
		}
		if c.NATSSubject == "" {
			return errors.New("NATS_SUBJECT cannot be empty")
		}
		// Cluster mode requires multiple URLs
		if c.NATSClusterMode && len(c.NATSURLs) < 2 {
			return fmt.Errorf("NATS_CLUSTER_MODE=true requires at least 2 NATS URLs, got %d", len(c.NATSURLs))
		}
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

	// NOTE: Authentication is now handled by ws-gateway
	// No auth config validation needed in ws-server

	// Kafka SASL validation
	if c.KafkaSASLEnabled {
		validMechanisms := map[string]bool{"scram-sha-256": true, "scram-sha-512": true}
		if !validMechanisms[c.KafkaSASLMechanism] {
			return fmt.Errorf("KAFKA_SASL_MECHANISM must be 'scram-sha-256' or 'scram-sha-512', got: %s", c.KafkaSASLMechanism)
		}
		if c.KafkaSASLUsername == "" {
			return errors.New("KAFKA_SASL_USERNAME is required when KAFKA_SASL_ENABLED=true")
		}
		if c.KafkaSASLPassword == "" {
			return errors.New("KAFKA_SASL_PASSWORD is required when KAFKA_SASL_ENABLED=true")
		}
	}

	// Kafka TLS validation
	// Note: We don't require CA path - system CA pool is used by default if not specified
	// KafkaTLSInsecure is allowed but should be warned about in production

	// Multi-tenant consumer validation (required)
	if c.ProvisioningDatabaseURL == "" {
		return errors.New("PROVISIONING_DATABASE_URL is required")
	}
	if c.TopicRefreshInterval < 5*time.Second {
		return fmt.Errorf("TOPIC_REFRESH_INTERVAL must be >= 5s, got %v", c.TopicRefreshInterval)
	}
	if c.ProvisioningDBMaxOpenConns < 1 {
		return fmt.Errorf("PROVISIONING_DB_MAX_OPEN_CONNS must be >= 1, got %d", c.ProvisioningDBMaxOpenConns)
	}

	return nil
}

// Print logs server configuration for debugging (human-readable format)
// For production, use LogConfig() with structured logging
func (c *ServerConfig) Print() {
	fmt.Println("=== Server Configuration ===")
	fmt.Printf("Environment:     %s\n", c.Environment)
	if c.KafkaTopicNamespace != "" {
		fmt.Printf("Topic Namespace: %s (override)\n", c.KafkaTopicNamespace)
	}
	fmt.Printf("Address:         %s\n", c.Addr)
	fmt.Printf("Kafka Brokers:   %s\n", c.KafkaBrokers)
	fmt.Printf("Consumer Group:  %s\n", c.ConsumerGroup)
	fmt.Println("\n=== Kafka Security ===")
	fmt.Printf("SASL Enabled:    %v\n", c.KafkaSASLEnabled)
	if c.KafkaSASLEnabled {
		fmt.Printf("SASL Mechanism:  %s\n", c.KafkaSASLMechanism)
		fmt.Printf("SASL Username:   %s\n", c.KafkaSASLUsername)
		fmt.Printf("SASL Password:   %s\n", "****")
	}
	fmt.Printf("TLS Enabled:     %v\n", c.KafkaTLSEnabled)
	if c.KafkaTLSEnabled {
		fmt.Printf("TLS Insecure:    %v\n", c.KafkaTLSInsecure)
		fmt.Printf("TLS CA Path:     %s\n", c.KafkaTLSCAPath)
	}
	fmt.Println("\n=== Resource Limits ===")
	fmt.Printf("GOMAXPROCS:      %d (from cgroup via automaxprocs)\n", runtime.GOMAXPROCS(0))
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
	fmt.Println("\n=== Authentication ===")
	fmt.Println("Auth:            Handled by ws-gateway")
	fmt.Println("\n=== Broadcast Bus ===")
	fmt.Printf("Type:            %s\n", c.BroadcastType)
	if c.BroadcastType == "valkey" || c.BroadcastType == "redis" || c.BroadcastType == "" {
		fmt.Printf("Valkey Addrs:    %v\n", c.ValkeyAddrs)
		fmt.Printf("Master Name:     %s\n", c.ValkeyMasterName)
		fmt.Printf("Channel:         %s\n", c.ValkeyChannel)
		fmt.Printf("Database:        %d\n", c.ValkeyDB)
	}
	if c.BroadcastType == "nats" {
		fmt.Printf("NATS URLs:       %v\n", c.NATSURLs)
		fmt.Printf("Cluster Mode:    %v\n", c.NATSClusterMode)
		fmt.Printf("Subject:         %s\n", c.NATSSubject)
		fmt.Printf("Token Auth:      %v\n", c.NATSToken != "")
		fmt.Printf("User Auth:       %v\n", c.NATSUser != "")
	}
	fmt.Println("\n=== Client Buffers ===")
	fmt.Printf("Send Buffer:     %d slots (~%dKB/client)\n", c.ClientSendBufferSize, c.ClientSendBufferSize/2)
	fmt.Println("\n=== Multi-Tenant Consumer ===")
	fmt.Printf("Provisioning DB:     %s\n", maskDatabaseURL(c.ProvisioningDatabaseURL))
	fmt.Printf("Topic Refresh:       %s\n", c.TopicRefreshInterval)
	fmt.Printf("DB Max Open Conns:   %d\n", c.ProvisioningDBMaxOpenConns)
	fmt.Printf("DB Max Idle Conns:   %d\n", c.ProvisioningDBMaxIdleConns)
	fmt.Println("\n=== Monitoring ===")
	fmt.Printf("Metrics Interval: %s\n", c.MetricsInterval)
	fmt.Printf("CPU Poll Interval: %s\n", c.CPUPollInterval)
	fmt.Println("============================")
}

// LogConfig logs server configuration using structured logging (Loki-compatible)
func (c *ServerConfig) LogConfig(logger zerolog.Logger) {
	logger.Info().
		Str("environment", c.Environment).
		Str("kafka_topic_namespace", c.KafkaTopicNamespace).
		Str("addr", c.Addr).
		Str("kafka_brokers", c.KafkaBrokers).
		Str("consumer_group", c.ConsumerGroup).
		Bool("kafka_sasl_enabled", c.KafkaSASLEnabled).
		Str("kafka_sasl_mechanism", c.KafkaSASLMechanism).
		Bool("kafka_sasl_username_set", c.KafkaSASLUsername != "").
		Bool("kafka_tls_enabled", c.KafkaTLSEnabled).
		Bool("kafka_tls_insecure", c.KafkaTLSInsecure).
		Str("kafka_tls_ca_path", c.KafkaTLSCAPath).
		Int("gomaxprocs", runtime.GOMAXPROCS(0)).
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
		Str("auth", "handled by ws-gateway").
		Str("broadcast_type", c.BroadcastType).
		Strs("valkey_addrs", c.ValkeyAddrs).
		Str("valkey_master_name", c.ValkeyMasterName).
		Str("valkey_channel", c.ValkeyChannel).
		Int("valkey_db", c.ValkeyDB).
		Strs("nats_urls", c.NATSURLs).
		Bool("nats_cluster_mode", c.NATSClusterMode).
		Str("nats_subject", c.NATSSubject).
		Bool("nats_token_set", c.NATSToken != "").
		Bool("nats_user_set", c.NATSUser != "").
		Int("client_send_buffer_size", c.ClientSendBufferSize).
		Dur("topic_refresh_interval", c.TopicRefreshInterval).
		Int("provisioning_db_max_open_conns", c.ProvisioningDBMaxOpenConns).
		Msg("Server configuration loaded")
}
