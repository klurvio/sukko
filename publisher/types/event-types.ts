// Event type to Redpanda topic mapping (matches v1 deployment)
export const EVENT_TYPE_TOPICS = {
  // Trading events → odin.trades
  TRADE_EXECUTED: 'odin.trades',
  BUY_COMPLETED: 'odin.trades',
  SELL_COMPLETED: 'odin.trades',

  // Liquidity events → odin.liquidity
  LIQUIDITY_ADDED: 'odin.liquidity',
  LIQUIDITY_REMOVED: 'odin.liquidity',
  LIQUIDITY_REBALANCED: 'odin.liquidity',

  // Metadata events → odin.metadata
  METADATA_UPDATED: 'odin.metadata',
  TOKEN_NAME_CHANGED: 'odin.metadata',
  TOKEN_FLAGS_CHANGED: 'odin.metadata',

  // Social events → odin.social
  TWITTER_VERIFIED: 'odin.social',
  SOCIAL_LINKS_UPDATED: 'odin.social',

  // Community events → odin.community
  COMMENT_POSTED: 'odin.community',
  COMMENT_PINNED: 'odin.community',
  COMMENT_UPVOTED: 'odin.community',
  FAVORITE_TOGGLED: 'odin.community',

  // Creation events → odin.creation
  TOKEN_CREATED: 'odin.creation',
  TOKEN_LISTED: 'odin.creation',

  // Analytics events → odin.analytics
  PRICE_DELTA_UPDATED: 'odin.analytics',
  HOLDER_COUNT_UPDATED: 'odin.analytics',
  ANALYTICS_RECALCULATED: 'odin.analytics',
  TRENDING_UPDATED: 'odin.analytics',

  // Balance events → odin.balances
  BALANCE_UPDATED: 'odin.balances',
  TRANSFER_COMPLETED: 'odin.balances',
} as const;

export type EventType = keyof typeof EVENT_TYPE_TOPICS;
export type TopicName = (typeof EVENT_TYPE_TOPICS)[EventType];

// Token event payload interface
export interface TokenEvent {
  type: EventType;
  tokenId: string;
  timestamp: number;
  data: Record<string, any>;
}
