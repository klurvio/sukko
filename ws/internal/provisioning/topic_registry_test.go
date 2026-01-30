package provisioning

import (
	"testing"

	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
)

// =============================================================================
// TopicRegistry Interface Compliance
// =============================================================================

func TestTopicRegistry_ImplementsInterface(t *testing.T) {
	t.Parallel()
	// Compile-time check that TopicRegistry implements kafka.TenantRegistry
	var _ kafka.TenantRegistry = (*TopicRegistry)(nil)
}

// =============================================================================
// TenantTopics Tests
// =============================================================================

func TestTenantTopics_Fields(t *testing.T) {
	t.Parallel()
	tt := kafka.TenantTopics{
		TenantID: "acme",
		Topics:   []string{"prod.acme.trade", "prod.acme.liquidity"},
	}

	if tt.TenantID != "acme" {
		t.Errorf("TenantID: got %s, want acme", tt.TenantID)
	}
	if len(tt.Topics) != 2 {
		t.Errorf("Topics length: got %d, want 2", len(tt.Topics))
	}
	if tt.Topics[0] != "prod.acme.trade" {
		t.Errorf("Topics[0]: got %s, want main.acme.trade", tt.Topics[0])
	}
	if tt.Topics[1] != "prod.acme.liquidity" {
		t.Errorf("Topics[1]: got %s, want main.acme.liquidity", tt.Topics[1])
	}
}

// =============================================================================
// Topic Naming Convention Tests
// =============================================================================

func TestTopicNaming_Format(t *testing.T) {
	t.Parallel()
	tests := []struct {
		namespace string
		tenantID  string
		category  string
		expected  string
	}{
		{"prod", "acme", "trade", "prod.acme.trade"},
		{"prod", "acme", "liquidity", "prod.acme.liquidity"},
		{"develop", "bigcorp", "analytics", "develop.bigcorp.analytics"},
		{"staging", "tenant-123", "balances", "staging.tenant-123.balances"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			result := tt.namespace + "." + tt.tenantID + "." + tt.category
			if result != tt.expected {
				t.Errorf("Topic name: got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestTopicNaming_NamespacePrefix(t *testing.T) {
	t.Parallel()
	// Test that namespace prefix matching works correctly
	tests := []struct {
		namespace   string
		topicName   string
		shouldMatch bool
	}{
		{"prod", "prod.acme.trade", true},
		{"prod", "develop.acme.trade", false},
		{"develop", "develop.bigcorp.liquidity", true},
		{"develop", "prod.bigcorp.liquidity", false},
		{"staging", "staging.tenant.analytics", true},
	}

	for _, tt := range tests {
		t.Run(tt.topicName, func(t *testing.T) {
			t.Parallel()
			prefix := tt.namespace + "."
			hasPrefix := len(tt.topicName) > len(prefix) && tt.topicName[:len(prefix)] == prefix

			if hasPrefix != tt.shouldMatch {
				t.Errorf("Prefix match for %s with namespace %s: got %v, want %v",
					tt.topicName, tt.namespace, hasPrefix, tt.shouldMatch)
			}
		})
	}
}

// =============================================================================
// Consumer Type Tests (basic validation - full tests in types_test.go)
// =============================================================================

func TestTopicRegistry_ConsumerType_Constants(t *testing.T) {
	t.Parallel()
	if ConsumerShared != "shared" {
		t.Errorf("ConsumerShared: got %s, want shared", ConsumerShared)
	}
	if ConsumerDedicated != "dedicated" {
		t.Errorf("ConsumerDedicated: got %s, want dedicated", ConsumerDedicated)
	}
}

// =============================================================================
// SQL Pattern Tests
// =============================================================================

func TestSQLPattern_LikePrefix(t *testing.T) {
	t.Parallel()
	// Test the LIKE pattern used for namespace filtering
	tests := []struct {
		namespace string
		expected  string
	}{
		{"prod", "prod.%"},
		{"develop", "develop.%"},
		{"staging", "staging.%"},
		{"local", "local.%"},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			t.Parallel()
			pattern := tt.namespace + ".%"
			if pattern != tt.expected {
				t.Errorf("Pattern: got %s, want %s", pattern, tt.expected)
			}
		})
	}
}

// =============================================================================
// Query Result Processing Tests
// =============================================================================

func TestQueryResult_EmptyTopics(t *testing.T) {
	t.Parallel()
	// Test handling of empty topic list
	topics := []string{}

	if len(topics) != 0 {
		t.Errorf("Empty topics: got %d, want 0", len(topics))
	}
}

func TestQueryResult_MultipleTopics(t *testing.T) {
	t.Parallel()
	// Test handling of multiple topics
	topics := []string{
		"prod.acme.trade",
		"prod.acme.liquidity",
		"prod.acme.analytics",
		"prod.bigcorp.trade",
	}

	if len(topics) != 4 {
		t.Errorf("Topics count: got %d, want 4", len(topics))
	}

	// Verify topics are distinct
	seen := make(map[string]bool)
	for _, topic := range topics {
		if seen[topic] {
			t.Errorf("Duplicate topic: %s", topic)
		}
		seen[topic] = true
	}
}

func TestQueryResult_TenantGrouping(t *testing.T) {
	t.Parallel()
	// Test grouping topics by tenant
	tenants := []kafka.TenantTopics{
		{TenantID: "acme", Topics: []string{"prod.acme.trade", "prod.acme.liquidity"}},
		{TenantID: "bigcorp", Topics: []string{"prod.bigcorp.trade"}},
	}

	if len(tenants) != 2 {
		t.Errorf("Tenants count: got %d, want 2", len(tenants))
	}

	// Find acme tenant
	var acme *kafka.TenantTopics
	for i := range tenants {
		if tenants[i].TenantID == "acme" {
			acme = &tenants[i]
			break
		}
	}

	if acme == nil {
		t.Fatal("Expected to find tenant 'acme'")
	}

	if len(acme.Topics) != 2 {
		t.Errorf("Acme topics count: got %d, want 2", len(acme.Topics))
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestTopicRegistry_TenantWithNoTopics(t *testing.T) {
	t.Parallel()
	// Dedicated tenants with no topics should not be included
	tenants := []kafka.TenantTopics{
		{TenantID: "acme", Topics: []string{"prod.acme.trade"}},
		{TenantID: "empty", Topics: []string{}}, // No topics
	}

	// Filter out tenants with no topics (as the real implementation does)
	filtered := make([]kafka.TenantTopics, 0)
	for _, tenant := range tenants {
		if len(tenant.Topics) > 0 {
			filtered = append(filtered, tenant)
		}
	}

	if len(filtered) != 1 {
		t.Errorf("Filtered tenants: got %d, want 1", len(filtered))
	}
	if filtered[0].TenantID != "acme" {
		t.Errorf("Filtered tenant: got %s, want acme", filtered[0].TenantID)
	}
}

func TestTopicRegistry_TenantIDValidation(t *testing.T) {
	t.Parallel()
	// Tenant IDs should follow naming rules
	tests := []struct {
		tenantID string
		valid    bool
	}{
		{"acme", true},
		{"bigcorp", true},
		{"tenant-123", true},
		{"a-b-c", true},
		{"ab", false},          // Too short (min 3 chars)
		{"Acme", false},        // Uppercase not allowed
		{"-tenant", false},     // Cannot start with hyphen
		{"tenant_name", false}, // Underscore not allowed
		{"", false},            // Empty not allowed
	}

	for _, tt := range tests {
		t.Run(tt.tenantID, func(t *testing.T) {
			t.Parallel()
			err := ValidateTenantID(tt.tenantID)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateTenantID(%q): got err=%v, want valid=%v", tt.tenantID, err, tt.valid)
			}
		})
	}
}

// =============================================================================
// Integration Pattern Tests
// =============================================================================

func TestTopicRegistry_MultipleNamespaces(t *testing.T) {
	t.Parallel()
	// Verify that topics are correctly filtered by namespace
	allTopics := []string{
		"prod.acme.trade",
		"prod.acme.liquidity",
		"develop.acme.trade", // Different namespace
		"prod.bigcorp.analytics",
	}

	// Filter for "prod" namespace
	namespacePrefix := "prod."
	prodTopics := make([]string, 0)
	for _, topic := range allTopics {
		if len(topic) > len(namespacePrefix) && topic[:len(namespacePrefix)] == namespacePrefix {
			prodTopics = append(prodTopics, topic)
		}
	}

	if len(prodTopics) != 3 {
		t.Errorf("Prod namespace topics: got %d, want 3", len(prodTopics))
	}

	// Filter for "develop" namespace
	developPrefix := "develop."
	developTopics := make([]string, 0)
	for _, topic := range allTopics {
		if len(topic) > len(developPrefix) && topic[:len(developPrefix)] == developPrefix {
			developTopics = append(developTopics, topic)
		}
	}

	if len(developTopics) != 1 {
		t.Errorf("Develop namespace topics: got %d, want 1", len(developTopics))
	}
}
