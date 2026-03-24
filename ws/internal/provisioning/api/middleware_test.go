package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
)

func TestAuthMiddleware_SkipsWhenPreAuthenticated(t *testing.T) {
	t.Parallel()

	// Create a validator that should never be called — if it is, the test will
	// panic because StaticKeyRegistry has no keys, but more importantly the
	// handler assertion below would fail.
	registry := &auth.StaticKeyRegistry{}
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("unexpected error creating validator: %v", err)
	}

	logger := zerolog.Nop()

	// Pre-set claims in context (as admin token middleware would)
	preClaims := &auth.Claims{
		Roles: []string{"admin", "system"},
	}

	var gotClaims *auth.Claims
	handler := AuthMiddleware(validator, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims = auth.GetClaims(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	ctx := auth.WithClaims(context.Background(), preClaims)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotClaims == nil {
		t.Fatal("expected claims in context")
	}
	if !gotClaims.HasRole("admin") {
		t.Error("expected admin role preserved from pre-auth")
	}
	if !gotClaims.HasRole("system") {
		t.Error("expected system role preserved from pre-auth")
	}
}

func TestAuthMiddleware_RejectsWhenNoClaims(t *testing.T) {
	t.Parallel()

	registry := &auth.StaticKeyRegistry{}
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry: registry,
	})
	if err != nil {
		t.Fatalf("unexpected error creating validator: %v", err)
	}

	logger := zerolog.Nop()

	handlerCalled := false
	handler := AuthMiddleware(validator, logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/tenants", nil)
	// No Authorization header, no claims in context
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if handlerCalled {
		t.Error("handler should not have been called without auth")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}
