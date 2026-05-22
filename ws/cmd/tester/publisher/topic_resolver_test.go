package publisher

import (
	"strings"
	"testing"
)

func TestTopicResolver_ExactMatch(t *testing.T) {
	t.Parallel()

	r := NewTopicResolver("prod", "acme", []RoutingRule{
		{Pattern: "general.test", Topics: []string{"general"}},
	})

	topic, err := r.Resolve("general.test")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if topic != "prod.acme.general" {
		t.Errorf("topic = %q, want %q", topic, "prod.acme.general")
	}
}

func TestTopicResolver_WildcardMatch(t *testing.T) {
	t.Parallel()

	r := NewTopicResolver("local", "tenant1", []RoutingRule{
		{Pattern: "**", Topics: []string{"default"}},
	})

	topic, err := r.Resolve("general.test")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if topic != "local.tenant1.default" {
		t.Errorf("topic = %q, want %q", topic, "local.tenant1.default")
	}
}

func TestTopicResolver_FirstMatchWins(t *testing.T) {
	t.Parallel()

	r := NewTopicResolver("dev", "t1", []RoutingRule{
		{Pattern: "room.**", Topics: []string{"rooms"}},
		{Pattern: "**", Topics: []string{"default"}},
	})

	// "room.vip" matches first rule
	topic, err := r.Resolve("room.vip")
	if err != nil {
		t.Fatalf("Resolve room.vip: %v", err)
	}
	if topic != "dev.t1.rooms" {
		t.Errorf("topic = %q, want %q", topic, "dev.t1.rooms")
	}

	// "general.test" doesn't match first, matches second
	topic, err = r.Resolve("general.test")
	if err != nil {
		t.Fatalf("Resolve general.test: %v", err)
	}
	if topic != "dev.t1.default" {
		t.Errorf("topic = %q, want %q", topic, "dev.t1.default")
	}
}

func TestTopicResolver_NoMatch(t *testing.T) {
	t.Parallel()

	r := NewTopicResolver("prod", "t1", []RoutingRule{
		{Pattern: "room.**", Topics: []string{"rooms"}},
	})

	_, err := r.Resolve("general.test")
	if err == nil {
		t.Fatal("expected error for no matching rule")
	}
	if !strings.Contains(err.Error(), "no routing rule matches") {
		t.Errorf("error = %q, want 'no routing rule matches'", err.Error())
	}
}

func TestTopicResolver_EmptyRules(t *testing.T) {
	t.Parallel()

	r := NewTopicResolver("prod", "t1", nil)

	_, err := r.Resolve("any.channel")
	if err == nil {
		t.Fatal("expected error for empty rules")
	}
	if !strings.Contains(err.Error(), "no routing rules") {
		t.Errorf("error = %q, want 'no routing rules'", err.Error())
	}
}

func TestParseRoutingRules(t *testing.T) {
	t.Parallel()

	input := `{"items":[{"pattern":"**","topics":["default"],"priority":100},{"pattern":"room.**","topics":["rooms"],"priority":1}],"total":2,"limit":50,"offset":0}`
	rules, err := ParseRoutingRules([]byte(input))
	if err != nil {
		t.Fatalf("ParseRoutingRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len = %d, want 2", len(rules))
	}
	if rules[0].Pattern != "**" || len(rules[0].Topics) != 1 || rules[0].Topics[0] != "default" {
		t.Errorf("rule[0] = %+v", rules[0])
	}
	if rules[1].Pattern != "room.**" || len(rules[1].Topics) != 1 || rules[1].Topics[0] != "rooms" {
		t.Errorf("rule[1] = %+v", rules[1])
	}
}
