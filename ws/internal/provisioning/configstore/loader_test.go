package configstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

const validYAML = `
tenants:
  - id: loader-tenant
    name: Loader Tenant
    categories:
      - name: trade
    keys:
      - id: key-loader
        algorithm: ES256
        public_key: |
          -----BEGIN PUBLIC KEY-----
          MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEEVs/o5+uQbTjL3chynL4wXgUg2R9
          q9UU8I5mEovUf86QZ7kOBIjJwqnzD1omageEHWwHdBO6B+dFabmdT9POxg==
          -----END PUBLIC KEY-----
`

const updatedYAML = `
tenants:
  - id: loader-tenant
    name: Updated Tenant
    categories:
      - name: trade
      - name: order
    keys:
      - id: key-loader
        algorithm: ES256
        public_key: |
          -----BEGIN PUBLIC KEY-----
          MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEEVs/o5+uQbTjL3chynL4wXgUg2R9
          q9UU8I5mEovUf86QZ7kOBIjJwqnzD1omageEHWwHdBO6B+dFabmdT9POxg==
          -----END PUBLIC KEY-----
`

const invalidYAML = `
tenants:
  - id: ""
    name: ""
`

func TestLoader_StoresBeforeLoad(t *testing.T) {
	t.Parallel()

	loader := NewLoader("/nonexistent", zerolog.Nop())
	if stores := loader.Stores(); stores != nil {
		t.Error("Stores() should be nil before Load()")
	}
}

func TestLoader_Load(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := NewLoader(path, zerolog.Nop())
	if err := loader.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	stores := loader.Stores()
	if stores == nil {
		t.Fatal("Stores() should not be nil after Load()")
	}

	// Verify we can read data from the stores
	tenant, err := stores.TenantStore().Get(t.Context(), "loader-tenant")
	if err != nil {
		t.Fatalf("Get tenant error = %v", err)
	}
	if tenant.Name != "Loader Tenant" {
		t.Errorf("tenant name = %q, want %q", tenant.Name, "Loader Tenant")
	}
}

func TestLoader_LoadInvalidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(invalidYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := NewLoader(path, zerolog.Nop())
	if err := loader.Load(); err == nil {
		t.Fatal("expected error for invalid config")
	}

	if stores := loader.Stores(); stores != nil {
		t.Error("Stores() should be nil after failed Load()")
	}
}

func TestLoader_Reload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := NewLoader(path, zerolog.Nop())
	if err := loader.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Update file with new content
	if err := os.WriteFile(path, []byte(updatedYAML), 0o644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	stores := loader.Stores()
	tenant, err := stores.TenantStore().Get(t.Context(), "loader-tenant")
	if err != nil {
		t.Fatalf("Get tenant error = %v", err)
	}
	if tenant.Name != "Updated Tenant" {
		t.Errorf("tenant name = %q, want %q", tenant.Name, "Updated Tenant")
	}

	// Verify new categories present
	topics, err := stores.TopicStore().ListByTenant(t.Context(), "loader-tenant")
	if err != nil {
		t.Fatalf("ListByTenant error = %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("topics count = %d, want 2", len(topics))
	}
}

func TestLoader_ReloadFailure_KeepsPrevious(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := NewLoader(path, zerolog.Nop())
	if err := loader.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Overwrite with invalid config
	if err := os.WriteFile(path, []byte(invalidYAML), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	if err := loader.Reload(); err == nil {
		t.Fatal("expected error for invalid reload")
	}

	// Previous stores should still be active
	stores := loader.Stores()
	if stores == nil {
		t.Fatal("Stores() should not be nil after failed Reload()")
	}

	tenant, err := stores.TenantStore().Get(t.Context(), "loader-tenant")
	if err != nil {
		t.Fatalf("Get tenant error = %v", err)
	}
	if tenant.Name != "Loader Tenant" {
		t.Errorf("tenant name = %q, want %q (should be original)", tenant.Name, "Loader Tenant")
	}
}
