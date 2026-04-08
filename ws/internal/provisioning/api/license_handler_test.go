package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/testutil"
	"github.com/klurvio/sukko/internal/shared/license"
)

// testLicensePriv and testLicensePub are Ed25519 keypair for test license signing.
// Tests in this file MUST NOT use t.Parallel() since they call SetPublicKeyForTesting.
var testLicensePriv, testLicensePub = license.GenerateTestKeyPair()

func setupLicenseHandlerTest(t *testing.T) *LicenseHandler {
	t.Helper()
	license.SetPublicKeyForTesting(testLicensePub)

	logger := zerolog.Nop()
	mgr, err := license.NewManager("", logger)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	bus := eventbus.New(logger)
	repo := testutil.NewMockLicenseStateStore()

	return NewLicenseHandler(mgr, repo, bus, logger)
}

func TestHandleReload_Success(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	key := license.SignTestLicense(license.Claims{
		Edition: license.Pro,
		Org:     "TestOrg",
		Exp:     time.Now().Add(365 * 24 * time.Hour).Unix(),
		Iat:     time.Now().Unix(),
	}, testLicensePriv)

	body, _ := json.Marshal(licenseReloadRequest{Key: key})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleReload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp licenseReloadResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Edition != "pro" {
		t.Errorf("edition = %q, want %q", resp.Edition, "pro")
	}
	if resp.Org != "TestOrg" {
		t.Errorf("org = %q, want %q", resp.Org, "TestOrg")
	}
	if resp.Status != "reloaded" {
		t.Errorf("status = %q, want %q", resp.Status, "reloaded")
	}
}

func TestHandleReload_MissingKey(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	body, _ := json.Marshal(licenseReloadRequest{Key: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleReload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleReload_InvalidJSON(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()

	h.HandleReload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleReload_InvalidSignature(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	body, _ := json.Marshal(licenseReloadRequest{Key: "not-a-valid-license"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleReload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleReload_ReplayDetected(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	iat := time.Now().Unix()
	key1 := license.SignTestLicense(license.Claims{
		Edition: license.Pro,
		Org:     "Acme",
		Exp:     time.Now().Add(365 * 24 * time.Hour).Unix(),
		Iat:     iat,
	}, testLicensePriv)

	// First reload — success
	body1, _ := json.Marshal(licenseReloadRequest{Key: key1})
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body1))
	rec1 := httptest.NewRecorder()
	h.HandleReload(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first reload status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Second reload with same iat — replay
	key2 := license.SignTestLicense(license.Claims{
		Edition: license.Enterprise,
		Org:     "Acme",
		Exp:     time.Now().Add(365 * 24 * time.Hour).Unix(),
		Iat:     iat, // same iat
	}, testLicensePriv)

	body2, _ := json.Marshal(licenseReloadRequest{Key: key2})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	h.HandleReload(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Errorf("replay status = %d, want %d", rec2.Code, http.StatusConflict)
	}
}

func TestHandleReload_ExpiredKey(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	key := license.SignTestLicense(license.Claims{
		Edition: license.Pro,
		Org:     "Acme",
		Exp:     time.Now().Add(-24 * time.Hour).Unix(), // expired yesterday
		Iat:     time.Now().Unix(),
	}, testLicensePriv)

	body, _ := json.Marshal(licenseReloadRequest{Key: key})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.HandleReload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleReload_RateLimited(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	body, _ := json.Marshal(licenseReloadRequest{Key: "any"})

	// Exhaust the burst (3 requests allowed)
	for range 4 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.HandleReload(rec, req)
	}

	// Next request should be rate-limited
	req := httptest.NewRequest(http.MethodPost, "/api/v1/license", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleReload(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestCurrentKey_SetAndGet(t *testing.T) {
	h := setupLicenseHandlerTest(t)

	if got := h.CurrentKey(); got != "" {
		t.Errorf("initial CurrentKey() = %q, want empty", got)
	}

	h.SetCurrentKey("test-key-123")
	if got := h.CurrentKey(); got != "test-key-123" {
		t.Errorf("CurrentKey() = %q, want %q", got, "test-key-123")
	}
}
