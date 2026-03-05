# Plan: Remove Dedicated Consumer Functionality

**Date:** 2026-02-09
**Status:** Ready for Implementation

---

## Goal

Remove dedicated consumer functionality from `MultiTenantConsumerPool`, keeping only shared consumer support. This simplifies the codebase in preparation for a future architecture where each tenant has its own server instance.

---

## Current State

The `MultiTenantConsumerPool` supports two consumer modes:
- **Shared**: All shared-mode tenants use a single consumer group (`sukko-shared-{namespace}`)
- **Dedicated**: Each dedicated-mode tenant gets its own consumer group (`sukko-{tenant_id}-{namespace}`)

This is over-engineered for our current needs - we only use shared mode.

---

## Files to Modify

### 1. `ws/internal/server/orchestration/multitenant_pool.go`

**Remove:**
- `dedicatedConsumers map[string]*kafka.Consumer` field (line 62)
- `dedicatedCount atomic.Uint64` field (line 72)
- `GetDedicatedTenants()` call in `refreshTopics()` (lines 238-245)
- `updateDedicatedConsumers()` call in `refreshTopics()` (lines 258-264)
- `updateDedicatedConsumers()` function entirely (lines 360-431)
- Dedicated consumer start loop in `Start()` (lines 178-187)
- Dedicated consumer stop loop in `Stop()` (lines 477-485)
- `DedicatedCount` from metrics struct and `GetMetrics()` (lines 504, 524)

**Update:**
- Remove dedicated consumer references from comments/docstrings
- Simplify metrics reporting (remove dedicated count parameter)

### 2. `ws/internal/shared/kafka/tenant_registry.go`

**Remove:**
- `GetDedicatedTenants()` method from interface (lines 28-39)
- `TenantTopics` struct (lines 42-51)

### 3. `ws/internal/provisioning/topic_registry.go`

**Remove:**
- `GetDedicatedTenants()` method entirely (lines 71-132)
- `getCategoriesForTenant()` helper function (lines 134-158)

### 4. `ws/internal/shared/metrics/pool_metrics.go` (if exists)

**Update:**
- Remove `dedicatedCount` parameter from `OnRefresh()` callback signature

---

## Detailed Changes

### multitenant_pool.go - Struct Changes

```go
// Before
type MultiTenantConsumerPool struct {
    // ...
    dedicatedConsumers map[string]*kafka.Consumer  // REMOVE
    dedicatedCount     atomic.Uint64               // REMOVE
}

// After
type MultiTenantConsumerPool struct {
    // ... (no dedicated fields)
}
```

### multitenant_pool.go - refreshTopics() Changes

```go
// Before
func (p *MultiTenantConsumerPool) refreshTopics(ctx context.Context) error {
    // Get shared tenant topics
    sharedTopics, err := p.registry.GetSharedTenantTopics(ctx, p.config.Namespace)
    // ...

    // REMOVE: Get dedicated tenants
    dedicatedTenants, err := p.registry.GetDedicatedTenants(ctx, p.config.Namespace)
    // ...

    // Update shared consumer
    if err := p.updateSharedConsumer(ctx, sharedTopics); err != nil { ... }

    // REMOVE: Update dedicated consumers
    if err := p.updateDedicatedConsumers(ctx, dedicatedTenants); err != nil { ... }

    // ...
}

// After
func (p *MultiTenantConsumerPool) refreshTopics(ctx context.Context) error {
    // Get shared tenant topics
    sharedTopics, err := p.registry.GetSharedTenantTopics(ctx, p.config.Namespace)
    if err != nil {
        if p.metrics != nil {
            p.metrics.OnRefresh(false, 0)
        }
        return fmt.Errorf("failed to get shared tenant topics: %w", err)
    }

    p.mu.Lock()
    defer p.mu.Unlock()

    // Update shared consumer
    if err := p.updateSharedConsumer(ctx, sharedTopics); err != nil {
        if p.metrics != nil {
            p.metrics.OnRefresh(false, 0)
        }
        return fmt.Errorf("failed to update shared consumer: %w", err)
    }

    topicsCount := len(p.sharedTopics)
    p.topicsSubscribed.Store(uint64(topicsCount))

    if p.metrics != nil {
        p.metrics.OnRefresh(true, topicsCount)
    }

    return nil
}
```

### multitenant_pool.go - Start() Changes

```go
// Before
func (p *MultiTenantConsumerPool) Start() error {
    // ...

    // Start shared consumer if we have topics
    if p.sharedConsumer != nil {
        if err := p.sharedConsumer.Start(); err != nil { ... }
    }

    // REMOVE: Start all dedicated consumers
    for tenantID, consumer := range p.dedicatedConsumers {
        if err := consumer.Start(); err != nil { ... }
    }

    // ...
}

// After
func (p *MultiTenantConsumerPool) Start() error {
    // ...

    // Start shared consumer if we have topics
    if p.sharedConsumer != nil {
        if err := p.sharedConsumer.Start(); err != nil {
            return fmt.Errorf("failed to start shared consumer: %w", err)
        }
    }

    // Start refresh loop
    p.wg.Add(1)
    go p.refreshLoop()

    p.logger.Info().
        Int("shared_topics", len(p.sharedTopics)).
        Msg("Consumer pool started")

    return nil
}
```

### multitenant_pool.go - Stop() Changes

```go
// Before
func (p *MultiTenantConsumerPool) Stop() error {
    // ...

    // Stop shared consumer
    if p.sharedConsumer != nil {
        if err := p.sharedConsumer.Stop(); err != nil { ... }
    }

    // REMOVE: Stop all dedicated consumers
    for tenantID, consumer := range p.dedicatedConsumers {
        if err := consumer.Stop(); err != nil { ... }
    }

    // ...
}

// After
func (p *MultiTenantConsumerPool) Stop() error {
    // ...

    // Stop shared consumer
    if p.sharedConsumer != nil {
        if err := p.sharedConsumer.Stop(); err != nil {
            p.logger.Error().Err(err).Msg("Error stopping shared consumer")
        }
    }

    p.logger.Info().
        Uint64("total_routed", p.messagesRouted.Load()).
        Uint64("total_dropped", p.messagesDropped.Load()).
        Uint64("refresh_count", p.refreshCount.Load()).
        Uint64("refresh_errors", p.refreshErrors.Load()).
        Msg("Consumer pool stopped")

    return nil
}
```

### tenant_registry.go - Interface Changes

```go
// Before
type TenantRegistry interface {
    GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error)
    GetDedicatedTenants(ctx context.Context, namespace string) ([]TenantTopics, error)  // REMOVE
}

type TenantTopics struct { ... }  // REMOVE

// After
type TenantRegistry interface {
    // GetSharedTenantTopics returns all topics for tenants using shared consumer mode.
    GetSharedTenantTopics(ctx context.Context, namespace string) ([]string, error)
}
```

### topic_registry.go - Implementation Changes

Remove entire `GetDedicatedTenants()` method and `getCategoriesForTenant()` helper.

---

## Metrics Changes

Update `PoolMetrics` interface (if it exists):

```go
// Before
type PoolMetrics interface {
    OnRefresh(success bool, sharedTopics int, dedicatedConsumers int)
    OnMessageRouted()
}

// After
type PoolMetrics interface {
    OnRefresh(success bool, topicCount int)
    OnMessageRouted()
}
```

Also update any metrics adapter implementations.

---

## Verification

1. **Build:**
   ```bash
   cd ws && go build ./...
   ```

2. **Test:**
   ```bash
   cd ws && go test ./internal/server/orchestration/...
   ```

3. **Local deployment:**
   ```bash
   task local:build
   task local:deploy
   task local:provision:create
   task local:publisher:run RATE=5
   task local:loadtest:run CONNECTIONS=1 DURATION=1m
   ```

4. **Verify messages received > 0**

---

## Database Schema

The `tenants.consumer_type` column can be kept for now (default 'shared'). It's not used after this change but doesn't hurt to keep for potential future use.

Alternatively, remove it with a migration:
```sql
ALTER TABLE tenants DROP COLUMN consumer_type;
```

**Recommendation:** Keep the column for now to avoid migration complexity.

---

## Optional: Rename Pool

Consider renaming `MultiTenantConsumerPool` to `SharedConsumerPool` since it no longer handles multiple consumer types. This is optional and can be done in a follow-up PR.

---

## Files Summary

| File | Action |
|------|--------|
| `ws/internal/server/orchestration/multitenant_pool.go` | Remove dedicated consumer logic |
| `ws/internal/shared/kafka/tenant_registry.go` | Remove `GetDedicatedTenants()` and `TenantTopics` |
| `ws/internal/provisioning/topic_registry.go` | Remove `GetDedicatedTenants()` implementation |
| `ws/internal/shared/metrics/pool_metrics.go` | Update `OnRefresh()` signature (if exists) |
