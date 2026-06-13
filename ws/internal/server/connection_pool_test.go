package server

import (
	"testing"
)

// TestConnectionPool_GapChannelResetOnReuse verifies that a GapNotification enqueued
// during a client's session is NOT visible to the next client that reuses the same pool slot.
// gapChan is NOT nilled in Put() (to avoid a data race with Broadcast() reads); Get() always
// creates a fresh channel, so stale notifications from the previous lease cannot reach the new client.
func TestConnectionPool_GapChannelResetOnReuse(t *testing.T) {
	t.Parallel()

	pool := NewConnectionPool(10, 512, 8)

	// Get a client and enqueue a stale gap notification.
	c := pool.Get()
	if c == nil {
		t.Fatal("expected non-nil client from pool")
	}
	if c.gapChan == nil {
		t.Fatal("expected non-nil gapChan after Get()")
	}

	c.gapChan <- GapNotification{
		Channel: "test.chan",
		FromSeq: 1,
		ToSeq:   1,
		LastPos: "(1)-100",
		Ts:      123456789,
	}

	// Return to pool. replayInProgress and replayLastAt are nilled; gapChan is NOT
	// nilled (by design — Get() creates a fresh channel, avoiding the data race).
	pool.Put(c)

	if c.replayInProgress != nil {
		t.Error("Put() must nil replayInProgress")
	}
	if c.replayLastAt != nil {
		t.Error("Put() must nil replayLastAt")
	}

	// Get a new client (may be the same underlying object from sync.Pool).
	// Get() must always create a fresh, empty gapChan regardless of old content.
	c2 := pool.Get()
	if c2 == nil {
		t.Fatal("expected non-nil client from second Get()")
	}
	if c2.gapChan == nil {
		t.Fatal("expected non-nil gapChan on reused client")
	}
	if len(c2.gapChan) != 0 {
		t.Fatalf("expected empty gapChan on reused client (Get() must create a fresh channel), got len=%d", len(c2.gapChan))
	}
	if c2.replayInProgress == nil {
		t.Fatal("expected non-nil replayInProgress on reused client")
	}
	if c2.replayLastAt == nil {
		t.Fatal("expected non-nil replayLastAt on reused client")
	}
}
