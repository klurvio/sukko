// Package broadcast provides an abstraction for inter-instance message broadcasting.
// It provides Valkey-based inter-instance message broadcasting.
//
// Usage:
//
//	bus, err := broadcast.NewBus(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer bus.Shutdown()
//
//	// Subscribe before Run
//	ch, drops := bus.Subscribe(1024)
//
//	// Start receiving
//	bus.Run()
//
//	// Publish messages (fire-and-forget)
//	bus.Publish(&broadcast.Message{Subject: "BTC.trade", Payload: data})
//
//	// Unsubscribe when done
//	bus.Unsubscribe(ch)
package broadcast

import (
	"context"
	"sync/atomic"
	"time"
)

// Bus is the interface for inter-instance message broadcasting.
// Implementations must be safe for concurrent use from multiple goroutines.
//
// Lifecycle: Create with NewBus, call Subscribe for each consumer,
// call Run to start receiving, and Shutdown to stop.
type Bus interface {
	// Publish sends a message to all connected instances (fire-and-forget).
	// Errors are logged internally and tracked via metrics.
	// This method must be non-blocking and safe for concurrent use.
	Publish(msg *Message)

	// Subscribe returns a buffered channel for receiving broadcast messages and a drop counter.
	// bufSize controls the channel buffer capacity. The *atomic.Uint64 is incremented each time
	// a message is dropped for this subscriber (slow-consumer detection).
	// Each shard should call this once before Run is called.
	Subscribe(bufSize int) (<-chan *Message, *atomic.Uint64)

	// Unsubscribe removes the subscriber channel and stops delivering messages to it.
	// The channel is NOT closed — subscribers must exit via context cancellation, not channel close.
	// Returns an error if the channel is not found.
	Unsubscribe(ch <-chan *Message) error

	// Run starts the receive loop and health monitoring.
	// Must be called after all Subscribe calls. Returns immediately
	// after launching background goroutines.
	Run()

	// Shutdown gracefully stops the bus with a timeout.
	// After Shutdown returns, no more messages will be delivered
	// to subscriber channels.
	Shutdown()

	// ShutdownWithContext gracefully stops the bus using the provided context.
	// This allows for custom timeout control.
	ShutdownWithContext(ctx context.Context)

	// IsHealthy returns true if the bus connection is operational.
	// Used by health endpoints to report readiness.
	IsHealthy() bool

	// GetMetrics returns current bus metrics for monitoring.
	GetMetrics() Metrics
}

// Message represents a broadcast message sent between instances.
type Message struct {
	// Subject is the routing key (e.g., "BTC.trade")
	Subject string `json:"subject"`

	// Payload is the raw message data (typically JSON)
	Payload []byte `json:"payload"`

	// TenantID identifies the tenant that owns this message.
	// Required for history stream key construction; populated by the Kafka consumer.
	TenantID string `json:"tenant_id,omitempty"`

	// StreamID is the Kafka position "{partition}-{offset}", informational only.
	// XADD uses auto-assigned IDs (*); this field is preserved for traceability.
	StreamID string `json:"stream_id,omitempty"`

	// Channel is the bare channel name (distinct from the namespace-qualified Subject).
	// Required for per-channel stream key routing; populated by the Kafka consumer.
	Channel string `json:"channel,omitempty"`
}

// Metrics contains operational metrics for the broadcast bus.
type Metrics struct {
	// Type identifies the backend ("valkey")
	Type string `json:"type"`

	// Healthy indicates if the backend connection is operational
	Healthy bool `json:"healthy"`

	// Channel is the pub/sub channel or subject name
	Channel string `json:"channel"`

	// Subscribers is the number of local subscriber channels
	Subscribers int `json:"subscribers"`

	// PublishErrors is the count of failed publish attempts
	PublishErrors uint64 `json:"publish_errors"`

	// MessagesReceived is the count of messages received from backend
	MessagesReceived uint64 `json:"messages_received"`

	// LastPublishAgo is seconds since last successful publish (-1 if never)
	LastPublishAgo float64 `json:"last_publish_ago"`

	// LastPublishTime is the time of the last successful publish
	LastPublishTime time.Time `json:"last_publish_time,omitzero"`
}

// Config holds configuration for creating a Bus.
type Config struct {
	// Type selects the backend: "valkey"
	Type string

	// BufferSize is the subscriber channel buffer size (default: 1024)
	BufferSize int

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	ShutdownTimeout time.Duration

	// Valkey-specific configuration
	Valkey ValkeyConfig
}

// ValkeyConfig holds Valkey/Redis-specific configuration.
type ValkeyConfig struct {
	// Addrs is the list of Valkey addresses.
	// Single address: direct connection mode
	// Multiple addresses: Sentinel failover mode
	Addrs []string

	// MasterName is the Sentinel master name (default: "mymaster")
	MasterName string

	// Password for authentication
	Password string

	// DB is the database number (default: 0)
	DB int

	// Channel is the pub/sub channel name (default: "ws.broadcast")
	Channel string

	// Timeouts
	WriteTimeout time.Duration // Timeout for write operations (maps to valkey-go ConnWriteTimeout)

	// Publish
	PublishTimeout time.Duration // Timeout for publish operations

	// Startup
	StartupPingTimeout time.Duration // Timeout for initial connectivity check

	// Reconnection
	ReconnectInitialBackoff time.Duration // Initial backoff for reconnection attempts
	ReconnectMaxBackoff     time.Duration // Maximum backoff for reconnection attempts
	ReconnectMaxAttempts    int           // Maximum number of reconnection attempts

	// Health checks
	HealthCheckInterval time.Duration // Interval between health checks
	HealthCheckTimeout  time.Duration // Timeout for each health check

	// Staleness detection
	PublishStalenessThreshold time.Duration // Log warning if no publish within this window

	// TLS for managed Valkey/Redis services (ElastiCache, Memorystore, Upstash, etc.)
	TLSEnabled  bool
	TLSInsecure bool   // Skip TLS verification (not for production)
	TLSCAPath   string // Custom CA certificate path
}
