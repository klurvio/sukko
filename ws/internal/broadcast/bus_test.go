package broadcast

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Type != "valkey" {
		t.Errorf("Type: got %s, want valkey", cfg.Type)
	}
	if cfg.BufferSize != 1024 {
		t.Errorf("BufferSize: got %d, want 1024", cfg.BufferSize)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout: got %v, want 5s", cfg.ShutdownTimeout)
	}
	if cfg.Valkey.MasterName != "mymaster" {
		t.Errorf("Valkey.MasterName: got %s, want mymaster", cfg.Valkey.MasterName)
	}
	if cfg.Valkey.Channel != "ws.broadcast" {
		t.Errorf("Valkey.Channel: got %s, want ws.broadcast", cfg.Valkey.Channel)
	}
	if cfg.NATS.Subject != "ws.broadcast" {
		t.Errorf("NATS.Subject: got %s, want ws.broadcast", cfg.NATS.Subject)
	}
	if cfg.NATS.ReconnectWait != 2*time.Second {
		t.Errorf("NATS.ReconnectWait: got %v, want 2s", cfg.NATS.ReconnectWait)
	}
	if cfg.NATS.MaxReconnects != -1 {
		t.Errorf("NATS.MaxReconnects: got %d, want -1", cfg.NATS.MaxReconnects)
	}
}

func TestMessage_Fields(t *testing.T) {
	msg := &Message{
		Subject: "BTC.trade",
		Payload: []byte(`{"price":"50000.00"}`),
	}

	if msg.Subject != "BTC.trade" {
		t.Errorf("Subject: got %s, want BTC.trade", msg.Subject)
	}
	if string(msg.Payload) != `{"price":"50000.00"}` {
		t.Errorf("Payload: got %s, want {\"price\":\"50000.00\"}", msg.Payload)
	}
}

func TestMetrics_Fields(t *testing.T) {
	now := time.Now()
	m := Metrics{
		Type:             "valkey",
		Healthy:          true,
		Channel:          "ws.broadcast",
		Subscribers:      3,
		PublishErrors:    5,
		MessagesReceived: 1000,
		LastPublishAgo:   1.5,
		LastPublishTime:  now,
	}

	if m.Type != "valkey" {
		t.Errorf("Type: got %s, want valkey", m.Type)
	}
	if !m.Healthy {
		t.Error("Healthy: got false, want true")
	}
	if m.Channel != "ws.broadcast" {
		t.Errorf("Channel: got %s, want ws.broadcast", m.Channel)
	}
	if m.Subscribers != 3 {
		t.Errorf("Subscribers: got %d, want 3", m.Subscribers)
	}
	if m.PublishErrors != 5 {
		t.Errorf("PublishErrors: got %d, want 5", m.PublishErrors)
	}
	if m.MessagesReceived != 1000 {
		t.Errorf("MessagesReceived: got %d, want 1000", m.MessagesReceived)
	}
	if m.LastPublishAgo != 1.5 {
		t.Errorf("LastPublishAgo: got %f, want 1.5", m.LastPublishAgo)
	}
	if !m.LastPublishTime.Equal(now) {
		t.Errorf("LastPublishTime: got %v, want %v", m.LastPublishTime, now)
	}
}

func TestValkeyConfig_Fields(t *testing.T) {
	cfg := ValkeyConfig{
		Addrs:      []string{"valkey-1:6379", "valkey-2:6379"},
		MasterName: "mymaster",
		Password:   "secret",
		DB:         1,
		Channel:    "custom.channel",
	}

	if len(cfg.Addrs) != 2 {
		t.Errorf("Addrs length: got %d, want 2", len(cfg.Addrs))
	}
	if cfg.MasterName != "mymaster" {
		t.Errorf("MasterName: got %s, want mymaster", cfg.MasterName)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password: got %s, want secret", cfg.Password)
	}
	if cfg.DB != 1 {
		t.Errorf("DB: got %d, want 1", cfg.DB)
	}
	if cfg.Channel != "custom.channel" {
		t.Errorf("Channel: got %s, want custom.channel", cfg.Channel)
	}
}

func TestNATSConfig_Fields(t *testing.T) {
	cfg := NATSConfig{
		URLs:          []string{"nats://nats-1:4222", "nats://nats-2:4222"},
		ClusterMode:   true,
		Subject:       "custom.subject",
		Token:         "token123",
		User:          "user",
		Password:      "pass",
		Name:          "test-client",
		ReconnectWait: 5 * time.Second,
		MaxReconnects: 10,
	}

	if len(cfg.URLs) != 2 {
		t.Errorf("URLs length: got %d, want 2", len(cfg.URLs))
	}
	if !cfg.ClusterMode {
		t.Error("ClusterMode: got false, want true")
	}
	if cfg.Subject != "custom.subject" {
		t.Errorf("Subject: got %s, want custom.subject", cfg.Subject)
	}
	if cfg.Token != "token123" {
		t.Errorf("Token: got %s, want token123", cfg.Token)
	}
	if cfg.User != "user" {
		t.Errorf("User: got %s, want user", cfg.User)
	}
	if cfg.Password != "pass" {
		t.Errorf("Password: got %s, want pass", cfg.Password)
	}
	if cfg.Name != "test-client" {
		t.Errorf("Name: got %s, want test-client", cfg.Name)
	}
	if cfg.ReconnectWait != 5*time.Second {
		t.Errorf("ReconnectWait: got %v, want 5s", cfg.ReconnectWait)
	}
	if cfg.MaxReconnects != 10 {
		t.Errorf("MaxReconnects: got %d, want 10", cfg.MaxReconnects)
	}
}
