package orchestration

import (
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// =============================================================================
// SlotAwareProxy Creation Tests
// =============================================================================

func TestSlotAwareProxy_NewSlotAwareProxy(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewSlotAwareProxy(shard, backendURL, logger)

	if proxy == nil {
		t.Fatal("NewSlotAwareProxy should return non-nil proxy")
	}
	if proxy.shard != shard {
		t.Error("Proxy should reference the provided shard")
	}
	if proxy.backendURL != backendURL {
		t.Error("Proxy should reference the provided backendURL")
	}
}

func TestSlotAwareProxy_DefaultTimeouts(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewSlotAwareProxy(shard, backendURL, logger)

	// Check default timeouts
	if proxy.dialTimeout != 10*time.Second {
		t.Errorf("dialTimeout: got %v, want 10s", proxy.dialTimeout)
	}
	if proxy.messageTimeout != 60*time.Second {
		t.Errorf("messageTimeout: got %v, want 60s", proxy.messageTimeout)
	}
}

func TestSlotAwareProxy_UpgraderConfig(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewSlotAwareProxy(shard, backendURL, logger)

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

func TestSlotAwareProxy_DialerConfig(t *testing.T) {
	shard := &Shard{ID: 1}
	backendURL, _ := url.Parse("ws://localhost:3001/ws")
	logger := zerolog.Nop()

	proxy := NewSlotAwareProxy(shard, backendURL, logger)

	// Check dialer handshake timeout
	if proxy.dialer.HandshakeTimeout != 10*time.Second {
		t.Errorf("HandshakeTimeout: got %v, want 10s", proxy.dialer.HandshakeTimeout)
	}
}

// =============================================================================
// Slot Lifecycle Pattern Tests
// =============================================================================

// TestSlotReleasePattern tests the idempotent slot release pattern used in proxy
func TestSlotReleasePattern_IdempotentRelease(t *testing.T) {
	// Simulates the slot release pattern from proxy.go
	slotReleased := false
	slotReleaseMutex := sync.Mutex{}
	releaseCount := 0

	releaseSlot := func() {
		slotReleaseMutex.Lock()
		defer slotReleaseMutex.Unlock()
		if !slotReleased {
			releaseCount++
			slotReleased = true
		}
	}

	// Call release multiple times (simulating defer + explicit call)
	releaseSlot()
	releaseSlot()
	releaseSlot()

	// Should only release once
	if releaseCount != 1 {
		t.Errorf("Slot should only be released once, got %d releases", releaseCount)
	}
}

func TestSlotReleasePattern_ConcurrentRelease(t *testing.T) {
	// Tests concurrent release attempts don't cause issues
	slotReleased := false
	slotReleaseMutex := sync.Mutex{}
	var releaseCount atomic.Int32

	releaseSlot := func() {
		slotReleaseMutex.Lock()
		defer slotReleaseMutex.Unlock()
		if !slotReleased {
			releaseCount.Add(1)
			slotReleased = true
		}
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			releaseSlot()
		}()
	}

	wg.Wait()

	if releaseCount.Load() != 1 {
		t.Errorf("Concurrent releases should result in single release, got %d", releaseCount.Load())
	}
}

// =============================================================================
// Slot Lifecycle Integration Tests
// =============================================================================

func TestSlotAwareProxy_SlotAcquireReleaseFlow(t *testing.T) {
	// Create a shard with slots
	slots := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		slots <- struct{}{}
	}
	shard := &Shard{
		ID:             1,
		slots:          slots,
		maxConnections: 10,
	}

	// Verify initial state
	if shard.GetAvailableSlots() != 10 {
		t.Fatalf("Initial slots should be 10, got %d", shard.GetAvailableSlots())
	}

	// Simulate acquiring a slot (like ServeHTTP does)
	if !shard.TryAcquireSlot() {
		t.Fatal("Should acquire slot")
	}

	// Verify slot was acquired
	if shard.GetAvailableSlots() != 9 {
		t.Errorf("After acquire, slots should be 9, got %d", shard.GetAvailableSlots())
	}

	// Simulate the release pattern from proxy
	slotReleased := false
	slotReleaseMutex := sync.Mutex{}
	releaseSlot := func() {
		slotReleaseMutex.Lock()
		defer slotReleaseMutex.Unlock()
		if !slotReleased {
			shard.ReleaseSlot()
			slotReleased = true
		}
	}

	// First release (simulating normal path)
	releaseSlot()

	// Second release (simulating defer path - should be idempotent)
	releaseSlot()

	// Verify slot was released only once
	if shard.GetAvailableSlots() != 10 {
		t.Errorf("After release, slots should be 10, got %d", shard.GetAvailableSlots())
	}
}

func TestSlotAwareProxy_NoSlotLeak_MultipleAcquireRelease(t *testing.T) {
	// Create a shard with limited slots
	const maxSlots = 5
	slots := make(chan struct{}, maxSlots)
	for i := 0; i < maxSlots; i++ {
		slots <- struct{}{}
	}
	shard := &Shard{
		ID:             1,
		slots:          slots,
		maxConnections: maxSlots,
	}

	// Run 100 acquire/release cycles
	const cycles = 100
	for i := 0; i < cycles; i++ {
		// Acquire
		if !shard.TryAcquireSlot() {
			t.Fatalf("Cycle %d: Should acquire slot", i)
		}

		// Simulate connection handling
		// ... work done here ...

		// Release
		shard.ReleaseSlot()
	}

	// Verify no slot leak
	if shard.GetAvailableSlots() != maxSlots {
		t.Errorf("After %d cycles, should have %d slots, got %d",
			cycles, maxSlots, shard.GetAvailableSlots())
	}
}

func TestSlotAwareProxy_ConcurrentConnections(t *testing.T) {
	// Create a shard with slots
	const maxSlots = 100
	slots := make(chan struct{}, maxSlots)
	for i := 0; i < maxSlots; i++ {
		slots <- struct{}{}
	}
	shard := &Shard{
		ID:             1,
		slots:          slots,
		maxConnections: maxSlots,
	}

	const numGoroutines = 50
	const opsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				// Acquire
				if !shard.TryAcquireSlot() {
					continue // Slot not available
				}

				// Simulate connection duration
				time.Sleep(time.Microsecond)

				// Release with idempotent pattern
				slotReleased := false
				slotReleaseMutex := sync.Mutex{}
				releaseSlot := func() {
					slotReleaseMutex.Lock()
					defer slotReleaseMutex.Unlock()
					if !slotReleased {
						shard.ReleaseSlot()
						slotReleased = true
					}
				}
				releaseSlot()
			}
		}()
	}

	wg.Wait()

	// All slots should be returned
	if shard.GetAvailableSlots() != maxSlots {
		t.Errorf("After concurrent operations, should have %d slots, got %d",
			maxSlots, shard.GetAvailableSlots())
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestSlotAwareProxy_SlotExhaustion(t *testing.T) {
	// Create a shard with 2 slots
	slots := make(chan struct{}, 2)
	slots <- struct{}{}
	slots <- struct{}{}
	shard := &Shard{
		ID:             1,
		slots:          slots,
		maxConnections: 2,
	}

	// Acquire both slots
	if !shard.TryAcquireSlot() {
		t.Fatal("Should acquire first slot")
	}
	if !shard.TryAcquireSlot() {
		t.Fatal("Should acquire second slot")
	}

	// Third acquire should fail
	if shard.TryAcquireSlot() {
		t.Error("Should fail to acquire when slots exhausted")
	}

	// Available slots should be 0
	if shard.GetAvailableSlots() != 0 {
		t.Errorf("Should have 0 available slots, got %d", shard.GetAvailableSlots())
	}

	// Release one
	shard.ReleaseSlot()

	// Now acquire should work
	if !shard.TryAcquireSlot() {
		t.Error("Should acquire slot after release")
	}
}
