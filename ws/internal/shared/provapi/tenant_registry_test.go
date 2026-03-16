package provapi

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	provisioningv1 "github.com/Toniq-Labs/odin-ws/gen/proto/odin/provisioning/v1"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// newTestTenantRegistry creates a minimal StreamTenantRegistry for unit testing
// without gRPC connections or Prometheus metrics.
func newTestTenantRegistry() *StreamTenantRegistry {
	return &StreamTenantRegistry{
		issuerToTenant: make(map[string]string),
		oidcConfigs:    make(map[string]*types.TenantOIDCConfig),
		channelRules:   make(map[string]*types.ChannelRules),
		logger:         zerolog.Nop(),
	}
}

func TestTenantRegistry_Snapshot(t *testing.T) {
	t.Parallel()
	r := newTestTenantRegistry()

	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
				Oidc: &provisioningv1.OIDCConfig{
					IssuerUrl: "https://auth.example.com",
					JwksUrl:   "https://auth.example.com/.well-known/jwks.json",
					Audience:  "my-api",
					Enabled:   true,
				},
				ChannelRules: &provisioningv1.ChannelRules{
					PublicChannels:  []string{"*.trade"},
					DefaultChannels: []string{"news"},
					GroupMappings: map[string]*provisioningv1.GroupChannels{
						"admin": {Channels: []string{"admin.*"}},
					},
				},
			},
		},
	})

	ctx := context.Background()

	t.Run("GetTenantByIssuer", func(t *testing.T) {
		t.Parallel()
		tenantID, err := r.GetTenantByIssuer(ctx, "https://auth.example.com")
		if err != nil {
			t.Fatalf("GetTenantByIssuer() error = %v", err)
		}
		if tenantID != "tenant-a" {
			t.Errorf("tenantID = %q, want %q", tenantID, "tenant-a")
		}
	})

	t.Run("GetTenantByIssuer not found", func(t *testing.T) {
		t.Parallel()
		_, err := r.GetTenantByIssuer(ctx, "https://unknown.example.com")
		if !errors.Is(err, types.ErrIssuerNotFound) {
			t.Errorf("expected ErrIssuerNotFound, got %v", err)
		}
	})

	t.Run("GetOIDCConfig", func(t *testing.T) {
		t.Parallel()
		cfg, err := r.GetOIDCConfig(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("GetOIDCConfig() error = %v", err)
		}
		if cfg.IssuerURL != "https://auth.example.com" {
			t.Errorf("IssuerURL = %q, want %q", cfg.IssuerURL, "https://auth.example.com")
		}
		if cfg.Audience != "my-api" {
			t.Errorf("Audience = %q, want %q", cfg.Audience, "my-api")
		}
	})

	t.Run("GetOIDCConfig not found", func(t *testing.T) {
		t.Parallel()
		_, err := r.GetOIDCConfig(ctx, "nonexistent")
		if !errors.Is(err, types.ErrOIDCNotConfigured) {
			t.Errorf("expected ErrOIDCNotConfigured, got %v", err)
		}
	})

	t.Run("GetChannelRules", func(t *testing.T) {
		t.Parallel()
		rules, err := r.GetChannelRules(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("GetChannelRules() error = %v", err)
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

	t.Run("GetChannelRules not found", func(t *testing.T) {
		t.Parallel()
		_, err := r.GetChannelRules(ctx, "nonexistent")
		if !errors.Is(err, types.ErrChannelRulesNotFound) {
			t.Errorf("expected ErrChannelRulesNotFound, got %v", err)
		}
	})
}

func TestTenantRegistry_SnapshotReplacesAll(t *testing.T) {
	t.Parallel()
	r := newTestTenantRegistry()

	// First snapshot
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
				Oidc: &provisioningv1.OIDCConfig{
					IssuerUrl: "https://auth-a.example.com",
					Enabled:   true,
				},
			},
		},
	})

	// Second snapshot replaces all
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-b",
				Oidc: &provisioningv1.OIDCConfig{
					IssuerUrl: "https://auth-b.example.com",
					Enabled:   true,
				},
			},
		},
	})

	ctx := context.Background()

	// tenant-a should be gone
	_, err := r.GetOIDCConfig(ctx, "tenant-a")
	if !errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Error("tenant-a should not exist after new snapshot")
	}

	// tenant-b should exist
	cfg, err := r.GetOIDCConfig(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("GetOIDCConfig(tenant-b) error = %v", err)
	}
	if cfg.IssuerURL != "https://auth-b.example.com" {
		t.Errorf("IssuerURL = %q, want %q", cfg.IssuerURL, "https://auth-b.example.com")
	}
}

func TestTenantRegistry_DeltaRemove(t *testing.T) {
	t.Parallel()
	r := newTestTenantRegistry()

	// Load snapshot
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
				Oidc: &provisioningv1.OIDCConfig{
					IssuerUrl: "https://auth.example.com",
					Enabled:   true,
				},
				ChannelRules: &provisioningv1.ChannelRules{
					PublicChannels: []string{"*.trade"},
				},
			},
		},
	})

	// Delta: remove tenant-a
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot:       false,
		RemovedTenantIds: []string{"tenant-a"},
	})

	ctx := context.Background()

	_, err := r.GetOIDCConfig(ctx, "tenant-a")
	if !errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Error("tenant-a OIDC should be removed")
	}

	_, err = r.GetChannelRules(ctx, "tenant-a")
	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Error("tenant-a channel rules should be removed")
	}

	_, err = r.GetTenantByIssuer(ctx, "https://auth.example.com")
	if !errors.Is(err, types.ErrIssuerNotFound) {
		t.Error("issuer mapping should be removed")
	}
}

func TestTenantRegistry_OIDCDisabled(t *testing.T) {
	t.Parallel()
	r := newTestTenantRegistry()

	// Load snapshot with OIDC enabled
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
				Oidc: &provisioningv1.OIDCConfig{
					IssuerUrl: "https://auth.example.com",
					Enabled:   true,
				},
			},
		},
	})

	// Delta: update with OIDC disabled
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: false,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
				Oidc: &provisioningv1.OIDCConfig{
					IssuerUrl: "https://auth.example.com",
					Enabled:   false,
				},
			},
		},
	})

	ctx := context.Background()

	// OIDC should be removed when disabled
	_, err := r.GetOIDCConfig(ctx, "tenant-a")
	if !errors.Is(err, types.ErrOIDCNotConfigured) {
		t.Error("disabled OIDC should be treated as not configured")
	}
}

func TestProtoToChannelRules(t *testing.T) {
	t.Parallel()

	t.Run("full rules", func(t *testing.T) {
		t.Parallel()
		cr := &provisioningv1.ChannelRules{
			PublicChannels:  []string{"*.trade", "*.market"},
			DefaultChannels: []string{"news", "alerts"},
			GroupMappings: map[string]*provisioningv1.GroupChannels{
				"admin":  {Channels: []string{"admin.*"}},
				"trader": {Channels: []string{"trade.*", "order.*"}},
			},
		}

		rules := protoToChannelRules(cr)

		if len(rules.Public) != 2 {
			t.Errorf("Public len = %d, want 2", len(rules.Public))
		}
		if len(rules.Default) != 2 {
			t.Errorf("Default len = %d, want 2", len(rules.Default))
		}
		if len(rules.GroupMappings) != 2 {
			t.Errorf("GroupMappings len = %d, want 2", len(rules.GroupMappings))
		}
	})

	t.Run("empty group mappings", func(t *testing.T) {
		t.Parallel()
		cr := &provisioningv1.ChannelRules{
			PublicChannels: []string{"*.trade"},
		}

		rules := protoToChannelRules(cr)

		if rules.GroupMappings != nil {
			t.Error("GroupMappings should be nil when empty")
		}
	})
}
