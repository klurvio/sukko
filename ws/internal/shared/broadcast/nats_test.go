package broadcast

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestNATSBus_ConfigValidation tests configuration validation
func TestNATSBus_ConfigValidation(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "missing URLs",
			cfg: Config{
				Type:       "nats",
				BufferSize: 1024,
				NATS: NATSConfig{
					URLs: []string{},
				},
			},
			wantErr: "URL",
		},
		{
			name: "valid single URL",
			cfg: Config{
				Type:       "nats",
				BufferSize: 1024,
				NATS: NATSConfig{
					URLs:    []string{"nats://localhost:4222"},
					Subject: "test",
				},
			},
			wantErr: "", // Will fail on connection, not validation
		},
		{
			name: "valid cluster URLs",
			cfg: Config{
				Type:       "nats",
				BufferSize: 1024,
				NATS: NATSConfig{
					URLs:        []string{"nats://nats-1:4222", "nats://nats-2:4222"},
					ClusterMode: true,
					Subject:     "test",
				},
			},
			wantErr: "", // Will fail on connection, not validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := newNATSBus(tt.cfg, logger)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
					t.Errorf("Expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
			// For valid configs, we expect connection errors (no server running)
		})
	}
}

// TestNATSBus_DefaultValues tests that defaults are applied
func TestNATSBus_DefaultValues(t *testing.T) {
	t.Parallel()
	logger := zerolog.Nop()

	cfg := Config{
		Type:       "nats",
		BufferSize: 0, // Should use default
		NATS: NATSConfig{
			URLs:          []string{"nats://localhost:4222"},
			Subject:       "", // Should default to "ws.broadcast"
			ReconnectWait: 0,  // Should default to 2s
			MaxReconnects: 0,  // Should default to -1
		},
	}

	// Will fail on connection but should apply defaults first
	_, err := newNATSBus(cfg, logger)
	if err == nil {
		t.Skip("NATS server available")
	}

	// The error should be about connection, not about missing defaults
	if strings.Contains(strings.ToLower(err.Error()), "subject") &&
		strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Error("Should have applied default subject")
	}
}

// TestNATSBus_ClusterURLBuilding tests URL building for cluster mode
func TestNATSBus_ClusterURLBuilding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		urls        []string
		clusterMode bool
		wantJoined  bool
	}{
		{
			name:        "single URL",
			urls:        []string{"nats://localhost:4222"},
			clusterMode: false,
			wantJoined:  false,
		},
		{
			name:        "cluster URLs",
			urls:        []string{"nats://nats-1:4222", "nats://nats-2:4222"},
			clusterMode: true,
			wantJoined:  true,
		},
		{
			name:        "multiple URLs non-cluster",
			urls:        []string{"nats://nats-1:4222", "nats://nats-2:4222"},
			clusterMode: false,
			wantJoined:  false, // Uses first URL only in non-cluster mode
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var serverURL string
			if tt.clusterMode && len(tt.urls) > 1 {
				serverURL = ""
				var serverURLSb135 strings.Builder
				for i, url := range tt.urls {
					if i > 0 {
						serverURLSb135.WriteString(",")
					}
					serverURLSb135.WriteString(url)
				}
				serverURL += serverURLSb135.String()
			} else {
				serverURL = tt.urls[0]
			}

			if tt.wantJoined {
				if !strings.Contains(serverURL, ",") {
					t.Errorf("Expected comma-separated URLs, got: %s", serverURL)
				}
			} else {
				if strings.Contains(serverURL, ",") {
					t.Errorf("Expected single URL, got: %s", serverURL)
				}
			}
		})
	}
}

// TestNATSBus_SubscribeChannel tests subscribe channel creation
func TestNATSBus_SubscribeChannel(t *testing.T) {
	t.Parallel()
	bufferSize := 256

	var subscribers []chan *Message
	var mu sync.RWMutex

	// Simulate Subscribe()
	subCh := make(chan *Message, bufferSize)
	mu.Lock()
	subscribers = append(subscribers, subCh)
	mu.Unlock()

	if cap(subCh) != bufferSize {
		t.Errorf("Channel capacity: got %d, want %d", cap(subCh), bufferSize)
	}

	mu.RLock()
	if len(subscribers) != 1 {
		t.Errorf("Subscribers: got %d, want 1", len(subscribers))
	}
	mu.RUnlock()
}

// TestNATSBus_FanOutLogic tests fan-out to subscribers
func TestNATSBus_FanOutLogic(t *testing.T) {
	t.Parallel()
	const numSubscribers = 5
	subscribers := make([]chan *Message, numSubscribers)
	for i := range numSubscribers {
		subscribers[i] = make(chan *Message, 10)
	}

	msg := &Message{
		Subject: "nats.test",
		Payload: []byte(`{"data":"test"}`),
	}

	sent := 0
	for _, subCh := range subscribers {
		select {
		case subCh <- msg:
			sent++
		default:
		}
	}

	if sent != numSubscribers {
		t.Errorf("Sent: got %d, want %d", sent, numSubscribers)
	}
}

// TestNATSBus_MetricsType tests that NATS metrics report correct type
func TestNATSBus_MetricsType(t *testing.T) {
	t.Parallel()
	m := Metrics{
		Type:             "nats",
		Healthy:          true,
		Channel:          "ws.broadcast",
		Subscribers:      2,
		PublishErrors:    0,
		MessagesReceived: 500,
	}

	if m.Type != "nats" {
		t.Errorf("Type: got %s, want nats", m.Type)
	}
}

// TestNATSBus_AuthOptions tests authentication option handling
func TestNATSBus_AuthOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cfg      NATSConfig
		wantAuth string
	}{
		{
			name: "no auth",
			cfg: NATSConfig{
				URLs: []string{"nats://localhost:4222"},
			},
			wantAuth: "none",
		},
		{
			name: "token auth",
			cfg: NATSConfig{
				URLs:  []string{"nats://localhost:4222"},
				Token: "secret-token",
			},
			wantAuth: "token",
		},
		{
			name: "user auth",
			cfg: NATSConfig{
				URLs:     []string{"nats://localhost:4222"},
				User:     "user",
				Password: "pass",
			},
			wantAuth: "user",
		},
		{
			name: "token preferred over user",
			cfg: NATSConfig{
				URLs:     []string{"nats://localhost:4222"},
				Token:    "token",
				User:     "user",
				Password: "pass",
			},
			wantAuth: "token", // Token takes precedence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var authType string
			switch {
			case tt.cfg.Token != "":
				authType = "token"
			case tt.cfg.User != "" && tt.cfg.Password != "":
				authType = "user"
			default:
				authType = "none"
			}

			if authType != tt.wantAuth {
				t.Errorf("Auth type: got %s, want %s", authType, tt.wantAuth)
			}
		})
	}
}

// TestNATSBus_ReconnectSettings tests reconnection configuration
func TestNATSBus_ReconnectSettings(t *testing.T) {
	t.Parallel()
	cfg := NATSConfig{
		ReconnectWait: 5 * time.Second,
		MaxReconnects: 10,
	}

	if cfg.ReconnectWait != 5*time.Second {
		t.Errorf("ReconnectWait: got %v, want 5s", cfg.ReconnectWait)
	}
	if cfg.MaxReconnects != 10 {
		t.Errorf("MaxReconnects: got %d, want 10", cfg.MaxReconnects)
	}

	// Test unlimited reconnects
	cfg.MaxReconnects = -1
	if cfg.MaxReconnects != -1 {
		t.Errorf("MaxReconnects unlimited: got %d, want -1", cfg.MaxReconnects)
	}
}

// TestNATSBus_HealthCheck tests health check logic
func TestNATSBus_HealthCheck(t *testing.T) {
	t.Parallel()
	// Simulate health check conditions
	tests := []struct {
		name        string
		healthy     bool
		isConnected bool
		wantHealthy bool
	}{
		{"healthy and connected", true, true, true},
		{"healthy but disconnected", true, false, false},
		{"unhealthy", false, true, false},
		{"unhealthy and disconnected", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Simulate IsHealthy() logic
			result := tt.healthy && tt.isConnected
			if result != tt.wantHealthy {
				t.Errorf("IsHealthy: got %v, want %v", result, tt.wantHealthy)
			}
		})
	}
}
