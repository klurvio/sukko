// Package protocol provides shared WebSocket protocol types, constants, and error codes
// used by both gateway and server components.
//
// Server-only types (MsgTypeReconnect, MsgTypeHeartbeat, MsgTypeMessage, MsgTypePong,
// MsgTypeError, RespTypeSubscriptionAck, RespTypeUnsubscriptionAck, RespTypeReconnectAck,
// RespTypeReconnectError, RespTypeSubscribeError, RespTypeUnsubscribeError, UnsubscribeData)
// live in ws/internal/server/protocol.go.
package protocol

import "encoding/json"

// Message type constants for client→server messages used by both gateway and server.
const (
	MsgTypeSubscribe   = "subscribe"
	MsgTypeUnsubscribe = "unsubscribe"
	MsgTypePublish     = "publish"
)

// Response type constants used by both gateway and server.
const (
	RespTypePublishAck   = "publish_ack"
	RespTypePublishError = "publish_error"
)

// ClientMessage is the standard envelope for client→server messages.
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// SubscribeData is the payload for subscribe messages.
type SubscribeData struct {
	Channels []string `json:"channels"`
}

// PublishData is the payload for publish messages.
type PublishData struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}
