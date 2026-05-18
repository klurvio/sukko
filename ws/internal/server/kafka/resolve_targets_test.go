package kafka

import (
	"testing"

	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/types"
)

// =============================================================================
// resolveTargets tests
// =============================================================================

// stubRulesProvider implements RoutingSnapshotProvider for tests.
type stubRulesProvider struct {
	snap provapi.TenantRoutingSnapshot
	ok   bool
}

func (s *stubRulesProvider) GetRoutingSnapshot(_ string) (provapi.TenantRoutingSnapshot, bool) {
	return s.snap, s.ok
}

func TestResolveTargets_ProEdition_MatchingRule(t *testing.T) {
	t.Parallel()

	p := &Producer{
		topicNamespace: "prod",
		rulesProvider: &stubRulesProvider{
			ok: true,
			snap: provapi.TenantRoutingSnapshot{
				Edition: license.Pro,
				Rules: []types.RoutingRule{
					{Pattern: "acme.*.trade", Topics: []string{"trades"}, Priority: 1},
				},
			},
		},
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.trade", "acme")
	if dlqReason != "" {
		t.Errorf("dlqReason = %q, want empty (rule should match)", dlqReason)
	}
	if len(topics) != 1 || topics[0] != "prod.acme.trades" {
		t.Errorf("topics = %v, want [prod.acme.trades]", topics)
	}
}

func TestResolveTargets_ProEdition_FanoutMultipleTopics(t *testing.T) {
	t.Parallel()

	p := &Producer{
		topicNamespace: "prod",
		rulesProvider: &stubRulesProvider{
			ok: true,
			snap: provapi.TenantRoutingSnapshot{
				Edition: license.Pro,
				Rules: []types.RoutingRule{
					{Pattern: "acme.**.quotes", Topics: []string{"quotes", "all-data"}, Priority: 1},
				},
			},
		},
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.quotes", "acme")
	if dlqReason != "" {
		t.Errorf("dlqReason = %q, want empty", dlqReason)
	}
	if len(topics) != 2 {
		t.Fatalf("topics len = %d, want 2; got %v", len(topics), topics)
	}
	if topics[0] != "prod.acme.quotes" {
		t.Errorf("topics[0] = %q, want %q", topics[0], "prod.acme.quotes")
	}
	if topics[1] != "prod.acme.all-data" {
		t.Errorf("topics[1] = %q, want %q", topics[1], "prod.acme.all-data")
	}
}

func TestResolveTargets_ProEdition_NoMatch_ReturnsDLQReason(t *testing.T) {
	t.Parallel()

	p := &Producer{
		topicNamespace: "prod",
		rulesProvider: &stubRulesProvider{
			ok: true,
			snap: provapi.TenantRoutingSnapshot{
				Edition: license.Pro,
				Rules: []types.RoutingRule{
					{Pattern: "acme.*.orders", Topics: []string{"orders"}, Priority: 1},
				},
			},
		},
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.quotes", "acme")
	if topics != nil {
		t.Errorf("topics = %v, want nil on no match", topics)
	}
	if dlqReason != ReasonNoRoutingRuleMatched {
		t.Errorf("dlqReason = %q, want %q", dlqReason, ReasonNoRoutingRuleMatched)
	}
}

func TestResolveTargets_Community_FallsBackToLastSegment(t *testing.T) {
	t.Parallel()

	p := &Producer{
		topicNamespace: "prod",
		rulesProvider:  nil, // no provider = community mode
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.trade", "acme")
	if dlqReason != "" {
		t.Errorf("dlqReason = %q, want empty for community fallback", dlqReason)
	}
	if len(topics) != 1 || topics[0] != "prod.acme.trade" {
		t.Errorf("topics = %v, want [prod.acme.trade]", topics)
	}
}

func TestResolveTargets_CommunityEdition_WithProvider_FallsBack(t *testing.T) {
	t.Parallel()

	// Provider exists but edition is Community — should fall back to last-segment.
	p := &Producer{
		topicNamespace: "prod",
		rulesProvider: &stubRulesProvider{
			ok: true,
			snap: provapi.TenantRoutingSnapshot{
				Edition: license.Community,
				Rules: []types.RoutingRule{
					{Pattern: "acme.*.trade", Topics: []string{"trades"}, Priority: 1},
				},
			},
		},
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.trade", "acme")
	if dlqReason != "" {
		t.Errorf("dlqReason = %q, want empty for community fallback", dlqReason)
	}
	if len(topics) != 1 || topics[0] != "prod.acme.trade" {
		t.Errorf("topics = %v, want [prod.acme.trade] (community last-segment)", topics)
	}
}

func TestResolveTargets_ProviderMiss_FallsBackToCommunity(t *testing.T) {
	t.Parallel()

	// Provider present but returns ok=false (tenant not in cache).
	p := &Producer{
		topicNamespace: "prod",
		rulesProvider:  &stubRulesProvider{ok: false},
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.orders", "acme")
	if dlqReason != "" {
		t.Errorf("dlqReason = %q, want empty for community fallback", dlqReason)
	}
	if len(topics) != 1 || topics[0] != "prod.acme.orders" {
		t.Errorf("topics = %v, want [prod.acme.orders]", topics)
	}
}

func TestResolveTargets_PriorityOrder_FirstRuleWins(t *testing.T) {
	t.Parallel()

	// Both rules match; first in slice wins (caller must sort by priority before storing).
	p := &Producer{
		topicNamespace: "prod",
		rulesProvider: &stubRulesProvider{
			ok: true,
			snap: provapi.TenantRoutingSnapshot{
				Edition: license.Pro,
				Rules: []types.RoutingRule{
					{Pattern: "acme.**.trade", Topics: []string{"all-trades"}, Priority: 1},
					{Pattern: "acme.BTC.trade", Topics: []string{"btc-trades"}, Priority: 2},
				},
			},
		},
	}

	topics, dlqReason := p.resolveTargets("acme.BTC.trade", "acme")
	if dlqReason != "" {
		t.Errorf("dlqReason = %q, want empty", dlqReason)
	}
	if len(topics) != 1 || topics[0] != "prod.acme.all-trades" {
		t.Errorf("topics = %v, want [prod.acme.all-trades] (first rule wins)", topics)
	}
}
