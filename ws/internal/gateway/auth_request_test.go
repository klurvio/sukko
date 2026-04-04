package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klurvio/sukko/internal/shared/platform"
)

func TestAuthenticateRequest_AuthDisabled(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig:      platform.AuthConfig{AuthEnabled: false},
			DefaultTenantID: "default-tenant",
		},
		logger: testLogger(),
	}

	req := httptest.NewRequest(http.MethodGet, "/sse", http.NoBody)
	result, err := gw.authenticateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Principal != "anonymous" {
		t.Errorf("principal = %q, want anonymous", result.Principal)
	}
	if result.TenantID != "default-tenant" {
		t.Errorf("tenantID = %q, want default-tenant", result.TenantID)
	}
	if result.Claims != nil {
		t.Error("claims should be nil when auth disabled")
	}
}

func TestAuthenticateRequest_NoCredentials(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig: platform.AuthConfig{AuthEnabled: true},
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
