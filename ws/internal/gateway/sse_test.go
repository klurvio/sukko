package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klurvio/sukko/internal/shared/platform"
)

func sseTestGateway(t *testing.T) *Gateway {
	t.Helper()
	return &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig:           platform.AuthConfig{AuthMode: "disabled"},
			DefaultTenantID:      "test-tenant",
			SSEKeepAliveInterval: 45_000_000_000, // 45s
		},
		logger: testLogger(),
	}
}

func TestHandleSSE_MissingChannels(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/sse", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSSE_EmptyChannels(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/sse?channels=", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSSE_EmptyChannelsWithCommas(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/sse?channels=,,", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSSE_NoServerClient(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)
	gw.serverClient = nil

	req := httptest.NewRequest(http.MethodGet, "/sse?channels=test.ch", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSSE_TenantLimitExceeded(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)
	// Create a tracker with limit=0 (always rejects)
	gw.connTracker = NewTenantConnectionTracker(0)

	req := httptest.NewRequest(http.MethodGet, "/sse?channels=test.ch", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestHandleSSE_AllChannelsFiltered(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)
	// Auth disabled but set permissions — filter will still apply since permissions != nil
	gw.permissions = NewPermissionChecker([]string{"*.trade"}, nil, nil)

	// All channels have wrong tenant → all filtered by filterSubscribeChannels
	// (tenant is "test-tenant" but channels use "wrong")
	req := httptest.NewRequest(http.MethodGet, "/sse?channels=wrong.BTC.trade,wrong.ETH.trade", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSSE_AuthDisabled_AllChannelsPass(t *testing.T) {
	t.Parallel()

	gw := sseTestGateway(t)
	gw.serverClient = nil // will hit 503 if channels pass through

	req := httptest.NewRequest(http.MethodGet, "/sse?channels=any.channel,whatever.foo", http.NoBody)
	rec := httptest.NewRecorder()

	gw.HandleSSE(rec, req)

	// Auth disabled → no filtering → reaches server client check → 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (channels should pass through when auth disabled)", rec.Code, http.StatusServiceUnavailable)
	}
}
