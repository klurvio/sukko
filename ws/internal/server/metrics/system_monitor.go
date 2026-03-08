package metrics

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/platform"
)

// SystemMonitor is a singleton that centralizes system resource monitoring.
// It eliminates duplicate CPU/memory measurements across multiple ResourceGuards.
var (
	systemMonitorInstance *SystemMonitor
	systemMonitorOnce     sync.Once
)

// DefaultEWMABeta is the EWMA decay factor for CPU smoothing.
// At 1-second sample intervals, beta=0.8 gives an effective ~5-second window
// (1/(1-beta) = 5 samples), matching go-zero's production tuning.
// Higher values (0.9 = ~10s window) are more conservative but slower to react.
const DefaultEWMABeta = 0.8

// SystemMetrics holds current system resource measurements.
type SystemMetrics struct {
	CPUPercent    float64                // Current CPU usage percentage (container-aware)
	CPUSmoothed   float64                // EWMA-smoothed CPU percentage (for load-shedding decisions)
	MemoryBytes   int64                  // Current memory usage in bytes
	MemoryMB      float64                // Current memory usage in MB
	Goroutines    int                    // Current goroutine count
	CPUAllocation float64                // CPU allocation (cores) from container limits
	ThrottleStats platform.ThrottleStats // CPU throttling statistics
	Timestamp     time.Time              // When these metrics were captured
}

// SystemMonitor centralizes system resource monitoring.
type SystemMonitor struct {
	cpuMonitor *platform.CPUMonitor
	logger     zerolog.Logger

	// Current metrics (protected by mutex)
	mu      sync.RWMutex
	metrics SystemMetrics

	// EWMA state
	ewmaBeta       float64 // Decay factor (0-1), higher = smoother
	ewmaInitialized bool   // Whether first sample has been received

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// GetSystemMonitor returns the singleton SystemMonitor instance.
// First call initializes the monitor with the provided logger.
func GetSystemMonitor(logger zerolog.Logger) *SystemMonitor {
	systemMonitorOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())

		systemMonitorInstance = &SystemMonitor{
			cpuMonitor: platform.NewCPUMonitor(logger),
			logger:     logger.With().Str("component", "system_monitor").Logger(),
			ewmaBeta:   DefaultEWMABeta,
			ctx:        ctx,
			cancel:     cancel,
		}

		// Initialize metrics with zero values
		systemMonitorInstance.metrics = SystemMetrics{
			Timestamp: time.Now(),
		}

		// Set cgroup memory limit once at startup
		memLimit, err := platform.GetMemoryLimit()
		if err == nil && memLimit > 0 {
			memoryLimitBytes.Set(float64(memLimit))
		}

		logger.Info().
			Str("cpu_mode", systemMonitorInstance.cpuMonitor.Mode()).
			Float64("cpu_allocation", systemMonitorInstance.cpuMonitor.GetAllocation()).
			Msg("SystemMonitor singleton initialized")
	})

	return systemMonitorInstance
}

// SetEWMABeta configures the EWMA decay factor for CPU smoothing.
// Must be called before StartMonitoring. Values closer to 1.0 produce
// smoother (slower-reacting) output; values closer to 0.0 track raw CPU more closely.
// Beta must be in the open interval (0, 1). Invalid values are rejected with a warning.
func (sm *SystemMonitor) SetEWMABeta(beta float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if beta <= 0 || beta >= 1 {
		sm.logger.Warn().
			Float64("ewma_beta", beta).
			Float64("current_beta", sm.ewmaBeta).
			Msg("Invalid EWMA beta (must be 0 < beta < 1), keeping current value")
		return
	}
	sm.ewmaBeta = beta
	sm.logger.Info().
		Float64("ewma_beta", beta).
		Float64("effective_window_samples", 1/(1-beta)).
		Msg("CPU EWMA smoothing configured")
}

// StartMonitoring begins periodic system metric updates with two tickers:
// - cpuPollInterval: Fast CPU-only updates for protection decisions (default: 1s)
// - metricsInterval: Full metrics updates including memory, goroutines (default: 15s)
func (sm *SystemMonitor) StartMonitoring(metricsInterval, cpuPollInterval time.Duration) {
	sm.wg.Go(func() {
		defer logging.RecoverPanic(sm.logger, "SystemMonitor", nil)

		// Two tickers: fast for CPU, slow for full metrics
		cpuTicker := time.NewTicker(cpuPollInterval)
		metricsTicker := time.NewTicker(metricsInterval)
		defer cpuTicker.Stop()
		defer metricsTicker.Stop()

		sm.logger.Info().
			Dur("metrics_interval", metricsInterval).
			Dur("cpu_poll_interval", cpuPollInterval).
			Msg("SystemMonitor started with two-ticker approach")

		// Initial full update
		sm.updateMetrics()

		for {
			select {
			case <-cpuTicker.C:
				sm.updateCPUOnly()

			case <-metricsTicker.C:
				sm.updateMetrics()

			case <-sm.ctx.Done():
				sm.logger.Info().Msg("SystemMonitor stopped")
				return
			}
		}
	})
}

// updateCPUOnly performs a fast CPU-only measurement.
func (sm *SystemMonitor) updateCPUOnly() {
	cpuPercent, throttleStats, err := sm.cpuMonitor.GetPercent()
	if err != nil {
		// Graceful degradation: keep the previous CPU reading rather than
		// reporting zero, which would incorrectly release load-shedding.
		cpuPercent = sm.metrics.CPUPercent
	}

	sm.mu.Lock()
	sm.metrics.CPUPercent = cpuPercent
	sm.metrics.CPUSmoothed = sm.computeEWMA(cpuPercent)
	sm.metrics.ThrottleStats = throttleStats
	sm.metrics.Timestamp = time.Now()
	smoothed := sm.metrics.CPUSmoothed
	sm.mu.Unlock()

	// Update Prometheus CPU metrics
	CPUUsagePercent.Set(cpuPercent)
	CPUContainerPercent.Set(cpuPercent)
	CPUSmoothedPercent.Set(smoothed)

	if throttleStats.NrThrottled > 0 {
		CPUThrottleEventsTotal.Add(float64(throttleStats.NrThrottled))
	}
	if throttleStats.ThrottledSec > 0 {
		CPUThrottledSecondsTotal.Add(throttleStats.ThrottledSec)
	}
}

// updateMetrics performs a full measurement of all system resources.
func (sm *SystemMonitor) updateMetrics() {
	cpuPercent, throttleStats, err := sm.cpuMonitor.GetPercent()
	if err != nil {
		sm.logger.Error().Err(err).Msg("Failed to get CPU usage")
		cpuPercent = 0
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	goroutines := runtime.NumGoroutine()

	sm.mu.Lock()
	smoothed := sm.computeEWMA(cpuPercent)
	sm.metrics = SystemMetrics{
		CPUPercent:    cpuPercent,
		CPUSmoothed:   smoothed,
		MemoryBytes:   int64(mem.Alloc), //nolint:gosec // Memory allocation fits in int64
		MemoryMB:      float64(mem.Alloc) / (1024 * 1024),
		Goroutines:    goroutines,
		CPUAllocation: sm.cpuMonitor.GetAllocation(),
		ThrottleStats: throttleStats,
		Timestamp:     time.Now(),
	}
	sm.mu.Unlock()

	// Update Prometheus metrics
	CPUUsagePercent.Set(cpuPercent)
	CPUContainerPercent.Set(cpuPercent)
	CPUSmoothedPercent.Set(smoothed)
	CPUAllocationCores.Set(sm.cpuMonitor.GetAllocation())
	memoryUsageBytes.Set(float64(mem.Alloc))
	goroutinesActive.Set(float64(goroutines))

	if hostCPU, err := sm.cpuMonitor.GetHostPercent(); err == nil {
		CPUHostPercent.Set(hostCPU)
	}

	if throttleStats.NrThrottled > 0 {
		CPUThrottleEventsTotal.Add(float64(throttleStats.NrThrottled))
	}
	if throttleStats.ThrottledSec > 0 {
		CPUThrottledSecondsTotal.Add(throttleStats.ThrottledSec)
	}

	sm.logger.Debug().
		Float64("cpu_percent", cpuPercent).
		Uint64("cpu_throttled_events", throttleStats.NrThrottled).
		Float64("cpu_throttled_sec", throttleStats.ThrottledSec).
		Float64("memory_mb", sm.metrics.MemoryMB).
		Int("goroutines", goroutines).
		Msg("System metrics updated")
}

// GetMetrics returns a copy of the current system metrics.
func (sm *SystemMonitor) GetMetrics() SystemMetrics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics
}

// GetCPUPercent returns the current raw (unsmoothed) CPU usage percentage.
func (sm *SystemMonitor) GetCPUPercent() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics.CPUPercent
}

// GetSmoothedCPUPercent returns the EWMA-smoothed CPU percentage.
// This should be used for load-shedding decisions (ResourceGuard) to avoid
// false triggers from transient CPU spikes.
func (sm *SystemMonitor) GetSmoothedCPUPercent() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics.CPUSmoothed
}

// computeEWMA calculates the EWMA-smoothed CPU value.
// Must be called with sm.mu held.
func (sm *SystemMonitor) computeEWMA(rawCPU float64) float64 {
	if !sm.ewmaInitialized {
		sm.ewmaInitialized = true
		return rawCPU
	}
	return sm.metrics.CPUSmoothed*sm.ewmaBeta + rawCPU*(1-sm.ewmaBeta)
}

// GetMemoryBytes returns the current memory usage in bytes.
func (sm *SystemMonitor) GetMemoryBytes() int64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics.MemoryBytes
}

// GetMemoryMB returns the current memory usage in megabytes.
func (sm *SystemMonitor) GetMemoryMB() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics.MemoryMB
}

// GetGoroutines returns the current goroutine count.
func (sm *SystemMonitor) GetGoroutines() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics.Goroutines
}

// GetCPUAllocation returns the CPU allocation (cores) from container limits.
func (sm *SystemMonitor) GetCPUAllocation() float64 {
	return sm.cpuMonitor.GetAllocation()
}

// Shutdown gracefully stops the SystemMonitor.
func (sm *SystemMonitor) Shutdown() {
	sm.logger.Info().Msg("Shutting down SystemMonitor")
	sm.cancel()
	sm.wg.Wait()
	sm.logger.Info().Msg("SystemMonitor shut down")
}
