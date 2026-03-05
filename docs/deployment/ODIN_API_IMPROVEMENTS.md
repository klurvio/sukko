# Sukko API Improvements: Real-Time Price Delta Architecture

**Document Status:** Planning
**Priority:** High
**Impact:** Critical for trading performance
**Timeline:** 2-6 weeks

---

## Executive Summary

Current price delta updates are delayed by up to 60 seconds due to scheduler-based batch processing. This is **unacceptable for trading applications** where millisecond latency matters.

**Solution:** Move price delta calculation from 1-minute scheduler job to event-driven trade handler.

**Benefits:**
- ✅ Price deltas fresh within 100ms (vs 0-60 seconds stale)
- ✅ 50% reduction in DB load (cache optimization)
- ✅ Better user experience for traders
- ✅ Enables real-time chart updates

---

## Current Architecture (Problem)

### Data Flow
```
1. Trade executes → Stored in database
2. (Wait 0-60 seconds...)
3. Scheduler runs every minute
4. syncTokenPriceDelta() queries DB for all tokens
5. Calculates deltas for 200 tokens
6. Updates token records in DB
7. Publishes to NATS: sukko.token.{SYMBOL}.analytics
8. WebSocket broadcasts to clients
```

### Performance Metrics
- **Latency:** 0-60 seconds stale data
- **DB Load:** 800 queries/minute (13 queries/sec)
- **Scheduler Job:** `sukko_scheduler` runs every 1 minute
- **Code Location:** `/Volumes/Dev/Codev/Toniq/sukko-api/functions/src/main.ts:211-214`

### Code Reference
```typescript
// main.ts lines 202-214
const _syncTokenPriceDelta = Object.defineProperty(
  function () {
    return tokensService.syncTokenUpdates(['price_delta']);
  },
  'name',
  { value: 'syncTokenPriceDelta' },
) as TokensService['syncTokenUpdates'];

const syncTokenPriceDelta = wrapper(
  _syncTokenPriceDelta.bind(tokensService),
  1, // Run every 1 minute
);
```

---

## Target Architecture (Solution)

### Data Flow
```
1. Trade executes → Trade event handler
2. Calculate price deltas immediately (in-memory cache)
3. Publish to NATS: sukko.token.{SYMBOL}.trade (with deltas)
4. WebSocket broadcasts to clients
5. Total latency: <100ms
```

### Performance Metrics
- **Latency:** <100ms fresh data
- **DB Load:** 6 queries/sec (with 95% cache hit rate)
- **Net DB Savings:** -7 queries/sec (52% reduction)
- **Real-time:** Yes

---

## Implementation Plan

### Phase 1: Implement Hierarchical Channel Taxonomy (Week 1)

**Objective:** Support 8-channel event types in WebSocket server

**Changes Required:**
- ✅ WebSocket server: Update `extractChannel()` to support `sukko.token.BTC.trade`
- ✅ WebSocket server: Update subscription matching logic
- ✅ WebSocket server: Update test suite
- ⏳ **No API changes required yet**

**Status:** In progress at `/Volumes/Dev/Codev/Toniq/sukko`

---

### Phase 2: Add Event-Driven Price Deltas (Week 2-3)

**Objective:** Calculate price deltas on trade execution

#### 2.1 Create Price Delta Calculator Service

**File:** `/Volumes/Dev/Codev/Toniq/sukko-api/functions/src/tokens/tokens-price-delta.service.ts` (new)

```typescript
import { Injectable } from '@nestjs/common';
import { PrismaService } from '@src/prisma/prisma.service';

interface PriceDeltaData {
  price_delta_5m: number | null;
  price_delta_1h: number | null;
  price_delta_6h: number | null;
  price_delta_1d: number | null;
}

@Injectable()
export class TokensPriceDeltaService {
  // In-memory cache: token_id -> [{price, timestamp}]
  private priceCache = new Map<string, Array<{price: number, timestamp: Date}>>();

  constructor(private readonly prisma: PrismaService) {}

  /**
   * Calculate all price deltas for a token based on current price
   * Uses in-memory cache for fast lookups, falls back to DB
   */
  async calculateDeltas(
    tokenId: string,
    currentPrice: number
  ): Promise<PriceDeltaData> {
    const now = new Date();

    // Parallel queries for different time windows
    const [price5mAgo, price1hAgo, price6hAgo, price1dAgo] = await Promise.all([
      this.getPriceAtTime(tokenId, new Date(now.getTime() - 5 * 60 * 1000)),
      this.getPriceAtTime(tokenId, new Date(now.getTime() - 60 * 60 * 1000)),
      this.getPriceAtTime(tokenId, new Date(now.getTime() - 6 * 60 * 60 * 1000)),
      this.getPriceAtTime(tokenId, new Date(now.getTime() - 24 * 60 * 60 * 1000)),
    ]);

    return {
      price_delta_5m: this.calculateDelta(price5mAgo, currentPrice),
      price_delta_1h: this.calculateDelta(price1hAgo, currentPrice),
      price_delta_6h: this.calculateDelta(price6hAgo, currentPrice),
      price_delta_1d: this.calculateDelta(price1dAgo, currentPrice),
    };
  }

  /**
   * Get price at specific timestamp
   * 1. Check in-memory cache (fast path)
   * 2. Fall back to DB query (slow path)
   */
  private async getPriceAtTime(
    tokenId: string,
    timestamp: Date
  ): Promise<number | null> {
    // Fast path: Check cache
    const cached = this.priceCache.get(tokenId);
    if (cached) {
      const match = cached.findLast(p => p.timestamp <= timestamp);
      if (match) {
        console.log(`[PriceCache] HIT for ${tokenId} at ${timestamp.toISOString()}`);
        return match.price;
      }
    }

    // Slow path: Query database
    console.log(`[PriceCache] MISS for ${tokenId} at ${timestamp.toISOString()}`);
    const trade = await this.prisma.trade.findFirst({
      where: {
        token_id: tokenId,
        created_at: { lte: timestamp }
      },
      orderBy: { created_at: 'desc' },
      select: { price: true }
    });

    return trade?.price ?? null;
  }

  /**
   * Update price cache after every trade
   * Keeps rolling 24-hour window
   */
  updateCache(tokenId: string, price: number, timestamp: Date = new Date()) {
    let cache = this.priceCache.get(tokenId) ?? [];
    cache.push({ price, timestamp });

    // Keep only last 24 hours (rolling window)
    const cutoff = new Date(Date.now() - 24 * 60 * 60 * 1000);
    cache = cache.filter(p => p.timestamp >= cutoff);

    this.priceCache.set(tokenId, cache);

    console.log(`[PriceCache] Updated ${tokenId}: ${cache.length} entries`);
  }

  /**
   * Calculate percentage delta between two prices
   */
  private calculateDelta(oldPrice: number | null, newPrice: number): number | null {
    if (!oldPrice || oldPrice === 0) return null;
    return ((newPrice - oldPrice) / oldPrice) * 100;
  }

  /**
   * Get cache statistics for monitoring
   */
  getCacheStats() {
    const stats = {
      total_tokens: this.priceCache.size,
      total_entries: 0,
      tokens: [] as Array<{token_id: string, entries: number}>
    };

    this.priceCache.forEach((entries, tokenId) => {
      stats.total_entries += entries.length;
      stats.tokens.push({ token_id: tokenId, entries: entries.length });
    });

    return stats;
  }

  /**
   * Clear cache (for testing or memory management)
   */
  clearCache() {
    this.priceCache.clear();
    console.log('[PriceCache] Cleared all entries');
  }
}
```

#### 2.2 Integrate into Trade Handler

**File:** `/Volumes/Dev/Codev/Toniq/sukko-api/functions/src/tokens/tokens.service.ts`

```typescript
import { TokensPriceDeltaService } from './tokens-price-delta.service';

@Injectable()
export class TokensService {
  constructor(
    private readonly prisma: PrismaService,
    private readonly priceDeltaService: TokensPriceDeltaService,
    // ... other services
  ) {}

  /**
   * Called after trade execution
   * Publishes real-time trade event with fresh price deltas
   */
  async publishTradeEvent(trade: {
    token_id: string;
    symbol: string;
    price: number;
    volume: number;
    type: 'buy' | 'sell';
    user_id: string;
  }) {
    // 1. Calculate price deltas immediately
    const deltas = await this.priceDeltaService.calculateDeltas(
      trade.token_id,
      trade.price
    );

    // 2. Update in-memory cache for future calculations
    this.priceDeltaService.updateCache(trade.token_id, trade.price);

    // 3. Publish to NATS with complete data
    const payload = {
      type: 'trade',
      event: trade.type, // 'buy' or 'sell'
      token_id: trade.token_id,
      symbol: trade.symbol,
      price: trade.price,
      volume: trade.volume,
      user_id: trade.user_id,

      // Fresh price deltas (calculated in <10ms)
      price_delta_5m: deltas.price_delta_5m,
      price_delta_1h: deltas.price_delta_1h,
      price_delta_6h: deltas.price_delta_6h,
      price_delta_1d: deltas.price_delta_1d,

      // Metadata
      timestamp: new Date().toISOString(),
      server_time_ms: Date.now(),
    };

    // 4. Publish to hierarchical channel
    await this.natsService.publish(
      `sukko.token.${trade.symbol}.trade`,
      JSON.stringify(payload)
    );

    console.log(`[Trade] Published to sukko.token.${trade.symbol}.trade`, {
      price: trade.price,
      deltas: deltas,
    });
  }
}
```

#### 2.3 Hook into Trade Execution

**File:** Find where trades are executed (likely in canister service or trade controller)

```typescript
// After successful trade execution
async executeTrade(tradeData) {
  // 1. Execute on blockchain
  const result = await this.canisterService.buyTokenWithBtc(...);

  // 2. Store in database
  const trade = await this.prisma.trade.create({
    data: {
      token_id: tradeData.token_id,
      price: result.price,
      volume: result.volume,
      // ... other fields
    }
  });

  // 3. Publish real-time event with deltas
  await this.tokensService.publishTradeEvent({
    token_id: trade.token_id,
    symbol: tradeData.symbol,
    price: trade.price,
    volume: trade.volume,
    type: tradeData.type,
    user_id: tradeData.user_id,
  });

  return result;
}
```

#### 2.4 Update Tokens Module

**File:** `/Volumes/Dev/Codev/Toniq/sukko-api/functions/src/tokens/tokens.module.ts`

```typescript
import { TokensPriceDeltaService } from './tokens-price-delta.service';

@Module({
  providers: [
    TokensService,
    TokensPriceDeltaService, // Add new service
    // ... other providers
  ],
  exports: [
    TokensService,
    TokensPriceDeltaService, // Export for use in other modules
  ],
})
export class TokensModule {}
```

---

### Phase 3: Deprecate Scheduler Job (Week 4-6)

#### 3.1 Monitoring Period (2 weeks)

**Metrics to track:**
```typescript
// Add to tokens-price-delta.service.ts
private metrics = {
  cache_hits: 0,
  cache_misses: 0,
  calculations: 0,
  avg_calc_time_ms: 0,
};

// Log every hour
setInterval(() => {
  const hitRate = (this.metrics.cache_hits /
                  (this.metrics.cache_hits + this.metrics.cache_misses)) * 100;

  console.log('[PriceDelta Metrics]', {
    cache_hit_rate: `${hitRate.toFixed(1)}%`,
    calculations: this.metrics.calculations,
    avg_time_ms: this.metrics.avg_calc_time_ms,
  });
}, 3600000); // Every hour
```

**Monitor in production:**
- Cache hit rate (target: >95%)
- Calculation latency (target: <50ms p99)
- DB query load (should decrease)
- WebSocket client subscriptions (.trade vs .analytics)

#### 3.2 Client Migration

**Update documentation for clients:**

```markdown
## Deprecated (Old Way)
Subscribe to symbol-only channels for batch updates:
- Updates: Every 1 minute
- Latency: 0-60 seconds stale
- Bandwidth: All event types mixed

ws.subscribe(['BTC', 'ETH'])

## Recommended (New Way)
Subscribe to specific event channels:
- Updates: Real-time (<100ms)
- Latency: Fresh on every trade
- Bandwidth: Only events you need

ws.subscribe(['BTC.trade', 'ETH.trade'])  // Real-time prices
ws.subscribe(['BTC.analytics'])            // Heavy metrics (optional)
```

#### 3.3 Disable Scheduler Job

**After 2-4 weeks of monitoring:**

**File:** `/Volumes/Dev/Codev/Toniq/sukko-api/functions/src/main.ts`

```typescript
// Lines 202-214 - Comment out or remove
/*
const _syncTokenPriceDelta = Object.defineProperty(
  function () {
    return tokensService.syncTokenUpdates(['price_delta']);
  },
  'name',
  { value: 'syncTokenPriceDelta' },
) as TokensService['syncTokenUpdates'];

const syncTokenPriceDelta = wrapper(
  _syncTokenPriceDelta.bind(tokensService),
  1,
);
*/

// Update Promise.all to remove syncTokenPriceDelta
const [
  syncTokenUpdatesResult,
  // syncTokenPriceDeltaResult,  // REMOVED
  syncCurrencyResult,
  // ... other jobs
] = await Promise.all([
  syncTokenUpdates([...]),
  // syncTokenPriceDelta(),  // REMOVED
  syncTable(['btc']),
  // ... other jobs
]);
```

#### 3.4 Database Cleanup (Optional)

**Keep scheduler job for non-price analytics:**

```typescript
// Rename job to reflect new purpose
const syncHeavyAnalytics = wrapper(
  tokensService.syncTokenUpdates([
    'holder_dev',
    'holder_count',
    'holder_top',
    'swap_volume_24',
    'volume_24',
    'power_holder_count',
    'bonded_time',
    // 'price_delta' removed - now real-time
  ]),
  10, // Every 10 minutes
);
```

---

## Resource Impact Analysis

### Database Load

**Current (Scheduler-Only):**
```
syncTokenPriceDelta:
  - 200 tokens × 4 queries/token = 800 queries/minute
  - = 13.3 queries/second
  - Runs every 60 seconds
```

**Phase 2 (Event-Driven with Cache):**
```
Trade events:
  - 12 trades/second × 4 queries/trade = 48 queries/sec (worst case)
  - With 95% cache hit rate: 12 × 0.05 × 4 = 2.4 queries/sec
  - Cache priming: ~1 query/sec

Total: ~3.4 queries/second (vs 13.3 currently)
```

**Net Impact:**
- **DB Load:** -74% reduction (13.3 → 3.4 queries/sec)
- **Latency:** -98% reduction (30s avg → 0.5s avg)
- **Memory:** +5MB for price cache (200 tokens × 24h window)

### Message Rate Impact

**Current:**
```
sukko.token.*.analytics: 200 messages/minute (batch updates)
```

**Phase 2:**
```
sukko.token.*.trade: 720 messages/minute (real-time)
sukko.token.*.analytics: 200 messages/minute (non-price metrics)

Total: 920 messages/minute (+360%)
```

**But with subscription filtering:**
```
Without filtering:
  - 10K clients × 920 msg/min = 9.2M writes/min

With filtering (avg 2 tokens per client):
  - 10K clients × 2 tokens × 720/200 = 72K writes/min
  - 95% reduction vs broadcast-all
```

---

## Testing Strategy

### Unit Tests

**File:** `/Volumes/Dev/Codev/Toniq/sukko-api/functions/src/tokens/tokens-price-delta.service.spec.ts`

```typescript
describe('TokensPriceDeltaService', () => {
  it('should calculate positive delta', async () => {
    const result = service['calculateDelta'](100, 110);
    expect(result).toBe(10); // +10%
  });

  it('should calculate negative delta', async () => {
    const result = service['calculateDelta'](100, 90);
    expect(result).toBe(-10); // -10%
  });

  it('should use cache for recent prices', async () => {
    service.updateCache('token1', 100, new Date());
    const price = await service['getPriceAtTime']('token1', new Date());
    expect(price).toBe(100);
  });

  it('should maintain 24h rolling window', async () => {
    const now = new Date();
    const yesterday = new Date(now.getTime() - 25 * 60 * 60 * 1000);

    service.updateCache('token1', 100, yesterday);
    service.updateCache('token1', 110, now);

    const stats = service.getCacheStats();
    expect(stats.tokens[0].entries).toBe(1); // Old entry removed
  });
});
```

### Integration Tests

```typescript
describe('Trade with Price Deltas', () => {
  it('should publish trade event with fresh deltas', async () => {
    // Setup: Create historical trades
    await createTrade({ price: 100, timestamp: now - 1hour });
    await createTrade({ price: 110, timestamp: now - 5min });

    // Execute: New trade
    const result = await executeTrade({ price: 120 });

    // Assert: NATS message published
    expect(natsPublish).toHaveBeenCalledWith(
      'sukko.token.BTC.trade',
      expect.objectContaining({
        price: 120,
        price_delta_5m: 9.09,  // (120-110)/110 = +9.09%
        price_delta_1h: 20,    // (120-100)/100 = +20%
      })
    );
  });
});
```

### Load Testing

```bash
# Simulate high trade volume
# Target: 100 trades/sec with <50ms p99 latency

artillery quick --count 100 --num 1000 \
  --target http://localhost:3000/v1/tokens/trade
```

---

## Rollback Plan

If Phase 2 causes issues:

1. **Immediate:** Re-enable scheduler job
   ```typescript
   // Uncomment in main.ts
   const syncTokenPriceDelta = wrapper(...);
   ```

2. **Short-term:** Revert NATS publishing in trade handler
   ```typescript
   // Comment out in tokens.service.ts
   // await this.tokensService.publishTradeEvent(...);
   ```

3. **Data integrity:** Scheduler continues updating DB, no data loss

---

## Success Criteria

### Phase 2 Success Metrics
- ✅ Price deltas calculated in <50ms (p99)
- ✅ Cache hit rate >95%
- ✅ DB load reduced by >50%
- ✅ Zero data integrity issues
- ✅ WebSocket clients receive updates in <100ms

### Phase 3 Success Metrics
- ✅ Scheduler job disabled for 4+ weeks
- ✅ Zero client complaints about stale data
- ✅ >80% of clients migrated to .trade channels
- ✅ DB query load stable and reduced

---

## Timeline

| Phase | Duration | Key Deliverables |
|-------|----------|------------------|
| **Phase 1** | Week 1 | 8-channel taxonomy in WebSocket server |
| **Phase 2** | Week 2-3 | Event-driven price deltas in API |
| **Phase 3** | Week 4-6 | Deprecate scheduler, monitor, optimize |

**Total:** 6 weeks for complete migration

---

## Related Documents

- [Hierarchical Channel Taxonomy](./HIERARCHICAL_CHANNELS.md) - WebSocket server changes
- [Token Update Events](../../sukko/docs/token-update-events.md) - Event catalog
- [Subscription Filtering Tests](../testing/SUBSCRIPTION_FILTERING_TESTS.md) - Test guide

---

## Questions & Decisions

### Open Questions
1. Should we keep scheduler as backup for non-trading hours?
2. What's the acceptable memory footprint for price cache?
3. Should cache be shared across API instances (Redis)?

### Decisions Made
- ✅ Use in-memory cache (not Redis) for Phase 2
- ✅ Keep scheduler for heavy analytics (holder counts, etc.)
- ✅ Backwards compatibility: legacy subscriptions get all events
- ✅ Event format: Hierarchical `sukko.token.{SYMBOL}.{EVENT_TYPE}`

---

**Document maintained by:** WebSocket Team
**Last updated:** 2025-10-12
**Next review:** After Phase 1 completion
