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
//   - Used for: topic naming only (odin.{namespace}.{base})
//   - Defaults to: normalized ENVIRONMENT value if not set
//   - Example: KAFKA_TOPIC_NAMESPACE=prod means "consume from odin.prod.* topics"
//
// Why separate?
//   - Allows develop environment to consume from prod topics for testing
//   - Keeps logs/metrics accurate (shows "develop" not "prod")
//   - Explicit about intent - clearly shows cross-namespace access
//   - Safer - can't accidentally affect non-topic environment behavior
//
// Valid namespaces: local, dev, staging, prod
//
// =============================================================================

// Topic base names (without environment prefix)
//
// Deprecated: These constants are provided for backward compatibility with legacy
// single-tenant deployments. In multi-tenant deployments, topics are provisioned
// per-tenant via the provisioning service and queried through TenantRegistry.
// New code should not depend on this list - use TenantRegistry to discover topics.
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

// allTopicBases contains all base topic names.
//
// Deprecated: This list is provided for backward compatibility with legacy
// single-tenant deployments. In multi-tenant mode, topics are provisioned
// per-tenant and should be queried from TenantRegistry, not this hardcoded list.
// Each tenant can have a different set of categories based on their subscription.
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
//   - "production", "prod"       -> "prod"
func NormalizeEnv(env string) string {
	env = strings.ToLower(strings.TrimSpace(env))
	switch env {
	case "development", "local", "":
		return "local"
	case "develop", "dev":
		return "dev"
	case "staging", "stage":
		return "staging"
	case "production", "prod":
		return "prod"
	default:
		return env
	}
}

// GetTopic returns the full topic name for an environment
// Example: GetTopic("dev", "trade") -> "odin.dev.trade"
func GetTopic(env, base string) string {
	return fmt.Sprintf("odin.%s.%s", NormalizeEnv(env), base)
}

// AllTopicBases returns all base topic names (without environment prefix).
//
// Deprecated: Use TenantRegistry.GetSharedTenantTopics or GetDedicatedTenants
// to discover provisioned topics in multi-tenant mode. This function is
// provided for backward compatibility with legacy single-tenant deployments.
func AllTopicBases() []string {
	return allTopicBases
}

// AllTopics returns all regular topic names for an environment.
//
// Deprecated: Use TenantRegistry to discover provisioned topics in multi-tenant
// mode. This function assumes single-tenant deployment and is provided for
// backward compatibility only.
func AllTopics(env string) []string {
	topics := make([]string, len(allTopicBases))
	for i, base := range allTopicBases {
		topics[i] = GetTopic(env, base)
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

// TopicToEventType maps a full topic name to its event type category.
// Example: "odin.dev.trade" -> "trade"
func TopicToEventType(topic string) string {
	// Remove "odin." prefix
	topic = strings.TrimPrefix(topic, "odin.")

	// Remove environment prefix (e.g., "dev.", "local.", "staging.", "prod.")
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
