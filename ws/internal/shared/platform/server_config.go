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
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// WebSocket ping/pong validation constants.
// These define the minimum acceptable values to prevent misconfiguration.
const (
	// MinPongWait is the minimum allowed timeout for receiving a pong response.
	// Values under 10s cause spurious disconnects because the ping must traverse
	// the full proxy chain (8 hops in local dev: client → gateway → ws-server →
	// shard and back) within this window.
	MinPongWait = 10 * time.Second

	// MinPingPeriod is the minimum allowed interval for sending pings.
	// Values under 5s create excessive ping traffic (>20% of connection
	// lifetime becomes ping/pong overhead) and provide no reliability benefit.
	MinPingPeriod = 5 * time.Second
)

// ServerConfig holds all server configuration
// Tags:
//
//	env: Environment variable name
//	envDefault: Default value if not set
//	required: Must be provided (no default)
type ServerConfig struct {
	// Server basics
	Addr string `env:"WS_ADDR" envDefault:":3002"`

	// Message Backend Selection
	//
	// Controls which message ingestion/persistence layer the ws-server uses.
	// - "direct": Messages flow directly to broadcast bus. No persistence, no replay.
	//   Zero external dependencies beyond the broadcast bus. Default for lowest friction.
	// - "kafka": Full Kafka/Redpanda integration. Persistence, offset-based replay,
	//   multi-tenant consumer isolation. Requires Kafka infrastructure.
	// - "nats": NATS JetStream persistent streams. Sequence-based replay,
	//   stream-per-tenant isolation. Lighter than Kafka.
	MessageBackend string `env:"MESSAGE_BACKEND" envDefault:"direct"`

	// Kafka Configuration (only used when MESSAGE_BACKEND=kafka)
	KafkaBrokers string `env:"KAFKA_BROKERS" envDefault:"localhost:19092"`

	// KafkaConsumerEnabled controls whether the Kafka consumer pool is started.
	// When false, the consumer pool is not created — no consumer group join, no message
	// consumption. WebSocket connections still work (connection-only mode for loadtesting).
	// Default: true
	KafkaConsumerEnabled bool `env:"KAFKA_CONSUMER_ENABLED" envDefault:"true"`

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

	// Kafka Topic Defaults (for on-demand topic creation by KafkaBackend)
	KafkaDefaultPartitions        int `env:"KAFKA_DEFAULT_PARTITIONS" envDefault:"1"`
	KafkaDefaultReplicationFactor int `env:"KAFKA_DEFAULT_REPLICATION_FACTOR" envDefault:"1"`

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

	// KafkaTopicNamespaceOverride overrides ENVIRONMENT for Kafka topic naming only.
	// If empty, defaults to normalized ENVIRONMENT value via kafka.ResolveNamespace().
	// NOT allowed in production — startup validation blocks this.
	//
	// Use cases:
	//   - Set to "prod" in dev/stg environment to consume from production topics
	//   - Keeps logs/metrics accurate (shows "dev" not "prod")
	//
	// See ws/internal/shared/kafka/config.go for detailed documentation.
	KafkaTopicNamespaceOverride string `env:"KAFKA_TOPIC_NAMESPACE_OVERRIDE" envDefault:""`

	// ValidNamespaces is a comma-separated list of allowed topic namespace prefixes.
	// Used to validate KafkaTopicNamespaceOverride and topic formats at runtime.
	ValidNamespaces string `env:"VALID_NAMESPACES" envDefault:"local,dev,stag,prod"`

	// DefaultTenantID disables multi-tenant support. All messages
	// are routed to this tenant. Only used when AUTH_ENABLED=false.
	DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"odin"`

	// NOTE: Authentication is now handled by ws-gateway
	// ws-server is a dumb broadcaster with network-level security via NetworkPolicy

	// Valkey Configuration (for BroadcastBus when BROADCAST_TYPE=valkey)
	// Supports both self-hosted Sentinel (3 addresses) and single instance (1 address)
	ValkeyAddrs      []string `env:"VALKEY_ADDRS" envSeparator:","`
	ValkeyMasterName string   `env:"VALKEY_MASTER_NAME" envDefault:"mymaster"`
	ValkeyPassword   string   `env:"VALKEY_PASSWORD"`
	ValkeyDB         int      `env:"VALKEY_DB" envDefault:"0"`
	ValkeyChannel    string   `env:"VALKEY_CHANNEL" envDefault:"ws.broadcast"`

	// Slow Client Detection
	//
	// When a client's send buffer is full, the server tracks consecutive failed send attempts.
	// After SlowClientMaxAttempts consecutive failures, the client is disconnected.
	//
	// Industry comparison:
	//   - Coinbase: 2 attempts (aggressive)
	//   - Binance: No automatic disconnect (relies on ping timeout)
	//   - FIX protocol: 5 second timeout (lenient)
	//
	// Default: 3 (balanced - tolerates brief hiccups, disconnects persistent slow clients)
	// Range: 1-10 (1 = aggressive, 10 = very lenient)
	SlowClientMaxAttempts int `env:"WS_SLOW_CLIENT_MAX_ATTEMPTS" envDefault:"3"`

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

	// NATS Broadcast Bus TLS (for managed NATS: Synadia Cloud, etc.)
	NATSTLSEnabled  bool   `env:"NATS_TLS_ENABLED" envDefault:"false"`
	NATSTLSInsecure bool   `env:"NATS_TLS_INSECURE" envDefault:"false"`
	NATSTLSCAPath   string `env:"NATS_TLS_CA_PATH"`

	// Valkey Broadcast Bus TLS (for managed Valkey/Redis: ElastiCache, Memorystore, Upstash, etc.)
	ValkeyTLSEnabled  bool   `env:"VALKEY_TLS_ENABLED" envDefault:"false"`
	ValkeyTLSInsecure bool   `env:"VALKEY_TLS_INSECURE" envDefault:"false"`
	ValkeyTLSCAPath   string `env:"VALKEY_TLS_CA_PATH"`

	// NATS JetStream Configuration (only used when MESSAGE_BACKEND=nats)
	//
	// Connects to an external NATS server with JetStream enabled for persistent
	// message streams and sequence-based replay. Each tenant gets its own stream
	// for natural noisy-tenant isolation.
	NATSJetStreamURLs        string        `env:"NATS_JETSTREAM_URLS"`                              // Comma-separated NATS URLs
	NATSJetStreamToken       string        `env:"NATS_JETSTREAM_TOKEN"`                             // Auth token
	NATSJetStreamUser        string        `env:"NATS_JETSTREAM_USER"`                              // Username
	NATSJetStreamPassword    string        `env:"NATS_JETSTREAM_PASSWORD"`                          // Password
	NATSJetStreamReplicas    int           `env:"NATS_JETSTREAM_REPLICAS" envDefault:"1"`            // Stream replicas
	NATSJetStreamMaxAge      time.Duration `env:"NATS_JETSTREAM_MAX_AGE" envDefault:"24h"`           // Message retention
	NATSJetStreamTLSEnabled  bool          `env:"NATS_JETSTREAM_TLS_ENABLED" envDefault:"false"`     // TLS for managed NATS (Synadia Cloud, etc.)
	NATSJetStreamTLSInsecure bool          `env:"NATS_JETSTREAM_TLS_INSECURE" envDefault:"false"`    // Skip TLS verification (not for production)
	NATSJetStreamTLSCAPath   string        `env:"NATS_JETSTREAM_TLS_CA_PATH"`                       // Custom CA certificate path

	// Topic Refresh Interval
	//
	// How often to periodically re-sync tenant topics/streams from the registry.
	// This is a safety net — primary updates come from the gRPC stream. The periodic
	// refresh catches any events missed during transient disconnects.
	// Applies to both Kafka (MultiTenantConsumerPool) and NATS JetStream backends.
	TopicRefreshInterval time.Duration `env:"TOPIC_REFRESH_INTERVAL" envDefault:"60s"`

	// Multi-Tenant Consumer Configuration (Required)
	//
	// The server uses MultiTenantConsumerPool which:
	// - Receives tenant topics via gRPC streaming from the provisioning service
	// - Manages consumer groups dynamically based on tenant consumer_type:
	//   - Shared tenants: {env}-shared-consumer consumer group
	//   - Dedicated tenants: {env}-{tenant_id}-consumer consumer group
	//
	// ProvisioningGRPCAddr: Address of the provisioning gRPC service for topic discovery
	ProvisioningGRPCAddr  string        `env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`
	GRPCReconnectDelay    time.Duration `env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`
	GRPCReconnectMaxDelay time.Duration `env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`

	// WebSocket Ping/Pong Configuration
	//
	// These settings control the keep-alive mechanism between the shard and clients.
	// In a proxied architecture (gateway → ws-server → shard), pings travel through
	// multiple hops, requiring a larger buffer than direct connections.
	//
	// Buffer = PongWait - PingPeriod
	// The buffer must accommodate the round-trip time through all proxies.
	// With 8 network hops (local Kind + port-forward), 15+ seconds is recommended.
	//
	// Example with defaults (60s/45s):
	//   - Shard sends ping at t=0
	//   - Ping travels: Shard → ShardProxy → ws-server → Gateway → Client
	//   - Client sends pong
	//   - Pong travels: Client → Gateway → ws-server → ShardProxy → Shard
	//   - If pong arrives by t=60s, connection is healthy
	//   - 15 second buffer for network latency, GC pauses, etc.
	//
	// Common issues:
	//   - Buffer too small (<5s): Connections drop during GC pauses or network hiccups
	//   - PingPeriod >= PongWait: Invalid, ping would always timeout
	//
	PongWait   time.Duration `env:"WS_PONG_WAIT" envDefault:"60s"`
	PingPeriod time.Duration `env:"WS_PING_PERIOD" envDefault:"45s"`

	// WriteWait is the timeout for WebSocket write operations.
	// 5s is sufficient for local writes; network latency is handled by TCP.
	WriteWait time.Duration `env:"WS_WRITE_WAIT" envDefault:"5s"`

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
	if err := ValidateLogLevel(c.LogLevel); err != nil {
		return err
	}

	if err := ValidateLogFormat(c.LogFormat); err != nil {
		return err
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

	// Slow client threshold validation
	if c.SlowClientMaxAttempts < 1 || c.SlowClientMaxAttempts > 10 {
		return fmt.Errorf("WS_SLOW_CLIENT_MAX_ATTEMPTS must be 1-10, got %d", c.SlowClientMaxAttempts)
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

	// Message backend validation
	validBackends := map[string]bool{"direct": true, "kafka": true, "nats": true}
	if !validBackends[c.MessageBackend] {
		return fmt.Errorf("MESSAGE_BACKEND must be one of: direct, kafka, nats (got: %q)", c.MessageBackend)
	}

	// NATS JetStream validation (when MESSAGE_BACKEND=nats)
	if c.MessageBackend == "nats" {
		if c.NATSJetStreamURLs == "" {
			return errors.New("NATS_JETSTREAM_URLS is required when MESSAGE_BACKEND=nats")
		}
		if c.NATSJetStreamReplicas < 1 {
			return fmt.Errorf("NATS_JETSTREAM_REPLICAS must be >= 1, got %d", c.NATSJetStreamReplicas)
		}
		if c.NATSJetStreamMaxAge < 1*time.Minute {
			return fmt.Errorf("NATS_JETSTREAM_MAX_AGE must be >= 1m, got %v", c.NATSJetStreamMaxAge)
		}
	}

	// Topic refresh interval validation (applies to kafka and nats backends)
	if c.MessageBackend != "direct" && c.TopicRefreshInterval < 1*time.Second {
		return fmt.Errorf("TOPIC_REFRESH_INTERVAL must be >= 1s, got %v", c.TopicRefreshInterval)
	}

	// Kafka topic defaults validation (only relevant when MESSAGE_BACKEND=kafka)
	if c.MessageBackend == "kafka" {
		if c.KafkaDefaultPartitions < 1 {
			return fmt.Errorf("KAFKA_DEFAULT_PARTITIONS must be >= 1, got %d", c.KafkaDefaultPartitions)
		}
		if c.KafkaDefaultReplicationFactor < 1 {
			return fmt.Errorf("KAFKA_DEFAULT_REPLICATION_FACTOR must be >= 1, got %d", c.KafkaDefaultReplicationFactor)
		}
	}

	// Kafka SASL validation (only relevant when MESSAGE_BACKEND=kafka)
	if c.MessageBackend == "kafka" && c.KafkaSASLEnabled {
		if err := ValidateKafkaSASLMechanism(c.KafkaSASLMechanism); err != nil {
			return err
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

	// Provisioning gRPC validation (required for topic discovery)
	if c.ProvisioningGRPCAddr == "" {
		return errors.New("PROVISIONING_GRPC_ADDR is required")
	}
	if c.GRPCReconnectDelay < 100*time.Millisecond {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_DELAY must be >= 100ms, got %v", c.GRPCReconnectDelay)
	}
	if c.GRPCReconnectMaxDelay < c.GRPCReconnectDelay {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_MAX_DELAY (%v) must be >= PROVISIONING_GRPC_RECONNECT_DELAY (%v)",
			c.GRPCReconnectMaxDelay, c.GRPCReconnectDelay)
	}

	// WebSocket ping/pong validation
	// Values come from environment config with sensible defaults via envDefault.
	// See MinPongWait and MinPingPeriod constants for rationale on minimum values.
	// PingPeriod < PongWait is required for the buffer to exist.
	if c.PongWait < MinPongWait {
		return fmt.Errorf("WS_PONG_WAIT must be >= %v, got %v", MinPongWait, c.PongWait)
	}
	if c.PingPeriod < MinPingPeriod {
		return fmt.Errorf("WS_PING_PERIOD must be >= %v, got %v", MinPingPeriod, c.PingPeriod)
	}
	if c.PingPeriod >= c.PongWait {
		return fmt.Errorf("WS_PING_PERIOD (%v) must be < WS_PONG_WAIT (%v)", c.PingPeriod, c.PongWait)
	}
	// WriteWait should be reasonable (1-30 seconds)
	if c.WriteWait < time.Second || c.WriteWait > 30*time.Second {
		return fmt.Errorf("WS_WRITE_WAIT must be 1s-30s, got %v", c.WriteWait)
	}

	// Prod guard: namespace override is only for dev/stg
	env := strings.ToLower(strings.TrimSpace(c.Environment))
	if env == "prod" && c.KafkaTopicNamespaceOverride != "" {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE is not allowed in production (environment: %s)", c.Environment)
	}

	return nil
}

// Print logs server configuration for debugging (human-readable format)
// For production, use LogConfig() with structured logging
func (c *ServerConfig) Print() {
	fmt.Println("=== Server Configuration ===")
	fmt.Printf("Environment:     %s\n", c.Environment)
	if c.KafkaTopicNamespaceOverride != "" {
		fmt.Printf("Topic Namespace: %s (override)\n", c.KafkaTopicNamespaceOverride)
	}
	fmt.Printf("Address:         %s\n", c.Addr)
	fmt.Printf("Message Backend: %s\n", c.MessageBackend)
	fmt.Printf("Kafka Brokers:   %s\n", c.KafkaBrokers)
	fmt.Printf("Consumer Enabled: %v\n", c.KafkaConsumerEnabled)
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
	fmt.Printf("Default Tenant:  %s\n", c.DefaultTenantID)
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
		fmt.Printf("TLS Enabled:     %v\n", c.NATSTLSEnabled)
	}
	if c.BroadcastType == "valkey" && c.ValkeyTLSEnabled {
		fmt.Printf("Valkey TLS:      %v\n", c.ValkeyTLSEnabled)
	}
	if c.MessageBackend == "nats" {
		fmt.Println("\n=== NATS JetStream ===")
		fmt.Printf("JetStream URLs:  %s\n", c.NATSJetStreamURLs)
		fmt.Printf("Replicas:        %d\n", c.NATSJetStreamReplicas)
		fmt.Printf("Max Age:         %s\n", c.NATSJetStreamMaxAge)
		fmt.Printf("TLS Enabled:     %v\n", c.NATSJetStreamTLSEnabled)
		fmt.Printf("Token Auth:      %v\n", c.NATSJetStreamToken != "")
		fmt.Printf("User Auth:       %v\n", c.NATSJetStreamUser != "")
	}
	fmt.Println("\n=== Client Buffers ===")
	fmt.Printf("Send Buffer:     %d slots (~%dKB/client)\n", c.ClientSendBufferSize, c.ClientSendBufferSize/2)
	fmt.Println("\n=== Slow Client Detection ===")
	fmt.Printf("Max Attempts:    %d\n", c.SlowClientMaxAttempts)
	fmt.Println("\n=== Multi-Tenant Consumer ===")
	fmt.Printf("Provisioning gRPC:   %s\n", c.ProvisioningGRPCAddr)
	fmt.Printf("Reconnect Delay:     %s\n", c.GRPCReconnectDelay)
	fmt.Printf("Reconnect Max Delay: %s\n", c.GRPCReconnectMaxDelay)
	fmt.Println("\n=== WebSocket Ping/Pong ===")
	fmt.Printf("Pong Wait:           %s\n", c.PongWait)
	fmt.Printf("Ping Period:         %s\n", c.PingPeriod)
	fmt.Printf("Write Wait:          %s\n", c.WriteWait)
	fmt.Printf("Buffer:              %s\n", c.PongWait-c.PingPeriod)
	fmt.Println("\n=== Monitoring ===")
	fmt.Printf("Metrics Interval: %s\n", c.MetricsInterval)
	fmt.Printf("CPU Poll Interval: %s\n", c.CPUPollInterval)
	fmt.Println("============================")
}

// LogConfig logs server configuration using structured logging (Loki-compatible)
func (c *ServerConfig) LogConfig(logger zerolog.Logger) {
	logger.Info().
		Str("environment", c.Environment).
		Str("kafka_topic_namespace_override", c.KafkaTopicNamespaceOverride).
		Str("addr", c.Addr).
		Str("message_backend", c.MessageBackend).
		Str("kafka_brokers", c.KafkaBrokers).
		Bool("kafka_consumer_enabled", c.KafkaConsumerEnabled).
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
		Bool("nats_tls_enabled", c.NATSTLSEnabled).
		Bool("valkey_tls_enabled", c.ValkeyTLSEnabled).
		Str("nats_jetstream_urls", c.NATSJetStreamURLs).
		Int("nats_jetstream_replicas", c.NATSJetStreamReplicas).
		Dur("nats_jetstream_max_age", c.NATSJetStreamMaxAge).
		Bool("nats_jetstream_tls_enabled", c.NATSJetStreamTLSEnabled).
		Bool("nats_jetstream_token_set", c.NATSJetStreamToken != "").
		Bool("nats_jetstream_user_set", c.NATSJetStreamUser != "").
		Int("client_send_buffer_size", c.ClientSendBufferSize).
		Int("slow_client_max_attempts", c.SlowClientMaxAttempts).
		Int("kafka_default_partitions", c.KafkaDefaultPartitions).
		Int("kafka_default_replication_factor", c.KafkaDefaultReplicationFactor).
		Dur("topic_refresh_interval", c.TopicRefreshInterval).
		Str("provisioning_grpc_addr", c.ProvisioningGRPCAddr).
		Dur("grpc_reconnect_delay", c.GRPCReconnectDelay).
		Dur("grpc_reconnect_max_delay", c.GRPCReconnectMaxDelay).
		Str("default_tenant_id", c.DefaultTenantID).
		Dur("ws_pong_wait", c.PongWait).
		Dur("ws_ping_period", c.PingPeriod).
		Dur("ws_write_wait", c.WriteWait).
		Msg("Server configuration loaded")
}
