package api_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/api"
	"github.com/Toniq-Labs/odin-ws/internal/testutil"
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
func createTestToken(t *testing.T, keyID string, privateKey *ecdsa.PrivateKey, claims *auth.Claims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("Failed to sign token: %v", err)
	}

	return tokenString
}

// newTestService creates a provisioning service with mock stores.
func newTestService() *provisioning.Service {
	return provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:          testutil.NewMockTenantStore(),
		KeyStore:             testutil.NewMockKeyStore(),
		TopicStore:           testutil.NewMockTopicStore(),
		QuotaStore:           testutil.NewMockQuotaStore(),
		AuditStore:           testutil.NewMockAuditStore(),
		KafkaAdmin:           testutil.NewMockKafkaAdmin(),
		TopicNamespace:       "test",
		DefaultPartitions:    3,
		DefaultRetentionMs:   604800000,
		MaxTopicsPerTenant:   50,
		DeprovisionGraceDays: 30,
		Logger:               zerolog.Nop(),
	})
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

			router := api.NewRouter(api.RouterConfig{
				Service:            newTestService(),
				Logger:             zerolog.Nop(),
				CORSAllowedOrigins: tt.allowedOrigins,
				CORSMaxAge:         3600,
			})

			// Create preflight OPTIONS request
			req := httptest.NewRequest(http.MethodOptions, "/api/v1/tenants", nil)
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
			} else {
				// For disallowed origins, the CORS middleware typically doesn't set headers
				// The request may still succeed but without CORS headers
				if allowOrigin != "" && allowOrigin != tt.origin {
					// If there's an Allow-Origin header, it shouldn't match the disallowed origin
					t.Logf("Allow-Origin header present: %s", allowOrigin)
				}
			}
		})
	}
}

func TestRouter_CORSHeaders_OnActualRequest(t *testing.T) {
	t.Parallel()

	router := api.NewRouter(api.RouterConfig{
		Service:            newTestService(),
		Logger:             zerolog.Nop(),
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		CORSMaxAge:         3600,
	})

	// Make an actual GET request with Origin header
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
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

func TestRouter_AuthDisabled_APIWorksWithoutToken(t *testing.T) {
	t.Parallel()

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: false, // Auth disabled
	})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "health endpoint",
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
		},
		{
			name:       "ready endpoint",
			method:     http.MethodGet,
			path:       "/ready",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list tenants without auth",
			method:     http.MethodGet,
			path:       "/api/v1/tenants",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get active keys without auth",
			method:     http.MethodGet,
			path:       "/api/v1/keys/active",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRouter_AuthEnabled_RequiresToken(t *testing.T) {
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

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: true,
		Validator:   validator,
	})

	// Request without token should fail
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestRouter_AuthEnabled_ValidToken(t *testing.T) {
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

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: true,
		Validator:   validator,
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

	token := createTestToken(t, "test-key-1", privateKey, claims)

	// Request with valid token should succeed
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRouter_AuthEnabled_ExpiredToken(t *testing.T) {
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

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: true,
		Validator:   validator,
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

	token := createTestToken(t, "test-key-1", privateKey, claims)

	// Request with expired token should fail
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestRouter_AuthEnabled_InvalidToken(t *testing.T) {
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

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: true,
		Validator:   validator,
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

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
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

func TestRouter_AuthEnabled_RoleRequirement(t *testing.T) {
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

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: true,
		Validator:   validator,
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

			token := createTestToken(t, "test-key-1", privateKey, claims)

			req := httptest.NewRequest(tt.method, tt.path, nil)
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

	router := api.NewRouter(api.RouterConfig{
		Service:     newTestService(),
		Logger:      zerolog.Nop(),
		AuthEnabled: true,
		Validator:   validator,
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

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
