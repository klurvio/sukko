package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klurvio/sukko/cmd/tester/runner"
	"github.com/rs/zerolog"
)

func newTestRouter() (http.Handler, *runner.Runner) {
	r := runner.New(runner.Config{
		GatewayURL:      "ws://localhost:3000",
		ProvisioningURL: "http://localhost:8080",
		MessageBackend:  "direct",
	}, zerolog.Nop())
	return NewRouter(r, "test-auth", zerolog.Nop()), r
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
	handler := NewRouter(r, "", zerolog.Nop()) // no auth token

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
