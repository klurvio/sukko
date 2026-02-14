# Plan: Fix 120s WebSocket Client Disconnect

## Problem

WebSocket clients disconnect after exactly ~120 seconds:
```json
{
  "reason": "read_error",
  "initiated_by": "client",
  "connection_duration": "119998.94ms"
}
```

---

## Who Sends What

### The chain

```
loadtest  ──>  gateway  ──>  orchestration proxy  ──>  ws-server
```

### Roles

| Service | Sends PING | Sends PONG | Notes |
|---------|-----------|-----------|-------|
| **ws-server** | Every 45s (WriteLoop) | Should reply to loadtest's ping (ReadLoop) | **Pong reply is broken — this is the bug** |
| **gateway** | Nothing | Nothing | Transparent pipe — forwards everything unchanged |
| **orchestration proxy** | Nothing | Nothing | Transparent pipe — forwards everything unchanged |
| **loadtest** | Every 90s (writePump) | Auto-replies to server's ping (gorilla built-in) | Only stays alive by receiving pong back |

### Normal flow (when it works)

```
ws-server                              loadtest
    |                                      |
    |──── PING (every 45s) ──────────────>|  server pings loadtest
    |<─── PONG (auto-reply) ──────────────|  gorilla auto-responds
    |                                      |
    |<─── PING (every 90s) ──────────────-|  loadtest pings server
    |──── PONG (reply) ─────────────────->|  server should respond  <── BUG
    |                                      |
    |   loadtest resets 120s deadline      |  (only resets on receiving PONG)
```

**Key point:** The loadtest's 120s read deadline is ONLY refreshed when it receives a PONG. Not on data. Not on pings. Only pongs. If the server fails to send a pong, the loadtest disconnects after 120s.

---

## Root Cause: Stale Write Deadline

The ws-server has **two goroutines** that both need to write to the same connection (`c.conn`):

| Goroutine | What it writes | Sets deadline before writing? |
|-----------|---------------|------------------------------|
| **WriteLoop** | PING (every 45s), data messages | Yes — `SetWriteDeadline(now + 5s)` |
| **ReadLoop** | PONG (reply to client's ping) | **No — uses whatever deadline is already on the connection** |

`SetWriteDeadline` is a **connection-level** setting. Once set by WriteLoop, it applies to ALL writes from ANY goroutine. When it expires, ANY `Write()` call fails immediately — including ReadLoop's pong.

### Timeline of failure

```
t=0s     Loadtest connects. Read deadline = t+120s.

t=45s    WriteLoop sets write deadline to t=50s, sends PING.     ✓ works
t=50s    Write deadline expires.                                  (5s after ping)

t=50-90s ┌─────────────────────────────────────────────────────┐
         │  40 SECONDS OF STALE DEADLINE                       │
         │  Any Write() on this connection fails immediately.  │
         │  ReadLoop's pong write would fail here.             │
         └─────────────────────────────────────────────────────┘

t=90s    Loadtest sends its first PING. Arrives at ws-server.
         ReadLoop tries to write PONG:
           wsutil.WriteServerMessage(c.conn, ws.OpPong, payload)
         But write deadline expired at t=50s (40 seconds ago!).
         Go returns ErrDeadlineExceeded immediately.
         Error is silently discarded:  _ = wsutil.WriteServerMessage(...)
         PONG NEVER SENT.

t=90s    WriteLoop sets fresh deadline to t=95s, sends PING.     ✓ works (too late)

t=120s   Loadtest's 120s PongWait expires. No pong received.
         → "read_error", "initiated_by: client"                  DISCONNECT
```

### Why it works when data is flowing

When the publisher sends data at 100 msg/sec, WriteLoop calls `SetWriteDeadline(now+5s)` on **every data message**. The deadline is refreshed every ~10ms — always fresh. So when ReadLoop writes a pong, the deadline hasn't expired yet and the write succeeds.

When data stops, WriteLoop only refreshes the deadline every 45s (on the ping timer). That leaves a 40-second window where the deadline is expired. ReadLoop's pong write fails silently during this window.

### Why publisher stopping triggers the disconnect

- Publisher ran for exactly 30 minutes, then stopped
- Disconnects started ~120s after publisher stopped
- While data flowed: deadline always fresh, pongs succeeded
- After data stopped: 40/45s of each cycle has expired deadline, pongs fail silently
- The loadtest is **designed to survive without data** — it uses ping/pong keepalive for the full duration. The bug prevents that from working.

### The smoking gun (pump.go)

**ReadLoop line 194** — writes pong WITHOUT setting a deadline, discards the error:
```go
_ = wsutil.WriteServerMessage(c.conn, ws.OpPong, payload)
```

**WriteLoop line 375** — sets a 5s deadline that expires long before the next ping arrives:
```go
_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))  // WriteWait = 5s
```

---

## The Fix

### Idea

Stop letting ReadLoop write to the connection directly. Instead, ReadLoop puts the pong payload into a channel. WriteLoop picks it up, sets a **fresh deadline**, and writes it. WriteLoop becomes the **only goroutine** that ever writes to `c.conn`.

```
BEFORE (broken):
  ReadLoop:  receives ping → writes pong directly to c.conn → FAILS (stale deadline)

AFTER (fixed):
  ReadLoop:  receives ping → puts pong payload into c.control channel
  WriteLoop: picks up from c.control → sets fresh deadline → writes pong to c.conn → SUCCESS
```

This pattern already exists for data messages: the broadcast path sends data to `c.send`, and WriteLoop is the only goroutine that writes data to `c.conn`. ReadLoop never touches `c.conn` for writes — except for the pong at line 194, which is the one rogue write that bypasses this.

### Why channel instead of mutex?

| Approach | Fixes deadline? | Fixes concurrent write? | Drawback |
|----------|----------------|------------------------|----------|
| **Mutex** | No — ReadLoop would still need its own `SetWriteDeadline` call | Yes | Duplicates deadline logic. ReadLoop can block behind a large data flush. |
| **Channel** | Yes — WriteLoop sets fresh deadline before every write | Yes | None meaningful. ~50ns per channel op, pongs happen once per PingPeriod. |

---

## Changes

### Fix 1: Channel-based pong delivery (root cause fix)

**File: `ws/internal/server/connection.go`**

Add `control` channel to Client struct:
```go
type Client struct {
	id        int64
	conn      net.Conn
	server    *Server
	send      chan []byte   // existing: data messages go here
	control   chan []byte   // NEW: pong payloads go here
	closeOnce sync.Once
	// ... rest unchanged
}
```

Add a named constant for the control channel capacity:
```go
// controlChannelSize is the buffer size for the pong control channel.
// Each client ping produces exactly 1 pong. WriteLoop drains this channel with
// priority (sub-millisecond), so at most 1 pong is pending at any time. Cap of 2
// provides headroom if a data batch write briefly delays the drain. If the channel
// is ever full, the pong is dropped with a log and the next client ping retries.
// This works correctly regardless of PingPeriod configuration.
const controlChannelSize = 2
```

Initialize in `ConnectionPool.New`:
```go
client := &Client{
    send:    make(chan []byte, cp.bufferSize),
    control: make(chan []byte, controlChannelSize),
}
```

Drain in `ConnectionPool.Get()` on reuse (same pattern as `send`):
```go
// Drain stale pongs from previous connection
select {
case <-client.control:
default:
}
```

**File: `ws/internal/server/pump.go`**

ReadLoop — send pong to channel instead of writing directly (line ~194):
```go
// BEFORE:
_ = wsutil.WriteServerMessage(c.conn, ws.OpPong, payload)

// AFTER:
select {
case c.control <- payload:
default:
    // Control channel full — drop this pong. The client's next ping will trigger
    // another pong attempt. This is safe: WriteLoop drains control with priority,
    // so a full channel means 2 pongs are already queued and will be written shortly.
    if p.Logger != nil {
        p.Logger.Debug().Int64("client_id", c.id).Msg("Control channel full, dropped pong")
    }
}
```

WriteLoop — add `c.control` case with priority (line ~285):
```go
for {
    // Priority: drain pong responses first (they're time-sensitive)
    select {
    case payload := <-c.control:
        _ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))
        if err := wsutil.WriteServerMessage(c.conn, ws.OpPong, payload); err != nil {
            if p.Logger != nil {
                p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to send pong")
            }
            return
        }
        continue
    default:
    }

    // Normal select: data, ping, or shutdown
    select {
    case <-ctx.Done():
        return

    case payload := <-c.control:
        _ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))
        if err := wsutil.WriteServerMessage(c.conn, ws.OpPong, payload); err != nil {
            if p.Logger != nil {
                p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to send pong")
            }
            return
        }

    case message, ok := <-c.send:
        // ... existing data write logic (unchanged)

    case <-ticker.C():
        // ... existing ping logic (unchanged)
    }
}
```

**Test update: `ws/internal/server/pump_test.go`**

`TestReadLoop_PingFrame_SendsPongResponse` currently expects pong on `mockConn`. After the fix, pong goes to `c.control` channel instead. Update the test to read from `c.control` and verify the payload.

### Fix 2: Add ping/pong debug logging

Add `Debug()` level logs so we can trace ping/pong through the full chain. Zero overhead when log level is info (zerolog short-circuits).

| File | Where | Log message |
|------|-------|-------------|
| `ws/internal/server/pump.go` | ReadLoop, after queuing pong | `"Received ping, queued pong"` |
| `ws/internal/server/pump.go` | ReadLoop, control channel full | `"Control channel full, dropped pong"` |
| `ws/internal/server/pump.go` | ReadLoop, after receiving pong | `"Received pong from client"` |
| `ws/internal/server/pump.go` | WriteLoop, after writing pong | `"Sent pong to client"` |
| `ws/internal/server/pump.go` | WriteLoop, pong write failed | `"Failed to send pong"` (with err) |
| `ws/internal/server/pump.go` | WriteLoop, after writing ping | `"Sent ping to client"` |
| `ws/internal/server/orchestration/proxy.go` | `copyMessages`, on ping/pong | `"Forwarded control frame"` |
| `wsloadtest/connection.go` | PongHandler | `"Received pong, refreshed read deadline"` |
| `wsloadtest/connection.go` | writePump, after ping | `"Sent ping to server"` |

### Fix 3: Match loadtest timing to server

**File: `taskfiles/gce.yml`**

Add `WS_PONG_WAIT=60s` and `WS_PING_PERIOD=45s` to `loadtest:run` so the loadtest uses the same timing as the server instead of its overly generous defaults (120s/90s):

```yaml
docker run -d --rm --name wsloadtest \
    ...existing env vars...
    -e WS_PONG_WAIT=60s \
    -e WS_PING_PERIOD=45s \
    {{.GCE_REGISTRY}}/wsloadtest:latest
```

---

## Files Modified

| File | Change |
|------|--------|
| `ws/internal/server/connection.go` | Add `control chan []byte` to Client, init in pool, drain on reuse |
| `ws/internal/server/pump.go` | ReadLoop: queue pong to channel; WriteLoop: drain channel with fresh deadline |
| `ws/internal/server/pump_test.go` | Update pong test to check `c.control` channel instead of `mockConn` |
| `ws/internal/server/orchestration/proxy.go` | Add debug logging for forwarded ping/pong frames |
| `wsloadtest/connection.go` | Add debug logging in PongHandler and writePump |
| `taskfiles/gce.yml` | Add `WS_PONG_WAIT=60s` and `WS_PING_PERIOD=45s` to loadtest:run |

No changes to `ws/internal/gateway/proxy.go` — it already has `clientWriteMu` and no deadline issues.

## Testing

```bash
# Run existing tests before changes
cd ws && go test ./internal/server/ -run TestReadLoop -v
cd ws && go test ./internal/server/ -run TestPump -v

# After implementation
cd ws && go test ./internal/server/ -v

# Deploy and verify (connections should survive past 120s)
task gce:loadtest:build
task gce:publisher:build
task gce:publisher:run ENV=dev RATE=100 DURATION=30m
task gce:loadtest:run ENV=dev CONNECTIONS=100 DURATION=10m
task gce:loadtest:logs ENV=dev
```

## Safety

- **Additive change** — extends the existing `c.send` channel pattern to pong writes
- **Fixes root cause** — WriteLoop sets fresh `SetWriteDeadline` before every pong write
- **Fixes secondary issue** — eliminates concurrent writes to `c.conn` (single writer)
- **Non-blocking** — ReadLoop never waits on WriteLoop (buffered channel with `default` case)
- **Dropped pongs are harmless** — next ping from client will trigger another pong
- **Debug logging** — zero overhead at info level (zerolog short-circuits)
- **Timing change** — config-only, no code change
