package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/platform"
)

// newTestGatewayConfig returns a gateway config with valid defaults for testing.
func newTestGatewayConfig() *platform.GatewayConfig {
	return &platform.GatewayConfig{
		Port:                3000,
		ReadTimeout:         15 * time.Second,
		WriteTimeout:        15 * time.Second,
		IdleTimeout:         60 * time.Second,
		BackendURL:          "ws://localhost:3001/ws",
		DialTimeout:         10 * time.Second,
		MessageTimeout:      60 * time.Second,
		AuthEnabled:         true,
		JWTSecret:           "test-secret-key-at-least-32-bytes!!",
		PublicPatterns:      []string{"*.trade"},
		UserScopedPatterns:  []string{"balances.{principal}"},
		GroupScopedPatterns: []string{"community.{group_id}"},
		RateLimitEnabled:    true,
		RateLimitBurst:      100,
		RateLimitRate:       10.0,
		LogLevel:            "info",
		LogFormat:           "json",
		Environment:         "test",
	}
}

// newTestLogger returns a zerolog logger that discards output.
func newTestLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestNew_AuthEnabled(t *testing.T) {
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	gw := New(cfg, logger)

	if gw == nil {
		t.Fatal("New() returned nil")
	}
	if gw.config != cfg {
		t.Error("Gateway config not set correctly")
	}
	if gw.jwtValidator == nil {
		t.Error("JWT validator should be created when auth is enabled")
	}
	if gw.permissions == nil {
		t.Error("PermissionChecker should be created")
	}
}

func TestNew_AuthDisabled(t *testing.T) {
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = false
	logger := newTestLogger()

	gw := New(cfg, logger)

	if gw == nil {
		t.Fatal("New() returned nil")
	}
	if gw.jwtValidator != nil {
		t.Error("JWT validator should be nil when auth is disabled")
	}
	if gw.permissions == nil {
		t.Error("PermissionChecker should still be created")
	}
}

func TestGateway_ExtractToken(t *testing.T) {
	cfg := newTestGatewayConfig()
	logger := newTestLogger()
	gw := New(cfg, logger)

	tests := []struct {
		name       string
		queryToken string
		authHeader string
		expected   string
	}{
		{
			name:       "query param only",
			queryToken: "token-from-query",
			authHeader: "",
			expected:   "token-from-query",
		},
		{
			name:       "bearer header only",
			queryToken: "",
			authHeader: "Bearer token-from-header",
			expected:   "token-from-header",
		},
		{
			name:       "query param takes precedence",
			queryToken: "query-token",
			authHeader: "Bearer header-token",
			expected:   "query-token",
		},
		{
			name:       "no token",
			queryToken: "",
			authHeader: "",
			expected:   "",
		},
		{
			name:       "malformed bearer header - no space",
			queryToken: "",
			authHeader: "Bearertoken",
			expected:   "",
		},
		{
			name:       "malformed bearer header - short",
			queryToken: "",
			authHeader: "Bear",
			expected:   "",
		},
		{
			name:       "non-bearer auth header",
			queryToken: "",
			authHeader: "Basic dXNlcjpwYXNz",
			expected:   "",
		},
		{
			name:       "bearer header with empty token",
			queryToken: "",
			authHeader: "Bearer ",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/ws"
			if tt.queryToken != "" {
				url += "?token=" + tt.queryToken
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			got := gw.extractToken(req)
			if got != tt.expected {
				t.Errorf("extractToken() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGateway_HandleHealth(t *testing.T) {
	cfg := newTestGatewayConfig()
	logger := newTestLogger()
	gw := New(cfg, logger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	gw.HandleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HandleHealth() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("HandleHealth() Content-Type = %q, want %q", contentType, "application/json")
	}

	// Check body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	expectedBody := `{"status":"ok","service":"ws-gateway"}`
	if string(body) != expectedBody {
		t.Errorf("HandleHealth() body = %q, want %q", string(body), expectedBody)
	}
}

func TestGateway_NewServer(t *testing.T) {
	cfg := newTestGatewayConfig()
	cfg.Port = 3456
	cfg.ReadTimeout = 20 * time.Second
	cfg.WriteTimeout = 25 * time.Second
	cfg.IdleTimeout = 120 * time.Second
	logger := newTestLogger()
	gw := New(cfg, logger)

	server := gw.NewServer()

	if server == nil {
		t.Fatal("NewServer() returned nil")
	}

	// Check address
	expectedAddr := ":3456"
	if server.Addr != expectedAddr {
		t.Errorf("Server.Addr = %q, want %q", server.Addr, expectedAddr)
	}

	// Check timeouts
	if server.ReadTimeout != cfg.ReadTimeout {
		t.Errorf("Server.ReadTimeout = %v, want %v", server.ReadTimeout, cfg.ReadTimeout)
	}
	if server.WriteTimeout != cfg.WriteTimeout {
		t.Errorf("Server.WriteTimeout = %v, want %v", server.WriteTimeout, cfg.WriteTimeout)
	}
	if server.IdleTimeout != cfg.IdleTimeout {
		t.Errorf("Server.IdleTimeout = %v, want %v", server.IdleTimeout, cfg.IdleTimeout)
	}

	// Check that handler is set
	if server.Handler == nil {
		t.Error("Server.Handler should not be nil")
	}
}

func TestGateway_HandleWebSocket_NoToken(t *testing.T) {
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()
	gw := New(cfg, logger)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// Should return 401 Unauthorized when no token provided
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() without token status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestGateway_HandleWebSocket_InvalidToken(t *testing.T) {
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()
	gw := New(cfg, logger)

	req := httptest.NewRequest(http.MethodGet, "/ws?token=invalid-token", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// Should return 401 Unauthorized for invalid token
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() with invalid token status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
