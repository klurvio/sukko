package kafka

import (
	"testing"
)

// Test environment for consistent topic naming
const testEnv = "test"

// =============================================================================
// BundleType Constants Tests
// =============================================================================

func TestBundleTypeConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		bundle   BundleType
		expected string
	}{
		{"BundleTrading", BundleTrading, "TRADING"},
		{"BundleFullMarket", BundleFullMarket, "FULL_MARKET"},
		{"BundleCommunity", BundleCommunity, "COMMUNITY"},
		{"BundlePortfolio", BundlePortfolio, "PORTFOLIO"},
		{"BundlePriceOnly", BundlePriceOnly, "PRICE_ONLY"},
		{"BundleAll", BundleAll, "ALL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.bundle) != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.bundle, tt.expected)
			}
		})
	}
}

// =============================================================================
// GetBundleBases Tests
// =============================================================================

func TestGetBundleBases_Trading(t *testing.T) {
	t.Parallel()
	bases := GetBundleBases(BundleTrading)

	if bases == nil {
		t.Fatal("GetBundleBases(BundleTrading) returned nil")
	}

	expected := []string{TopicBaseTrade, TopicBaseLiquidity, TopicBaseAnalytics}
	if len(bases) != len(expected) {
		t.Errorf("GetBundleBases(BundleTrading) returned %d bases, want %d", len(bases), len(expected))
	}

	baseSet := make(map[string]bool)
	for _, base := range bases {
		baseSet[base] = true
	}

	for _, exp := range expected {
		if !baseSet[exp] {
			t.Errorf("BundleTrading missing base %s", exp)
		}
	}
}

func TestGetBundleBases_All(t *testing.T) {
	t.Parallel()
	bases := GetBundleBases(BundleAll)

	if bases == nil {
		t.Fatal("GetBundleBases(BundleAll) returned nil")
	}

	// BundleAll should contain all base topics
	allBases := AllTopicBases()
	if len(bases) != len(allBases) {
		t.Errorf("GetBundleBases(BundleAll) returned %d bases, want %d", len(bases), len(allBases))
	}
}

func TestGetBundleBases_Invalid(t *testing.T) {
	t.Parallel()
	bases := GetBundleBases(BundleType("INVALID"))

	if bases != nil {
		t.Errorf("GetBundleBases(INVALID) should return nil, got %v", bases)
	}
}

func TestGetBundleBases_ReturnsCopy(t *testing.T) {
	t.Parallel()
	bases1 := GetBundleBases(BundleTrading)
	bases2 := GetBundleBases(BundleTrading)

	if len(bases1) == 0 {
		t.Fatal("BundleTrading should have bases; got empty slice")
	}

	// Modify first slice
	bases1[0] = "modified"

	// Second slice should be unaffected
	if bases2[0] == "modified" {
		t.Error("GetBundleBases should return a copy")
	}
}

// =============================================================================
// GetBundleTopics Tests
// =============================================================================

func TestGetBundleTopics_Trading(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundleTrading)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleTrading) returned nil")
	}

	expected := []string{
		GetRefinedTopic(testEnv, TopicBaseTrade),
		GetRefinedTopic(testEnv, TopicBaseLiquidity),
		GetRefinedTopic(testEnv, TopicBaseAnalytics),
	}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundleTrading) returned %d topics, want %d", len(topics), len(expected))
	}

	topicSet := make(map[string]bool)
	for _, topic := range topics {
		topicSet[topic] = true
	}

	for _, exp := range expected {
		if !topicSet[exp] {
			t.Errorf("BundleTrading missing topic %s", exp)
		}
	}
}

func TestGetBundleTopics_FullMarket(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundleFullMarket)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleFullMarket) returned nil")
	}

	expected := []string{
		GetRefinedTopic(testEnv, TopicBaseTrade),
		GetRefinedTopic(testEnv, TopicBaseLiquidity),
		GetRefinedTopic(testEnv, TopicBaseAnalytics),
		GetRefinedTopic(testEnv, TopicBaseMetadata),
	}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundleFullMarket) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_Community(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundleCommunity)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleCommunity) returned nil")
	}

	expected := []string{
		GetRefinedTopic(testEnv, TopicBaseCommunity),
		GetRefinedTopic(testEnv, TopicBaseSocial),
		GetRefinedTopic(testEnv, TopicBaseMetadata),
	}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundleCommunity) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_Portfolio(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundlePortfolio)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundlePortfolio) returned nil")
	}

	expected := []string{
		GetRefinedTopic(testEnv, TopicBaseTrade),
		GetRefinedTopic(testEnv, TopicBaseAnalytics),
		GetRefinedTopic(testEnv, TopicBaseBalances),
	}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundlePortfolio) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_PriceOnly(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundlePriceOnly)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundlePriceOnly) returned nil")
	}

	expected := []string{
		GetRefinedTopic(testEnv, TopicBaseTrade),
		GetRefinedTopic(testEnv, TopicBaseAnalytics),
	}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundlePriceOnly) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_All(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundleAll)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleAll) returned nil")
	}

	// BundleAll should contain all refined topics
	allTopics := AllRefinedTopics(testEnv)
	if len(topics) != len(allTopics) {
		t.Errorf("GetBundleTopics(BundleAll) returned %d topics, want %d", len(topics), len(allTopics))
	}
}

func TestGetBundleTopics_Invalid(t *testing.T) {
	t.Parallel()
	topics := GetBundleTopics(testEnv, BundleType("INVALID"))

	if topics != nil {
		t.Errorf("GetBundleTopics(INVALID) should return nil, got %v", topics)
	}
}

func TestGetBundleTopics_DifferentEnvironments(t *testing.T) {
	t.Parallel()
	envs := []string{"local", "dev", "staging", "prod"}

	for _, env := range envs {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			topics := GetBundleTopics(env, BundleTrading)
			if topics == nil {
				t.Fatalf("GetBundleTopics(%s, BundleTrading) returned nil", env)
			}

			// Verify topics have correct environment prefix
			expectedPrefix := "odin." + NormalizeEnv(env) + "."
			for _, topic := range topics {
				if !hasPrefix(topic, expectedPrefix) {
					t.Errorf("Topic %s does not have expected prefix %s", topic, expectedPrefix)
				}
			}
		})
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// =============================================================================
// ValidBundle Tests
// =============================================================================

func TestValidBundle_ValidBundles(t *testing.T) {
	t.Parallel()
	validBundles := []BundleType{
		BundleTrading,
		BundleFullMarket,
		BundleCommunity,
		BundlePortfolio,
		BundlePriceOnly,
		BundleAll,
	}

	for _, bundle := range validBundles {
		t.Run(string(bundle), func(t *testing.T) {
			t.Parallel()
			if !ValidBundle(bundle) {
				t.Errorf("ValidBundle(%s) = false, want true", bundle)
			}
		})
	}
}

func TestValidBundle_InvalidBundles(t *testing.T) {
	t.Parallel()
	invalidBundles := []BundleType{
		BundleType("INVALID"),
		BundleType(""),
		BundleType("trading"), // lowercase
		BundleType("ALL "),    // with space
	}

	for _, bundle := range invalidBundles {
		t.Run(string(bundle), func(t *testing.T) {
			t.Parallel()
			if ValidBundle(bundle) {
				t.Errorf("ValidBundle(%s) = true, want false", bundle)
			}
		})
	}
}

// =============================================================================
// AllBundles Tests
// =============================================================================

func TestAllBundles_ReturnsAllBundles(t *testing.T) {
	t.Parallel()
	bundles := AllBundles()

	if len(bundles) != 6 {
		t.Errorf("AllBundles() returned %d bundles, want 6", len(bundles))
	}
}

func TestAllBundles_ContainsExpectedBundles(t *testing.T) {
	t.Parallel()
	bundles := AllBundles()
	bundleSet := make(map[BundleType]bool)
	for _, bundle := range bundles {
		bundleSet[bundle] = true
	}

	expectedBundles := []BundleType{
		BundleTrading,
		BundleFullMarket,
		BundleCommunity,
		BundlePortfolio,
		BundlePriceOnly,
		BundleAll,
	}

	for _, expected := range expectedBundles {
		if !bundleSet[expected] {
			t.Errorf("AllBundles() missing %s", expected)
		}
	}
}

// =============================================================================
// GetTopicsForSubscription Tests
// =============================================================================

func TestGetTopicsForSubscription_Bundle(t *testing.T) {
	t.Parallel()
	topics := GetTopicsForSubscription(testEnv, "TRADING")

	if topics == nil {
		t.Fatal("GetTopicsForSubscription(TRADING) returned nil")
	}

	// Should return same as GetBundleTopics(testEnv, BundleTrading)
	expected := GetBundleTopics(testEnv, BundleTrading)
	if len(topics) != len(expected) {
		t.Errorf("GetTopicsForSubscription(TRADING) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetTopicsForSubscription_BaseTopic(t *testing.T) {
	t.Parallel()
	topics := GetTopicsForSubscription(testEnv, TopicBaseTrade)

	if topics == nil {
		t.Fatal("GetTopicsForSubscription(trade) returned nil")
	}

	if len(topics) != 1 {
		t.Errorf("GetTopicsForSubscription(trade) returned %d topics, want 1", len(topics))
	}

	expected := GetRefinedTopic(testEnv, TopicBaseTrade)
	if topics[0] != expected {
		t.Errorf("GetTopicsForSubscription(trade)[0] = %s, want %s", topics[0], expected)
	}
}

func TestGetTopicsForSubscription_AllBaseTopics(t *testing.T) {
	t.Parallel()
	for _, base := range AllTopicBases() {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			topics := GetTopicsForSubscription(testEnv, base)

			if topics == nil {
				t.Fatalf("GetTopicsForSubscription(%s) returned nil", base)
			}
			if len(topics) != 1 {
				t.Errorf("GetTopicsForSubscription(%s) returned %d topics, want 1", base, len(topics))
			}
			expected := GetRefinedTopic(testEnv, base)
			if topics[0] != expected {
				t.Errorf("GetTopicsForSubscription(%s)[0] = %s, want %s", base, topics[0], expected)
			}
		})
	}
}

func TestGetTopicsForSubscription_Invalid(t *testing.T) {
	t.Parallel()
	topics := GetTopicsForSubscription(testEnv, "invalid.subscription")

	if topics != nil {
		t.Errorf("GetTopicsForSubscription(invalid) should return nil, got %v", topics)
	}
}

func TestGetTopicsForSubscription_Empty(t *testing.T) {
	t.Parallel()
	topics := GetTopicsForSubscription(testEnv, "")

	if topics != nil {
		t.Errorf("GetTopicsForSubscription('') should return nil, got %v", topics)
	}
}

func TestGetTopicsForSubscription_AllBundles(t *testing.T) {
	t.Parallel()
	for _, bundle := range AllBundles() {
		t.Run(string(bundle), func(t *testing.T) {
			t.Parallel()
			topics := GetTopicsForSubscription(testEnv, string(bundle))

			if topics == nil {
				t.Fatalf("GetTopicsForSubscription(%s) returned nil", bundle)
			}

			expected := GetBundleTopics(testEnv, bundle)
			if len(topics) != len(expected) {
				t.Errorf("GetTopicsForSubscription(%s) returned %d topics, want %d", bundle, len(topics), len(expected))
			}
		})
	}
}

// =============================================================================
// IsValidBaseTopic Tests
// =============================================================================

func TestIsValidBaseTopic_Valid(t *testing.T) {
	t.Parallel()
	validBases := AllTopicBases()
	for _, base := range validBases {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			if !IsValidBaseTopic(base) {
				t.Errorf("IsValidBaseTopic(%s) = false, want true", base)
			}
		})
	}
}

func TestIsValidBaseTopic_Invalid(t *testing.T) {
	t.Parallel()
	invalidBases := []string{"invalid", "", "odin.trades", "odin.dev.trade.refined"}
	for _, base := range invalidBases {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			if IsValidBaseTopic(base) {
				t.Errorf("IsValidBaseTopic(%s) = true, want false", base)
			}
		})
	}
}

// =============================================================================
// GetBundleRegularTopics Tests
// =============================================================================

func TestGetBundleRegularTopics_Trading(t *testing.T) {
	t.Parallel()
	topics := GetBundleRegularTopics(testEnv, BundleTrading)

	if topics == nil {
		t.Fatal("GetBundleRegularTopics(BundleTrading) returned nil")
	}

	expected := []string{
		GetTopic(testEnv, TopicBaseTrade),
		GetTopic(testEnv, TopicBaseLiquidity),
		GetTopic(testEnv, TopicBaseAnalytics),
	}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleRegularTopics(BundleTrading) returned %d topics, want %d", len(topics), len(expected))
	}

	// Verify these are regular topics (not refined)
	for _, topic := range topics {
		if hasSuffix(topic, ".refined") {
			t.Errorf("GetBundleRegularTopics returned refined topic: %s", topic)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
