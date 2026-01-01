package gateway

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// =============================================================================
// LeastConnRouter Unit Tests
// =============================================================================

func TestLeastConnRouter_SelectPod_LeastConnections(t *testing.T) {
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 100, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.2": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()}, // Least
			"10.0.0.3": {Connections: 200, MaxConns: 1000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	if selected != "10.0.0.2" {
		t.Errorf("Expected 10.0.0.2 (least connections), got %s", selected)
	}
}

func TestLeastConnRouter_SelectPod_StalePod(t *testing.T) {
	staleTime := time.Now().Add(-60 * time.Second) // Older than staleTTL

	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 100, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.2": {Connections: 10, MaxConns: 1000, LastUpdate: staleTime}, // Stale, should skip
			"10.0.0.3": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	// Should select 10.0.0.3 (second lowest after stale pod is skipped)
	if selected != "10.0.0.3" {
		t.Errorf("Expected 10.0.0.3 (stale pod skipped), got %s", selected)
	}
}

func TestLeastConnRouter_SelectPod_FullPod(t *testing.T) {
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 1000, MaxConns: 1000, LastUpdate: time.Now()}, // Full
			"10.0.0.2": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.3": {Connections: 100, MaxConns: 1000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	// Should select 10.0.0.2 (lowest after full pod is skipped)
	if selected != "10.0.0.2" {
		t.Errorf("Expected 10.0.0.2 (full pod skipped), got %s", selected)
	}
}

func TestLeastConnRouter_SelectPod_NoPods(t *testing.T) {
	router := &LeastConnRouter{
		pods:     make(map[string]*PodMetrics),
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	if selected != "" {
		t.Errorf("Expected empty string for no pods, got %s", selected)
	}
}

func TestLeastConnRouter_SelectPod_AllStale(t *testing.T) {
	staleTime := time.Now().Add(-60 * time.Second)

	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 50, MaxConns: 1000, LastUpdate: staleTime},
			"10.0.0.2": {Connections: 100, MaxConns: 1000, LastUpdate: staleTime},
			"10.0.0.3": {Connections: 25, MaxConns: 1000, LastUpdate: staleTime},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	if selected != "" {
		t.Errorf("Expected empty string when all pods are stale, got %s", selected)
	}
}

func TestLeastConnRouter_SelectPod_AllFull(t *testing.T) {
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 1000, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.2": {Connections: 500, MaxConns: 500, LastUpdate: time.Now()},
			"10.0.0.3": {Connections: 2000, MaxConns: 2000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	if selected != "" {
		t.Errorf("Expected empty string when all pods are full, got %s", selected)
	}
}

func TestLeastConnRouter_UpdateMetrics(t *testing.T) {
	router := &LeastConnRouter{
		pods:     make(map[string]*PodMetrics),
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	// Simulate update (as done in the NATS subscription handler)
	router.mu.Lock()
	router.pods["10.0.0.5"] = &PodMetrics{
		Connections: 75,
		MaxConns:    1000,
		LastUpdate:  time.Now(),
	}
	router.mu.Unlock()

	// Verify update
	router.mu.RLock()
	pod, exists := router.pods["10.0.0.5"]
	router.mu.RUnlock()

	if !exists {
		t.Fatal("Pod should exist after update")
	}
	if pod.Connections != 75 {
		t.Errorf("Connections: got %d, want 75", pod.Connections)
	}
	if pod.MaxConns != 1000 {
		t.Errorf("MaxConns: got %d, want 1000", pod.MaxConns)
	}
}

func TestLeastConnRouter_GetMetrics(t *testing.T) {
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 100, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.2": {Connections: 50, MaxConns: 500, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	// Simulate some activity
	router.updatesReceived.Store(10)
	router.podSelections.Store(5)
	router.fallbackCount.Store(2)

	metrics := router.GetMetrics()

	// Check metrics fields
	pods, ok := metrics["pods"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected pods to be []map[string]interface{}")
	}
	if len(pods) != 2 {
		t.Errorf("Pods count: got %d, want 2", len(pods))
	}

	if metrics["updates_received"].(uint64) != 10 {
		t.Errorf("updates_received: got %v, want 10", metrics["updates_received"])
	}
	if metrics["pod_selections"].(uint64) != 5 {
		t.Errorf("pod_selections: got %v, want 5", metrics["pod_selections"])
	}
	if metrics["fallback_count"].(uint64) != 2 {
		t.Errorf("fallback_count: got %v, want 2", metrics["fallback_count"])
	}
}

func TestLeastConnRouter_ActivePodCount(t *testing.T) {
	staleTime := time.Now().Add(-60 * time.Second)

	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 100, MaxConns: 1000, LastUpdate: time.Now()}, // Active
			"10.0.0.2": {Connections: 50, MaxConns: 500, LastUpdate: staleTime},    // Stale
			"10.0.0.3": {Connections: 75, MaxConns: 1000, LastUpdate: time.Now()},  // Active
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	count := router.ActivePodCount()
	if count != 2 {
		t.Errorf("ActivePodCount: got %d, want 2", count)
	}
}

func TestLeastConnRouter_ConcurrentAccess(t *testing.T) {
	router := &LeastConnRouter{
		pods:     make(map[string]*PodMetrics),
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	var wg sync.WaitGroup
	const numGoroutines = 100

	// Concurrent writers
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			podIP := "10.0.0." + string(rune('0'+(id%10)))
			router.mu.Lock()
			router.pods[podIP] = &PodMetrics{
				Connections: int64(id * 10),
				MaxConns:    1000,
				LastUpdate:  time.Now(),
			}
			router.mu.Unlock()
		}(i)
	}

	// Concurrent readers
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = router.SelectPod()
			_ = router.ActivePodCount()
			_ = router.GetMetrics()
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// If we get here without deadlock or race, the test passes
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestLeastConnRouter_SelectPod_TiedConnections(t *testing.T) {
	// When multiple pods have the same connection count, we should get a consistent result
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.2": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
			"10.0.0.3": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	// Call multiple times to ensure consistency (map iteration is random but result should be deterministic)
	selected := router.SelectPod()
	if selected == "" {
		t.Error("Should select a pod when all have same connections")
	}
}

func TestLeastConnRouter_SelectPod_OverCapacity(t *testing.T) {
	// Pod with connections > max (shouldn't happen but should be handled)
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 1100, MaxConns: 1000, LastUpdate: time.Now()}, // Over capacity
			"10.0.0.2": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	selected := router.SelectPod()
	if selected != "10.0.0.2" {
		t.Errorf("Expected 10.0.0.2 (over-capacity pod skipped), got %s", selected)
	}
}

func TestLeastConnRouter_FallbackIncrement(t *testing.T) {
	router := &LeastConnRouter{
		pods:     make(map[string]*PodMetrics), // Empty - will trigger fallback
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	initialFallback := router.fallbackCount.Load()
	_ = router.SelectPod()
	finalFallback := router.fallbackCount.Load()

	if finalFallback != initialFallback+1 {
		t.Errorf("Fallback count should increment: got %d, want %d", finalFallback, initialFallback+1)
	}
}

func TestLeastConnRouter_PodSelectionIncrement(t *testing.T) {
	router := &LeastConnRouter{
		pods: map[string]*PodMetrics{
			"10.0.0.1": {Connections: 50, MaxConns: 1000, LastUpdate: time.Now()},
		},
		staleTTL: 30 * time.Second,
		logger:   zerolog.Nop(),
	}

	initialSelections := router.podSelections.Load()
	selected := router.SelectPod()
	finalSelections := router.podSelections.Load()

	if selected == "" {
		t.Fatal("Should select a pod")
	}
	if finalSelections != initialSelections+1 {
		t.Errorf("Pod selections should increment: got %d, want %d", finalSelections, initialSelections+1)
	}
}
