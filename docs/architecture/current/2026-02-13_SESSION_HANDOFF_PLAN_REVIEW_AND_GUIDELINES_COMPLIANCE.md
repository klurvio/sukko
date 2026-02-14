# Session Handoff: 2026-02-13 - Plan Review, Readability Rewrite & Guidelines Compliance

**Date:** 2026-02-13
**Duration:** ~1.5 hours (continuation of prior session)
**Status:** Plan Complete, Implementation Pending

## Session Goals

1. Make the 120s WebSocket disconnect plan easy to understand (user was lost on who sends what)
2. Review and validate the plan against actual source code
3. Ensure the plan complies with `docs/architecture/CODING_GUIDELINES.md`
4. Make the fix timing-agnostic (works regardless of PingPeriod/PongWait config)

## What Was Accomplished

### 1. Complete Plan Rewrite for Readability

Rewrote `docs/architecture/current/2026-02-13_PLAN_FIX_120S_WEBSOCKET_DISCONNECT.md` from a dense technical doc into an easy-to-follow plan:

- Added **"Who Sends What"** section with roles table and ASCII flow diagram
- Added **visual timeline** with box highlighting the 40-second stale deadline window
- Added **before/after diagram** explaining the fix in plain English before any code
- Replaced dense paragraphs with tables, bullet lists, and annotated code blocks
- Removed Go internals deep-dive (FD write lock, `poll.FD.prepareWrite`) — not needed to understand the plan

### 2. Full Code Validation (Second Pass)

Re-validated every claim in the plan against actual source code. Checked all line numbers, code snippets, and architectural descriptions. Found one wording nuance:

- **Line 133 said:** "ReadLoop never writes data — it goes through `c.send` to WriteLoop"
- **Problem:** ReadLoop doesn't put data into `c.send` — the broadcast path does. ReadLoop reads incoming client messages.
- **Fixed to:** "the broadcast path sends data to `c.send`, and WriteLoop is the only goroutine that writes data to `c.conn`. ReadLoop never touches `c.conn` for writes — except for the pong at line 194"

### 3. Coding Guidelines Compliance (3 Violations Fixed)

Read `docs/architecture/CODING_GUIDELINES.md` (1785 lines) and checked every aspect of the plan. Found 3 violations:

| Guideline violated | Plan section | Fix applied |
|---|---|---|
| **#4 Observable by Default**: "Silent failures are forbidden" | WriteLoop pong write used `_ = wsutil.WriteServerMessage(...)` | Added error check + log + return (matches existing ping write pattern at `pump.go:376-380`) |
| **#5 No Hacks**: "No magic numbers" | `make(chan []byte, 2)` — unexplained `2` | Added named constant `controlChannelSize = 2` with comment explaining why |
| **Observable by Default** | ReadLoop silently dropped pong when channel full | Added `Debug()` log: `"Control channel full, dropped pong"` |

### 4. Made Fix Timing-Agnostic

Three comments in the fix code hardcoded "45-90s" timing assumptions. These would become misleading if `WS_PING_PERIOD` or `WS_PONG_WAIT` are reconfigured. Fixed all three:

| Location | Before | After |
|---|---|---|
| Channel vs mutex rationale | "pongs happen once per 45-90s" | "pongs happen once per PingPeriod" |
| `controlChannelSize` constant comment | "per ping cycle (45-90s)" | "WriteLoop drains with priority (sub-millisecond)...works correctly regardless of PingPeriod configuration" |
| ReadLoop drop comment | "pings arrive at most every 45-90s per client" | "WriteLoop drains control with priority, so a full channel means 2 pongs are already queued and will be written shortly" |

### 5. Generated Daily Summary

Listed everything done on Feb 13 including:
- GCE loadtest & publisher deployment fixes (committed: `f853216`)
- IAP tunneling + firewall rule (committed: `f853216`)
- Multi-tenant architecture review + dead code cleanup (uncommitted)
- 120s WebSocket disconnect investigation & plan (plan complete)

## Key Decisions & Context

### Error handling on pong write must match ping write
The existing ping write at `pump.go:376-380` checks errors, logs at Debug level, and returns (closes connection) on failure. The pong write in WriteLoop must follow the same pattern — the coding guidelines explicitly forbid `_ =` on non-trivial operations ("Silent failures are forbidden", "Never Ignore Errors").

### `controlChannelSize = 2` is not a tuning parameter
Unlike `c.send` (configurable via `WS_CLIENT_SEND_BUFFER_SIZE`), the control channel doesn't need to scale. WriteLoop drains it with a priority select (sub-millisecond). Cap=2 provides headroom if a data batch write briefly delays the drain. Drops are safe and logged.

### Timing assumptions removed from fix code
The fix works correctly regardless of PingPeriod/PongWait configuration. No new hardcoded timing values introduced. All timing comes from existing config (`p.Config.WriteWait`, `p.Config.PingPeriod`, `p.Config.PongWait`).

## Current State

- **Plan document is complete, validated, and guidelines-compliant**: `docs/architecture/current/2026-02-13_PLAN_FIX_120S_WEBSOCKET_DISCONNECT.md`
- **No code changes made** — user wants plan-first approach
- **Root cause confirmed** with high confidence (stale write deadline)
- **All code claims validated** against actual source (line numbers, patterns, behavior)

## Next Steps

### Immediate Priority
- Implement Fix 1: Add `control chan []byte` to Client, update `pump.go` ReadLoop and WriteLoop
- Update `TestReadLoop_PingFrame_SendsPongResponse` for channel-based pong

### Near Term
- Implement Fix 2: Add ping/pong debug logging across server, proxy, loadtest
- Implement Fix 3: Add `WS_PONG_WAIT=60s` / `WS_PING_PERIOD=45s` to `gce.yml` loadtest:run
- Run tests: `cd ws && go test ./internal/server/ -v`
- Build and deploy: `task gce:loadtest:build && task gce:publisher:build`

### Future Considerations
- Consider changing loadtest defaults in `wsloadtest/config.go` (PongWait=120s/PingPeriod=90s are too generous vs industry 60s/45s)
- `orchestration/proxy.go:39` `messageTimeout: 60s` is defined but unused — cleanup candidate
- Uncommitted multi-tenant cleanup changes (7 files, -471 lines) should be committed

## Files Modified (This Session)

| File | Change |
|------|--------|
| `docs/architecture/current/2026-02-13_PLAN_FIX_120S_WEBSOCKET_DISCONNECT.md` | Complete rewrite for readability, guidelines compliance fixes, timing-agnostic comments |

## Commands for Next Session

```bash
# Run existing tests before changes
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws && go test ./internal/server/ -run TestReadLoop -v
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws && go test ./internal/server/ -run TestPump -v

# After implementation
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws && go test ./internal/server/ -v

# Build and deploy for verification
task gce:loadtest:build
task gce:publisher:build
task gce:publisher:run ENV=dev RATE=100 DURATION=30m
task gce:loadtest:run ENV=dev CONNECTIONS=100 DURATION=10m
task gce:loadtest:logs ENV=dev
```

## Open Questions

1. Should the uncommitted multi-tenant cleanup (7 files, -471 lines) be committed before or after the disconnect fix?
2. Should loadtest defaults in `wsloadtest/config.go` be changed to match industry norms, or only overridden via env vars in `gce.yml`?
3. Should `orchestration/proxy.go` `messageTimeout` be removed or wired up?
