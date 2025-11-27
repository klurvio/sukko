package kafka

import (
	"testing"
)

// =============================================================================
// BundleType Constants Tests
// =============================================================================

func TestBundleTypeConstants(t *testing.T) {
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
			if string(tt.bundle) != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.bundle, tt.expected)
			}
		})
	}
}

// =============================================================================
// GetBundleTopics Tests
// =============================================================================

func TestGetBundleTopics_Trading(t *testing.T) {
	topics := GetBundleTopics(BundleTrading)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleTrading) returned nil")
	}

	expected := []string{TopicTrades, TopicLiquidity, TopicAnalytics}
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
	topics := GetBundleTopics(BundleFullMarket)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleFullMarket) returned nil")
	}

	expected := []string{TopicTrades, TopicLiquidity, TopicAnalytics, TopicMetadata}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundleFullMarket) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_Community(t *testing.T) {
	topics := GetBundleTopics(BundleCommunity)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleCommunity) returned nil")
	}

	expected := []string{TopicCommunity, TopicSocial, TopicMetadata}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundleCommunity) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_Portfolio(t *testing.T) {
	topics := GetBundleTopics(BundlePortfolio)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundlePortfolio) returned nil")
	}

	expected := []string{TopicTrades, TopicAnalytics, TopicBalances}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundlePortfolio) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_PriceOnly(t *testing.T) {
	topics := GetBundleTopics(BundlePriceOnly)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundlePriceOnly) returned nil")
	}

	expected := []string{TopicTrades, TopicAnalytics}
	if len(topics) != len(expected) {
		t.Errorf("GetBundleTopics(BundlePriceOnly) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetBundleTopics_All(t *testing.T) {
	topics := GetBundleTopics(BundleAll)

	if topics == nil {
		t.Fatal("GetBundleTopics(BundleAll) returned nil")
	}

	// BundleAll should contain all topics
	allTopics := AllTopics()
	if len(topics) != len(allTopics) {
		t.Errorf("GetBundleTopics(BundleAll) returned %d topics, want %d", len(topics), len(allTopics))
	}
}

func TestGetBundleTopics_Invalid(t *testing.T) {
	topics := GetBundleTopics(BundleType("INVALID"))

	if topics != nil {
		t.Errorf("GetBundleTopics(INVALID) should return nil, got %v", topics)
	}
}

func TestGetBundleTopics_ReturnsCopy(t *testing.T) {
	topics1 := GetBundleTopics(BundleTrading)
	topics2 := GetBundleTopics(BundleTrading)

	if len(topics1) == 0 {
		t.Fatal("BundleTrading should have topics; got empty slice")
	}

	// Modify first slice
	topics1[0] = "modified"

	// Second slice should be unaffected
	if topics2[0] == "modified" {
		t.Error("GetBundleTopics should return a copy")
	}
}

// =============================================================================
// ValidBundle Tests
// =============================================================================

func TestValidBundle_ValidBundles(t *testing.T) {
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
			if !ValidBundle(bundle) {
				t.Errorf("ValidBundle(%s) = false, want true", bundle)
			}
		})
	}
}

func TestValidBundle_InvalidBundles(t *testing.T) {
	invalidBundles := []BundleType{
		BundleType("INVALID"),
		BundleType(""),
		BundleType("trading"), // lowercase
		BundleType("ALL "),    // with space
	}

	for _, bundle := range invalidBundles {
		t.Run(string(bundle), func(t *testing.T) {
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
	bundles := AllBundles()

	if len(bundles) != 6 {
		t.Errorf("AllBundles() returned %d bundles, want 6", len(bundles))
	}
}

func TestAllBundles_ContainsExpectedBundles(t *testing.T) {
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
	topics := GetTopicsForSubscription("TRADING")

	if topics == nil {
		t.Fatal("GetTopicsForSubscription(TRADING) returned nil")
	}

	// Should return same as GetBundleTopics(BundleTrading)
	expected := GetBundleTopics(BundleTrading)
	if len(topics) != len(expected) {
		t.Errorf("GetTopicsForSubscription(TRADING) returned %d topics, want %d", len(topics), len(expected))
	}
}

func TestGetTopicsForSubscription_Topic(t *testing.T) {
	topics := GetTopicsForSubscription(TopicTrades)

	if topics == nil {
		t.Fatal("GetTopicsForSubscription(odin.trades) returned nil")
	}

	if len(topics) != 1 {
		t.Errorf("GetTopicsForSubscription(odin.trades) returned %d topics, want 1", len(topics))
	}

	if topics[0] != TopicTrades {
		t.Errorf("GetTopicsForSubscription(odin.trades)[0] = %s, want %s", topics[0], TopicTrades)
	}
}

func TestGetTopicsForSubscription_AllTopics(t *testing.T) {
	for _, topic := range AllTopics() {
		t.Run(topic, func(t *testing.T) {
			topics := GetTopicsForSubscription(topic)

			if topics == nil {
				t.Fatalf("GetTopicsForSubscription(%s) returned nil", topic)
			}
			if len(topics) != 1 {
				t.Errorf("GetTopicsForSubscription(%s) returned %d topics, want 1", topic, len(topics))
			}
			if topics[0] != topic {
				t.Errorf("GetTopicsForSubscription(%s)[0] = %s, want %s", topic, topics[0], topic)
			}
		})
	}
}

func TestGetTopicsForSubscription_Invalid(t *testing.T) {
	topics := GetTopicsForSubscription("invalid.subscription")

	if topics != nil {
		t.Errorf("GetTopicsForSubscription(invalid) should return nil, got %v", topics)
	}
}

func TestGetTopicsForSubscription_Empty(t *testing.T) {
	topics := GetTopicsForSubscription("")

	if topics != nil {
		t.Errorf("GetTopicsForSubscription('') should return nil, got %v", topics)
	}
}

func TestGetTopicsForSubscription_AllBundles(t *testing.T) {
	for _, bundle := range AllBundles() {
		t.Run(string(bundle), func(t *testing.T) {
			topics := GetTopicsForSubscription(string(bundle))

			if topics == nil {
				t.Fatalf("GetTopicsForSubscription(%s) returned nil", bundle)
			}

			expected := GetBundleTopics(bundle)
			if len(topics) != len(expected) {
				t.Errorf("GetTopicsForSubscription(%s) returned %d topics, want %d", bundle, len(topics), len(expected))
			}
		})
	}
}
