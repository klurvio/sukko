package shared

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// HTTP handlers for health and stats endpoints
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Get current metrics
	s.stats.Mu.RLock()
	cpuPercent := s.stats.CPUPercent
	memoryMB := s.stats.MemoryMB
	s.stats.Mu.RUnlock()

	currentConns := atomic.LoadInt64(&s.stats.CurrentConnections)
	maxConns := int64(s.config.MaxConnections) // Static configuration
	slowClients := atomic.LoadInt64(&s.stats.SlowClientsDisconnected)
	totalConns := atomic.LoadInt64(&s.stats.TotalConnections)

	// Get ResourceGuard stats
	resourceStats := s.resourceGuard.GetStats()

	// Use actual configured limits
	memLimitMB := float64(s.config.MemoryLimit) / 1024.0 / 1024.0 // Convert bytes to MB
	goroutinesCurrent := resourceStats["goroutines_current"].(int)
	goroutinesLimit := s.config.MaxGoroutines

	// Calculate health status
	// Logic: if resource > configured limit, set unhealthy
	isHealthy := true
	warnings := []string{}
	errors := []string{}

	// Check Kafka consumer (critical dependency)
	kafkaStatus := "stopped"
	kafkaHealthy := false
	if s.kafkaConsumer != nil {
		kafkaStatus = "running"
		kafkaHealthy = true
	} else {
		isHealthy = false
		errors = append(errors, "Kafka consumer not initialized")
		s.logger.Error().Msg("Health check failed: Kafka consumer not initialized")
	}

	// Check CPU (against configured reject threshold)
	cpuHealthy := true
	if cpuPercent > s.config.CPURejectThreshold {
		isHealthy = false
		cpuHealthy = false
		errors = append(errors, fmt.Sprintf("CPU exceeds reject threshold (%.1f%% > %.1f%%)", cpuPercent, s.config.CPURejectThreshold))
		s.logger.Error().
			Float64("cpu_percent", cpuPercent).
			Float64("cpu_threshold", s.config.CPURejectThreshold).
			Msg("Health check failed: CPU exceeds threshold")
	}

	// Check memory (against configured limit)
	memPercent := (memoryMB / memLimitMB) * 100
	memHealthy := true
	if memoryMB > memLimitMB {
		isHealthy = false
		memHealthy = false
		errors = append(errors, fmt.Sprintf("Memory exceeds limit (%.1fMB > %.1fMB)", memoryMB, memLimitMB))
		s.logger.Error().
			Float64("used_mb", memoryMB).
			Float64("limit_mb", memLimitMB).
			Msg("Health check failed: Memory exceeds limit")
	}

	// Check goroutines (against configured limit)
	goroutinesPercent := float64(goroutinesCurrent) / float64(goroutinesLimit) * 100
	goroutinesHealthy := true
	if goroutinesCurrent > goroutinesLimit {
		isHealthy = false
		goroutinesHealthy = false
		errors = append(errors, fmt.Sprintf("Goroutines exceed limit (%d > %d)", goroutinesCurrent, goroutinesLimit))
		s.logger.Error().
			Int("current", goroutinesCurrent).
			Int("limit", goroutinesLimit).
			Msg("Health check failed: Goroutines exceed limit")
	}

	// Check capacity (NOT a failure condition - server operates within design limits at 100%)
	capacityPercent := float64(currentConns) / float64(maxConns) * 100
	capacityHealthy := true
	if capacityPercent > 100 {
		// If it comes here, it means the server is over loaded and the limit check is failing
		capacityHealthy = false
		errors = append(errors, fmt.Sprintf("Server at over capacity (%d/%d)", currentConns, maxConns))
		s.logger.Error().
			Int64("current", currentConns).
			Int64("max", maxConns).
			Msg("Health check failed: Server at over capacity")
	} else if capacityPercent == 100 {
		capacityHealthy = true
		warnings = append(warnings, fmt.Sprintf("Server at full capacity (%d/%d)", currentConns, maxConns))
		s.logger.Warn().
			Int64("current", currentConns).
			Int64("max", maxConns).
			Msg("Server at full capacity - consider scaling")
	} else if capacityPercent > 90 && capacityPercent < 100 {
		capacityHealthy = true
		warnings = append(warnings, fmt.Sprintf("Server near capacity (%.1f%%)", capacityPercent))
	}

	// Check slow client rate
	if totalConns > 0 {
		slowClientPercent := float64(slowClients) / float64(totalConns) * 100
		if slowClientPercent > 10 {
			warnings = append(warnings, "High slow client disconnect rate (>10%)")
		}
	}

	// Determine overall status
	status := "healthy"
	statusCode := http.StatusOK
	if !isHealthy {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	} else if len(warnings) > 0 {
		status = "degraded"
		statusCode = http.StatusOK // Still accepting traffic
	}

	// Build response
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":  status,
		"healthy": isHealthy,
		"checks": map[string]any{
			"kafka": map[string]any{
				"status":  kafkaStatus,
				"healthy": kafkaHealthy,
			},
			"capacity": map[string]any{
				"current":    currentConns,
				"max":        maxConns,
				"percentage": capacityPercent,
				"healthy":    capacityHealthy,
			},
			"goroutines": map[string]any{
				"current":    goroutinesCurrent,
				"limit":      goroutinesLimit,
				"percentage": goroutinesPercent,
				"healthy":    goroutinesHealthy,
			},
			"memory": map[string]any{
				"used_mb":    memoryMB,
				"limit_mb":   memLimitMB,
				"percentage": memPercent,
				"healthy":    memHealthy,
			},
			"cpu": map[string]any{
				"percentage": cpuPercent,
				"threshold":  s.config.CPURejectThreshold,
				"healthy":    cpuHealthy,
			},
			"limits": map[string]any{
				"max_connections":  s.config.MaxConnections,
				"max_goroutines":   s.config.MaxGoroutines,
				"cpu_threshold":    s.config.CPURejectThreshold,
				"memory_limit_mb":  memLimitMB,
				"kafka_rate":       resourceStats["kafka_rate_limit"],
				"broadcast_rate":   resourceStats["broadcast_rate_limit"],
				"worker_pool_size": resourceStats["worker_pool_size"],
			},
		},
		"observability": s.getObservabilityStats(),
		"warnings":      warnings,
		"errors":        errors,
		"uptime":        time.Since(s.stats.StartTime).Seconds(),
	}); err != nil {
		// Can't do much since WriteHeader already called, just log for debugging
		s.logger.Debug().Err(err).Msg("Failed to encode health response")
	}
}

// getObservabilityStats returns Phase 2 observability metrics for /health endpoint
func (s *Server) getObservabilityStats() map[string]any {
	// Get disconnect stats
	s.stats.DisconnectsMu.RLock()
	disconnects := make(map[string]int64)
	totalDisconnects := int64(0)
	for reason, count := range s.stats.DisconnectsByReason {
		disconnects[reason] = count
		totalDisconnects += count
	}
	s.stats.DisconnectsMu.RUnlock()

	// Get dropped broadcast stats
	s.stats.DropsMu.RLock()
	droppedBroadcasts := make(map[string]int64)
	totalDropped := int64(0)
	for channel, count := range s.stats.DroppedBroadcastsByChannel {
		droppedBroadcasts[channel] = count
		totalDropped += count
	}
	s.stats.DropsMu.RUnlock()

	// Calculate buffer saturation statistics
	s.stats.BuffersMu.RLock()
	bufferStats := calculateBufferStats(s.stats.BufferSaturationSamples)
	s.stats.BuffersMu.RUnlock()

	return map[string]any{
		"disconnects": map[string]any{
			"total":     totalDisconnects,
			"by_reason": disconnects,
		},
		"dropped_broadcasts": map[string]any{
			"total":      totalDropped,
			"by_channel": droppedBroadcasts,
		},
		"buffer_saturation": bufferStats,
	}
}

// calculateBufferStats computes statistics from buffer saturation samples
func calculateBufferStats(samples []int) map[string]any {
	if len(samples) == 0 {
		return map[string]any{
			"samples": 0,
			"avg":     0,
			"p50":     0,
			"p95":     0,
			"p99":     0,
			"max":     0,
		}
	}

	// Sort for percentile calculation
	sorted := make([]int, len(samples))
	copy(sorted, samples)

	// Simple bubble sort for small arrays (max 100 elements)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate average
	sum := 0
	max := 0
	for _, v := range sorted {
		sum += v
		if v > max {
			max = v
		}
	}
	avg := float64(sum) / float64(len(sorted))

	// Calculate percentiles
	p50idx := int(float64(len(sorted)) * 0.50)
	p95idx := int(float64(len(sorted)) * 0.95)
	p99idx := int(float64(len(sorted)) * 0.99)

	return map[string]any{
		"samples": len(samples),
		"avg":     avg,
		"p50":     sorted[p50idx],
		"p95":     sorted[p95idx],
		"p99":     sorted[p99idx],
		"max":     max,
	}
}

// handleKafkaReconnect handles client reconnection using Kafka offset tracking
// This replaces the old in-memory replay buffer with proper Kafka-based message replay
//
// Protocol:
//
//	Client sends: {"type": "reconnect", "data": {"client_id": "abc123", "last_offset": {"odin.trades": 12345}}}
//	Server creates temporary Kafka consumer starting at last_offset per topic
//	Server reads missed messages and sends to client
//	Client catches up, resumes normal message flow
//
// Benefits over in-memory buffer:
//   - 7 days of history (vs 40 seconds with in-memory buffer)
//   - Survives server restarts (offsets in Kafka, not RAM)
//   - Zero RAM overhead per client (no duplicate storage)
//   - Scales horizontally (any server can replay from Kafka)
