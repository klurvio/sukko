# Plan: MultiTenantConsumerPool Bugfix - Consumer Not Started

**Date:** 2026-02-09
**Status:** Ready for Implementation

---

## Problem

When `shared_topics=0` at startup (database not yet populated or timing issue), no consumer is created initially. After 60 seconds, the refresh loop queries the database, finds topics, and creates the shared consumer - but **never starts it**.

### Symptoms
- `messages_received=0` in loadtest despite publisher working
- Consumer group `sukko-shared-local` shows as `Dead` with 0 members
- No "Starting Kafka consumer" log after "Created shared consumer with initial topics"
- Connection times out after 61 seconds (ping/pong timeout)

### Root Cause

In `updateSharedConsumer()`, the consumer is created but `consumer.Start()` is never called:

```go
// Current code (BUGGY)
p.sharedConsumer = consumer
p.sharedTopics = newTopics

p.logger.Info().
    Strs("topics", topics).
    Msg("Created shared consumer with initial topics")
return nil  // <-- Returns without starting!
```

The `Start()` method only starts the consumer if it exists at initial startup:

```go
func (p *MultiTenantConsumerPool) Start() error {
    // Initial topic discovery
    if err := p.refreshTopics(p.ctx); err != nil { ... }

    // Only starts if consumer exists NOW
    if p.sharedConsumer != nil {
        if err := p.sharedConsumer.Start(); err != nil { ... }
    }
    // ...
}
```

But when consumer is created during refresh (not at startup), it's never started.

---

## Fix

**File:** `ws/internal/server/orchestration/multitenant_pool.go`

**Location:** `updateSharedConsumer()` function, after line 319

**Change:** Add `consumer.Start()` after creating the consumer:

```go
// FIXED code
p.sharedConsumer = consumer
p.sharedTopics = newTopics

// Start the consumer (critical: must start after creation!)
if err := consumer.Start(); err != nil {
    return fmt.Errorf("failed to start shared consumer: %w", err)
}

p.logger.Info().
    Strs("topics", topics).
    Msg("Created shared consumer with initial topics")
return nil
```

---

## Verification

1. Rebuild and deploy:
   ```bash
   task local:build
   ```

2. Check logs for "Starting Kafka consumer" after "Created shared consumer":
   ```bash
   task local:logs
   ```

3. Run publisher and loadtest:
   ```bash
   task local:publisher:run RATE=5
   task local:loadtest:run CONNECTIONS=1 DURATION=1m
   ```

4. Verify `messages_received > 0` in loadtest output

5. Check consumer group is active:
   ```bash
   kubectl exec -n sukko-local sukko-redpanda-0 -- rpk group describe sukko-shared-local
   ```
   Should show `STATE: Stable` with 1 member.

---

## Files Modified

| File | Change |
|------|--------|
| `ws/internal/server/orchestration/multitenant_pool.go` | Add `consumer.Start()` in `updateSharedConsumer()` |
