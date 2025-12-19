package broadcast

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNewBus_InvalidType(t *testing.T) {
	logger := zerolog.Nop()

	cfg := Config{
		Type: "invalid",
	}

	_, err := NewBus(cfg, logger)
	if err == nil {
		t.Error("Expected error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported broadcast bus type") {
		t.Errorf("Error should mention unsupported type: %v", err)
	}
}

func TestNewBus_DefaultsApplied(t *testing.T) {
	// Test that defaults are applied when values are zero
	cfg := Config{
		Type:            "valkey",
		BufferSize:      0, // Should default to 1024
		ShutdownTimeout: 0, // Should default to 5s
		Valkey: ValkeyConfig{
			Addrs: []string{"localhost:6379"},
		},
	}

	// We can't actually create the bus without a real Valkey connection,
	// but we can verify the factory accepts the config
	logger := zerolog.Nop()
	_, err := NewBus(cfg, logger)

	// Will fail because no Valkey server, but error should be connection-related
	if err == nil {
		t.Skip("Valkey server available, skipping connection error test")
	}

	// Error should be about connection, not about missing buffer size
	if strings.Contains(err.Error(), "buffer") {
		t.Errorf("Should not error about buffer size: %v", err)
	}
}

func TestNewBus_ValkeyType(t *testing.T) {
	tests := []struct {
		name     string
		busType  string
		wantType string
	}{
		{"valkey", "valkey", "valkey"},
		{"redis", "redis", "valkey"}, // redis is alias for valkey
		{"empty", "", "valkey"},      // empty defaults to valkey
	}

	logger := zerolog.Nop()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Type:       tt.busType,
				BufferSize: 1024,
				Valkey: ValkeyConfig{
					Addrs:   []string{"localhost:6379"},
					Channel: "test",
				},
			}

			_, err := NewBus(cfg, logger)
			// Will fail due to no connection, but should attempt Valkey
			if err != nil && strings.Contains(err.Error(), "unsupported") {
				t.Errorf("Type %q should be supported: %v", tt.busType, err)
			}
		})
	}
}

func TestNewBus_NATSType(t *testing.T) {
	logger := zerolog.Nop()

	cfg := Config{
		Type:       "nats",
		BufferSize: 1024,
		NATS: NATSConfig{
			URLs:    []string{"nats://localhost:4222"},
			Subject: "test",
		},
	}

	_, err := NewBus(cfg, logger)
	// Will fail due to no connection, but should attempt NATS
	if err != nil && strings.Contains(err.Error(), "unsupported") {
		t.Errorf("NATS type should be supported: %v", err)
	}
}

func TestNewBus_ValkeyMissingAddrs(t *testing.T) {
	logger := zerolog.Nop()

	cfg := Config{
		Type:       "valkey",
		BufferSize: 1024,
		Valkey: ValkeyConfig{
			Addrs: []string{}, // Empty
		},
	}

	_, err := NewBus(cfg, logger)
	if err == nil {
		t.Error("Expected error for missing Valkey addrs")
	}
	if !strings.Contains(err.Error(), "address") {
		t.Errorf("Error should mention address requirement: %v", err)
	}
}

func TestNewBus_NATSMissingURLs(t *testing.T) {
	logger := zerolog.Nop()

	cfg := Config{
		Type:       "nats",
		BufferSize: 1024,
		NATS: NATSConfig{
			URLs: []string{}, // Empty
		},
	}

	_, err := NewBus(cfg, logger)
	if err == nil {
		t.Error("Expected error for missing NATS URLs")
	}
	if !strings.Contains(err.Error(), "URL") {
		t.Errorf("Error should mention URL requirement: %v", err)
	}
}

func TestConfig_Defaults(t *testing.T) {
	// Verify that DefaultConfig returns sensible values
	cfg := DefaultConfig()

	if cfg.BufferSize != 1024 {
		t.Errorf("BufferSize: got %d, want 1024", cfg.BufferSize)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout: got %v, want 5s", cfg.ShutdownTimeout)
	}
	if cfg.Type != "valkey" {
		t.Errorf("Type: got %s, want valkey", cfg.Type)
	}
}
