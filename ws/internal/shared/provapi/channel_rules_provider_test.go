package provapi

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/shared/types"
)

// newTestChannelRulesProvider creates a minimal StreamChannelRulesProvider for unit testing
// without gRPC connections or Prometheus metrics.
func newTestChannelRulesProvider() *StreamChannelRulesProvider {
	return &StreamChannelRulesProvider{
		channelRules: make(map[string]*types.ChannelRules),
		logger:       zerolog.Nop(),
	}
}

func TestNewStreamChannelRulesProvider_EmptyGRPCAddr(t *testing.T) {
	t.Parallel()
	_, err := NewStreamChannelRulesProvider(StreamChannelRulesProviderConfig{
		GRPCAddr: "",
	})
	if err == nil {
		t.Fatal("expected error for empty GRPCAddr, got nil")
	}
}

func TestChannelRulesProvider_Snapshot(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
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

func TestChannelRulesProvider_SnapshotReplacesAll(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	// First snapshot
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
				ChannelRules: &provisioningv1.ChannelRules{
					PublicChannels: []string{"*.trade"},
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
				ChannelRules: &provisioningv1.ChannelRules{
					PublicChannels: []string{"*.market"},
				},
			},
		},
	})

	ctx := context.Background()

	// tenant-a should be gone
	_, err := r.GetChannelRules(ctx, "tenant-a")
	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Error("tenant-a should not exist after new snapshot")
	}

	// tenant-b should exist
	rules, err := r.GetChannelRules(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("GetChannelRules(tenant-b) error = %v", err)
	}
	if len(rules.Public) != 1 || rules.Public[0] != "*.market" {
		t.Errorf("Public = %v, want [*.market]", rules.Public)
	}
}

func TestChannelRulesProvider_DeltaRemove(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	// Load snapshot
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "tenant-a",
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

	_, err := r.GetChannelRules(ctx, "tenant-a")
	if !errors.Is(err, types.ErrChannelRulesNotFound) {
		t.Error("tenant-a channel rules should be removed")
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
