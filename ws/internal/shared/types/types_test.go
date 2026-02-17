package types //nolint:revive // test file for types package

import (
	"sync"
	"testing"
	"time"
)

// =============================================================================
// LogLevel Tests
// =============================================================================

func TestLogLevel_Constants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		level    LogLevel
		expected string
	}{
		{"debug", LogLevelDebug, "debug"},
		{"info", LogLevelInfo, "info"},
		{"warn", LogLevelWarn, "warn"},
		{"error", LogLevelError, "error"},
		{"fatal", LogLevelFatal, "fatal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.level) != tt.expected {
				t.Errorf("LogLevel %s = %q, want %q", tt.name, tt.level, tt.expected)
			}
		})
	}
}

func TestLogFormat_Constants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		format   LogFormat
		expected string
	}{
		{"json", LogFormatJSON, "json"},
		{"pretty", LogFormatPretty, "pretty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.format) != tt.expected {
				t.Errorf("LogFormat %s = %q, want %q", tt.name, tt.format, tt.expected)
			}
		})
	}
}

// =============================================================================
// ServerConfig Tests
// =============================================================================

func TestServerConfig_DefaultFields(t *testing.T) {
	t.Parallel()
	cfg := ServerConfig{
		Addr:           ":8080",
		KafkaBrokers:   []string{"kafka:9092"},
		MaxConnections: 10000,
	}

	if cfg.Addr != ":8080" {
		t.Errorf("Addr: got %s, want :8080", cfg.Addr)
	}
	if len(cfg.KafkaBrokers) != 1 || cfg.KafkaBrokers[0] != "kafka:9092" {
		t.Errorf("KafkaBrokers: got %v, want [kafka:9092]", cfg.KafkaBrokers)
	}
	if cfg.MaxConnections != 10000 {
		t.Errorf("MaxConnections: got %d, want 10000", cfg.MaxConnections)
	}
}

func TestServerConfig_ResourceLimits(t *testing.T) {
	t.Parallel()
	cfg := ServerConfig{
		MemoryLimit:            1024 * 1024 * 1024, // 1GB
		MaxKafkaMessagesPerSec: 1000,
		MaxBroadcastsPerSec:    100,
		MaxGoroutines:          10000,
	}

	if cfg.MemoryLimit != 1024*1024*1024 {
		t.Errorf("MemoryLimit: got %d, want 1073741824", cfg.MemoryLimit)
	}
	if cfg.MaxKafkaMessagesPerSec != 1000 {
		t.Errorf("MaxKafkaMessagesPerSec: got %d, want 1000", cfg.MaxKafkaMessagesPerSec)
	}
	if cfg.MaxBroadcastsPerSec != 100 {
		t.Errorf("MaxBroadcastsPerSec: got %d, want 100", cfg.MaxBroadcastsPerSec)
	}
	if cfg.MaxGoroutines != 10000 {
		t.Errorf("MaxGoroutines: got %d, want 10000", cfg.MaxGoroutines)
	}
}

func TestServerConfig_HysteresisThresholds(t *testing.T) {
	t.Parallel()
	cfg := ServerConfig{
		CPURejectThreshold:      75.0,
		CPURejectThresholdLower: 65.0,
		CPUPauseThreshold:       80.0,
		CPUPauseThresholdLower:  70.0,
	}

	// Verify hysteresis bands
	rejectBand := cfg.CPURejectThreshold - cfg.CPURejectThresholdLower
	if rejectBand != 10.0 {
		t.Errorf("Reject hysteresis band: got %f, want 10.0", rejectBand)
	}

	pauseBand := cfg.CPUPauseThreshold - cfg.CPUPauseThresholdLower
	if pauseBand != 10.0 {
		t.Errorf("Pause hysteresis band: got %f, want 10.0", pauseBand)
	}
}

func TestServerConfig_ConnectionRateLimiting(t *testing.T) {
	t.Parallel()
	cfg := ServerConfig{
		ConnectionRateLimitEnabled: true,
		ConnRateLimitIPBurst:       10,
		ConnRateLimitIPRate:        1.0,
		ConnRateLimitGlobalBurst:   300,
		ConnRateLimitGlobalRate:    50.0,
	}

	if !cfg.ConnectionRateLimitEnabled {
		t.Error("ConnectionRateLimitEnabled should be true")
	}
	if cfg.ConnRateLimitIPBurst != 10 {
		t.Errorf("ConnRateLimitIPBurst: got %d, want 10", cfg.ConnRateLimitIPBurst)
	}
	if cfg.ConnRateLimitIPRate != 1.0 {
		t.Errorf("ConnRateLimitIPRate: got %f, want 1.0", cfg.ConnRateLimitIPRate)
	}
}

func TestServerConfig_HTTPTimeouts(t *testing.T) {
	t.Parallel()
	cfg := ServerConfig{
		HTTPReadTimeout:  15 * time.Second,
		HTTPWriteTimeout: 15 * time.Second,
		HTTPIdleTimeout:  60 * time.Second,
	}

	if cfg.HTTPReadTimeout != 15*time.Second {
		t.Errorf("HTTPReadTimeout: got %v, want 15s", cfg.HTTPReadTimeout)
	}
	if cfg.HTTPWriteTimeout != 15*time.Second {
		t.Errorf("HTTPWriteTimeout: got %v, want 15s", cfg.HTTPWriteTimeout)
	}
	if cfg.HTTPIdleTimeout != 60*time.Second {
		t.Errorf("HTTPIdleTimeout: got %v, want 60s", cfg.HTTPIdleTimeout)
	}
}

// =============================================================================
// Stats Tests
// =============================================================================

func TestStats_ZeroValues(t *testing.T) {
	t.Parallel()
	stats := &Stats{}

	if stats.TotalConnections.Load() != 0 {
		t.Errorf("TotalConnections: got %d, want 0", stats.TotalConnections.Load())
	}
	if stats.CurrentConnections.Load() != 0 {
		t.Errorf("CurrentConnections: got %d, want 0", stats.CurrentConnections.Load())
	}
	if stats.MessagesSent.Load() != 0 {
		t.Errorf("MessagesSent: got %d, want 0", stats.MessagesSent.Load())
	}
}

func TestStats_Fields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	stats := &Stats{
		StartTime:  now,
		CPUPercent: 45.5,
		MemoryMB:   256.0,
	}
	stats.TotalConnections.Store(1000)
	stats.CurrentConnections.Store(500)
	stats.MessagesSent.Store(10000)
	stats.MessagesReceived.Store(5000)
	stats.BytesSent.Store(1024 * 1024)
	stats.BytesReceived.Store(512 * 1024)
	stats.SlowClientsDisconnected.Store(10)
	stats.RateLimitedMessages.Store(50)
	stats.MessageReplayRequests.Store(5)

	if stats.TotalConnections.Load() != 1000 {
		t.Errorf("TotalConnections: got %d, want 1000", stats.TotalConnections.Load())
	}
	if stats.CurrentConnections.Load() != 500 {
		t.Errorf("CurrentConnections: got %d, want 500", stats.CurrentConnections.Load())
	}
	if !stats.StartTime.Equal(now) {
		t.Errorf("StartTime mismatch")
	}
	if stats.CPUPercent != 45.5 {
		t.Errorf("CPUPercent: got %f, want 45.5", stats.CPUPercent)
	}
}

func TestStats_ConcurrentMapAccess(t *testing.T) {
	t.Parallel()
	stats := &Stats{
		DisconnectsByReason:        make(map[string]int64),
		DroppedBroadcastsByChannel: make(map[string]int64),
		BufferSaturationSamples:    make([]int, 0, 100),
	}

	var wg sync.WaitGroup

	// Concurrent writes to DisconnectsByReason
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 100 {
				reason := "reason" + string(rune('0'+(id%10)))
				stats.DisconnectsMu.Lock()
				stats.DisconnectsByReason[reason]++
				stats.DisconnectsMu.Unlock()
			}
		}(i)
	}

	// Concurrent writes to DroppedBroadcastsByChannel
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 100 {
				channel := "channel" + string(rune('0'+(id%10)))
				stats.DropsMu.Lock()
				stats.DroppedBroadcastsByChannel[channel]++
				stats.DropsMu.Unlock()
			}
		}(i)
	}

	// Concurrent writes to BufferSaturationSamples
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 10 {
				stats.BuffersMu.Lock()
				if len(stats.BufferSaturationSamples) >= 100 {
					stats.BufferSaturationSamples = stats.BufferSaturationSamples[1:]
				}
				stats.BufferSaturationSamples = append(stats.BufferSaturationSamples, id*j)
				stats.BuffersMu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify data was written correctly
	stats.DisconnectsMu.RLock()
	if len(stats.DisconnectsByReason) == 0 {
		t.Error("DisconnectsByReason should have entries")
	}
	stats.DisconnectsMu.RUnlock()

	stats.DropsMu.RLock()
	if len(stats.DroppedBroadcastsByChannel) == 0 {
		t.Error("DroppedBroadcastsByChannel should have entries")
	}
	stats.DropsMu.RUnlock()
}

func TestStats_DisconnectReasonTracking(t *testing.T) {
	t.Parallel()
	stats := &Stats{
		DisconnectsByReason: make(map[string]int64),
	}

	// Simulate tracking different disconnect reasons
	reasons := []string{"read_error", "write_timeout", "slow_client", "idle_timeout"}

	for i, reason := range reasons {
		stats.DisconnectsMu.Lock()
		stats.DisconnectsByReason[reason] = int64(i + 1)
		stats.DisconnectsMu.Unlock()
	}

	stats.DisconnectsMu.RLock()
	defer stats.DisconnectsMu.RUnlock()

	if len(stats.DisconnectsByReason) != 4 {
		t.Errorf("DisconnectsByReason count: got %d, want 4", len(stats.DisconnectsByReason))
	}

	if stats.DisconnectsByReason["read_error"] != 1 {
		t.Errorf("read_error count: got %d, want 1", stats.DisconnectsByReason["read_error"])
	}
}

func TestStats_BufferSaturationSampling(t *testing.T) {
	t.Parallel()
	stats := &Stats{
		BufferSaturationSamples: make([]int, 0, 100),
	}

	// Add samples
	for i := range 100 {
		stats.BuffersMu.Lock()
		stats.BufferSaturationSamples = append(stats.BufferSaturationSamples, i)
		stats.BuffersMu.Unlock()
	}

	stats.BuffersMu.RLock()
	if len(stats.BufferSaturationSamples) != 100 {
		t.Errorf("BufferSaturationSamples count: got %d, want 100", len(stats.BufferSaturationSamples))
	}
	stats.BuffersMu.RUnlock()
}
