# Session Handoff: 2026-02-10 - Configurable Ping/Pong Timeouts

**Date:** 2026-02-10
**Duration:** ~1.5 hours
**Status:** Plan Complete - Ready for Implementation

---

## Session Goals

1. Continue from previous session investigating 19-minute disconnect issue
2. Create implementation plan for fixing the ping/pong timeout issue
3. Ensure plan follows coding guidelines

---

## What Was Accomplished

### 1. Corrected Architectural Assumption

Initially created a plan to move ping/pong handling to the gateway, believing this was "industry standard." After user questioned this, researched and found:

- **AWS API Gateway:** Does NOT send pings - only responds to client pings
- **NGINX:** Transparent proxy for WebSocket, backend handles ping/pong
- **Industry standard:** Backend servers send pings, not gateways

Superseded the gateway plan and created correct approach: make shard timeouts configurable.

### 2. Created Comprehensive Implementation Plan

**File:** `docs/architecture/current/2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md`

The plan includes:
- 10 implementation steps with exact file locations and line numbers
- Code snippets for all changes
- Table-driven tests per coding guidelines
- Helm configuration updates
- Coding guidelines compliance matrix

### 3. Identified All Files to Modify

| File | Change |
|------|--------|
| `ws/internal/shared/platform/server_config.go` | Add `PongWait`, `PingPeriod` fields + validation |
| `ws/internal/shared/types/types.go` | Add fields to internal config |
| `ws/internal/server/pump.go` | Add constants, `NewPumpConfig()` |
| `ws/internal/server/pump_test.go` | Update test expectations |
| `ws/internal/server/server.go` | Use config values with fallback logging |
| `ws/cmd/server/main.go` | Copy fields to shardConfig |
| `deployments/helm/sukko/charts/ws-server/values.yaml` | Add config |
| `deployments/helm/sukko/charts/ws-server/templates/configmap.yaml` | Map env vars |
| `deployments/helm/sukko/values/local.yaml` | Set lenient local values |

### 4. Reviewed Plan Against Coding Guidelines

Updated plan to comply with:
- No magic numbers (added named constants with rationale)
- Never ignore errors (log warnings on fallback)
- Table-driven tests
- Structured logging with zerolog

---

## Key Decisions & Context

### Decision 1: Keep Ping/Pong in Shard (Not Gateway)

**Context:** Initially assumed gateway should handle ping/pong based on intuition about "edge handling."

**Research Finding:** AWS API Gateway, NGINX, and other major reverse proxies do NOT send pings to clients. Backend servers are responsible for keepalive.

**Decision:** Keep ping/pong in shard, just make timeouts configurable.

**Rationale:**
- Industry standard approach
- Minimal code changes (vs gateway refactor)
- Solves the problem with increased buffer

### Decision 2: Default Values 60s/45s (15s Buffer)

| Setting | Old | New | Rationale |
|---------|-----|-----|-----------|
| PongWait | 30s | 60s | Matches AWS API Gateway idle timeout |
| PingPeriod | 27s | 45s | 75% ratio provides 15s buffer |
| Buffer | 3s | 15s | 5x improvement, handles 8-hop latency |

### Decision 3: Local Dev Values 120s/90s (30s Buffer)

**Rationale:** Local development has additional latency sources:
- `kubectl port-forward` can stall for seconds
- Docker Desktop networking overhead
- Kind container networking
- Resource contention on developer machine
- Debugger pauses

30s buffer (2x production) provides stable connections during development.

### Decision 4: NewPumpConfig Takes Logger Parameter

**Rationale:** Per coding guidelines "Never Ignore Errors", when `pingPeriod >= pongWait` (invalid), the function logs a warning before falling back to 75% ratio instead of silently correcting.

---

## Technical Details

### Two ServerConfig Types

The codebase has two `ServerConfig` types that both need updating:

1. **`platform.ServerConfig`** (`ws/internal/shared/platform/server_config.go`)
   - Loaded from environment variables
   - Source of truth for configuration
   - Has validation, Print(), LogConfig()

2. **`types.ServerConfig`** (`ws/internal/shared/types/types.go`)
   - Used internally by server
   - Subset of fields
   - Created manually in `main.go` by copying from platform config

### Config Flow

```
platform.LoadServerConfig()
    ↓
main.go copies fields to types.ServerConfig (line 270-304)
    ↓
orchestration.NewShard(ShardConfig{ServerConfig: ...})
    ↓
server.NewServer(config types.ServerConfig)
    ↓
server.NewPump(NewPumpConfig(config.PongWait, config.PingPeriod, logger))
```

### New Environment Variables

```bash
WS_PONG_WAIT=60s      # Timeout for pong response (default: 60s)
WS_PING_PERIOD=45s    # How often to send pings (default: 45s)
```

### Validation Constraints

```go
const (
    minPongWait   = 10 * time.Second  // Must allow 8-hop round-trip
    minPingPeriod = 5 * time.Second   // Prevents excessive ping traffic
)
// Also: PingPeriod < PongWait (required for buffer to exist)
```

---

## Issues & Solutions

### Issue 1: Incorrect Assumption About Gateway Ping/Pong

**Problem:** Assumed gateway handling ping/pong was industry standard.

**Solution:** Researched AWS API Gateway, NGINX documentation. Found backend servers handle ping/pong. Created correct plan.

**Sources:**
- https://repost.aws/questions/QUV-egTr6_Skylz2_OHp8irw/websocket-api-server-side-ping-pong
- https://nginx.org/en/docs/http/websocket.html

### Issue 2: Existing Tests Would Fail

**Problem:** `TestDefaultPumpConfig` tests hardcoded 30s/27s values.

**Solution:** Added step to update test expectations to 60s/45s.

### Issue 3: Silent Fallback Violates Coding Guidelines

**Problem:** Original `NewPumpConfig` silently fell back to 75% ratio when `pingPeriod >= pongWait`.

**Solution:** Added logger parameter, log warning when falling back.

---

## Current State

### Plan Files Created

| File | Status |
|------|--------|
| `2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md` | Complete, ready for implementation |
| `2026-02-10_PLAN_GATEWAY_PING_PONG.md` | Superseded (marked in file) |

### Previous Session Context

From `2026-02-09_SESSION_HANDOFF_CONSUMER_BUGFIX_AND_PING_PONG_INVESTIGATION.md`:
- Consumer start bug was fixed (commit `3a61b8c`)
- 19-minute disconnect root cause identified (3s buffer too tight)
- Ping/pong travels through 8 network hops

### What's Working

- Consumer bugfix deployed
- Publisher running
- Kafka consumer receiving messages
- Root cause identified and documented

### What's Not Working

- WebSocket connections still drop after ~19 minutes (fix not yet implemented)

---

## Next Steps

### Immediate Priority

1. **Implement the plan** - Follow the 10 steps in `2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md`

### Implementation Order

1. `platform/server_config.go` - Add fields, validation, Print/LogConfig
2. `types/types.go` - Add fields
3. `pump.go` - Add constants, NewPumpConfig
4. `pump_test.go` - Update TestDefaultPumpConfig
5. `server.go` - Use config values
6. `main.go` - Copy fields
7. Helm values/configmap
8. `local.yaml` - Set lenient values
9. Build and deploy
10. Run 60-minute loadtest to verify

### Near Term

- Run 60-minute loadtest to verify fix
- Monitor for any remaining disconnect issues

### Future Considerations

- Consider adding ping/pong latency metrics
- May need to tune values for production with CDN
- Document recommended values for different deployment scenarios

---

## Files Modified This Session

| File | Change |
|------|--------|
| `docs/architecture/current/2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md` | Created - full implementation plan |
| `docs/architecture/current/2026-02-10_PLAN_GATEWAY_PING_PONG.md` | Created then superseded |

---

## Commands for Next Session

```bash
# After implementing changes, build and deploy
task local:build
task local:deploy

# Run extended loadtest to verify fix
task local:loadtest:run CONNECTIONS=1 DURATION=60m

# Check ws-server logs for ping/pong configuration
kubectl logs -n sukko-local -l app.kubernetes.io/name=ws-server | grep -E "(pong|ping|Ping|Pong)"

# Verify no disconnects
kubectl logs -n sukko-local -l app.kubernetes.io/name=ws-server --tail=1000 | grep disconnect

# Run tests after implementation
cd ws && go test ./internal/server/... -v -run "Pump"
cd ws && go test ./internal/shared/platform/... -v -run "PingPong"
```

---

## Open Questions

1. **Should we add ping/pong latency metrics?**
   - Would help diagnose future timeout issues
   - Low priority, can be added later

2. **What values should production with CDN use?**
   - Plan recommends 90s/60s (30s buffer)
   - May need tuning based on actual CDN latency

3. **Should we warn if buffer < 10s?**
   - Currently validation just checks `pingPeriod < pongWait`
   - Could add warning for small buffers (not blocking)

---

## Git Status

```
Branch: feature/multi-tenant-auth-implementation
Last commit from previous session: 3a61b8c fix: start shared consumer after creation in refresh loop
New commits this session: None (planning only)
```

---

## References

- Previous session: `2026-02-09_SESSION_HANDOFF_CONSUMER_BUGFIX_AND_PING_PONG_INVESTIGATION.md`
- Implementation plan: `2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md`
- Coding guidelines: `docs/architecture/CODING_GUIDELINES.md`
- WebSocket RFC: https://datatracker.ietf.org/doc/html/rfc6455#section-5.5.2
