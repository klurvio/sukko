package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/auth"
)

// withTenantSlug injects a chi URL param "tenantSlug" into r's context.
func withTenantSlug(r *http.Request, slug string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenantSlug", slug)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withClaims injects auth claims into r's context.
func withClaims(r *http.Request, claims *auth.Claims) *http.Request {
	return r.WithContext(auth.WithClaims(r.Context(), claims))
}

// requireTenantHandler wraps a "got it" handler with RequireTenant using the provided lookup func.
// holdPeriod controls how long old-slug JWTs are accepted after a rename.
func requireTenantHandler(lookup provisioning.TenantLookupFunc, holdPeriod time.Duration) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return RequireTenant(lookup, holdPeriod)(ok)
}

func TestAuthMiddleware_SkipsWhenPreAuthenticated(t *testing.T) {
	t.Parallel()

	// Create a validator that should never be called — if it is, the test will
	// panic because StaticKeyRegistry has no keys, but more importantly the
	// handler assertion below would fail.
	registry := &auth.StaticKeyRegistry{}
	validator, err := auth.NewMultiTenantValidator(auth.MultiTenantValidatorConfig{
		KeyRegistry:    registry,
		TenantResolver: identityTenantResolver{},
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
		KeyRegistry:    registry,
		TenantResolver: identityTenantResolver{},
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

func TestRequireTenant(t *testing.T) {
	t.Parallel()

	activeTenant := &provisioning.Tenant{
		ID:     "uuid-1234",
		Slug:   "acme-corp",
		Status: provisioning.StatusActive,
	}
	recentRename := time.Now().Add(-time.Minute)    // within any reasonable hold period
	expiredRename := time.Now().Add(-2 * time.Hour) // older than holdPeriod (1h) — grace window closed
	renamedTenant := &provisioning.Tenant{
		ID:              "uuid-1234",
		Slug:            "new-corp",
		Status:          provisioning.StatusActive,
		SlugRenameState: provisioning.SlugRenameStateComplete,
		PreviousSlug:    "acme-corp",
		SlugRenamedAt:   &recentRename,
	}
	deprovisioningTenant := &provisioning.Tenant{
		ID:              "uuid-1234",
		Slug:            "new-corp",
		Status:          provisioning.StatusDeprovisioning,
		SlugRenameState: provisioning.SlugRenameStateComplete,
		PreviousSlug:    "acme-corp",
	}
	deletedTenant := &provisioning.Tenant{
		ID:              "uuid-1234",
		Slug:            "new-corp",
		Status:          provisioning.StatusDeleted,
		SlugRenameState: provisioning.SlugRenameStateComplete,
		PreviousSlug:    "acme-corp",
	}

	tests := []struct {
		name       string
		claims     *auth.Claims
		slugParam  string
		tenant     *provisioning.Tenant
		lookupErr  error
		wantStatus int
	}{
		{
			name:       "admin role bypasses ownership check",
			claims:     &auth.Claims{TenantID: "other-tenant", Roles: []string{"admin"}},
			slugParam:  "acme-corp",
			tenant:     activeTenant,
			wantStatus: http.StatusOK,
		},
		{
			name:       "system role bypasses ownership check",
			claims:     &auth.Claims{TenantID: "other-tenant", Roles: []string{"system"}},
			slugParam:  "acme-corp",
			tenant:     activeTenant,
			wantStatus: http.StatusOK,
		},
		{
			name:       "direct slug match passes",
			claims:     &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam:  "acme-corp",
			tenant:     activeTenant,
			wantStatus: http.StatusOK,
		},
		{
			name:       "grace period — previous slug accepted for active renamed tenant",
			claims:     &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam:  "new-corp",
			tenant:     renamedTenant,
			wantStatus: http.StatusOK,
		},
		{
			name:       "grace period — rejected when tenant is deprovisioning (not active)",
			claims:     &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam:  "new-corp",
			tenant:     deprovisioningTenant,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "grace period — rejected when tenant is deleted (not active)",
			claims:     &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam:  "new-corp",
			tenant:     deletedTenant,
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "empty PreviousSlug guard — complete state without previous slug is not grace period",
			claims:    &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam: "new-corp",
			tenant: &provisioning.Tenant{
				ID: "uuid-1234", Slug: "new-corp", Status: provisioning.StatusActive,
				SlugRenameState: provisioning.SlugRenameStateComplete, PreviousSlug: "",
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "mismatch — different tenant denied",
			claims:     &auth.Claims{TenantID: "other-tenant", Roles: []string{"user"}},
			slugParam:  "acme-corp",
			tenant:     activeTenant,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "tenant not found — 404",
			claims:     &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam:  "acme-corp",
			lookupErr:  provisioning.ErrTenantNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store error — 503",
			claims:     &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam:  "acme-corp",
			lookupErr:  errors.New("db connection lost"),
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "no claims in context — 401",
			claims:     nil,
			slugParam:  "acme-corp",
			tenant:     activeTenant,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:      "grace period — rejected when hold period has expired",
			claims:    &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam: "new-corp",
			tenant: &provisioning.Tenant{
				ID:              "uuid-1234",
				Slug:            "new-corp",
				Status:          provisioning.StatusActive,
				SlugRenameState: provisioning.SlugRenameStateComplete,
				PreviousSlug:    "acme-corp",
				SlugRenamedAt:   &expiredRename,
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "grace period — rejected when SlugRenameState is pending (rename in progress)",
			claims:    &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam: "new-corp",
			tenant: &provisioning.Tenant{
				ID:              "uuid-1234",
				Slug:            "new-corp",
				Status:          provisioning.StatusActive,
				SlugRenameState: provisioning.SlugRenameStatePending,
				PreviousSlug:    "acme-corp",
				SlugRenamedAt:   &recentRename,
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "grace period — rejected when SlugRenamedAt is nil despite complete state",
			claims:    &auth.Claims{TenantID: "acme-corp", Roles: []string{"user"}},
			slugParam: "new-corp",
			tenant: &provisioning.Tenant{
				ID:              "uuid-1234",
				Slug:            "new-corp",
				Status:          provisioning.StatusActive,
				SlugRenameState: provisioning.SlugRenameStateComplete,
				PreviousSlug:    "acme-corp",
				SlugRenamedAt:   nil,
			},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lookup := func(_ context.Context, _ string) (*provisioning.Tenant, error) {
				if tt.lookupErr != nil {
					return nil, tt.lookupErr
				}
				return tt.tenant, nil
			}

			handler := requireTenantHandler(lookup, time.Hour)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			req = withTenantSlug(req, tt.slugParam)
			if tt.claims != nil {
				req = withClaims(req, tt.claims)
			}
			// Inject zerolog logger (required by middleware for warning logs).
			logger := zerolog.Nop()
			req = req.WithContext(logger.WithContext(req.Context()))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}
