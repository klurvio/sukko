// Package broadcast provides an abstraction for inter-instance message broadcasting.
// It allows switching between different backends (Valkey, NATS) without code changes.
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
//	ch := bus.Subscribe()
//
//	// Start receiving
//	bus.Run()
//
//	// Publish messages (fire-and-forget)
//	bus.Publish(&broadcast.Message{Subject: "BTC.trade", Payload: data})
package broadcast

import (
	"context"
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

	// Subscribe returns a channel for receiving broadcast messages.
	// Each shard should call this once before Run is called.
	// The returned channel is buffered and will drop messages if full.
	Subscribe() <-chan *Message

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
}

// Metrics contains operational metrics for the broadcast bus.
type Metrics struct {
	// Type identifies the backend ("valkey" or "nats")
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
// Backend-specific fields are only used when the corresponding Type is selected.
type Config struct {
	// Type selects the backend: "valkey" or "nats"
	Type string

	// BufferSize is the subscriber channel buffer size (default: 1024)
	BufferSize int

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	ShutdownTimeout time.Duration

	// Valkey-specific configuration
	Valkey ValkeyConfig

	// NATS-specific configuration
	NATS NATSConfig
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
}

// NATSConfig holds NATS-specific configuration.
type NATSConfig struct {
	// URLs is the list of NATS server URLs (e.g., ["nats://localhost:4222"])
	URLs []string

	// ClusterMode enables connection to multiple NATS servers for HA
	ClusterMode bool

	// Subject is the NATS subject for broadcasting (default: "ws.broadcast")
	Subject string

	// Token for token-based authentication (preferred)
	Token string

	// User for user/password authentication
	User string

	// Password for user/password authentication
	Password string

	// Name is the client name for NATS connections (for debugging)
	Name string

	// ReconnectWait is the wait time between reconnect attempts
	ReconnectWait time.Duration

	// MaxReconnects is the maximum number of reconnect attempts (-1 for unlimited)
	MaxReconnects int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Type:            "valkey",
		BufferSize:      1024,
		ShutdownTimeout: 5 * time.Second,
		Valkey: ValkeyConfig{
			MasterName: "mymaster",
			Channel:    "ws.broadcast",
		},
		NATS: NATSConfig{
			Subject:       "ws.broadcast",
			ReconnectWait: 2 * time.Second,
			MaxReconnects: -1, // Unlimited
		},
	}
}
