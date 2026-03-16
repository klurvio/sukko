package main

import (
	"encoding/json"
	"testing"
)

func TestNewSubscribeMessage(t *testing.T) {
	t.Parallel()

	channels := []string{"odin.BTC.trade", "odin.ETH.trade"}
	got, err := NewSubscribeMessage(channels)
	if err != nil {
		t.Fatalf("NewSubscribeMessage error: %v", err)
	}

	want := `{"type":"subscribe","data":{"channels":["odin.BTC.trade","odin.ETH.trade"]}}`
	if string(got) != want {
		t.Errorf("NewSubscribeMessage = %s, want %s", got, want)
	}
}

func TestNewUnsubscribeMessage(t *testing.T) {
	t.Parallel()

	channels := []string{"odin.BTC.trade"}
	got, err := NewUnsubscribeMessage(channels)
	if err != nil {
		t.Fatalf("NewUnsubscribeMessage error: %v", err)
	}

	want := `{"type":"unsubscribe","data":{"channels":["odin.BTC.trade"]}}`
	if string(got) != want {
		t.Errorf("NewUnsubscribeMessage = %s, want %s", got, want)
	}
}

func TestNewHeartbeatMessage(t *testing.T) {
	t.Parallel()

	got, err := NewHeartbeatMessage()
	if err != nil {
		t.Fatalf("NewHeartbeatMessage error: %v", err)
	}

	want := `{"type":"heartbeat","data":{}}`
	if string(got) != want {
		t.Errorf("NewHeartbeatMessage = %s, want %s", got, want)
	}
}

func TestSubscriptionAck_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"type":"subscription_ack","subscribed":["odin.BTC.trade"],"count":5}`

	var ack SubscriptionAck
	if err := json.Unmarshal([]byte(input), &ack); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if ack.Type != RespTypeSubscriptionAck {
		t.Errorf("Type = %s, want %s", ack.Type, RespTypeSubscriptionAck)
	}
	if len(ack.Subscribed) != 1 || ack.Subscribed[0] != "odin.BTC.trade" {
		t.Errorf("Subscribed = %v, want [odin.BTC.trade]", ack.Subscribed)
	}
	if ack.Count != 5 {
		t.Errorf("Count = %d, want 5", ack.Count)
	}
}

func TestUnsubscriptionAck_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"type":"unsubscription_ack","unsubscribed":["odin.BTC.trade"],"count":4}`

	var ack UnsubscriptionAck
	if err := json.Unmarshal([]byte(input), &ack); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if ack.Type != RespTypeUnsubscriptionAck {
		t.Errorf("Type = %s, want %s", ack.Type, RespTypeUnsubscriptionAck)
	}
	if len(ack.Unsubscribed) != 1 || ack.Unsubscribed[0] != "odin.BTC.trade" {
		t.Errorf("Unsubscribed = %v, want [odin.BTC.trade]", ack.Unsubscribed)
	}
	if ack.Count != 4 {
		t.Errorf("Count = %d, want 4", ack.Count)
	}
}

func TestMessageEnvelope_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"type":"message","seq":1234,"ts":1706000000000,"channel":"odin.BTC.trade","data":{"price":50000}}`

	var env MessageEnvelope
	if err := json.Unmarshal([]byte(input), &env); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if env.Type != RespTypeMessage {
		t.Errorf("Type = %s, want %s", env.Type, RespTypeMessage)
	}
	if env.Seq != 1234 {
		t.Errorf("Seq = %d, want 1234", env.Seq)
	}
	if env.Ts != 1706000000000 {
		t.Errorf("Ts = %d, want 1706000000000", env.Ts)
	}
	if env.Channel != "odin.BTC.trade" {
		t.Errorf("Channel = %s, want odin.BTC.trade", env.Channel)
	}
}

func TestPong_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"type":"pong","ts":1706000000000}`

	var pong Pong
	if err := json.Unmarshal([]byte(input), &pong); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if pong.Type != RespTypePong {
		t.Errorf("Type = %s, want %s", pong.Type, RespTypePong)
	}
	if pong.Ts != 1706000000000 {
		t.Errorf("Ts = %d, want 1706000000000", pong.Ts)
	}
}

func TestErrorResponse_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"type":"subscribe_error","code":"invalid_channel","message":"Channel not found"}`

	var errResp ErrorResponse
	if err := json.Unmarshal([]byte(input), &errResp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if errResp.Type != RespTypeSubscribeError {
		t.Errorf("Type = %s, want %s", errResp.Type, RespTypeSubscribeError)
	}
	if errResp.Code != "invalid_channel" {
		t.Errorf("Code = %s, want invalid_channel", errResp.Code)
	}
	if errResp.Message != "Channel not found" {
		t.Errorf("Message = %s, want Channel not found", errResp.Message)
	}
}

func TestServerMessage_TypeDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantType string
	}{
		{
			name:     "subscription_ack",
			input:    `{"type":"subscription_ack","subscribed":[],"count":0}`,
			wantType: RespTypeSubscriptionAck,
		},
		{
			name:     "message",
			input:    `{"type":"message","seq":1,"ts":1,"channel":"test","data":{}}`,
			wantType: RespTypeMessage,
		},
		{
			name:     "pong",
			input:    `{"type":"pong","ts":1}`,
			wantType: RespTypePong,
		},
		{
			name:     "error",
			input:    `{"type":"error","message":"test"}`,
			wantType: RespTypeError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var msg ServerMessage
			if err := json.Unmarshal([]byte(tt.input), &msg); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if msg.Type != tt.wantType {
				t.Errorf("Type = %s, want %s", msg.Type, tt.wantType)
			}
		})
	}
}
