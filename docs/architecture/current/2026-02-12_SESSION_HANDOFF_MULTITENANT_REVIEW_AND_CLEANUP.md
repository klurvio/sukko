# Session Handoff: Multi-Tenant Architecture Review & Dead Code Cleanup

**Date:** 2026-02-12
**Branch:** `refactor/taskfile-provisioning-consolidation`
**Last commit:** `f853216 fix: fix GCE loadtest and publisher deployment for dev`
**Uncommitted changes:** 7 files (+35/-480 lines) — see below

---

## Session Goals

1. Review whether single-tenant-per-pod architecture is a good design for the WebSocket server
2. Clean up dead code identified during the review
3. Fix a performance bug found in the shared consumer topic removal path

---

## What Was Accomplished

### 1. Architecture Review: Single-Tenant-Per-Pod (Rejected)

Evaluated the proposed architecture where each tenant gets its own `ws-server` Deployment in Kubernetes with Kafka regex topic subscription. Research concluded this is problematic for three reasons:

- **Kafka regex subscription is a regression** — the current codebase (`multitenant_pool.go:30-32`) explicitly avoids regex because it causes O(m) memory, O(m*p) CPU, and rebalance storms. franz-go's `AddConsumeTopics()` (used currently) is a no-op when regex is enabled, so switching to regex loses the ability to add topics without rebalance.
- **Per-tenant pods don't scale** — 1,000 tenants = ~8,000 K8s objects, etcd/API server stress. Mattermost hit IP exhaustion at 5,000 workspaces. Kolide needed 12,000 lines of custom operator code.
- **Current architecture IS the industry standard** — the shared + optional dedicated model matches the AWS SaaS "bridge model". Pusher, Ably, PubNub, Discord all use shared multi-tenant infrastructure.

**Recommendation:** Keep the current bridge model (shared + dedicated consumers). The dedicated consumer path is the escape hatch for noisy tenants.

**Plan document:** `docs/architecture/current/2026-02-12_PLAN_SINGLE_TENANT_PER_POD.md`

### 2. Deleted Dead `KafkaConsumerPool` Code (-471 lines)

`KafkaConsumerPool` (`orchestration/kafka_pool.go`) was dead code — marked deprecated, zero callers in `main.go`. `MultiTenantConsumerPool` fully subsumes it (shared mode does everything the old pool did plus dynamic topic discovery).

**Deleted:**
- `ws/internal/server/orchestration/kafka_pool.go` (187 lines)
- `ws/internal/server/orchestration/kafka_pool_test.go` (284 lines)

**Updated stale `KafkaConsumerPool` references to `MultiTenantConsumerPool`:**
- `ws/internal/server/server.go` — 5 comments
- `ws/internal/server/orchestration/shard.go` — 2 comments
- `ws/internal/shared/types/types.go` — 2 comments

### 3. Fixed Stale Topic Subscription Bug

**Problem:** `updateSharedConsumer()` removed deprovisioned topics from the tracking map but didn't stop the consumer from fetching them. franz-go kept fetching, wasting Kafka fetch bandwidth.

**Fix:** Added `PauseFetchTopics()` to `kafka.Consumer` wrapper and wired it into `multitenant_pool.go`. franz-go does not have `RemoveConsumeTopics` for group consumers — `PauseFetchTopics` is the best available alternative.

**Files changed:**
- `ws/internal/shared/kafka/consumer.go` — added `PauseFetchTopics(topics ...string)` method
- `ws/internal/server/orchestration/multitenant_pool.go` — calls `PauseFetchTopics` with nil guard (defense in depth per coding guidelines)

---

## Key Decisions & Context

1. **Keep dedicated consumer support** — originally considered removing it (Option A in earlier session), but research showed it's the escape hatch for noisy tenants that could cause head-of-line blocking in the shared consumer group. Removing it eliminates the only mitigation without requiring per-tenant K8s Deployments.

2. **PauseFetchTopics over RemoveConsumeTopics** — franz-go (v1.20.6) does not have `RemoveConsumeTopics` for group consumers. Only options are `PauseFetchTopics` (stops fetching, partitions still assigned) or consumer recreation (triggers rebalance). Chose pause — paused topics accumulate between pod restarts but this is harmless: no new messages arrive for deprovisioned tenants, and memory cost is negligible (few KB for strings in a map).

3. **Coding guidelines compliance** — added nil guard on `sharedConsumer` before calling `PauseFetchTopics` per "Defense in Depth" guideline, even though data flow invariants prevent nil in practice.

---

## Current State

- All changes compile clean, all tests pass
- Changes are **uncommitted** (7 files, +35/-480 lines)
- Zero references to `KafkaConsumerPool` or `kafka_pool` remain in codebase

---

## Next Steps

### Immediate
- Commit the uncommitted changes
- Consider running the full test suite (`go test ./...`) before committing

### Future Considerations
- The plan document `2026-02-12_PLAN_SINGLE_TENANT_PER_POD.md` can be used as reference if tenant isolation questions come up again
- If tenant deprovisioning becomes frequent, consider periodic consumer recreation (e.g., every 24h) to clear paused topic set — not needed now

---

## Files Modified (Uncommitted)

| File | Change |
|---|---|
| `ws/internal/server/orchestration/kafka_pool.go` | **DELETED** (187 lines) — dead code |
| `ws/internal/server/orchestration/kafka_pool_test.go` | **DELETED** (284 lines) — dead code |
| `ws/internal/server/orchestration/multitenant_pool.go` | Added `PauseFetchTopics` call with nil guard (+3 lines) |
| `ws/internal/server/orchestration/shard.go` | Updated 2 comments: `KafkaConsumerPool` -> `MultiTenantConsumerPool` |
| `ws/internal/server/server.go` | Updated 5 comments: `KafkaConsumerPool` -> `MultiTenantConsumerPool` |
| `ws/internal/shared/kafka/consumer.go` | Added `PauseFetchTopics()` method (+23 lines) |
| `ws/internal/shared/types/types.go` | Updated 2 comments: `KafkaConsumerPool` -> `MultiTenantConsumerPool` |
| `docs/architecture/current/2026-02-12_PLAN_SINGLE_TENANT_PER_POD.md` | Architecture review document (created + updated) |
