// Package protocol provides shared WebSocket protocol types, constants, and error codes
// used by both gateway and server components.
package protocol

import "encoding/json"

// Message type constants for client→server messages.
const (
	MsgTypeSubscribe   = "subscribe"
	MsgTypeUnsubscribe = "unsubscribe"
	MsgTypePublish     = "publish"
	MsgTypeReconnect   = "reconnect"
	MsgTypeHeartbeat   = "heartbeat"
)

// Message type constants for server→client messages.
const (
	MsgTypeMessage = "message"
	MsgTypePong    = "pong"
	MsgTypeError   = "error"
)

// Response type constants for acknowledgments.
const (
	RespTypeSubscriptionAck   = "subscription_ack"
	RespTypeUnsubscriptionAck = "unsubscription_ack"
	RespTypePublishAck        = "publish_ack"
	RespTypePublishError      = "publish_error"
	RespTypeReconnectAck      = "reconnect_ack"
	RespTypeReconnectError    = "reconnect_error"
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

// UnsubscribeData is the payload for unsubscribe messages.
type UnsubscribeData struct {
	Channels []string `json:"channels"`
}

// PublishData is the payload for publish messages.
type PublishData struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}
