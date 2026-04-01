package publisher

import (
	"strings"
	"testing"
)

func TestTopicResolver_ExactMatch(t *testing.T) {
	t.Parallel()

	r := NewTopicResolver("prod", "acme", []RoutingRule{
		{Pattern: "general.test", TopicSuffix: "general"},
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
		{Pattern: "*.*", TopicSuffix: "default"},
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
		{Pattern: "room.*", TopicSuffix: "rooms"},
		{Pattern: "*.*", TopicSuffix: "default"},
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
		{Pattern: "room.*", TopicSuffix: "rooms"},
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

func TestMatchWildcard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		channel string
		want    bool
	}{
		{"*.*", "general.test", true},
		{"*.*", "a.b.c", true},
		{"room.*", "room.vip", true},
		{"room.*", "general.test", false},
		{"*", "anything", true},
		{"exact", "exact", true},
		{"exact", "other", false},
		{"*.trade", "BTC.trade", true},
		{"*.trade", "crypto.BTC.trade", true},
		{"dm.*", "dm.user123", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.channel, func(t *testing.T) {
			t.Parallel()
			got := matchWildcard(tt.pattern, tt.channel)
			if got != tt.want {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.channel, got, tt.want)
			}
		})
	}
}

func TestParseRoutingRules(t *testing.T) {
	t.Parallel()

	input := `{"rules":[{"pattern":"*.*","topic_suffix":"default"},{"pattern":"room.*","topic_suffix":"rooms"}]}`
	rules, err := ParseRoutingRules([]byte(input))
	if err != nil {
		t.Fatalf("ParseRoutingRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len = %d, want 2", len(rules))
	}
	if rules[0].Pattern != "*.*" || rules[0].TopicSuffix != "default" {
		t.Errorf("rule[0] = %+v", rules[0])
	}
	if rules[1].Pattern != "room.*" || rules[1].TopicSuffix != "rooms" {
		t.Errorf("rule[1] = %+v", rules[1])
	}
}
