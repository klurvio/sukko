package kafka

import (
	"strings"
)

// =============================================================================
// Topic Namespace Configuration
// =============================================================================
//
// ENVIRONMENT vs KAFKA_TOPIC_NAMESPACE_OVERRIDE:
//
// ENVIRONMENT: Identifies the deployment environment (dev, stg, prod).
//   - Used for: logging, metrics labels, consumer group naming, operational context
//   - Example: ENVIRONMENT=dev means "this is the dev deployment"
//
// KAFKA_TOPIC_NAMESPACE_OVERRIDE: Overrides ENVIRONMENT for Kafka topic naming only.
//   - Used for: topic naming only ({namespace}.{tenant}.{category})
//   - Defaults to: empty (uses normalized ENVIRONMENT value)
//   - Example: KAFKA_TOPIC_NAMESPACE_OVERRIDE=prod means "consume from prod.* topics"
//   - NOT allowed in production (startup validation blocks this)
//
// Resolution: Use ResolveNamespace(override, environment) to get the effective namespace.
//
// Why separate?
//   - Allows dev/stg environments to consume from prod topics (Sukko API always publishes as prod)
//   - Keeps logs/metrics accurate (shows "dev" not "prod")
//   - Explicit about intent - "Override" in the name makes cross-env consumption deliberate
//   - Safer - can't accidentally affect non-topic environment behavior
//   - Blocked in prod - startup validation prevents override in production
//
// Valid namespaces (configured via Helm): local, dev, stg, prod
//
// Topic names are built at runtime using BuildTopicName() in topic.go:
//   BuildTopicName(namespace, tenantID, category) -> "{namespace}.{tenantID}.{category}"
//
// =============================================================================

// ResolveNamespace resolves the effective topic namespace from an override and environment.
// If override is non-empty, it takes precedence (normalized). Otherwise, falls back to
// the normalized environment value. Both inputs are lowercased and trimmed.
//
// This is the single source of truth for namespace resolution — used by both ws-server
// and provisioning to eliminate duplicated logic.
//
// Examples:
//   - ResolveNamespace("prod", "dev") → "prod" (override wins)
//   - ResolveNamespace("", "dev") → "dev" (fallback to environment)
//   - ResolveNamespace("", "") → "" (no default — BaseConfig validates non-empty at startup)
func ResolveNamespace(override, environment string) string {
	if override != "" {
		return strings.ToLower(strings.TrimSpace(override))
	}
	return strings.ToLower(strings.TrimSpace(environment))
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
//	"prod.sukko.trade" -> "trade"
//	"dev.acme.balances" -> "balances"
//	"sukko.dev.trade" (legacy) -> "trade"
func TopicToEventType(topic string) string {
	// Handle new format: {namespace}.{tenant}.{category}
	// Handle legacy format: sukko.{env}.{category}
	parts := strings.Split(topic, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-1] // Return last part (category)
	}

	// Handle legacy format with sukko. prefix
	topic = strings.TrimPrefix(topic, "sukko.")
	if _, after, found := strings.Cut(topic, "."); found {
		return after
	}

	return topic
}
