# Token Data Update Events

This document catalogs every known backend event that mutates token-derived data surfaced in the Sukko front-end. Use it to design websocket topics or fan-out flows between the API (publisher), NATS, websocket server, and clients.

## How to Read
- **Trigger** – What action occurs in the system.
- **Source** – Component or API performing the action.
- **Affected Fields** – Token fields (per `/Volumes/Dev/Codev/Toniq/sukko/src/app/models/token.ts`) updated as a result.
- **Consumers** – Front-end screens/components depending on the data.

## 1. Trading Lifecycle
- **Trigger**: Buy token with BTC
  - **Source**: `CanisterContextProvider.sukko.buyTokenWithBtc()` (`/Volumes/Dev/Codev/Toniq/sukko/src/app/services/CanisterContextProvider.tsx`) invoked from `QuickBuy` (`/Volumes/Dev/Codev/Toniq/sukko/src/app/components/TokenTable/QuickBuy.tsx`) or trade flows (`/Volumes/Dev/Codev/Toniq/sukko/src/app/components/TokenPlaceTxn/TokenPlaceTxn.tsx`).
  - **Affected Fields**: `price`, `marketcap`, `volume`, `swap_volume`, `swap_volume_24`, `buy_count`, `holder_count`, `sold`, `last_action_time`, `btc_liquidity`, `token_liquidity`, `liquidity_threshold`, `price_*`, `price_delta_*`, derived `bonding_curve`, `ownership`, `progress`, and user liquidity fields (`user_lp_tokens`, `user_btc_liquidity`, `user_token_liquidity`).
  - **Consumers**: `TokenTable`, `TokenPage` detail tab, quick buy widgets, liquidity health.

- **Trigger**: Sell token for BTC
  - **Source**: `CanisterContextProvider.sukko.sellTokenForBtc()` (same entry points as above).
  - **Affected Fields**: `price`, `marketcap`, `volume`, `swap_volume`, `swap_volume_24`, `sell_count`, `holder_count`, `sold`, `last_action_time`, liquidity metrics, price history deltas, derived numbers.
  - **Consumers**: Same as buy events.

- **Trigger**: Buy token by specifying token amount (`buyTokenAmount`) / Sell token by amount (`sellTokenAmount`)
  - **Source**: `CanisterContextProvider` trade helpers, typically triggered from `TokenPlaceTxn` advanced mode.
  - **Affected Fields**: Same as buy/sell above.
  - **Consumers**: Same as above.

- **Trigger**: Token trade completes in backend from other clients (not initiated by this user)
  - **Source**: External trades processed by core API/canister.
  - **Affected Fields**: Same as above; clients must receive updates even if not originator.
  - **Consumers**: Global token list, token detail pages.

## 2. Liquidity Lifecycle
- **Trigger**: Add liquidity
  - **Source**: `CanisterContextProvider.sukko.addLiquidity()` invoked within `TokenPlaceTxn` when `TradeActionEnum.Add`.
  - **Affected Fields**: `btc_liquidity`, `token_liquidity`, `liquidity_threshold`, user liquidity fields, potentially `price`, `marketcap`, `swap_volume_*`, `last_action_time` if backend records as activity.
  - **Consumers**: `TokenTable`, liquidity dashboards, liquidity health widgets.

- **Trigger**: Remove liquidity
  - **Source**: `CanisterContextProvider.sukko.removeLiquidity()` from `TokenPlaceTxn` (`TradeActionEnum.Remove`).
  - **Affected Fields**: Same liquidity metrics as add; may affect overall price/liquidity-derived fields.
  - **Consumers**: Same as above.

- **Trigger**: Backend rebalances / automatic liquidity adjustments
  - **Source**: Core API/canister events (not necessarily user-facing component).
  - **Affected Fields**: Liquidity metrics, potentially price-related derived values.
  - **Consumers**: `TokenTable`, token detail pages.

## 3. Metadata & Verification
- **Trigger**: Token metadata update (name/ticker/description/socials/verified)
  - **Source**: `api.token.updateToken()` called via `ManageTokenDialog` (`/Volumes/Dev/Codev/Toniq/sukko/src/app/components/Dialogs/ManageTokenDialog/ManageTokenDialog.tsx`).
  - **Affected Fields**: `name`, `ticker`, `description`, `twitter`, `telegram`, `website`, `verified`.
  - **Consumers**: `TokenTable`, token header, sharing dialogs.

- **Trigger**: Twitter verification
  - **Source**: `api.token.verifyTwitter()` (OAuth callback at `/Volumes/Dev/Codev/Toniq/sukko/src/app/oauth/twitter/page.tsx`).
  - **Affected Fields**: `twitter_verified` and possibly `twitter` metadata.
  - **Consumers**: Token badges, filters requiring verified social accounts.

- **Trigger**: Backend flag updates (featured/bonded/external/trading)
  - **Source**: Administrative or automated backend processes.
  - **Affected Fields**: `featured`, `bonded`, `external`, `trading` flags.
  - **Consumers**: `TokenTable` preset filters, token cards.

## 4. Comments & Community Activity
- **Trigger**: Post comment / community comment
  - **Source**: `api.token.addCommentForToken()` via `TokenCommentForm` (`/Volumes/Dev/Codev/Toniq/sukko/src/app/components/TokenCommentForm/TokenCommentForm.tsx`).
  - **Affected Fields**: `comment_count`, `last_comment_time`, potentially `last_action_time`.
  - **Consumers**: Token comment feeds, metrics displayed in table and detail.

- **Trigger**: Pin/unpin comment
  - **Source**: `api.token.pinComment()` / `api.token.unpinComment()`.
  - **Affected Fields**: May influence ordering in feeds; confirm whether additional token metrics change (usually none, but document as “meta change”).
  - **Consumers**: Comment components, pinned section.

- **Trigger**: Upvote/remove upvote comment
  - **Source**: `api.token.upvoteComment()` / `api.token.removeUpvoteComment()`.
  - **Affected Fields**: Comment-level data; token summary not directly impacted unless backend aggregates to `comment_count` or `last_action_time`.
  - **Consumers**: Comment lists; note because websocket might fan updates to comment subscribers.

## 5. Favorites & User-Specific Views
- **Trigger**: Favorite/unfavorite token
  - **Source**: `api.token.updateFavoriteForToken()` accessed via `FavoriteToken` component, `UserFavoritesPoller`.
  - **Affected Fields**: `favorite` status surfaces through filters and counts; may not mutate token core fields but influences derived `favorite` filter results and user-specific state.
  - **Consumers**: `TokenTable` when `favorite` filter active, user profile favorites list.

- **Trigger**: Backend updates on user favorites (e.g., mass import)
  - **Source**: API processes.
  - **Affected Fields**: `favorite` relationships per user.
  - **Consumers**: Same as above.

## 6. Token Creation & Listing
- **Trigger**: Create new token
  - **Source**: `CanisterContextProvider.sukko.createToken()` (UI flow not shown in detail).
  - **Affected Fields**: New token row inserted with metadata, initial liquidity, `created_time`, etc.
  - **Consumers**: `TokenTable`, token discovery components, route watchers (new entries).

- **Trigger**: Token list/listing updates (e.g., new listing API events)
  - **Source**: Backend creation or listing operations.
  - **Affected Fields**: Entire token object for newly listed token; may also set `featured`, `bonded`, etc.
  - **Consumers**: `TokenTable`, notifications.

## 7. Backend-Driven Metrics & Analytics
- **Trigger**: Scheduled analytics calculations (price deltas, trending, ascension progress)
  - **Source**: Backend jobs updating fields like `price_delta_5m/1h/6h/1d`, `progress`, `featured`, `trending` sorts.
  - **Affected Fields**: Price delta metrics, `progress`, `featured`, `bonded`, other flags.
  - **Consumers**: `TokenTable` preset filters, trend widgets.

- **Trigger**: Holder count recalculation
  - **Source**: Backend after transfers/trades.
  - **Affected Fields**: `holder_count`, potentially `volume`, `marketcap`.
  - **Consumers**: Holder stats displays, token detail metrics.

## 8. Balances & User Holdings (indirect but relevant)
- **Trigger**: User balance updates after trade/liquidity
  - **Source**: `updateLocalBalance()` and `triggerUpdateBalance()` in `CanisterContextProvider`, with backend confirmation consumed by `UserBalancesPoller`.
  - **Affected Fields**: User-specific `Balance` data (not token schema), but these actions often correlate with token metric changes above.
  - **Consumers**: Wallet components, quick buy UI (for available amounts).

## Implementation Notes for Websocket Design
- Each trigger above should emit a domain event on NATS (e.g., `token.trade.executed`, `token.liquidity.added`, `token.metadata.updated`).
- Websocket server should fan out payloads containing minimal diff or full token snapshot, depending on client needs.
- Consider separate channels/topics for token list updates, token detail (per-token), user-specific streams (favorites, balances), and comment-related updates.
- Reference files:
  - Token model: `/Volumes/Dev/Codev/Toniq/sukko/src/app/models/token.ts`
  - Trade UI/calls: `/Volumes/Dev/Codev/Toniq/sukko/src/app/components/TokenTable/QuickBuy.tsx`, `/Volumes/Dev/Codev/Toniq/sukko/src/app/components/TokenPlaceTxn/TokenPlaceTxn.tsx`, `/Volumes/Dev/Codev/Toniq/sukko/src/app/services/CanisterContextProvider.tsx`
  - Metadata update: `/Volumes/Dev/Codev/Toniq/sukko/src/app/components/Dialogs/ManageTokenDialog/ManageTokenDialog.tsx`
  - Comments: `/Volumes/Dev/Codev/Toniq/sukko/src/app/components/TokenCommentForm/TokenCommentForm.tsx`
  - Favorites: `/Volumes/Dev/Codev/Toniq/sukko/src/components/Pollers/UserFavoritesPoller.tsx`
  - Token polling: `/Volumes/Dev/Codev/Toniq/sukko/src/app/tokens/page.tsx`

Keep this document up to date as backend capabilities evolve so websocket topics stay aligned with actual mutating events.
