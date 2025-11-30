package kafka

// Topic names for all event types
const (
	TopicTrades    = "odin.trades"
	TopicLiquidity = "odin.liquidity"
	TopicMetadata  = "odin.metadata"
	TopicSocial    = "odin.social"
	TopicCommunity = "odin.community"
	TopicCreation  = "odin.creation"
	TopicAnalytics = "odin.analytics"
	TopicBalances  = "odin.balances"
)

// AllTopics returns all topic names
func AllTopics() []string {
	return []string{
		TopicTrades,
		TopicLiquidity,
		TopicMetadata,
		TopicSocial,
		TopicCommunity,
		TopicCreation,
		TopicAnalytics,
		TopicBalances,
	}
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

// TopicToEventType maps topic names to their event type category
func TopicToEventType(topic string) string {
	switch topic {
	case TopicTrades:
		return "trade"
	case TopicLiquidity:
		return "liquidity"
	case TopicMetadata:
		return "metadata"
	case TopicSocial:
		return "social"
	case TopicCommunity:
		return "community"
	case TopicCreation:
		return "creation"
	case TopicAnalytics:
		return "analytics"
	case TopicBalances:
		return "balances"
	default:
		return "unknown"
	}
}
