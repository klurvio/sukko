package orchestration

import (
	"net/url"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// =============================================================================
// ShardProxy Creation Tests
// =============================================================================

func TestShardProxy_NewShardProxy(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewShardProxy(shard, backendURL, logger)

	if proxy == nil {
		t.Fatal("NewShardProxy should return non-nil proxy")
	}
	if proxy.shard != shard {
		t.Error("Proxy should reference the provided shard")
	}
	if proxy.backendURL != backendURL {
		t.Error("Proxy should reference the provided backendURL")
	}
}

func TestShardProxy_DefaultTimeouts(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewShardProxy(shard, backendURL, logger)

	// Check default timeouts
	if proxy.dialTimeout != 10*time.Second {
		t.Errorf("dialTimeout: got %v, want 10s", proxy.dialTimeout)
	}
	if proxy.messageTimeout != 60*time.Second {
		t.Errorf("messageTimeout: got %v, want 60s", proxy.messageTimeout)
	}
}

func TestShardProxy_UpgraderConfig(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewShardProxy(shard, backendURL, logger)

	// Check upgrader buffer sizes
	if proxy.upgrader.ReadBufferSize != 1024 {
		t.Errorf("ReadBufferSize: got %d, want 1024", proxy.upgrader.ReadBufferSize)
	}
	if proxy.upgrader.WriteBufferSize != 1024 {
		t.Errorf("WriteBufferSize: got %d, want 1024", proxy.upgrader.WriteBufferSize)
	}

	// Check origin is allowed
	if !proxy.upgrader.CheckOrigin(nil) {
		t.Error("CheckOrigin should allow all origins")
	}
}

func TestShardProxy_DialerConfig(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewShardProxy(shard, backendURL, logger)

	// Check dialer handshake timeout
	if proxy.dialer.HandshakeTimeout != 10*time.Second {
		t.Errorf("HandshakeTimeout: got %v, want 10s", proxy.dialer.HandshakeTimeout)
	}
}
