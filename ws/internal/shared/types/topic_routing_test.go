package types

import (
	"errors"
	"strings"
	"testing"
)

// stubMatchWildcard is a simple wildcard matcher for tests, mirroring auth.MatchWildcard
// without importing the auth package.
func stubMatchWildcard(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if pattern == "*" {
		return true
	}
	if after, found := strings.CutPrefix(pattern, "*"); found {
		return strings.HasSuffix(value, after)
	}
	if before, found := strings.CutSuffix(pattern, "*"); found {
		return strings.HasPrefix(value, before)
	}
	if idx := strings.Index(pattern, "*"); idx > 0 {
		prefix := pattern[:idx]
		suffix := pattern[idx+1:]
		return strings.HasPrefix(value, prefix) && strings.HasSuffix(value, suffix)
	}
	return false
}

func TestValidateRoutingRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rules        []TopicRoutingRule
		wantSentinel error
		wantContains string // additional substring check (e.g. index prefix)
	}{
		{
			name: "valid_single_rule",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "trade"},
			},
		},
		{
			name: "valid_multiple_rules",
			rules: []TopicRoutingRule{
				{Pattern: "crypto.*.trade", TopicSuffix: "crypto.trade"},
				{Pattern: "*.trade", TopicSuffix: "trade"},
				{Pattern: "*.analytics", TopicSuffix: "analytics"},
			},
		},
		{
			name: "valid_exact_pattern",
			rules: []TopicRoutingRule{
				{Pattern: "BTC.trade", TopicSuffix: "btc-trade"},
			},
		},
		{
			name:         "empty_rules",
			rules:        []TopicRoutingRule{},
			wantSentinel: ErrEmptyRoutingRules,
		},
		{
			name: "empty_pattern",
			rules: []TopicRoutingRule{
				{Pattern: "", TopicSuffix: "trade"},
			},
			wantSentinel: ErrEmptyRoutingPattern,
		},
		{
			name: "empty_suffix",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: ""},
			},
			wantSentinel: ErrEmptyTopicSuffix,
		},
		{
			name: "wildcard_in_suffix",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "*.trade"},
			},
			wantSentinel: ErrInvalidTopicSuffix,
		},
		{
			name: "placeholder_in_pattern",
			rules: []TopicRoutingRule{
				{Pattern: "{principal}.trade", TopicSuffix: "trade"},
			},
			wantSentinel: ErrRoutingPlaceholderForbid,
		},
		{
			name: "opening_brace_in_pattern",
			rules: []TopicRoutingRule{
				{Pattern: "{broken.trade", TopicSuffix: "trade"},
			},
			wantSentinel: ErrRoutingPlaceholderForbid,
		},
		{
			name: "closing_brace_in_pattern",
			rules: []TopicRoutingRule{
				{Pattern: "broken}.trade", TopicSuffix: "trade"},
			},
			wantSentinel: ErrRoutingPlaceholderForbid,
		},
		{
			name: "invalid_pattern_chars",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade!", TopicSuffix: "trade"},
			},
			wantSentinel: ErrInvalidRoutingPattern,
		},
		{
			name: "invalid_suffix_chars",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "trade!"},
			},
			wantSentinel: ErrInvalidTopicSuffix,
		},
		{
			name: "second_rule_invalid",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "trade"},
				{Pattern: "", TopicSuffix: "analytics"},
			},
			wantSentinel: ErrEmptyRoutingPattern,
			wantContains: "routing rule 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRoutingRules(tt.rules)

			if tt.wantSentinel == nil {
				if err != nil {
					t.Errorf("ValidateRoutingRules() unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("ValidateRoutingRules() expected error %v, got nil", tt.wantSentinel)
				return
			}

			if !errors.Is(err, tt.wantSentinel) {
				t.Errorf("ValidateRoutingRules() error = %v, want sentinel %v", err, tt.wantSentinel)
			}

			if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("ValidateRoutingRules() error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestResolveTopicSuffix(t *testing.T) {
	t.Parallel()

	rules := []TopicRoutingRule{
		{Pattern: "crypto.*.trade", TopicSuffix: "crypto.trade"},
		{Pattern: "nft.*.trade", TopicSuffix: "nft.trade"},
		{Pattern: "*.trade", TopicSuffix: "trade"},
		{Pattern: "*.analytics", TopicSuffix: "analytics"},
	}

	tests := []struct {
		name          string
		rules         []TopicRoutingRule
		channelSuffix string
		wantSuffix    string
		wantErr       error
	}{
		{
			name:          "first_match_specific",
			rules:         rules,
			channelSuffix: "crypto.BTC.trade",
			wantSuffix:    "crypto.trade",
		},
		{
			name:          "first_match_nft",
			rules:         rules,
			channelSuffix: "nft.punk.trade",
			wantSuffix:    "nft.trade",
		},
		{
			name:          "catch_all_trade",
			rules:         rules,
			channelSuffix: "forex.EUR.trade",
			wantSuffix:    "trade",
		},
		{
			name:          "analytics_match",
			rules:         rules,
			channelSuffix: "crypto.BTC.analytics",
			wantSuffix:    "analytics",
		},
		{
			name:          "exact_match",
			rules:         []TopicRoutingRule{{Pattern: "BTC.trade", TopicSuffix: "btc-trade"}},
			channelSuffix: "BTC.trade",
			wantSuffix:    "btc-trade",
		},
		{
			name:          "multi_segment_wildcard",
			rules:         rules,
			channelSuffix: "crypto.nft.BTC.trade",
			wantSuffix:    "crypto.trade",
		},
		{
			name:          "no_match",
			rules:         rules,
			channelSuffix: "unknown.data",
			wantErr:       ErrNoMatchingRoute,
		},
		{
			name:          "empty_rules",
			rules:         []TopicRoutingRule{},
			channelSuffix: "BTC.trade",
			wantErr:       ErrNoRoutingRules,
		},
		{
			name:          "nil_rules",
			rules:         nil,
			channelSuffix: "BTC.trade",
			wantErr:       ErrNoRoutingRules,
		},
		{
			name:          "first_match_ordering",
			rules:         rules,
			channelSuffix: "crypto.ETH.trade",
			wantSuffix:    "crypto.trade", // crypto.*.trade matches before *.trade
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			suffix, err := ResolveTopicSuffix(tt.rules, tt.channelSuffix, stubMatchWildcard)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("ResolveTopicSuffix() expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ResolveTopicSuffix() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ResolveTopicSuffix() unexpected error: %v", err)
				return
			}

			if suffix != tt.wantSuffix {
				t.Errorf("ResolveTopicSuffix() = %q, want %q", suffix, tt.wantSuffix)
			}
		})
	}
}

func TestValidateRoutingRules_DuplicatePattern(t *testing.T) {
	t.Parallel()

	rules := []TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
		{Pattern: "*.orderbook", TopicSuffix: "orderbook"},
		{Pattern: "*.trade", TopicSuffix: "trade-alt"}, // duplicate of rule 0
	}

	err := ValidateRoutingRules(rules)
	if err == nil {
		t.Fatal("expected error for duplicate pattern, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate pattern") {
		t.Errorf("expected 'duplicate pattern' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "*.trade") {
		t.Errorf("expected pattern name in error, got: %v", err)
	}
}

func TestUniqueTopicSuffixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rules []TopicRoutingRule
		want  []string
	}{
		{
			name:  "nil_rules",
			rules: nil,
			want:  []string{},
		},
		{
			name:  "empty_rules",
			rules: []TopicRoutingRule{},
			want:  []string{},
		},
		{
			name: "single_rule",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "trade"},
			},
			want: []string{"trade"},
		},
		{
			name: "distinct_suffixes",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "trade"},
				{Pattern: "*.orderbook", TopicSuffix: "orderbook"},
				{Pattern: "*.analytics", TopicSuffix: "analytics"},
			},
			want: []string{"trade", "orderbook", "analytics"},
		},
		{
			name: "duplicate_suffixes_deduped",
			rules: []TopicRoutingRule{
				{Pattern: "crypto.*.trade", TopicSuffix: "trade"},
				{Pattern: "forex.*.trade", TopicSuffix: "trade"},
				{Pattern: "*.orderbook", TopicSuffix: "orderbook"},
			},
			want: []string{"trade", "orderbook"},
		},
		{
			name: "all_same_suffix",
			rules: []TopicRoutingRule{
				{Pattern: "*.trade", TopicSuffix: "default"},
				{Pattern: "*.orderbook", TopicSuffix: "default"},
				{Pattern: "*.analytics", TopicSuffix: "default"},
			},
			want: []string{"default"},
		},
		{
			name: "preserves_order",
			rules: []TopicRoutingRule{
				{Pattern: "*.z", TopicSuffix: "zebra"},
				{Pattern: "*.a", TopicSuffix: "alpha"},
				{Pattern: "*.m", TopicSuffix: "mid"},
				{Pattern: "*.z2", TopicSuffix: "zebra"}, // duplicate — should not appear again
			},
			want: []string{"zebra", "alpha", "mid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := UniqueTopicSuffixes(tt.rules)

			if len(got) != len(tt.want) {
				t.Fatalf("UniqueTopicSuffixes() returned %d items, want %d; got %v", len(got), len(tt.want), got)
			}

			for i, s := range got {
				if s != tt.want[i] {
					t.Errorf("UniqueTopicSuffixes()[%d] = %q, want %q", i, s, tt.want[i])
				}
			}
		})
	}
}

func TestResolveTopicSuffix_EmptyChannelSuffix(t *testing.T) {
	t.Parallel()

	rules := []TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
	}

	_, err := ResolveTopicSuffix(rules, "", stubMatchWildcard)
	if err == nil {
		t.Fatal("expected error for empty channel suffix, got nil")
	}
	if !errors.Is(err, ErrEmptyChannelSuffix) {
		t.Errorf("expected ErrEmptyChannelSuffix, got: %v", err)
	}
}

func TestResolveTopicSuffix_NilMatchFn(t *testing.T) {
	t.Parallel()

	rules := []TopicRoutingRule{
		{Pattern: "*.trade", TopicSuffix: "trade"},
	}

	_, err := ResolveTopicSuffix(rules, "BTC.trade", nil)
	if err == nil {
		t.Fatal("expected error for nil matchFn, got nil")
	}
	if !errors.Is(err, ErrNilMatchFunc) {
		t.Errorf("expected ErrNilMatchFunc, got: %v", err)
	}
}

func TestResolveTopicSuffix_SingleRule(t *testing.T) {
	t.Parallel()

	rules := []TopicRoutingRule{
		{Pattern: "*", TopicSuffix: "default"},
	}

	suffix, err := ResolveTopicSuffix(rules, "anything.here", stubMatchWildcard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suffix != "default" {
		t.Errorf("got %q, want %q", suffix, "default")
	}
}
