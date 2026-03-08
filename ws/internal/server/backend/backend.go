// Package backend provides the pluggable message backend abstraction for Sukko WS.
//
// The MessageBackend interface decouples the ws-server from any specific message
// ingestion system. Three implementations are provided:
//
//   - DirectBackend: routes client-published messages directly to the broadcast bus
//     with zero external dependencies. No persistence, no replay.
//   - KafkaBackend: wraps the existing Kafka/Redpanda consumer pool and producer.
//     Full persistence, offset-based replay, multi-tenant consumer isolation.
//   - JetStreamBackend: uses NATS JetStream for persistent streams with
//     sequence-based replay. Lighter than Kafka, supports multi-tenant stream isolation.
//
// The backend is selected at startup via the MESSAGE_BACKEND environment variable.
package backend

import (
	"context"
	"errors"
)

// MessageBackend is the abstraction for message ingestion, publishing, and replay.
// Implementations: DirectBackend, KafkaBackend, JetStreamBackend.
type MessageBackend interface {
	// Start begins the backend's consumption loop (if applicable).
	// For Kafka: starts the MultiTenantConsumerPool.
	// For JetStream: starts stream consumers.
	// For Direct: no-op (returns nil immediately).
	Start(ctx context.Context) error

	// Publish sends a client-published message through the backend.
	// The backend is responsible for routing the message to reach subscribers
	// (either directly via broadcast bus or through the backend's consume loop).
	Publish(ctx context.Context, clientID int64, channel string, data []byte) error

	// Replay returns messages from the specified positions for client reconnection.
	// Returns nil, nil if replay is not supported (direct mode).
	Replay(ctx context.Context, req ReplayRequest) ([]ReplayMessage, error)

	// IsHealthy returns true if the backend is operational.
	IsHealthy() bool

	// Shutdown gracefully stops the backend.
	Shutdown(ctx context.Context) error
}

// DefaultMaxReplayMessages is the maximum number of messages replayed on reconnect.
const DefaultMaxReplayMessages = 100

// Sentinel errors for expected backend conditions.
var (
	ErrBackendUnavailable = errors.New("message backend unavailable")
	ErrReplayNotSupported = errors.New("replay not supported by this backend")
	ErrPublishFailed      = errors.New("message publish failed")
	ErrUnknownBackend     = errors.New("unknown message backend")
)

// ReplayRequest contains parameters for message replay on reconnect.
type ReplayRequest struct {
	Positions     map[string]int64 // Topic/stream → last position (offset or sequence)
	MaxMessages   int              // Maximum messages to replay
	Subscriptions []string         // Only replay messages for these channels
}

// ReplayMessage is a backend-agnostic replayed message.
type ReplayMessage struct {
	Subject string // Channel/routing key (e.g., "acme.BTC.trade")
	Data    []byte // Raw message payload
}
