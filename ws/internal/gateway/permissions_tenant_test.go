package gateway

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/types"
)

// mustNewTenantPermissionChecker creates a TenantPermissionChecker or fails the test.
func mustNewTenantPermissionChecker(t *testing.T, provider ChannelRulesProvider, fallback *types.ChannelRules) *TenantPermissionChecker {
	t.Helper()
	checker, err := NewTenantPermissionChecker(provider, fallback, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewTenantPermissionChecker: %v", err)
	}
	return checker
}

func TestTenantPermissionChecker_CanSubscribe_WithTenantRules(t *testing.T) {
	t.Parallel()

	registry := newMockChannelRulesProvider()
	registry.channelRules["acme"] = &types.ChannelRules{
		Public: []string{"*.public", "*.metadata"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade", "*.liquidity"},
			"premium": {"*.realtime"},
		},
		Default: []string{"*.basic"},
	}

	fallback := &types.ChannelRules{
		Public: []string{"*.fallback"},
	}

	checker := mustNewTenantPermissionChecker(t, registry, fallback)

	tests := []struct {
		name    string
		claims  *auth.Claims
		channel string
		want    bool
	}{
		{
			name: "public channel allowed for any user",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{},
			},
			channel: "BTC.public",
			want:    true,
		},
		{
			name: "public metadata channel allowed",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{},
			},
			channel: "BTC.metadata",
			want:    true,
		},
		{
			name: "trader group can access trade channels",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"traders"},
			},
			channel: "BTC.trade",
			want:    true,
		},
		{
			name: "trader group can access liquidity channels",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"traders"},
			},
			channel: "ETH.liquidity",
			want:    true,
		},
		{
			name: "premium group can access realtime channels",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"premium"},
			},
			channel: "BTC.realtime",
			want:    true,
		},
		{
			name: "user with multiple groups",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"traders", "premium"},
			},
			channel: "BTC.realtime",
			want:    true,
		},
		{
			name: "user without matching group gets default",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"unknown-group"},
			},
			channel: "BTC.basic",
			want:    true,
		},
		{
			name: "user without groups gets default",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{},
			},
			channel: "BTC.basic",
			want:    true,
		},
		{
			name: "trader cannot access premium channels",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"traders"},
			},
			channel: "BTC.realtime",
			want:    false,
		},
		{
			name: "user cannot access unmatched channel",
			claims: &auth.Claims{
				TenantID: "acme",
				Groups:   []string{"traders"},
			},
			channel: "BTC.private",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checker.CanSubscribe(context.Background(), tt.claims, tt.channel)
			if got != tt.want {
				t.Errorf("CanSubscribe() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTenantPermissionChecker_CanSubscribe_FallbackRules(t *testing.T) {
	t.Parallel()

	// Registry with no rules for this tenant
	registry := newMockChannelRulesProvider()

	fallback := &types.ChannelRules{
		Public: []string{"*.fallback", "*.public"},
	}

	checker := mustNewTenantPermissionChecker(t, registry, fallback)

	tests := []struct {
		name    string
		claims  *auth.Claims
		channel string
		want    bool
	}{
		{
			name: "fallback public channel allowed",
			claims: &auth.Claims{
				TenantID: "unknown-tenant",
				Groups:   []string{},
			},
			channel: "BTC.fallback",
			want:    true,
		},
		{
			name: "fallback public pattern matches",
			claims: &auth.Claims{
				TenantID: "unknown-tenant",
				Groups:   []string{},
			},
			channel: "ETH.public",
			want:    true,
		},
		{
			name: "non-fallback channel denied",
			claims: &auth.Claims{
				TenantID: "unknown-tenant",
				Groups:   []string{},
			},
			channel: "BTC.trade",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checker.CanSubscribe(context.Background(), tt.claims, tt.channel)
			if got != tt.want {
				t.Errorf("CanSubscribe() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTenantPermissionChecker_FilterChannels(t *testing.T) {
	t.Parallel()

	registry := newMockChannelRulesProvider()
	registry.channelRules["acme"] = &types.ChannelRules{
		Public: []string{"*.public"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade"},
		},
	}

	checker := mustNewTenantPermissionChecker(t, registry, types.NewChannelRules())

	claims := &auth.Claims{
		TenantID: "acme",
		Groups:   []string{"traders"},
	}

	channels := []string{
		"BTC.public",
		"BTC.trade",
		"BTC.private",
		"ETH.public",
		"ETH.liquidity",
	}

	got := checker.FilterChannels(context.Background(), claims, channels)

	want := []string{"BTC.public", "BTC.trade", "ETH.public"}

	if len(got) != len(want) {
		t.Errorf("FilterChannels() returned %d channels, want %d", len(got), len(want))
		t.Errorf("got: %v", got)
		t.Errorf("want: %v", want)
		return
	}

	for i, ch := range got {
		if ch != want[i] {
			t.Errorf("FilterChannels()[%d] = %q, want %q", i, ch, want[i])
		}
	}
}

func TestTenantPermissionChecker_NilFallback(t *testing.T) {
	t.Parallel()

	registry := newMockChannelRulesProvider()
	// Create with nil fallback - should use empty rules
	checker := mustNewTenantPermissionChecker(t, registry, nil)

	claims := &auth.Claims{
		TenantID: "unknown",
		Groups:   []string{},
	}

	// Should deny everything since no rules and fallback is empty
	got := checker.CanSubscribe(context.Background(), claims, "any.channel")
	if got {
		t.Error("expected denial with nil fallback and no rules")
	}
}

// TestTenantPermissionChecker_CanSubscribe_NilClaims verifies that
// TenantPermissionChecker returns false for ALL channels when claims are nil,
// including channels that match public patterns. This is correct because
// API-key-only connections (which have nil claims) use the global
// PermissionChecker, not TenantPermissionChecker. TenantPermissionChecker
// requires claims to resolve the tenant and look up per-tenant rules.
func TestTenantPermissionChecker_CanSubscribe_NilClaims(t *testing.T) {
	t.Parallel()

	registry := newMockChannelRulesProvider()
	registry.channelRules["acme"] = &types.ChannelRules{
		Public: []string{"*.public", "*.metadata"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade"},
		},
		Default: []string{"*.basic"},
	}

	fallback := &types.ChannelRules{
		Public: []string{"*.fallback"},
	}

	checker := mustNewTenantPermissionChecker(t, registry, fallback)

	tests := []struct {
		name    string
		channel string
	}{
		{"public channel denied with nil claims", "BTC.public"},
		{"metadata channel denied with nil claims", "ETH.metadata"},
		{"trade channel denied with nil claims", "BTC.trade"},
		{"basic channel denied with nil claims", "BTC.basic"},
		{"fallback channel denied with nil claims", "BTC.fallback"},
		{"arbitrary channel denied with nil claims", "unknown.channel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checker.CanSubscribe(context.Background(), nil, tt.channel)
			if got {
				t.Errorf("CanSubscribe(ctx, nil, %q) = true, want false", tt.channel)
			}
		})
	}
}

// TestTenantPermissionChecker_FilterChannels_NilClaims verifies that
// FilterChannels returns nil when claims are nil, regardless of channel
// content. API-key-only connections bypass TenantPermissionChecker entirely.
func TestTenantPermissionChecker_FilterChannels_NilClaims(t *testing.T) {
	t.Parallel()

	registry := newMockChannelRulesProvider()
	registry.channelRules["acme"] = &types.ChannelRules{
		Public: []string{"*.public"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade"},
		},
	}

	fallback := &types.ChannelRules{
		Public: []string{"*.fallback"},
	}

	checker := mustNewTenantPermissionChecker(t, registry, fallback)

	channels := []string{
		"BTC.public",
		"BTC.trade",
		"BTC.fallback",
		"unknown.channel",
	}

	got := checker.FilterChannels(context.Background(), nil, channels)
	if got != nil {
		t.Errorf("FilterChannels(ctx, nil, channels) = %v, want nil", got)
	}
}

func TestDefaultChannelRules(t *testing.T) {
	t.Parallel()

	patterns := []string{"*.metadata", "*.public"}
	rules := DefaultChannelRules(patterns)

	if len(rules.Public) != len(patterns) {
		t.Errorf("expected %d public patterns, got %d", len(patterns), len(rules.Public))
	}

	for i, p := range patterns {
		if rules.Public[i] != p {
			t.Errorf("Public[%d] = %q, want %q", i, rules.Public[i], p)
		}
	}

	if len(rules.GroupMappings) != 0 {
		t.Error("expected empty GroupMappings")
	}

	if len(rules.Default) != 0 {
		t.Error("expected empty Default")
	}
}
