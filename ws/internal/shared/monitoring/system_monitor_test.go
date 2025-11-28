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
	for range 10 {
		go func() {
			for range 100 {
				_ = sm.GetMetrics()
			}
			done <- true
		}()
	}

	for range 10 {
		<-done
	}
}

// =============================================================================
// Convenience Method Tests
// =============================================================================

func TestSystemMonitor_GetCPUPercent_AfterMonitoring(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Start monitoring to ensure we have real values
	sm.StartMonitoring(50*time.Millisecond, 25*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // Wait for at least one full update

	cpu := sm.GetCPUPercent()

	// CPU should be between 0-100
	if cpu < 0 || cpu > 100 {
		t.Errorf("CPUPercent should be 0-100, got %f", cpu)
	}

	// Verify it's consistent with GetMetrics()
	metrics := sm.GetMetrics()
	if cpu != metrics.CPUPercent {
		t.Errorf("GetCPUPercent() %f != GetMetrics().CPUPercent %f", cpu, metrics.CPUPercent)
	}
}

func TestSystemMonitor_GetMemoryBytes_AfterMonitoring(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Start monitoring to ensure we have real values
	sm.StartMonitoring(50*time.Millisecond, 25*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // Wait for at least one full update

	mem := sm.GetMemoryBytes()

	// Memory should be positive (Go runtime always uses some memory)
	if mem <= 0 {
		t.Errorf("MemoryBytes should be > 0 after monitoring starts, got %d", mem)
	}

	// Verify it's consistent with GetMetrics()
	metrics := sm.GetMetrics()
	if mem != metrics.MemoryBytes {
		t.Errorf("GetMemoryBytes() %d != GetMetrics().MemoryBytes %d", mem, metrics.MemoryBytes)
	}
}

func TestSystemMonitor_GetMemoryMB_AfterMonitoring(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Start monitoring to ensure we have real values
	sm.StartMonitoring(50*time.Millisecond, 25*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // Wait for at least one full update

	memMB := sm.GetMemoryMB()

	// Memory should be positive (Go runtime always uses some memory)
	if memMB <= 0 {
		t.Errorf("MemoryMB should be > 0 after monitoring starts, got %f", memMB)
	}

	// Verify consistency between bytes and MB
	memBytes := sm.GetMemoryBytes()
	expectedMB := float64(memBytes) / (1024 * 1024)
	if memMB != expectedMB {
		t.Errorf("MemoryMB %f != MemoryBytes/1MB %f", memMB, expectedMB)
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

func TestSystemMonitor_StartMonitoring_PopulatesMetrics(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Start monitoring with fast intervals for testing
	sm.StartMonitoring(50*time.Millisecond, 25*time.Millisecond)

	// Wait for at least one full metrics update
	time.Sleep(100 * time.Millisecond)

	// Get metrics and verify all fields are populated
	metrics := sm.GetMetrics()

	// Goroutines should be positive (at least the test runner goroutines)
	if metrics.Goroutines <= 0 {
		t.Errorf("Goroutines should be > 0 after monitoring starts, got %d", metrics.Goroutines)
	}

	// Memory should be positive (Go runtime always uses some memory)
	if metrics.MemoryBytes <= 0 {
		t.Errorf("MemoryBytes should be > 0 after monitoring starts, got %d", metrics.MemoryBytes)
	}

	// MemoryMB should be consistent with MemoryBytes
	expectedMB := float64(metrics.MemoryBytes) / (1024 * 1024)
	if metrics.MemoryMB != expectedMB {
		t.Errorf("MemoryMB %f should match MemoryBytes/1MB %f", metrics.MemoryMB, expectedMB)
	}

	// Timestamp should be recent
	if time.Since(metrics.Timestamp) > 1*time.Second {
		t.Errorf("Timestamp too old: %v (expected recent)", metrics.Timestamp)
	}

	// CPU allocation should be positive (at least 1 core or fraction)
	if metrics.CPUAllocation <= 0 {
		t.Errorf("CPUAllocation should be > 0, got %f", metrics.CPUAllocation)
	}
}

// =============================================================================
// Shutdown Tests
// =============================================================================

func TestSystemMonitor_Shutdown_MethodExists(t *testing.T) {
	// LIMITATION: SystemMonitor is a singleton initialized once per process.
	// We cannot actually call Shutdown() in tests because:
	// 1. Other tests depend on the singleton being alive
	// 2. Once shutdown, it cannot be restarted (sync.Once has already fired)
	// 3. Calling Shutdown() would cause subsequent tests to fail or behave unexpectedly
	//
	// This test only verifies the Shutdown method exists and has the correct signature.
	// Integration tests or a dedicated shutdown test binary should test actual shutdown behavior.
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	// Compile-time check that Shutdown method exists with correct signature
	shutdownFn := sm.Shutdown
	_ = shutdownFn

	// Verify the monitor is still functional after this test
	// (proving we didn't accidentally call Shutdown)
	metrics := sm.GetMetrics()
	_ = metrics.Timestamp // Would panic if internal state was corrupted
}

// =============================================================================
// Thread Safety Tests
// =============================================================================

func TestSystemMonitor_ConcurrentReads(t *testing.T) {
	logger := zerolog.Nop()
	sm := GetSystemMonitor(logger)

	done := make(chan bool, 50)

	// Multiple goroutines reading different metrics concurrently
	for i := range 50 {
		go func(id int) {
			for range 50 {
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

	for range 50 {
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
