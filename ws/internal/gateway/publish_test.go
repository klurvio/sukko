package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/platform"
)

// publishTestGateway creates a minimal Gateway for publish handler testing.
func publishTestGateway(t *testing.T) *Gateway {
	t.Helper()
	return &Gateway{
		config: &platform.GatewayConfig{
			AuthConfig:      platform.AuthConfig{AuthEnabled: false},
			DefaultTenantID: "test-tenant",
			MaxPublishSize:  65536,
		},
		logger: testLogger(),
	}
}

func TestHandlePublish_NoServerClient(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)
	gw.serverClient = nil // no gRPC client

	body := `{"channel":"test.ch","data":{"msg":"hello"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePublish(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePublish_InvalidJSON(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePublish(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePublish_MissingChannel(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)

	body := `{"data":{"msg":"hello"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePublish(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorCode(t, rec, "INVALID_REQUEST")
}

func TestHandlePublish_MissingData(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)

	body := `{"channel":"test.ch"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePublish(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlePublish_BodyTooLarge(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)
	gw.config.MaxPublishSize = 10 // 10 bytes max

	body := `{"channel":"test.ch","data":{"msg":"this is way too long for 10 bytes"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	gw.HandlePublish(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandlePublish_InvalidContentType(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader("data"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	gw.HandlePublish(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlePublish_RateLimited(t *testing.T) {
	t.Parallel()

	gw := publishTestGateway(t)
	// Create a rate limiter with burst=1
	gw.publishRateLimiter = &PublishRateLimiter{
		rateLimit: 0.001, // extremely low rate
		burst:     1,
	}

	makeReq := func() *httptest.ResponseRecorder {
		body := `{"channel":"test.ch","data":{"msg":"hello"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		gw.HandlePublish(rec, req)
		return rec
	}

	// First request passes (burst=1)
	rec1 := makeReq()
	// With no server client, it should get to ServiceUnavailable (past rate limit)
	if rec1.Code != http.StatusServiceUnavailable {
		t.Errorf("first request: status = %d, want %d (no server client)", rec1.Code, http.StatusServiceUnavailable)
	}

	// Second request should be rate limited
	rec2 := makeReq()
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}
}

// assertErrorCode checks the error response has the expected code field.
func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var resp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if resp.Code != wantCode {
		t.Errorf("error code = %q, want %q", resp.Code, wantCode)
	}
}

// testLogger returns a no-op logger for tests.
func testLogger() zerolog.Logger {
	return zerolog.Nop()
}
