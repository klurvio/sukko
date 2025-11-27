package multi

import (
	"sync"
	"testing"
)

// =============================================================================
// Slot Management Tests
// =============================================================================

// newTestShardSlots creates a minimal shard for slot testing only.
// It only initializes the slot semaphore, not the full server.
func newTestShardSlots(maxConns int) *Shard {
	slots := make(chan struct{}, maxConns)
	// Pre-fill with available slots
	for i := 0; i < maxConns; i++ {
		slots <- struct{}{}
	}
	return &Shard{
		slots:          slots,
		maxConnections: maxConns,
	}
}

func TestShard_TryAcquireSlot_Success(t *testing.T) {
	shard := newTestShardSlots(10)

	// Should succeed when slots available
	if !shard.TryAcquireSlot() {
		t.Error("TryAcquireSlot should succeed when slots available")
	}

	// Verify available slots decreased
	if shard.GetAvailableSlots() != 9 {
		t.Errorf("Available slots should be 9, got %d", shard.GetAvailableSlots())
	}
}

func TestShard_TryAcquireSlot_AtCapacity(t *testing.T) {
	shard := newTestShardSlots(3)

	// Acquire all slots
	for i := 0; i < 3; i++ {
		if !shard.TryAcquireSlot() {
			t.Errorf("Acquire %d should succeed", i+1)
		}
	}

	// Verify no slots available
	if shard.GetAvailableSlots() != 0 {
		t.Errorf("Available slots should be 0, got %d", shard.GetAvailableSlots())
	}

	// Next acquire should fail
	if shard.TryAcquireSlot() {
		t.Error("TryAcquireSlot should fail when at capacity")
	}
}

func TestShard_ReleaseSlot(t *testing.T) {
	shard := newTestShardSlots(5)

	// Acquire a slot
	shard.TryAcquireSlot()
	if shard.GetAvailableSlots() != 4 {
		t.Errorf("Should have 4 available after acquire, got %d", shard.GetAvailableSlots())
	}

	// Release the slot
	shard.ReleaseSlot()
	if shard.GetAvailableSlots() != 5 {
		t.Errorf("Should have 5 available after release, got %d", shard.GetAvailableSlots())
	}
}

func TestShard_GetAvailableSlots(t *testing.T) {
	tests := []struct {
		name       string
		maxConns   int
		acquireN   int
		expectedN  int
	}{
		{"all available", 10, 0, 10},
		{"half used", 10, 5, 5},
		{"all used", 10, 10, 0},
		{"single slot", 1, 0, 1},
		{"single slot used", 1, 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shard := newTestShardSlots(tt.maxConns)

			for i := 0; i < tt.acquireN; i++ {
				shard.TryAcquireSlot()
			}

			if got := shard.GetAvailableSlots(); got != tt.expectedN {
				t.Errorf("GetAvailableSlots() = %d, want %d", got, tt.expectedN)
			}
		})
	}
}

func TestShard_SlotsConcurrent(t *testing.T) {
	shard := newTestShardSlots(100)
	const numGoroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Acquirers and releasers

	// Track successful acquires
	acquired := make(chan bool, numGoroutines*opsPerGoroutine)

	// Concurrent acquires
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				if shard.TryAcquireSlot() {
					acquired <- true
				}
			}
		}()
	}

	// Concurrent releases (release what we acquired)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				select {
				case <-acquired:
					shard.ReleaseSlot()
				default:
					// Nothing to release
				}
			}
		}()
	}

	wg.Wait()
	close(acquired)

	// Drain remaining acquired slots
	remaining := 0
	for range acquired {
		remaining++
		shard.ReleaseSlot()
	}

	// Final state should be back to max
	if got := shard.GetAvailableSlots(); got != 100 {
		t.Errorf("Final available slots should be 100, got %d", got)
	}
}

func TestShard_AcquireReleaseRoundTrip(t *testing.T) {
	shard := newTestShardSlots(5)

	// Multiple acquire/release cycles
	for cycle := 0; cycle < 100; cycle++ {
		// Acquire all
		for i := 0; i < 5; i++ {
			if !shard.TryAcquireSlot() {
				t.Fatalf("Cycle %d: acquire %d failed", cycle, i)
			}
		}

		if shard.GetAvailableSlots() != 0 {
			t.Fatalf("Cycle %d: should have 0 available", cycle)
		}

		// Release all
		for i := 0; i < 5; i++ {
			shard.ReleaseSlot()
		}

		if shard.GetAvailableSlots() != 5 {
			t.Fatalf("Cycle %d: should have 5 available after release", cycle)
		}
	}
}

func TestShard_GetMaxConnections(t *testing.T) {
	tests := []int{1, 10, 100, 1000, 10000}

	for _, max := range tests {
		shard := newTestShardSlots(max)
		if got := shard.GetMaxConnections(); got != max {
			t.Errorf("GetMaxConnections() = %d, want %d", got, max)
		}
	}
}

// =============================================================================
// BroadcastMessage Tests
// =============================================================================

func TestBroadcastMessage_Fields(t *testing.T) {
	msg := &BroadcastMessage{
		Subject: "odin.token.BTC.trade",
		Message: []byte(`{"price":"100.50"}`),
	}

	if msg.Subject != "odin.token.BTC.trade" {
		t.Errorf("Subject: got %s, want odin.token.BTC.trade", msg.Subject)
	}
	if string(msg.Message) != `{"price":"100.50"}` {
		t.Errorf("Message: got %s, want {\"price\":\"100.50\"}", msg.Message)
	}
}
