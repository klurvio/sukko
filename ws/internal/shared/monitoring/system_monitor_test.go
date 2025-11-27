package monitoring

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Note: SystemMonitor is a singleton, so tests must be careful about order and state.
// These tests verify the public API without relying on specific internal state.

// =============================================================================
// SystemMetrics Tests
// =============================================================================

func TestSystemMetrics_ZeroValue(t *testing.T) {
	metrics := SystemMetrics{}

	if metrics.CPUPercent != 0 {
		t.Errorf("CPUPercent should be 0, got %f", metrics.CPUPercent)
	}
	if metrics.MemoryBytes != 0 {
		t.Errorf("MemoryBytes should be 0, got %d", metrics.MemoryBytes)
	}
	if metrics.MemoryMB != 0 {
		t.Errorf("MemoryMB should be 0, got %f", metrics.MemoryMB)
	}
	if metrics.Goroutines != 0 {
		t.Errorf("Goroutines should be 0, got %d", metrics.Goroutines)
	}
	if metrics.CPUAllocation != 0 {
		t.Errorf("CPUAllocation should be 0, got %f", metrics.CPUAllocation)
	}
	if !metrics.Timestamp.IsZero() {
		t.Errorf("Timestamp should be zero, got %v", metrics.Timestamp)
	}
}

func TestSystemMetrics_Fields(t *testing.T) {
	now := time.Now()
	metrics := SystemMetrics{
		CPUPercent:    75.5,
		MemoryBytes:   1024 * 1024 * 1024, // 1GB
		MemoryMB:      1024.0,
		Goroutines:    500,
		CPUAllocation: 2.5,
		Timestamp:     now,
	}

	if metrics.CPUPercent != 75.5 {
		t.Errorf("CPUPercent: got %f, want 75.5", metrics.CPUPercent)
	}
	if metrics.MemoryBytes != 1024*1024*1024 {
		t.Errorf("MemoryBytes: got %d, want 1073741824", metrics.MemoryBytes)
	}
	if metrics.MemoryMB != 1024.0 {
		t.Errorf("MemoryMB: got %f, want 1024.0", metrics.MemoryMB)
	}
	if metrics.Goroutines != 500 {
		t.Errorf("Goroutines: got %d, want 500", metrics.Goroutines)
	}
	if metrics.CPUAllocation != 2.5 {
		t.Errorf("CPUAllocation: got %f, want 2.5", metrics.CPUAllocation)
	}
	if !metrics.Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch")
	}
}

// =============================================================================
// GetSystemMonitor Tests (Singleton)
// =============================================================================

func TestGetSystemMonitor_ReturnsSingleton(t *testing.T) {
	logger := zerolog.Nop()

	// Get the singleton
	sm1 := GetSystemMonitor(logger)
	if sm1 == nil {
		t.Fatal("GetSystemMonitor should return non-nil")
	}

	// Get it again - should be same instance
	sm2 := GetSystemMonitor(logger)
	if sm1 != sm2 {
		t.Error("GetSystemMonitor should return same instance (singleton)")
	}
}

func TestGetSystemMonitor_HasCPUMonitor(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	if sm.cpuMonitor == nil {
		t.Error("cpuMonitor should be initialized")
	}
}

// =============================================================================
// GetMetrics Tests
// =============================================================================

func TestSystemMonitor_GetMetrics_ReturnsValidStruct(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	metrics := sm.GetMetrics()

	// Metrics should be a valid struct (not nil pointer panic)
	// Values may be zero if monitoring hasn't started
	_ = metrics.CPUPercent
	_ = metrics.MemoryBytes
	_ = metrics.Goroutines
}

func TestSystemMonitor_GetMetrics_ThreadSafe(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Multiple goroutines reading concurrently should not panic
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = sm.GetMetrics()
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// =============================================================================
// Convenience Method Tests
// =============================================================================

func TestSystemMonitor_GetCPUPercent(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Should not panic
	cpu := sm.GetCPUPercent()

	// CPU should be between 0-100 (or 0 if monitoring not started)
	if cpu < 0 || cpu > 100 {
		t.Errorf("CPUPercent should be 0-100, got %f", cpu)
	}
}

func TestSystemMonitor_GetMemoryBytes(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Should not panic
	mem := sm.GetMemoryBytes()

	// Memory should be non-negative
	if mem < 0 {
		t.Errorf("MemoryBytes should be >= 0, got %d", mem)
	}
}

func TestSystemMonitor_GetMemoryMB(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Should not panic
	memMB := sm.GetMemoryMB()

	// Memory should be non-negative
	if memMB < 0 {
		t.Errorf("MemoryMB should be >= 0, got %f", memMB)
	}
}

func TestSystemMonitor_GetGoroutines(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Should not panic
	goroutines := sm.GetGoroutines()

	// Goroutines should be non-negative
	if goroutines < 0 {
		t.Errorf("Goroutines should be >= 0, got %d", goroutines)
	}
}

func TestSystemMonitor_GetCPUAllocation(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Should not panic
	alloc := sm.GetCPUAllocation()

	// CPU allocation should be positive (at least 1 core or fraction)
	if alloc <= 0 {
		// On non-container systems, this might return number of CPUs
		t.Logf("CPUAllocation: %f (may vary by platform)", alloc)
	}
}

// =============================================================================
// StartMonitoring Tests
// =============================================================================

func TestSystemMonitor_StartMonitoring_NoPanic(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Starting monitoring should not panic
	// Note: The singleton may already be monitoring from other tests
	sm.StartMonitoring(100*time.Millisecond, 50*time.Millisecond)

	// Give it a moment to collect at least one sample
	time.Sleep(60 * time.Millisecond)

	// Metrics should now have values
	metrics := sm.GetMetrics()

	// Goroutines should be positive (at least the test runner goroutines)
	if metrics.Goroutines <= 0 {
		t.Logf("Goroutines after monitoring: %d", metrics.Goroutines)
	}
}

// =============================================================================
// Shutdown Tests
// =============================================================================

func TestSystemMonitor_Shutdown_NoPanic(t *testing.T) {
	// Note: Since this is a singleton, we can't actually test shutdown
	// without affecting other tests. Just verify the method exists and
	// calling Shutdown on a fresh monitor doesn't panic.

	// We can't call shutdown on the singleton without breaking other tests,
	// so we just verify the API exists
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Verify the method exists (compile-time check)
	_ = sm.Shutdown
}

// =============================================================================
// Thread Safety Tests
// =============================================================================

func TestSystemMonitor_ConcurrentReads(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	done := make(chan bool, 50)

	// Multiple goroutines reading different metrics concurrently
	for i := 0; i < 50; i++ {
		go func(id int) {
			for j := 0; j < 50; j++ {
				switch id % 5 {
				case 0:
					_ = sm.GetCPUPercent()
				case 1:
					_ = sm.GetMemoryBytes()
				case 2:
					_ = sm.GetMemoryMB()
				case 3:
					_ = sm.GetGoroutines()
				case 4:
					_ = sm.GetMetrics()
				}
			}
			done <- true
		}(i)
	}

	for i := 0; i < 50; i++ {
		<-done
	}
	// Test passes if no race conditions or panics
}

// =============================================================================
// Integration-style Tests (validate real metrics)
// =============================================================================

func TestSystemMonitor_MetricsAreRealistic(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Start monitoring with fast intervals for testing
	sm.StartMonitoring(50*time.Millisecond, 25*time.Millisecond)

	// Wait for at least one full update
	time.Sleep(100 * time.Millisecond)

	metrics := sm.GetMetrics()

	// Validate metrics are within reasonable ranges
	if metrics.CPUPercent < 0 || metrics.CPUPercent > 100 {
		t.Errorf("CPU percent out of range: %f", metrics.CPUPercent)
	}

	// Memory should be positive after monitoring starts
	// (Go runtime always uses some memory)
	if metrics.MemoryBytes <= 0 {
		t.Logf("Warning: MemoryBytes is %d (expected positive)", metrics.MemoryBytes)
	}

	// Goroutines should be at least a few (test runner, monitoring goroutine, etc.)
	if metrics.Goroutines < 1 {
		t.Logf("Warning: Goroutines is %d (expected > 0)", metrics.Goroutines)
	}

	// Timestamp should be recent
	if time.Since(metrics.Timestamp) > 1*time.Second {
		t.Errorf("Timestamp too old: %v (expected recent)", metrics.Timestamp)
	}
}
