package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/revocation"
)

func newTestRevocationHandler() (*RevocationHandler, *chi.Mux) {
	store := revocation.New(zerolog.Nop())
	bus := eventbus.New(zerolog.Nop())
	h := NewRevocationHandler(store, bus, 24*time.Hour, zerolog.Nop())

	r := chi.NewRouter()
	r.Post("/api/v1/tenants/{tenantID}/tokens/revoke", h.HandleRevoke)
	return h, r
}

func TestHandleRevoke_ByJTI(t *testing.T) {
	t.Parallel()
	h, router := newTestRevocationHandler()
	defer h.store.Close()

	body, _ := json.Marshal(revocationRequest{JTI: "tok-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/tokens/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var resp revocationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Type != "token" {
		t.Errorf("type = %q, want %q", resp.Type, "token")
	}
	if resp.TenantID != "acme" {
		t.Errorf("tenant_id = %q, want %q", resp.TenantID, "acme")
	}
}

func TestHandleRevoke_BySub(t *testing.T) {
	t.Parallel()
	h, router := newTestRevocationHandler()
	defer h.store.Close()

	body, _ := json.Marshal(revocationRequest{Sub: "alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/tokens/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var resp revocationResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Type != "user" {
		t.Errorf("type = %q, want %q", resp.Type, "user")
	}
}

func TestHandleRevoke_BothSubAndJTI(t *testing.T) {
	t.Parallel()
	h, router := newTestRevocationHandler()
	defer h.store.Close()

	body, _ := json.Marshal(revocationRequest{Sub: "alice", JTI: "tok-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/tokens/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleRevoke_MissingFields(t *testing.T) {
	t.Parallel()
	h, router := newTestRevocationHandler()
	defer h.store.Close()

	body, _ := json.Marshal(revocationRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/tokens/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleRevoke_InvalidJSON(t *testing.T) {
	t.Parallel()
	h, router := newTestRevocationHandler()
	defer h.store.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/tokens/revoke", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleRevoke_WithExplicitExp(t *testing.T) {
	t.Parallel()
	h, router := newTestRevocationHandler()
	defer h.store.Close()

	customExp := time.Now().Add(30 * time.Minute).Unix()
	body, _ := json.Marshal(revocationRequest{JTI: "tok-exp", Exp: &customExp})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/tokens/revoke", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp revocationResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	parsed, err := time.Parse(time.RFC3339, resp.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	// Should be close to the custom exp, not 24h from now
	if parsed.Unix() != customExp {
		t.Errorf("expires_at = %d, want %d", parsed.Unix(), customExp)
	}
}
