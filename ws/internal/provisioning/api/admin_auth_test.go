package api //nolint:revive // api is a common package name for HTTP handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
)

func newTestAdminAuth(token string) (*AdminAuth, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	logger := zerolog.Nop()
	aa := NewAdminAuth(ctx, &wg, token, DefaultAdminAuthConfig(), logger)
	return aa, func() {
		aa.Close()
		cancel()
		wg.Wait()
	}
}

func TestAdminAuth_ValidToken(t *testing.T) {
	t.Parallel()

	aa, cleanup := newTestAdminAuth("my-admin-token-12345")
	defer cleanup()

	var gotClaims *auth.Claims
	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims = auth.GetClaims(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer my-admin-token-12345")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotClaims == nil {
		t.Fatal("expected admin claims in context")
	}
	if !gotClaims.HasRole("admin") {
		t.Error("expected admin role in claims")
	}
	if !gotClaims.HasRole("system") {
		t.Error("expected system role in claims")
	}
}

func TestAdminAuth_InvalidToken_FallsThrough(t *testing.T) {
	t.Parallel()

	aa, cleanup := newTestAdminAuth("my-admin-token-12345")
	defer cleanup()

	handlerCalled := false
	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		// No claims should be set by admin auth — it fell through
		claims := auth.GetClaims(r.Context())
		if claims != nil {
			t.Error("expected no claims for invalid token fallthrough")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("handler should have been called (fallthrough)")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (fallthrough), got %d", rr.Code)
	}
}

func TestAdminAuth_EmptyToken_Disabled(t *testing.T) {
	t.Parallel()

	aa, cleanup := newTestAdminAuth("")
	defer cleanup()

	handlerCalled := false
	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("handler should have been called when admin auth disabled")
	}
}

func TestAdminAuth_NoAuthHeader(t *testing.T) {
	t.Parallel()

	aa, cleanup := newTestAdminAuth("my-admin-token-12345")
	defer cleanup()

	handlerCalled := false
	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("handler should have been called without auth header")
	}
}

func TestAdminAuth_RateLimiting(t *testing.T) {
	t.Parallel()

	aa, cleanup := newTestAdminAuth("my-admin-token-12345")
	defer cleanup()

	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send FailureThreshold failures
	defaults := DefaultAdminAuthConfig()
	for range defaults.FailureThreshold {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		req.RemoteAddr = "192.168.1.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Next request from same IP should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}

	// Different IP should not be rate limited
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req2.Header.Set("Authorization", "Bearer wrong-token")
	req2.RemoteAddr = "10.0.0.1:1234"
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code == http.StatusTooManyRequests {
		t.Error("different IP should not be rate limited")
	}
}

func TestAdminAuth_ValidTokenAfterFailures(t *testing.T) {
	t.Parallel()

	token := "my-admin-token-12345"
	aa, cleanup := newTestAdminAuth(token)
	defer cleanup()

	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send some failures (below threshold)
	for range 5 {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		req.RemoteAddr = "192.168.1.1:1234"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Valid token should still work
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "192.168.1.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestAdminAuth_BearerCaseInsensitive(t *testing.T) {
	t.Parallel()

	aa, cleanup := newTestAdminAuth("my-admin-token-12345")
	defer cleanup()

	var gotClaims *auth.Claims
	handler := aa.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims = auth.GetClaims(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// "bearer" lowercase should also work
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "bearer my-admin-token-12345")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if gotClaims == nil {
		t.Error("expected admin claims with lowercase bearer")
	}
}
