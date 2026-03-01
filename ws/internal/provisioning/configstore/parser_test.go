package configstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBytes_ValidYAML(t *testing.T) {
	t.Parallel()

	yaml := `
tenants:
  - id: test-tenant
    name: Test Tenant
    consumer_type: shared
    categories:
      - name: trade
        partitions: 3
        retention_ms: 86400000
    keys:
      - id: key-one
        algorithm: ES256
        public_key: "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----"
    quotas:
      max_topics: 10
      max_connections: 100
    oidc:
      issuer_url: https://auth.example.com
      audience: my-api
    channel_rules:
      public_channels:
        - "*.trade"
      default_channels:
        - "news"
`

	cfg, err := ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseBytes() error = %v", err)
	}
	if len(cfg.Tenants) != 1 {
		t.Fatalf("expected 1 tenant, got %d", len(cfg.Tenants))
	}

	tc := cfg.Tenants[0]
	if tc.ID != "test-tenant" {
		t.Errorf("tenant ID = %q, want %q", tc.ID, "test-tenant")
	}
	if tc.Name != "Test Tenant" {
		t.Errorf("tenant name = %q, want %q", tc.Name, "Test Tenant")
	}
	if tc.ConsumerType != "shared" {
		t.Errorf("consumer_type = %q, want %q", tc.ConsumerType, "shared")
	}
	if len(tc.Categories) != 1 {
		t.Fatalf("expected 1 category, got %d", len(tc.Categories))
	}
	if tc.Categories[0].Name != "trade" {
		t.Errorf("category name = %q, want %q", tc.Categories[0].Name, "trade")
	}
	if len(tc.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(tc.Keys))
	}
	if tc.Quotas == nil {
		t.Fatal("expected quotas, got nil")
	}
	if tc.Quotas.MaxTopics != 10 {
		t.Errorf("max_topics = %d, want %d", tc.Quotas.MaxTopics, 10)
	}
	if tc.OIDC == nil {
		t.Fatal("expected OIDC config, got nil")
	}
	if tc.OIDC.IssuerURL != "https://auth.example.com" {
		t.Errorf("issuer_url = %q, want %q", tc.OIDC.IssuerURL, "https://auth.example.com")
	}
	if tc.ChannelRules == nil {
		t.Fatal("expected channel rules, got nil")
	}
	if len(tc.ChannelRules.PublicChannels) != 1 {
		t.Errorf("expected 1 public channel, got %d", len(tc.ChannelRules.PublicChannels))
	}
}

func TestParseBytes_EmptyYAML(t *testing.T) {
	t.Parallel()

	cfg, err := ParseBytes([]byte(""))
	if err != nil {
		t.Fatalf("ParseBytes() error = %v", err)
	}
	if len(cfg.Tenants) != 0 {
		t.Errorf("expected 0 tenants, got %d", len(cfg.Tenants))
	}
}

func TestParseBytes_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := ParseBytes([]byte("{{invalid: yaml: ["))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseFile_Success(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yaml := `
tenants:
  - id: file-tenant
    name: File Tenant
    categories:
      - name: trade
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if len(cfg.Tenants) != 1 {
		t.Fatalf("expected 1 tenant, got %d", len(cfg.Tenants))
	}
	if cfg.Tenants[0].ID != "file-tenant" {
		t.Errorf("tenant ID = %q, want %q", cfg.Tenants[0].ID, "file-tenant")
	}
}

func TestParseFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ParseFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
