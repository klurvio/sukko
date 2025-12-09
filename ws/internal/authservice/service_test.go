package authservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceIssueToken(t *testing.T) {
	// Setup: Create a test config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tenants.yaml")
	configContent := `
tenants:
  - id: "test-tenant"
    name: "Test Tenant"
    apps:
      - id: "test-app"
        name: "Test App"
        secret_env: "TEST_APP_SECRET"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set the app secret environment variable
	os.Setenv("TEST_APP_SECRET", "test-secret-123")
	defer os.Unsetenv("TEST_APP_SECRET")

	// Load config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Create service
	svc := New("test-jwt-secret-at-least-32-bytes")
	svc.SetConfig(cfg)

	// Test successful token issuance
	t.Run("valid credentials", func(t *testing.T) {
		resp, errMsg, code := svc.IssueToken("test-app", "test-secret-123")
		if errMsg != "" {
			t.Errorf("Expected no error, got: %s (code %d)", errMsg, code)
		}
		if resp == nil {
			t.Fatal("Expected response, got nil")
		}
		if resp.Token == "" {
			t.Error("Expected token, got empty string")
		}
		if resp.TenantID != "test-tenant" {
			t.Errorf("Expected tenant_id 'test-tenant', got '%s'", resp.TenantID)
		}
	})

	// Test invalid app ID
	t.Run("invalid app_id", func(t *testing.T) {
		resp, errMsg, code := svc.IssueToken("nonexistent-app", "any-secret")
		if resp != nil {
			t.Errorf("Expected nil response, got: %+v", resp)
		}
		if code != 401 {
			t.Errorf("Expected status 401, got %d", code)
		}
		if errMsg != "Invalid credentials" {
			t.Errorf("Expected 'Invalid credentials', got '%s'", errMsg)
		}
	})

	// Test invalid secret
	t.Run("invalid secret", func(t *testing.T) {
		resp, _, code := svc.IssueToken("test-app", "wrong-secret")
		if resp != nil {
			t.Errorf("Expected nil response, got: %+v", resp)
		}
		if code != 401 {
			t.Errorf("Expected status 401, got %d", code)
		}
	})
}

func TestHandlerIssueToken(t *testing.T) {
	// Setup: Create a test config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "tenants.yaml")
	configContent := `
tenants:
  - id: "acme-corp"
    name: "Acme Corp"
    apps:
      - id: "acme-web"
        name: "Acme Web"
        secret_env: "ACME_WEB_SECRET"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	os.Setenv("ACME_WEB_SECRET", "acme-secret-xyz")
	defer os.Unsetenv("ACME_WEB_SECRET")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	svc := New("test-jwt-secret-at-least-32-bytes")
	svc.SetConfig(cfg)
	handler := svc.Handler()

	// Test POST /auth/token
	t.Run("POST /auth/token - success", func(t *testing.T) {
		body := `{"app_id": "acme-web", "app_secret": "acme-secret-xyz"}`
		req := httptest.NewRequest("POST", "/auth/token", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp TokenResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Token == "" {
			t.Error("Expected token, got empty")
		}
		if resp.TenantID != "acme-corp" {
			t.Errorf("Expected tenant_id 'acme-corp', got '%s'", resp.TenantID)
		}
	})

	t.Run("POST /auth/token - invalid credentials", func(t *testing.T) {
		body := `{"app_id": "acme-web", "app_secret": "wrong-secret"}`
		req := httptest.NewRequest("POST", "/auth/token", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("POST /auth/token - missing fields", func(t *testing.T) {
		body := `{"app_id": "acme-web"}`
		req := httptest.NewRequest("POST", "/auth/token", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	// Test GET /health
	t.Run("GET /health", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp HealthResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if resp.Status != "ok" {
			t.Errorf("Expected status 'ok', got '%s'", resp.Status)
		}
	})
}

func TestConfigValidation(t *testing.T) {
	t.Run("empty tenants", func(t *testing.T) {
		cfg := &Config{Tenants: []Tenant{}}
		if err := cfg.Validate(); err == nil {
			t.Error("Expected error for empty tenants")
		}
	})

	t.Run("missing app secret", func(t *testing.T) {
		cfg := &Config{
			Tenants: []Tenant{
				{
					ID:   "test",
					Name: "Test",
					Apps: []App{
						{ID: "app1", Name: "App 1", SecretEnv: "NONEXISTENT_VAR"},
					},
				},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("Expected error for missing app secret")
		}
	})

	t.Run("duplicate app ID", func(t *testing.T) {
		cfg := &Config{
			Tenants: []Tenant{
				{
					ID:   "tenant1",
					Name: "Tenant 1",
					Apps: []App{
						{ID: "app1", Name: "App 1", SecretEnv: "APP1_SECRET", secret: "secret1"},
					},
				},
				{
					ID:   "tenant2",
					Name: "Tenant 2",
					Apps: []App{
						{ID: "app1", Name: "App 1 Duplicate", SecretEnv: "APP1_SECRET2", secret: "secret2"},
					},
				},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("Expected error for duplicate app ID")
		}
	})
}

func TestConfigFindApp(t *testing.T) {
	cfg := &Config{
		Tenants: []Tenant{
			{
				ID:   "tenant1",
				Name: "Tenant 1",
				Apps: []App{
					{ID: "app1", TenantID: "tenant1", Name: "App 1"},
					{ID: "app2", TenantID: "tenant1", Name: "App 2"},
				},
			},
			{
				ID:   "tenant2",
				Name: "Tenant 2",
				Apps: []App{
					{ID: "app3", TenantID: "tenant2", Name: "App 3"},
				},
			},
		},
	}

	t.Run("find existing app", func(t *testing.T) {
		app := cfg.FindApp("app2")
		if app == nil {
			t.Fatal("Expected to find app2")
		}
		if app.ID != "app2" {
			t.Errorf("Expected app ID 'app2', got '%s'", app.ID)
		}
		if app.TenantID != "tenant1" {
			t.Errorf("Expected tenant ID 'tenant1', got '%s'", app.TenantID)
		}
	})

	t.Run("find app in second tenant", func(t *testing.T) {
		app := cfg.FindApp("app3")
		if app == nil {
			t.Fatal("Expected to find app3")
		}
		if app.TenantID != "tenant2" {
			t.Errorf("Expected tenant ID 'tenant2', got '%s'", app.TenantID)
		}
	})

	t.Run("app not found", func(t *testing.T) {
		app := cfg.FindApp("nonexistent")
		if app != nil {
			t.Errorf("Expected nil for nonexistent app, got: %+v", app)
		}
	})
}
