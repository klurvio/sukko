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

// bundleBasesMap maps bundle types to their base topic lists
var bundleBasesMap = map[BundleType][]string{
	BundleTrading: {
		TopicBaseTrade,
		TopicBaseLiquidity,
		TopicBaseAnalytics,
	},
	BundleFullMarket: {
		TopicBaseTrade,
		TopicBaseLiquidity,
		TopicBaseAnalytics,
		TopicBaseMetadata,
	},
	BundleCommunity: {
		TopicBaseCommunity,
		TopicBaseSocial,
		TopicBaseMetadata,
	},
	BundlePortfolio: {
		TopicBaseTrade,
		TopicBaseAnalytics,
		TopicBaseBalances,
	},
	BundlePriceOnly: {
		TopicBaseTrade,
		TopicBaseAnalytics,
	},
	BundleAll: nil, // Special case: returns all topics
}

// GetBundleBases returns the base topic names for a given bundle type
func GetBundleBases(bundle BundleType) []string {
	if bundle == BundleAll {
		return AllTopicBases()
	}
	bases, ok := bundleBasesMap[bundle]
	if !ok {
		return nil
	}
	// Return a copy to prevent modification
	result := make([]string, len(bases))
	copy(result, bases)
	return result
}

// GetBundleTopics returns the full topic names for a given bundle type and environment
// Returns refined topics for ws-server consumption
func GetBundleTopics(env string, bundle BundleType) []string {
	bases := GetBundleBases(bundle)
	if bases == nil {
		return nil
	}
	topics := make([]string, len(bases))
	for i, base := range bases {
		topics[i] = GetRefinedTopic(env, base)
	}
	return topics
}

// GetBundleRegularTopics returns regular (non-refined) topics for a bundle
// Used by publisher to know which topics to publish to
func GetBundleRegularTopics(env string, bundle BundleType) []string {
	bases := GetBundleBases(bundle)
	if bases == nil {
		return nil
	}
	topics := make([]string, len(bases))
	for i, base := range bases {
		topics[i] = GetTopic(env, base)
	}
	return topics
}

// ValidBundle checks if a bundle type is valid
func ValidBundle(bundle BundleType) bool {
	_, ok := bundleBasesMap[bundle]
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

// GetTopicsForSubscription expands a subscription request into refined topic list
// Supports both bundle types and individual base topic names
func GetTopicsForSubscription(env, subscription string) []string {
	// Check if it's a bundle
	bundle := BundleType(subscription)
	if topics := GetBundleTopics(env, bundle); topics != nil {
		return topics
	}

	// Check if it's a valid base topic name
	for _, base := range AllTopicBases() {
		if base == subscription {
			return []string{GetRefinedTopic(env, base)}
		}
	}

	// Invalid subscription
	return nil
}

// IsValidBaseTopic checks if a base topic name is valid
func IsValidBaseTopic(base string) bool {
	for _, b := range AllTopicBases() {
		if b == base {
			return true
		}
	}
	return false
}
