package kafka

// BundleType represents different subscription bundle types
type BundleType string

const (
	BundleTrading    BundleType = "TRADING"
	BundleFullMarket BundleType = "FULL_MARKET"
	BundleCommunity  BundleType = "COMMUNITY"
	BundlePortfolio  BundleType = "PORTFOLIO"
	BundlePriceOnly  BundleType = "PRICE_ONLY"
	BundleAll        BundleType = "ALL"
)

// bundleTopicMap maps bundle types to their topic lists
var bundleTopicMap = map[BundleType][]string{
	BundleTrading: {
		TopicTrades,
		TopicLiquidity,
		TopicAnalytics,
	},
	BundleFullMarket: {
		TopicTrades,
		TopicLiquidity,
		TopicAnalytics,
		TopicMetadata,
	},
	BundleCommunity: {
		TopicCommunity,
		TopicSocial,
		TopicMetadata,
	},
	BundlePortfolio: {
		TopicTrades,
		TopicAnalytics,
		TopicBalances,
	},
	BundlePriceOnly: {
		TopicTrades,
		TopicAnalytics,
	},
	BundleAll: AllTopics(),
}

// GetBundleTopics returns the topics for a given bundle type
func GetBundleTopics(bundle BundleType) []string {
	topics, ok := bundleTopicMap[bundle]
	if !ok {
		return nil
	}
	// Return a copy to prevent modification
	result := make([]string, len(topics))
	copy(result, topics)
	return result
}

// ValidBundle checks if a bundle type is valid
func ValidBundle(bundle BundleType) bool {
	_, ok := bundleTopicMap[bundle]
	return ok
}

// AllBundles returns all available bundle types
func AllBundles() []BundleType {
	return []BundleType{
		BundleTrading,
		BundleFullMarket,
		BundleCommunity,
		BundlePortfolio,
		BundlePriceOnly,
		BundleAll,
	}
}

// GetTopicsForSubscription expands a subscription request into topic list
// Supports both bundle types and individual topic names
func GetTopicsForSubscription(subscription string) []string {
	// Check if it's a bundle
	bundle := BundleType(subscription)
	if topics := GetBundleTopics(bundle); topics != nil {
		return topics
	}

	// Check if it's a valid topic name
	for _, topic := range AllTopics() {
		if topic == subscription {
			return []string{topic}
		}
	}

	// Invalid subscription
	return nil
}
