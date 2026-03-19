// Package server implements the core WebSocket server with sharded connections,
// Kafka consumption, and NATS broadcast.
//
// Protocol constants and types in this file are used exclusively by the
// ws-server and MUST NOT be imported by the gateway. Shared types (used by
// both gateway and server) remain in shared/protocol. All ErrorCode constants
// (including server-originated ones) are defined in shared/protocol/errors.go.
package server

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
