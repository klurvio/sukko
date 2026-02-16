# Session Handoff: 2026-02-15 - KAFKA_CONSUMER_ENABLED Toggle Implementation

**Date:** 2026-02-15
**Status:** Complete (code implemented, not committed)
**Branch:** `refactor/taskfile-provisioning-consolidation`

## Session Goals

1. Ensure the KAFKA_CONSUMER_ENABLED toggle plan follows `docs/architecture/CODING_GUIDELINES.md`
2. Review and validate the plan for regressions and safety
3. Implement the plan

## What Was Accomplished

### 1. Coding Guidelines Compliance Review
- Mapped every plan item to specific guideline sections with line numbers
- Created a compliance table covering: No Hardcoded Values (L29), Feature Flags (L790), Degradation Decision Matrix (L822), Health check three-state (L704), Configuration patterns (L904), Nil checks (L631), Logging (L836), Print()/LogConfig() (L503/594)
- Both plan files updated with compliance table

### 2. Safety Review — Full Code Path Trace
Traced every code path that touches `kafkaConsumer` when disabled:
- **Health check** (`handlers_http.go:49-62`): Reports "disabled", healthy=true
- **Metrics collector** (`metrics.go:611`): Pre-existing Go interface nil semantics issue (not introduced by this plan)
- **Reconnect handler** (`handlers_message.go:229`): Already handles nil consumer gracefully — sends error to client
- **Publish handler** (`handlers_publish.go:36`): Independent — uses `kafkaProducer`, not consumer
- **Server init** (`server.go:156`): Skips type assertion when `SharedKafkaConsumer` is nil
- **Shutdown** (`main.go:372-389`): All nil checks already present
- **WebSocket, BroadcastBus, rate limiting, ResourceGuard**: All independent of Kafka consumer

### 3. Critical Issue Found and Fixed
**Provisioning DB would crash server when unreachable** — The original plan guarded only lines 215-236 (pool creation). But lines 190-213 (provisioning DB connect + ping + topic registry) are exclusively used by the consumer pool. If `KAFKA_CONSUMER_ENABLED=false` and the DB is unreachable, the server would `Fatalf` at line 205, defeating the purpose. Guard expanded to cover lines 190-236.

### 4. Implementation Complete (5 files, all tests pass)
All 24 Go test suites pass with zero regressions.

## Key Decisions & Context

| Decision | Rationale |
|---|---|
| Guard expanded to 190-236 (not just 215-236) | Provisioning DB + topic registry are exclusively for consumer pool; unreachable DB would crash server |
| `KafkaConsumerDisabled` bool in types.go (not `KafkaConsumerEnabled`) | Health check needs a positive flag for the disabled state; avoids double-negation in `if !config.KafkaConsumerEnabled` |
| Health check reports "disabled" + healthy (not degraded/unhealthy) | Intentionally disabled != broken. User explicitly chose "Stay healthy" during planning |
| Producer stays outside guard | Clients can still publish messages even when consumer is disabled |
| No changes to metrics.go | `kafka_connected` gauge has a pre-existing Go nil interface semantics issue — fixing it is out of scope |
| Added to Helm chart | User explicitly requested `consumerEnabled: true` in values.yaml + configmap.yaml |
| `logger.Printf` for disabled message (not zerolog) | Consistent with existing main.go startup pattern (lines 143, 210, 236) |

## Technical Details

### Consumer Disabled Flow
```
main.go: if cfg.KafkaConsumerEnabled → false
  → Skip provisioning DB connection (lines 190-213)
  → Skip consumer pool creation (lines 215-236)
  → Log "Kafka consumer DISABLED"
  → Producer still created (lines 242-263)
  → ShardConfig gets KafkaConsumerDisabled: true

server.go: config.SharedKafkaConsumer is nil
  → kafkaConsumer stays nil
  → No consumer lifecycle to manage

handlers_http.go: s.config.KafkaConsumerDisabled == true
  → kafkaStatus = "disabled", kafkaHealthy = true
  → Health check returns 200 OK

handlers_message.go: s.kafkaConsumer == nil
  → Reconnect requests get error: "Message replay not available"
  → No crash, graceful degradation
```

### Pre-existing Issue (Not Introduced)
`metrics.go:611` — `GetKafkaConsumer()` returns `any` wrapping a nil `*kafka.Consumer`. Due to Go's interface nil semantics `(type=*kafka.Consumer, value=nil)`, the nil check `!= nil` returns TRUE. The `kafka_connected` Prometheus gauge always reports 1 regardless of actual state. This is a pre-existing bug, not related to this change.

## Issues & Solutions

| Issue | Solution |
|---|---|
| Plan initially guarded only pool creation (215-236) | Expanded to include provisioning DB (190-236) after tracing startup crash path |
| Print() placement inconsistency between plan files | Fixed — both now say "after line 513, before Kafka Security section" |
| Missing space in Print() format string | Fixed — `"Consumer Enabled: %v"` with consistent spacing |
| Docs plan file had "No Helm chart changes" after adding Helm section | Fixed — removed contradictory line |

## Current State

- **Code**: Implemented and compiling, all 24 test suites pass
- **Not committed**: Changes are staged but no git commit was made
- **Not deployed**: No Helm upgrade or kubectl changes applied
- **Loadtest VM**: `wspublisher-dev` is TERMINATED (preempted from previous session). `wsloadtest-dev` is RUNNING.
- **NAT fix**: Applied to dev, stg Terraform updated but not applied

## Next Steps

### Immediate Priority
- Commit the KAFKA_CONSUMER_ENABLED changes
- Deploy publisher VM: `task gce:publisher:deploy ENV=dev`
- Run loadtest with consumer enabled first (baseline): `task gce:loadtest:run ENV=dev CONNECTIONS=500 DURATION=30m RAMP=5`

### Near Term
- Run loadtest with consumer disabled (`KAFKA_CONSUMER_ENABLED=false`) to isolate connection handling
- Compare connection sustainability: with vs without consumer
- Scale up to 10K, 30K connections
- Apply NAT Terraform to stg: `task k8s:tf:plan ENV=stg` + `task k8s:tf:apply ENV=stg`

### Future Considerations
- Fix pre-existing `kafka_connected` metric nil interface bug in `metrics.go:611`
- Consider adding a `task gce:loadtest:status` command to check VM + container state
- Prod Cloud NAT stays on defaults (64 ports) — no loadtest VMs expected in prod

## Files Modified

| File | Change |
|---|---|
| `ws/internal/shared/platform/server_config.go` | Added `KafkaConsumerEnabled` env var field (L54), Print() output (L520), LogConfig() field (L608) |
| `ws/internal/shared/types/types.go` | Added `KafkaConsumerDisabled bool` to ServerConfig (L41) |
| `ws/cmd/server/main.go` | Wrapped provisioning DB + pool creation (L190-240) in `if cfg.KafkaConsumerEnabled`; added flag to shard config (L314) |
| `ws/internal/server/handlers_http.go` | 3-way health check: disabled/running/stopped (L49-62) |
| `deployments/helm/odin/charts/ws-server/values.yaml` | Added `consumerEnabled: true` under kafka section (L148-151) |
| `deployments/helm/odin/charts/ws-server/templates/configmap.yaml` | Added `KAFKA_CONSUMER_ENABLED` env var (L59-60) |
| `docs/architecture/current/2026-02-14_PLAN_KAFKA_CONSUMER_TOGGLE.md` | Updated plan with compliance table, expanded guard scope, Helm section |

## Commands for Next Session

```bash
# Commit the changes
cd /Volumes/Dev/Codev/Toniq/odin-ws
git add ws/internal/shared/platform/server_config.go \
        ws/internal/shared/types/types.go \
        ws/cmd/server/main.go \
        ws/internal/server/handlers_http.go \
        deployments/helm/odin/charts/ws-server/values.yaml \
        deployments/helm/odin/charts/ws-server/templates/configmap.yaml
git commit -m "feat: add KAFKA_CONSUMER_ENABLED toggle for connection-only loadtesting"

# Deploy publisher VM (was preempted)
task gce:publisher:deploy ENV=dev

# Run loadtest (baseline with consumer enabled)
task gce:loadtest:run ENV=dev CONNECTIONS=500 DURATION=30m RAMP=5

# Disable consumer for isolated connection test
kubectl set env deployment/ws-server KAFKA_CONSUMER_ENABLED=false -n odin-ws-dev

# Re-enable consumer after test
kubectl set env deployment/ws-server KAFKA_CONSUMER_ENABLED=true -n odin-ws-dev

# Apply NAT fix to stg
task k8s:tf:init ENV=stg
task k8s:tf:plan ENV=stg
task k8s:tf:apply ENV=stg
```

## Open Questions

1. **`kafka_connected` metric**: Pre-existing nil interface bug means it always reports 1. Should this be fixed separately?
2. **Loadtest at 30K connections**: Will the 2 gateway pods + 2 ws-server pods handle 30K? May need to scale replicas or node resources.
3. **`KAFKA_CONSUMER_GROUP` env var**: Set to `odin-ws-consumer` in Helm but the code constructs `odin-shared-dev` from namespace. The env var appears unused — should it be removed?
