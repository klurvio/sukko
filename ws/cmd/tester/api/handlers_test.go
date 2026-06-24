package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klurvio/sukko/cmd/tester/runner"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/rs/zerolog"
)

func newTestRouter() (http.Handler, *runner.Runner) {
	r := runner.New(runner.Config{
		GatewayURL:      "ws://localhost:3000",
		ProvisioningURL: "http://localhost:8080",
		MessageBackend:  "direct",
	}, zerolog.Nop())
	return NewRouter(r, "test-auth", "", zerolog.Nop()), r
}

func TestHealth(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/version", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["service"] != "sukko-tester" {
		t.Errorf("service = %v, want sukko-tester", resp["service"])
	}
}

func TestStartTest_NoAuth(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	body := `{"type":"smoke"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestStartTest_InvalidType(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	body := `{"type":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStartTest_MissingType(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStartTest_InvalidBody(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStartTest_Success(t *testing.T) {
	t.Parallel()

	handler, r := newTestRouter()
	body := `{"type":"smoke"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected non-empty id")
	}
	if resp["status"] != "running" {
		t.Errorf("status = %v, want running", resp["status"])
	}

	// Cleanup
	id := resp["id"].(string)
	_ = r.Stop(id)
	r.Wait()
}

func TestGetTest_NotFound(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tests/nonexistent", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestStopTest_NotFound(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests/nonexistent/stop", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAuthMiddleware_EmptyToken(t *testing.T) {
	t.Parallel()

	// Router with empty auth token — all requests should pass through
	r := runner.New(runner.Config{
		GatewayURL:     "ws://localhost:3000",
		MessageBackend: "direct",
	}, zerolog.Nop())
	handler := NewRouter(r, "", "", zerolog.Nop()) // no auth token

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tests/any", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should reach the handler (404 because test doesn't exist, not 401)
	if w.Code == http.StatusUnauthorized {
		t.Error("expected no auth enforcement when token is empty")
	}
}

func TestAuthMiddleware_WrongToken(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tests/any", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestStartTest_TenantIDPassthrough(t *testing.T) {
	t.Parallel()

	handler, r := newTestRouter()
	body := `{"type":"smoke","tenant_id":"my-tenant"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}

	if run.Config.TenantID != "my-tenant" {
		t.Errorf("TenantID = %q, want %q", run.Config.TenantID, "my-tenant")
	}

	_ = r.Stop(id)
	r.Wait()
}

func TestStartTest_AllTypes(t *testing.T) {
	t.Parallel()

	validTypes := []string{"smoke", "load", "stress", "soak", "validate"}
	for _, typ := range validTypes {
		t.Run(typ, func(t *testing.T) {
			t.Parallel()

			handler, r := newTestRouter()
			body := `{"type":"` + typ + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
			req.Header.Set("Authorization", "Bearer test-auth")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusCreated {
				t.Errorf("status = %d, want %d for type %q", w.Code, http.StatusCreated, typ)
			}

			var resp map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			if id, ok := resp["id"].(string); ok {
				_ = r.Stop(id)
			}
			r.Wait()
		})
	}
}

func TestStartTest_SigningKey_Valid(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(priv)

	handler, r := newTestRouter()
	body := fmt.Sprintf(`{"type":"validate","suite":"license-reload","signing_key":"%s"}`, encoded) //nolint:gocritic // sprintfQuotedString: %s is correct — value is inside a raw JSON template, %q would double-escape
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if id, ok := resp["id"].(string); ok {
		_ = r.Stop(id)
	}
	r.Wait()
}

func TestStartTest_SigningKey_InvalidBase64(t *testing.T) {
	t.Parallel()
	handler, _ := newTestRouter()
	body := `{"type":"validate","suite":"license-reload","signing_key":"not-valid-base64!!!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStartTest_SigningKey_WrongSize(t *testing.T) {
	t.Parallel()
	wrongKey := make([]byte, 32) // should be 64
	encoded := base64.StdEncoding.EncodeToString(wrongKey)

	handler, _ := newTestRouter()
	body := fmt.Sprintf(`{"type":"validate","suite":"license-reload","signing_key":"%s"}`, encoded) //nolint:gocritic // sprintfQuotedString: %s is correct — value is inside a raw JSON template, %q would double-escape
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestStartTest_SigningKey_NotProvided(t *testing.T) {
	t.Parallel()
	handler, r := newTestRouter()
	body := `{"type":"smoke"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should succeed — signing_key is optional, only needed for license-reload suite
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if id, ok := resp["id"].(string); ok {
		_ = r.Stop(id)
	}
	r.Wait()
}

func TestStartTest_AdminKey_OverridesConfigured(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(priv)

	handler, r := newTestRouter()
	body := fmt.Sprintf(`{"type":"smoke","admin_key":"%s"}`, encoded) //nolint:gocritic // sprintfQuotedString: %s is correct — value is inside a raw JSON template
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(run.Config.AdminKeyBytes, []byte(priv)) {
		t.Error("AdminKeyBytes does not match decoded admin_key")
	}

	_ = r.Stop(id)
	r.Wait()
}

func TestStartTest_AdminKey_Empty(t *testing.T) {
	t.Parallel()

	handler, r := newTestRouter()
	body := `{"type":"smoke"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if run.Config.AdminKeyBytes != nil {
		t.Errorf("AdminKeyBytes = %x, want nil", run.Config.AdminKeyBytes)
	}

	_ = r.Stop(id)
	r.Wait()
}

func TestStartTest_AdminKey_LocalDevMode(t *testing.T) {
	t.Parallel()

	// Router without TESTER_ADMIN_KEY_FILE (local dev mode) — per-request admin_key still works.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(priv)

	handler, r := newTestRouter()                                     // newTestRouter passes empty adminKeyID → local dev mode
	body := fmt.Sprintf(`{"type":"smoke","admin_key":"%s"}`, encoded) //nolint:gocritic // sprintfQuotedString: %s is correct
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(run.Config.AdminKeyBytes, []byte(priv)) {
		t.Error("AdminKeyBytes does not match body key in local dev mode")
	}

	_ = r.Stop(id)
	r.Wait()
}

func TestStartTest_AdminKeyID_OverridesDefault(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(priv)

	rr := runner.New(runner.Config{GatewayURL: "ws://localhost:3000", MessageBackend: "direct"}, zerolog.Nop())
	h := NewRouter(rr, "test-auth", "default-key-id", zerolog.Nop())

	body := fmt.Sprintf(`{"type":"smoke","admin_key":"%s","admin_key_id":"override-kid"}`, encoded) //nolint:gocritic // sprintfQuotedString: %s is correct — value is inside a raw JSON template, %q would double-escape
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, getErr := rr.Get(id)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if run.Config.AdminKeyID != "override-kid" {
		t.Errorf("AdminKeyID = %q, want %q", run.Config.AdminKeyID, "override-kid")
	}

	_ = rr.Stop(id)
	rr.Wait()
}

func TestStartTest_AdminKey_DefaultKID(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(priv)

	rr := runner.New(runner.Config{GatewayURL: "ws://localhost:3000", MessageBackend: "direct"}, zerolog.Nop())
	h := NewRouter(rr, "test-auth", "configured-key-id", zerolog.Nop())

	body := fmt.Sprintf(`{"type":"smoke","admin_key":"%s"}`, encoded) //nolint:gocritic // sprintfQuotedString
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, getErr := rr.Get(id)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	// When admin_key is provided without admin_key_id, effective kid = h.adminKeyID
	if run.Config.AdminKeyID != "configured-key-id" {
		t.Errorf("AdminKeyID = %q, want %q", run.Config.AdminKeyID, "configured-key-id")
	}

	_ = rr.Stop(id)
	rr.Wait()
}

func TestStartTest_AdminKeyPath_DeprecationWarned(t *testing.T) {
	t.Parallel()

	// Use a log buffer to capture Warn output.
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf)

	rr := runner.New(runner.Config{GatewayURL: "ws://localhost:3000", MessageBackend: "direct"}, zerolog.Nop())
	h := NewRouter(rr, "test-auth", "", logger)

	body := `{"type":"smoke","context":{"gateway_url":"ws://gw","provisioning_url":"http://prov","environment":"test","admin_key_path":"/some/path.bin"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "admin_key_path") {
		t.Errorf("expected deprecation warning containing 'admin_key_path' in log, got: %s", logOutput)
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, getErr := rr.Get(id)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if run.Config.AdminKeyBytes != nil {
		t.Errorf("AdminKeyBytes = %x, want nil (admin_key_path is deprecated, not decoded)", run.Config.AdminKeyBytes)
	}
	if run.Config.AdminKeyID != "" {
		t.Errorf("AdminKeyID = %q, want empty string", run.Config.AdminKeyID)
	}

	_ = rr.Stop(id)
	rr.Wait()
}

func TestStartTest_BackendValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "direct backend accepted",
			body:       `{"type":"smoke","message_backend":"direct"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "kafka backend with context accepted",
			body:       `{"type":"smoke","message_backend":"kafka","context":{"gateway_url":"ws://gw","provisioning_url":"http://prov","environment":"test","message_backend_urls":"localhost:9092"}}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "nats backend rejected with 400",
			body:       `{"type":"smoke","message_backend":"nats"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown backend rejected with 400",
			body:       `{"type":"smoke","message_backend":"zombie"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rr := runner.New(runner.Config{GatewayURL: "ws://localhost:3000", MessageBackend: platform.MessageBackendDirect}, zerolog.Nop())
			h := NewRouter(rr, "", "", zerolog.Nop())
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestStartTest_BackendURLsRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "kafka with empty backend URLs returns 400",
			body:       `{"type":"smoke","message_backend":"kafka","context":{"gateway_url":"ws://gw","provisioning_url":"http://prov","environment":"test","message_backend_urls":""}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "kafka with backend URLs returns 201",
			body:       `{"type":"smoke","message_backend":"kafka","context":{"gateway_url":"ws://gw","provisioning_url":"http://prov","environment":"test","message_backend_urls":"localhost:9092"}}`,
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rr := runner.New(runner.Config{GatewayURL: "ws://localhost:3000", MessageBackend: platform.MessageBackendDirect}, zerolog.Nop())
			h := NewRouter(rr, "", "", zerolog.Nop())
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestStartTest_AdminKeyID_WithoutAdminKey_Returns400(t *testing.T) {
	t.Parallel()

	rr := runner.New(runner.Config{GatewayURL: "ws://localhost:3000", MessageBackend: "direct"}, zerolog.Nop())
	h := NewRouter(rr, "", "", zerolog.Nop())

	body := `{"type":"smoke","admin_key_id":"bootstrap-0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["code"].(string); got != "INVALID_ADMIN_KEY_ID" {
		t.Errorf("code = %q, want %q", got, "INVALID_ADMIN_KEY_ID")
	}
}

func TestStartTest_AuthModeValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			// mixed mode not valid for validate
			name:       "auth_mode=mixed + type=validate → 400",
			body:       `{"type":"validate","auth_mode":"mixed"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// api-key mode not valid for load (only validate allowed)
			name:       "auth_mode=api-key + type=load → 400",
			body:       `{"type":"load","auth_mode":"api-key"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// upgrade mode not valid for load (only validate allowed)
			name:       "auth_mode=upgrade + type=load → 400",
			body:       `{"type":"load","auth_mode":"upgrade"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// api-key + validate + suite=api-key + tenant_id → 201
			name:       "auth_mode=api-key + type=validate + suite=api-key + tenant_id → 201",
			body:       `{"type":"validate","auth_mode":"api-key","suite":"api-key","tenant_id":"test-tenant"}`,
			wantStatus: http.StatusCreated,
		},
		{
			// api-key + validate + suite=auth is not in the allowlist (api-key, rest-publish)
			name:       "auth_mode=api-key + type=validate + suite=auth → 400",
			body:       `{"type":"validate","auth_mode":"api-key","suite":"auth","tenant_id":"test-tenant"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// upgrade mode only supports suite=upgrade; suite=channels is rejected
			name:       "auth_mode=upgrade + type=validate + suite=channels → 400",
			body:       `{"type":"validate","auth_mode":"upgrade","suite":"channels","tenant_id":"test-tenant"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// unrecognized auth_mode value → 400
			name:       "auth_mode=invalid → 400",
			body:       `{"type":"smoke","auth_mode":"invalid"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// auth_mix_ratio outside [0,1] (non-zero) → 400
			name:       "auth_mix_ratio=1.5 → 400",
			body:       `{"type":"load","auth_mix_ratio":1.5}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// auth_mix_ratio=0.0 is treated as "omitted" (float64 zero value) → uses default; valid for load
			name:       "auth_mix_ratio=0.0 → 201",
			body:       `{"type":"load","auth_mix_ratio":0.0}`,
			wantStatus: http.StatusCreated,
		},
		{
			// mixed mode is valid for load
			name:       "auth_mode=mixed + type=load → 201",
			body:       `{"type":"load","auth_mode":"mixed"}`,
			wantStatus: http.StatusCreated,
		},
		{
			// api-key + validate requires non-empty tenant_id
			name:       "auth_mode=api-key + type=validate + empty tenant_id → 400",
			body:       `{"type":"validate","auth_mode":"api-key","suite":"api-key"}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler, r := newTestRouter()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(tt.body))
			req.Header.Set("Authorization", "Bearer test-auth")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var resp map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				if id, ok := resp["id"].(string); ok {
					_ = r.Stop(id)
				}
				r.Wait()
			}
		})
	}
}

func TestStartTest_MissingAuthMode_DefaultsToJWT(t *testing.T) {
	t.Parallel()

	handler, r := newTestRouter()
	body := `{"type":"smoke"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tests", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-auth")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)

	run, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	if run.Config.AuthMode != runner.AuthModeJWT {
		t.Errorf("AuthMode = %q, want %q", run.Config.AuthMode, runner.AuthModeJWT)
	}

	_ = r.Stop(id)
	r.Wait()
}
