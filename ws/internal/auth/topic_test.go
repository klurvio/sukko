package auth

import (
	"testing"
)

func TestNewTopicIsolator(t *testing.T) {
	t.Run("with default config", func(t *testing.T) {
		iso := NewTopicIsolator(DefaultTopicIsolationConfig())
		if iso == nil {
			t.Fatal("expected non-nil isolator")
		}
		if iso.config.Environment != "main" {
			t.Errorf("expected environment 'main', got %q", iso.config.Environment)
		}
		if iso.config.Separator != "." {
			t.Errorf("expected separator '.', got %q", iso.config.Separator)
		}
	})

	t.Run("with empty separator defaults to dot", func(t *testing.T) {
		iso := NewTopicIsolator(TopicIsolationConfig{Separator: ""})
		if iso.config.Separator != "." {
			t.Errorf("expected separator '.', got %q", iso.config.Separator)
		}
	})

	t.Run("with empty environment defaults to main", func(t *testing.T) {
		iso := NewTopicIsolator(TopicIsolationConfig{Environment: ""})
		if iso.config.Environment != "main" {
			t.Errorf("expected environment 'main', got %q", iso.config.Environment)
		}
	})
}

func TestTopicIsolator_CheckTopicAccess(t *testing.T) {
	iso := NewTopicIsolator(TopicIsolationConfig{
		Environment:         "main",
		TenantPosition:      1,
		Separator:           ".",
		CrossTenantRoles:    []string{"admin", "system"},
		SharedTopicPatterns: []string{"main.shared.*"},
	})

	tests := []struct {
		name          string
		claims        *Claims
		topic         string
		action        TopicAction
		expectAllowed bool
		expectReason  string
	}{
		{
			name:          "same tenant allowed",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.acme.trade",
			action:        TopicActionConsume,
			expectAllowed: true,
			expectReason:  "tenant match",
		},
		{
			name:          "different tenant denied",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.globex.trade",
			action:        TopicActionConsume,
			expectAllowed: false,
		},
		{
			name:          "nil claims allowed (auth disabled)",
			claims:        nil,
			topic:         "main.acme.trade",
			action:        TopicActionConsume,
			expectAllowed: true,
		},
		{
			name:          "empty tenant allowed (auth disabled)",
			claims:        &Claims{TenantID: ""},
			topic:         "main.acme.trade",
			action:        TopicActionConsume,
			expectAllowed: true,
		},
		{
			name:          "admin role cross-tenant allowed",
			claims:        &Claims{TenantID: "acme", Roles: []string{"admin"}},
			topic:         "main.globex.trade",
			action:        TopicActionConsume,
			expectAllowed: true,
		},
		{
			name:          "system role cross-tenant allowed",
			claims:        &Claims{TenantID: "acme", Roles: []string{"system"}},
			topic:         "main.globex.trade",
			action:        TopicActionPublish,
			expectAllowed: true,
		},
		{
			name:          "shared topic allowed",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.shared.broadcast",
			action:        TopicActionConsume,
			expectAllowed: true,
		},
		{
			name:          "rejects topic without tenant segment",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.trade", // Missing tenant - not in shared patterns
			action:        TopicActionConsume,
			expectAllowed: false,
		},
		{
			name:          "publish same tenant allowed",
			claims:        &Claims{TenantID: "acme"},
			topic:         "main.acme.trade",
			action:        TopicActionPublish,
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := iso.CheckTopicAccess(tt.claims, tt.topic, tt.action)

			if result.Allowed != tt.expectAllowed {
				t.Errorf("CheckTopicAccess() Allowed = %v, want %v (reason: %s)",
					result.Allowed, tt.expectAllowed, result.Reason)
			}

			if tt.expectReason != "" && result.Reason != tt.expectReason {
				t.Errorf("CheckTopicAccess() Reason = %q, want %q",
					result.Reason, tt.expectReason)
			}
		})
	}
}

func TestTopicIsolator_CheckTopicAccess_CrossTenantFlags(t *testing.T) {
	iso := NewTopicIsolator(TopicIsolationConfig{
		Environment:         "main",
		TenantPosition:      1,
		Separator:           ".",
		CrossTenantRoles:    []string{"admin"},
		SharedTopicPatterns: []string{"main.shared.*"},
	})

	t.Run("cross-tenant flag set", func(t *testing.T) {
		claims := &Claims{TenantID: "acme", Roles: []string{"admin"}}
		result := iso.CheckTopicAccess(claims, "main.globex.trade", TopicActionConsume)

		if !result.IsCrossTenant {
			t.Error("expected IsCrossTenant to be true for cross-tenant access")
		}
	})

	t.Run("shared topic flag set", func(t *testing.T) {
		claims := &Claims{TenantID: "acme"}
		result := iso.CheckTopicAccess(claims, "main.shared.broadcast", TopicActionConsume)

		if !result.IsSharedTopic {
			t.Error("expected IsSharedTopic to be true for shared topic")
		}
	})
}

func TestTopicIsolator_ExtractTenantFromTopic(t *testing.T) {
	iso := NewTopicIsolator(DefaultTopicIsolationConfig())

	tests := []struct {
		topic    string
		expected string
	}{
		{"main.acme.trade", "acme"},
		{"main.globex.liquidity", "globex"},
		{"dev.startup.trade", "startup"},
		{"main.trade", "trade"}, // Position 1 exists, returns "trade"
		{"trade", ""},           // Not enough parts (position 1 doesn't exist)
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			result := iso.ExtractTenantFromTopic(tt.topic)
			if result != tt.expected {
				t.Errorf("ExtractTenantFromTopic(%q) = %q, want %q",
					tt.topic, result, tt.expected)
			}
		})
	}
}

func TestTopicIsolator_BuildTopicName(t *testing.T) {
	iso := NewTopicIsolator(TopicIsolationConfig{
		Environment:    "main",
		TenantPosition: 1,
		Separator:      ".",
	})

	tests := []struct {
		tenant   string
		category string
		expected string
	}{
		{"acme", "trade", "main.acme.trade"},
		{"globex", "liquidity", "main.globex.liquidity"},
		{"startup", "balances", "main.startup.balances"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := iso.BuildTopicName(tt.tenant, tt.category)
			if result != tt.expected {
				t.Errorf("BuildTopicName(%q, %q) = %q, want %q",
					tt.tenant, tt.category, result, tt.expected)
			}
		})
	}
}

func TestTopicIsolator_BuildTopicNameWithEnv(t *testing.T) {
	iso := NewTopicIsolator(DefaultTopicIsolationConfig())

	result := iso.BuildTopicNameWithEnv("dev", "acme", "trade")
	expected := "dev.acme.trade"

	if result != expected {
		t.Errorf("BuildTopicNameWithEnv() = %q, want %q", result, expected)
	}
}

func TestTopicIsolator_ParseTopic(t *testing.T) {
	iso := NewTopicIsolator(DefaultTopicIsolationConfig())

	tests := []struct {
		topic     string
		expectEnv string
		expectTnt string
		expectCat string
	}{
		{"main.acme.trade", "main", "acme", "trade"},
		{"dev.globex.liquidity", "dev", "globex", "liquidity"},
		{"main.acme.trade.refined", "main", "acme", "trade.refined"},
		{"main.acme", "main", "acme", ""},
		{"main", "main", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			result := iso.ParseTopic(tt.topic)

			if result.Environment != tt.expectEnv {
				t.Errorf("ParseTopic(%q).Environment = %q, want %q",
					tt.topic, result.Environment, tt.expectEnv)
			}
			if result.Tenant != tt.expectTnt {
				t.Errorf("ParseTopic(%q).Tenant = %q, want %q",
					tt.topic, result.Tenant, tt.expectTnt)
			}
			if result.Category != tt.expectCat {
				t.Errorf("ParseTopic(%q).Category = %q, want %q",
					tt.topic, result.Category, tt.expectCat)
			}
			if result.Original != tt.topic {
				t.Errorf("ParseTopic(%q).Original = %q, want %q",
					tt.topic, result.Original, tt.topic)
			}
		})
	}
}

func TestTopicIsolator_ValidateTopicFormat(t *testing.T) {
	iso := NewTopicIsolator(DefaultTopicIsolationConfig())

	tests := []struct {
		topic     string
		expectErr bool
	}{
		{"main.acme.trade", false},
		{"dev.globex.liquidity", false},
		{"main.acme.trade.refined", false},
		{"main.acme", true},   // Missing category
		{"main", true},        // Missing tenant and category
		{"", true},            // Empty
		{".acme.trade", true}, // Empty environment
		{"main..trade", true}, // Empty tenant
		{"main.acme.", true},  // Empty category
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			err := iso.ValidateTopicFormat(tt.topic)
			hasErr := err != nil

			if hasErr != tt.expectErr {
				t.Errorf("ValidateTopicFormat(%q) error = %v, want error = %v",
					tt.topic, err, tt.expectErr)
			}
		})
	}
}

func TestTopicIsolator_CanPublish_CanConsume(t *testing.T) {
	iso := NewTopicIsolator(DefaultTopicIsolationConfig())

	claims := &Claims{TenantID: "acme"}

	// Same tenant
	if !iso.CanPublish(claims, "main.acme.trade") {
		t.Error("expected CanPublish to return true for same tenant")
	}
	if !iso.CanConsume(claims, "main.acme.trade") {
		t.Error("expected CanConsume to return true for same tenant")
	}

	// Different tenant
	if iso.CanPublish(claims, "main.globex.trade") {
		t.Error("expected CanPublish to return false for different tenant")
	}
	if iso.CanConsume(claims, "main.globex.trade") {
		t.Error("expected CanConsume to return false for different tenant")
	}
}

func TestTopicIsolator_BuildTopicPrefix(t *testing.T) {
	iso := NewTopicIsolator(TopicIsolationConfig{
		Environment: "main",
		Separator:   ".",
	})

	result := iso.BuildTopicPrefix("acme")
	expected := "main.acme."

	if result != expected {
		t.Errorf("BuildTopicPrefix() = %q, want %q", result, expected)
	}
}

func TestTopicIsolator_ListAllowedTopicPatterns(t *testing.T) {
	iso := NewTopicIsolator(TopicIsolationConfig{
		Environment:         "main",
		Separator:           ".",
		SharedTopicPatterns: []string{"main.shared.*"},
	})

	patterns := iso.ListAllowedTopicPatterns("acme")

	if len(patterns) < 1 {
		t.Fatal("expected at least 1 pattern")
	}

	// First pattern should be tenant's topics
	expected := "main\\.acme\\..*"
	if patterns[0] != expected {
		t.Errorf("patterns[0] = %q, want %q", patterns[0], expected)
	}
}

func TestMatchTopicPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		topic    string
		expected bool
	}{
		// Exact match
		{"main.acme.trade", "main.acme.trade", true},
		{"main.acme.trade", "main.acme.liquidity", false},

		// Wildcard in tenant position
		{"main.*.trade", "main.acme.trade", true},
		{"main.*.trade", "main.globex.trade", true},
		{"main.*.trade", "main.acme.liquidity", false},

		// Wildcard in category position
		{"main.shared.*", "main.shared.broadcast", true},
		{"main.shared.*", "main.shared.alerts", true},
		{"main.shared.*", "main.acme.trade", false},

		// Multiple wildcards
		{"*.*.*", "main.acme.trade", true},
		{"*.*.*", "dev.globex.liquidity", true},

		// Length mismatch
		{"main.*.trade", "main.acme", false},
		{"main.*", "main.acme.trade", false},
	}

	for _, tt := range tests {
		name := tt.pattern + "_vs_" + tt.topic
		t.Run(name, func(t *testing.T) {
			result := matchTopicPattern(tt.pattern, tt.topic, ".")
			if result != tt.expected {
				t.Errorf("matchTopicPattern(%q, %q) = %v, want %v",
					tt.pattern, tt.topic, result, tt.expected)
			}
		})
	}
}

func TestDefaultTopicIsolationConfig(t *testing.T) {
	config := DefaultTopicIsolationConfig()

	if config.Environment != "main" {
		t.Errorf("expected environment 'main', got %q", config.Environment)
	}
	if config.TenantPosition != 1 {
		t.Errorf("expected tenant position 1, got %d", config.TenantPosition)
	}
	if config.Separator != "." {
		t.Errorf("expected separator '.', got %q", config.Separator)
	}
	if len(config.CrossTenantRoles) != 2 {
		t.Errorf("expected 2 cross-tenant roles, got %d", len(config.CrossTenantRoles))
	}
}

func TestTopicIsolator_FailSecure(t *testing.T) {
	iso := NewTopicIsolator(TopicIsolationConfig{
		Environment:         "main",
		TenantPosition:      1,
		Separator:           ".",
		SharedTopicPatterns: []string{"main.shared.*"},
	})

	claims := &Claims{TenantID: "acme"}

	t.Run("topic with matching tenant allowed", func(t *testing.T) {
		result := iso.CheckTopicAccess(claims, "main.acme.trade", TopicActionConsume)
		if !result.Allowed {
			t.Errorf("expected topic with matching tenant to be allowed, got: %s", result.Reason)
		}
	})

	t.Run("topic without tenant segment denied", func(t *testing.T) {
		// Single part topic has no tenant - must be rejected (fail-secure)
		result := iso.CheckTopicAccess(claims, "broadcast", TopicActionConsume)
		if result.Allowed {
			t.Error("expected topic without tenant segment to be denied (fail-secure)")
		}
	})

	t.Run("shared topic allowed", func(t *testing.T) {
		// Explicitly configured shared topic
		result := iso.CheckTopicAccess(claims, "main.shared.broadcast", TopicActionConsume)
		if !result.Allowed {
			t.Errorf("expected shared topic to be allowed, got: %s", result.Reason)
		}
	})

	t.Run("topic with different tenant denied", func(t *testing.T) {
		result := iso.CheckTopicAccess(claims, "main.globex.trade", TopicActionConsume)
		if result.Allowed {
			t.Error("expected topic with different tenant to be denied")
		}
	})
}
