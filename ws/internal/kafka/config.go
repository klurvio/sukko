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

const (
	// Trading events
	EventTradeExecuted EventType = "TRADE_EXECUTED"
	EventBuyCompleted  EventType = "BUY_COMPLETED"
	EventSellCompleted EventType = "SELL_COMPLETED"

	// Liquidity events
	EventLiquidityAdded      EventType = "LIQUIDITY_ADDED"
	EventLiquidityRemoved    EventType = "LIQUIDITY_REMOVED"
	EventLiquidityRebalanced EventType = "LIQUIDITY_REBALANCED"

	// Metadata events
	EventMetadataUpdated   EventType = "METADATA_UPDATED"
	EventTokenNameChanged  EventType = "TOKEN_NAME_CHANGED"
	EventTokenFlagsChanged EventType = "TOKEN_FLAGS_CHANGED"

	// Social events
	EventTwitterVerified    EventType = "TWITTER_VERIFIED"
	EventSocialLinksUpdated EventType = "SOCIAL_LINKS_UPDATED"

	// Community events
	EventCommentPosted   EventType = "COMMENT_POSTED"
	EventCommentPinned   EventType = "COMMENT_PINNED"
	EventCommentUpvoted  EventType = "COMMENT_UPVOTED"
	EventFavoriteToggled EventType = "FAVORITE_TOGGLED"

	// Creation events
	EventTokenCreated EventType = "TOKEN_CREATED"
	EventTokenListed  EventType = "TOKEN_LISTED"

	// Analytics events
	EventPriceDeltaUpdated     EventType = "PRICE_DELTA_UPDATED"
	EventHolderCountUpdated    EventType = "HOLDER_COUNT_UPDATED"
	EventAnalyticsRecalculated EventType = "ANALYTICS_RECALCULATED"
	EventTrendingUpdated       EventType = "TRENDING_UPDATED"

	// Balance events
	EventBalanceUpdated    EventType = "BALANCE_UPDATED"
	EventTransferCompleted EventType = "TRANSFER_COMPLETED"
)

// TopicToEventType maps a full topic name to its event type category
// Handles both regular and refined topics
// Example: "odin.dev.trade" -> "trade", "odin.dev.trade.refined" -> "trade"
func TopicToEventType(topic string) string {
	// Remove "odin." prefix and ".refined" suffix
	topic = strings.TrimPrefix(topic, "odin.")
	topic = strings.TrimSuffix(topic, ".refined")

	// Remove environment prefix (e.g., "dev.", "local.", "staging.", "main.")
	parts := strings.SplitN(topic, ".", 2)
	if len(parts) == 2 {
		return parts[1] // Return the base topic name
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
