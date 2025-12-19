package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/limits"
	"github.com/Toniq-Labs/odin-ws/internal/messaging"
	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
	"github.com/Toniq-Labs/odin-ws/internal/types"
	"github.com/rs/zerolog"
)

// =============================================================================
// Buffer Usage Calculation Tests
// =============================================================================

func TestBufferUsagePercent_Empty(t *testing.T) {
	bufferLen := 0
	bufferCap := 512

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 0 {
		t.Errorf("percent: got %f, want 0", percent)
	}
}

func TestBufferUsagePercent_Half(t *testing.T) {
	bufferLen := 256
	bufferCap := 512

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 50 {
		t.Errorf("percent: got %f, want 50", percent)
	}
}

func TestBufferUsagePercent_Full(t *testing.T) {
	bufferLen := 512
	bufferCap := 512

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 100 {
		t.Errorf("percent: got %f, want 100", percent)
	}
}

func TestBufferUsagePercent_Quarter(t *testing.T) {
	bufferLen := 128
	bufferCap := 512

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 25 {
		t.Errorf("percent: got %f, want 25", percent)
	}
}

func TestBufferUsagePercent_ThreeQuarters(t *testing.T) {
	bufferLen := 384
	bufferCap := 512

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 75 {
		t.Errorf("percent: got %f, want 75", percent)
	}
}

func TestBufferUsagePercent_SmallBuffer(t *testing.T) {
	bufferLen := 5
	bufferCap := 10

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 50 {
		t.Errorf("percent: got %f, want 50", percent)
	}
}

func TestBufferUsagePercent_LargeBuffer(t *testing.T) {
	bufferLen := 512
	bufferCap := 1024

	percent := float64(bufferLen) / float64(bufferCap) * 100

	if percent != 50 {
		t.Errorf("percent: got %f, want 50", percent)
	}
}

// =============================================================================
// Connection Duration Tests
// =============================================================================

func TestConnectionDuration_Calculation(t *testing.T) {
	connectedAt := time.Now().Add(-5 * time.Second)

	duration := time.Since(connectedAt)

	// Should be approximately 5 seconds (allow some tolerance)
	if duration < 4*time.Second || duration > 6*time.Second {
		t.Errorf("duration: got %v, expected ~5s", duration)
	}
}

func TestConnectionDuration_LongConnection(t *testing.T) {
	connectedAt := time.Now().Add(-1 * time.Hour)

	duration := time.Since(connectedAt)

	// Should be approximately 1 hour
	if duration < 59*time.Minute || duration > 61*time.Minute {
		t.Errorf("duration: got %v, expected ~1h", duration)
	}
}

func TestConnectionDuration_VeryShort(t *testing.T) {
	connectedAt := time.Now().Add(-100 * time.Millisecond)

	duration := time.Since(connectedAt)

	// Should be approximately 100ms
	if duration < 90*time.Millisecond || duration > 200*time.Millisecond {
		t.Errorf("duration: got %v, expected ~100ms", duration)
	}
}

// =============================================================================
// Client State Tests
// =============================================================================

func TestClient_SendAttemptsAtomic(t *testing.T) {
	client := &Client{
		sendAttempts: 0,
	}

	// Concurrent increments
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			atomic.AddInt32(&client.sendAttempts, 1)
		})
	}
	wg.Wait()

	if atomic.LoadInt32(&client.sendAttempts) != 100 {
		t.Errorf("sendAttempts: got %d, want 100", client.sendAttempts)
	}
}

func TestClient_SlowClientWarnedFlag(t *testing.T) {
	client := &Client{
		slowClientWarned: 0,
	}

	// First warning should succeed
	if !atomic.CompareAndSwapInt32(&client.slowClientWarned, 0, 1) {
		t.Error("First CAS should succeed")
	}

	// Second warning should fail (already warned)
	if atomic.CompareAndSwapInt32(&client.slowClientWarned, 0, 1) {
		t.Error("Second CAS should fail (already warned)")
	}

	if atomic.LoadInt32(&client.slowClientWarned) != 1 {
		t.Error("slowClientWarned should be 1")
	}
}

// =============================================================================
// Disconnect Reason Constants Tests
// =============================================================================

func TestDisconnectReasons_Constants(t *testing.T) {
	// Verify disconnect reason constants are defined correctly
	reasons := []string{
		monitoring.DisconnectReasonReadError,
		monitoring.DisconnectReasonWriteTimeout,
		monitoring.DisconnectReasonPingTimeout,
		monitoring.DisconnectReasonServerShutdown,
		monitoring.DisconnectReasonRateLimitExceeded,
	}

	for _, reason := range reasons {
		if reason == "" {
			t.Error("Disconnect reason constant should not be empty")
		}
	}
}

func TestDisconnectInitiatedBy_Constants(t *testing.T) {
	initiators := []string{
		monitoring.DisconnectInitiatedByClient,
		monitoring.DisconnectInitiatedByServer,
	}

	for _, initiator := range initiators {
		if initiator == "" {
			t.Error("Disconnect initiator constant should not be empty")
		}
	}
}

// =============================================================================
// Stats Decrement Tests
// =============================================================================

func TestStats_CurrentConnectionsDecrement(t *testing.T) {
	stats := &types.Stats{
		CurrentConnections: 10,
	}

	atomic.AddInt64(&stats.CurrentConnections, -1)

	if stats.CurrentConnections != 9 {
		t.Errorf("CurrentConnections: got %d, want 9", stats.CurrentConnections)
	}
}

func TestStats_CurrentConnectionsDecrement_ToZero(t *testing.T) {
	stats := &types.Stats{
		CurrentConnections: 1,
	}

	atomic.AddInt64(&stats.CurrentConnections, -1)

	if stats.CurrentConnections != 0 {
		t.Errorf("CurrentConnections: got %d, want 0", stats.CurrentConnections)
	}
}

func TestStats_CurrentConnectionsDecrement_Concurrent(t *testing.T) {
	stats := &types.Stats{
		CurrentConnections: 1000,
	}

	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			atomic.AddInt64(&stats.CurrentConnections, -1)
		})
	}
	wg.Wait()

	if stats.CurrentConnections != 0 {
		t.Errorf("CurrentConnections: got %d, want 0", stats.CurrentConnections)
	}
}

// =============================================================================
// CloseOnce Tests
// =============================================================================

func TestCloseOnce_SingleExecution(t *testing.T) {
	var closeOnce sync.Once
	closeCount := 0

	closeOnce.Do(func() {
		closeCount++
	})

	closeOnce.Do(func() {
		closeCount++
	})

	closeOnce.Do(func() {
		closeCount++
	})

	if closeCount != 1 {
		t.Errorf("closeCount: got %d, want 1 (sync.Once should execute only once)", closeCount)
	}
}

func TestCloseOnce_ConcurrentCalls(t *testing.T) {
	var closeOnce sync.Once
	var closeCount int32

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			closeOnce.Do(func() {
				atomic.AddInt32(&closeCount, 1)
			})
		})
	}
	wg.Wait()

	if atomic.LoadInt32(&closeCount) != 1 {
		t.Errorf("closeCount: got %d, want 1", closeCount)
	}
}

// =============================================================================
// Semaphore Release Tests
// =============================================================================

func TestSemaphore_Release(t *testing.T) {
	maxConn := 10
	sem := make(chan struct{}, maxConn)

	// Fill the semaphore
	for range maxConn {
		sem <- struct{}{}
	}

	// Should be full
	select {
	case sem <- struct{}{}:
		t.Error("Semaphore should be full")
	default:
		// Expected
	}

	// Release one slot
	<-sem

	// Now should be able to acquire
	select {
	case sem <- struct{}{}:
		// Expected
	default:
		t.Error("Should be able to acquire after release")
	}
}

func TestSemaphore_ReleaseMultiple(t *testing.T) {
	maxConn := 10
	sem := make(chan struct{}, maxConn)

	// Fill the semaphore
	for range maxConn {
		sem <- struct{}{}
	}

	// Release 5 slots
	for range 5 {
		<-sem
	}

	// Should be able to acquire 5 more
	for i := range 5 {
		select {
		case sem <- struct{}{}:
			// Expected
		default:
			t.Errorf("Should be able to acquire slot %d", i+1)
		}
	}

	// Now should be full again
	select {
	case sem <- struct{}{}:
		t.Error("Semaphore should be full again")
	default:
		// Expected
	}
}

// =============================================================================
// Integration Test: Minimal disconnectClient
// =============================================================================

func TestDisconnectClient_Integration(t *testing.T) {
	// Create minimal server with required components
	logger := zerolog.Nop()
	stats := &types.Stats{
		CurrentConnections:  1,
		DisconnectsByReason: make(map[string]int64),
	}

	server := &Server{
		logger:            logger,
		stats:             stats,
		connections:       NewConnectionPool(100, 256),
		connectionsSem:    make(chan struct{}, 100),
		subscriptionIndex: NewSubscriptionIndex(),
		rateLimiter:       limits.NewRateLimiter(),
	}

	// Acquire a connection slot
	server.connectionsSem <- struct{}{}

	// Create a client
	client := &Client{
		id:            12345,
		send:          make(chan []byte, 256),
		seqGen:        messaging.NewSequenceGenerator(),
		subscriptions: NewSubscriptionSet(),
		connectedAt:   time.Now().Add(-5 * time.Second),
		conn:          nil, // No real connection for this test
	}

	// Store client in server's map
	server.clients.Store(client, true)

	// Add some subscriptions
	client.subscriptions.Add("BTC.trade")
	server.subscriptionIndex.Add("BTC.trade", client)

	// Disconnect
	server.disconnectClient(client, monitoring.DisconnectReasonReadError, monitoring.DisconnectInitiatedByClient)

	// Verify stats updated
	if stats.CurrentConnections != 0 {
		t.Errorf("CurrentConnections should be 0, got %d", stats.CurrentConnections)
	}

	// Verify client removed from subscription index
	clients := server.subscriptionIndex.Get("BTC.trade")
	if len(clients) != 0 {
		t.Errorf("Client should be removed from subscription index, got %d clients", len(clients))
	}

	// Verify semaphore released (should be able to acquire again)
	select {
	case server.connectionsSem <- struct{}{}:
		// Good - slot was released
	default:
		t.Error("Connection semaphore slot should have been released")
	}
}

// =============================================================================
// Client Fields Access Tests
// =============================================================================

func TestClient_SequenceGenerator(t *testing.T) {
	client := &Client{
		seqGen: messaging.NewSequenceGenerator(),
	}

	// Generate some sequences
	seq1 := client.seqGen.Next()
	seq2 := client.seqGen.Next()
	seq3 := client.seqGen.Next()

	if seq1 != 1 {
		t.Errorf("First sequence should be 1, got %d", seq1)
	}
	if seq2 != 2 {
		t.Errorf("Second sequence should be 2, got %d", seq2)
	}
	if seq3 != 3 {
		t.Errorf("Third sequence should be 3, got %d", seq3)
	}

	// Current should return last generated
	if client.seqGen.Current() != 3 {
		t.Errorf("Current should be 3, got %d", client.seqGen.Current())
	}
}

func TestClient_SubscriptionsCount(t *testing.T) {
	client := &Client{
		subscriptions: NewSubscriptionSet(),
	}

	if client.subscriptions.Count() != 0 {
		t.Errorf("Initial count should be 0, got %d", client.subscriptions.Count())
	}

	client.subscriptions.Add("BTC.trade")
	client.subscriptions.Add("ETH.trade")
	client.subscriptions.Add("SOL.liquidity")

	if client.subscriptions.Count() != 3 {
		t.Errorf("Count should be 3, got %d", client.subscriptions.Count())
	}
}

// =============================================================================
// Buffer Length/Capacity Tests
// =============================================================================

func TestClient_SendBufferMetrics(t *testing.T) {
	tests := []struct {
		name        string
		bufferCap   int
		fillCount   int
		wantLen     int
		wantPercent float64
	}{
		{
			name:        "empty buffer",
			bufferCap:   256,
			fillCount:   0,
			wantLen:     0,
			wantPercent: 0,
		},
		{
			name:        "quarter full",
			bufferCap:   256,
			fillCount:   64,
			wantLen:     64,
			wantPercent: 25,
		},
		{
			name:        "half full",
			bufferCap:   256,
			fillCount:   128,
			wantLen:     128,
			wantPercent: 50,
		},
		{
			name:        "three quarters",
			bufferCap:   256,
			fillCount:   192,
			wantLen:     192,
			wantPercent: 75,
		},
		{
			name:        "full",
			bufferCap:   256,
			fillCount:   256,
			wantLen:     256,
			wantPercent: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				send: make(chan []byte, tt.bufferCap),
			}

			// Fill buffer
			for range tt.fillCount {
				client.send <- []byte("msg")
			}

			bufferLen := len(client.send)
			bufferCap := cap(client.send)
			bufferPercent := float64(bufferLen) / float64(bufferCap) * 100

			if bufferLen != tt.wantLen {
				t.Errorf("len: got %d, want %d", bufferLen, tt.wantLen)
			}
			if bufferCap != tt.bufferCap {
				t.Errorf("cap: got %d, want %d", bufferCap, tt.bufferCap)
			}
			if bufferPercent != tt.wantPercent {
				t.Errorf("percent: got %f, want %f", bufferPercent, tt.wantPercent)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkBufferUsageCalculation(b *testing.B) {
	bufferLen := 256
	bufferCap := 512

	for b.Loop() {
		_ = float64(bufferLen) / float64(bufferCap) * 100
	}
}

func BenchmarkAtomicDecrement(b *testing.B) {
	var counter int64 = 1000000

	for b.Loop() {
		atomic.AddInt64(&counter, -1)
		atomic.AddInt64(&counter, 1) // Reset for next iteration
	}
}

func BenchmarkSyncOnce(b *testing.B) {

	for b.Loop() {
		var once sync.Once
		once.Do(func() {})
	}
}

func BenchmarkTimeSince(b *testing.B) {
	connectedAt := time.Now().Add(-5 * time.Second)

	for b.Loop() {
		_ = time.Since(connectedAt)
	}
}
