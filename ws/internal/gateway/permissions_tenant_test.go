package gateway

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

func TestTenantPermissionChecker_CanSubscribe_WithTenantRules(t *testing.T) {
	t.Parallel()

	registry := newMockTenantRegistry()
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

	checker := NewTenantPermissionChecker(registry, fallback, zerolog.Nop())

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
	registry := newMockTenantRegistry()

	fallback := &types.ChannelRules{
		Public: []string{"*.fallback", "*.public"},
	}

	checker := NewTenantPermissionChecker(registry, fallback, zerolog.Nop())

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
			got := checker.CanSubscribe(context.Background(), tt.claims, tt.channel)
			if got != tt.want {
				t.Errorf("CanSubscribe() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTenantPermissionChecker_FilterChannels(t *testing.T) {
	t.Parallel()

	registry := newMockTenantRegistry()
	registry.channelRules["acme"] = &types.ChannelRules{
		Public: []string{"*.public"},
		GroupMappings: map[string][]string{
			"traders": {"*.trade"},
		},
	}

	checker := NewTenantPermissionChecker(registry, types.NewChannelRules(), zerolog.Nop())

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

	registry := newMockTenantRegistry()
	// Create with nil fallback - should use empty rules
	checker := NewTenantPermissionChecker(registry, nil, zerolog.Nop())

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
