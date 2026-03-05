package server

import (
	"os"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/klurvio/sukko/internal/server/metrics"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// Internal monitoring and metric collection
func (s *Server) collectMetrics() {
	// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
	defer logging.RecoverPanic(s.logger, "collectMetrics", nil)

	defer s.wg.Done()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Get current process for memory stats
	// NOTE: CPU is now measured by ResourceGuard via container-aware CPUMonitor
	// to provide single source of truth and avoid divergence
	proc, err := process.NewProcess(int32(os.Getpid())) //nolint:gosec // PID always fits in int32 on all supported platforms
	if err != nil {
		s.logger.Error().
			Err(err).
			Msg("Failed to get process info")
		proc = nil
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// CPU measurement REMOVED - now handled by ResourceGuard.UpdateResources()
			// This eliminates dual measurement paths and prevents divergence

			// Get memory usage
			if proc != nil {
				memInfo, err := proc.MemoryInfo()
				if err == nil {
					memMB := float64(memInfo.RSS) / 1024 / 1024
					s.stats.Mu.Lock()
					s.stats.MemoryMB = memMB
					s.stats.Mu.Unlock()
				}
			} else {
				// Fallback to system memory
				vmem, err := mem.VirtualMemory()
				if err == nil {
					memMB := float64(vmem.Used) / 1024 / 1024
					s.stats.Mu.Lock()
					s.stats.MemoryMB = memMB
					s.stats.Mu.Unlock()
				}
			}
		}
	}
}

func (s *Server) monitorMemory() {
	// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
	defer logging.RecoverPanic(s.logger, "monitorMemory", nil)

	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	memLimitMB := float64(s.config.MemoryLimit) / 1024.0 / 1024.0 // Container memory limit from config

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.stats.Mu.RLock()
			memUsedMB := s.stats.MemoryMB
			s.stats.Mu.RUnlock()

			memPercent := (memUsedMB / memLimitMB) * 100
			currentConns := s.stats.CurrentConnections.Load()

			// Critical threshold: >90%
			if memPercent > 90 {
				s.auditLogger.Critical("HighMemoryUsage", "Memory usage above 90%, OOM risk", map[string]any{
					"memory_used_mb":  memUsedMB,
					"memory_limit_mb": memLimitMB,
					"percentage":      memPercent,
					"connections":     currentConns,
				})
			} else if memPercent > 80 {
				// Warning threshold: >80%
				s.auditLogger.Warning("ModerateMemoryUsage", "Memory usage above 80%", map[string]any{
					"memory_used_mb":  memUsedMB,
					"memory_limit_mb": memLimitMB,
					"percentage":      memPercent,
					"connections":     currentConns,
				})
			}
		}
	}
}

// sampleClientBuffers periodically samples client send buffer usage for saturation metrics
// Samples a subset of clients to avoid expensive iteration over all connections
func (s *Server) sampleClientBuffers() {
	// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
	defer logging.RecoverPanic(s.logger, "sampleClientBuffers", nil)

	defer s.wg.Done()

	// Sample every 10 seconds (balance between granularity and overhead)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Sample up to 100 random clients to avoid iterating all connections
			// This gives statistically significant buffer saturation data without overhead
			samplesCollected := 0
			maxSamples := 100
			highSaturationCount := 0 // Track clients near capacity

			s.clients.Range(func(key, _ any) bool {
				if samplesCollected >= maxSamples {
					return false // Stop iteration
				}

				client, ok := key.(*Client)
				if !ok {
					return true // Skip invalid entry
				}

				// Record buffer usage (both Prometheus and Stats)
				bufferLen := len(client.send)
				bufferCap := cap(client.send)
				usagePercent := float64(bufferLen) / float64(bufferCap) * 100
				metrics.RecordClientBufferSizeWithStats(s.stats, bufferLen, bufferCap)

				// Phase 4: Track high saturation clients
				if usagePercent >= 90 {
					highSaturationCount++
				}

				samplesCollected++
				return true // Continue iteration
			})

			// Phase 4: Log warning if many clients are near buffer capacity
			if samplesCollected > 0 {
				highSaturationPercent := float64(highSaturationCount) / float64(samplesCollected) * 100
				if highSaturationPercent >= 25 {
					// Warning: 25%+ of sampled clients are at >90% buffer capacity
					s.logger.Warn().
						Int("high_saturation_count", highSaturationCount).
						Int("total_sampled", samplesCollected).
						Float64("high_saturation_pct", highSaturationPercent).
						Int64("current_connections", s.stats.CurrentConnections.Load()).
						Msg("High buffer saturation detected across client population")

					s.auditLogger.Warning("HighBufferSaturation", "Many clients near buffer capacity", map[string]any{
						"high_saturation_count": highSaturationCount,
						"total_sampled":         samplesCollected,
						"saturation_percent":    highSaturationPercent,
						"current_connections":   s.stats.CurrentConnections.Load(),
					})
				}
			}
		}
	}
}
