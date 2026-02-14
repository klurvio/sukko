# Session Handoff: 2026-02-09 - Consumer Bugfix & Ping/Pong Investigation

**Date:** 2026-02-09 22:45
**Duration:** ~2 hours
**Status:** In Progress (architectural decision needed)

---

## Session Goals

1. Fix `messages_received=0` issue in loadtest
2. Investigate WebSocket connection dropping after ~19 minutes
3. Document findings and create implementation plans

---

## What Was Accomplished

### 1. Fixed MultiTenantConsumerPool Bug ✅

**Problem:** When `shared_topics=0` at startup, no consumer created initially. After 60s refresh, consumer was created but **never started**.

**Fix Applied:**
```go
// ws/internal/server/orchestration/multitenant_pool.go (lines 322-325)
p.sharedConsumer = consumer
p.sharedTopics = newTopics

// Start the consumer (critical: must start after creation!)
if err := consumer.Start(); err != nil {
    return fmt.Errorf("failed to start shared consumer: %w", err)
}
```

**Commit:** `3a61b8c fix: start shared consumer after creation in refresh loop`

### 2. Identified 19-Minute Disconnect Root Cause ✅

**Symptom:** WebSocket connection drops after ~19 minutes with `initiated_by=client reason=read_error`

**Root Cause Found:** Shard's ping/pong timing too aggressive for proxied architecture.

| Setting | Current Value | Problem |
|---------|---------------|---------|
| pongWait | 30s | Read timeout |
| pingPeriod | 27s | Ping interval |
| **Buffer** | **3s** | Must cover 8-hop round-trip! |

**Why 19 minutes?** 38 successful ping/pong cycles × 30s ≈ 19 min. On cycle 39, round-trip took >3s.

### 3. Architectural Assessment: Ping/Pong Location

**Current (problematic):** Shard handles ping/pong → ping must travel through 8 hops

**Industry Standard:** Gateway should handle client ping/pong (AWS API Gateway, Cloudflare, Kong)

**Recommendation:** Move ping/pong handling to gateway layer.

### 4. Created Documentation

| File | Purpose |
|------|---------|
| `2026-02-09_PLAN_MULTITENANT_POOL_BUGFIX.md` | Consumer start bug fix |
| `2026-02-09_PLAN_REMOVE_DEDICATED_CONSUMERS.md` | Simplify to shared-only |
| `2026-02-09_PLAN_RPK_WARNINGS_FIX.md` | Redpanda warnings fix |
| `2026-02-09_PLAN_PUMP_TIMEOUT_FIX.md` | Ping/pong timeout analysis |
| `2026-02-09_PLAN_19MIN_DISCONNECT_INVESTIGATION.md` | Full disconnect investigation |

---

## Key Decisions & Context

### Decision Pending: Where Should Ping/Pong Be Handled?

**Option A: Gateway (Recommended)**
- Gateway sends pings to client, handles pongs
- Shard trusts gateway for connection health
- Industry standard approach
- Requires gateway refactor

**Option B: Increase Shard Timeouts (Quick Fix)**
- Change pongWait from 30s to 60s
- Change pingPeriod from 27s to 45s (75% ratio)
- 15s buffer instead of 3s
- Doesn't fix architectural issue

### Context: WebSocket Library Usage

| Component | Library | Ping/Pong |
|-----------|---------|-----------|
| ws-server, gateway | gobwas/ws | Manual handling in pump.go |
| wsloadtest | gorilla/websocket | Auto-response |

### Context: Proxy Chain Architecture

```
Loadtest Client
    ↕ kubectl port-forward (2 hops)
Gateway (gobwas/ws proxy)
    ↕ K8s service (2 hops)
ws-server ShardProxy (gobwas/ws proxy)
    ↕ localhost (2 hops)
Shard (pump.go - ping/pong here)
```

**Total: 8 network hops for ping/pong round-trip**

---

## Technical Details

### Current Ping/Pong Configuration

**Shard (pump.go):**
```go
func DefaultPumpConfig() PumpConfig {
    pongWait := 30 * time.Second  // HARDCODED
    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  5 * time.Second,
        PingPeriod: (pongWait * 9) / 10, // 27 seconds
    }
}
```

**Loadtest (connection.go):**
```go
pongWait = 60 * time.Second
pingPeriod = (pongWait * 9) / 10  // 54 seconds
```

### Kafka Consumer Status (Working)
```
Consumer group: odin-shared-local
State: Stable
Members: 1
LAG: 2
Topics: local.odin.{trade,liquidity,metadata,social,community,creation,analytics,balances}
```

---

## Issues & Solutions

| Issue | Solution |
|-------|----------|
| Consumer not started after refresh | Added `consumer.Start()` call - FIXED |
| 19-min disconnect | Root cause identified - PENDING FIX |
| Hardcoded timeouts | Need to make configurable - PENDING |
| Grafana port-forward died | Separate from loadtest issue (different port) |

---

## Current State

**Working:**
- Consumer bugfix committed and pushed
- Publisher publishing ~1 msg/sec
- Kafka consumer receiving messages
- NATS broadcast working
- Gateway health endpoint responding

**Not Working:**
- WebSocket connections drop after ~19 minutes
- Ping/pong buffer too tight (3s for 8 hops)

**Running:**
- Publisher: `docker ps | grep wspublisher` → running
- Loadtest: completed (connection dropped at 19 min)
- Port-forwards: gateway (3006), redpanda (9092), prometheus (9090) active; grafana (3010) died

---

## Next Steps

### Immediate Priority
1. **Decide:** Gateway handles ping/pong vs increase shard timeouts
2. If gateway approach: Create implementation plan for gateway ping/pong
3. If quick fix: Make `pongWait`/`pingPeriod` configurable via env vars

### Near Term
- Remove dedicated consumer functionality (plan exists)
- Add Redpanda `--mode=dev-container` for local (plan exists)
- Align loadtest and shard ping/pong timeouts

### Future Considerations
- Per-tenant server instances (one tenant per ws-server deployment)
- XFS filesystem for production Redpanda
- TCP keepalives in proxy connections

---

## Files Modified This Session

| File | Change |
|------|--------|
| `ws/internal/server/orchestration/multitenant_pool.go` | Added `consumer.Start()` in `updateSharedConsumer()` |
| `docs/architecture/current/2026-02-09_PLAN_*.md` | Multiple plan documents created |

---

## Commands for Next Session

```bash
# Rebuild and deploy
task local:build
task local:deploy

# Run longer test to verify fix
task local:loadtest:run CONNECTIONS=1 DURATION=60m

# Check consumer group
kubectl exec -n odin-local odin-redpanda-0 -- rpk group describe odin-shared-local

# Check ws-server logs
kubectl logs -n odin-local -l app.kubernetes.io/name=ws-server --tail=100

# Restart port-forwards if needed
task local:port-forward:start
```

---

## Open Questions

1. **Should gateway handle all client ping/pong?** (Architectural decision needed)
   - Industry standard says yes
   - Requires gateway refactor
   - Alternative: just increase timeouts

2. **Should ping/pong timeouts be configurable?**
   - Currently hardcoded in `DefaultPumpConfig()`
   - Different environments need different values

3. **Why exactly 19 minutes?**
   - Best hypothesis: GC pause or CPU throttle on cycle 39
   - Could add metrics to track ping/pong latency

---

## Git Status

```
Branch: feature/multi-tenant-auth-implementation
Last commit: 3a61b8c fix: start shared consumer after creation in refresh loop
Pushed: Yes
```
