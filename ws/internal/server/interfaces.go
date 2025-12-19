// Package shared provides core WebSocket server functionality.
// This file defines interfaces for dependency injection and testability.
package server

import (
	"context"
	"net"
	"time"

	"github.com/adred-codev/odin-ws/internal/types"
)

// =============================================================================
// Logging Interfaces
// =============================================================================

// Logger abstracts structured logging for testability.
// Compatible with zerolog.Logger interface.
type Logger interface {
	Debug() LogEvent
	Info() LogEvent
	Warn() LogEvent
	Error() LogEvent
	Printf(format string, v ...any)
}

// LogEvent represents a log event builder.
// Compatible with zerolog.Event interface.
type LogEvent interface {
	Err(err error) LogEvent
	Int64(key string, val int64) LogEvent
	Int(key string, val int) LogEvent
	Str(key string, val string) LogEvent
	Interface(key string, val any) LogEvent
	Msg(msg string)
}

// =============================================================================
// Rate Limiting Interface
// =============================================================================

// RateLimiter abstracts rate limiting for testability.
// Used to prevent clients from flooding the server with messages.
type RateLimiter interface {
	// CheckLimit returns true if the client is within rate limits.
	// Returns false if the client should be rate limited.
	CheckLimit(clientID int64) bool
}

// =============================================================================
// Audit Logging Interface
// =============================================================================

// AuditLogger abstracts audit logging for testability.
// Used for security-relevant events like rate limiting, authentication, etc.
type AuditLogger interface {
	Warning(event, message string, metadata map[string]any)
	Info(event, message string, metadata map[string]any)
	Critical(event, message string, metadata map[string]any)
}

// =============================================================================
// Metrics Interface
// =============================================================================

// MetricsRecorder abstracts metrics collection for testability.
// Used to record message counts, bytes, rate limiting, and disconnects.
type MetricsRecorder interface {
	// RecordMessageSent records sent message metrics
	RecordMessageSent(count, bytes int64)
	// RecordMessageReceived records received message metrics
	RecordMessageReceived(count, bytes int64)
	// IncrementRateLimited increments the rate limited message counter
	IncrementRateLimited()
	// RecordDisconnect records a client disconnection with reason and duration
	RecordDisconnect(reason, initiatedBy string, duration time.Duration)
}

// =============================================================================
// Time Interface
// =============================================================================

// Clock abstracts time operations for testability.
// Allows tests to control time advancement.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
	After(d time.Duration) <-chan time.Time
}

// Ticker abstracts time.Ticker for testability.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// =============================================================================
// Connection Interface
// =============================================================================

// WSConn abstracts WebSocket connection operations for testability.
// Extends net.Conn with WebSocket-specific deadline operations.
type WSConn interface {
	net.Conn
}

// =============================================================================
// Message Handling Interfaces
// =============================================================================

// MessageHandler abstracts client message processing.
// Called when a client sends a message to the server.
type MessageHandler interface {
	Handle(ctx context.Context, client *Client, data []byte) error
}

// DisconnectHandler abstracts client disconnection processing.
// Called when a client disconnects or needs to be disconnected.
type DisconnectHandler interface {
	Disconnect(client *Client, reason, initiatedBy string)
}

// =============================================================================
// Network Interfaces
// =============================================================================

// ListenerFactory abstracts network listener creation for testability.
// Allows tests to provide mock listeners.
type ListenerFactory interface {
	Listen(network, addr string) (net.Listener, error)
}

// =============================================================================
// Kafka Interfaces
// =============================================================================

// KafkaConsumerFactory abstracts Kafka consumer creation for testability.
type KafkaConsumerFactory interface {
	NewConsumer(cfg any) (KafkaConsumer, error)
}

// KafkaConsumer abstracts Kafka consumer operations.
type KafkaConsumer interface {
	Start() error
	Stop() error
}

// =============================================================================
// Dependency Containers
// =============================================================================

// PumpDependencies holds injectable dependencies for read/write pumps.
// Used to create testable pump instances.
type PumpDependencies struct {
	Logger            Logger
	RateLimiter       RateLimiter
	AuditLogger       AuditLogger
	Metrics           MetricsRecorder
	MessageHandler    MessageHandler
	DisconnectHandler DisconnectHandler
	Clock             Clock
	Stats             *types.Stats
}

// ServerDependencies holds optional injectable dependencies for Server.
// nil values use production defaults.
type ServerDependencies struct {
	Logger               Logger               // nil = use default zerolog
	ListenerFactory      ListenerFactory      // nil = use net.Listen
	KafkaConsumerFactory KafkaConsumerFactory // nil = use real Kafka
	Clock                Clock                // nil = use real time
	TestMode             bool                 // Skip background goroutines in tests
}
