package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func newTestProvisioningProxy(t *testing.T, backend *httptest.Server) *ProvisioningProxy {
	t.Helper()
	p, err := NewProvisioningProxy(backend.URL, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewProvisioningProxy() error = %v", err)
	}
	return p
}

func TestProvisioningProxy_SuccessfulProxy(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "from-provisioning")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tenant-1"}`))
	}))
	defer backend.Close()

	proxy := newTestProvisioningProxy(t, backend)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(`{"id":"tenant-1"}`))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if got := rec.Header().Get("X-Custom"); got != "from-provisioning" {
		t.Errorf("X-Custom = %q, want %q", got, "from-provisioning")
	}
	if got := rec.Body.String(); got != `{"id":"tenant-1"}` {
		t.Errorf("body = %q, want %q", got, `{"id":"tenant-1"}`)
	}
}

func TestProvisioningProxy_Unreachable(t *testing.T) {
	t.Parallel()

	// Create proxy pointing to a server that's already closed
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	backend.Close() // immediately close so proxy can't reach it

	proxy := newTestProvisioningProxy(t, backend)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", http.NoBody)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}

	var errResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != "BAD_GATEWAY" {
		t.Errorf("error code = %q, want %q", errResp.Code, "BAD_GATEWAY")
	}
}

func TestProvisioningProxy_HeadersForwarded(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy := newTestProvisioningProxy(t, backend)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-jwt-token")
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := capturedHeaders.Get("Authorization"); got != "Bearer test-jwt-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer test-jwt-token")
	}
	if got := capturedHeaders.Get("X-API-Key"); got != "test-api-key" {
		t.Errorf("X-API-Key = %q, want %q", got, "test-api-key")
	}
	if got := capturedHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
}

func TestProvisioningProxy_XForwardedFor(t *testing.T) {
	t.Parallel()

	var capturedXFF string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy := newTestProvisioningProxy(t, backend)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", http.NoBody)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if !strings.Contains(capturedXFF, "10.0.0.1") {
		t.Errorf("X-Forwarded-For = %q, want to contain %q", capturedXFF, "10.0.0.1")
	}
}

func TestProvisioningProxy_ResponseBodyPreserved(t *testing.T) {
	t.Parallel()

	largeBody := strings.Repeat("x", 1024*1024) // 1MB
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(largeBody))
	}))
	defer backend.Close()

	proxy := newTestProvisioningProxy(t, backend)
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if len(body) != len(largeBody) {
		t.Errorf("body length = %d, want %d", len(body), len(largeBody))
	}
}

func TestNewProvisioningProxy_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://localhost:8080", false},
		{"valid https", "https://provisioning.example.com", false},
		{"invalid scheme", "ftp://localhost:8080", true},
		{"empty scheme", "localhost:8080", true},
		{"invalid URL", "://invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := NewProvisioningProxy(tt.url, zerolog.Nop())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvisioningProxy(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err == nil && p == nil {
				t.Error("expected non-nil proxy on success")
			}
		})
	}
}
