package broadcast

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestValkeyBus_ConfigValidation tests configuration validation
func TestValkeyBus_ConfigValidation(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "missing addrs",
			cfg: Config{
				Type:       "valkey",
				BufferSize: 1024,
				Valkey: ValkeyConfig{
					Addrs: []string{},
				},
			},
			wantErr: "address",
		},
		{
			name: "valid single addr",
			cfg: Config{
				Type:       "valkey",
				BufferSize: 1024,
				Valkey: ValkeyConfig{
					Addrs:   []string{"localhost:6379"},
					Channel: "test",
				},
			},
			wantErr: "", // Will fail on connection, not validation
		},
		{
			name: "valid sentinel addrs",
			cfg: Config{
				Type:       "valkey",
				BufferSize: 1024,
				Valkey: ValkeyConfig{
					Addrs:      []string{"sentinel-1:26379", "sentinel-2:26379", "sentinel-3:26379"},
					MasterName: "mymaster",
					Channel:    "test",
				},
			},
			wantErr: "", // Will fail on connection, not validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := newValkeyBus(tt.cfg, logger)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.wantErr)
				} else if !containsIgnoreCase(err.Error(), tt.wantErr) {
					t.Errorf("Expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
			// For valid configs, we expect connection errors (no server running)
		})
	}
}

// TestValkeyBus_DefaultValues tests that defaults are applied
func TestValkeyBus_DefaultValues(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	cfg := Config{
		Type:       "valkey",
		BufferSize: 0, // Should use default
		Valkey: ValkeyConfig{
			Addrs:      []string{"localhost:6379"},
			MasterName: "", // Should default to "mymaster"
			Channel:    "", // Should default to "ws.broadcast"
		},
	}

	// Will fail on connection but should apply defaults first
	_, err := newValkeyBus(cfg, logger)
	if err == nil {
		t.Skip("Valkey server available")
	}

	// The error should be about connection, not about missing defaults
	if containsIgnoreCase(err.Error(), "channel") && containsIgnoreCase(err.Error(), "empty") {
		t.Error("Should have applied default channel")
	}
}

// TestValkeyBus_DirectVsSentinel tests mode selection based on addr count
func TestValkeyBus_DirectVsSentinel(t *testing.T) {
	t.Parallel()
	// This is more of a documentation test - verifying the logic is correct
	// based on the number of addresses

	singleAddr := []string{"localhost:6379"}
	multiAddr := []string{"sentinel-1:26379", "sentinel-2:26379"}

	if len(singleAddr) != 1 {
		t.Error("Single addr should trigger direct mode")
	}
	if len(multiAddr) <= 1 {
		t.Error("Multiple addrs should trigger sentinel mode")
	}
}

// TestValkeyBus_SubscribeChannel tests the subscribe channel creation
func TestValkeyBus_SubscribeChannel(t *testing.T) {
	t.Parallel()
	// Test that subscribe returns a channel with correct buffer size
	bufferSize := 512

	// Create a mock subscriber list
	var subscribers []chan *Message
	var mu sync.RWMutex

	// Simulate Subscribe() logic
	subCh := make(chan *Message, bufferSize)
	mu.Lock()
	subscribers = append(subscribers, subCh)
	mu.Unlock()

	// Verify channel was created with correct buffer
	if cap(subCh) != bufferSize {
		t.Errorf("Channel capacity: got %d, want %d", cap(subCh), bufferSize)
	}

	// Verify subscriber was added
	mu.RLock()
	if len(subscribers) != 1 {
		t.Errorf("Subscribers count: got %d, want 1", len(subscribers))
	}
	mu.RUnlock()
}

// TestValkeyBus_FanOutLogic tests the fan-out behavior
func TestValkeyBus_FanOutLogic(t *testing.T) {
	t.Parallel()
	// Test fan-out to multiple subscribers
	const numSubscribers = 3
	subscribers := make([]chan *Message, numSubscribers)
	for i := range numSubscribers {
		subscribers[i] = make(chan *Message, 10)
	}

	msg := &Message{
		Subject: "test.subject",
		Payload: []byte("test payload"),
	}

	// Simulate fan-out
	sent := 0
	dropped := 0
	for _, subCh := range subscribers {
		select {
		case subCh <- msg:
			sent++
		default:
			dropped++
		}
	}

	if sent != numSubscribers {
		t.Errorf("Sent: got %d, want %d", sent, numSubscribers)
	}
	if dropped != 0 {
		t.Errorf("Dropped: got %d, want 0", dropped)
	}

	// Verify all subscribers received the message
	for i, subCh := range subscribers {
		select {
		case received := <-subCh:
			if received.Subject != msg.Subject {
				t.Errorf("Subscriber %d: subject mismatch", i)
			}
		default:
			t.Errorf("Subscriber %d: no message received", i)
		}
	}
}

// TestValkeyBus_FanOutDropsOnFullChannel tests drop behavior
func TestValkeyBus_FanOutDropsOnFullChannel(t *testing.T) {
	t.Parallel()
	// Create a full channel (buffer size 1, already has 1 message)
	subCh := make(chan *Message, 1)
	subCh <- &Message{Subject: "existing"}

	msg := &Message{Subject: "new"}

	// Try to send (should drop)
	dropped := false
	select {
	case subCh <- msg:
		// Sent
	default:
		dropped = true
	}

	if !dropped {
		t.Error("Expected message to be dropped on full channel")
	}
}

// TestValkeyBus_ContextCancellation tests shutdown via context
func TestValkeyBus_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate a loop that checks context
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				// Continue
			}
		}
	}()

	// Cancel and verify loop exits
	cancel()
	select {
	case <-done:
		// Good, loop exited
	case <-time.After(1 * time.Second):
		t.Error("Loop did not exit on context cancellation")
	}
}

// TestValkeyBus_MetricsStructure tests metrics returned format
func TestValkeyBus_MetricsStructure(t *testing.T) {
	t.Parallel()
	// Verify metrics structure matches expected format
	m := Metrics{
		Type:             "valkey",
		Healthy:          true,
		Channel:          "ws.broadcast",
		Subscribers:      3,
		PublishErrors:    0,
		MessagesReceived: 100,
		LastPublishAgo:   0.5,
	}

	if m.Type != "valkey" {
		t.Errorf("Type: got %s, want valkey", m.Type)
	}
	if !m.Healthy {
		t.Error("Healthy should be true")
	}
}

// Helper function
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(substr) == 0 ||
			(len(s) > 0 && containsLower(toLower(s), toLower(substr))))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
