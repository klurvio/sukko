# Plan: Remove Hardcoded Topic Categories

**Goal:** Remove hardcoded topic categories from code. Categories are derived from topics, which are provisioned via database. All code should query the `TenantRegistry` instead of using hardcoded constants.

**Status:** In Progress
**Date:** 2026-02-02 (Revised)

---

## Key Insight

**Categories are derived from topics.** The database stores topics (e.g., `prod.odin.trade`), and categories are extracted from the topic name's last segment (e.g., `trade`). The existing `TopicToEventType()` function already does this extraction.

**Two systems currently exist:**
1. **Data-driven** (correct): `TenantRegistry` → used by `MultiTenantConsumerPool`, `Producer`
2. **Hardcoded** (to remove): `AllTopicBases()`, `AllTopics()` → used by legacy `KafkaConsumerPool`, bundles, tests

---

## Current State

### What Exists in Database
```sql
-- tenant_topics table (001_initial.sql)
CREATE TABLE tenant_topics (
    topic_name TEXT UNIQUE,    -- e.g., "prod.odin.trade"
    category TEXT,             -- e.g., "trade" (derived from topic_name)
    tenant_id TEXT,
    ...
);
```

### What Exists in Code (TenantRegistry)
```go
// ws/internal/provisioning/topic_registry.go
type TopicRegistry struct { db *sql.DB }

func (r *TopicRegistry) GetSharedTenantTopics(ctx, namespace) ([]string, error)
func (r *TopicRegistry) GetDedicatedTenants(ctx, namespace) ([]TenantTopics, error)
```

### What's Hardcoded (to remove)
```go
// ws/internal/shared/kafka/config.go
const TopicBaseTrade = "trade"  // etc.
var allTopicBases = []string{...}
func AllTopicBases() []string
func AllTopics(env string) []string

// ws/internal/shared/kafka/bundles.go
var bundleBasesMap = map[BundleType][]string{...}
```

### Problem with Current Migration
```sql
-- 003_seed_odin_tenant.sql - HARDCODES 'prod' namespace
INSERT INTO tenant_topics (tenant_id, topic_name, category, ...)
VALUES ('odin', 'prod.odin.trade', 'trade', ...);  -- ❌ Should be environment-aware
```

---

## Solution Overview

| Current | Target |
|---------|--------|
| Hardcoded `TopicBase*` constants | Query from `TenantRegistry` |
| Hardcoded `AllTopicBases()` | New `TenantRegistry.GetCategories()` method |
| Hardcoded `AllTopics()` | Existing `TenantRegistry.GetSharedTenantTopics()` |
| `prod` namespace in migration | Environment-based seeding |
| Bundles hardcoded in code | Remove (or future: store in DB) |

---

## Phase 1: Fix Migration to be Environment-Aware

**Problem:** Current migration hardcodes `prod` namespace.

**File:** `ws/internal/provisioning/repository/migrations/003_seed_odin_tenant.sql`

**Option A: Use environment variable in migration** (Complex - requires migration tooling support)

**Option B: Seed without namespace, build topics dynamically** (Recommended)

Change migration to store category-only, then build full topic names at runtime:
```sql
-- Store categories, not full topic names
INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES ('odin', 'trade', 3, 604800000, NOW());
```

**Option C: Seed all namespaces** (Simple but verbose)
```sql
-- Seed for each known namespace
INSERT INTO tenant_topics VALUES ('odin', 'local.odin.trade', 'trade', ...);
INSERT INTO tenant_topics VALUES ('odin', 'dev.odin.trade', 'trade', ...);
INSERT INTO tenant_topics VALUES ('odin', 'stag.odin.trade', 'trade', ...);
INSERT INTO tenant_topics VALUES ('odin', 'prod.odin.trade', 'trade', ...);
```

**Recommendation:** Option C is simplest and explicit. Each environment has its own DB, so only relevant rows are used.

**Status:** ⏳ PENDING

---

## Phase 2: Add GetCategories to TenantRegistry

**File:** `ws/internal/provisioning/topic_registry.go`

**Add new method:**
```go
// GetCategories returns distinct categories for a tenant in the given namespace.
// Categories are derived from the topic names (last segment).
func (r *TopicRegistry) GetCategories(ctx context.Context, tenantID, namespace string) ([]string, error) {
    query := `
        SELECT DISTINCT category
        FROM tenant_topics
        WHERE tenant_id = $1
          AND topic_name LIKE $2
          AND deleted_at IS NULL
        ORDER BY category
    `
    // ... implementation
}
```

This allows consumers to get available categories without hardcoding.

**Status:** ⏳ PENDING

---

## Phase 3: Update kafka.TenantRegistry Interface

**File:** `ws/internal/shared/kafka/registry.go` (or where interface is defined)

**Add to interface:**
```go
type TenantRegistry interface {
    GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error)
    GetDedicatedTenants(ctx context.Context, namespace string) ([]TenantTopics, error)
    GetCategories(ctx context.Context, tenantID, namespace string) ([]string, error)  // NEW
}
```

**Status:** ⏳ PENDING

---

## Phase 4: Improve Topic Caching

**Current state:**
- Producer cache: 60s TTL (`ws/internal/shared/kafka/producer.go`)
- Consumer pool: 60s refresh interval (`ws/internal/server/orchestration/multitenant_pool.go`)
- **Bug:** Consumer `AddConsumeTopics()` is not actually called (placeholder comment at line 333)

**Changes required:**

### 4a. Reduce TTL from 60s to 30s

**Files:**
- `producer.go`: Change `topicCacheTTL` default to 30s
- `multitenant_pool.go`: Change `refreshInterval` default to 30s

### 4b. Fix incomplete consumer topic updates

**File:** `ws/internal/server/orchestration/multitenant_pool.go`

Replace placeholder comment with actual implementation:
```go
// Current (line ~333):
// Note: franz-go AddConsumeTopics() would be called here

// Should be:
if err := p.sharedConsumer.AddConsumeTopics(newTopics...); err != nil {
    p.logger.Error("failed to add topics to consumer", "error", err)
}
```

### 4c. (Optional) Extract shared TopicCache

If caching patterns diverge significantly, consider extracting to shared module:

**New file:** `ws/internal/shared/kafka/topic_cache.go`
```go
type TopicCache struct {
    registry TenantRegistry
    ttl      time.Duration
    mu       sync.RWMutex
    topics   map[string][]string  // namespace -> topics
    expiry   map[string]time.Time
}
```

This is optional - may not be needed if producer and consumer caching stays simple.

**Status:** ⏳ PENDING

---

## Phase 5: Remove Hardcoded Constants

**File:** `ws/internal/shared/kafka/config.go`

**Remove:**
| Item | Replacement |
|------|-------------|
| `TopicBaseTrade`, etc. | Query `TenantRegistry.GetCategories()` |
| `allTopicBases` variable | Query `TenantRegistry.GetCategories()` |
| `AllTopicBases()` function | `TenantRegistry.GetCategories()` |
| `AllTopics(env)` function | `TenantRegistry.GetSharedTenantTopics()` |
| `EventTypeToTopicBase()` | Keep - maps event types to category names (still needed) |

**Keep:**
| Item | Reason |
|------|--------|
| `NormalizeEnv()` | Namespace normalization |
| `GetTopic(env, base)` | Generic topic builder |
| `TopicToEventType()` | Extracts category from topic name |
| `EventType` constants | Event type definitions |
| `EventTypeToTopicBase()` | Maps events to categories (logic, not data) |

**Status:** ⏳ PENDING

---

## Phase 6: Remove Bundles

**Files to delete:**
- `ws/internal/shared/kafka/bundles.go`
- `ws/internal/shared/kafka/bundles_test.go`

**Reason:** Bundles are convenience groupings. If needed in future:
- Store as tenant metadata in DB
- Or configure via Helm values

**Status:** ⏳ PENDING

---

## Phase 7: Update Legacy KafkaConsumerPool

**File:** `ws/internal/server/orchestration/kafka_pool.go`

**Current (fallback to hardcoded):**
```go
topics := config.Topics
if len(topics) == 0 {
    topics = kafka.AllTopics(config.Environment)  // Hardcoded fallback
}
```

**Change to (require explicit or query registry):**
```go
topics := config.Topics
if len(topics) == 0 {
    if config.TenantRegistry != nil {
        var err error
        topics, err = config.TenantRegistry.GetSharedTenantTopics(ctx, config.Namespace)
        if err != nil {
            return nil, fmt.Errorf("failed to get topics from registry: %w", err)
        }
    } else {
        return nil, fmt.Errorf("KafkaPoolConfig.Topics is required: no topics configured and no registry available")
    }
}
```

**Status:** ⏳ PENDING

---

## Phase 8: Update Tests

**Files to update:**
| File | Action |
|------|--------|
| `config_test.go` | Remove tests for removed functions; add tests for new registry methods |
| `bundles_test.go` | DELETE (dead code) |
| `consumer_test.go` | Update to use string literals (e.g., `"trade"`) instead of removed constants |
| `kafka_pool_test.go` | Test registry-based topic resolution |
| `topic_cache_test.go` | NEW - test caching behavior |

**Test approach:** String literals are OK if they make tests effective and readable. Use `"trade"`, `"liquidity"` directly rather than over-engineering with mock registries for simple cases.

**Status:** ⏳ PENDING

---

## Phase 9: Update Documentation

**File:** `ws/docs/asyncapi/asyncapi.yaml`

**Add clarification:**
```yaml
### Categories
Categories are dynamically provisioned per-tenant via the provisioning API.
Topics are queried from the database via TenantRegistry.
The channels documented below represent the default Odin tenant categories.
```

**Status:** ⏳ PENDING

---

## Files Summary

| File | Action |
|------|--------|
| `migrations/003_seed_odin_tenant.sql` | Add all namespace variants (local/dev/stag/prod) |
| `provisioning/topic_registry.go` | Add `GetCategories()` method |
| `shared/kafka/registry.go` | Update interface with `GetCategories()` |
| `shared/kafka/producer.go` | Reduce TTL from 60s to 30s |
| `shared/kafka/config.go` | Remove hardcoded constants and functions |
| `shared/kafka/bundles.go` | DELETE (dead code - zero callers) |
| `shared/kafka/bundles_test.go` | DELETE |
| `server/orchestration/kafka_pool.go` | Use registry instead of hardcoded fallback |
| `server/orchestration/multitenant_pool.go` | Reduce refresh to 30s, fix `AddConsumeTopics()` |
| Tests | Update to use string literals where constants removed |

---

## Deployment Order

1. **Phase 1** - Update migration with all namespaces
2. **Phase 2-4** - Add registry methods and caching (additive, no breaking changes)
3. **Phase 5-7** - Remove hardcoded code (requires phases 2-4)
4. **Phase 8-9** - Tests and documentation

---

## Verification

```bash
# Run tests
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go test ./internal/shared/kafka/...
go test ./internal/server/orchestration/...
go test ./internal/provisioning/...

# Verify no hardcoded topic bases remain
grep -rn "TopicBaseTrade\|TopicBaseLiquidity\|AllTopicBases\|allTopicBases" \
  --include="*.go" ws/internal/ | grep -v "_test.go"

# Should return nothing (all removed)
```

---

## Resolved Questions (Investigation 2026-02-02)

### 1. EventTypeToTopicBase() - **KEEP AS CODE (Static)**

**Finding:** Static mapping, same for all tenants. This is domain logic, not configuration.

**Additional discovery:** Function is **unused in production code**:
- Only used in tests and standalone `publisher-go` tool
- `publisher-go` has its own duplicate mapping
- No actual production code calls this function

**Decision:** Keep as code. Can optionally remove from `config.go` since unused, or move to `ws/internal/shared/events/type_mapping.go` if we want to preserve it for future use.

---

### 2. Bundles - **REMOVE ENTIRELY (Dead Code)**

**Finding:** Zero production callers. Completely dead code (589 lines).

**Evidence:**
- `GetBundleBases()`, `GetBundleTopics()`, `ValidBundle()`, `AllBundles()` - NO CALLERS
- Only `bundles_test.go` (471 lines) tests these unused functions
- Superseded by hierarchical channel subscriptions (`BTC.trade`, `ETH.analytics`)
- WebSocket protocol has no bundle support - clients subscribe directly to channels

**Decision:** Delete `bundles.go` and `bundles_test.go`. If convenience groupings needed in future, store in DB or Helm.

---

### 3. Cache Invalidation - **REDUCE TTL TO 30s + FIX CONSUMER**

**Finding:** Current 60s TTL is marginally acceptable but has issues.

**Critical discovery:** Consumer topic updates are **INCOMPLETE**:
```go
// multitenant_pool.go line 333 - PLACEHOLDER COMMENT, NOT IMPLEMENTED
// Note: franz-go AddConsumeTopics() would be called here
// For now, we track the topics; full implementation requires consumer recreation
```

**Recommendation (phased):**

| Phase | Action | Effort |
|-------|--------|--------|
| 1 | Reduce TTL from 60s to 30s | Low |
| 2 | Fix incomplete `AddConsumeTopics()` implementation | Medium |
| 3 | Add explicit invalidation if needed | Optional |

**What happens during TTL window:**
- Messages accumulate in Kafka (not lost - retained per `retention.ms`)
- Producers refresh cache on-demand when topic not found
- Consumers wait for next refresh tick (up to 30s with reduced TTL)

**Decision:** Reduce TTL to 30s as immediate fix. Complete consumer implementation as separate task.

---

## Additional Findings

### Test String Literals - **OK TO USE**

Per user feedback: String literals in tests are acceptable if they make tests effective. No need for complex mock registries if direct strings work.

### Migration Namespace Issue

Current migration hardcodes `prod` namespace. Options:
- **Option C (recommended):** Seed all namespaces (local/dev/stag/prod) - each environment DB uses relevant rows
- Alternative: Store categories without namespace, build topic names at runtime

---

## Rollback Plan

1. Revert code changes (git)
2. Migration is additive - no schema rollback needed
3. Hardcoded constants can be restored from git history
