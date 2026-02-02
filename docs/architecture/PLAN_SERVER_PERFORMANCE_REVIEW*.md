# Server Package Performance Review and Improvements

**Goal:** Review `ws/internal/server` for performance hotspots, CPU bottlenecks, and improve robustness, testability, readability, security, and maintainability where needed.

**Status:** Planned (Reviewed 2026-02-02)
**Date:** 2026-02-02

---

## Review Notes (2026-02-02)

Investigation completed for two open questions:

### Config Constants Location: Use `shared/config/` NOT `server/config/`

**Reason:** Cross-package usage analysis revealed:
- 5-second timeout used in: `server/handlers_*.go`, `broadcast/valkey.go`, `broadcast/nats.go`, `orchestration/loadbalancer.go`, **`shared/alerting/slack.go`**
- 10-second timeout used in: `broadcast/valkey.go`, `broadcast/nats.go`, `metrics.go`, `orchestration/proxy.go`, `gateway/multi_issuer_oidc.go`
- 30-second timeout used in: `server.go`, `pump.go`, `metrics.go`, `broadcast/valkey.go`, `provisioning/kafka/admin.go`

If constants lived in `server/config/`, then `shared/alerting/slack.go` would need to import from `server/` — this **breaks dependency direction** (shared should never depend on server).

**Decision:** Create `ws/internal/shared/config/timeouts.go` for cross-package constants. Keep single-use constants (100ms broadcast, 100 max replay) local to their packages.

### Buffer Stats Cache: Safe to Implement

**Analysis of `/health` endpoint consumers:**
| Consumer | What it checks | Uses buffer_saturation? |
|----------|---------------|------------------------|
| K8s liveness probe | HTTP 200 | No |
| K8s readiness probe | HTTP 200 | No |
| Load balancer | status field | No |
| Prometheus | `/metrics` endpoint | No |
| `loadtest/main.go` | capacity, cpu, memory | **No** |

**Verified:** The loadtest tool only reads `checks.capacity.current`, `checks.cpu.percentage`, `checks.memory.percentage` — NOT `buffer_saturation`.

**Decision:** 5-second cache for buffer stats is safe. Only cache `observability.buffer_saturation.*`, NOT critical checks (kafka, cpu, memory, capacity).

---

## Executive Summary

The server package is **already well-optimized** for a high-performance WebSocket trading platform. Key optimizations already in place:
- Copy-on-write SubscriptionIndex with atomic.Value for lock-free reads (93% CPU savings)
- Single-serialization broadcast pattern (99.92% reduction in JSON marshaling)
- Message batching in WriteLoop (reduces syscalls)
- Token bucket rate limiting with sync.Pool for client reuse
- Hysteresis in ResourceGuard (prevents oscillation)

**Primary gaps to address:**
1. Test coverage (39% server, 18.3% broadcast, 9.6% orchestration)
2. Minor CPU hotspots in health endpoint and disconnect path
3. Magic numbers scattered throughout code
4. Some error handling inconsistencies

---

## Current Performance Analysis

### Already Optimized (No Changes Needed)

| Component | Optimization | Location |
|-----------|-------------|----------|
| SubscriptionIndex.Get | Lock-free atomic load (10ns vs 250ns) | connection.go:516-538 |
| Broadcast | Single serialization for all clients | broadcast.go:115-134 |
| WriteLoop | Message batching before flush | pump.go:306-331 |
| RateLimiter | sync.Map for concurrent access | rate_limiter.go:220-224 |
| ConnectionPool | sync.Pool for client reuse | connection.go:99-136 |
| ResourceGuard | Hysteresis for CPU decisions | resource_guard.go:79-80 |

### Identified CPU Hotspots

| Priority | Issue | Location | Impact |
|----------|-------|----------|--------|
| LOW | Bubble sort O(n²) in buffer stats | handlers_http.go:253-259 | ~1μs per /health call |
| LOW | RemoveClient O(channels × subscribers) | connection.go:476-503 | Disconnect path, not hot |
| LOW | runtime.ReadMemStats STW pause | metrics/metrics.go:600-601 | Every metrics interval |
| VERY LOW | JSON marshal for ack messages | handlers_message.go | Infrequent, not hot path |

---

## Phase 1: Test Coverage Improvements (HIGH PRIORITY)

Current coverage is inadequate for a production trading platform.

### Current State

| Package | Coverage | Target |
|---------|----------|--------|
| server | 39.0% | 70%+ |
| broadcast | 18.3% | 60%+ |
| orchestration | 9.6% | 50%+ |
| limits | 79.5% | 80%+ ✓ |
| messaging | 100.0% | 100% ✓ |

### Files Needing Tests

| File | Current Tests | Priority |
|------|---------------|----------|
| handlers_message.go | handlers_message_test.go exists but incomplete | HIGH |
| handlers_http.go | handlers_http_test.go exists but incomplete | MEDIUM |
| handlers_publish.go | handlers_publish_test.go exists but incomplete | HIGH |
| broadcast.go | broadcast_test.go exists but incomplete | MEDIUM |
| orchestration/loadbalancer.go | loadbalancer_test.go minimal | MEDIUM |

### Test Scenarios to Add

**handlers_message_test.go:**
- TestHandleClientMessage_Subscribe_Success
- TestHandleClientMessage_Subscribe_InvalidChannels
- TestHandleClientMessage_Unsubscribe_Success
- TestHandleClientMessage_Publish_Success
- TestHandleClientMessage_Publish_RateLimited
- TestHandleClientMessage_Reconnect_Success
- TestHandleClientMessage_UnknownType

**handlers_publish_test.go:**
- TestHandleClientPublish_NoProducer
- TestHandleClientPublish_InvalidChannel
- TestHandleClientPublish_MessageTooLarge
- TestHandleClientPublish_KafkaError

**handlers_http_test.go:**
- TestHandleHealth_AllHealthy
- TestHandleHealth_CPUOverThreshold
- TestHandleHealth_MemoryOverLimit
- TestHandleHealth_KafkaDown

---

## Phase 2: Minor Performance Optimizations (LOW PRIORITY)

These are not critical but are easy wins.

### 2.1 Replace Bubble Sort with slices.Sort

**File:** `handlers_http.go:253-259`

**Current:**
```go
// Simple bubble sort for small arrays (max 100 elements)
for i := range sorted {
    for j := i + 1; j < len(sorted); j++ {
        if sorted[i] > sorted[j] {
            sorted[i], sorted[j] = sorted[j], sorted[i]
        }
    }
}
```

**Improved:**
```go
slices.Sort(sorted)
```

**Impact:** O(n log n) vs O(n²), though n is capped at 100 so minimal real-world impact.

### 2.2 Cache Buffer Stats Calculation

Instead of recalculating on every /health call, cache the result.

**File:** `handlers_http.go:219-220`

**Approach:** Calculate buffer stats during sampling (every 5s), not on demand.

**What to cache (safe):**
- `observability.buffer_saturation.*` (samples, avg, p50, p95, p99, max)

**What NOT to cache (must remain live):**
- `checks.kafka` — critical connectivity check
- `checks.cpu`, `checks.memory`, `checks.goroutines` — used for rejection decisions
- `checks.capacity.current` — live connection count

**Implementation:**
```go
type CachedBufferStats struct {
    Stats     map[string]any
    ExpiresAt time.Time
}

const BufferStatsCacheTTL = 5 * time.Second

// In health handler:
if time.Now().Before(s.cachedBufferStats.ExpiresAt) {
    return s.cachedBufferStats.Stats
}
// Recalculate and update cache
```

**Staleness tolerance:** Acceptable because:
1. Buffer samples are already collected every 10 seconds (inherently stale)
2. K8s probes don't inspect buffer_saturation field
3. Loadtest tool doesn't use buffer_saturation
4. No monitoring/alerting depends on this field

---

## Phase 3: Configuration Consolidation (MEDIUM PRIORITY)

Extract magic numbers to configuration constants.

### Magic Numbers Found

| Value | Location | Description | Scope |
|-------|----------|-------------|-------|
| 5 seconds | handlers_publish.go:87 | Kafka publish timeout | Cross-package |
| 5 seconds | handlers_message.go:247 | Replay context timeout | Cross-package |
| 5 seconds | orchestration/loadbalancer.go:154 | Metrics aggregation interval | Cross-package |
| 5 seconds | shared/alerting/slack.go:38 | HTTP client timeout | Cross-package |
| 10 seconds | broadcast/valkey.go:466 | Health check interval | Cross-package |
| 10 seconds | metrics.go:119 | Metrics collection interval | Cross-package |
| 30 seconds | server.go:341 | Shutdown grace period | Cross-package |
| 30 seconds | pump.go:32 | WebSocket pong wait | Cross-package |
| 100ms | broadcast/valkey.go:166 | Publish timeout | Local (broadcast) |
| 100 | handlers_message.go:254 | Max replay messages | Local (server) |

### Proposed Constants File

**New file:** `ws/internal/shared/config/timeouts.go`

> **Note:** Using `shared/config/` NOT `server/config/` because these constants are used across multiple packages including `shared/alerting/slack.go`. Putting them in `server/config/` would create a reverse dependency (shared → server).

```go
package config

import "time"

// Operation timeouts (5 seconds) - used for publish, replay, external API calls
const (
    // PublishOperationTimeout is the timeout for publishing to Kafka
    PublishOperationTimeout = 5 * time.Second

    // ReplayOperationTimeout is the timeout for replaying messages from Kafka
    ReplayOperationTimeout = 5 * time.Second

    // ExternalAPITimeout is the timeout for external service calls (Slack, etc.)
    ExternalAPITimeout = 5 * time.Second

    // MetricsAggregationInterval is how often metrics are aggregated
    MetricsAggregationInterval = 5 * time.Second
)

// Health/monitoring intervals (10 seconds)
const (
    // HealthCheckInterval is how often to check service health
    HealthCheckInterval = 10 * time.Second

    // MetricsCollectionInterval is how often to collect metrics
    MetricsCollectionInterval = 10 * time.Second

    // JWKSFetchTimeout is the timeout for fetching JWKS from OIDC provider
    JWKSFetchTimeout = 10 * time.Second
)

// Shutdown/grace periods (30 seconds)
const (
    // ShutdownGracePeriod is the time to drain connections on shutdown
    ShutdownGracePeriod = 30 * time.Second

    // WebSocketPongWait is the max time to wait for a WebSocket pong response
    WebSocketPongWait = 30 * time.Second

    // KafkaAdminTimeout is the timeout for Kafka admin operations
    KafkaAdminTimeout = 30 * time.Second
)
```

### Local Constants (Keep in Package)

**`broadcast/` package:**
```go
const BroadcastPublishTimeout = 100 * time.Millisecond
```

**`server/` package:**
```go
const MaxReplayMessages = 100
```

---

## Phase 4: Error Handling Improvements (LOW PRIORITY)

### 4.1 Consistent Error Wrapping

Some errors are logged but not wrapped with context.

**Example - handlers_publish.go:90-95:**
```go
if err := s.kafkaProducer.Publish(ctx, c.id, pubReq.Channel, pubReq.Data); err != nil {
    // Good: error is logged with context
    // Could improve: wrap error for upstream handlers
}
```

---

## Phase 5: Minor Security Hardening (LOW PRIORITY)

### 5.1 JSON Unmarshal Size Limits

Add size checks before JSON unmarshal in handlers.

**File:** `handlers_message.go:15-25`

**Current:**
```go
func (s *Server) handleClientMessage(c *Client, data []byte) {
    var req protocol.ClientMessage
    if err := json.Unmarshal(data, &req); err != nil {
```

**Improved:**
```go
func (s *Server) handleClientMessage(c *Client, data []byte) {
    if len(data) > protocol.MaxClientMessageSize {
        s.logger.Warn().Int("size", len(data)).Msg("Message too large")
        return
    }
    var req protocol.ClientMessage
    if err := json.Unmarshal(data, &req); err != nil {
```

**Note:** The ReadLoop already has read deadline and the WebSocket frame size is bounded by gobwas/ws defaults, so this is defense-in-depth.

---

## Implementation Order

| Phase | Priority | Effort | Description |
|-------|----------|--------|-------------|
| 1 | HIGH | 4-6h | Test coverage improvements |
| 2 | LOW | 1h | Minor performance optimizations |
| 3 | MEDIUM | 2h | Configuration consolidation |
| 4 | LOW | 1h | Error handling improvements |
| 5 | LOW | 0.5h | Security hardening |

**Total Estimated Effort:** 8-10 hours

---

## Verification

```bash
# Run all tests with coverage
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go test -cover ./internal/server/...

# Run benchmarks (if added)
go test -bench=. ./internal/server/...

# Check for race conditions
go test -race ./internal/server/...

# Verify no regressions
task test
```

---

## Files to Modify

### Test Files (Phase 1)
| File | Action |
|------|--------|
| `handlers_message_test.go` | Expand with more scenarios |
| `handlers_publish_test.go` | Expand with edge cases |
| `handlers_http_test.go` | Add health check scenarios |
| `orchestration/loadbalancer_test.go` | Expand coverage |

### Source Files (Phases 2-5)
| File | Changes |
|------|---------|
| `handlers_http.go` | Replace bubble sort with slices.Sort, add buffer stats cache |
| `handlers_message.go` | Add size check before unmarshal |
| `shared/config/timeouts.go` | NEW - centralized cross-package timeout constants |

### Files That Will Import from `shared/config/`
- `ws/internal/server/handlers_publish.go`
- `ws/internal/server/handlers_message.go`
- `ws/internal/server/server.go`
- `ws/internal/server/pump.go`
- `ws/internal/server/metrics.go`
- `ws/internal/server/broadcast/valkey.go`
- `ws/internal/server/broadcast/nats.go`
- `ws/internal/server/orchestration/loadbalancer.go`
- `ws/internal/shared/alerting/slack.go`
- `ws/internal/gateway/multi_issuer_oidc.go`

---

## What NOT to Change

The following are already well-optimized and should not be modified:

1. **SubscriptionIndex copy-on-write pattern** - Already optimal
2. **Single-serialization broadcast** - Already 99.92% efficient
3. **WriteLoop message batching** - Already optimal
4. **RateLimiter with sync.Map** - Industry standard
5. **ResourceGuard hysteresis** - Correct design pattern
6. **Pump read/write separation** - Clean design

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Test changes break existing tests | Run full test suite after each change |
| Performance regression | Benchmark before/after |
| Config changes affect production | Use existing values as defaults |
| Race conditions | Always run with -race flag |

---

## Conclusion

The server package is already well-designed for high-performance WebSocket operations. The main gaps are in **test coverage** (39% is too low for production trading systems) and **minor code quality improvements**. The performance-critical paths (broadcast, subscription lookup) are already optimized with industry-best patterns.

**Recommendation:** Focus 80% of effort on Phase 1 (test coverage). The performance optimizations in Phase 2 are micro-optimizations that won't meaningfully impact production performance.
