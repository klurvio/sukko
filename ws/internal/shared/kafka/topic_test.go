package kafka

import "testing"

func TestBuildTopicName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		namespace string
		tenantID  string
		category  string
		expected  string
	}{
		// Standard cases
		{"prod", "odin", "trade", "prod.odin.trade"},
		{"dev", "odin", "liquidity", "dev.odin.liquidity"},
		{"stag", "acme", "metadata", "stag.acme.metadata"},
		{"local", "test", "analytics", "local.test.analytics"},

		// All 8 default categories
		{"prod", "odin", "trade", "prod.odin.trade"},
		{"prod", "odin", "liquidity", "prod.odin.liquidity"},
		{"prod", "odin", "metadata", "prod.odin.metadata"},
		{"prod", "odin", "social", "prod.odin.social"},
		{"prod", "odin", "community", "prod.odin.community"},
		{"prod", "odin", "creation", "prod.odin.creation"},
		{"prod", "odin", "analytics", "prod.odin.analytics"},
		{"prod", "odin", "balances", "prod.odin.balances"},

		// Edge cases
		{"", "odin", "trade", ".odin.trade"},         // Empty namespace
		{"prod", "", "trade", "prod..trade"},         // Empty tenant
		{"prod", "odin", "", "prod.odin."},           // Empty category
		{"PROD", "ODIN", "TRADE", "PROD.ODIN.TRADE"}, // Uppercase (preserved)
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			result := BuildTopicName(tt.namespace, tt.tenantID, tt.category)
			if result != tt.expected {
				t.Errorf("BuildTopicName(%q, %q, %q) = %q, want %q",
					tt.namespace, tt.tenantID, tt.category, result, tt.expected)
			}
		})
	}
}

func BenchmarkBuildTopicName(b *testing.B) {
	for b.Loop() {
		_ = BuildTopicName("prod", "odin", "trade")
	}
}
