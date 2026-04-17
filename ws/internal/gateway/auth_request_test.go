package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klurvio/sukko/internal/shared/platform"
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
