// Server-only protocol constants and types.
// These are used exclusively by the ws-server and MUST NOT be imported by the gateway.
// Shared types (used by both gateway and server) remain in shared/protocol.
package server

import "github.com/Toniq-Labs/odin-ws/internal/shared/protocol"

// Message type constants for client→server messages handled only by the server.
const (
	MsgTypeReconnect = "reconnect"
	MsgTypeHeartbeat = "heartbeat"
)

// Message type constants for server→client messages produced only by the server.
const (
	MsgTypeMessage = "message"
	MsgTypePong    = "pong"
	MsgTypeError   = "error"
)

// Response type constants for server-only acknowledgments and errors.
const (
	RespTypeSubscriptionAck   = "subscription_ack"
	RespTypeUnsubscriptionAck = "unsubscription_ack"
	RespTypeReconnectAck      = "reconnect_ack"
	RespTypeReconnectError    = "reconnect_error"
	RespTypeSubscribeError    = "subscribe_error"
	RespTypeUnsubscribeError  = "unsubscribe_error"
)

// UnsubscribeData is the payload for unsubscribe messages, used only by the server.
type UnsubscribeData struct {
	Channels []string `json:"channels"`
}

// Server-only error codes.
const (
	// ErrCodeInvalidJSON indicates a client message is not valid JSON.
	ErrCodeInvalidJSON protocol.ErrorCode = "invalid_json"

	// ErrCodePublishFailed indicates Kafka publish failed.
	ErrCodePublishFailed protocol.ErrorCode = "publish_failed"

	// ErrCodeReplayFailed indicates Kafka message replay failed.
	ErrCodeReplayFailed protocol.ErrorCode = "replay_failed"
)
