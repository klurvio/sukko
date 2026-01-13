// Package testutil provides test fixtures, helpers, and mock implementations
// for unit testing WebSocket server components. It includes safe default
// configurations, mock clients, and assertion helpers.
package testutil

import (
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/types"
)

// NewTestServerConfig creates a ServerConfig with safe defaults for testing.
// All limits are set high to avoid triggering protection mechanisms unless explicitly tested.
func NewTestServerConfig() types.ServerConfig {
	return types.ServerConfig{
		Addr:           "localhost:0", // Random available port
		MaxConnections: 1000,

		// Resource limits (generous for testing)
		MemoryLimit:   4 * 1024 * 1024 * 1024, // 4GB
		MaxGoroutines: 10000,

		// Rate limiting (high limits for testing)
		MaxKafkaMessagesPerSec: 10000,
		MaxBroadcastsPerSec:    10000,

		// Connection rate limiting (disabled by default for tests)
		ConnectionRateLimitEnabled: false,
		ConnRateLimitIPBurst:       100,
		ConnRateLimitIPRate:        10.0,
		ConnRateLimitGlobalBurst:   1000,
		ConnRateLimitGlobalRate:    100.0,

		// CPU thresholds (high to avoid triggering in normal tests)
		CPURejectThreshold:      95.0,
		CPURejectThresholdLower: 90.0,
		CPUPauseThreshold:       98.0,
		CPUPauseThresholdLower:  95.0,

		// Client buffer
		ClientSendBufferSize: 512,

		// Timeouts
		HTTPReadTimeout:  30 * time.Second,
		HTTPWriteTimeout: 30 * time.Second,
		HTTPIdleTimeout:  120 * time.Second,

		// Monitoring
		MetricsInterval: 15 * time.Second,
		CPUPollInterval: 1 * time.Second,

		// Logging (quiet for tests)
		LogLevel:  types.LogLevelError,
		LogFormat: types.LogFormatJSON,
	}
}

// NewTestServerConfigWithLowLimits creates a config for testing protection mechanisms.
// Rate limits and thresholds are set low to easily trigger them.
func NewTestServerConfigWithLowLimits() types.ServerConfig {
	cfg := NewTestServerConfig()
	cfg.MaxConnections = 10
	cfg.MaxKafkaMessagesPerSec = 5
	cfg.MaxBroadcastsPerSec = 5
	cfg.MaxGoroutines = 100
	cfg.CPURejectThreshold = 50.0
	cfg.CPURejectThresholdLower = 40.0
	cfg.CPUPauseThreshold = 60.0
	cfg.CPUPauseThresholdLower = 50.0
	return cfg
}

// TokenEventFixture represents a sample Kafka message for testing.
type TokenEventFixture struct {
	TokenID   string
	EventType string
	Data      map[string]any
	JSON      []byte
}

// NewTokenEventFixture creates a sample token event.
func NewTokenEventFixture(tokenID, eventType string) TokenEventFixture {
	data := map[string]any{
		"tokenId":   tokenID,
		"price":     "100.50",
		"volume":    "1000000",
		"timestamp": time.Now().UnixMilli(),
	}

	// Pre-serialized JSON for common cases
	json := []byte(`{"tokenId":"` + tokenID + `","price":"100.50","volume":"1000000"}`)

	return TokenEventFixture{
		TokenID:   tokenID,
		EventType: eventType,
		Data:      data,
		JSON:      json,
	}
}

// SampleTokenEvents returns a slice of common token events for testing.
func SampleTokenEvents() []TokenEventFixture {
	return []TokenEventFixture{
		NewTokenEventFixture("BTC", "trade"),
		NewTokenEventFixture("ETH", "trade"),
		NewTokenEventFixture("SOL", "trade"),
		NewTokenEventFixture("BTC", "orderbook"),
		NewTokenEventFixture("ETH", "orderbook"),
	}
}

// SampleChannels returns common channel names for testing subscriptions.
func SampleChannels() []string {
	return []string{
		"BTC.trade",
		"ETH.trade",
		"SOL.trade",
		"BTC.orderbook",
		"ETH.orderbook",
		"DOGE.trade",
	}
}

// SampleSubjects returns common broadcast subjects for testing.
// The broadcast subject IS the channel in the new format.
func SampleSubjects() []string {
	return []string{
		"BTC.trade",
		"ETH.trade",
		"SOL.trade",
		"BTC.orderbook",
		"ETH.orderbook",
	}
}
