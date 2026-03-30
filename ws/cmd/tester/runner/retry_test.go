package runner

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestConnectWithRetry_FailsAllAttempts(t *testing.T) {
	t.Parallel()

	// Use an unreachable address — all attempts should fail
	_, err := connectWithRetry(context.Background(), "ws://localhost:59999", "token", zerolog.Nop())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestConnectWithRetry_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := connectWithRetry(ctx, "ws://localhost:59999", "token", zerolog.Nop())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	// Should return quickly, not wait for all backoff delays
	if elapsed > 2*time.Second {
		t.Errorf("took %v, expected fast cancellation", elapsed)
	}
}
