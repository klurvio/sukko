package main

import (
	"encoding/json"
	"testing"
)

func TestMessage_Structure(t *testing.T) {
	msg := &Message{
		Topic:   "local.odin.trade",
		Key:     "odin.BTC.trade",
		Payload: json.RawMessage(`{"token":"BTC","price":50000}`),
	}

	if msg.Topic != "local.odin.trade" {
		t.Errorf("unexpected topic: %s", msg.Topic)
	}
	if msg.Key != "odin.BTC.trade" {
		t.Errorf("unexpected key: %s", msg.Key)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload["token"] != "BTC" {
		t.Errorf("unexpected token: %v", payload["token"])
	}
}

func TestMessage_TopicKeyRelationship(t *testing.T) {
	// Verify the relationship between topic and key per asyncapi spec:
	// Topic: {namespace}.{tenant}.{category}
	// Key:   {tenant}.{identifier}.{category}

	tests := []struct {
		name          string
		namespace     string
		key           string
		expectedTopic string
	}{
		{
			name:          "trade channel",
			namespace:     "local",
			key:           "odin.BTC.trade",
			expectedTopic: "local.odin.trade",
		},
		{
			name:          "liquidity channel",
			namespace:     "dev",
			key:           "odin.ETH.liquidity",
			expectedTopic: "dev.odin.liquidity",
		},
		{
			name:          "aggregate channel",
			namespace:     "stag",
			key:           "odin.all.trade",
			expectedTopic: "stag.odin.trade",
		},
		{
			name:          "different tenant",
			namespace:     "prod",
			key:           "acme.SOL.orderbook",
			expectedTopic: "prod.acme.orderbook",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Channels:       tt.key,
				KafkaNamespace: tt.namespace,
			}
			g := NewGenerator(cfg)

			msg, err := g.NextMessage()
			if err != nil {
				t.Fatalf("NextMessage failed: %v", err)
			}

			if msg.Topic != tt.expectedTopic {
				t.Errorf("topic = %s, want %s", msg.Topic, tt.expectedTopic)
			}
			if msg.Key != tt.key {
				t.Errorf("key = %s, want %s", msg.Key, tt.key)
			}
		})
	}
}

// Note: Full publisher integration tests require a running Kafka instance.
// These would be run in CI with docker-compose or similar.
