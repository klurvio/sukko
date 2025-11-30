package wsserver

import (
	"sync"
	"testing"
)

// =============================================================================
// SubscriptionSet Tests
// =============================================================================

func TestNewSubscriptionSet(t *testing.T) {
	set := NewSubscriptionSet()

	if set == nil {
		t.Fatal("NewSubscriptionSet should return non-nil")
	}
	if set.Count() != 0 {
		t.Errorf("New set should be empty, got count %d", set.Count())
	}
}

func TestSubscriptionSet_Add(t *testing.T) {
	set := NewSubscriptionSet()

	set.Add("BTC.trade")

	if !set.Has("BTC.trade") {
		t.Error("Should have BTC.trade after Add")
	}
	if set.Count() != 1 {
		t.Errorf("Count should be 1, got %d", set.Count())
	}
}

func TestSubscriptionSet_Add_Duplicate(t *testing.T) {
	set := NewSubscriptionSet()

	set.Add("BTC.trade")
	set.Add("BTC.trade") // Add same channel again

	if set.Count() != 1 {
		t.Errorf("Duplicate add should not increase count, got %d", set.Count())
	}
}

func TestSubscriptionSet_AddMultiple(t *testing.T) {
	set := NewSubscriptionSet()

	channels := []string{"BTC.trade", "ETH.trade", "SOL.liquidity"}
	set.AddMultiple(channels)

	if set.Count() != 3 {
		t.Errorf("Count should be 3, got %d", set.Count())
	}

	for _, ch := range channels {
		if !set.Has(ch) {
			t.Errorf("Should have %s after AddMultiple", ch)
		}
	}
}

func TestSubscriptionSet_Remove(t *testing.T) {
	set := NewSubscriptionSet()

	set.Add("BTC.trade")
	set.Add("ETH.trade")
	set.Remove("BTC.trade")

	if set.Has("BTC.trade") {
		t.Error("Should not have BTC.trade after Remove")
	}
	if !set.Has("ETH.trade") {
		t.Error("Should still have ETH.trade")
	}
	if set.Count() != 1 {
		t.Errorf("Count should be 1 after remove, got %d", set.Count())
	}
}

func TestSubscriptionSet_Remove_NonExistent(t *testing.T) {
	set := NewSubscriptionSet()

	// Should not panic
	set.Remove("nonexistent")

	if set.Count() != 0 {
		t.Error("Count should still be 0")
	}
}

func TestSubscriptionSet_RemoveMultiple(t *testing.T) {
	set := NewSubscriptionSet()

	set.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.liquidity", "DOGE.social"})
	set.RemoveMultiple([]string{"BTC.trade", "SOL.liquidity"})

	if set.Has("BTC.trade") || set.Has("SOL.liquidity") {
		t.Error("Removed channels should not exist")
	}
	if !set.Has("ETH.trade") || !set.Has("DOGE.social") {
		t.Error("Non-removed channels should still exist")
	}
	if set.Count() != 2 {
		t.Errorf("Count should be 2, got %d", set.Count())
	}
}

func TestSubscriptionSet_Has(t *testing.T) {
	set := NewSubscriptionSet()

	if set.Has("BTC.trade") {
		t.Error("Empty set should not have any channel")
	}

	set.Add("BTC.trade")

	if !set.Has("BTC.trade") {
		t.Error("Should have BTC.trade after Add")
	}
	if set.Has("ETH.trade") {
		t.Error("Should not have ETH.trade (not added)")
	}
}

func TestSubscriptionSet_List(t *testing.T) {
	set := NewSubscriptionSet()

	set.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.liquidity"})

	list := set.List()

	if len(list) != 3 {
		t.Errorf("List should have 3 items, got %d", len(list))
	}

	// Create a map to check presence (order not guaranteed)
	listMap := make(map[string]bool)
	for _, ch := range list {
		listMap[ch] = true
	}

	for _, expected := range []string{"BTC.trade", "ETH.trade", "SOL.liquidity"} {
		if !listMap[expected] {
			t.Errorf("List should contain %s", expected)
		}
	}
}

func TestSubscriptionSet_List_ReturnsCopy(t *testing.T) {
	set := NewSubscriptionSet()
	set.Add("BTC.trade")

	list := set.List()
	list[0] = "MODIFIED"

	// Original should not be affected
	if !set.Has("BTC.trade") {
		t.Error("Modifying list should not affect set")
	}
}

func TestSubscriptionSet_Clear(t *testing.T) {
	set := NewSubscriptionSet()

	set.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.liquidity"})
	set.Clear()

	if set.Count() != 0 {
		t.Errorf("Count should be 0 after Clear, got %d", set.Count())
	}
	if set.Has("BTC.trade") {
		t.Error("Should not have any channel after Clear")
	}
}

func TestSubscriptionSet_ThreadSafety(t *testing.T) {
	set := NewSubscriptionSet()
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent adds
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range iterations {
				set.Add("channel" + string(rune('A'+id)))
			}
		}(i)
	}

	// Concurrent reads
	for range 10 {
		wg.Go(func() {
			for range iterations {
				_ = set.Count()
				_ = set.Has("channelA")
				_ = set.List()
			}
		})
	}

	wg.Wait()
	// Test passes if no race conditions or panics
}

// =============================================================================
// SubscriptionIndex Tests
// =============================================================================

func TestNewSubscriptionIndex(t *testing.T) {
	idx := NewSubscriptionIndex()

	if idx == nil {
		t.Fatal("NewSubscriptionIndex should return non-nil")
	}
	if idx.Count("BTC.trade") != 0 {
		t.Error("New index should have no subscribers")
	}
}

func TestSubscriptionIndex_Add(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	idx.Add("BTC.trade", client)

	if idx.Count("BTC.trade") != 1 {
		t.Errorf("Count should be 1, got %d", idx.Count("BTC.trade"))
	}

	clients := idx.Get("BTC.trade")
	if len(clients) != 1 || clients[0] != client {
		t.Error("Get should return the added client")
	}
}

func TestSubscriptionIndex_Add_Duplicate(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	idx.Add("BTC.trade", client)
	idx.Add("BTC.trade", client) // Add same client again

	if idx.Count("BTC.trade") != 1 {
		t.Errorf("Duplicate add should not increase count, got %d", idx.Count("BTC.trade"))
	}
}

func TestSubscriptionIndex_Add_MultipleClients(t *testing.T) {
	idx := NewSubscriptionIndex()
	client1 := &Client{id: 1}
	client2 := &Client{id: 2}
	client3 := &Client{id: 3}

	idx.Add("BTC.trade", client1)
	idx.Add("BTC.trade", client2)
	idx.Add("BTC.trade", client3)

	if idx.Count("BTC.trade") != 3 {
		t.Errorf("Count should be 3, got %d", idx.Count("BTC.trade"))
	}
}

func TestSubscriptionIndex_AddMultiple(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	channels := []string{"BTC.trade", "ETH.trade", "SOL.liquidity"}
	idx.AddMultiple(channels, client)

	for _, ch := range channels {
		if idx.Count(ch) != 1 {
			t.Errorf("Count for %s should be 1", ch)
		}
	}
}

func TestSubscriptionIndex_Remove(t *testing.T) {
	idx := NewSubscriptionIndex()
	client1 := &Client{id: 1}
	client2 := &Client{id: 2}

	idx.Add("BTC.trade", client1)
	idx.Add("BTC.trade", client2)
	idx.Remove("BTC.trade", client1)

	if idx.Count("BTC.trade") != 1 {
		t.Errorf("Count should be 1 after remove, got %d", idx.Count("BTC.trade"))
	}

	clients := idx.Get("BTC.trade")
	if len(clients) != 1 || clients[0] != client2 {
		t.Error("Should only have client2 remaining")
	}
}

func TestSubscriptionIndex_Remove_NonExistent(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	// Should not panic
	idx.Remove("BTC.trade", client)
	idx.Remove("nonexistent", client)
}

func TestSubscriptionIndex_Remove_LastClient(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	idx.Add("BTC.trade", client)
	idx.Remove("BTC.trade", client)

	if idx.Count("BTC.trade") != 0 {
		t.Errorf("Count should be 0 after removing last client, got %d", idx.Count("BTC.trade"))
	}

	clients := idx.Get("BTC.trade")
	if clients != nil {
		t.Error("Get should return nil for empty channel")
	}
}

func TestSubscriptionIndex_RemoveMultiple(t *testing.T) {
	idx := NewSubscriptionIndex()
	client := &Client{id: 1}

	idx.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.liquidity"}, client)
	idx.RemoveMultiple([]string{"BTC.trade", "SOL.liquidity"}, client)

	if idx.Count("BTC.trade") != 0 {
		t.Error("BTC.trade should have no subscribers")
	}
	if idx.Count("ETH.trade") != 1 {
		t.Error("ETH.trade should still have subscriber")
	}
	if idx.Count("SOL.liquidity") != 0 {
		t.Error("SOL.liquidity should have no subscribers")
	}
}

func TestSubscriptionIndex_RemoveClient(t *testing.T) {
	idx := NewSubscriptionIndex()
	client1 := &Client{id: 1}
	client2 := &Client{id: 2}

	// Client1 subscribes to multiple channels
	idx.AddMultiple([]string{"BTC.trade", "ETH.trade", "SOL.liquidity"}, client1)
	// Client2 also subscribes to some
	idx.AddMultiple([]string{"BTC.trade", "ETH.trade"}, client2)

	// Remove client1 from all channels
	idx.RemoveClient(client1)

	// client1 should be gone from all channels
	for _, ch := range []string{"BTC.trade", "ETH.trade", "SOL.liquidity"} {
		clients := idx.Get(ch)
		for _, c := range clients {
			if c == client1 {
				t.Errorf("client1 should not be in %s", ch)
			}
		}
	}

	// client2 should still be in its channels
	if idx.Count("BTC.trade") != 1 || idx.Count("ETH.trade") != 1 {
		t.Error("client2 should still be subscribed")
	}
}

func TestSubscriptionIndex_Get_NonExistent(t *testing.T) {
	idx := NewSubscriptionIndex()

	clients := idx.Get("nonexistent")

	if clients != nil {
		t.Error("Get should return nil for non-existent channel")
	}
}

func TestSubscriptionIndex_ThreadSafety(t *testing.T) {
	idx := NewSubscriptionIndex()
	var wg sync.WaitGroup
	iterations := 100

	// Create some clients
	clients := make([]*Client, 10)
	for i := range clients {
		clients[i] = &Client{id: int64(i)}
	}

	// Concurrent adds
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range iterations {
				idx.Add("channel"+string(rune('A'+id%3)), clients[id])
			}
		}(i)
	}

	// Concurrent reads
	for range 10 {
		wg.Go(func() {
			for range iterations {
				_ = idx.Count("channelA")
				_ = idx.Get("channelB")
			}
		})
	}

	// Concurrent removes
	for i := range 5 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range iterations / 2 {
				idx.Remove("channelA", clients[id])
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions or panics
}

// =============================================================================
// ConnectionPool Tests
// =============================================================================

func TestNewConnectionPool(t *testing.T) {
	pool := NewConnectionPool(100, 512)
	if pool == nil {
		t.Fatal("NewConnectionPool should return non-nil")
	}

	if pool.maxSize != 100 { //nolint:staticcheck // SA5011 false positive - t.Fatal stops execution
		t.Errorf("maxSize should be 100, got %d", pool.maxSize)
	}
	if pool.bufferSize != 512 {
		t.Errorf("bufferSize should be 512, got %d", pool.bufferSize)
	}
}

func TestNewConnectionPool_DefaultBufferSize(t *testing.T) {
	pool := NewConnectionPool(100, 0) // 0 = use default

	if pool.bufferSize != 512 {
		t.Errorf("Default bufferSize should be 512, got %d", pool.bufferSize)
	}
}

func TestNewConnectionPool_NegativeBufferSize(t *testing.T) {
	pool := NewConnectionPool(100, -1) // Negative = use default

	if pool.bufferSize != 512 {
		t.Errorf("Default bufferSize should be 512 for negative input, got %d", pool.bufferSize)
	}
}

func TestConnectionPool_Get(t *testing.T) {
	pool := NewConnectionPool(100, 256)

	client := pool.Get()
	if client == nil {
		t.Fatal("Get should return non-nil client")
	}

	if cap(client.send) != 256 { //nolint:staticcheck // SA5011 false positive - t.Fatal stops execution
		t.Errorf("send channel capacity should be 256, got %d", cap(client.send))
	}
	if client.seqGen == nil {
		t.Error("seqGen should be initialized")
	}
	if client.subscriptions == nil {
		t.Error("subscriptions should be initialized")
	}
}

func TestConnectionPool_Get_SequenceReset(t *testing.T) {
	pool := NewConnectionPool(100, 256)

	client := pool.Get()
	// Advance sequence number
	for range 10 {
		client.seqGen.Next()
	}

	// Return and get again
	pool.Put(client)
	client2 := pool.Get()

	// Sequence should be reset
	if client2.seqGen.Current() != 0 {
		t.Errorf("Sequence should be reset, got %d", client2.seqGen.Current())
	}
}

func TestConnectionPool_Get_SubscriptionsClear(t *testing.T) {
	pool := NewConnectionPool(100, 256)

	client := pool.Get()
	client.subscriptions.Add("BTC.trade")
	client.subscriptions.Add("ETH.trade")

	// Return and get again
	pool.Put(client)
	client2 := pool.Get()

	// Subscriptions should be cleared
	if client2.subscriptions.Count() != 0 {
		t.Errorf("Subscriptions should be cleared, got %d", client2.subscriptions.Count())
	}
}

func TestConnectionPool_Put(t *testing.T) {
	pool := NewConnectionPool(100, 256)

	client := pool.Get()
	client.id = 123

	pool.Put(client)

	// After Put, id should be reset
	if client.id != 0 {
		t.Errorf("id should be reset to 0, got %d", client.id)
	}
	if client.conn != nil {
		t.Error("conn should be nil after Put")
	}
	if client.server != nil {
		t.Error("server should be nil after Put")
	}
}

func TestConnectionPool_Put_Nil(t *testing.T) {
	pool := NewConnectionPool(100, 256)

	// Should not panic
	pool.Put(nil)
}

func TestConnectionPool_ThreadSafety(t *testing.T) {
	pool := NewConnectionPool(100, 256)
	var wg sync.WaitGroup
	iterations := 100

	// Concurrent Get and Put
	for range 20 {
		wg.Go(func() {
			for j := range iterations {
				client := pool.Get()
				if client != nil {
					client.id = int64(j)
					pool.Put(client)
				}
			}
		})
	}

	wg.Wait()
	// Test passes if no race conditions or panics
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkSubscriptionSet_Add(b *testing.B) {
	set := NewSubscriptionSet()
	for b.Loop() {
		set.Add("BTC.trade")
	}
}

func BenchmarkSubscriptionSet_Has(b *testing.B) {
	set := NewSubscriptionSet()
	set.Add("BTC.trade")

	for b.Loop() {
		_ = set.Has("BTC.trade")
	}
}

func BenchmarkSubscriptionIndex_Add(b *testing.B) {
	idx := NewSubscriptionIndex()
	clients := make([]*Client, 100)
	for i := range clients {
		clients[i] = &Client{id: int64(i)}
	}

	for i := 0; b.Loop(); i++ {
		idx.Add("BTC.trade", clients[i%100])
	}
}

func BenchmarkSubscriptionIndex_Get(b *testing.B) {
	idx := NewSubscriptionIndex()
	for i := range 1000 {
		idx.Add("BTC.trade", &Client{id: int64(i)})
	}

	for b.Loop() {
		_ = idx.Get("BTC.trade")
	}
}

func BenchmarkConnectionPool_GetPut(b *testing.B) {
	pool := NewConnectionPool(1000, 256)

	for b.Loop() {
		client := pool.Get()
		pool.Put(client)
	}
}
