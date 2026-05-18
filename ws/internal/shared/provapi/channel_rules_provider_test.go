package provapi

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/types"
)

// newTestChannelRulesProvider creates a minimal StreamChannelRulesProvider for unit testing
// without gRPC connections or Prometheus metrics.
func newTestChannelRulesProvider() *StreamChannelRulesProvider {
	r := &StreamChannelRulesProvider{
		channelRules: make(map[string]*types.ChannelRules),
		logger:       zerolog.Nop(),
	}
	r.routingSnapshots.Store(make(map[string]TenantRoutingSnapshot))
	return r
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

// =============================================================================
// Routing snapshot tests
// =============================================================================

func TestGetRoutingSnapshot_NotFound(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	_, ok := r.GetRoutingSnapshot("nonexistent")
	if ok {
		t.Error("GetRoutingSnapshot should return ok=false for unknown tenant")
	}
}

func TestGetRoutingSnapshot_FoundAfterUpdate(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{
				TenantId: "acme",
				RoutingRules: []*provisioningv1.TopicRoutingRule{
					{Pattern: "acme.*.trade", Topics: []string{"trades"}, Priority: 1},
				},
			},
		},
	})

	snap, ok := r.GetRoutingSnapshot("acme")
	if !ok {
		t.Fatal("GetRoutingSnapshot should return ok=true after updateTenantConfigs")
	}
	if len(snap.Rules) != 1 {
		t.Errorf("Rules len = %d, want 1", len(snap.Rules))
	}
	// NormalizePattern converts bare * to ** before storage.
	if snap.Rules[0].Pattern != "acme.**.trade" {
		t.Errorf("Rules[0].Pattern = %q, want %q", snap.Rules[0].Pattern, "acme.**.trade")
	}
}

func TestUpdateEdition_PropagatesEditionToAllTenants(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	// Seed two tenants.
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{TenantId: "acme"},
			{TenantId: "globex"},
		},
	})

	// Both start as Community (zero value).
	r.UpdateEdition(license.Pro)

	for _, id := range []string{"acme", "globex"} {
		snap, ok := r.GetRoutingSnapshot(id)
		if !ok {
			t.Errorf("tenant %q missing after UpdateEdition", id)
			continue
		}
		if snap.Edition != license.Pro {
			t.Errorf("tenant %q Edition = %q, want Pro", id, snap.Edition)
		}
	}
}

func TestUpdateEdition_COWDoesNotMutatePrior(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants:    []*provisioningv1.TenantConfig{{TenantId: "acme"}},
	})

	// Set an explicit edition so before.Edition is a well-defined value (zero-value Edition is "").
	r.UpdateEdition(license.Community)

	// Capture snapshot before edition change.
	before, _ := r.GetRoutingSnapshot("acme")

	r.UpdateEdition(license.Enterprise)

	after, _ := r.GetRoutingSnapshot("acme")

	// Before snapshot must not have been mutated.
	if before.Edition != license.Community {
		t.Errorf("before Edition = %q, want Community (COW must not mutate old map)", before.Edition)
	}
	if after.Edition != license.Enterprise {
		t.Errorf("after Edition = %q, want Enterprise", after.Edition)
	}
}

func TestGetRoutingSnapshot_ConcurrentReadsDontRace(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants:    []*provisioningv1.TenantConfig{{TenantId: "acme"}},
	})

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			r.GetRoutingSnapshot("acme")
			r.GetRoutingSnapshot("missing")
		}()
	}
	wg.Wait()
}

func TestUpdateEdition_ConcurrentWithReads_NoRace(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants:    []*provisioningv1.TenantConfig{{TenantId: "acme"}},
	})

	var wg sync.WaitGroup

	// Writer.
	wg.Go(func() {
		for range 50 {
			r.UpdateEdition(license.Pro)
			r.UpdateEdition(license.Community)
		}
	})

	// Readers.
	for range 10 {
		wg.Go(func() {
			for range 50 {
				r.GetRoutingSnapshot("acme")
			}
		})
	}

	wg.Wait()
}

func TestSnapshotReplace_RoutingSnapshotsTrimmedForRemovedTenants(t *testing.T) {
	t.Parallel()
	r := newTestChannelRulesProvider()

	// Initial snapshot with two tenants.
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants: []*provisioningv1.TenantConfig{
			{TenantId: "acme"},
			{TenantId: "globex"},
		},
	})

	// New snapshot with only one tenant.
	r.updateTenantConfigs(&provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants:    []*provisioningv1.TenantConfig{{TenantId: "globex"}},
	})

	_, acmeOk := r.GetRoutingSnapshot("acme")
	if acmeOk {
		t.Error("acme snapshot should be removed after new full snapshot")
	}

	_, globexOk := r.GetRoutingSnapshot("globex")
	if !globexOk {
		t.Error("globex snapshot should remain")
	}
}
