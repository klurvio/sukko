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

	proxy := NewShardProxy(shard, backendURL, logger, 10*time.Second, 60*time.Second)

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

func TestShardProxy_ExplicitTimeouts(t *testing.T) {
	t.Parallel()

	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewShardProxy(shard, backendURL, logger, 15*time.Second, 90*time.Second)

	// Check explicit timeouts
	if proxy.dialTimeout != 15*time.Second {
		t.Errorf("dialTimeout: got %v, want 15s", proxy.dialTimeout)
	}
	if proxy.messageTimeout != 90*time.Second {
		t.Errorf("messageTimeout: got %v, want 90s", proxy.messageTimeout)
	}
}
