# Session Handoff: 2026-02-16 - Per-Topic Metrics + Collector Cleanup Implementation

**Date:** 2026-02-16
**Status:** In Progress (code implemented, tests partially added, compliance audit incomplete)
**Branch:** `refactor/taskfile-provisioning-consolidation`

## Session Goals

1. Execute the validated plan at `docs/architecture/current/2026-02-15_PLAN_PER_TOPIC_GRAFANA_METRICS.md`
2. Part A: Add per-topic and per-consumer-group labels to Kafka metrics
3. Part B: Remove the legacy Collector (dual-writer bug fix)
4. Verify all changes comply with `docs/architecture/CODING_GUIDELINES.md`
5. Add/update tests as needed

## What Was Accomplished

### Part A: Per-Topic Metrics (Completed)

1. **`ws/internal/server/metrics/metrics.go`** — Changed `kafkaMessagesReceived` and `kafkaMessagesDropped` from `Counter` to `CounterVec` with `["topic", "consumer_group"]` labels. Updated `IncrementKafkaMessages()` and `IncrementKafkaDropped()` to accept `(topic, consumerGroup string)`.

2. **`ws/internal/shared/kafka/consumer.go`** — Added `consumerGroup string` field to `Consumer` struct (set from `cfg.ConsumerGroup` in NewConsumer). Added `topic string` field to `batchedMessage` struct and `prepareMessage` return type. Changed `incrementProcessed()` and `incrementDropped()` to accept `topic string`. Updated all 4 call sites (line 387 batch flush, line 491 prepareMessage rate limit, line 575 processRecord rate limit, line 661 processRecord success).

3. **`deployments/helm/sukko/charts/monitoring/dashboards/overview.json`** — Updated Message Throughput (id:11), Redpanda Throughput (id:20), Kafka Consumer (id:21), Active Connections stat (id:4), Active Connections by Pod (id:10) panels with per-topic/gateway queries. Added new "Gateway Tenant Connections" panel (id:32).

### Part B: Collector Removal (Completed)

4. **`ws/internal/server/metrics/metrics.go`** — Removed entire Collector section (~100 lines): `ServerMetrics` interface, `Collector` struct, `NewCollector()`, `Start()`, `Stop()`, `collect()`, `estimateCPU()`. Removed 5 dead helpers: `SetMemoryUsage`, `SetMemoryLimit`, `SetCPUUsage`, `SetGoroutines`, `SetMaxConnections`. Removed `runtime` and `platform` imports.

5. **`ws/internal/server/metrics/system_monitor.go`** — Added `memoryUsageBytes.Set(float64(mem.Alloc))` and `goroutinesActive.Set(float64(goroutines))` in `updateMetrics()`. Added `memoryLimitBytes.Set()` in `GetSystemMonitor()` init using `platform.GetMemoryLimit()`.

6. **`ws/internal/server/server.go`** — Removed `metricsCollector` field, `metrics.NewCollector(s)` creation, `s.metricsCollector.Start()` call, `GetKafkaConsumer()` method. Updated comments on `GetConfig()`/`GetStats()` to remove "implements metrics.ServerMetrics interface".

7. **`ws/internal/server/server_test.go`** — Removed `TestServer_GetKafkaConsumer_Nil` test and `kafka` import.

8. **`ws/cmd/server/main.go`** — Added `metrics.SetKafkaConnected(true)` after multi-tenant pool starts (line 237).

### Tests Updated/Added (Partially Complete)

9. **`ws/internal/shared/kafka/consumer_test.go`** — Updated all `incrementProcessed()`/`incrementDropped()` calls to pass `"test-topic"`. Added new tests:
   - `TestConsumer_ConsumerGroupField` — verifies field storage
   - `TestConsumer_ConsumerGroupField_Empty` — edge case
   - `TestConsumer_IncrementProcessed_MultipleTopics` — different topics
   - `TestConsumer_IncrementDropped_MultipleTopics` — different topics
   - `TestConsumer_PrepareMessage_IncludesTopic` — verifies topic in return struct
   - `TestConsumer_PrepareMessage_EmptyKey_ReturnNil` — empty key edge case
   - `TestConsumer_PrepareMessage_RateLimited_DropsWithTopic` — rate limit drops
   - Added `kgo` import for `kgo.Record` in prepareMessage tests

### Coding Guidelines Compliance Audit (Partially Complete)

Audited all changes against `docs/architecture/CODING_GUIDELINES.md`:

| Guideline | Status |
|---|---|
| No Hardcoded Values (L29) | ✅ |
| Defense in Depth (L44) | ✅ |
| Observable by Default (L61) | ✅ |
| No Shortcuts/Hacks (L64) | ✅ |
| Interface Design (L217) | ✅ |
| Error Handling (L473) | ✅ |
| Graceful Degradation (L564) | ✅ |
| Logging (L836) | ✅ |
| Configuration (L904) | ✅ |
| Metrics naming/labels (L1137) | ✅ |
| Performance (L1339) | ✅ |
| Concurrency (L1451) | ✅ |
| **Testing (L995, L1731)** | **IN PROGRESS** |

### Build & Test Status

- `go build ./...` — ✅ Compiles cleanly
- `go test ./...` — ✅ All 24 test suites pass (run BEFORE adding the new prepareMessage tests)
- **New tests NOT yet verified** — The prepareMessage tests were added but `go test` was NOT re-run after adding them

## Key Decisions & Context

| Decision | Rationale |
|---|---|
| `CounterVec` with `["topic", "consumer_group"]` | Low cardinality (~16 series), enables per-topic Grafana lines |
| `consumerGroup` stored on struct | Only passed to `kgo.ConsumerGroup()` before, not stored — needed for metrics labels |
| `topic` field added to anonymous return struct | Flows through `prepareMessage` → `batchedMessage` → `incrementProcessed(topic)` |
| SystemMonitor takes over memory/goroutine gauges | Already calls `ReadMemStats` and `NumGoroutine` in `updateMetrics()` — just adds `.Set()` calls |
| `memoryLimitBytes` set in `GetSystemMonitor()` init | Cgroup memory limit is static — set once at startup (same pattern Collector used) |
| `SetKafkaConnected(true)` in main.go | Replaces Collector's nil interface check (which was always true due to Go nil semantics bug) |
| New panel id: 32 | id:30 = "WS Server CPU Usage", id:31 = "WS Server Memory Usage" — both taken |

## Current State

- **Code**: All implementation complete, compiles, all pre-existing tests pass
- **New tests**: Added to consumer_test.go but NOT yet run (need `go test ./...`)
- **Not committed**: All changes are unstaged
- **Not deployed**: No Helm upgrade applied
- **Plan status**: Updated to "Implemented" in plan file
- **metrics/ package**: Still has NO test file — SystemMonitor gauge updates are untested at the unit level (functional testing via integration)

## Next Steps

### Immediate Priority
1. **Run tests** — `cd ws && go test ./...` to verify the new prepareMessage tests pass
2. **Fix any test failures** if the new tests don't compile or pass (kgo.Record usage in test)

### Near Term
3. **Consider adding SystemMonitor tests** — `ws/internal/server/metrics/` has zero test files. The guidelines say "Test coverage for new code paths" (L1731). The new gauge updates (`memoryUsageBytes.Set`, `goroutinesActive.Set`, `memoryLimitBytes.Set`) are untested at the unit level. However, SystemMonitor uses a singleton pattern (`sync.Once`) which makes unit testing harder — may need a reset mechanism or test helper.
4. **Commit all changes** — Once tests pass
5. **Deploy** — `task k8s:build:push:ws-server ENV=dev && task k8s:deploy ENV=dev`
6. **Verify in Grafana** — Check per-topic lines appear in Message Throughput, Redpanda Throughput, Kafka Consumer panels
7. **Verify CPU gauge accuracy** — With Collector removed, `ws_cpu_usage_percent` should now reflect 1s-fresh cgroup data without stale overwrites

### Future Considerations
- `ws/internal/server/metrics.go` `collectMetrics()` (line 15) — another legacy dual-writer for memory (uses gopsutil). Noted as "Future Consideration" in plan.
- `kafka_connected` metric was always 1 due to Go nil interface bug — now correctly set only when pool actually starts
- Consider adding `task gce:loadtest:status` command

## Files Modified

| File | Change |
|---|---|
| `ws/internal/server/metrics/metrics.go` | Counter → CounterVec for Kafka metrics; removed Collector + dead helpers + imports |
| `ws/internal/shared/kafka/consumer.go` | Added consumerGroup field, topic flow through prepareMessage, updated incrementProcessed/Dropped signatures |
| `ws/internal/shared/kafka/consumer_test.go` | Updated call signatures; added 7 new tests for consumerGroup, prepareMessage, multi-topic |
| `ws/internal/server/metrics/system_monitor.go` | Added memory/goroutine gauge updates + memory limit init |
| `ws/internal/server/server.go` | Removed metricsCollector field/creation/start, removed GetKafkaConsumer() |
| `ws/internal/server/server_test.go` | Removed TestServer_GetKafkaConsumer_Nil, removed kafka import |
| `ws/cmd/server/main.go` | Added metrics.SetKafkaConnected(true) after pool starts |
| `deployments/helm/sukko/charts/monitoring/dashboards/overview.json` | Per-topic queries + gateway panels + new Gateway Tenant Connections panel (id:32) |
| `docs/architecture/current/2026-02-15_PLAN_PER_TOPIC_GRAFANA_METRICS.md` | Status → Implemented |

## Commands for Next Session

```bash
# 1. Run tests (FIRST — verify new prepareMessage tests pass)
cd /Volumes/Dev/Codev/Toniq/sukko/ws && go test ./...

# 2. If tests pass, commit
cd /Volumes/Dev/Codev/Toniq/sukko
git add ws/internal/server/metrics/metrics.go \
        ws/internal/server/metrics/system_monitor.go \
        ws/internal/server/server.go \
        ws/internal/server/server_test.go \
        ws/internal/shared/kafka/consumer.go \
        ws/internal/shared/kafka/consumer_test.go \
        ws/cmd/server/main.go \
        deployments/helm/sukko/charts/monitoring/dashboards/overview.json
git commit -m "feat: add per-topic Kafka metrics and remove legacy Collector"

# 3. Deploy
task k8s:build:push:ws-server ENV=dev && task k8s:deploy ENV=dev

# 4. Verify metrics
kubectl port-forward -n sukko-dev svc/ws-server 3005:3005
curl -s localhost:3005/metrics | grep ws_kafka_messages_received_total
# Should show topic and consumer_group labels

# 5. Verify Collector removed
curl -s localhost:3005/metrics | grep ws_cpu_usage_percent
# Still present (now only from SystemMonitor, no stale overwrites)
```

## Open Questions

1. **SystemMonitor test file**: Should `ws/internal/server/metrics/system_monitor_test.go` be created? The singleton `sync.Once` pattern makes unit testing harder — would need a reset mechanism. The gauge updates are simple `.Set()` calls tested by Prometheus library itself.
2. **prepareMessage tests**: Do the new tests compile? They use `kgo.Record` directly — need to verify the import resolves correctly.
3. **KAFKA_CONSUMER_ENABLED toggle**: From previous session — 6 files implemented but NOT committed. Should these be committed in the same or separate commit?
