package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/provapi"
)

// newTestGatewayConfig returns a gateway config with auth disabled for testing.
// Auth requires a database connection, so most unit tests run with auth disabled.
func newTestGatewayConfig() *platform.GatewayConfig {
	return &platform.GatewayConfig{
		BaseConfig: platform.BaseConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			Environment: "test",
		},
		AuthConfig: platform.AuthConfig{
			AuthEnabled: false, // Disabled by default for unit tests
		},
		Port:                         3000,
		ReadTimeout:                  15 * time.Second,
		WriteTimeout:                 15 * time.Second,
		IdleTimeout:                  60 * time.Second,
		BackendURL:                   "ws://localhost:3001/ws",
		DialTimeout:                  10 * time.Second,
		MessageTimeout:               60 * time.Second,
		DefaultTenantID:              "sukko", // Required when auth disabled
		PublicPatterns:               []string{"*.trade"},
		UserScopedPatterns:           []string{"balances.{principal}"},
		GroupScopedPatterns:          []string{"community.{group_id}"},
		RateLimitEnabled:             true,
		RateLimitBurst:               100,
		RateLimitRate:                10.0,
		PublishRateLimit:             10.0,
		PublishBurst:                 100,
		MaxPublishSize:               65536,
		MaxFrameSize:                 1048576,
		TenantConnectionLimitEnabled: true,
		DefaultTenantConnectionLimit: 1000,
		AuthRefreshRateInterval:      30 * time.Second,
		AuthValidationTimeout:        5 * time.Second,
		ShutdownTimeout:              30 * time.Second,
		ChannelRulesCacheTTL:         1 * time.Minute,
		RegistryQueryTimeout:         5 * time.Second,
		RequireTenantID:              true,
		ProvisioningClientConfig: platform.ProvisioningClientConfig{
			ProvisioningGRPCAddr:  "localhost:9090",
			GRPCReconnectDelay:    1 * time.Second,
			GRPCReconnectMaxDelay: 30 * time.Second,
		},
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

func TestNew_AuthEnabled_ConfigValidation(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	cfg.ProvisioningGRPCAddr = "" // No gRPC address

	// Config validation should catch missing gRPC address
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail when auth is enabled without gRPC address")
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

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
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

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
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
	})
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	gw := newGatewayWithMockValidator(cfg, logger, validator)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws", nil)
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
	})
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	gw := newGatewayWithMockValidator(cfg, logger, validator)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?token=invalid-token", nil)
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

	// Should not panic when closing gateway with nil streamKeyRegistry and streamTenantRegistry
	if err := gw.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

// mockAPIKeyLookup implements APIKeyLookup for testing API key auth flows.
type mockAPIKeyLookup struct {
	keys map[string]*provapi.APIKeyInfo
}

func (m *mockAPIKeyLookup) Lookup(apiKey string) (*provapi.APIKeyInfo, bool) {
	info, ok := m.keys[apiKey]
	if !ok || !info.IsActive {
		return nil, false
	}
	return info, true
}

func (m *mockAPIKeyLookup) Close() error { return nil }

// newGatewayWithAPIKeyMock creates a gateway with auth enabled and injects both
// a mock API key registry and an optional JWT validator. When validator is nil,
// only API-key auth paths are exercisable.
func newGatewayWithAPIKeyMock(cfg *platform.GatewayConfig, logger zerolog.Logger, validator *auth.MultiTenantValidator, apiKeys *mockAPIKeyLookup) *Gateway {
	return &Gateway{
		config: cfg,
		permissions: NewPermissionChecker(
			cfg.PublicPatterns,
			cfg.UserScopedPatterns,
			cfg.GroupScopedPatterns,
		),
		validator:      validator,
		apiKeyRegistry: apiKeys,
		logger:         logger.With().Str("component", "gateway").Logger(),
	}
}

// generateTestECKeyForGateway generates an ECDSA P-256 key pair and returns
// the PEM-encoded public key and private key for signing test JWTs.
func generateTestECKeyForGateway(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate EC key: %v", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to marshal public key: %v", err)
	}

	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}
	return string(pem.EncodeToMemory(pemBlock)), privateKey
}

// createTestTokenForGateway creates a signed JWT for testing.
func createTestTokenForGateway(t *testing.T, key *auth.KeyInfo, privateKey any, claims *auth.Claims) string {
	t.Helper()

	method, err := auth.GetSigningMethod(key.Algorithm)
	if err != nil {
		t.Fatalf("GetSigningMethod failed: %v", err)
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = key.KeyID

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}
	return tokenString
}

func TestHandleWebSocket_NoCredentials(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, nil, mock)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() no credentials status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}
	if errResp["code"] != "UNAUTHORIZED" {
		t.Errorf("error code = %q, want %q", errResp["code"], "UNAUTHORIZED")
	}
	if errResp["message"] != "token or api_key required" {
		t.Errorf("error message = %q, want %q", errResp["message"], "token or api_key required")
	}
}

func TestHandleWebSocket_InvalidAPIKey(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"valid-key": {
			KeyID:    "pk_live_abc",
			TenantID: "acme",
			Name:     "test key",
			IsActive: true,
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, nil, mock)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?api_key=wrong-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() invalid API key status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}
	if errResp["code"] != "UNAUTHORIZED" {
		t.Errorf("error code = %q, want %q", errResp["code"], "UNAUTHORIZED")
	}
	if errResp["message"] != "invalid api key" {
		t.Errorf("error message = %q, want %q", errResp["message"], "invalid api key")
	}
}

func TestHandleWebSocket_InactiveAPIKey(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"inactive-key": {
			KeyID:    "pk_live_inactive",
			TenantID: "acme",
			Name:     "inactive key",
			IsActive: false, // Inactive keys should be rejected
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, nil, mock)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?api_key=inactive-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() inactive API key status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandleWebSocket_APIKeyOnly_Valid(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	cfg.TenantConnectionLimitEnabled = false // Disable to isolate auth testing
	logger := newTestLogger()

	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"valid-key": {
			KeyID:    "pk_live_abc",
			TenantID: "acme",
			Name:     "test key",
			IsActive: true,
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, nil, mock)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?api_key=valid-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// A valid API key should pass auth. The request will fail at the WebSocket
	// upgrade step (httptest.ResponseRecorder doesn't support upgrades), but
	// critically it must NOT fail with 401. The gobwas/ws upgrader writes
	// directly to the ResponseWriter and does not set a standard HTTP status on
	// failure, so the recorder keeps its default 200.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() valid API key should not return 401, got %d", resp.StatusCode)
	}
}

func TestHandleWebSocket_APIKeyViaHeader(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"header-key": {
			KeyID:    "pk_live_header",
			TenantID: "acme",
			Name:     "header key",
			IsActive: true,
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, nil, mock)

	// Send API key via X-API-Key header instead of query parameter
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// Invalid key via header should return 401
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() invalid API key via header status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandleWebSocket_APIKeyAndJWT_TenantMismatch(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	cfg.TenantConnectionLimitEnabled = false
	logger := newTestLogger()

	// Set up JWT validator with a test key for tenant "acme"
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKeyForGateway(t)

	key := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// API key belongs to tenant "globex" (different from JWT tenant "acme")
	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"globex-key": {
			KeyID:    "pk_live_globex",
			TenantID: "globex",
			Name:     "globex key",
			IsActive: true,
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, validator, mock)

	// Create a valid JWT for tenant "acme"
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "acme",
	}
	tokenString := createTestTokenForGateway(t, key, privateKey, claims)

	// Send both API key (globex) and JWT (acme) — tenant mismatch
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?token="+tokenString+"&api_key=globex-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() tenant mismatch status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}
	if errResp["message"] != "api key and token tenant mismatch" {
		t.Errorf("error message = %q, want %q", errResp["message"], "api key and token tenant mismatch")
	}
}

func TestHandleWebSocket_APIKeyAndJWT_TenantMatch(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	cfg.TenantConnectionLimitEnabled = false
	logger := newTestLogger()

	// Set up JWT validator with a test key for tenant "acme"
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKeyForGateway(t)

	key := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// API key also belongs to tenant "acme" (matching JWT)
	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"acme-key": {
			KeyID:    "pk_live_acme",
			TenantID: "acme",
			Name:     "acme key",
			IsActive: true,
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, validator, mock)

	// Create a valid JWT for tenant "acme"
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-456",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "acme",
	}
	tokenString := createTestTokenForGateway(t, key, privateKey, claims)

	// Send both API key and JWT for same tenant — should pass auth
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?token="+tokenString+"&api_key=acme-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// Auth should pass; request fails at WebSocket upgrade (not 401)
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() matching tenants should not return 401, got %d", resp.StatusCode)
	}
}

func TestHandleWebSocket_BothCredentials_InvalidAPIKey(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	// Set up JWT validator
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKeyForGateway(t)

	key := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "acme",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// Empty API key registry — all keys invalid
	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, validator, mock)

	// Create a valid JWT
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-789",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "acme",
	}
	tokenString := createTestTokenForGateway(t, key, privateKey, claims)

	// Both credentials present but API key is invalid — should reject before JWT validation
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?token="+tokenString+"&api_key=bad-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() invalid API key with valid JWT status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}
	if errResp["message"] != "invalid api key" {
		t.Errorf("error message = %q, want %q", errResp["message"], "invalid api key")
	}
}

func TestHandleWebSocket_BothCredentials_InvalidJWT(t *testing.T) {
	t.Parallel()
	cfg := newTestGatewayConfig()
	cfg.AuthEnabled = true
	logger := newTestLogger()

	// Set up JWT validator (empty registry — no keys registered, so all tokens fail)
	registry := auth.NewStaticKeyRegistry()

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	// Valid API key
	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"acme-key": {
			KeyID:    "pk_live_acme",
			TenantID: "acme",
			Name:     "acme key",
			IsActive: true,
		},
	}}
	gw := newGatewayWithAPIKeyMock(cfg, logger, validator, mock)

	// Both credentials: valid API key but garbled JWT
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ws?token=not-a-valid-jwt&api_key=acme-key", nil)
	w := httptest.NewRecorder()

	gw.HandleWebSocket(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HandleWebSocket() valid API key + invalid JWT status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}
	if errResp["message"] != "invalid token" {
		t.Errorf("error message = %q, want %q", errResp["message"], "invalid token")
	}
}
