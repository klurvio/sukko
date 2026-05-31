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
		{"prod", "sukko", "trade", "prod.sukko.trade"},
		{"dev", "sukko", "liquidity", "dev.sukko.liquidity"},
		{"stag", "acme", "metadata", "stag.acme.metadata"},
		{"local", "test", "analytics", "local.test.analytics"},

		// All 8 default categories
		{"prod", "sukko", "trade", "prod.sukko.trade"},
		{"prod", "sukko", "liquidity", "prod.sukko.liquidity"},
		{"prod", "sukko", "metadata", "prod.sukko.metadata"},
		{"prod", "sukko", "social", "prod.sukko.social"},
		{"prod", "sukko", "community", "prod.sukko.community"},
		{"prod", "sukko", "creation", "prod.sukko.creation"},
		{"prod", "sukko", "analytics", "prod.sukko.analytics"},
		{"prod", "sukko", "balances", "prod.sukko.balances"},

		// Edge cases
		{"", "sukko", "trade", ".sukko.trade"},         // Empty namespace
		{"prod", "", "trade", "prod..trade"},           // Empty tenant
		{"prod", "sukko", "", "prod.sukko."},           // Empty category
		{"PROD", "SUKKO", "TRADE", "PROD.SUKKO.TRADE"}, // Uppercase (preserved)
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

func TestExtractTenantID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		topicName   string
		namespace   string
		wantTenant  string
		wantChannel string
		wantErr     bool
	}{
		// Happy paths
		{"simple", "prod.acme.trade", "prod", "acme", "trade", false},
		{"dev env", "dev.tenant1.liquidity", "dev", "tenant1", "liquidity", false},
		{"stag env", "stag.foo.metadata", "stag", "foo", "metadata", false},
		{"multi-dot channel", "prod.tenant.BTC-USD.spot", "prod", "tenant", "BTC-USD.spot", false},
		{"deeply nested channel", "prod.t.a.b.c.d", "prod", "t", "a.b.c.d", false},

		// Error paths
		{"wrong namespace prefix", "prod.acme.trade", "dev", "", "", true},
		{"no tenant.channel segment", "prod.onlytenant", "prod", "", "", true},
		{"empty tenantID", "prod..channel", "prod", "", "", true},
		{"namespace only", "prod.", "prod", "", "", true},
		{"empty topic", "", "prod", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTenant, gotChannel, err := ExtractTenantID(tt.topicName, tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExtractTenantID(%q, %q) error = %v, wantErr %v", tt.topicName, tt.namespace, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if gotTenant != tt.wantTenant {
				t.Errorf("tenantID = %q, want %q", gotTenant, tt.wantTenant)
			}
			if gotChannel != tt.wantChannel {
				t.Errorf("channel = %q, want %q", gotChannel, tt.wantChannel)
			}
		})
	}
}

func BenchmarkBuildTopicName(b *testing.B) {
	for b.Loop() {
		_ = BuildTopicName("prod", "sukko", "trade")
	}
}
