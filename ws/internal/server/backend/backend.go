// Package backend provides the pluggable message backend abstraction for Sukko WS.
//
// The MessageBackend interface decouples the ws-server from any specific message
// ingestion system. Two implementations are provided:
//
//   - DirectBackend: routes client-published messages directly to the broadcast bus
//     with zero external dependencies. No persistence, no replay.
//   - KafkaBackend: wraps the existing Kafka/Redpanda consumer pool and producer.
//     Full persistence, offset-based replay, multi-tenant consumer isolation.
//
// The backend is selected at startup via the MESSAGE_BACKEND environment variable.
package backend

import (
	"context"
	"errors"
	"fmt"
)

// MessageBackend is the abstraction for message ingestion, publishing, and replay.
// Implementations: DirectBackend, KafkaBackend.
type MessageBackend interface {
	// Start begins the backend's consumption loop (if applicable).
	// For Kafka: starts the MultiTenantConsumerPool.
	// For Direct: no-op (returns nil immediately).
	Start(ctx context.Context) error

	// Publish sends a client-published message through the backend.
	// tenantID identifies the tenant that owns the channel (extracted from the channel prefix).
	// The backend is responsible for routing the message to reach subscribers
	// (either directly via broadcast bus or through the backend's consume loop).
	Publish(ctx context.Context, clientID int64, tenantID string, channel string, data []byte) error

	// Replay returns messages from the specified positions for client reconnection.
	// Returns nil, nil if replay is not supported (direct mode).
	Replay(ctx context.Context, req ReplayRequest) ([]ReplayMessage, error)

	// IsHealthy returns true if the backend is operational (liveness).
	IsHealthy() bool

	// Ready reports whether the backend is ready to serve traffic (readiness). In kafka mode this is
	// false until the topic-registry routing snapshot has been applied, so ws-server does not consume
	// or broadcast before it can resolve topic->tenant — closing the startup cross-tenant window (#179
	// P3). Direct mode is always ready. Unlike IsHealthy, a false result MUST NOT crash-loop the pod:
	// it gates the /ready probe only, never /health.
	Ready() bool

	// Shutdown gracefully stops the backend.
	Shutdown(ctx context.Context) error

	// ChannelTopic returns the Kafka topic name for a given channel name.
	// Returns ok=false for backends that don't have a topic mapping (Direct)
	// or when the channel has no registered consumer.
	ChannelTopic(channel string) (topic string, ok bool)
}

// Sentinel errors for expected backend conditions.
var (
	ErrBackendUnavailable = errors.New("message backend unavailable")
	ErrReplayNotSupported = errors.New("replay not supported by this backend")
	ErrPublishFailed      = errors.New("message publish failed")
	ErrUnknownBackend     = errors.New("unknown message backend")

	// ErrPublishNotRoutable indicates a publish was rejected because the tenant has no
	// applicable routing rule (no rules provisioned, or channel-topic routing unavailable).
	// It is an expected, client-caused condition — callers MUST map it to a 4xx/reject
	// (FailedPrecondition/409), NOT an infrastructure error, and MUST NOT let it trip
	// producer circuit breakers.
	ErrPublishNotRoutable = errors.New("publish not routable: no applicable routing rule")

	// ErrNoMatchingRoute is the more-specific not-routable case: the tenant HAS routing
	// rules provisioned, but none match this channel (a typo'd or unprovisioned channel
	// suffix). It wraps ErrPublishNotRoutable so errors.Is(err, ErrPublishNotRoutable)
	// catches both cases for the shared gRPC/metric/log reject-class handling; callers that
	// need the distinction (the WS handler emits protocol.ErrCodeNoMatchingRoute vs
	// ErrCodeNoRoutingRules) MUST check ErrNoMatchingRoute FIRST. Per the ratified
	// feat/topic-routing spec (FR-014), no-match is a reject — never a silent dead-letter.
	ErrNoMatchingRoute = fmt.Errorf("%w: no rule matches this channel", ErrPublishNotRoutable)
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
	Pos     string // Encoded Kafka position "(partition+1)-offset"; empty for backends without pos support
}
