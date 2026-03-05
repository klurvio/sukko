# Session Handoff: 2026-02-13 - Fix 120s WebSocket Client Disconnect

**Date:** 2026-02-13
**Duration:** ~2 hours (continued from prior session)
**Status:** Plan Complete, Implementation Pending

## Session Goals

1. Complete the investigation and plan for the 120s WebSocket client disconnect bug
2. Validate the root cause analysis against actual code
3. Choose the right synchronization approach (mutex vs channel)

## What Was Accomplished

### 1. Plan Document Created and Refined
- Created comprehensive plan: `docs/architecture/current/2026-02-13_PLAN_FIX_120S_WEBSOCKET_DISCONNECT.md`
- Multiple review/revision cycles to ensure accuracy

### 2. Root Cause CORRECTED Through Deep Validation
The initial root cause theory (concurrent write race corrupting WebSocket frames) was **proven wrong** through code-level validation. The actual root cause was identified:

**Initial (wrong) theory:** `wsutil.WriteServerMessage` concurrent calls interleave bytes, corrupting frames on the wire.

**Why it's wrong:** Go's `net.Conn.Write()` is serialized by an internal FD write lock (`poll.FD.writeLock`). For empty-payload ping/pong, each frame is a single 2-byte `Write()` call. Two concurrent calls produce two valid frames, not corrupted bytes.

**Actual root cause: Stale Write Deadline**
- WriteLoop calls `c.conn.SetWriteDeadline(now + 5s)` when writing (line 375 for ping, line 309 for data)
- `SetWriteDeadline` is a conn-level setting affecting ALL goroutines
- PingPeriod=45s, WriteWait=5s → deadline expires 5s after each ping → **40s stale window every 45s cycle**
- ReadLoop writes pong at line 194 WITHOUT setting its own deadline → uses stale conn deadline
- `prepareWrite()` checks deadline BEFORE syscall → returns `ErrDeadlineExceeded` immediately
- Error silently discarded by `_ = wsutil.WriteServerMessage(...)` at line 194
- Pong never written → loadtest PongWait=120s expires → disconnect

### 3. Channel-Based Fix Design (over Mutex)
- User asked about alternatives to `sync.Mutex` → designed channel-based approach
- Add `control chan []byte` to Client struct
- ReadLoop sends pong payload to `c.control` (non-blocking)
- WriteLoop drains `c.control` with priority select, sets fresh deadline before each write
- No new goroutine needed — just one more case in existing WriteLoop select

### 4. Industry Ping/Pong Research
Benchmarked against major exchanges:
- Binance: 20s ping, 60s pong timeout
- Bybit: 20s ping, 10min idle disconnect
- Kraken: 1s heartbeat
- Server (ours): 45s ping, 60s pong timeout — reasonable
- Loadtest (ours): 90s ping, 120s pong timeout — too generous

### 5. Publisher Correlation Analysis
- Publisher VM ran for exactly DURATION=30m, exited cleanly
- Disconnects started ~120s after publisher stopped
- While data flows, WriteLoop refreshes deadline on every data write → always fresh
- When data stops, deadline stale for 40/45s of each cycle → pong writes fail

## Key Decisions & Context

### Channel over Mutex
**Decision:** Use channel-based write serialization instead of `sync.Mutex`
**Rationale:**
- Fixes both the stale deadline (WriteLoop sets fresh deadline before each pong write) and concurrent write issues
- Mutex would serialize writes but ReadLoop would still need its own `SetWriteDeadline` call, duplicating logic
- Channel gives single-writer ownership — idiomatic Go ("share memory by communicating")
- `c.send` already uses this pattern for data; `c.control` extends it to pong
- No priority inversion risk (mutex: ReadLoop could block behind large data batch flush)

### Loadtest is Designed for No-Data Survival
**Confirmed:** The loadtest maintains connections purely via ping/pong for the full DURATION. Zero dependency on data messages. `runner.go:sustain()` waits for full duration regardless of data flow. The 120s disconnect is entirely a server-side bug.

## Technical Details

### The Stale Deadline Mechanism
```
t=45s:  WriteLoop SetWriteDeadline(t=50s), writes PING → succeeds
t=50s:  Deadline expires
t=50-90s: ANY conn.Write() fails with ErrDeadlineExceeded (40s window!)
~t=90s: ReadLoop receives loadtest PING, writes pong → FAILS (deadline t=50s expired)
         Error discarded by `_ =` at line 194
t=90s:  WriteLoop SetWriteDeadline(t=95s), writes PING → succeeds (too late)
t=120s: Loadtest PongWait expires → disconnect
```

### Go FD Write Lock (why frame corruption theory was wrong)
- `poll.FD.Write()` acquires `fd.writeLock` (exclusive) for entire Write() duration
- Two concurrent `conn.Write([0x8A, 0x00])` calls are fully serialized
- Empty-payload frame = single 2-byte Write() = atomic
- `conn.Write(nil)` still acquires lock but writes 0 bytes (no-op on wire)
- Corruption CAN happen with non-empty payloads (header+payload = 2 Write calls), but ping/pong use empty payloads

### Three Fixes in Plan
1. **Channel-based write serialization** (root cause fix) — `c.control` channel, priority select in WriteLoop
2. **Ping/pong trace logging** — Debug-level logging across full chain (server, proxy, loadtest)
3. **Loadtest timing alignment** — Pass `WS_PONG_WAIT=60s` and `WS_PING_PERIOD=45s` to loadtest:run in gce.yml

## Issues & Solutions

| Issue | Solution |
|-------|----------|
| Initial root cause was wrong (frame corruption) | Deep-dived into Go's `poll.FD`, `wsutil.WriteServerMessage` call chain, proved FD write lock prevents interleaving |
| User rejected direct code edit | Created plan document in `docs/architecture/current/` instead |
| Test `TestReadLoop_PingFrame_SendsPongResponse` will break | Plan needs test update — ReadLoop no longer writes pong to conn, sends to channel instead |
| `ConnectionPool.Get()` doesn't drain new `control` channel | Added drain logic to plan (same pattern as existing `send` drain) |

## Current State

- **Plan document is complete and validated**: `docs/architecture/current/2026-02-13_PLAN_FIX_120S_WEBSOCKET_DISCONNECT.md`
- **No code changes made** — user wants plan-first approach
- **gce.yml has IAP changes from prior session** (already committed: `f853216`)
- **Root cause is confirmed** with high confidence (stale write deadline mechanism verified against Go source)

## Next Steps

### Immediate Priority
- Implement Fix 1: Add `control chan []byte` to Client, update pump.go ReadLoop and WriteLoop
- Update `TestReadLoop_PingFrame_SendsPongResponse` to verify pong goes to `c.control` channel (not `mockConn`)

### Near Term
- Implement Fix 2: Add ping/pong debug logging across the chain
- Implement Fix 3: Add `WS_PONG_WAIT=60s` / `WS_PING_PERIOD=45s` to `taskfiles/gce.yml` loadtest:run
- Run existing pump tests: `cd ws && go test ./internal/server/ -run TestReadLoop -v`
- Build and deploy: `task gce:loadtest:build && task gce:publisher:build`

### Future Considerations
- Consider adding a unit test for the stale deadline scenario specifically
- The concurrent write to `c.conn` is still technically unsafe for non-empty payloads (secondary bug) — channel fix addresses this too
- `orchestration/proxy.go:39` `messageTimeout: 60s` is defined but unused — cleanup candidate
- Loadtest default PongWait=120s / PingPeriod=90s are unusually generous vs industry (20-60s) — consider changing defaults in `wsloadtest/config.go`

## Files Modified (This Session)

| File | Change |
|------|--------|
| `docs/architecture/current/2026-02-13_PLAN_FIX_120S_WEBSOCKET_DISCONNECT.md` | Created and refined — complete plan with corrected root cause |

## Files To Be Modified (Implementation)

| File | Change |
|------|--------|
| `ws/internal/server/connection.go` | Add `control chan []byte` to Client struct, initialize in ConnectionPool, drain in Get() |
| `ws/internal/server/pump.go` | ReadLoop: send pong to `c.control`; WriteLoop: add `c.control` case with priority select + `SetWriteDeadline` |
| `ws/internal/server/pump_test.go` | Update `TestReadLoop_PingFrame_SendsPongResponse` for channel-based pong |
| `ws/internal/server/orchestration/proxy.go` | Add debug logging for ping/pong frame forwarding |
| `wsloadtest/connection.go` | Add debug logging in PongHandler and writePump |
| `taskfiles/gce.yml` | Add `WS_PONG_WAIT=60s` and `WS_PING_PERIOD=45s` to loadtest:run |

## Commands for Next Session

```bash
# Run existing tests before changes
cd /Volumes/Dev/Codev/Toniq/sukko/ws && go test ./internal/server/ -run TestReadLoop -v
cd /Volumes/Dev/Codev/Toniq/sukko/ws && go test ./internal/server/ -run TestPump -v

# After implementation, run tests
cd /Volumes/Dev/Codev/Toniq/sukko/ws && go test ./internal/server/ -v

# Build and deploy for verification
task gce:loadtest:build
task gce:publisher:build
task gce:publisher:run ENV=dev RATE=100 DURATION=30m
task gce:loadtest:run ENV=dev CONNECTIONS=100 DURATION=10m
task gce:loadtest:logs ENV=dev
```

## Open Questions

1. Should the test for the new `c.control` drain in WriteLoop be a unit test or integration test?
2. Should the loadtest defaults (PongWait=120s, PingPeriod=90s) be changed in `wsloadtest/config.go` to match industry norms (60s/45s), or just overridden via env vars in gce.yml?
3. The `orchestration/proxy.go` `messageTimeout` field is unused — should it be removed or wired up?
