package main

import "encoding/json"

// Message types (from AsyncAPI #/components/schemas/ClientMessage)
const (
	MsgTypeSubscribe   = "subscribe"
	MsgTypeUnsubscribe = "unsubscribe"
	MsgTypeHeartbeat   = "heartbeat"
)

// Response types (from AsyncAPI spec)
const (
	RespTypeSubscriptionAck   = "subscription_ack"
	RespTypeUnsubscriptionAck = "unsubscription_ack"
	RespTypeMessage           = "message"
	RespTypePong              = "pong"
	RespTypeSubscribeError    = "subscribe_error"
	RespTypeUnsubscribeError  = "unsubscribe_error"
	RespTypeError             = "error"
)

// ClientMessage is the envelope for all client-to-server messages.
// Matches AsyncAPI #/components/schemas/ClientMessage
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// SubscribeData matches AsyncAPI #/components/schemas/SubscribeData
type SubscribeData struct {
	Channels []string `json:"channels"`
}

// SubscriptionAck matches AsyncAPI #/components/schemas/SubscriptionAck
type SubscriptionAck struct {
	Type       string   `json:"type"`
	Subscribed []string `json:"subscribed"`
	Count      int      `json:"count"`
}

// UnsubscriptionAck matches AsyncAPI #/components/schemas/UnsubscriptionAck
type UnsubscriptionAck struct {
	Type         string   `json:"type"`
	Unsubscribed []string `json:"unsubscribed"`
	Count        int      `json:"count"`
}

// MessageEnvelope matches AsyncAPI #/components/schemas/MessageEnvelope
type MessageEnvelope struct {
	Type    string          `json:"type"`
	Seq     int64           `json:"seq"`
	Ts      int64           `json:"ts"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// Pong matches AsyncAPI #/components/schemas/Pong
type Pong struct {
	Type string `json:"type"`
	Ts   int64  `json:"ts"`
}

// ErrorResponse matches AsyncAPI error schemas
type ErrorResponse struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// ServerMessage is a generic server response for type detection
type ServerMessage struct {
	Type string `json:"type"`
}

// NewSubscribeMessage creates a subscribe message for the given channels
func NewSubscribeMessage(channels []string) ([]byte, error) {
	data := SubscribeData{Channels: channels}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	msg := ClientMessage{
		Type: MsgTypeSubscribe,
		Data: dataBytes,
	}
	return json.Marshal(msg)
}

// NewUnsubscribeMessage creates an unsubscribe message for the given channels
func NewUnsubscribeMessage(channels []string) ([]byte, error) {
	data := SubscribeData{Channels: channels}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	msg := ClientMessage{
		Type: MsgTypeUnsubscribe,
		Data: dataBytes,
	}
	return json.Marshal(msg)
}

// NewHeartbeatMessage creates a heartbeat message
func NewHeartbeatMessage() ([]byte, error) {
	msg := ClientMessage{
		Type: MsgTypeHeartbeat,
		Data: json.RawMessage(`{}`),
	}
	return json.Marshal(msg)
}
