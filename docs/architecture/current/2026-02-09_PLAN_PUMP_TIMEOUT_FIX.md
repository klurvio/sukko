# Plan: Fix Shard Ping/Pong Timeout Configuration

**Date:** 2026-02-09
**Status:** Root Cause Identified

---

## Problem Statement

WebSocket connections drop after ~19 minutes with `initiated_by=client reason=read_error`.

**Root Cause:** The shard's ping/pong timeout configuration is too aggressive for a proxied architecture.

---

## Architecture Context

### Connection Chain (8 Network Hops)

```
Loadtest Client
    ↕ [hop 1-2: kubectl port-forward]
Gateway (ws-gateway pod)
    ↕ [hop 3-4: K8s service network]
ws-server Proxy
    ↕ [hop 5-6: localhost within pod]
ShardProxy
    ↕ [hop 7-8: localhost within pod]
Shard (pump.go)
```

A ping/pong round-trip must traverse **all 8 hops**.

### Current Timing Configuration

| Component | pongWait | pingPeriod | Buffer |
|-----------|----------|------------|--------|
| Shard (pump.go) | 30s | 27s | **3s** |
| Loadtest (connection.go) | 60s | 54s | 6s |
| Gateway | N/A (pass-through) | N/A | N/A |

**Problem:** The shard only allows **3 seconds** for the ping to travel through 8 hops and back.

### Why 19 Minutes?

```
19 minutes = 1140 seconds
1140 seconds / 30 second timeout = 38 cycles

38 successful ping/pong cycles completed.
On cycle 39, round-trip took >3 seconds → connection dropped.
```

Possible causes for occasional >3s latency:
- Go garbage collection pause
- Container CPU throttling
- kubectl port-forward hiccup
- Network congestion in Kind cluster
- OS scheduling delays

---

## Why Values Are Hardcoded (Current State)

**File:** `ws/internal/server/pump.go`

```go
func DefaultPumpConfig() PumpConfig {
    pongWait := 30 * time.Second  // HARDCODED
    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  5 * time.Second,
        PingPeriod: (pongWait * 9) / 10, // 27 seconds - HARDCODED
    }
}
```

**Problems:**
1. Not configurable via environment variables
2. Not tunable per environment (local vs production)
3. 90% ratio (pingPeriod/pongWait) is too aggressive for proxied architectures
4. No documentation explaining the values

---

## Recommended Fix

### 1. Make Timing Configurable

**File:** `ws/internal/shared/platform/server_config.go`

Add new config fields:

```go
type ServerConfig struct {
    // ... existing fields ...

    // WebSocket ping/pong configuration
    // PongWait is the timeout for receiving any frame (message or pong).
    // Must be greater than PingPeriod to allow time for round-trip.
    // Default: 60s
    PongWait time.Duration `env:"WS_PONG_WAIT" envDefault:"60s"`

    // PingPeriod is how often to send ping frames to clients.
    // Should be 60-75% of PongWait to allow buffer for network latency.
    // Default: 45s (75% of 60s = 15s buffer)
    PingPeriod time.Duration `env:"WS_PING_PERIOD" envDefault:"45s"`
}
```

### 2. Update pump.go

```go
func NewPumpConfig(pongWait, pingPeriod time.Duration) PumpConfig {
    // Validate: pingPeriod must be less than pongWait
    if pingPeriod >= pongWait {
        pingPeriod = (pongWait * 3) / 4 // Default to 75%
    }

    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  5 * time.Second,
        PingPeriod: pingPeriod,
    }
}

// DefaultPumpConfig for backwards compatibility
func DefaultPumpConfig() PumpConfig {
    return NewPumpConfig(60*time.Second, 45*time.Second)
}
```

### 3. Add Helm Configuration

**File:** `deployments/helm/odin/charts/ws-server/values.yaml`

```yaml
config:
  # WebSocket ping/pong timing
  # Increase for high-latency environments (proxies, port-forward)
  pongWait: "60s"      # Timeout for receiving frames
  pingPeriod: "45s"    # How often to send pings (should be < pongWait)
```

### 4. Environment-Specific Recommendations

| Environment | pongWait | pingPeriod | Buffer | Reason |
|-------------|----------|------------|--------|--------|
| Production (direct) | 30s | 20s | 10s | Low latency, direct connections |
| Production (with gateway) | 60s | 45s | 15s | Gateway adds 2 hops |
| Local (Kind + port-forward) | 90s | 60s | 30s | High latency, multiple proxies |
| Development | 120s | 90s | 30s | Very lenient for debugging |

---

## Immediate Workaround

Until the config is made dynamic, change the hardcoded defaults:

**File:** `ws/internal/server/pump.go`

```go
func DefaultPumpConfig() PumpConfig {
    // 60s timeout with 45s ping interval = 15s buffer
    // Sufficient for: Shard → ShardProxy → ws-server → Gateway → Client (and back)
    pongWait := 60 * time.Second
    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  5 * time.Second,
        PingPeriod: (pongWait * 3) / 4, // 45 seconds (75% ratio)
    }
}
```

---

## Buffer Calculation

```
Buffer = pongWait - pingPeriod

Example with 90% ratio (current):
  Buffer = 30s - 27s = 3s  ❌ Too tight

Example with 75% ratio (recommended):
  Buffer = 60s - 45s = 15s ✅ Safe for proxied architecture

Example with 50% ratio (very safe):
  Buffer = 60s - 30s = 30s ✅ Very safe, but more ping traffic
```

**Recommendation:** Use 75% ratio (pingPeriod = pongWait * 0.75)

---

## Files to Modify

| File | Change |
|------|--------|
| `ws/internal/shared/platform/server_config.go` | Add `PongWait` and `PingPeriod` config fields |
| `ws/internal/server/pump.go` | Accept config values, update defaults |
| `ws/cmd/server/main.go` | Pass config to pump |
| `deployments/helm/odin/charts/ws-server/values.yaml` | Add configurable values |
| `deployments/helm/odin/charts/ws-server/templates/configmap.yaml` | Map env vars |
| `deployments/helm/odin/values/local.yaml` | Set lenient values for local dev |

---

## Verification

After fix, run:

```bash
task local:build
task local:deploy
task local:loadtest:run CONNECTIONS=1 DURATION=60m
```

Connection should remain stable for the full 60 minutes.

---

## WebSocket Libraries Used

| Component | Library | Notes |
|-----------|---------|-------|
| ws-server | gobwas/ws | Low-level, manual ping/pong in pump.go |
| ws-gateway | gobwas/ws | Proxies frames transparently |
| wsloadtest | gorilla/websocket | High-level, auto ping/pong response |

**Note:** The server (gobwas/ws) explicitly handles ping/pong in `pump.go`:
- Sends pings via `wsutil.WriteServerMessage(conn, ws.OpPing, nil)`
- Responds to client pings with `wsutil.WriteServerMessage(conn, ws.OpPong, payload)`
- Refreshes read deadline on ANY frame received (including pong)

---

## References

- [WebSocket RFC 6455 - Ping/Pong](https://datatracker.ietf.org/doc/html/rfc6455#section-5.5.2)
- [gobwas/ws Documentation](https://pkg.go.dev/github.com/gobwas/ws)
- [gorilla/websocket (loadtest only)](https://pkg.go.dev/github.com/gorilla/websocket)
