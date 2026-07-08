package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/provapi"
)

// TestAuthenticateRequest_CrossTenantJWT_Rejected is the gateway-level regression
// for #158: a JWT signed by tenant A's key but claiming tenant B is rejected with
// ErrTenantMismatch; a matching-tenant token is accepted.
func TestAuthenticateRequest_CrossTenantJWT_Rejected(t *testing.T) {
	t.Parallel()

	ecPEM, privateKey := generateTestECKeyForGateway(t)
	registry := auth.NewStaticKeyRegistry()
	key := &auth.KeyInfo{KeyID: "k1", TenantID: "tenant-a", Algorithm: "ES256", PublicKeyPEM: ecPEM, IsActive: true}
	if err := registry.AddKey(key); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:     registry,
		RequireTenantID: true,
		TenantResolver:  identityTenantResolver{}, // resolves slug->slug; key TenantID is a slug here
	})
	if err != nil {
		t.Fatalf("NewMultiTenantValidator: %v", err)
	}
	gw := &Gateway{
		config:    &platform.GatewayConfig{AuthConfig: platform.AuthConfig{AuthMode: "required"}},
		validator: validator,
		logger:    zerolog.Nop(),
	}

	mint := func(tenantClaim string) string {
		return createTestTokenForGateway(t, key, privateKey, &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-1",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
			TenantID: tenantClaim,
		})
	}

	// Cross-tenant: key owns tenant-a, token claims tenant-b -> rejected.
	req := httptest.NewRequest(http.MethodGet, "/ws", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+mint("tenant-b"))
	if _, err := gw.authenticateRequest(context.Background(), req); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("cross-tenant token: err = %v, want ErrTenantMismatch", err)
	}

	// Matching tenant -> accepted.
	req2 := httptest.NewRequest(http.MethodGet, "/ws", http.NoBody)
	req2.Header.Set("Authorization", "Bearer "+mint("tenant-a"))
	if _, err := gw.authenticateRequest(context.Background(), req2); err != nil {
		t.Fatalf("matching-tenant token: err = %v, want nil", err)
	}
}

func TestAuthenticateRequest_NoCredentials(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig: platform.AuthConfig{AuthMode: "required"},
		},
		logger: testLogger(),
	}

	req := httptest.NewRequest(http.MethodGet, "/sse", http.NoBody)
	// No Authorization header, no X-API-Key, no ?token=
	_, err := gw.authenticateRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for no credentials")
	}
	if !errors.Is(err, ErrNoCredentials) {
		t.Errorf("error = %v, want ErrNoCredentials", err)
	}
}

func TestAuthenticateRequest_SentinelErrors(t *testing.T) {
	t.Parallel()

	// Verify sentinel errors are distinct
	if errors.Is(ErrNoCredentials, ErrInvalidToken) {
		t.Error("ErrNoCredentials should not equal ErrInvalidToken")
	}
	if errors.Is(ErrInvalidAPIKey, ErrTenantMismatch) {
		t.Error("ErrInvalidAPIKey should not equal ErrTenantMismatch")
	}
}

// TestAuthenticateRequest_APIKeyOnly_PopulatesAPIKeyID verifies that the api_key-only
// auth path correctly populates APIKeyID from the resolved KeyID and leaves UserID empty.
func TestAuthenticateRequest_APIKeyOnly_PopulatesAPIKeyID(t *testing.T) {
	t.Parallel()

	cfg := &platform.GatewayConfig{
		AuthConfig: platform.AuthConfig{AuthMode: "required"},
	}
	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{
		"sk_live_secret": {
			KeyID:    "db-key-uuid-001",
			TenantID: "acme",
			Name:     "test key",
			IsActive: true,
		},
	}}
	gw := &Gateway{
		config:         cfg,
		apiKeyRegistry: mock,
		logger:         zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodGet, "/ws?api_key=sk_live_secret", http.NoBody)
	result, err := gw.authenticateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("authenticateRequest() error = %v, want nil", err)
	}

	if result.APIKeyID != "db-key-uuid-001" {
		t.Errorf("APIKeyID = %q, want %q", result.APIKeyID, "db-key-uuid-001")
	}
	if result.UserID != "" {
		t.Errorf("UserID = %q, want empty for API-key-only auth", result.UserID)
	}
	if result.AuthMethod != "api_key" {
		t.Errorf("AuthMethod = %q, want %q", result.AuthMethod, "api_key")
	}
	if result.TenantID != "acme" {
		t.Errorf("TenantID = %q, want %q", result.TenantID, "acme")
	}
}

// TestAuthenticateRequest_APIKeyOnly_InvalidKey verifies that APIKeyID is NOT populated
// when the API key is invalid (lookup fails).
func TestAuthenticateRequest_APIKeyOnly_InvalidKey(t *testing.T) {
	t.Parallel()

	cfg := &platform.GatewayConfig{
		AuthConfig: platform.AuthConfig{AuthMode: "required"},
	}
	mock := &mockAPIKeyLookup{keys: map[string]*provapi.APIKeyInfo{}} // empty registry
	gw := &Gateway{
		config:         cfg,
		apiKeyRegistry: mock,
		logger:         zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodGet, "/ws?api_key=nonexistent", http.NoBody)
	_, err := gw.authenticateRequest(context.Background(), req)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("error = %v, want ErrInvalidAPIKey", err)
	}
}
