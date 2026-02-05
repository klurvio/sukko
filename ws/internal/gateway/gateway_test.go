package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/httputil"
	"github.com/Toniq-Labs/odin-ws/internal/shared/platform"
)

// newTestGatewayConfig returns a gateway config with auth disabled for testing.
// Auth requires a database connection, so most unit tests run with auth disabled.
func newTestGatewayConfig() *platform.GatewayConfig {
	return &platform.GatewayConfig{
		Port:                    3000,
		ReadTimeout:             15 * time.Second,
		WriteTimeout:            15 * time.Second,
		IdleTimeout:             60 * time.Second,
		BackendURL:              "ws://localhost:3001/ws",
		DialTimeout:             10 * time.Second,
		MessageTimeout:          60 * time.Second,
		AuthEnabled:             false,  // Disabled by default for unit tests
		DefaultTenantID:         "odin", // Required when auth disabled
		PublicPatterns:          []string{"*.trade"},
		UserScopedPatterns:      []string{"balances.{principal}"},
		GroupScopedPatterns:     []string{"community.{group_id}"},
		RateLimitEnabled:        true,
		RateLimitBurst:          100,
		RateLimitRate:           10.0,
		LogLevel:                "info",
		LogFormat:               "json",
		Environment:             "test",
		KeyCacheRefreshInterval: 1 * time.Minute,
		KeyCacheQueryTimeout:    5 * time.Second,
		RequireTenantID:         true,
		DBMaxOpenConns:          10,
		DBMaxIdleConns:          5,
		DBConnMaxLifetime:       5 * time.Minute,
		DBConnMaxIdleTime:       1 * time.Minute,
		DBPingTimeout:           5 * time.Second,
	}
}

// newTestLogger returns a zerolog logger that discards output.
func newTestLogger() zerolog.Logger {
	return zerolog.Nop()
}

// newGatewayWithMockValidator creates a gateway with auth disabled and injects a mock validator.
// This is useful for testing auth behavior without a database connection.
func newGatewayWithMockValidator(cfg *platform.GatewayConfig, logger zerolog.Logger, validator *auth.MultiTenantValidator) *Gateway {
	return &Gateway{
		config: cfg,
		permissions: NewPermissionChecker(
			cfg.PublicPatterns,
			cfg.UserScopedPatterns,
			cfg.GroupScopedPatterns,
		),
		validator: validator,
		logger:    logger.With().Str("component", "gateway").Logger(),
	}
}

func TestNew_AuthDisabled(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = false
	logger := newTestLogger()

	gw, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = gw.Close() }()

	if gw == nil {
		t.Fatal("New() returned nil")
	}
	if gw.validator != nil {
		t.Error("Validator should be nil when auth is disabled")
	}
	if gw.permissions != nil {
		t.Error("PermissionChecker should be nil when auth is disabled")
	}
}

func TestNew_AuthEnabled_RequiresDatabase(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	cfg.ProvisioningDBURL = "" // No database URL
	logger := newTestLogger()

	// Should fail because no database URL is provided
	_, err := New(cfg, logger)
	if err == nil {
		t.Error("New() should fail when auth is enabled without database URL")
	}
}

func TestExtractBearerToken(t *testing.T) {
	t.Parallel()

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
			t.Parallel()
			url := "/ws"
			if tt.queryToken != "" {
				url += "?token=" + tt.queryToken
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			got := httputil.ExtractBearerToken(req)
			if got != tt.expected {
				t.Errorf("ExtractBearerToken() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGateway_HandleHealth(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = false
	logger := newTestLogger()
	gw, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = gw.Close() }()

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

	// Check body - parse as JSON to avoid ordering issues
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse response body as JSON: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("HandleHealth() status = %q, want %q", result["status"], "ok")
	}
	if result["service"] != "ws-gateway" {
		t.Errorf("HandleHealth() service = %q, want %q", result["service"], "ws-gateway")
	}
}

func TestGateway_NewServer(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.Port = 3456
	cfg.ReadTimeout = 20 * time.Second
	cfg.WriteTimeout = 25 * time.Second
	cfg.IdleTimeout = 120 * time.Second
	cfg.AuthEnabled = false
	logger := newTestLogger()
	gw, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = gw.Close() }()

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

func TestGateway_HandleWebSocket_NoToken_WithMockValidator(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true // Enable auth for this test
	logger := newTestLogger()

	// Create a mock validator using StaticKeyRegistry
	registry := auth.NewStaticKeyRegistry()
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
		RequireKeyID:    true,
	})
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	gw := newGatewayWithMockValidator(cfg, logger, validator)

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

func TestGateway_HandleWebSocket_InvalidToken_WithMockValidator(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true // Enable auth for this test
	logger := newTestLogger()

	// Create a mock validator using StaticKeyRegistry (empty, so all tokens fail)
	registry := auth.NewStaticKeyRegistry()
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
		RequireKeyID:    true,
	})
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	gw := newGatewayWithMockValidator(cfg, logger, validator)

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

func TestGateway_Close_NilFields(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = false
	logger := newTestLogger()

	gw, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Should not panic when closing gateway with nil keyRegistry and dbConn
	if err := gw.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}
