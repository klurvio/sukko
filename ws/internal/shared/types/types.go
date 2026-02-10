// Package types defines core configuration structures and type definitions
// used across the WebSocket server. It provides ServerConfig for server
// settings, Stats for runtime metrics, and various constants and enums.
package types //nolint:revive // package name is intentional for shared types

import (
	"sync"
	"sync/atomic"
	"time"
)

// LogLevel represents log verbosity level
type LogLevel string

// LogLevel constants for configuring log verbosity.
const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelFatal LogLevel = "fatal"
)

// LogFormat represents log output format
type LogFormat string

// LogFormat constants for configuring log output format.
const (
	LogFormatJSON   LogFormat = "json"   // JSON format for Loki
	LogFormatPretty LogFormat = "pretty" // Human-readable for local dev
)

// ServerConfig contains the configuration for the WebSocket server
type ServerConfig struct {
	Addr                string
	KafkaBrokers        []string
	ConsumerGroup       string
	Environment         string // Environment for logging (topic naming handled by KafkaConsumerPool)
	SharedKafkaConsumer any    // Shared Kafka consumer reference (managed by KafkaConsumerPool)
	KafkaProducer       any    // Kafka producer for client message publishing (optional)
	MaxConnections      int

	// Static resource limits
	// Note: CPU limit is detected automatically via automaxprocs reading cgroup
	MemoryLimit int64 // Memory bytes available (from docker limit)

	// Rate limiting (prevent overload)
	MaxKafkaMessagesPerSec int // Max Kafka messages consumed per second
	MaxBroadcastsPerSec    int // Max broadcasts per second
	MaxGoroutines          int // Hard goroutine limit

	// Connection rate limiting (DoS protection)
	ConnectionRateLimitEnabled bool
	ConnRateLimitIPBurst       int
	ConnRateLimitIPRate        float64
	ConnRateLimitGlobalBurst   int
	ConnRateLimitGlobalRate    float64

	// Safety thresholds (emergency brakes) with hysteresis
	// Hysteresis prevents oscillation by using different thresholds for entering/exiting states
	CPURejectThreshold      float64 // Upper: reject new connections above this CPU % (default: 75)
	CPURejectThresholdLower float64 // Lower: stop rejecting below this CPU % (default: 65)
	CPUPauseThreshold       float64 // Upper: pause Kafka above this CPU % (default: 80)
	CPUPauseThresholdLower  float64 // Lower: resume Kafka below this CPU % (default: 70)

	// Client buffer configuration
	ClientSendBufferSize int // Per-client send channel buffer size (default: 512)

	// Slow client detection
	SlowClientMaxAttempts int // Consecutive send failures before disconnecting slow client (default: 3)

	// Monitoring intervals
	MetricsInterval time.Duration // Full metrics collection interval (default: 15s)
	CPUPollInterval time.Duration // CPU polling interval for protection (default: 1s)

	// TCP/Network Tuning (Trading Platform Burst Tolerance)
	TCPListenBacklog int           // TCP accept queue size (default: 2048, Go default: ~128)
	HTTPReadTimeout  time.Duration // HTTP server read timeout (default: 15s)
	HTTPWriteTimeout time.Duration // HTTP server write timeout (default: 15s)
	HTTPIdleTimeout  time.Duration // HTTP server idle timeout (default: 60s)

	// WebSocket ping/pong timing
	PongWait   time.Duration // Timeout for pong response (default: 60s)
	PingPeriod time.Duration // How often to send pings (default: 45s)
	WriteWait  time.Duration // Timeout for write operations (default: 5s)

	// Logging configuration
	LogLevel  LogLevel  // Log level (default: info)
	LogFormat LogFormat // Log format (default: json)

	// NOTE: Authentication is now handled by ws-gateway
	// ws-server is a dumb broadcaster with network-level security via NetworkPolicy
}

// Stats tracks server statistics
type Stats struct {
	TotalConnections   atomic.Int64
	CurrentConnections atomic.Int64
	MessagesSent       atomic.Int64
	MessagesReceived   atomic.Int64
	BytesSent          atomic.Int64
	BytesReceived      atomic.Int64
	StartTime          time.Time
	Mu                 sync.RWMutex
	CPUPercent         float64
	MemoryMB           float64

	// Message delivery reliability metrics
	SlowClientsDisconnected atomic.Int64 // Count of clients disconnected for being too slow
	RateLimitedMessages     atomic.Int64 // Count of messages dropped due to rate limiting
	MessageReplayRequests   atomic.Int64 // Count of replay requests served (gap recovery)

	// Phase 2 observability metrics
	DisconnectsByReason        map[string]int64 // Disconnect counts by reason (read_error, write_timeout, etc.)
	DroppedBroadcastsByChannel map[string]int64 // Dropped broadcast counts by channel
	BufferSaturationSamples    []int            // Recent buffer saturation samples (last 100)
	DisconnectsMu              sync.RWMutex     // Protects DisconnectsByReason map
	DropsMu                    sync.RWMutex     // Protects DroppedBroadcastsByChannel map
	BuffersMu                  sync.RWMutex     // Protects BufferSaturationSamples slice

	// Phase 4 logging counters
	DroppedBroadcastLogCounter atomic.Int64 // Counter for sampled logging (every 100th drop)
}
