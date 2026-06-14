package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/provapi"
)

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
