package gateway

import (
	"testing"

	"github.com/klurvio/sukko/internal/shared/auth"
)

func TestPermissionChecker_CanSubscribe_PublicChannels(t *testing.T) {
	t.Parallel()
	pc := NewPermissionChecker(
		[]string{"*.trade", "*.liquidity", "sukko.*"},
		[]string{},
		[]string{},
	)

	claims := &auth.Claims{
		TenantID: "tenant1",
	}
	claims.Subject = "user123"

	tests := []struct {
		name    string
		channel string
		want    bool
	}{
		{"BTC.trade matches *.trade", "BTC.trade", true},
		{"ETH.trade matches *.trade", "ETH.trade", true},
		{"SOL.liquidity matches *.liquidity", "SOL.liquidity", true},
		{"sukko.trades matches sukko.*", "sukko.trades", true},
		{"sukko.metadata matches sukko.*", "sukko.metadata", true},
		{"unknown.channel denied", "unknown.channel", false},
		{"balances.user123 denied (no user pattern)", "balances.user123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pc.CanSubscribe(claims, tt.channel)
			if got != tt.want {
				t.Errorf("CanSubscribe(%q) = %v, want %v", tt.channel, got, tt.want)
			}
		})
	}
}

func TestPermissionChecker_CanSubscribe_UserScopedChannels(t *testing.T) {
	t.Parallel()
	pc := NewPermissionChecker(
		[]string{},
		[]string{"balances.{principal}", "notifications.{principal}"},
		[]string{},
	)

	tests := []struct {
		name    string
		subject string
		channel string
		want    bool
	}{
		{"user can access own balances", "user123", "balances.user123", true},
		{"user can access own notifications", "user123", "notifications.user123", true},
		{"user cannot access other's balances", "user123", "balances.user456", false},
		{"user cannot access other's notifications", "user123", "notifications.user456", false},
		{"empty subject denied", "", "balances.user123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims := &auth.Claims{
				TenantID: "tenant1",
			}
			claims.Subject = tt.subject

			got := pc.CanSubscribe(claims, tt.channel)
			if got != tt.want {
				t.Errorf("CanSubscribe(%q) with subject=%q = %v, want %v",
					tt.channel, tt.subject, got, tt.want)
			}
		})
	}
}

func TestPermissionChecker_CanSubscribe_GroupScopedChannels(t *testing.T) {
	t.Parallel()
	pc := NewPermissionChecker(
		[]string{},
		[]string{},
		[]string{"community.{group_id}", "social.{group_id}"},
	)

	tests := []struct {
		name    string
		groups  []string
		channel string
		want    bool
	}{
		{"member can access group community", []string{"traders", "whales"}, "community.traders", true},
		{"member can access group social", []string{"traders", "whales"}, "social.whales", true},
		{"non-member denied community", []string{"traders"}, "community.whales", false},
		{"non-member denied social", []string{"traders"}, "social.whales", false},
		{"no groups denied", []string{}, "community.traders", false},
		{"nil groups denied", nil, "community.traders", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims := &auth.Claims{
				TenantID: "tenant1",
				Groups:   tt.groups,
			}
			claims.Subject = "user123"

			got := pc.CanSubscribe(claims, tt.channel)
			if got != tt.want {
				t.Errorf("CanSubscribe(%q) with groups=%v = %v, want %v",
					tt.channel, tt.groups, got, tt.want)
			}
		})
	}
}

func TestPermissionChecker_FilterChannels(t *testing.T) {
	t.Parallel()
	pc := NewPermissionChecker(
		[]string{"*.trade"},
		[]string{"balances.{principal}"},
		[]string{"community.{group_id}"},
	)

	claims := &auth.Claims{
		TenantID: "tenant1",
		Groups:   []string{"vip"},
	}
	claims.Subject = "user123"

	input := []string{
		"BTC.trade",        // allowed (public)
		"ETH.trade",        // allowed (public)
		"balances.user123", // allowed (own balance)
		"balances.user456", // denied (other's balance)
		"community.vip",    // allowed (member)
		"community.whales", // denied (not member)
		"unknown.channel",  // denied (no pattern)
	}

	expected := []string{
		"BTC.trade",
		"ETH.trade",
		"balances.user123",
		"community.vip",
	}

	got := pc.FilterChannels(claims, input)

	if len(got) != len(expected) {
		t.Errorf("FilterChannels() returned %d channels, want %d", len(got), len(expected))
		t.Errorf("Got: %v", got)
		t.Errorf("Expected: %v", expected)
		return
	}

	for i, ch := range expected {
		if got[i] != ch {
			t.Errorf("FilterChannels()[%d] = %q, want %q", i, got[i], ch)
		}
	}
}

func TestMatchWildcard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern string
		channel string
		want    bool
	}{
		// Exact match
		{"BTC.trade", "BTC.trade", true},
		{"BTC.trade", "ETH.trade", false},

		// Wildcard *
		{"*", "anything", true},
		{"*", "BTC.trade", true},

		// Prefix wildcard *.suffix
		{"*.trade", "BTC.trade", true},
		{"*.trade", "ETH.trade", true},
		{"*.trade", "BTC.liquidity", false},

		// Suffix wildcard prefix.*
		{"sukko.*", "sukko.trades", true},
		{"sukko.*", "sukko.metadata", true},
		{"sukko.*", "other.trades", false},

		// Middle wildcard prefix*suffix
		{"BTC*trade", "BTC.trade", true},
		{"BTC*trade", "BTC-USD.trade", true},
		{"BTC*trade", "ETH.trade", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.channel, func(t *testing.T) {
			t.Parallel()
			got := auth.MatchWildcard(tt.pattern, tt.channel)
			if got != tt.want {
				t.Errorf("auth.MatchWildcard(%q, %q) = %v, want %v",
					tt.pattern, tt.channel, got, tt.want)
			}
		})
	}
}

func TestMatchPatternCaptures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern      string
		channel      string
		wantCaptures map[string]string
		wantMatched  bool
	}{
		// User principal extraction
		{"balances.{principal}", "balances.user123", map[string]string{"principal": "user123"}, true},
		{"balances.{principal}", "balances.abc-def", map[string]string{"principal": "abc-def"}, true},
		{"notifications.{principal}", "notifications.xyz", map[string]string{"principal": "xyz"}, true},

		// Group ID extraction
		{"community.{group_id}", "community.traders", map[string]string{"group_id": "traders"}, true},
		{"social.{group_id}", "social.whales", map[string]string{"group_id": "whales"}, true},

		// Non-matching cases
		{"balances.{principal}", "other.user123", nil, false},
		{"community.{group_id}", "other.traders", nil, false},

		// Suffix pattern
		{"{principal}.balances", "user123.balances", map[string]string{"principal": "user123"}, true},
	}

	for _, tt := range tests {
		name := tt.pattern + "_" + tt.channel
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := auth.MatchPattern(tt.pattern, tt.channel)
			if result.Matched != tt.wantMatched {
				t.Errorf("auth.MatchPattern(%q, %q).Matched = %v, want %v",
					tt.pattern, tt.channel, result.Matched, tt.wantMatched)
			}
			if tt.wantMatched {
				for key, wantValue := range tt.wantCaptures {
					if gotValue, ok := result.Captures[key]; !ok || gotValue != wantValue {
						t.Errorf("auth.MatchPattern(%q, %q).Captures[%q] = %q, want %q",
							tt.pattern, tt.channel, key, gotValue, wantValue)
					}
				}
			}
		})
	}
}

func BenchmarkCanSubscribe_Public(b *testing.B) {
	pc := NewPermissionChecker(
		[]string{"*.trade", "*.liquidity", "*.metadata"},
		[]string{"balances.{principal}"},
		[]string{"community.{group_id}"},
	)
	claims := &auth.Claims{
		TenantID: "tenant1",
		Groups:   []string{"traders"},
	}
	claims.Subject = "user123"

	for b.Loop() {
		pc.CanSubscribe(claims, "BTC.trade")
	}
}

func BenchmarkCanSubscribe_UserScoped(b *testing.B) {
	pc := NewPermissionChecker(
		[]string{"*.trade"},
		[]string{"balances.{principal}", "notifications.{principal}"},
		[]string{"community.{group_id}"},
	)
	claims := &auth.Claims{
		TenantID: "tenant1",
	}
	claims.Subject = "user123"

	for b.Loop() {
		pc.CanSubscribe(claims, "balances.user123")
	}
}

func BenchmarkFilterChannels(b *testing.B) {
	pc := NewPermissionChecker(
		[]string{"*.trade", "*.liquidity"},
		[]string{"balances.{principal}"},
		[]string{"community.{group_id}"},
	)
	claims := &auth.Claims{
		TenantID: "tenant1",
		Groups:   []string{"vip"},
	}
	claims.Subject = "user123"

	channels := []string{
		"BTC.trade", "ETH.trade", "SOL.liquidity",
		"balances.user123", "balances.other",
		"community.vip", "community.other",
	}

	for b.Loop() {
		pc.FilterChannels(claims, channels)
	}
}
