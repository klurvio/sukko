package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerator_ResolveChannels_Static(t *testing.T) {
	cfg := &Config{
		Channels:       "odin.BTC.trade,odin.ETH.liquidity",
		KafkaNamespace: "local",
	}
	g := NewGenerator(cfg)

	channels := g.Channels()
	if len(channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(channels))
	}
	if channels[0] != "odin.BTC.trade" {
		t.Errorf("expected odin.BTC.trade, got %s", channels[0])
	}
	if channels[1] != "odin.ETH.liquidity" {
		t.Errorf("expected odin.ETH.liquidity, got %s", channels[1])
	}
}

func TestGenerator_ResolveChannels_Dynamic(t *testing.T) {
	cfg := &Config{
		TenantID:       "testcorp",
		Identifiers:    "BTC,ETH",
		Categories:     "trade,liquidity",
		KafkaNamespace: "local",
	}
	g := NewGenerator(cfg)

	channels := g.Channels()
	expected := map[string]bool{
		"testcorp.BTC.trade":     true,
		"testcorp.BTC.liquidity": true,
		"testcorp.ETH.trade":     true,
		"testcorp.ETH.liquidity": true,
	}

	if len(channels) != len(expected) {
		t.Fatalf("expected %d channels, got %d: %v", len(expected), len(channels), channels)
	}

	for _, ch := range channels {
		if !expected[ch] {
			t.Errorf("unexpected channel: %s", ch)
		}
	}
}

func TestGenerator_ResolveChannels_CustomPattern(t *testing.T) {
	cfg := &Config{
		ChannelPattern: "{category}.{tenant}.{identifier}",
		TenantID:       "odin",
		Identifiers:    "BTC",
		Categories:     "trade",
		KafkaNamespace: "local",
	}
	g := NewGenerator(cfg)

	channels := g.Channels()
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0] != "trade.odin.BTC" {
		t.Errorf("expected trade.odin.BTC, got %s", channels[0])
	}
}

func TestGenerator_ChannelToTopic(t *testing.T) {
	cfg := &Config{
		KafkaNamespace: "local",
		TenantID:       "odin",
	}
	g := NewGenerator(cfg)

	tests := []struct {
		channel       string
		expectedTopic string
	}{
		{"odin.BTC.trade", "local.odin.trade"},
		{"odin.ETH.liquidity", "local.odin.liquidity"},
		{"acme.SOL.orderbook", "local.acme.orderbook"},
		{"odin.all.trade", "local.odin.trade"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			topic, err := g.channelToTopic(tt.channel)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if topic != tt.expectedTopic {
				t.Errorf("topic = %s, want %s", topic, tt.expectedTopic)
			}
		})
	}
}

func TestGenerator_ChannelToTopic_InvalidFormat(t *testing.T) {
	cfg := &Config{KafkaNamespace: "local"}
	g := NewGenerator(cfg)

	_, err := g.channelToTopic("invalid")
	if err == nil {
		t.Error("expected error for invalid channel format")
	}
}

func TestGenerator_NextMessage(t *testing.T) {
	cfg := &Config{
		Channels:       "odin.BTC.trade",
		KafkaNamespace: "local",
		TenantID:       "odin",
	}
	g := NewGenerator(cfg)

	msg, err := g.NextMessage()
	if err != nil {
		t.Fatalf("NextMessage failed: %v", err)
	}

	// Check topic follows spec: {namespace}.{tenant}.{category}
	if msg.Topic != "local.odin.trade" {
		t.Errorf("expected topic local.odin.trade, got %s", msg.Topic)
	}

	// Check key follows spec: {tenant}.{identifier}.{category}
	if msg.Key != "odin.BTC.trade" {
		t.Errorf("expected key odin.BTC.trade, got %s", msg.Key)
	}

	// Check payload is valid JSON
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	// Check timestamp exists
	if _, ok := payload["timestamp"]; !ok {
		t.Error("expected timestamp in payload")
	}

	// Check trade-specific fields
	if _, ok := payload["token"]; !ok {
		t.Error("expected token in trade payload")
	}
	if _, ok := payload["price"]; !ok {
		t.Error("expected price in trade payload")
	}
}

func TestGenerator_NextMessage_DifferentCategories(t *testing.T) {
	categories := []struct {
		channel        string
		expectedFields []string
	}{
		{"odin.BTC.trade", []string{"token", "price", "volume", "side"}},
		{"odin.ETH.liquidity", []string{"token", "poolId", "liquidity"}},
		{"odin.SOL.orderbook", []string{"token", "bids", "asks"}},
		{"odin.user123.balances", []string{"address", "balance"}},
	}

	for _, tt := range categories {
		t.Run(tt.channel, func(t *testing.T) {
			cfg := &Config{
				Channels:       tt.channel,
				KafkaNamespace: "local",
			}
			g := NewGenerator(cfg)

			msg, err := g.NextMessage()
			if err != nil {
				t.Fatalf("NextMessage failed: %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("failed to unmarshal payload: %v", err)
			}

			for _, field := range tt.expectedFields {
				if _, ok := payload[field]; !ok {
					t.Errorf("expected field %s in payload for category", field)
				}
			}
		})
	}
}

func TestGenerator_MessageDistribution(t *testing.T) {
	cfg := &Config{
		Channels:       "ch1.id.cat,ch2.id.cat,ch3.id.cat,ch4.id.cat,ch5.id.cat",
		KafkaNamespace: "local",
	}
	g := NewGenerator(cfg)

	counts := make(map[string]int)
	total := 10000
	for range total {
		msg, err := g.NextMessage()
		if err != nil {
			t.Fatalf("NextMessage failed: %v", err)
		}
		counts[msg.Key]++
	}

	// Each channel should get roughly 20% of messages
	expectedPerChannel := total / 5
	tolerance := expectedPerChannel / 4 // 25% tolerance

	for ch, count := range counts {
		if count < expectedPerChannel-tolerance || count > expectedPerChannel+tolerance {
			t.Errorf("channel %s: count %d, expected ~%d", ch, count, expectedPerChannel)
		}
	}
}

func TestGenerator_RandomAddress(t *testing.T) {
	cfg := &Config{}
	g := NewGenerator(cfg)

	for range 100 {
		addr := g.randomAddress()
		if !strings.HasPrefix(addr, "0x") {
			t.Errorf("address should start with 0x: %s", addr)
		}
		if len(addr) != 42 {
			t.Errorf("address should be 42 chars: got %d", len(addr))
		}
	}
}
