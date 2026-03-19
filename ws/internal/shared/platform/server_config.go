// Package platform provides environment configuration and container resource
// detection. It handles loading configuration from environment variables and
// detecting cgroup-based resource limits for containers.
//
// Key features:
//   - Environment variable parsing with defaults via caarlos0/env
//   - Optional .env file loading via godotenv
//   - Cgroup v1/v2 CPU and memory limit detection
//   - Container-aware GOMAXPROCS (Go 1.25+ runtime)
package platform

import (
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/alerting"
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
	BaseConfig
	ProvisioningClientConfig
	KafkaNamespaceConfig
	HTTPTimeoutConfig

	// Server basics
	Addr      string `env:"WS_ADDR" envDefault:":3002"`
	NumShards int    `env:"WS_NUM_SHARDS" envDefault:"1"`   // Number of server shards
	BasePort  int    `env:"WS_BASE_PORT" envDefault:"3002"` // Base port for shard binding (3002, 3003, ...)
	LBAddr    string `env:"WS_LB_ADDR" envDefault:":3005"`  // Load balancer listen address

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
	KafkaSASLPassword  string `env:"KAFKA_SASL_PASSWORD" redact:"true"`

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
	// Note: CPU limit is detected automatically by Go runtime reading cgroup (Go 1.25+)
	// WS_CPU_LIMIT env var is only used by Docker to set the container limit
	MemoryLimit int64 `env:"WS_MEMORY_LIMIT" envDefault:"536870912"` // 512MB

	// Capacity
	MaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"500"`

	// Rate limiting
	MaxKafkaMessagesPerSec   int `env:"WS_MAX_KAFKA_RATE" envDefault:"1000"` // Kafka message consumption rate
	MaxBroadcastsPerSec      int `env:"WS_MAX_BROADCAST_RATE" envDefault:"25"`
	MaxGoroutines            int `env:"WS_MAX_GOROUTINES" envDefault:"100000"`
	RateLimitBurstMultiplier int `env:"WS_RATE_LIMIT_BURST_MULTIPLIER" envDefault:"2"` // Burst capacity as a multiple of the steady-state rate

	// Per-client message rate limiting (token bucket algorithm)
	//
	// Controls how fast each WebSocket client can send messages.
	// Uses a token bucket: burst allows instant requests, rate is the sustained limit.
	//
	// Industry comparison:
	//   - Coinbase: 10 orders/second
	//   - Binance: 10 orders/second per symbol, 100/sec total
	//   - Interactive Brokers: 50 orders/second
	ClientMsgBurstLimit int     `env:"WS_CLIENT_MSG_BURST_LIMIT" envDefault:"100"` // Per-client burst capacity
	ClientMsgRatePerSec float64 `env:"WS_CLIENT_MSG_RATE_PER_SEC" envDefault:"10"` // Per-client sustained rate/sec

	// Connection rate limiting (DoS protection)
	ConnectionRateLimitEnabled bool    `env:"CONN_RATE_LIMIT_ENABLED" envDefault:"true"`
	ConnRateLimitIPBurst       int     `env:"CONN_RATE_LIMIT_IP_BURST" envDefault:"100"`
	ConnRateLimitIPRate        float64 `env:"CONN_RATE_LIMIT_IP_RATE" envDefault:"100.0"`
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
	CPURejectThreshold      float64 `env:"WS_CPU_REJECT_THRESHOLD" envDefault:"60.0"`    // Upper: start rejecting above this %
	CPURejectThresholdLower float64 `env:"WS_CPU_REJECT_THRESHOLD_LOWER" envDefault:"0"` // Lower: stop rejecting below this % (0 = auto: upper - 10)
	CPUPauseThreshold       float64 `env:"WS_CPU_PAUSE_THRESHOLD" envDefault:"70.0"`     // Upper: pause Kafka above this %
	CPUPauseThresholdLower  float64 `env:"WS_CPU_PAUSE_THRESHOLD_LOWER" envDefault:"0"`  // Lower: resume Kafka below this % (0 = auto: upper - 10)
	CPUEWMABeta             float64 `env:"WS_CPU_EWMA_BETA" envDefault:"0.8"`            // EWMA decay factor for CPU smoothing (0-1, higher = smoother)

	// TCP/Network Tuning (Burst Tolerance)
	//
	// These settings improve tolerance to connection bursts by increasing buffers and timeouts.
	// Defaults are conservative - increase for high-burst workloads.
	//
	// REVERT: Set TCP_LISTEN_BACKLOG=0 to disable custom backlog (use Go defaults)
	//         Set HTTP_*_TIMEOUT to lower values if needed
	//
	TCPListenBacklog int `env:"TCP_LISTEN_BACKLOG" envDefault:"2048"` // TCP accept queue size (0 = Go default ~128)

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

	// Alerting
	AlertEnabled         bool          `env:"ALERT_ENABLED" envDefault:"false"`
	AlertMinLevel        string        `env:"ALERT_MIN_LEVEL" envDefault:"WARNING"`
	AlertSlackWebhookURL string        `env:"ALERT_SLACK_WEBHOOK_URL" redact:"true"`
	AlertSlackChannel    string        `env:"ALERT_SLACK_CHANNEL"`
	AlertSlackUsername   string        `env:"ALERT_SLACK_USERNAME" envDefault:"AlertBot"`
	AlertSlackTimeout    time.Duration `env:"ALERT_SLACK_TIMEOUT" envDefault:"5s"`
	AlertRateLimitWindow time.Duration `env:"ALERT_RATE_LIMIT_WINDOW" envDefault:"5m"`
	AlertRateLimitMax    int           `env:"ALERT_RATE_LIMIT_MAX" envDefault:"3"`
	AlertConsoleEnabled  bool          `env:"ALERT_CONSOLE_ENABLED" envDefault:"false"`

	// DefaultTenantID disables multi-tenant support. All messages
	// are routed to this tenant. Only used when AUTH_ENABLED=false.
	DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"sukko"`

	// Valkey Configuration (for BroadcastBus when BROADCAST_TYPE=valkey)
	// Supports both self-hosted Sentinel (3 addresses) and single instance (1 address)
	ValkeyAddrs      []string `env:"VALKEY_ADDRS" envSeparator:","`
	ValkeyMasterName string   `env:"VALKEY_MASTER_NAME" envDefault:"mymaster"`
	ValkeyPassword   string   `env:"VALKEY_PASSWORD" redact:"true"`
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
	// BroadcastType: Backend for inter-instance messaging ("nats" or "valkey")
	// - nats: NATS Core Pub/Sub (default, sub-millisecond latency, fire-and-forget)
	// - valkey: Redis-compatible Pub/Sub (~1-2ms latency)
	//
	// When switching backends:
	// 1. Set BROADCAST_TYPE to the new backend
	// 2. Configure the corresponding backend settings (NATS_* or VALKEY_*)
	// 3. Restart all instances to use the same backend
	BroadcastType string `env:"BROADCAST_TYPE" envDefault:"nats"`

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
	NATSToken       string   `env:"NATS_TOKEN" redact:"true"`
	NATSUser        string   `env:"NATS_USER"`
	NATSPassword    string   `env:"NATS_PASSWORD" redact:"true"`

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
	NATSJetStreamURLs        string        `env:"NATS_JETSTREAM_URLS"`                            // Comma-separated NATS URLs
	NATSJetStreamToken       string        `env:"NATS_JETSTREAM_TOKEN" redact:"true"`             // Auth token
	NATSJetStreamUser        string        `env:"NATS_JETSTREAM_USER"`                            // Username
	NATSJetStreamPassword    string        `env:"NATS_JETSTREAM_PASSWORD" redact:"true"`          // Password
	NATSJetStreamReplicas    int           `env:"NATS_JETSTREAM_REPLICAS" envDefault:"1"`         // Stream replicas
	NATSJetStreamMaxAge      time.Duration `env:"NATS_JETSTREAM_MAX_AGE" envDefault:"24h"`        // Message retention
	NATSJetStreamTLSEnabled  bool          `env:"NATS_JETSTREAM_TLS_ENABLED" envDefault:"false"`  // TLS for managed NATS (Synadia Cloud, etc.)
	NATSJetStreamTLSInsecure bool          `env:"NATS_JETSTREAM_TLS_INSECURE" envDefault:"false"` // Skip TLS verification (not for production)
	NATSJetStreamTLSCAPath   string        `env:"NATS_JETSTREAM_TLS_CA_PATH"`                     // Custom CA certificate path

	// Topic Refresh Interval
	//
	// How often to periodically re-sync tenant topics/streams from the registry.
	// This is a safety net — primary updates come from the gRPC stream. The periodic
	// refresh catches any events missed during transient disconnects.
	// Applies to both Kafka (MultiTenantConsumerPool) and NATS JetStream backends.
	TopicRefreshInterval time.Duration `env:"TOPIC_REFRESH_INTERVAL" envDefault:"60s"`

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

	// Handler timeouts
	ReplayTimeout     time.Duration `env:"WS_REPLAY_TIMEOUT" envDefault:"5s"`
	PublishTimeout    time.Duration `env:"WS_PUBLISH_TIMEOUT" envDefault:"5s"`
	MaxReplayMessages int           `env:"WS_MAX_REPLAY_MESSAGES" envDefault:"100"`

	// Topic/backend timeouts
	TopicCreationTimeout time.Duration `env:"WS_TOPIC_CREATION_TIMEOUT" envDefault:"30s"`

	// Orchestration
	ShardDialTimeout           time.Duration `env:"WS_SHARD_DIAL_TIMEOUT" envDefault:"10s"`
	ShardMessageTimeout        time.Duration `env:"WS_SHARD_MESSAGE_TIMEOUT" envDefault:"60s"`
	MetricsAggregationInterval time.Duration `env:"WS_METRICS_AGGREGATION_INTERVAL" envDefault:"5s"`

	// Shutdown
	ShutdownGracePeriod   time.Duration `env:"WS_SHUTDOWN_GRACE_PERIOD" envDefault:"30s"`
	ShutdownCheckInterval time.Duration `env:"WS_SHUTDOWN_CHECK_INTERVAL" envDefault:"1s"`

	// Internal monitoring
	MetricsCollectInterval      time.Duration `env:"WS_METRICS_COLLECT_INTERVAL" envDefault:"2s"`
	MemoryMonitorInterval       time.Duration `env:"WS_MEMORY_MONITOR_INTERVAL" envDefault:"30s"`
	MemoryWarningPercent        int           `env:"WS_MEMORY_WARNING_PERCENT" envDefault:"80"`
	MemoryCriticalPercent       int           `env:"WS_MEMORY_CRITICAL_PERCENT" envDefault:"90"`
	BufferSampleInterval        time.Duration `env:"WS_BUFFER_SAMPLE_INTERVAL" envDefault:"10s"`
	BufferMaxSamples            int           `env:"WS_BUFFER_MAX_SAMPLES" envDefault:"100"`
	BufferHighSaturationPercent int           `env:"WS_BUFFER_HIGH_SATURATION_PERCENT" envDefault:"90"`
	BufferPopulationWarnPercent int           `env:"WS_BUFFER_POPULATION_WARN_PERCENT" envDefault:"25"`

	// Connection rate limiter internals
	ConnRateLimitIPTTL           time.Duration `env:"CONN_RATE_LIMIT_IP_TTL" envDefault:"5m"`
	ConnRateLimitCleanupInterval time.Duration `env:"CONN_RATE_LIMIT_CLEANUP_INTERVAL" envDefault:"1m"`

	// Broadcast bus
	BroadcastBufferSize      int           `env:"BROADCAST_BUFFER_SIZE" envDefault:"1024"`
	BroadcastShutdownTimeout time.Duration `env:"BROADCAST_SHUTDOWN_TIMEOUT" envDefault:"5s"`

	// NATS broadcast tuning
	NATSReconnectBufSize    int           `env:"NATS_RECONNECT_BUF_SIZE" envDefault:"5242880"`
	NATSPingInterval        time.Duration `env:"NATS_PING_INTERVAL" envDefault:"10s"`
	NATSMaxPingsOutstanding int           `env:"NATS_MAX_PINGS_OUTSTANDING" envDefault:"3"`
	NATSReconnectWait       time.Duration `env:"NATS_RECONNECT_WAIT" envDefault:"2s"`
	NATSMaxReconnects       int           `env:"NATS_MAX_RECONNECTS" envDefault:"-1"`
	NATSHealthCheckInterval time.Duration `env:"NATS_HEALTH_CHECK_INTERVAL" envDefault:"10s"`
	NATSFlushTimeout        time.Duration `env:"NATS_FLUSH_TIMEOUT" envDefault:"5s"`

	// Valkey broadcast tuning
	ValkeyPoolSize                  int           `env:"VALKEY_POOL_SIZE" envDefault:"50"`
	ValkeyMinIdleConns              int           `env:"VALKEY_MIN_IDLE_CONNS" envDefault:"10"`
	ValkeyDialTimeout               time.Duration `env:"VALKEY_DIAL_TIMEOUT" envDefault:"5s"`
	ValkeyReadTimeout               time.Duration `env:"VALKEY_READ_TIMEOUT" envDefault:"3s"`
	ValkeyWriteTimeout              time.Duration `env:"VALKEY_WRITE_TIMEOUT" envDefault:"3s"`
	ValkeyMaxRetries                int           `env:"VALKEY_MAX_RETRIES" envDefault:"3"`
	ValkeyMinRetryBackoff           time.Duration `env:"VALKEY_MIN_RETRY_BACKOFF" envDefault:"100ms"`
	ValkeyMaxRetryBackoff           time.Duration `env:"VALKEY_MAX_RETRY_BACKOFF" envDefault:"1s"`
	ValkeyPublishTimeout            time.Duration `env:"VALKEY_PUBLISH_TIMEOUT" envDefault:"100ms"`
	ValkeyStartupPingTimeout        time.Duration `env:"VALKEY_STARTUP_PING_TIMEOUT" envDefault:"5s"`
	ValkeyReconnectInitialBackoff   time.Duration `env:"VALKEY_RECONNECT_INITIAL_BACKOFF" envDefault:"100ms"`
	ValkeyReconnectMaxBackoff       time.Duration `env:"VALKEY_RECONNECT_MAX_BACKOFF" envDefault:"30s"`
	ValkeyReconnectMaxAttempts      int           `env:"VALKEY_RECONNECT_MAX_ATTEMPTS" envDefault:"10"`
	ValkeyHealthCheckInterval       time.Duration `env:"VALKEY_HEALTH_CHECK_INTERVAL" envDefault:"10s"`
	ValkeyHealthCheckTimeout        time.Duration `env:"VALKEY_HEALTH_CHECK_TIMEOUT" envDefault:"5s"`
	ValkeyPublishStalenessThreshold time.Duration `env:"VALKEY_PUBLISH_STALENESS_THRESHOLD" envDefault:"60s"` // Log warning if no publish within this window

	// Kafka consumer tuning
	KafkaBatchSize    int           `env:"KAFKA_BATCH_SIZE" envDefault:"50"`
	KafkaBatchTimeout time.Duration `env:"KAFKA_BATCH_TIMEOUT" envDefault:"10ms"`

	// Kafka consumer transport tuning
	KafkaFetchMaxWait              time.Duration `env:"KAFKA_FETCH_MAX_WAIT" envDefault:"500ms"`
	KafkaFetchMinBytes             int32         `env:"KAFKA_FETCH_MIN_BYTES" envDefault:"1"`
	KafkaFetchMaxBytes             int32         `env:"KAFKA_FETCH_MAX_BYTES" envDefault:"10485760"`
	KafkaSessionTimeout            time.Duration `env:"KAFKA_SESSION_TIMEOUT" envDefault:"30s"`
	KafkaRebalanceTimeout          time.Duration `env:"KAFKA_REBALANCE_TIMEOUT" envDefault:"60s"`
	KafkaReplayFetchMaxBytes       int32         `env:"KAFKA_REPLAY_FETCH_MAX_BYTES" envDefault:"5242880"`
	KafkaBackpressureCheckInterval time.Duration `env:"KAFKA_BACKPRESSURE_CHECK_INTERVAL" envDefault:"100ms"`

	// Kafka producer tuning
	KafkaProducerBatchMaxBytes      int           `env:"KAFKA_PRODUCER_BATCH_MAX_BYTES" envDefault:"1048576"`
	KafkaProducerMaxBufferedRecords int           `env:"KAFKA_PRODUCER_MAX_BUFFERED_RECORDS" envDefault:"10000"`
	KafkaProducerRecordRetries      int           `env:"KAFKA_PRODUCER_RECORD_RETRIES" envDefault:"8"`
	KafkaProducerCBTimeout          time.Duration `env:"KAFKA_PRODUCER_CB_TIMEOUT" envDefault:"30s"`
	KafkaProducerCBMaxFailures      int           `env:"KAFKA_PRODUCER_CB_MAX_FAILURES" envDefault:"5"`
	KafkaProducerCBHalfOpenReqs     int           `env:"KAFKA_PRODUCER_CB_HALF_OPEN_REQS" envDefault:"1"`
	KafkaProducerTopicCacheTTL      time.Duration `env:"KAFKA_PRODUCER_TOPIC_CACHE_TTL" envDefault:"30s"`

	// JetStream backend tuning
	JetStreamReconnectWait   time.Duration `env:"JETSTREAM_RECONNECT_WAIT" envDefault:"2s"`
	JetStreamMaxDeliver      int           `env:"JETSTREAM_MAX_DELIVER" envDefault:"3"`
	JetStreamAckWait         time.Duration `env:"JETSTREAM_ACK_WAIT" envDefault:"30s"`
	JetStreamRefreshTimeout  time.Duration `env:"JETSTREAM_REFRESH_TIMEOUT" envDefault:"30s"`
	JetStreamRefreshInterval time.Duration `env:"JETSTREAM_REFRESH_INTERVAL" envDefault:"60s"`
	JetStreamReplayFetchWait time.Duration `env:"JETSTREAM_REPLAY_FETCH_WAIT" envDefault:"2s"`
	JetStreamMaxAge          time.Duration `env:"JETSTREAM_MAX_AGE" envDefault:"24h"`
}

// LoadServerConfig reads server configuration from .env file and environment variables
// Priority: ENV vars > .env file > defaults
func LoadServerConfig(logger zerolog.Logger) (*ServerConfig, error) {
	// Load .env file (optional - OK if it doesn't exist)
	// In production (Docker), we use environment variables directly
	// In development, .env file provides convenience
	if err := godotenv.Load(); err != nil {
		// Only log, don't fail - we can run without .env file
		logger.Info().Msg("No .env file found (using environment variables only)")
	} else {
		logger.Info().Msg("Loaded configuration from .env file")
	}

	cfg := &ServerConfig{}

	// Parse environment variables into struct
	// This validates types and applies defaults
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Normalize derived fields before validation
	cfg.Normalize()

	// Validation
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	logger.Info().Msg("Configuration loaded and validated successfully")

	return cfg, nil
}

// Normalize computes derived configuration values from user-provided settings.
// It MUST be called before Validate(). LoadServerConfig calls this automatically.
//
// Currently handles:
//   - Auto-computing CPU hysteresis lower thresholds (upper - 10) when not set explicitly (0 = auto).
func (c *ServerConfig) Normalize() {
	if c.CPURejectThresholdLower == 0 {
		c.CPURejectThresholdLower = c.CPURejectThreshold - 10.0
	}
	if c.CPUPauseThresholdLower == 0 {
		c.CPUPauseThresholdLower = c.CPUPauseThreshold - 10.0
	}
}

// Validate checks server configuration for errors.
// Normalize() MUST be called before Validate() to compute derived fields.
func (c *ServerConfig) Validate() error {
	// Required fields (no sensible defaults)
	if c.Addr == "" {
		return errors.New("WS_ADDR is required")
	}

	// Resource limit checks
	if c.MemoryLimit < MinMemoryLimit {
		return fmt.Errorf("WS_MEMORY_LIMIT must be >= %d (64MB), got %d", MinMemoryLimit, c.MemoryLimit)
	}

	// Shard configuration checks
	if c.NumShards < 1 {
		return fmt.Errorf("WS_NUM_SHARDS must be > 0, got %d", c.NumShards)
	}
	if c.BasePort < 1 || c.BasePort > MaxPort {
		return fmt.Errorf("WS_BASE_PORT must be 1-%d, got %d", MaxPort, c.BasePort)
	}
	if maxPort := c.BasePort + c.NumShards - 1; maxPort > MaxPort {
		return fmt.Errorf("WS_BASE_PORT (%d) + WS_NUM_SHARDS (%d) exceeds max port %d (last port would be %d)", c.BasePort, c.NumShards, MaxPort, maxPort)
	}
	if c.LBAddr == "" {
		return errors.New("WS_LB_ADDR must not be empty")
	}

	// Range checks
	if c.MaxConnections < 1 {
		return fmt.Errorf("WS_MAX_CONNECTIONS must be > 0, got %d", c.MaxConnections)
	}
	if c.MaxKafkaMessagesPerSec < 1 {
		return fmt.Errorf("WS_MAX_KAFKA_RATE must be > 0, got %d", c.MaxKafkaMessagesPerSec)
	}
	if c.MaxBroadcastsPerSec < 1 {
		return fmt.Errorf("WS_MAX_BROADCAST_RATE must be > 0, got %d", c.MaxBroadcastsPerSec)
	}
	if c.MaxGoroutines < MinMaxGoroutines {
		return fmt.Errorf("WS_MAX_GOROUTINES must be >= %d, got %d", MinMaxGoroutines, c.MaxGoroutines)
	}
	if c.RateLimitBurstMultiplier < 1 {
		return fmt.Errorf("WS_RATE_LIMIT_BURST_MULTIPLIER must be >= 1, got %d", c.RateLimitBurstMultiplier)
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
	if c.CPUEWMABeta <= 0 || c.CPUEWMABeta >= 1 {
		return fmt.Errorf("WS_CPU_EWMA_BETA must be between 0 and 1 exclusive, got %.2f", c.CPUEWMABeta)
	}

	// Post-Normalize() range validation (Normalize computes lower from upper - 10 when 0)
	if c.CPURejectThresholdLower < 0 {
		return fmt.Errorf("[CONFIG ERROR] auto-computed WS_CPU_REJECT_THRESHOLD_LOWER is negative (%.1f); set WS_CPU_REJECT_THRESHOLD >= 10 or set lower explicitly", c.CPURejectThresholdLower)
	}
	if c.CPUPauseThresholdLower < 0 {
		return fmt.Errorf("[CONFIG ERROR] auto-computed WS_CPU_PAUSE_THRESHOLD_LOWER is negative (%.1f); set WS_CPU_PAUSE_THRESHOLD >= 10 or set lower explicitly", c.CPUPauseThresholdLower)
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

	// Connection rate limit validation (conditional on enabled)
	if c.ConnectionRateLimitEnabled {
		if c.ConnRateLimitIPBurst < 1 {
			return fmt.Errorf("CONN_RATE_LIMIT_IP_BURST must be > 0 when rate limiting enabled, got %d", c.ConnRateLimitIPBurst)
		}
		if c.ConnRateLimitIPRate <= 0 {
			return fmt.Errorf("CONN_RATE_LIMIT_IP_RATE must be > 0 when rate limiting enabled, got %f", c.ConnRateLimitIPRate)
		}
		if c.ConnRateLimitGlobalBurst < 1 {
			return fmt.Errorf("CONN_RATE_LIMIT_GLOBAL_BURST must be > 0 when rate limiting enabled, got %d", c.ConnRateLimitGlobalBurst)
		}
		if c.ConnRateLimitGlobalRate <= 0 {
			return fmt.Errorf("CONN_RATE_LIMIT_GLOBAL_RATE must be > 0 when rate limiting enabled, got %f", c.ConnRateLimitGlobalRate)
		}
	}

	// TCP listen backlog validation
	if c.TCPListenBacklog < 0 {
		return fmt.Errorf("TCP_LISTEN_BACKLOG must be >= 0, got %d", c.TCPListenBacklog)
	}

	// HTTP timeout validation
	if err := c.HTTPTimeoutConfig.Validate(); err != nil {
		return err
	}

	// Metrics interval validation
	if c.MetricsInterval < time.Second {
		return fmt.Errorf("METRICS_INTERVAL must be >= 1s, got %v", c.MetricsInterval)
	}

	// Shared field validation (LogLevel, LogFormat, Environment)
	if err := c.BaseConfig.Validate(); err != nil {
		return err
	}

	// Broadcast bus configuration validation
	validBroadcastTypes := map[string]bool{"valkey": true, "nats": true}
	if !validBroadcastTypes[c.BroadcastType] {
		return fmt.Errorf("[CONFIG ERROR] BROADCAST_TYPE=%q is invalid (valid: nats, valkey)", c.BroadcastType)
	}

	// Valkey-specific validation (when BROADCAST_TYPE=valkey)
	if c.BroadcastType == "valkey" {
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
		return fmt.Errorf("[CONFIG ERROR] MESSAGE_BACKEND=%q is invalid (valid: direct, kafka, nats)", c.MessageBackend)
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
		if err := validateKafkaSASLMechanism(c.KafkaSASLMechanism); err != nil {
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
	if err := c.ProvisioningClientConfig.Validate(); err != nil {
		return err
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

	// --- Externalized constant validation (FR-026a-d) ---

	// Duration fields (FR-026a: reject non-positive)
	if c.ReplayTimeout <= 0 {
		return fmt.Errorf("WS_REPLAY_TIMEOUT must be > 0, got %v", c.ReplayTimeout)
	}
	if c.PublishTimeout <= 0 {
		return fmt.Errorf("WS_PUBLISH_TIMEOUT must be > 0, got %v", c.PublishTimeout)
	}
	if c.TopicCreationTimeout <= 0 {
		return fmt.Errorf("WS_TOPIC_CREATION_TIMEOUT must be > 0, got %v", c.TopicCreationTimeout)
	}
	if c.ShardDialTimeout <= 0 {
		return fmt.Errorf("WS_SHARD_DIAL_TIMEOUT must be > 0, got %v", c.ShardDialTimeout)
	}
	if c.ShardMessageTimeout <= 0 {
		return fmt.Errorf("WS_SHARD_MESSAGE_TIMEOUT must be > 0, got %v", c.ShardMessageTimeout)
	}
	if c.MetricsAggregationInterval <= 0 {
		return fmt.Errorf("WS_METRICS_AGGREGATION_INTERVAL must be > 0, got %v", c.MetricsAggregationInterval)
	}
	if c.ShutdownGracePeriod <= 0 {
		return fmt.Errorf("WS_SHUTDOWN_GRACE_PERIOD must be > 0, got %v", c.ShutdownGracePeriod)
	}
	if c.ShutdownCheckInterval <= 0 {
		return fmt.Errorf("WS_SHUTDOWN_CHECK_INTERVAL must be > 0, got %v", c.ShutdownCheckInterval)
	}
	if c.MetricsCollectInterval <= 0 {
		return fmt.Errorf("WS_METRICS_COLLECT_INTERVAL must be > 0, got %v", c.MetricsCollectInterval)
	}
	if c.MemoryMonitorInterval <= 0 {
		return fmt.Errorf("WS_MEMORY_MONITOR_INTERVAL must be > 0, got %v", c.MemoryMonitorInterval)
	}
	if c.BufferSampleInterval <= 0 {
		return fmt.Errorf("WS_BUFFER_SAMPLE_INTERVAL must be > 0, got %v", c.BufferSampleInterval)
	}
	if c.ConnRateLimitIPTTL <= 0 {
		return fmt.Errorf("CONN_RATE_LIMIT_IP_TTL must be > 0, got %v", c.ConnRateLimitIPTTL)
	}
	if c.ConnRateLimitCleanupInterval <= 0 {
		return fmt.Errorf("CONN_RATE_LIMIT_CLEANUP_INTERVAL must be > 0, got %v", c.ConnRateLimitCleanupInterval)
	}
	if c.BroadcastShutdownTimeout <= 0 {
		return fmt.Errorf("BROADCAST_SHUTDOWN_TIMEOUT must be > 0, got %v", c.BroadcastShutdownTimeout)
	}
	if c.NATSPingInterval <= 0 {
		return fmt.Errorf("NATS_PING_INTERVAL must be > 0, got %v", c.NATSPingInterval)
	}
	if c.NATSReconnectWait <= 0 {
		return fmt.Errorf("NATS_RECONNECT_WAIT must be > 0, got %v", c.NATSReconnectWait)
	}
	if c.NATSHealthCheckInterval <= 0 {
		return fmt.Errorf("NATS_HEALTH_CHECK_INTERVAL must be > 0, got %v", c.NATSHealthCheckInterval)
	}
	if c.NATSFlushTimeout <= 0 {
		return fmt.Errorf("NATS_FLUSH_TIMEOUT must be > 0, got %v", c.NATSFlushTimeout)
	}
	if c.ValkeyDialTimeout <= 0 {
		return fmt.Errorf("VALKEY_DIAL_TIMEOUT must be > 0, got %v", c.ValkeyDialTimeout)
	}
	if c.ValkeyReadTimeout <= 0 {
		return fmt.Errorf("VALKEY_READ_TIMEOUT must be > 0, got %v", c.ValkeyReadTimeout)
	}
	if c.ValkeyWriteTimeout <= 0 {
		return fmt.Errorf("VALKEY_WRITE_TIMEOUT must be > 0, got %v", c.ValkeyWriteTimeout)
	}
	if c.ValkeyMinRetryBackoff <= 0 {
		return fmt.Errorf("VALKEY_MIN_RETRY_BACKOFF must be > 0, got %v", c.ValkeyMinRetryBackoff)
	}
	if c.ValkeyMaxRetryBackoff <= 0 {
		return fmt.Errorf("VALKEY_MAX_RETRY_BACKOFF must be > 0, got %v", c.ValkeyMaxRetryBackoff)
	}
	if c.ValkeyPublishTimeout <= 0 {
		return fmt.Errorf("VALKEY_PUBLISH_TIMEOUT must be > 0, got %v", c.ValkeyPublishTimeout)
	}
	if c.ValkeyStartupPingTimeout <= 0 {
		return fmt.Errorf("VALKEY_STARTUP_PING_TIMEOUT must be > 0, got %v", c.ValkeyStartupPingTimeout)
	}
	if c.ValkeyReconnectInitialBackoff <= 0 {
		return fmt.Errorf("VALKEY_RECONNECT_INITIAL_BACKOFF must be > 0, got %v", c.ValkeyReconnectInitialBackoff)
	}
	if c.ValkeyReconnectMaxBackoff <= 0 {
		return fmt.Errorf("VALKEY_RECONNECT_MAX_BACKOFF must be > 0, got %v", c.ValkeyReconnectMaxBackoff)
	}
	if c.ValkeyHealthCheckInterval <= 0 {
		return fmt.Errorf("VALKEY_HEALTH_CHECK_INTERVAL must be > 0, got %v", c.ValkeyHealthCheckInterval)
	}
	if c.ValkeyHealthCheckTimeout <= 0 {
		return fmt.Errorf("VALKEY_HEALTH_CHECK_TIMEOUT must be > 0, got %v", c.ValkeyHealthCheckTimeout)
	}
	if c.KafkaBatchTimeout <= 0 {
		return fmt.Errorf("KAFKA_BATCH_TIMEOUT must be > 0, got %v", c.KafkaBatchTimeout)
	}
	if c.KafkaFetchMaxWait <= 0 {
		return fmt.Errorf("KAFKA_FETCH_MAX_WAIT must be > 0, got %v", c.KafkaFetchMaxWait)
	}
	if c.KafkaSessionTimeout <= 0 {
		return fmt.Errorf("KAFKA_SESSION_TIMEOUT must be > 0, got %v", c.KafkaSessionTimeout)
	}
	if c.KafkaRebalanceTimeout <= 0 {
		return fmt.Errorf("KAFKA_REBALANCE_TIMEOUT must be > 0, got %v", c.KafkaRebalanceTimeout)
	}
	if c.KafkaBackpressureCheckInterval <= 0 {
		return fmt.Errorf("KAFKA_BACKPRESSURE_CHECK_INTERVAL must be > 0, got %v", c.KafkaBackpressureCheckInterval)
	}
	if c.KafkaProducerCBTimeout <= 0 {
		return fmt.Errorf("KAFKA_PRODUCER_CB_TIMEOUT must be > 0, got %v", c.KafkaProducerCBTimeout)
	}
	if c.KafkaProducerTopicCacheTTL <= 0 {
		return fmt.Errorf("KAFKA_PRODUCER_TOPIC_CACHE_TTL must be > 0, got %v", c.KafkaProducerTopicCacheTTL)
	}
	if c.JetStreamReconnectWait <= 0 {
		return fmt.Errorf("JETSTREAM_RECONNECT_WAIT must be > 0, got %v", c.JetStreamReconnectWait)
	}
	if c.JetStreamAckWait <= 0 {
		return fmt.Errorf("JETSTREAM_ACK_WAIT must be > 0, got %v", c.JetStreamAckWait)
	}
	if c.JetStreamRefreshTimeout <= 0 {
		return fmt.Errorf("JETSTREAM_REFRESH_TIMEOUT must be > 0, got %v", c.JetStreamRefreshTimeout)
	}
	if c.JetStreamRefreshInterval <= 0 {
		return fmt.Errorf("JETSTREAM_REFRESH_INTERVAL must be > 0, got %v", c.JetStreamRefreshInterval)
	}
	if c.JetStreamReplayFetchWait <= 0 {
		return fmt.Errorf("JETSTREAM_REPLAY_FETCH_WAIT must be > 0, got %v", c.JetStreamReplayFetchWait)
	}
	if c.JetStreamMaxAge <= 0 {
		return fmt.Errorf("JETSTREAM_MAX_AGE must be > 0, got %v", c.JetStreamMaxAge)
	}

	// Int fields (must be > 0)
	if c.MaxReplayMessages < 1 {
		return fmt.Errorf("WS_MAX_REPLAY_MESSAGES must be > 0, got %d", c.MaxReplayMessages)
	}
	if c.BufferMaxSamples < 1 {
		return fmt.Errorf("WS_BUFFER_MAX_SAMPLES must be > 0, got %d", c.BufferMaxSamples)
	}
	if c.BroadcastBufferSize < 1 {
		return fmt.Errorf("BROADCAST_BUFFER_SIZE must be > 0, got %d", c.BroadcastBufferSize)
	}
	if c.NATSReconnectBufSize < 1 {
		return fmt.Errorf("NATS_RECONNECT_BUF_SIZE must be > 0, got %d", c.NATSReconnectBufSize)
	}
	if c.NATSMaxPingsOutstanding < 1 {
		return fmt.Errorf("NATS_MAX_PINGS_OUTSTANDING must be > 0, got %d", c.NATSMaxPingsOutstanding)
	}
	if c.ValkeyPoolSize < 1 {
		return fmt.Errorf("VALKEY_POOL_SIZE must be > 0, got %d", c.ValkeyPoolSize)
	}
	if c.ValkeyMinIdleConns < 0 {
		return fmt.Errorf("VALKEY_MIN_IDLE_CONNS must be >= 0, got %d", c.ValkeyMinIdleConns)
	}
	if c.ValkeyMaxRetries < 0 {
		return fmt.Errorf("VALKEY_MAX_RETRIES must be >= 0, got %d", c.ValkeyMaxRetries)
	}
	if c.ValkeyReconnectMaxAttempts < 1 {
		return fmt.Errorf("VALKEY_RECONNECT_MAX_ATTEMPTS must be > 0, got %d", c.ValkeyReconnectMaxAttempts)
	}
	if c.KafkaBatchSize < 1 {
		return fmt.Errorf("KAFKA_BATCH_SIZE must be > 0, got %d", c.KafkaBatchSize)
	}
	if c.KafkaFetchMinBytes < 1 {
		return fmt.Errorf("KAFKA_FETCH_MIN_BYTES must be > 0, got %d", c.KafkaFetchMinBytes)
	}
	if c.KafkaFetchMaxBytes < 1 {
		return fmt.Errorf("KAFKA_FETCH_MAX_BYTES must be > 0, got %d", c.KafkaFetchMaxBytes)
	}
	if c.KafkaReplayFetchMaxBytes < 1 {
		return fmt.Errorf("KAFKA_REPLAY_FETCH_MAX_BYTES must be > 0, got %d", c.KafkaReplayFetchMaxBytes)
	}
	if c.KafkaProducerBatchMaxBytes < 1 || c.KafkaProducerBatchMaxBytes > math.MaxInt32 {
		return fmt.Errorf("KAFKA_PRODUCER_BATCH_MAX_BYTES must be 1-%d, got %d", math.MaxInt32, c.KafkaProducerBatchMaxBytes)
	}
	if c.KafkaProducerMaxBufferedRecords < 1 {
		return fmt.Errorf("KAFKA_PRODUCER_MAX_BUFFERED_RECORDS must be > 0, got %d", c.KafkaProducerMaxBufferedRecords)
	}
	if c.KafkaProducerRecordRetries < 1 {
		return fmt.Errorf("KAFKA_PRODUCER_RECORD_RETRIES must be > 0, got %d", c.KafkaProducerRecordRetries)
	}
	if c.KafkaProducerCBMaxFailures < 1 || c.KafkaProducerCBMaxFailures > math.MaxUint32 {
		return fmt.Errorf("KAFKA_PRODUCER_CB_MAX_FAILURES must be 1-%d, got %d", math.MaxUint32, c.KafkaProducerCBMaxFailures)
	}
	if c.KafkaProducerCBHalfOpenReqs < 1 || c.KafkaProducerCBHalfOpenReqs > math.MaxUint32 {
		return fmt.Errorf("KAFKA_PRODUCER_CB_HALF_OPEN_REQS must be 1-%d, got %d", math.MaxUint32, c.KafkaProducerCBHalfOpenReqs)
	}
	if c.JetStreamMaxDeliver < 1 {
		return fmt.Errorf("JETSTREAM_MAX_DELIVER must be > 0, got %d", c.JetStreamMaxDeliver)
	}

	// Percentage fields (FR-026b: must be 1-100)
	if c.MemoryWarningPercent < 1 || c.MemoryWarningPercent > 100 {
		return fmt.Errorf("WS_MEMORY_WARNING_PERCENT must be 1-100, got %d", c.MemoryWarningPercent)
	}
	if c.MemoryCriticalPercent < 1 || c.MemoryCriticalPercent > 100 {
		return fmt.Errorf("WS_MEMORY_CRITICAL_PERCENT must be 1-100, got %d", c.MemoryCriticalPercent)
	}
	if c.BufferHighSaturationPercent < 1 || c.BufferHighSaturationPercent > 100 {
		return fmt.Errorf("WS_BUFFER_HIGH_SATURATION_PERCENT must be 1-100, got %d", c.BufferHighSaturationPercent)
	}
	if c.BufferPopulationWarnPercent < 1 || c.BufferPopulationWarnPercent > 100 {
		return fmt.Errorf("WS_BUFFER_POPULATION_WARN_PERCENT must be 1-100, got %d", c.BufferPopulationWarnPercent)
	}

	// Ordered pairs (FR-026c: lower < upper)
	if c.MemoryWarningPercent >= c.MemoryCriticalPercent {
		return fmt.Errorf("WS_MEMORY_WARNING_PERCENT (%d) must be < WS_MEMORY_CRITICAL_PERCENT (%d)",
			c.MemoryWarningPercent, c.MemoryCriticalPercent)
	}
	if c.ValkeyMinRetryBackoff >= c.ValkeyMaxRetryBackoff {
		return fmt.Errorf("VALKEY_MIN_RETRY_BACKOFF (%v) must be < VALKEY_MAX_RETRY_BACKOFF (%v)",
			c.ValkeyMinRetryBackoff, c.ValkeyMaxRetryBackoff)
	}
	if c.ValkeyReconnectInitialBackoff >= c.ValkeyReconnectMaxBackoff {
		return fmt.Errorf("VALKEY_RECONNECT_INITIAL_BACKOFF (%v) must be < VALKEY_RECONNECT_MAX_BACKOFF (%v)",
			c.ValkeyReconnectInitialBackoff, c.ValkeyReconnectMaxBackoff)
	}

	// Special values (FR-026d: NATSMaxReconnects allows -1 for unlimited)
	if c.NATSMaxReconnects < -1 {
		return fmt.Errorf("NATS_MAX_RECONNECTS must be >= -1, got %d", c.NATSMaxReconnects)
	}

	// Kafka namespace validation (includes prod guard)
	if err := c.KafkaNamespaceConfig.Validate(c.Environment); err != nil {
		return err
	}

	// Alerting validation (conditional on enabled)
	if c.AlertEnabled {
		if c.AlertSlackWebhookURL == "" && !c.AlertConsoleEnabled {
			return errors.New("ALERT_ENABLED is true but no alerter configured (set ALERT_SLACK_WEBHOOK_URL or ALERT_CONSOLE_ENABLED)")
		}
		if c.AlertSlackTimeout <= 0 {
			return fmt.Errorf("ALERT_SLACK_TIMEOUT must be > 0, got %s", c.AlertSlackTimeout)
		}
		if c.AlertRateLimitWindow <= 0 {
			return fmt.Errorf("ALERT_RATE_LIMIT_WINDOW must be > 0, got %s", c.AlertRateLimitWindow)
		}
		if c.AlertRateLimitMax < 1 {
			return fmt.Errorf("ALERT_RATE_LIMIT_MAX must be >= 1, got %d", c.AlertRateLimitMax)
		}
	}

	return nil
}

// AlertConfig returns an alerting.Config derived from the server configuration.
func (c *ServerConfig) AlertConfig() alerting.Config {
	return alerting.Config{
		Enabled:         c.AlertEnabled,
		MinLevel:        alerting.ParseLevel(c.AlertMinLevel),
		SlackWebhookURL: c.AlertSlackWebhookURL,
		SlackChannel:    c.AlertSlackChannel,
		SlackUsername:   c.AlertSlackUsername,
		ServiceName:     "ws-server",
		Environment:     c.Environment,
		RateLimitWindow: c.AlertRateLimitWindow,
		RateLimitMax:    c.AlertRateLimitMax,
		SlackTimeout:    c.AlertSlackTimeout,
		ConsoleEnabled:  c.AlertConsoleEnabled,
	}
}

// Print logs server configuration for debugging (human-readable format)
// For production, use LogConfig() with structured logging
func (c *ServerConfig) Print() {
	_, _ = fmt.Fprintln(os.Stdout, "=== Server Configuration ===")
	_, _ = fmt.Fprintf(os.Stdout, "Environment:     %s\n", c.Environment)
	if c.KafkaTopicNamespaceOverride != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Topic Namespace: %s (override)\n", c.KafkaTopicNamespaceOverride)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Address:         %s\n", c.Addr)
	_, _ = fmt.Fprintf(os.Stdout, "Message Backend: %s\n", c.MessageBackend)
	_, _ = fmt.Fprintf(os.Stdout, "Kafka Brokers:   %s\n", c.KafkaBrokers)
	_, _ = fmt.Fprintf(os.Stdout, "Consumer Enabled: %v\n", c.KafkaConsumerEnabled)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Kafka Security ===")
	_, _ = fmt.Fprintf(os.Stdout, "SASL Enabled:    %v\n", c.KafkaSASLEnabled)
	if c.KafkaSASLEnabled {
		_, _ = fmt.Fprintf(os.Stdout, "SASL Mechanism:  %s\n", c.KafkaSASLMechanism)
		_, _ = fmt.Fprintf(os.Stdout, "SASL Username:   %s\n", c.KafkaSASLUsername)
		_, _ = fmt.Fprintf(os.Stdout, "SASL Password:   %s\n", "****")
	}
	_, _ = fmt.Fprintf(os.Stdout, "TLS Enabled:     %v\n", c.KafkaTLSEnabled)
	if c.KafkaTLSEnabled {
		_, _ = fmt.Fprintf(os.Stdout, "TLS Insecure:    %v\n", c.KafkaTLSInsecure)
		_, _ = fmt.Fprintf(os.Stdout, "TLS CA Path:     %s\n", c.KafkaTLSCAPath)
	}
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Resource Limits ===")
	_, _ = fmt.Fprintf(os.Stdout, "GOMAXPROCS:      %d (container-aware, Go runtime)\n", runtime.GOMAXPROCS(0))
	_, _ = fmt.Fprintf(os.Stdout, "Memory Limit:    %d MB\n", c.MemoryLimit/(1024*1024))
	_, _ = fmt.Fprintf(os.Stdout, "Max Connections: %d\n", c.MaxConnections)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Rate Limits ===")
	_, _ = fmt.Fprintf(os.Stdout, "Kafka Messages:  %d/sec\n", c.MaxKafkaMessagesPerSec)
	_, _ = fmt.Fprintf(os.Stdout, "Broadcasts:      %d/sec\n", c.MaxBroadcastsPerSec)
	_, _ = fmt.Fprintf(os.Stdout, "Max Goroutines:  %d\n", c.MaxGoroutines)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Connection Rate Limiting (DoS Protection) ===")
	_, _ = fmt.Fprintf(os.Stdout, "Enabled:         %v\n", c.ConnectionRateLimitEnabled)
	_, _ = fmt.Fprintf(os.Stdout, "IP Burst:        %d connections\n", c.ConnRateLimitIPBurst)
	_, _ = fmt.Fprintf(os.Stdout, "IP Rate:         %.1f conn/sec\n", c.ConnRateLimitIPRate)
	_, _ = fmt.Fprintf(os.Stdout, "Global Burst:    %d connections\n", c.ConnRateLimitGlobalBurst)
	_, _ = fmt.Fprintf(os.Stdout, "Global Rate:     %.1f conn/sec\n", c.ConnRateLimitGlobalRate)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Safety Thresholds (with Hysteresis) ===")
	_, _ = fmt.Fprintf(os.Stdout, "CPU Reject (upper):    %.1f%%\n", c.CPURejectThreshold)
	_, _ = fmt.Fprintf(os.Stdout, "CPU Reject (lower):    %.1f%%\n", c.CPURejectThresholdLower)
	_, _ = fmt.Fprintf(os.Stdout, "CPU Reject Band:       %.1f%%\n", c.CPURejectThreshold-c.CPURejectThresholdLower)
	_, _ = fmt.Fprintf(os.Stdout, "CPU Pause (upper):     %.1f%%\n", c.CPUPauseThreshold)
	_, _ = fmt.Fprintf(os.Stdout, "CPU Pause (lower):     %.1f%%\n", c.CPUPauseThresholdLower)
	_, _ = fmt.Fprintf(os.Stdout, "CPU Pause Band:        %.1f%%\n", c.CPUPauseThreshold-c.CPUPauseThresholdLower)
	_, _ = fmt.Fprintf(os.Stdout, "CPU EWMA Beta:         %.2f (~%.0f sample window)\n", c.CPUEWMABeta, 1/(1-c.CPUEWMABeta))
	_, _ = fmt.Fprintln(os.Stdout, "\n=== TCP/Network Tuning ===")
	_, _ = fmt.Fprintf(os.Stdout, "Listen Backlog:  %d\n", c.TCPListenBacklog)
	_, _ = fmt.Fprintf(os.Stdout, "Read Timeout:    %s\n", c.HTTPReadTimeout)
	_, _ = fmt.Fprintf(os.Stdout, "Write Timeout:   %s\n", c.HTTPWriteTimeout)
	_, _ = fmt.Fprintf(os.Stdout, "Idle Timeout:    %s\n", c.HTTPIdleTimeout)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Logging ===")
	_, _ = fmt.Fprintf(os.Stdout, "Level:           %s\n", c.LogLevel)
	_, _ = fmt.Fprintf(os.Stdout, "Format:          %s\n", c.LogFormat)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Authentication ===")
	_, _ = fmt.Fprintln(os.Stdout, "Auth:            Handled by ws-gateway")
	_, _ = fmt.Fprintf(os.Stdout, "Default Tenant:  %s\n", c.DefaultTenantID)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Broadcast Bus ===")
	_, _ = fmt.Fprintf(os.Stdout, "Type:            %s\n", c.BroadcastType)
	if c.BroadcastType == "valkey" {
		_, _ = fmt.Fprintf(os.Stdout, "Valkey Addrs:    %v\n", c.ValkeyAddrs)
		_, _ = fmt.Fprintf(os.Stdout, "Master Name:     %s\n", c.ValkeyMasterName)
		_, _ = fmt.Fprintf(os.Stdout, "Channel:         %s\n", c.ValkeyChannel)
		_, _ = fmt.Fprintf(os.Stdout, "Database:        %d\n", c.ValkeyDB)
	}
	if c.BroadcastType == "nats" {
		_, _ = fmt.Fprintf(os.Stdout, "NATS URLs:       %v\n", c.NATSURLs)
		_, _ = fmt.Fprintf(os.Stdout, "Cluster Mode:    %v\n", c.NATSClusterMode)
		_, _ = fmt.Fprintf(os.Stdout, "Subject:         %s\n", c.NATSSubject)
		_, _ = fmt.Fprintf(os.Stdout, "Token Auth:      %v\n", c.NATSToken != "")
		_, _ = fmt.Fprintf(os.Stdout, "User Auth:       %v\n", c.NATSUser != "")
		_, _ = fmt.Fprintf(os.Stdout, "TLS Enabled:     %v\n", c.NATSTLSEnabled)
	}
	if c.BroadcastType == "valkey" && c.ValkeyTLSEnabled {
		_, _ = fmt.Fprintf(os.Stdout, "Valkey TLS:      %v\n", c.ValkeyTLSEnabled)
	}
	if c.MessageBackend == "nats" {
		_, _ = fmt.Fprintln(os.Stdout, "\n=== NATS JetStream ===")
		_, _ = fmt.Fprintf(os.Stdout, "JetStream URLs:  %s\n", c.NATSJetStreamURLs)
		_, _ = fmt.Fprintf(os.Stdout, "Replicas:        %d\n", c.NATSJetStreamReplicas)
		_, _ = fmt.Fprintf(os.Stdout, "Max Age:         %s\n", c.NATSJetStreamMaxAge)
		_, _ = fmt.Fprintf(os.Stdout, "TLS Enabled:     %v\n", c.NATSJetStreamTLSEnabled)
		_, _ = fmt.Fprintf(os.Stdout, "Token Auth:      %v\n", c.NATSJetStreamToken != "")
		_, _ = fmt.Fprintf(os.Stdout, "User Auth:       %v\n", c.NATSJetStreamUser != "")
	}
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Client Buffers ===")
	_, _ = fmt.Fprintf(os.Stdout, "Send Buffer:     %d slots (~%dKB/client)\n", c.ClientSendBufferSize, c.ClientSendBufferSize/2)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Slow Client Detection ===")
	_, _ = fmt.Fprintf(os.Stdout, "Max Attempts:    %d\n", c.SlowClientMaxAttempts)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Multi-Tenant Consumer ===")
	_, _ = fmt.Fprintf(os.Stdout, "Provisioning gRPC:   %s\n", c.ProvisioningGRPCAddr)
	_, _ = fmt.Fprintf(os.Stdout, "Reconnect Delay:     %s\n", c.GRPCReconnectDelay)
	_, _ = fmt.Fprintf(os.Stdout, "Reconnect Max Delay: %s\n", c.GRPCReconnectMaxDelay)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== WebSocket Ping/Pong ===")
	_, _ = fmt.Fprintf(os.Stdout, "Pong Wait:           %s\n", c.PongWait)
	_, _ = fmt.Fprintf(os.Stdout, "Ping Period:         %s\n", c.PingPeriod)
	_, _ = fmt.Fprintf(os.Stdout, "Write Wait:          %s\n", c.WriteWait)
	_, _ = fmt.Fprintf(os.Stdout, "Buffer:              %s\n", c.PongWait-c.PingPeriod)
	_, _ = fmt.Fprintln(os.Stdout, "\n=== Monitoring ===")
	_, _ = fmt.Fprintf(os.Stdout, "Metrics Interval: %s\n", c.MetricsInterval)
	_, _ = fmt.Fprintf(os.Stdout, "CPU Poll Interval: %s\n", c.CPUPollInterval)
	_, _ = fmt.Fprintln(os.Stdout, "============================")
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
		Int("max_kafka_rate", c.MaxKafkaMessagesPerSec).
		Int("max_broadcast_rate", c.MaxBroadcastsPerSec).
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
