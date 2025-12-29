package orchestration

import (
	"testing"

	"github.com/Toniq-Labs/odin-ws/internal/broadcast"
)

// =============================================================================
// Shard Configuration Tests
// =============================================================================

func TestShard_GetMaxConnections(t *testing.T) {
	tests := []int{1, 10, 100, 1000, 10000}

	for _, max := range tests {
		shard := &Shard{maxConnections: max}
		if got := shard.GetMaxConnections(); got != max {
			t.Errorf("GetMaxConnections() = %d, want %d", got, max)
		}
	}
}

// =============================================================================
// Broadcast Message Tests
// =============================================================================

func TestBroadcastMessage_Fields(t *testing.T) {
	msg := &broadcast.Message{
		Subject: "odin.token.BTC.trade",
		Payload: []byte(`{"price":"100.50"}`),
	}

	if msg.Subject != "odin.token.BTC.trade" {
		t.Errorf("Subject: got %s, want odin.token.BTC.trade", msg.Subject)
	}
	if string(msg.Payload) != `{"price":"100.50"}` {
		t.Errorf("Payload: got %s, want {\"price\":\"100.50\"}", msg.Payload)
	}
}
