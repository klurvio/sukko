package kafka

import (
	"fmt"
	"strings"
)

// =============================================================================
// Topic Namespace Configuration
// =============================================================================
//
// ENVIRONMENT vs KAFKA_TOPIC_NAMESPACE:
//
// ENVIRONMENT: Identifies the deployment environment (develop, staging, production).
//   - Used for: logging, metrics labels, feature flags, operational context
//   - Example: ENVIRONMENT=develop means "this is the develop deployment"
//
// KAFKA_TOPIC_NAMESPACE: Identifies which Kafka topic namespace to use.
//   - Used for: topic naming only (odin.{namespace}.{base}.refined)
//   - Defaults to: normalized ENVIRONMENT value if not set
//   - Example: KAFKA_TOPIC_NAMESPACE=main means "consume from odin.main.* topics"
//
// Why separate?
//   - Allows develop environment to consume from main topics for testing
//   - Keeps logs/metrics accurate (shows "develop" not "main")
//   - Explicit about intent - clearly shows cross-namespace access
//   - Safer - can't accidentally affect non-topic environment behavior
//
// Valid namespaces: local, dev, staging, main
//
// =============================================================================

// Topic base names (without environment prefix)
const (
	TopicBaseTrade     = "trade"
	TopicBaseLiquidity = "liquidity"
	TopicBaseMetadata  = "metadata"
	TopicBaseSocial    = "social"
	TopicBaseCommunity = "community"
	TopicBaseCreation  = "creation"
	TopicBaseAnalytics = "analytics"
	TopicBaseBalances  = "balances"
)

// allTopicBases contains all base topic names
var allTopicBases = []string{
	TopicBaseTrade,
	TopicBaseLiquidity,
	TopicBaseMetadata,
	TopicBaseSocial,
	TopicBaseCommunity,
	TopicBaseCreation,
	TopicBaseAnalytics,
	TopicBaseBalances,
}

// NormalizeEnv converts environment names to short form for topic namespace.
// This is used for Kafka topic naming: odin.{namespace}.{base}
//
// Mapping:
//   - "development", "local", "" -> "local"
//   - "develop", "dev"           -> "dev"
//   - "staging", "stage"         -> "staging"
//   - "production", "prod", "main" -> "main"
//
// Note: Production uses "main" namespace (not "prod") for clarity.
func NormalizeEnv(env string) string {
	env = strings.ToLower(strings.TrimSpace(env))
	switch env {
	case "development", "local", "":
		return "local"
	case "develop", "dev":
		return "dev"
	case "staging", "stage":
		return "staging"
	case "production", "prod", "main":
		return "main"
	default:
		return env
	}
}

// GetTopic returns the full topic name for an environment
// Example: GetTopic("dev", "trade") -> "odin.dev.trade"
func GetTopic(env, base string) string {
	return fmt.Sprintf("odin.%s.%s", NormalizeEnv(env), base)
}

// GetRefinedTopic returns the refined topic name for an environment
// Example: GetRefinedTopic("dev", "trade") -> "odin.dev.trade.refined"
func GetRefinedTopic(env, base string) string {
	return fmt.Sprintf("odin.%s.%s.refined", NormalizeEnv(env), base)
}

// AllTopicBases returns all base topic names (without environment prefix)
func AllTopicBases() []string {
	return allTopicBases
}

// AllTopics returns all regular topic names for an environment
func AllTopics(env string) []string {
	topics := make([]string, len(allTopicBases))
	for i, base := range allTopicBases {
		topics[i] = GetTopic(env, base)
	}
	return topics
}

// AllRefinedTopics returns all refined topic names for an environment
func AllRefinedTopics(env string) []string {
	topics := make([]string, len(allTopicBases))
	for i, base := range allTopicBases {
		topics[i] = GetRefinedTopic(env, base)
	}
	return topics
}

// AllTopicsWithRefined returns both regular and refined topics for an environment
func AllTopicsWithRefined(env string) []string {
	topics := make([]string, 0, len(allTopicBases)*2)
	for _, base := range allTopicBases {
		topics = append(topics, GetTopic(env, base))
		topics = append(topics, GetRefinedTopic(env, base))
	}
	return topics
}

// EventType represents different event types
type EventType string

// EventType constants for trading platform events.
const (
	EventTradeExecuted EventType = "TRADE_EXECUTED" // Trade executed event
	EventBuyCompleted  EventType = "BUY_COMPLETED"  // Buy completed event
	EventSellCompleted EventType = "SELL_COMPLETED" // Sell completed event

	EventLiquidityAdded      EventType = "LIQUIDITY_ADDED"      // Liquidity added event
	EventLiquidityRemoved    EventType = "LIQUIDITY_REMOVED"    // Liquidity removed event
	EventLiquidityRebalanced EventType = "LIQUIDITY_REBALANCED" // Liquidity rebalanced event

	EventMetadataUpdated   EventType = "METADATA_UPDATED"    // Token metadata updated event
	EventTokenNameChanged  EventType = "TOKEN_NAME_CHANGED"  // Token name changed event
	EventTokenFlagsChanged EventType = "TOKEN_FLAGS_CHANGED" // Token flags changed event

	EventTwitterVerified    EventType = "TWITTER_VERIFIED"     // Twitter verification event
	EventSocialLinksUpdated EventType = "SOCIAL_LINKS_UPDATED" // Social links updated event

	EventCommentPosted   EventType = "COMMENT_POSTED"   // Comment posted event
	EventCommentPinned   EventType = "COMMENT_PINNED"   // Comment pinned event
	EventCommentUpvoted  EventType = "COMMENT_UPVOTED"  // Comment upvoted event
	EventFavoriteToggled EventType = "FAVORITE_TOGGLED" // Favorite toggled event

	EventTokenCreated EventType = "TOKEN_CREATED" // Token created event
	EventTokenListed  EventType = "TOKEN_LISTED"  // Token listed event

	EventPriceDeltaUpdated     EventType = "PRICE_DELTA_UPDATED"    // Price delta updated event
	EventHolderCountUpdated    EventType = "HOLDER_COUNT_UPDATED"   // Holder count updated event
	EventAnalyticsRecalculated EventType = "ANALYTICS_RECALCULATED" // Analytics recalculated event
	EventTrendingUpdated       EventType = "TRENDING_UPDATED"       // Trending updated event

	EventBalanceUpdated    EventType = "BALANCE_UPDATED"    // Balance updated event
	EventTransferCompleted EventType = "TRANSFER_COMPLETED" // Transfer completed event
)

// TopicToEventType maps a full topic name to its event type category
// Handles both regular and refined topics
// Example: "odin.dev.trade" -> "trade", "odin.dev.trade.refined" -> "trade"
func TopicToEventType(topic string) string {
	// Remove "odin." prefix and ".refined" suffix
	topic = strings.TrimPrefix(topic, "odin.")
	topic = strings.TrimSuffix(topic, ".refined")

	// Remove environment prefix (e.g., "dev.", "local.", "staging.", "main.")
	if _, after, found := strings.Cut(topic, "."); found {
		return after // Return the base topic name
	}

	// Fallback for legacy topic names (e.g., "odin.trades" -> "trades")
	return topic
}

// EventTypeToTopicBase maps event types to their base topic name
func EventTypeToTopicBase(eventType EventType) string {
	switch eventType {
	case EventTradeExecuted, EventBuyCompleted, EventSellCompleted:
		return TopicBaseTrade
	case EventLiquidityAdded, EventLiquidityRemoved, EventLiquidityRebalanced:
		return TopicBaseLiquidity
	case EventMetadataUpdated, EventTokenNameChanged, EventTokenFlagsChanged:
		return TopicBaseMetadata
	case EventTwitterVerified, EventSocialLinksUpdated:
		return TopicBaseSocial
	case EventCommentPosted, EventCommentPinned, EventCommentUpvoted, EventFavoriteToggled:
		return TopicBaseCommunity
	case EventTokenCreated, EventTokenListed:
		return TopicBaseCreation
	case EventPriceDeltaUpdated, EventHolderCountUpdated, EventAnalyticsRecalculated, EventTrendingUpdated:
		return TopicBaseAnalytics
	case EventBalanceUpdated, EventTransferCompleted:
		return TopicBaseBalances
	default:
		return TopicBaseTrade // Default to trade for unknown events
	}
}
