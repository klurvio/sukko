package kafka

import (
	"strings"
)

// =============================================================================
// Topic Namespace Configuration
// =============================================================================
//
// ENVIRONMENT vs KAFKA_TOPIC_NAMESPACE:
//
// ENVIRONMENT: Identifies the deployment environment (develop, stging, production).
//   - Used for: logging, metrics labels, feature flags, operational context
//   - Example: ENVIRONMENT=develop means "this is the develop deployment"
//
// KAFKA_TOPIC_NAMESPACE: Identifies which Kafka topic namespace to use.
//   - Used for: topic naming only ({namespace}.{tenant}.{category})
//   - Defaults to: normalized ENVIRONMENT value if not set
//   - Example: KAFKA_TOPIC_NAMESPACE=prod means "consume from prod.* topics"
//
// Why separate?
//   - Allows develop environment to consume from prod topics for testing
//   - Keeps logs/metrics accurate (shows "develop" not "prod")
//   - Explicit about intent - clearly shows cross-namespace access
//   - Safer - can't accidentally affect non-topic environment behavior
//
// Valid namespaces (configured via Helm): local, dev, stg, prod
//
// Topic names are built at runtime using BuildTopicName() in topic.go:
//   BuildTopicName(namespace, tenantID, category) -> "{namespace}.{tenantID}.{category}"
//
// =============================================================================

// NormalizeEnv normalizes the namespace string (lowercase, trim whitespace).
// The actual namespace value comes from configuration (Helm charts), not code mapping.
//
// Valid namespaces (configured via Helm):
//   - local
//   - dev
//   - stg
//   - prod
//
// Empty string defaults to "local" for backward compatibility.
func NormalizeEnv(env string) string {
	env = strings.ToLower(strings.TrimSpace(env))
	if env == "" {
		return "local"
	}
	return env
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

// TopicToEventType maps a full topic name to its category.
// This extracts the category (last part) from any topic format.
//
// Example:
//
//	"prod.odin.trade" -> "trade"
//	"dev.acme.balances" -> "balances"
//	"odin.dev.trade" (legacy) -> "trade"
func TopicToEventType(topic string) string {
	// Handle new format: {namespace}.{tenant}.{category}
	// Handle legacy format: odin.{env}.{category}
	parts := strings.Split(topic, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-1] // Return last part (category)
	}

	// Handle legacy format with odin. prefix
	topic = strings.TrimPrefix(topic, "odin.")
	if _, after, found := strings.Cut(topic, "."); found {
		return after
	}

	return topic
}
