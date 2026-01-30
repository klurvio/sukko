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
	t.Parallel()

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
	t.Parallel()

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
