# Pagination Patterns for Real-Time WebSocket Systems

---

## Overview

This document explains pagination strategies for systems that combine REST APIs with real-time WebSocket updates. The challenge: how do you paginate historical data while simultaneously receiving live updates?

---

## The Problem

Traditional pagination assumes static data:

```
Page 1: items 1-10
Page 2: items 11-20
Page 3: items 21-30
```

But with real-time updates, new items arrive constantly:

```
User loads Page 1 (items 1-10)
3 new items arrive (now items 1-13)
User requests Page 2...
  - Offset-based: Gets items 11-20 (WRONG - misses items 11-13, gets duplicates)
  - Cursor-based: Gets items after item 10 (CORRECT - gets 11-20 as expected)
```

---

## Industry Standard: Cursor-Based Pagination

**Cursor-based pagination is the industry standard** for real-time systems. Here's why major platforms use it:

### Twitter/X

```json
{
  "data": [...tweets...],
  "meta": {
    "next_token": "7140w",
    "previous_token": "77qp8"
  }
}
```

- Cursors are opaque tokens
- Stable during timeline mutations
- Handles deletions gracefully

### Discord

```
GET /channels/{id}/messages?before=123456789&limit=50
GET /channels/{id}/messages?after=123456789&limit=50
```

- Uses message IDs (Snowflakes) as cursors
- Snowflakes encode timestamp + sequence
- Natural ordering without separate sort field

### Slack

```json
{
  "messages": [...],
  "response_metadata": {
    "next_cursor": "dGVhbTpDMDYxRkE1UEI="
  }
}
```

- Base64-encoded cursors
- Cursor encodes position + query context
- Consistent across real-time updates

### Facebook/Meta Graph API

```json
{
  "data": [...],
  "paging": {
    "cursors": {
      "before": "QVFIUk...",
      "after": "QVFIUl..."
    },
    "next": "https://graph.facebook.com/...&after=QVFIUl..."
  }
}
```

- Bidirectional cursors
- URL-encoded for easy navigation
- Works with Feed's real-time updates

---

## Why Not Offset-Based?

| Aspect | Offset-Based | Cursor-Based |
|--------|--------------|--------------|
| **New items added** | Duplicates on next page | Stable, no duplicates |
| **Items deleted** | Skips items | Stable, no gaps |
| **Performance at scale** | O(n) - must skip rows | O(1) - seeks directly |
| **Concurrent updates** | Race conditions | Naturally handles |
| **Caching** | Invalidated by changes | Cache-friendly |
| **Deep pagination** | Expensive (OFFSET 10000) | Efficient (WHERE id > X) |

### The Mutation Problem

```
Initial state: [A, B, C, D, E, F, G, H, I, J]
User fetches page 1 (offset=0, limit=5): [A, B, C, D, E]

New item X arrives: [X, A, B, C, D, E, F, G, H, I, J]

Offset-based page 2 (offset=5): [E, F, G, H, I]  // E is duplicated!
Cursor-based (after=E): [F, G, H, I, J]          // Correct!
```

---

## The Three-Part Architecture

Modern real-time systems use this pattern:

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLIENT APPLICATION                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│   │   REST API   │    │  WebSocket   │    │    UI State      │  │
│   │   (History)  │    │   (Live)     │    │    Manager       │  │
│   └──────┬───────┘    └──────┬───────┘    └────────┬─────────┘  │
│          │                   │                      │            │
│          │ Historical        │ Real-time           │            │
│          │ cursor-paginated  │ streaming           │            │
│          │                   │                      │            │
│          └───────────────────┴──────────────────────┘            │
│                              │                                   │
│                              ▼                                   │
│                    ┌─────────────────┐                          │
│                    │   Merged View   │                          │
│                    │  (Deduped by ID)│                          │
│                    └─────────────────┘                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### How It Works

1. **Initial Load (REST)**
   ```
   GET /api/trades?limit=50
   Response: { data: [...], cursor: "abc123" }
   ```

2. **WebSocket Connection**
   ```javascript
   ws.subscribe('trades', { token: 'SOL' })
   // Receives: { type: 'trade', data: {...} }
   ```

3. **UI Merge Logic**
   ```javascript
   const mergedTrades = new Map()

   // Add historical trades
   historicalTrades.forEach(t => mergedTrades.set(t.id, t))

   // Add/update from WebSocket
   ws.onTrade(t => mergedTrades.set(t.id, t))

   // Render sorted by timestamp
   const display = [...mergedTrades.values()]
     .sort((a, b) => b.timestamp - a.timestamp)
   ```

4. **Load More (REST with cursor)**
   ```
   GET /api/trades?cursor=abc123&limit=50
   Response: { data: [...], cursor: "def456" }
   ```

---

## Cursor Implementation Patterns

### 1. Opaque Encoded Cursor (Recommended)

```go
type Cursor struct {
    ID        string    `json:"i"`
    Timestamp time.Time `json:"t"`
    Direction string    `json:"d"` // "next" or "prev"
}

func EncodeCursor(c Cursor) string {
    data, _ := json.Marshal(c)
    return base64.URLEncoding.EncodeToString(data)
}

func DecodeCursor(s string) (Cursor, error) {
    data, err := base64.URLEncoding.DecodeString(s)
    if err != nil {
        return Cursor{}, err
    }
    var c Cursor
    err = json.Unmarshal(data, &c)
    return c, err
}
```

**Pros:**
- Hides implementation details
- Can encode multiple fields
- Version-able (add fields later)

### 2. ID-Based Cursor (Simple)

```go
// Query: WHERE id > :cursor ORDER BY id ASC LIMIT :limit
func GetTradesAfter(cursor string, limit int) ([]Trade, string) {
    trades := db.Query("SELECT * FROM trades WHERE id > ? ORDER BY id LIMIT ?", cursor, limit)

    var nextCursor string
    if len(trades) == limit {
        nextCursor = trades[len(trades)-1].ID
    }
    return trades, nextCursor
}
```

**Pros:**
- Simple implementation
- Works with Snowflake IDs
- No encoding/decoding

### 3. Timestamp + ID Cursor (Tiebreaker)

```go
// For when multiple items have same timestamp
type Cursor struct {
    Timestamp time.Time
    ID        string
}

// Query: WHERE (timestamp, id) > (:ts, :id) ORDER BY timestamp, id LIMIT :limit
```

**Pros:**
- Handles duplicate timestamps
- Natural time ordering
- Good for event streams

### 4. Keyset Pagination (Database-native)

```sql
-- Instead of: OFFSET 1000 LIMIT 50
-- Use keyset:
SELECT * FROM trades
WHERE (created_at, id) > ('2025-01-01 12:00:00', 'abc123')
ORDER BY created_at DESC, id DESC
LIMIT 50;
```

**Pros:**
- Database-optimized
- Index-friendly
- Consistent performance

---

## Odin-Specific Recommendations

For the Odin WebSocket system, here's the recommended approach:

### REST API Design

```go
// GET /api/v1/trades?token=SOL&limit=50
// GET /api/v1/trades?token=SOL&cursor=xyz&limit=50

type TradesResponse struct {
    Data   []Trade `json:"data"`
    Cursor string  `json:"cursor,omitempty"` // Empty if no more pages
    Meta   struct {
        HasMore bool `json:"has_more"`
        Total   int  `json:"total,omitempty"` // Optional, expensive
    } `json:"meta"`
}
```

### Cursor Structure

```go
type TradeCursor struct {
    Timestamp int64  `json:"t"` // Unix nano
    TradeID   string `json:"i"`
}
```

### WebSocket Message Format

```go
type TradeEvent struct {
    Type      string `json:"type"`      // "trade"
    Token     string `json:"token"`     // "SOL"
    TradeID   string `json:"trade_id"`  // For deduplication
    Timestamp int64  `json:"timestamp"` // For ordering
    Data      Trade  `json:"data"`
}
```

### Client Merge Strategy

```typescript
class TradeManager {
    private trades = new Map<string, Trade>()
    private cursor: string | null = null

    // Called on REST response
    addHistorical(trades: Trade[], cursor: string) {
        trades.forEach(t => this.trades.set(t.id, t))
        this.cursor = cursor
    }

    // Called on WebSocket message
    addLive(trade: Trade) {
        this.trades.set(trade.id, trade)
    }

    // Get sorted, deduplicated list
    getAll(): Trade[] {
        return [...this.trades.values()]
            .sort((a, b) => b.timestamp - a.timestamp)
    }

    // Load more historical
    async loadMore() {
        if (!this.cursor) return
        const res = await fetch(`/api/trades?cursor=${this.cursor}`)
        const { data, cursor } = await res.json()
        this.addHistorical(data, cursor)
    }
}
```

---

## Edge Cases and Solutions

### 1. Rapid Updates During Pagination

**Problem:** User is scrolling through history while many new trades arrive.

**Solution:** Client maintains two lists:
- `historical`: Paginated from REST
- `live`: From WebSocket since page load

```typescript
// UI shows: [...live, ...historical] with deduplication
```

### 2. Reconnection Gaps

**Problem:** WebSocket disconnects, misses some events.

**Solution:** On reconnect, fetch gap:
```typescript
ws.onReconnect = async () => {
    const lastKnown = getLatestTradeTimestamp()
    const missed = await fetch(`/api/trades?since=${lastKnown}`)
    mergeTrades(missed.data)
}
```

### 3. Very Old Data Requests

**Problem:** User scrolls to very old history (expensive query).

**Solution:**
- Time-based archival cutoff
- Different endpoint for archived data
- Lazy loading with virtualized scrolling

### 4. Deleted Items

**Problem:** Item was in cursor range but got deleted.

**Solution:** Cursor-based naturally handles this:
```sql
-- If item 'abc' was deleted, this still works
WHERE id > 'abc' ORDER BY id LIMIT 50
-- Returns next items regardless of 'abc' existence
```

---

## Performance Considerations

### Database Indexing

```sql
-- Essential index for cursor pagination
CREATE INDEX idx_trades_timestamp_id ON trades(timestamp DESC, id DESC);

-- For token-filtered queries
CREATE INDEX idx_trades_token_timestamp ON trades(token, timestamp DESC, id DESC);
```

### Caching Strategy

```
┌─────────────────────────────────────────────────┐
│              Pagination Cache                    │
├─────────────────────────────────────────────────┤
│ Key: trades:SOL:cursor:abc123                   │
│ Value: {data: [...], nextCursor: "def456"}      │
│ TTL: 5 minutes (short, data changes)            │
├─────────────────────────────────────────────────┤
│ Key: trades:SOL:latest                          │
│ Value: {data: [...], cursor: "abc123"}          │
│ TTL: 30 seconds (very short for live data)      │
└─────────────────────────────────────────────────┘
```

### Rate Limiting

```go
// Pagination requests should be rate-limited separately
rateLimiter.Allow("pagination", userID, 10, time.Second) // 10 req/sec
rateLimiter.Allow("websocket", userID, 100, time.Second) // 100 msg/sec
```

---

## Summary

| Component | Pattern | Notes |
|-----------|---------|-------|
| **REST API** | Cursor-based pagination | Opaque base64 cursors |
| **Cursor** | Timestamp + ID | Handles ties, natural order |
| **WebSocket** | Include trade_id | For client deduplication |
| **Client** | Map-based merge | O(1) dedup, O(n log n) sort |
| **Database** | Keyset pagination | Index on (timestamp, id) |
| **Cache** | Short TTL | Data changes frequently |

---

## References

- [Twitter API Pagination](https://developer.twitter.com/en/docs/twitter-api/pagination)
- [Discord API - Get Channel Messages](https://discord.com/developers/docs/resources/channel#get-channel-messages)
- [Slack API - Pagination](https://api.slack.com/docs/pagination)
- [Use The Index, Luke - Keyset Pagination](https://use-the-index-luke.com/no-offset)
- [Shopify Engineering - Pagination](https://shopify.engineering/pagination-relative-cursors)
