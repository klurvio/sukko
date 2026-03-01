package configstore

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

func testConfigFile() *ConfigFile {
	active := true
	return &ConfigFile{
		Tenants: []TenantConfig{
			{
				ID:           "tenant-a",
				Name:         "Tenant A",
				ConsumerType: "shared",
				Categories: []CategoryConfig{
					{Name: "trade", Partitions: 3, RetentionMs: 86400000},
					{Name: "order", Partitions: 1},
				},
				Keys: []KeyConfig{
					{ID: "key-one", Algorithm: "ES256", PublicKey: "pem-data", Active: &active},
				},
				Quotas: &QuotaConfig{
					MaxTopics:      10,
					MaxConnections: 500,
				},
				OIDC: &OIDCConfig{
					IssuerURL: "https://auth.example.com",
					Audience:  "my-api",
				},
				ChannelRules: &ChannelRulesConfig{
					PublicChannels:  []string{"*.trade"},
					DefaultChannels: []string{"news"},
					GroupMappings: map[string][]string{
						"admin": {"admin.*"},
					},
				},
			},
		},
	}
}

func buildTestStores(t *testing.T) *ConfigStores {
	t.Helper()
	return BuildStores(testConfigFile(), zerolog.Nop())
}

func TestTenantStore_Get(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ts := stores.TenantStore()

	t.Run("existing tenant", func(t *testing.T) {
		t.Parallel()
		tenant, err := ts.Get(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if tenant.ID != "tenant-a" {
			t.Errorf("ID = %q, want %q", tenant.ID, "tenant-a")
		}
		if tenant.Name != "Tenant A" {
			t.Errorf("Name = %q, want %q", tenant.Name, "Tenant A")
		}
		if tenant.Status != provisioning.StatusActive {
			t.Errorf("Status = %q, want %q", tenant.Status, provisioning.StatusActive)
		}
	})

	t.Run("unknown tenant", func(t *testing.T) {
		t.Parallel()
		_, err := ts.Get(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown tenant")
		}
	})
}

func TestTenantStore_WriteOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ts := stores.TenantStore()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Create", func() error { return ts.Create(ctx, &provisioning.Tenant{}) }},
		{"Update", func() error { return ts.Update(ctx, &provisioning.Tenant{}) }},
		{"UpdateStatus", func() error { return ts.UpdateStatus(ctx, "x", provisioning.StatusActive) }},
		{"SetDeprovisionAt", func() error { return ts.SetDeprovisionAt(ctx, "x", nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.fn(); !errors.Is(err, ErrReadOnlyMode) {
				t.Errorf("expected ErrReadOnlyMode, got %v", err)
			}
		})
	}
}

func TestTenantStore_List(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ts := stores.TenantStore()

	tenants, total, err := ts.List(ctx, provisioning.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(tenants) != 1 {
		t.Errorf("len(tenants) = %d, want 1", len(tenants))
	}
}

func TestKeyStore_Get(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ks := stores.KeyStore()

	t.Run("existing key", func(t *testing.T) {
		t.Parallel()
		key, err := ks.Get(ctx, "key-one")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if key.KeyID != "key-one" {
			t.Errorf("KeyID = %q, want %q", key.KeyID, "key-one")
		}
		if key.TenantID != "tenant-a" {
			t.Errorf("TenantID = %q, want %q", key.TenantID, "tenant-a")
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		t.Parallel()
		_, err := ks.Get(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
	})
}

func TestKeyStore_ListByTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ks := stores.KeyStore()

	keys, err := ks.ListByTenant(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("len(keys) = %d, want 1", len(keys))
	}
}

func TestKeyStore_GetActiveKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ks := stores.KeyStore()

	keys, err := ks.GetActiveKeys(ctx)
	if err != nil {
		t.Fatalf("GetActiveKeys() error = %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("len(keys) = %d, want 1", len(keys))
	}
}

func TestKeyStore_WriteOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ks := stores.KeyStore()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Create", func() error { return ks.Create(ctx, &provisioning.TenantKey{}) }},
		{"Revoke", func() error { return ks.Revoke(ctx, "x") }},
		{"RevokeAllForTenant", func() error { return ks.RevokeAllForTenant(ctx, "x") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.fn(); !errors.Is(err, ErrReadOnlyMode) {
				t.Errorf("expected ErrReadOnlyMode, got %v", err)
			}
		})
	}
}

func TestTopicStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	ts := stores.TopicStore()

	t.Run("ListByTenant", func(t *testing.T) {
		t.Parallel()
		topics, err := ts.ListByTenant(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("ListByTenant() error = %v", err)
		}
		if len(topics) != 2 {
			t.Errorf("len(topics) = %d, want 2", len(topics))
		}
	})

	t.Run("CountByTenant", func(t *testing.T) {
		t.Parallel()
		count, err := ts.CountByTenant(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("CountByTenant() error = %v", err)
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
	})

	t.Run("CountPartitionsByTenant", func(t *testing.T) {
		t.Parallel()
		count, err := ts.CountPartitionsByTenant(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("CountPartitionsByTenant() error = %v", err)
		}
		if count != 4 { // 3 + 1
			t.Errorf("partitions = %d, want 4", count)
		}
	})

	t.Run("Create returns ErrReadOnlyMode", func(t *testing.T) {
		t.Parallel()
		if err := ts.Create(ctx, &provisioning.TenantTopic{}); !errors.Is(err, ErrReadOnlyMode) {
			t.Errorf("expected ErrReadOnlyMode, got %v", err)
		}
	})
}

func TestQuotaStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	qs := stores.QuotaStore()

	t.Run("Get existing quota", func(t *testing.T) {
		t.Parallel()
		quota, err := qs.Get(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if quota.MaxTopics != 10 {
			t.Errorf("MaxTopics = %d, want 10", quota.MaxTopics)
		}
		if quota.MaxConnections != 500 {
			t.Errorf("MaxConnections = %d, want 500", quota.MaxConnections)
		}
	})

	t.Run("Get unknown quota", func(t *testing.T) {
		t.Parallel()
		_, err := qs.Get(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown quota")
		}
	})

	t.Run("Create returns ErrReadOnlyMode", func(t *testing.T) {
		t.Parallel()
		if err := qs.Create(ctx, &provisioning.TenantQuota{}); !errors.Is(err, ErrReadOnlyMode) {
			t.Errorf("expected ErrReadOnlyMode, got %v", err)
		}
	})

	t.Run("Update returns ErrReadOnlyMode", func(t *testing.T) {
		t.Parallel()
		if err := qs.Update(ctx, &provisioning.TenantQuota{}); !errors.Is(err, ErrReadOnlyMode) {
			t.Errorf("expected ErrReadOnlyMode, got %v", err)
		}
	})
}

func TestAuditStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	as := stores.AuditStore()

	t.Run("Log returns nil (noop)", func(t *testing.T) {
		t.Parallel()
		err := as.Log(ctx, &provisioning.AuditEntry{
			TenantID: "tenant-a",
			Action:   "test",
			Actor:    "tester",
		})
		if err != nil {
			t.Errorf("Log() error = %v", err)
		}
	})

	t.Run("ListByTenant returns empty", func(t *testing.T) {
		t.Parallel()
		entries, total, err := as.ListByTenant(ctx, "tenant-a", provisioning.ListOptions{})
		if err != nil {
			t.Fatalf("ListByTenant() error = %v", err)
		}
		if total != 0 {
			t.Errorf("total = %d, want 0", total)
		}
		if len(entries) != 0 {
			t.Errorf("len(entries) = %d, want 0", len(entries))
		}
	})
}

func TestOIDCConfigStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	os := stores.OIDCConfigStore()

	t.Run("Get existing config", func(t *testing.T) {
		t.Parallel()
		cfg, err := os.Get(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if cfg.IssuerURL != "https://auth.example.com" {
			t.Errorf("IssuerURL = %q, want %q", cfg.IssuerURL, "https://auth.example.com")
		}
	})

	t.Run("Get unknown config", func(t *testing.T) {
		t.Parallel()
		_, err := os.Get(ctx, "nonexistent")
		if !errors.Is(err, types.ErrOIDCNotConfigured) {
			t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
		}
	})

	t.Run("GetByIssuer", func(t *testing.T) {
		t.Parallel()
		cfg, err := os.GetByIssuer(ctx, "https://auth.example.com")
		if err != nil {
			t.Fatalf("GetByIssuer() error = %v", err)
		}
		if cfg.TenantID != "tenant-a" {
			t.Errorf("TenantID = %q, want %q", cfg.TenantID, "tenant-a")
		}
	})

	t.Run("GetByIssuer unknown", func(t *testing.T) {
		t.Parallel()
		_, err := os.GetByIssuer(ctx, "https://unknown.example.com")
		if !errors.Is(err, types.ErrIssuerNotFound) {
			t.Errorf("expected ErrIssuerNotFound, got %v", err)
		}
	})

	t.Run("Create returns ErrReadOnlyMode", func(t *testing.T) {
		t.Parallel()
		if err := os.Create(ctx, &types.TenantOIDCConfig{}); !errors.Is(err, ErrReadOnlyMode) {
			t.Errorf("expected ErrReadOnlyMode, got %v", err)
		}
	})

	t.Run("ListEnabled", func(t *testing.T) {
		t.Parallel()
		configs, err := os.ListEnabled(ctx)
		if err != nil {
			t.Fatalf("ListEnabled() error = %v", err)
		}
		if len(configs) != 1 {
			t.Errorf("len(configs) = %d, want 1", len(configs))
		}
	})
}

func TestChannelRulesStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stores := buildTestStores(t)
	crs := stores.ChannelRulesStore()

	t.Run("Get existing rules", func(t *testing.T) {
		t.Parallel()
		rules, err := crs.Get(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if rules.TenantID != "tenant-a" {
			t.Errorf("TenantID = %q, want %q", rules.TenantID, "tenant-a")
		}
	})

	t.Run("Get unknown rules", func(t *testing.T) {
		t.Parallel()
		_, err := crs.Get(ctx, "nonexistent")
		if !errors.Is(err, types.ErrChannelRulesNotFound) {
			t.Errorf("expected ErrChannelRulesNotFound, got %v", err)
		}
	})

	t.Run("GetRules returns typed rules", func(t *testing.T) {
		t.Parallel()
		rules, err := crs.GetRules(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("GetRules() error = %v", err)
		}
		if len(rules.Public) != 1 || rules.Public[0] != "*.trade" {
			t.Errorf("Public = %v, want [*.trade]", rules.Public)
		}
		if len(rules.Default) != 1 || rules.Default[0] != "news" {
			t.Errorf("Default = %v, want [news]", rules.Default)
		}
		if len(rules.GroupMappings) != 1 {
			t.Errorf("GroupMappings len = %d, want 1", len(rules.GroupMappings))
		}
	})

	t.Run("Create returns ErrReadOnlyMode", func(t *testing.T) {
		t.Parallel()
		if err := crs.Create(ctx, "x", &types.ChannelRules{}); !errors.Is(err, ErrReadOnlyMode) {
			t.Errorf("expected ErrReadOnlyMode, got %v", err)
		}
	})

	t.Run("List returns all rules", func(t *testing.T) {
		t.Parallel()
		rules, err := crs.List(ctx)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(rules) != 1 {
			t.Errorf("len(rules) = %d, want 1", len(rules))
		}
	})
}
