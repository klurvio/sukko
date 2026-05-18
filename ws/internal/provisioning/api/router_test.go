package api_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/api"
	provauth "github.com/klurvio/sukko/internal/provisioning/auth"
	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/testutil"
	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/license"
)

// generateTestECKey generates an ECDSA P-256 key pair for testing.
func generateTestECKey(t *testing.T) (string, *ecdsa.PrivateKey) {
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

// createTestToken creates a signed JWT token for testing.
func createTestToken(t *testing.T, privateKey *ecdsa.PrivateKey, claims *auth.Claims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = "test-key-1"

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	return tokenString
}

// newTestService creates a provisioning service with mock stores.
func newTestService() *provisioning.Service {
	return newTestServiceWithStores(
		testutil.NewMockTenantStore(),
		testutil.NewMockRoutingRulesStore(),
		testutil.NewMockChannelRulesStore(),
	)
}

// newTestServiceWithStores creates a provisioning service with specific mock stores.
func newTestServiceWithStores(tenantStore *testutil.MockTenantStore, routingStore *testutil.MockRoutingRulesStore, channelRulesStore ...*testutil.MockChannelRulesStore) *provisioning.Service {
	svc, _ := newTestServiceWithKafka(tenantStore, routingStore, channelRulesStore...)
	return svc
}

// newTestServiceWithKafka creates a provisioning service and returns the kafka admin for test assertions.
func newTestServiceWithKafka(tenantStore *testutil.MockTenantStore, routingStore *testutil.MockRoutingRulesStore, channelRulesStore ...*testutil.MockChannelRulesStore) (*provisioning.Service, *testutil.MockKafkaAdmin) {
	kafkaAdmin := testutil.NewMockKafkaAdmin()
	cfg := provisioning.ServiceConfig{
		TenantStore:              tenantStore,
		KeyStore:                 testutil.NewMockKeyStore(),
		APIKeyStore:              testutil.NewMockAPIKeyStore(),
		RoutingRulesStore:        routingStore,
		QuotaStore:               testutil.NewMockQuotaStore(),
		AuditStore:               testutil.NewMockAuditStore(),
		KafkaAdmin:               kafkaAdmin,
		EventBus:                 eventbus.New(zerolog.Nop()),
		TopicNamespace:           "test",
		DefaultPartitions:        3,
		DefaultRetentionMs:       604800000,
		MaxTopicsPerTenant:       50,
		MaxRoutingRulesPerTenant: 100,
		DeprovisionGraceDays:     30,
		Logger:                   zerolog.Nop(),
	}
	if len(channelRulesStore) > 0 {
		cfg.ChannelRulesStore = channelRulesStore[0]
	}
	svc, err := provisioning.NewService(cfg)
	if err != nil {
		panic("newTestServiceWithKafka: " + err.Error())
	}
	return svc, kafkaAdmin
}

// mustNewRouter creates a test router, failing the test on error.
// Automatically sets EditionManager to Enterprise if not provided,
// so all feature gates pass through in existing tests.
// newTestValidator creates a MultiTenantValidator with a static key registry for tests.
// Returns the validator and the private key for signing test tokens.
func newTestValidator(t *testing.T) (*auth.MultiTenantValidator, *ecdsa.PrivateKey) {
	t.Helper()
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)
	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}
	return validator, privateKey
}

func mustNewRouter(t *testing.T, cfg api.RouterConfig) http.Handler {
	t.Helper()
	r, _ := mustNewRouterWithAuth(t, cfg)
	return r
}

// mustNewRouterWithAuth creates a test router and returns a function that adds
// an admin-level Authorization header to HTTP requests. This is the preferred
// helper for tests that need to send authenticated requests.
func mustNewRouterWithAuth(t *testing.T, cfg api.RouterConfig) (handler http.Handler, setAuth func(*http.Request)) {
	t.Helper()
	if cfg.EditionManager == nil {
		cfg.EditionManager = license.NewTestManager(license.Enterprise)
	}
	var privateKey *ecdsa.PrivateKey
	if cfg.Validator == nil {
		cfg.Validator, privateKey = newTestValidator(t)
	}
	router, err := api.NewRouter(cfg)
	if err != nil {
		t.Fatalf("mustNewRouterWithAuth: %v", err)
	}

	addAuth := func(req *http.Request) {
		if privateKey == nil {
			t.Fatal("mustNewRouterWithAuth: no private key available — pass Validator via cfg to use custom auth")
		}
		claims := &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-admin",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
			TenantID: "test-tenant",
			Roles:    []string{"admin"},
		}
		token := createTestToken(t, privateKey, claims)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return router, addAuth
}

func TestRouter_CORSPreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		origin         string
		allowedOrigins []string
		wantAllowed    bool
	}{
		{
			name:           "allowed origin",
			origin:         "http://localhost:3000",
			allowedOrigins: []string{"http://localhost:3000"},
			wantAllowed:    true,
		},
		{
			name:           "multiple allowed origins",
			origin:         "https://app.example.com",
			allowedOrigins: []string{"http://localhost:3000", "https://app.example.com"},
			wantAllowed:    true,
		},
		{
			name:           "disallowed origin",
			origin:         "https://evil.com",
			allowedOrigins: []string{"http://localhost:3000"},
			wantAllowed:    false,
		},
		{
			name:           "wildcard origin",
			origin:         "https://any.domain.com",
			allowedOrigins: []string{"*"},
			wantAllowed:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := mustNewRouter(t, api.RouterConfig{
				Service:            newTestService(),
				Logger:             zerolog.Nop(),
				CORSAllowedOrigins: tt.allowedOrigins,
				CORSMaxAge:         3600,
			})

			// Create preflight OPTIONS request
			req := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/tenants", nil)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("Access-Control-Request-Method", "POST")
			req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// Check response
			allowOrigin := rec.Header().Get("Access-Control-Allow-Origin")
			allowMethods := rec.Header().Get("Access-Control-Allow-Methods")
			allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
			maxAge := rec.Header().Get("Access-Control-Max-Age")

			if tt.wantAllowed {
				if allowOrigin == "" {
					t.Error("expected Access-Control-Allow-Origin header, got empty")
				}
				if allowMethods == "" {
					t.Error("expected Access-Control-Allow-Methods header, got empty")
				}
				if allowHeaders == "" {
					t.Error("expected Access-Control-Allow-Headers header, got empty")
				}
				if maxAge == "" {
					t.Error("expected Access-Control-Max-Age header, got empty")
				}
				// Preflight should return 200 or 204
				if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
					t.Errorf("expected status 200 or 204, got %d", rec.Code)
				}
			} else if allowOrigin != "" && allowOrigin != tt.origin {
				// For disallowed origins, the CORS middleware typically doesn't set headers
				// If there's an Allow-Origin header, it shouldn't match the disallowed origin
				t.Logf("Allow-Origin header present: %s", allowOrigin)
			}
		})
	}
}

func TestRouter_CORSHeaders_OnActualRequest(t *testing.T) {
	t.Parallel()

	router := mustNewRouter(t, api.RouterConfig{
		Service:            newTestService(),
		Logger:             zerolog.Nop(),
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		CORSMaxAge:         3600,
	})

	// Make an actual GET request with Origin header
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should have CORS headers on actual request too
	allowOrigin := rec.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", allowOrigin, "http://localhost:3000")
	}

	// Credentials should be allowed
	allowCreds := rec.Header().Get("Access-Control-Allow-Credentials")
	if allowCreds != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want %q", allowCreds, "true")
	}
}

func TestRouter_AuthRequired_RequiresToken(t *testing.T) {
	t.Parallel()

	// Create a key registry with a test key
	registry := auth.NewStaticKeyRegistry()
	ecPEM, _ := generateTestECKey(t)

	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	router := mustNewRouter(t, api.RouterConfig{
		Service: newTestService(),
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	// Request without token should fail
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestRouter_AuthRequired_ValidToken(t *testing.T) {
	t.Parallel()

	// Create a key registry with a test key
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	router := mustNewRouter(t, api.RouterConfig{
		Service: newTestService(),
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	// Create a valid token with admin role
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		TenantID: "test-tenant",
		Roles:    []string{"admin"},
	}

	token := createTestToken(t, privateKey, claims)

	// Request with valid token should succeed
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRouter_AuthRequired_ExpiredToken(t *testing.T) {
	t.Parallel()

	// Create a key registry with a test key
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	router := mustNewRouter(t, api.RouterConfig{
		Service: newTestService(),
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	// Create an expired token
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // Expired
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
		TenantID: "test-tenant",
		Roles:    []string{"admin"},
	}

	token := createTestToken(t, privateKey, claims)

	// Request with expired token should fail
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestRouter_AuthRequired_InvalidToken(t *testing.T) {
	t.Parallel()

	// Create a key registry with a test key
	registry := auth.NewStaticKeyRegistry()
	ecPEM, _ := generateTestECKey(t)

	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	router := mustNewRouter(t, api.RouterConfig{
		Service: newTestService(),
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "malformed token",
			token: "not-a-valid-jwt",
		},
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "only header",
			token: "Bearer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", tt.token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestRouter_AuthRequired_RoleRequirement(t *testing.T) {
	t.Parallel()

	// Create a key registry with a test key
	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)

	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	router := mustNewRouter(t, api.RouterConfig{
		Service: newTestService(),
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	tests := []struct {
		name       string
		roles      []string
		path       string
		method     string
		wantStatus int
	}{
		{
			name:       "admin can list tenants",
			roles:      []string{"admin"},
			path:       "/api/v1/tenants",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "system can list tenants",
			roles:      []string{"system"},
			path:       "/api/v1/tenants",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "user without admin role cannot list tenants",
			roles:      []string{"user"},
			path:       "/api/v1/tenants",
			method:     http.MethodGet,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "system can get active keys",
			roles:      []string{"system"},
			path:       "/api/v1/keys/active",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "admin can get active keys",
			roles:      []string{"admin"},
			path:       "/api/v1/keys/active",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "user cannot get active keys",
			roles:      []string{"user"},
			path:       "/api/v1/keys/active",
			method:     http.MethodGet,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			claims := &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-123",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					IssuedAt:  jwt.NewNumericDate(time.Now()),
				},
				TenantID: "test-tenant",
				Roles:    tt.roles,
			}

			token := createTestToken(t, privateKey, claims)

			req := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRouter_HealthEndpoints_NoAuth(t *testing.T) {
	t.Parallel()

	// Even with auth enabled, health endpoints should work without auth
	registry := auth.NewStaticKeyRegistry()
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	router := mustNewRouter(t, api.RouterConfig{
		Service: newTestService(),
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/health", http.StatusOK},
		{"/ready", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// =============================================================================
// Routing Rules Endpoint Tests (W8)
// =============================================================================

func TestRouter_RoutingRules_CRUD(t *testing.T) {
	t.Parallel()

	tenantStore := testutil.NewMockTenantStore()
	routingStore := testutil.NewMockRoutingRulesStore()
	svc, kafkaAdmin := newTestServiceWithKafka(tenantStore, routingStore)

	// Pre-create a tenant so service calls succeed
	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

	// Pre-seed Kafka topics referenced by routing rules in this test.
	_ = kafkaAdmin.CreateTopic(context.Background(), "test.test-tenant.trade", 1, nil)
	_ = kafkaAdmin.CreateTopic(context.Background(), "test.test-tenant.orderbook", 1, nil)

	router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
		Service: svc,
		Logger:  zerolog.Nop(),
	})

	// 1. GET routing rules — empty initially (200 with empty array)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants/test-tenant/routing-rules", nil)
	addAuth(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET (empty) status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// 2. PUT routing rules
	rules := provisioning.ReplaceRoutingRulesRequest{
		Rules: []provisioning.TopicRoutingRule{
			{Pattern: "**.trade", Topics: []string{"trade"}, Priority: 1},
			{Pattern: "**.orderbook", Topics: []string{"orderbook"}, Priority: 2},
		},
	}
	body, _ := json.Marshal(rules)
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/tenants/test-tenant/routing-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("PUT status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// 3. GET routing rules — should find them now
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants/test-tenant/routing-rules", nil)
	addAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET (after set) status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var getResp struct {
		Items  []provisioning.TopicRoutingRule `json:"items"`
		Total  int                             `json:"total"`
		Limit  int                             `json:"limit"`
		Offset int                             `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("failed to parse GET response: %v", err)
	}
	if len(getResp.Items) != 2 {
		t.Errorf("expected 2 rules, got %d", len(getResp.Items))
	}

	// 4. DELETE routing rules
	req = httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/tenants/test-tenant/routing-rules", nil)
	addAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("DELETE status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// 5. GET after delete — empty (200 with empty array)
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants/test-tenant/routing-rules", nil)
	addAuth(req)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET (after delete) status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRouter_RoutingRules_InvalidJSON(t *testing.T) {
	t.Parallel()

	tenantStore := testutil.NewMockTenantStore()
	routingStore := testutil.NewMockRoutingRulesStore()
	svc := newTestServiceWithStores(tenantStore, routingStore)
	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

	router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
		Service: svc,
		Logger:  zerolog.Nop(),
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/tenants/test-tenant/routing-rules",
		bytes.NewReader([]byte(`{invalid json}`)))
	req.Header.Set("Content-Type", "application/json")
	addAuth(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRouter_RoutingRules_InvalidRules(t *testing.T) {
	t.Parallel()

	tenantStore := testutil.NewMockTenantStore()
	routingStore := testutil.NewMockRoutingRulesStore()
	svc := newTestServiceWithStores(tenantStore, routingStore)
	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

	router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
		Service: svc,
		Logger:  zerolog.Nop(),
	})

	// Empty rules should fail validation
	rules := provisioning.ReplaceRoutingRulesRequest{
		Rules: []provisioning.TopicRoutingRule{},
	}
	body, _ := json.Marshal(rules)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/tenants/test-tenant/routing-rules", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRouter_RoutingRules_DeleteNonexistent(t *testing.T) {
	t.Parallel()

	tenantStore := testutil.NewMockTenantStore()
	routingStore := testutil.NewMockRoutingRulesStore()
	svc := newTestServiceWithStores(tenantStore, routingStore)
	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

	router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
		Service: svc,
		Logger:  zerolog.Nop(),
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/tenants/test-tenant/routing-rules", nil)
	addAuth(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// =============================================================================
// Channel Rules Endpoint Tests
// =============================================================================

func TestRouter_ChannelRules_PublishFields(t *testing.T) {
	t.Parallel()

	tenantStore := testutil.NewMockTenantStore()
	routingStore := testutil.NewMockRoutingRulesStore()
	channelRulesStore := testutil.NewMockChannelRulesStore()
	svc := newTestServiceWithStores(tenantStore, routingStore, channelRulesStore)

	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("tenant-sub-only"))
	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("tenant-sub-pub"))

	router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
		Service: svc,
		Logger:  zerolog.Nop(),
	})

	tests := []struct {
		name               string
		tenantID           string
		body               string
		wantPublic         []string
		wantPublishPublic  []string
		wantPublishDefault []string
		wantPublishGroups  map[string][]string
		wantStatus         int
	}{
		{
			name:               "subscribe-only rules",
			tenantID:           "tenant-sub-only",
			body:               `{"public":["general.*"],"group_mappings":{"vip":["room.vip"]},"default":["general.*"]}`,
			wantPublic:         []string{"general.*"},
			wantPublishPublic:  []string{},
			wantPublishDefault: []string{},
			wantPublishGroups:  map[string][]string{},
			wantStatus:         http.StatusOK,
		},
		{
			name:               "subscribe and publish rules",
			tenantID:           "tenant-sub-pub",
			body:               `{"public":["general.*","dm.{principal}"],"publish_public":["general.*","dm.{principal}"],"publish_group_mappings":{"vip":["room.vip"]},"publish_default":["general.*"]}`,
			wantPublic:         []string{"general.*", "dm.{principal}"},
			wantPublishPublic:  []string{"general.*", "dm.{principal}"},
			wantPublishDefault: []string{"general.*"},
			wantPublishGroups:  map[string][]string{"vip": {"room.vip"}},
			wantStatus:         http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// PUT channel rules
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/api/v1/tenants/"+tt.tenantID+"/channel-rules",
				bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			addAuth(req)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("PUT status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			// GET channel rules back and verify publish fields roundtrip
			req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants/"+tt.tenantID+"/channel-rules", nil)
			addAuth(req)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
			}

			var got struct {
				Rules struct {
					Public               []string            `json:"public"`
					PublishPublic        []string            `json:"publish_public"`
					PublishDefault       []string            `json:"publish_default"`
					PublishGroupMappings map[string][]string `json:"publish_group_mappings"`
				} `json:"rules"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to parse GET response: %v", err)
			}

			if len(got.Rules.Public) != len(tt.wantPublic) {
				t.Errorf("public = %v, want %v", got.Rules.Public, tt.wantPublic)
			}
			if len(got.Rules.PublishPublic) != len(tt.wantPublishPublic) {
				t.Errorf("publish_public = %v, want %v", got.Rules.PublishPublic, tt.wantPublishPublic)
			}
			if len(got.Rules.PublishDefault) != len(tt.wantPublishDefault) {
				t.Errorf("publish_default = %v, want %v", got.Rules.PublishDefault, tt.wantPublishDefault)
			}
			if len(got.Rules.PublishGroupMappings) != len(tt.wantPublishGroups) {
				t.Errorf("publish_group_mappings = %v, want %v", got.Rules.PublishGroupMappings, tt.wantPublishGroups)
			}
		})
	}
}

func TestRouter_RoutingRules_RequiresAdminRole(t *testing.T) {
	t.Parallel()

	registry := auth.NewStaticKeyRegistry()
	ecPEM, privateKey := generateTestECKey(t)
	keyInfo := &auth.KeyInfo{
		KeyID:        "test-key-1",
		TenantID:     "test-tenant",
		Algorithm:    "ES256",
		PublicKeyPEM: ecPEM,
		IsActive:     true,
	}
	if err := registry.AddKey(keyInfo); err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator failed: %v", err)
	}

	tenantStore := testutil.NewMockTenantStore()
	routingStore := testutil.NewMockRoutingRulesStore()
	svc := newTestServiceWithStores(tenantStore, routingStore)
	_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

	router := mustNewRouter(t, api.RouterConfig{
		Service: svc,
		Logger:  zerolog.Nop(),

		Validator: validator,
	})

	tests := []struct {
		name          string
		roles         []string
		method        string
		wantForbidden bool // true = expect 403, false = expect auth to pass (any non-401/403)
	}{
		{
			name:          "user cannot PUT routing rules",
			roles:         []string{"user"},
			method:        http.MethodPut,
			wantForbidden: true,
		},
		{
			name:          "admin can PUT routing rules",
			roles:         []string{"admin"},
			method:        http.MethodPut,
			wantForbidden: false,
		},
		{
			name:          "user cannot DELETE routing rules",
			roles:         []string{"user"},
			method:        http.MethodDelete,
			wantForbidden: true,
		},
		{
			name:          "user can GET routing rules",
			roles:         []string{"user"},
			method:        http.MethodGet,
			wantForbidden: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			claims := &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "user-123",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
					IssuedAt:  jwt.NewNumericDate(time.Now()),
				},
				TenantID: "test-tenant",
				Roles:    tt.roles,
			}
			token := createTestToken(t, privateKey, claims)

			var body *bytes.Reader
			if tt.method == http.MethodPut {
				rules := provisioning.ReplaceRoutingRulesRequest{
					Rules: []provisioning.TopicRoutingRule{
						{Pattern: "**.trade", Topics: []string{"trade"}, Priority: 1},
					},
				}
				b, _ := json.Marshal(rules)
				body = bytes.NewReader(b)
			} else {
				body = bytes.NewReader(nil)
			}

			req := httptest.NewRequestWithContext(context.Background(), tt.method, "/api/v1/tenants/test-tenant/routing-rules", body)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if tt.wantForbidden {
				if rec.Code != http.StatusForbidden {
					t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
				}
			} else {
				// Verify auth passes (not 401/403)
				if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
					t.Errorf("expected auth to pass, got status %d; body: %s", rec.Code, rec.Body.String())
				}
			}
		})
	}
}

// =============================================================================
// API Key Endpoint Tests
// =============================================================================

func TestRouter_APIKeys(t *testing.T) {
	t.Parallel()

	t.Run("create api key - 201", func(t *testing.T) {
		t.Parallel()

		tenantStore := testutil.NewMockTenantStore()
		routingStore := testutil.NewMockRoutingRulesStore()
		svc := newTestServiceWithStores(tenantStore, routingStore)

		_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

		router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
			Service: svc,
			Logger:  zerolog.Nop(),
		})

		body, _ := json.Marshal(map[string]string{"name": "test-key"})
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/tenants/test-tenant/api-keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addAuth(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if _, ok := resp["key_id"]; !ok {
			t.Error("response missing key_id field")
		}
	})

	t.Run("list api keys - 200", func(t *testing.T) {
		t.Parallel()

		tenantStore := testutil.NewMockTenantStore()
		routingStore := testutil.NewMockRoutingRulesStore()
		svc := newTestServiceWithStores(tenantStore, routingStore)

		_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

		router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
			Service: svc,
			Logger:  zerolog.Nop(),
		})

		// Create a key first
		body, _ := json.Marshal(map[string]string{"name": "list-test-key"})
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/tenants/test-tenant/api-keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addAuth(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("seed create status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		// List keys
		req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants/test-tenant/api-keys", nil)
		addAuth(req)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var resp struct {
			Items []map[string]any `json:"items"`
			Total int              `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if len(resp.Items) == 0 {
			t.Error("expected at least 1 item in list, got 0")
		}
	})

	t.Run("revoke api key - 200", func(t *testing.T) {
		t.Parallel()

		tenantStore := testutil.NewMockTenantStore()
		routingStore := testutil.NewMockRoutingRulesStore()
		svc := newTestServiceWithStores(tenantStore, routingStore)

		_ = tenantStore.Create(context.Background(), testutil.NewTestTenant("test-tenant"))

		router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
			Service: svc,
			Logger:  zerolog.Nop(),
		})

		// Create a key first
		body, _ := json.Marshal(map[string]string{"name": "revoke-test-key"})
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/tenants/test-tenant/api-keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addAuth(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("seed create status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		var createResp struct {
			KeyID string `json:"key_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("failed to parse create response: %v", err)
		}

		// Revoke the key
		req = httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/tenants/test-tenant/api-keys/"+createResp.KeyID, nil)
		addAuth(req)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	t.Run("get active api keys - 200", func(t *testing.T) {
		t.Parallel()

		router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
			Service: newTestService(),
			Logger:  zerolog.Nop(),
		})

		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/api-keys/active", nil)
		addAuth(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var resp struct {
			Keys []map[string]any `json:"keys"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
	})

	t.Run("create api key - tenant not found - 404", func(t *testing.T) {
		t.Parallel()

		router, addAuth := mustNewRouterWithAuth(t, api.RouterConfig{
			Service: newTestService(),
			Logger:  zerolog.Nop(),
		})

		body, _ := json.Marshal(map[string]string{"name": "orphan-key"})
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/tenants/nonexistent-tenant/api-keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		addAuth(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// The mock tenant store returns ErrTenantNotFound for nonexistent tenants,
		// which writeServiceError maps to 404.
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})
}

// emptyKeyRegistry implements auth.KeyRegistry with no keys (all lookups fail).
// Used only for the license auth test where tenant JWT validation is in the
// middleware chain but never exercised (admin JWT takes precedence).
type emptyKeyRegistry struct{}

func (emptyKeyRegistry) GetKey(context.Context, string) (*auth.KeyInfo, error) {
	return nil, auth.ErrKeyNotFound
}

func (emptyKeyRegistry) GetKeysByTenant(context.Context, string) ([]*auth.KeyInfo, error) {
	return nil, nil
}

func (emptyKeyRegistry) Close() error { return nil }

// noopLicenseStore implements provisioning.LicenseStateStore as a no-op.
// Used in the license auth integration test for the happy-path (200) case
// where Upsert is called after a successful license reload.
type noopLicenseStore struct{}

func (noopLicenseStore) Upsert(context.Context, string, string, string, *time.Time) error {
	return nil
}

func (noopLicenseStore) Load(context.Context) (string, error) { return "", nil }

//nolint:paralleltest // shares package-level publicKey via license.SetPublicKeyForTesting
func TestRouter_LicenseEndpoint_AdminAuth(t *testing.T) {
	// --- License signing keypair (for the happy-path 200 test case) ---
	licensePriv, licensePub := license.GenerateTestKeyPair()
	license.SetPublicKeyForTesting(licensePub)

	// --- Admin auth keypair (for AdminJWTMiddleware) ---
	adminPub, adminPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate admin keypair: %v", err)
	}
	const adminKID = "test-admin-key-1"

	// Register admin key in the AdminKeyRegistry.
	adminRegistry := provauth.NewAdminKeyRegistry()
	adminRegistry.Refresh([]*auth.KeyInfo{{
		KeyID:     adminKID,
		Algorithm: "EdDSA",
		PublicKey: adminPub,
		IsActive:  true,
	}})
	adminValidator := provauth.NewAdminValidator(adminRegistry)

	// Generate a second unregistered Ed25519 keypair (for UNAUTHORIZED test case).
	_, unregisteredPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate unregistered keypair: %v", err)
	}

	// Create MultiTenantValidator with an empty key registry.
	tenantValidator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: emptyKeyRegistry{},
	})
	if err != nil {
		t.Fatalf("create tenant validator: %v", err)
	}

	// Create a real LicenseManager via NewManager (empty key = Community, but Reload
	// uses the package-level publicKey overridden above).
	licenseManager, err := license.NewManager("", zerolog.Nop())
	if err != nil {
		t.Fatalf("create license manager: %v", err)
	}

	licenseHandler := api.NewLicenseHandler(
		licenseManager,
		noopLicenseStore{},
		eventbus.New(zerolog.Nop()),
		zerolog.Nop(),
	)

	router := mustNewRouter(t, api.RouterConfig{
		Service:        newTestService(),
		Logger:         zerolog.Nop(),
		Validator:      tenantValidator,
		AdminValidator: adminValidator,
		LicenseHandler: licenseHandler,
	})

	// Helper: mint an admin JWT with the given private key, kid, and roles.
	mintAdminJWT := func(privKey ed25519.PrivateKey, kid string, roles []string) string {
		t.Helper()
		now := time.Now()
		claims := jwt.MapClaims{
			"iss":   "sukko-admin",
			"sub":   "test-admin",
			"roles": roles,
			"exp":   jwt.NewNumericDate(now.Add(5 * time.Minute)),
			"iat":   jwt.NewNumericDate(now),
			"jti":   uuid.NewString(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
		token.Header["kid"] = kid
		signed, err := token.SignedString(privKey)
		if err != nil {
			t.Fatalf("sign admin JWT: %v", err)
		}
		return signed
	}

	// Sign a valid test license for the happy-path case.
	// Manager is Community (no key), so key must use Community edition to pass the edition gate.
	validLicenseKey := license.SignTestLicense(license.Claims{
		Edition: license.Community,
		Org:     "Test Org",
		Iat:     time.Now().Unix(),
		Exp:     time.Now().Add(24 * time.Hour).Unix(),
	}, licensePriv)

	tests := []struct {
		name       string
		setAuth    func(*http.Request)
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "no auth header → MISSING_TOKEN",
			setAuth:    nil,
			body:       `{"key":"test"}`,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "MISSING_TOKEN",
		},
		{
			name: "admin JWT signed by unregistered key → UNAUTHORIZED",
			setAuth: func(req *http.Request) {
				token := mintAdminJWT(unregisteredPriv, "unregistered-kid", []string{"admin"})
				req.Header.Set("Authorization", "Bearer "+token)
			},
			body:       `{"key":"test"}`,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
		{
			name: "valid admin JWT with wrong role → INSUFFICIENT_ROLE",
			setAuth: func(req *http.Request) {
				token := mintAdminJWT(adminPriv, adminKID, []string{"viewer"})
				req.Header.Set("Authorization", "Bearer "+token)
			},
			body:       `{"key":"test"}`,
			wantStatus: http.StatusForbidden,
			wantCode:   "INSUFFICIENT_ROLE",
		},
		{
			name: "valid admin JWT + valid license → 200 (end-to-end)",
			setAuth: func(req *http.Request) {
				token := mintAdminJWT(adminPriv, adminKID, []string{"admin"})
				req.Header.Set("Authorization", "Bearer "+token)
			},
			body:       `{"key":"` + validLicenseKey + `"}`,
			wantStatus: http.StatusOK,
			wantCode:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel — shares package-level license publicKey.

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/license", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			if tt.setAuth != nil {
				tt.setAuth(req)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantCode != "" {
				var errResp struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("unmarshal error response: %v; body: %s", err, rec.Body.String())
				}
				if errResp.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q; body: %s", errResp.Code, tt.wantCode, rec.Body.String())
				}
			}
		})
	}
}
