# Plan: Remove Hardcoded Topic Categories

**Goal:** Remove hardcoded topic categories from code. Categories are stored in database without namespace prefix. Topic names are built at runtime using `KAFKA_TOPIC_NAMESPACE` config. All code queries `TenantRegistry` instead of using hardcoded constants.

**Status:** ✅ Implemented
**Date:** 2026-02-04 (Revised - Option B selected)
**Implemented:** 2026-02-04
**Commit:** `6204e4b` - refactor[86aeyd4d5]: remove hardcoded topic categories

---

## Key Insight

**Namespace is runtime configuration, not stored data.**

Current architecture builds topic names at runtime in multiple places:
- **Producer** (`producer.go:321`): `fmt.Sprintf("%s.%s.%s", p.topicNamespace, tenant, category)`
- **Provisioning** (`service.go:495`): `fmt.Sprintf("%s.%s.%s", s.config.TopicNamespace, tenantID, category)`
- **TenantRegistry** (after plan): Will also build topic names

The database should store **categories only** (e.g., `trade`), not full topic names with namespace (e.g., `prod.odin.trade`). This makes `KAFKA_TOPIC_NAMESPACE` the single source of truth.

**To avoid code duplication**, create a shared `BuildTopicName()` function that all three use.

**Two systems currently exist:**
1. **Data-driven** (correct): `TenantRegistry` → used by `MultiTenantConsumerPool`, `Producer`
2. **Hardcoded** (to remove): `AllTopicBases()`, `AllTopics()`, `GetTopic()` → used by legacy code, bundles, tests

---

## Current State (Problem)

### Database stores full topic names with namespace
```sql
-- tenant_topics table stores namespace-prefixed names
-- This is WRONG - namespace should be runtime config
INSERT INTO tenant_topics (topic_name, category, ...)
VALUES ('prod.odin.trade', 'trade', ...);  -- ❌ Hardcodes 'prod'
```

### TenantRegistry filters by namespace prefix
```go
// topic_registry.go - filters stored topics by namespace
query := `SELECT topic_name FROM tenant_topics WHERE topic_name LIKE $1`
namespacePrefix := namespace + ".%"  // e.g., "dev.%"
```

**Problem:** If migration seeds `prod.odin.trade` but runtime uses `KAFKA_TOPIC_NAMESPACE=dev`:
- Query filters for `dev.%`
- DB only has `prod.odin.trade`
- **Result: No topics found!**

---

## Solution: Option B - Store Categories, Build Topics at Runtime

### Target Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│ KAFKA_TOPIC_NAMESPACE env var (single source of truth)          │
│ e.g., "local", "dev", "stag", "prod"                            │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
         Producer      TenantRegistry    Provisioning
              │               │               │
   builds topic:      builds topic:     builds topic:
   {ns}.{tenant}.     {ns}.{tenant}.    {ns}.{tenant}.
    {category}         {category}        {category}
              │               │               │
              └───────────────┴───────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ tenant_categories│
                    │ (no namespace)   │
                    │                  │
                    │ tenant: odin     │
                    │ category: trade  │
                    └─────────────────┘
```

| Current | Target |
|---------|--------|
| `tenant_topics.topic_name = 'prod.odin.trade'` | `tenant_categories.category = 'trade'` |
| `WHERE topic_name LIKE 'prod.%'` | `SELECT category` + build in code |
| Namespace in data | Namespace from `KAFKA_TOPIC_NAMESPACE` config |
| Hardcoded `TopicBase*` constants | Query `TenantRegistry` |
| Bundles in code | Remove (dead code) |

---

## Phase 1: Modify Migration to Use tenant_categories Table

**File:** `ws/internal/provisioning/repository/migrations/003_seed_odin_tenant.sql`

**Note:** Nothing is deployed yet, so we modify the existing migration directly instead of creating a new one.

**Changes:**

1. **Add `tenant_categories` table** (stores categories without namespace)
2. **Remove `tenant_topics` seeding** (no longer store full topic names)
3. **Seed categories only** for odin tenant

```sql
-- Add to 003_seed_odin_tenant.sql (replace tenant_topics seeding)

-- ====================
-- TENANT CATEGORIES TABLE
-- ====================

CREATE TABLE IF NOT EXISTS tenant_categories (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    partitions INT NOT NULL DEFAULT 3,
    retention_ms BIGINT NOT NULL DEFAULT 604800000,  -- 7 days
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    UNIQUE(tenant_id, category)
);

CREATE INDEX IF NOT EXISTS idx_tenant_categories_tenant
    ON tenant_categories(tenant_id) WHERE deleted_at IS NULL;

-- ====================
-- ODIN TENANT CATEGORIES (no namespace - built at runtime)
-- ====================

INSERT INTO tenant_categories (tenant_id, category, partitions, retention_ms, created_at)
VALUES
    ('odin', 'trade', 3, 604800000, NOW()),
    ('odin', 'liquidity', 3, 604800000, NOW()),
    ('odin', 'metadata', 3, 604800000, NOW()),
    ('odin', 'social', 3, 604800000, NOW()),
    ('odin', 'community', 3, 604800000, NOW()),
    ('odin', 'creation', 3, 604800000, NOW()),
    ('odin', 'analytics', 3, 604800000, NOW()),
    ('odin', 'balances', 3, 604800000, NOW())
ON CONFLICT (tenant_id, category) DO NOTHING;

-- Remove the old tenant_topics seeding (full topic names with namespace)
-- Topic names are now built at runtime: {KAFKA_TOPIC_NAMESPACE}.{tenant}.{category}
```

**Status:** ⏳ PENDING

---

## Phase 2: Add Shared BuildTopicName Function

**Problem:** Three places build topic names with same format - avoid duplication.

| File | Current Code |
|------|--------------|
| `producer.go:321` | `fmt.Sprintf("%s.%s.%s", p.topicNamespace, tenant, category)` |
| `service.go:495` | `fmt.Sprintf("%s.%s.%s", s.config.TopicNamespace, tenantID, category)` |
| `topic_registry.go` (new) | `fmt.Sprintf("%s.%s.%s", namespace, tenantID, category)` |

**Solution:** Create shared function as single source of truth.

**New file:** `ws/internal/shared/kafka/topic.go`

```go
package kafka

import "fmt"

// BuildTopicName constructs a Kafka topic name from components.
// This is the single source of truth for topic name format.
//
// Format: {namespace}.{tenantID}.{category}
// Example: BuildTopicName("prod", "odin", "trade") -> "prod.odin.trade"
//
// Components:
//   - namespace: From KAFKA_TOPIC_NAMESPACE env var (e.g., "local", "dev", "prod")
//   - tenantID: Tenant identifier (e.g., "odin", "acme")
//   - category: Topic category (e.g., "trade", "liquidity")
func BuildTopicName(namespace, tenantID, category string) string {
    return fmt.Sprintf("%s.%s.%s", namespace, tenantID, category)
}
```

**Update callers:**
- `producer.go`: `topic := kafka.BuildTopicName(p.topicNamespace, tenant, category)`
- `service.go`: `return kafka.BuildTopicName(s.config.TopicNamespace, tenantID, category)`
- `topic_registry.go`: `topic := kafka.BuildTopicName(namespace, tenantID, category)`

**Also remove:** `GetTopic()` function in `config.go` (wrong format: `odin.{env}.{base}`, unused in production)

**Status:** ⏳ PENDING

---

## Phase 3: Update TenantRegistry to Build Topic Names

**File:** `ws/internal/provisioning/topic_registry.go`

**Change `GetSharedTenantTopics()` to query categories and build topic names:**

```go
// GetSharedTenantTopics returns all topics for active shared-mode tenants.
// Topics are built at runtime: {namespace}.{tenant_id}.{category}
func (r *TopicRegistry) GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error) {
    query := `
        SELECT t.id, c.category
        FROM tenants t
        JOIN tenant_categories c ON c.tenant_id = t.id
        WHERE t.status = 'active'
          AND t.consumer_type = 'shared'
          AND c.deleted_at IS NULL
        ORDER BY t.id, c.category
    `

    rows, err := r.db.QueryContext(ctx, query)
    if err != nil {
        return nil, fmt.Errorf("query shared tenant categories: %w", err)
    }
    defer rows.Close()

    var topics []string
    for rows.Next() {
        var tenantID, category string
        if err := rows.Scan(&tenantID, &category); err != nil {
            return nil, fmt.Errorf("scan tenant category: %w", err)
        }
        // Build topic name using shared function
        topic := kafka.BuildTopicName(namespace, tenantID, category)
        topics = append(topics, topic)
    }

    return topics, nil
}
```

**Similarly update `GetDedicatedTenants()`** to use `tenant_categories` and `kafka.BuildTopicName()`.

**Status:** ⏳ PENDING

---

## Phase 4: Update Provisioning Service

**File:** `ws/internal/provisioning/service.go`

Update topic creation to use `tenant_categories` and shared `BuildTopicName`:

```go
// CreateTopics creates categories for a tenant
func (s *Service) CreateTopics(ctx context.Context, tenantID string, categories []string) error {
    // Insert into tenant_categories (not tenant_topics)
    for _, category := range categories {
        _, err := s.db.ExecContext(ctx, `
            INSERT INTO tenant_categories (tenant_id, category)
            VALUES ($1, $2)
            ON CONFLICT (tenant_id, category) DO NOTHING
        `, tenantID, category)
        if err != nil {
            return fmt.Errorf("insert category %s: %w", category, err)
        }

        // Create actual Kafka topic using shared function
        topicName := kafka.BuildTopicName(s.config.TopicNamespace, tenantID, category)
        if err := s.kafkaAdmin.CreateTopic(ctx, topicName, partitions, replication); err != nil {
            return fmt.Errorf("create kafka topic %s: %w", topicName, err)
        }
    }
    return nil
}

// Remove buildTopicName() method - use kafka.BuildTopicName() instead
```

**Status:** ⏳ PENDING

---

## Phase 5: Improve Topic Caching

**Current state:**
- Producer cache: 60s TTL
- Consumer pool: 60s refresh interval
- **Bug:** Consumer `AddConsumeTopics()` is placeholder comment, not implemented

**Changes required:**

### 5a. Reduce TTL from 60s to 30s

**Files:**
- `producer.go`: Change `topicCacheTTL` default to 30s
- `multitenant_pool.go`: Change `refreshInterval` default to 30s

### 5b. Add AddConsumeTopics method to Consumer wrapper

**Problem:** `sharedConsumer` is type `*kafka.Consumer` (our wrapper), but the wrapper doesn't expose franz-go's `AddConsumeTopics()`. The `client` field is private.

**File:** `ws/internal/shared/kafka/consumer.go`

**Add new method:**
```go
// AddConsumeTopics dynamically adds topics to the consumer without triggering
// a consumer group rebalance. This is critical for multi-tenant systems where
// topics are provisioned dynamically.
//
// Background: Traditional Kafka Topic Addition
//
// In standard Kafka clients, changing the subscription (adding/removing topics)
// triggers a "rebalance" - a stop-the-world event where:
//   1. All consumers in the group STOP consuming
//   2. All partition assignments are released
//   3. Kafka coordinator recalculates assignments
//   4. New assignments distributed to all consumers
//   5. Consumers resume from last committed offset
//
// This typically takes 5-30 seconds, during which NO messages are processed.
//
// franz-go's Incremental Approach (KIP-429, Kafka 2.4+)
//
// AddConsumeTopics() uses cooperative rebalancing to add topics incrementally:
//   - Only fetches metadata for the new topic
//   - Existing partition assignments are preserved
//   - No disruption to ongoing consumption
//   - Sub-second topic addition
//
// Trade-offs:
//   - Pro: No consumption pause when adding topics
//   - Pro: Better for dynamic multi-tenant systems
//   - Con: franz-go specific (not portable to other Kafka clients)
//   - Con: If migrating clients, would need full resubscription approach
//
// This is the right choice for odin-ws because:
//   - Topics are dynamically provisioned per-tenant
//   - WS-server needs to discover new topics without downtime
//   - We're committed to franz-go as our Kafka client
func (c *Consumer) AddConsumeTopics(topics ...string) {
    c.client.AddConsumeTopics(topics...)
}
```

### 5c. Use AddConsumeTopics in MultiTenantPool

**File:** `ws/internal/server/orchestration/multitenant_pool.go`

**Replace placeholder (line ~333):**
```go
// Current (placeholder):
// Note: franz-go AddConsumeTopics() would be called here

// Replace with:
if len(toAdd) > 0 {
    p.sharedConsumer.AddConsumeTopics(toAdd...)
    for _, topic := range toAdd {
        p.sharedTopics[topic] = true
    }
    p.logger.Info().Strs("topics", toAdd).Msg("Added new topics to shared consumer")
}
```

**Status:** ⏳ PENDING

---

## Phase 6: Remove Hardcoded Constants

**File:** `ws/internal/shared/kafka/config.go`

**Remove:**
| Item | Reason |
|------|--------|
| `TopicBaseTrade`, `TopicBaseLiquidity`, etc. | Hardcoded - query DB instead |
| `allTopicBases` variable | Hardcoded list |
| `AllTopicBases()` function | No callers after bundles removed |
| `AllTopics(env)` function | Use `TenantRegistry.GetSharedTenantTopics()` |
| `EventTypeToTopicBase()` | Unused in production; `publisher-go` has own copy |
| `GetTopic(env, base)` | Wrong format (`odin.{env}.{base}`), unused in production |

**Keep:**
| Item | Reason |
|------|--------|
| `NormalizeEnv()` | Namespace normalization |
| `TopicToEventType()` | Extracts category from topic name |
| `EventType` constants | Event type definitions |

**Also remove from `config_test.go`:**
- `TestEventTypeToTopicBase()`
- `TestEventTypeToTopicBase_Unknown()`
- `TestGetTopic()`
- `BenchmarkGetTopic()`

**Status:** ⏳ PENDING

---

## Phase 7: Remove Bundles (Dead Code)

**Files to delete:**
- `ws/internal/shared/kafka/bundles.go` (118 lines)
- `ws/internal/shared/kafka/bundles_test.go` (471 lines)

**Finding:** Zero production callers. Completely dead code.
- Superseded by hierarchical channel subscriptions (`BTC.trade`, `ETH.analytics`)
- WebSocket protocol has no bundle support

**Status:** ⏳ PENDING

---

## Phase 8: Update Legacy KafkaConsumerPool

**File:** `ws/internal/server/orchestration/kafka_pool.go`

**Current (fallback to hardcoded):**
```go
topics := config.Topics
if len(topics) == 0 {
    topics = kafka.AllTopics(config.Environment)  // Hardcoded fallback
}
```

**Change to (require registry):**
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

## Phase 9: Update Tests

| File | Action |
|------|--------|
| `config_test.go` | Remove tests for removed functions (`GetTopic`, `EventTypeToTopicBase`, etc.) |
| `bundles_test.go` | DELETE |
| `consumer_test.go` | Use string literals (`"trade"`) instead of constants |
| `kafka_pool_test.go` | Test registry-based topic resolution |
| `topic_registry_test.go` | Test new category-based queries |
| `topic_test.go` | NEW - test `BuildTopicName()` |

**Status:** ⏳ PENDING

---

## Phase 10: Deprecate tenant_topics Table

After migration is stable:

```sql
-- Future migration: drop tenant_topics if no longer needed
-- Or keep for backward compatibility / audit trail
ALTER TABLE tenant_topics RENAME TO tenant_topics_deprecated;
```

**Status:** ⏳ FUTURE (after validation)

---

## Files Summary

| File | Action |
|------|--------|
| `migrations/003_seed_odin_tenant.sql` | MODIFY - add tenant_categories table, remove tenant_topics seeding |
| `shared/kafka/topic.go` | NEW - shared `BuildTopicName()` function |
| `provisioning/topic_registry.go` | Query tenant_categories, use `BuildTopicName()` |
| `provisioning/service.go` | Use tenant_categories, use `BuildTopicName()` |
| `shared/kafka/consumer.go` | Add `AddConsumeTopics()` method |
| `shared/kafka/producer.go` | Reduce TTL 60s → 30s, use `BuildTopicName()` |
| `shared/kafka/config.go` | Remove hardcoded constants + `GetTopic()` + `EventTypeToTopicBase()` |
| `shared/kafka/config_test.go` | Remove tests for deleted functions |
| `shared/kafka/bundles.go` | DELETE |
| `shared/kafka/bundles_test.go` | DELETE |
| `server/orchestration/kafka_pool.go` | Use registry, remove hardcoded fallback |
| `server/orchestration/multitenant_pool.go` | Reduce TTL, call `AddConsumeTopics()` |

---

## Deployment Order

1. **Phase 1** - Modify migration (add tenant_categories table)
2. **Phase 2** - Add shared `BuildTopicName()` function
3. **Phase 3-4** - Update TenantRegistry and Service to use new table + shared function
4. **Phase 5** - Caching improvements (independent)
5. **Phase 6-8** - Remove hardcoded code (requires phases 1-4)
6. **Phase 9** - Update tests
7. **Phase 10** - Deprecate old table (future)

---

## Pre-Deployment Checklist (Phase 8)

Before deploying Phase 8 changes that remove hardcoded fallback:

- [ ] Verify `TenantRegistry` is wired up in server initialization
- [ ] Verify `KAFKA_TOPIC_NAMESPACE` is set in all environments
- [ ] Verify migration has run successfully
- [ ] Verify `tenant_categories` has expected rows: `SELECT * FROM tenant_categories WHERE tenant_id = 'odin'`
- [ ] Test in staging: `GetSharedTenantTopics()` returns expected topics
- [ ] Verify Helm values have `provisioning.database` configured

---

## Verification

### Unit Tests
```bash
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go test ./internal/shared/kafka/...
go test ./internal/server/orchestration/...
go test ./internal/provisioning/...
```

### Code Verification
```bash
# Verify no hardcoded topic bases remain
grep -rn "TopicBaseTrade\|TopicBaseLiquidity\|AllTopicBases\|allTopicBases" \
  --include="*.go" ws/internal/ | grep -v "_test.go"
# Should return nothing

# Verify tenant_categories has data
psql -c "SELECT tenant_id, category FROM tenant_categories WHERE deleted_at IS NULL"
```

### Integration Test: Dynamic Topic Discovery

Test that new topics are discovered within 30s cache window:

```bash
# 1. Start services with test namespace
export KAFKA_TOPIC_NAMESPACE=test
# Start provisioning service and ws-server

# 2. Verify server is healthy
curl -s http://localhost:8080/health | jq .

# 3. Check initial topics (odin tenant categories)
# Server logs should show: "Subscribed to topics" with test.odin.*

# 4. Create new tenant with custom category via provisioning API
curl -X POST http://localhost:8081/api/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{"id": "acme", "name": "Acme Corp", "consumer_type": "shared"}'

curl -X POST http://localhost:8081/api/v1/tenants/acme/topics \
  -H "Content-Type: application/json" \
  -d '{"categories": ["custom"]}'

# 5. Wait for cache refresh (max 30s with new TTL)
echo "Waiting 35s for cache refresh..."
sleep 35

# 6. Verify consumer discovered new topic
# Check server logs for: "Added new topics to shared consumer" containing "test.acme.custom"

# 7. Verify producer can publish to new topic
# Connect via WebSocket and publish:
# {"type": "publish", "channel": "acme.TOKEN123.custom", "data": {"test": true}}
# Should succeed (not return ErrTopicNotProvisioned)

# 8. Cleanup
curl -X DELETE http://localhost:8081/api/v1/tenants/acme
```

**Success criteria:**
- New topic discovered within 30s (not 60s)
- Consumer logs show `AddConsumeTopics` called
- Producer accepts messages to new topic

---

## Resolved Questions

### 1. GetCategories() Method - **NOT NEEDED**

**Finding:** After bundles are deleted, no production code needs a list of categories.
- All callers of `AllTopicBases()` are in `bundles.go` (being deleted)
- Producer and consumer use full topic names from `GetSharedTenantTopics()`
- Skip adding `GetCategories()` - premature abstraction

### 2. franz-go AddConsumeTopics() - **SUPPORTED (Use It)**

**Finding:** franz-go supports dynamic topic addition via `AddConsumeTopics()`.

**What is a Kafka Rebalance?**
When consumers change their subscription, Kafka triggers a "rebalance":
1. All consumers STOP consuming (5-30 seconds)
2. All partition assignments released
3. Coordinator recalculates assignments
4. Consumers resume

**Why AddConsumeTopics() is Better:**
- Uses cooperative rebalancing (KIP-429, Kafka 2.4+, industry standard)
- Adds topics incrementally without stop-the-world pause
- Existing partition assignments preserved
- Sub-second topic addition

**Trade-offs:**
| Approach | Pros | Cons |
|----------|------|------|
| `AddConsumeTopics()` | No pause, sub-second | franz-go specific |
| Traditional `Subscribe()` | Portable, well-tested | 5-30s pause, all consumers affected |

**Decision:** Use `AddConsumeTopics()` - right tool for dynamic multi-tenant topic discovery.

**Note:** If migrating away from franz-go, this would need to be replaced with full resubscription (accept the rebalance cost or redesign).

### 3. EventTypeToTopicBase() - **REMOVE**

**Finding:** Unused in production. `publisher-go` has its own complete duplicate.

**Evidence:**
- Zero production callers (only `config_test.go` tests it)
- `publisher-go/main.go` lines 74-123 has its own `TopicBase*` constants and `EventTypeToBase` map
- Function depends on `TopicBase*` constants we're removing

**Decision:** Remove entirely along with its tests. No code depends on it.

### 4. Bundles - **REMOVE (Dead Code)**

**Finding:** Zero production callers. 589 lines of dead code.
- Superseded by hierarchical channel subscriptions
- Delete entirely

### 5. GetTopic() - **REMOVE (Wrong Format)**

**Finding:** Uses wrong topic format, unused in production.

**Evidence:**
- `GetTopic("dev", "trade")` → `odin.dev.trade` (wrong: `{tenant}.{ns}.{category}`)
- Correct format is: `dev.odin.trade` (`{ns}.{tenant}.{category}`)
- Only called in tests and `bundles.go` (being deleted)
- Replaced by `BuildTopicName()` with correct format

**Decision:** Remove entirely. Use `kafka.BuildTopicName()` instead.

---

## Rollback Plan

1. Revert code changes (git)
2. Migration is additive - `tenant_categories` can coexist with `tenant_topics`
3. If needed, revert TenantRegistry to query `tenant_topics` again
