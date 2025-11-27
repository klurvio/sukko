package shared

import (
	"sync"
	"testing"
)

// =============================================================================
// SubscriptionSet Tests
// =============================================================================

func TestSubscriptionSet_AddRemove(t *testing.T) {
	ss := NewSubscriptionSet()

	// Initially empty
	if ss.Count() != 0 {
		t.Errorf("New SubscriptionSet should be empty, got %d", ss.Count())
	}

	// Add channel
	ss.Add("BTC.trade")
	if !ss.Has("BTC.trade") {
		t.Error("Has(BTC.trade) should return true after Add")
	}
	if ss.Count() != 1 {
		t.Errorf("Count should be 1, got %d", ss.Count())
	}

	// Add another channel
	ss.Add("ETH.trade")
	if ss.Count() != 2 {
		t.Errorf("Count should be 2, got %d", ss.Count())
	}

	// Add duplicate (should not increase count)
	ss.Add("BTC.trade")
	if ss.Count() != 2 {
		t.Errorf("Duplicate Add should not increase count, got %d", ss.Count())
	}

	// Remove channel
	ss.Remove("BTC.trade")
	if ss.Has("BTC.trade") {
		t.Error("Has(BTC.trade) should return false after Remove")
	}
	if ss.Count() != 1 {
		t.Errorf("Count should be 1 after Remove, got %d", ss.Count())
	}

	// Remove non-existent (should not panic)
	ss.Remove("NONEXISTENT")
	if ss.Count() != 1 {
		t.Errorf("Remove non-existent should not change count, got %d", ss.Count())
	}
}

func TestSubscriptionSet_AddMultiple(t *testing.T) {
	ss := NewSubscriptionSet()
	channels := []string{"BTC.trade", "ETH.trade", "SOL.trade"}

	ss.AddMultiple(channels)

	if ss.Count() != 3 {
		t.Errorf("Count should be 3 after AddMultiple, got %d", ss.Count())
	}

	for _, ch := range channels {
		if !ss.Has(ch) {
			t.Errorf("Has(%s) should return true", ch)
		}
	}
}

func TestSubscriptionSet_RemoveMultiple(t *testing.T) {
	ss := NewSubscriptionSet()
	ss.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.trade", "DOGE.trade"})

	ss.RemoveMultiple([]string{"BTC.trade", "SOL.trade"})

	if ss.Count() != 2 {
		t.Errorf("Count should be 2 after RemoveMultiple, got %d", ss.Count())
	}
	if ss.Has("BTC.trade") || ss.Has("SOL.trade") {
		t.Error("Removed channels should not be present")
	}
	if !ss.Has("ETH.trade") || !ss.Has("DOGE.trade") {
		t.Error("Remaining channels should still be present")
	}
}

func TestSubscriptionSet_List(t *testing.T) {
	ss := NewSubscriptionSet()
	channels := []string{"BTC.trade", "ETH.trade", "SOL.trade"}
	ss.AddMultiple(channels)

	list := ss.List()
	if len(list) != 3 {
		t.Errorf("List should have 3 items, got %d", len(list))
	}

	// Verify all channels are in list (order not guaranteed)
	channelSet := make(map[string]bool)
	for _, ch := range list {
		channelSet[ch] = true
	}
	for _, ch := range channels {
		if !channelSet[ch] {
			t.Errorf("List should contain %s", ch)
		}
	}
}

func TestSubscriptionSet_Clear(t *testing.T) {
	ss := NewSubscriptionSet()
	ss.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.trade"})

	ss.Clear()

	if ss.Count() != 0 {
		t.Errorf("Count should be 0 after Clear, got %d", ss.Count())
	}
	if ss.Has("BTC.trade") {
		t.Error("Has should return false after Clear")
	}
}

func TestSubscriptionSet_Concurrent(t *testing.T) {
	ss := NewSubscriptionSet()
	const numGoroutines = 100
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // Add, Has, Remove goroutines

	// Concurrent Add
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ss.Add("BTC.trade")
			}
		}(i)
	}

	// Concurrent Has
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ss.Has("BTC.trade")
			}
		}(i)
	}

	// Concurrent Remove
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				ss.Remove("ETH.trade")
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no race/panic occurred
}

// =============================================================================
// SubscriptionIndex Tests
// =============================================================================

func TestSubscriptionIndex_AddGet(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	// Initially empty
	subs := idx.Get("BTC.trade")
	if len(subs) != 0 {
		t.Errorf("Get on empty index should return nil/empty, got %d items", len(subs))
	}

	// Add subscriber
	idx.Add("BTC.trade", client)
	subs = idx.Get("BTC.trade")
	if len(subs) != 1 {
		t.Errorf("Get should return 1 subscriber, got %d", len(subs))
	}
	if subs[0] != client {
		t.Error("Subscriber should match the added client")
	}

	// Count
	if idx.Count("BTC.trade") != 1 {
		t.Errorf("Count should be 1, got %d", idx.Count("BTC.trade"))
	}
}

func TestSubscriptionIndex_AddDuplicate(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	idx.Add("BTC.trade", client)
	idx.Add("BTC.trade", client) // Duplicate

	if idx.Count("BTC.trade") != 1 {
		t.Errorf("Duplicate Add should not increase count, got %d", idx.Count("BTC.trade"))
	}
}

func TestSubscriptionIndex_AddMultiple(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}
	channels := []string{"BTC.trade", "ETH.trade", "SOL.trade"}

	idx.AddMultiple(channels, client)

	for _, ch := range channels {
		if idx.Count(ch) != 1 {
			t.Errorf("Count(%s) should be 1, got %d", ch, idx.Count(ch))
		}
	}
}

func TestSubscriptionIndex_Remove(t *testing.T) {
	idx := NewSubscriptionIndex()
	client1 := &Client{id: 1}
	client2 := &Client{id: 2}

	idx.Add("BTC.trade", client1)
	idx.Add("BTC.trade", client2)

	if idx.Count("BTC.trade") != 2 {
		t.Errorf("Should have 2 subscribers, got %d", idx.Count("BTC.trade"))
	}

	// Remove one
	idx.Remove("BTC.trade", client1)
	if idx.Count("BTC.trade") != 1 {
		t.Errorf("Should have 1 subscriber after Remove, got %d", idx.Count("BTC.trade"))
	}

	// Remaining should be client2
	subs := idx.Get("BTC.trade")
	if len(subs) != 1 || subs[0] != client2 {
		t.Error("Remaining subscriber should be client2")
	}

	// Remove last subscriber - channel should be removed from map
	idx.Remove("BTC.trade", client2)
	if idx.Count("BTC.trade") != 0 {
		t.Errorf("Count should be 0 after removing all, got %d", idx.Count("BTC.trade"))
	}
}

func TestSubscriptionIndex_RemoveMultiple(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}
	channels := []string{"BTC.trade", "ETH.trade", "SOL.trade"}

	idx.AddMultiple(channels, client)
	idx.RemoveMultiple([]string{"BTC.trade", "SOL.trade"}, client)

	if idx.Count("BTC.trade") != 0 {
		t.Error("BTC.trade should have 0 subscribers")
	}
	if idx.Count("ETH.trade") != 1 {
		t.Error("ETH.trade should still have 1 subscriber")
	}
	if idx.Count("SOL.trade") != 0 {
		t.Error("SOL.trade should have 0 subscribers")
	}
}

func TestSubscriptionIndex_RemoveClient(t *testing.T) {
	idx := NewSubscriptionIndex()
	client1 := &Client{id: 1}
	client2 := &Client{id: 2}

	// Client1 subscribed to multiple channels
	idx.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.trade"}, client1)
	// Client2 subscribed to one channel
	idx.Add("BTC.trade", client2)

	// Remove client1 from all channels
	idx.RemoveClient(client1)

	// Client1 should be gone from all channels
	if idx.Count("ETH.trade") != 0 {
		t.Error("ETH.trade should have 0 subscribers after RemoveClient")
	}
	if idx.Count("SOL.trade") != 0 {
		t.Error("SOL.trade should have 0 subscribers after RemoveClient")
	}

	// Client2 should still be subscribed to BTC.trade
	if idx.Count("BTC.trade") != 1 {
		t.Errorf("BTC.trade should have 1 subscriber (client2), got %d", idx.Count("BTC.trade"))
	}
}

func TestSubscriptionIndex_AtomicSnapshot(t *testing.T) {
	idx := NewSubscriptionIndex()
	client1 := &Client{id: 1}
	client2 := &Client{id: 2}

	idx.Add("BTC.trade", client1)

	// Get snapshot
	snapshot1 := idx.Get("BTC.trade")
	if len(snapshot1) != 1 {
		t.Errorf("Snapshot should have 1 client, got %d", len(snapshot1))
	}

	// Modify index
	idx.Add("BTC.trade", client2)

	// Original snapshot should be unchanged (immutable)
	if len(snapshot1) != 1 {
		t.Errorf("Original snapshot should still have 1 client, got %d", len(snapshot1))
	}

	// New Get should return updated list
	snapshot2 := idx.Get("BTC.trade")
	if len(snapshot2) != 2 {
		t.Errorf("New snapshot should have 2 clients, got %d", len(snapshot2))
	}
}

func TestSubscriptionIndex_CopyOnWrite(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	idx.Add("BTC.trade", client)

	// Get snapshot while iterating
	snapshot := idx.Get("BTC.trade")

	// Concurrent modification shouldn't affect iteration
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			idx.Add("BTC.trade", &Client{id: int64(i + 10)})
		}
		done <- true
	}()

	// Iterate snapshot (should be safe even with concurrent modifications)
	count := 0
	for range snapshot {
		count++
	}

	<-done

	if count != 1 {
		t.Errorf("Snapshot iteration count should be 1, got %d", count)
	}
}

func TestSubscriptionIndex_Concurrent(t *testing.T) {
	idx := NewSubscriptionIndex()
	const numGoroutines = 50
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 4) // Add, Get, Remove, Count goroutines

	// Concurrent Add
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			client := &Client{id: int64(id)}
			for j := 0; j < numOps; j++ {
				idx.Add("BTC.trade", client)
			}
		}(i)
	}

	// Concurrent Get
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				_ = idx.Get("BTC.trade")
			}
		}(i)
	}

	// Concurrent Remove
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			client := &Client{id: int64(id + 1000)}
			for j := 0; j < numOps; j++ {
				idx.Remove("BTC.trade", client)
			}
		}(i)
	}

	// Concurrent Count
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				_ = idx.Count("BTC.trade")
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no race/panic occurred
}

// =============================================================================
// ConnectionPool Tests
// =============================================================================

func TestConnectionPool_GetPut(t *testing.T) {
	pool := NewConnectionPool(100, 512)

	// Get client from pool
	client := pool.Get()
	if client == nil {
		t.Fatal("Get should return a non-nil client")
	}

	// Verify send channel capacity
	if cap(client.send) != 512 {
		t.Errorf("Send channel capacity should be 512, got %d", cap(client.send))
	}

	// Verify sequence generator initialized
	if client.seqGen == nil {
		t.Error("seqGen should be initialized")
	}

	// Verify subscriptions initialized
	if client.subscriptions == nil {
		t.Error("subscriptions should be initialized")
	}

	// Put client back
	pool.Put(client)
	// Should not panic
}

func TestConnectionPool_BufferSize(t *testing.T) {
	tests := []struct {
		name       string
		bufferSize int
		expected   int
	}{
		{"default", 0, 512},
		{"custom 256", 256, 256},
		{"custom 1024", 1024, 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewConnectionPool(100, tt.bufferSize)
			client := pool.Get()
			if cap(client.send) != tt.expected {
				t.Errorf("Buffer size: got %d, want %d", cap(client.send), tt.expected)
			}
		})
	}
}

func TestConnectionPool_Reset(t *testing.T) {
	pool := NewConnectionPool(100, 512)

	// Get and modify client
	client := pool.Get()
	client.id = 999
	client.subscriptions.Add("BTC.trade")

	// Put back
	pool.Put(client)

	// Get again (might be same or different instance due to sync.Pool behavior)
	client2 := pool.Get()

	// ID should be 0 (reset during Put)
	if client2.id != 0 {
		t.Errorf("Client ID should be 0 after reset, got %d", client2.id)
	}

	// Note: sync.Pool doesn't guarantee same instance, so we can't reliably
	// test that subscriptions were cleared. But we can verify new clients
	// start with empty subscriptions.
	if client2.subscriptions.Count() != 0 {
		t.Errorf("Subscriptions should be cleared, got %d", client2.subscriptions.Count())
	}
}
