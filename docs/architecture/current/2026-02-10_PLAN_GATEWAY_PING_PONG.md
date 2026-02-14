# Plan: Move Ping/Pong Handling to Gateway

**Date:** 2026-02-10
**Status:** SUPERSEDED - See 2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md
**Priority:** N/A

> **Note:** This plan was superseded after research showed that moving ping/pong to the gateway
> is NOT industry standard. AWS API Gateway, NGINX, and other reverse proxies expect the
> backend server to handle ping/pong. The simpler solution is to make shard timeouts configurable.
> See `2026-02-10_PLAN_CONFIGURABLE_PING_PONG_TIMEOUTS.md` for the actual implementation plan.

---

## Original Plan (For Reference)

---

## Executive Summary

Move WebSocket ping/pong keepalive handling from the shard (deep in the server) to the gateway (edge). This follows industry standard practices (AWS API Gateway, Cloudflare, Kong) where the edge proxy handles client keepalives.

---

## Problem Statement

### Current Architecture

```
Loadtest Client
    ↕ [hop 1-2: kubectl port-forward]
Gateway (ws-gateway) ←── NO ping/pong handling, just pass-through
    ↕ [hop 3-4: K8s service network]
ws-server Proxy ←── NO ping/pong handling, just pass-through
    ↕ [hop 5-6: localhost within pod]
ShardProxy ←── NO ping/pong handling, just pass-through
    ↕ [hop 7-8: localhost within pod]
Shard (pump.go) ←── PING/PONG HERE with 30s timeout, 27s interval
```

**Problem:** Ping must travel through **8 network hops** for round-trip. With only 3-second buffer (30s - 27s), any latency spike causes disconnect.

**Result:** Connections drop after ~19 minutes (38 successful cycles × 30s, then one cycle takes >3s).

### Industry Standard

Edge proxies handle client keepalives:
- **AWS API Gateway:** 10-minute idle timeout, gateway sends pings
- **Cloudflare:** Gateway handles WebSocket keepalive
- **Kong:** Edge-level ping/pong configuration
- **NGINX:** `proxy_read_timeout` at edge, not forwarded to backend

---

## Target Architecture

### Two-Level Keepalive Model

```
Client
    ↕ [PING/PONG: Gateway ↔ Client] ←── NEW: Gateway handles client keepalive
Gateway (ws-gateway)
    ↕ [PING/PONG: Gateway ↔ Backend] ←── NEW: Gateway-to-backend keepalive
ws-server Proxy
    ↕ [pass-through]
ShardProxy
    ↕ [pass-through]
Shard (pump.go) ←── REMOVE or SIMPLIFY: No client ping/pong needed
```

### Key Principles

1. **Gateway owns client connection health** - Gateway determines if client is alive
2. **Gateway-to-backend keepalive** - Separate keepalive for internal connections
3. **Shard trusts gateway** - If gateway forwards data, connection is valid
4. **No pass-through of ping/pong** - Gateway intercepts and handles, doesn't forward

---

## Detailed Design

### 1. Gateway Ping/Pong Handler

**File:** `ws/internal/gateway/proxy.go`

Add dedicated ping/pong handling in the proxy:

```go
type Proxy struct {
    // ... existing fields ...

    // Ping/pong configuration (client-facing)
    clientPongWait   time.Duration  // Time to wait for client pong
    clientPingPeriod time.Duration  // How often to ping client

    // Ping/pong configuration (backend-facing)
    backendPongWait   time.Duration  // Time to wait for backend pong
    backendPingPeriod time.Duration  // How often to ping backend

    // Lifecycle
    done chan struct{}
}

type ProxyConfig struct {
    MessageTimeout time.Duration

    // Client keepalive
    ClientPongWait   time.Duration  // Default: 60s
    ClientPingPeriod time.Duration  // Default: 45s

    // Backend keepalive
    BackendPongWait   time.Duration  // Default: 30s
    BackendPingPeriod time.Duration  // Default: 20s
}
```

### 2. Client Ping/Pong Loop

Add a new goroutine in the proxy for client keepalive:

```go
// clientPingLoop sends periodic pings to client and monitors responses
func (p *Proxy) clientPingLoop() {
    ticker := time.NewTicker(p.clientPingPeriod)
    defer ticker.Stop()

    for {
        select {
        case <-p.done:
            return
        case <-ticker.C:
            // Set write deadline
            p.clientConn.SetWriteDeadline(time.Now().Add(5 * time.Second))

            // Send ping frame
            p.clientWriteMu.Lock()
            err := ws.WriteHeader(p.clientConn, ws.Header{
                Fin:    true,
                OpCode: ws.OpPing,
                Length: 0,
            })
            p.clientWriteMu.Unlock()

            if err != nil {
                p.logger.Debug().Err(err).Msg("Failed to send ping to client")
                return
            }
        }
    }
}
```

### 3. Intercept Ping/Pong Frames

Modify `proxyClientToBackend` to handle ping/pong instead of forwarding:

```go
func (p *Proxy) proxyClientToBackend() {
    // Set initial read deadline
    p.clientConn.SetReadDeadline(time.Now().Add(p.clientPongWait))

    for {
        header, err := ws.ReadHeader(p.clientConn)
        if err != nil {
            // Handle error...
            return
        }

        payload := make([]byte, header.Length)
        if header.Length > 0 {
            _, err = io.ReadFull(p.clientConn, payload)
            if err != nil {
                return
            }
        }

        // Unmask if needed
        if header.Masked {
            ws.Cipher(payload, header.Mask, 0)
        }

        switch header.OpCode {
        case ws.OpPing:
            // INTERCEPT: Respond with pong, don't forward
            p.clientWriteMu.Lock()
            ws.WriteHeader(p.clientConn, ws.Header{
                Fin:    true,
                OpCode: ws.OpPong,
                Length: int64(len(payload)),
            })
            p.clientConn.Write(payload)
            p.clientWriteMu.Unlock()

            // Refresh read deadline
            p.clientConn.SetReadDeadline(time.Now().Add(p.clientPongWait))
            continue  // Don't forward to backend

        case ws.OpPong:
            // INTERCEPT: Client responded to our ping
            p.logger.Debug().Msg("Received pong from client")

            // Refresh read deadline
            p.clientConn.SetReadDeadline(time.Now().Add(p.clientPongWait))
            continue  // Don't forward to backend

        case ws.OpClose:
            // Handle close frame (existing logic)

        default:
            // Forward other frames to backend
            p.forwardFrame(header, payload)

            // Refresh read deadline on any activity
            p.clientConn.SetReadDeadline(time.Now().Add(p.clientPongWait))
        }
    }
}
```

### 4. Backend Keepalive

Add separate keepalive for gateway-to-backend connection:

```go
// backendPingLoop keeps the backend connection alive
func (p *Proxy) backendPingLoop() {
    ticker := time.NewTicker(p.backendPingPeriod)
    defer ticker.Stop()

    for {
        select {
        case <-p.done:
            return
        case <-ticker.C:
            // Set write deadline
            p.backendConn.SetWriteDeadline(time.Now().Add(5 * time.Second))

            // Send ping to backend (masked, as client)
            frame := ws.NewPingFrame(nil)
            if err := ws.WriteFrame(p.backendConn, frame); err != nil {
                p.logger.Debug().Err(err).Msg("Failed to send ping to backend")
                return
            }
        }
    }
}
```

Similarly, modify `proxyBackendToClient` to intercept backend ping/pong:

```go
func (p *Proxy) proxyBackendToClient() {
    // Set initial read deadline for backend
    p.backendConn.SetReadDeadline(time.Now().Add(p.backendPongWait))

    for {
        header, err := ws.ReadHeader(p.backendConn)
        // ... read payload ...

        switch header.OpCode {
        case ws.OpPing:
            // Backend pinged us - respond with pong
            ws.WriteFrame(p.backendConn, ws.NewPongFrame(payload))
            p.backendConn.SetReadDeadline(time.Now().Add(p.backendPongWait))
            continue  // Don't forward to client

        case ws.OpPong:
            // Backend responded to our ping
            p.backendConn.SetReadDeadline(time.Now().Add(p.backendPongWait))
            continue  // Don't forward to client

        default:
            // Forward data frames to client
            p.sendToClient(header, payload)
            p.backendConn.SetReadDeadline(time.Now().Add(p.backendPongWait))
        }
    }
}
```

### 5. Modify Proxy.Run()

Update to start keepalive goroutines:

```go
func (p *Proxy) Run() error {
    p.done = make(chan struct{})

    // Start keepalive goroutines
    go p.clientPingLoop()
    go p.backendPingLoop()

    // Run proxy loops (existing)
    errChan := make(chan error, 2)
    go func() { errChan <- p.proxyClientToBackend() }()
    go func() { errChan <- p.proxyBackendToClient() }()

    // Wait for first error
    err := <-errChan

    // Signal shutdown
    close(p.done)

    // Close connections
    p.clientConn.Close()
    p.backendConn.Close()

    return err
}
```

### 6. Simplify Shard Ping/Pong

**File:** `ws/internal/server/pump.go`

Since gateway now handles client keepalive, shard can simplify:

**Option A: Remove shard ping/pong entirely**
- Shard trusts that if data arrives, connection is valid
- Gateway handles all keepalive
- Simpler code

**Option B: Keep shard ping/pong for internal health (Recommended)**
- Reduce frequency (ping every 60s instead of 27s)
- Increase timeout (120s instead of 30s)
- Acts as last-resort health check
- Catches zombie gateway connections

```go
func DefaultPumpConfig() PumpConfig {
    // Lenient settings - gateway handles primary keepalive
    pongWait := 120 * time.Second
    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  10 * time.Second,
        PingPeriod: 60 * time.Second,  // Much less frequent
    }
}
```

---

## Configuration

### Gateway Config Updates

**File:** `ws/internal/shared/platform/gateway_config.go`

```go
type GatewayConfig struct {
    // ... existing fields ...

    // Client keepalive (gateway ↔ client)
    ClientPongWait   time.Duration `env:"GATEWAY_CLIENT_PONG_WAIT" envDefault:"60s"`
    ClientPingPeriod time.Duration `env:"GATEWAY_CLIENT_PING_PERIOD" envDefault:"45s"`

    // Backend keepalive (gateway ↔ ws-server)
    BackendPongWait   time.Duration `env:"GATEWAY_BACKEND_PONG_WAIT" envDefault:"30s"`
    BackendPingPeriod time.Duration `env:"GATEWAY_BACKEND_PING_PERIOD" envDefault:"20s"`
}
```

### Helm Values

**File:** `deployments/helm/odin/charts/ws-gateway/values.yaml`

```yaml
config:
  # Client keepalive (gateway ↔ client)
  clientPongWait: "60s"      # Timeout for client response
  clientPingPeriod: "45s"    # How often to ping client

  # Backend keepalive (gateway ↔ ws-server)
  backendPongWait: "30s"     # Timeout for backend response
  backendPingPeriod: "20s"   # How often to ping backend
```

### Environment-Specific Values

| Environment | Client Pong Wait | Client Ping Period | Backend Pong Wait | Backend Ping Period |
|-------------|------------------|--------------------|--------------------|---------------------|
| Production | 60s | 45s | 30s | 20s |
| Staging | 60s | 45s | 30s | 20s |
| Local (Kind) | 90s | 60s | 60s | 45s |

---

## Files to Modify

| File | Changes |
|------|---------|
| `ws/internal/shared/platform/gateway_config.go` | Add ping/pong config fields |
| `ws/internal/gateway/proxy.go` | Add ping/pong handling, intercept frames |
| `ws/internal/gateway/gateway.go` | Pass config to proxy |
| `ws/internal/server/pump.go` | Relax timeouts, optional ping/pong |
| `deployments/helm/odin/charts/ws-gateway/values.yaml` | Add config values |
| `deployments/helm/odin/charts/ws-gateway/templates/configmap.yaml` | Map env vars |
| `deployments/helm/odin/charts/ws-server/values.yaml` | Update shard timeouts |
| `deployments/helm/odin/values/local.yaml` | Set lenient local values |

---

## Implementation Steps

### Phase 1: Gateway Ping/Pong (Core Fix)

1. **Add config fields** to `gateway_config.go`
2. **Update Proxy struct** with keepalive fields and done channel
3. **Implement `clientPingLoop()`** - periodic pings to client
4. **Modify `proxyClientToBackend()`** - intercept ping/pong, don't forward
5. **Add `backendPingLoop()`** - periodic pings to backend
6. **Modify `proxyBackendToClient()`** - intercept ping/pong from backend
7. **Update `Proxy.Run()`** - start keepalive goroutines
8. **Update `NewProxy()`** - accept and apply config

### Phase 2: Relax Shard Timeouts

1. **Update `DefaultPumpConfig()`** - increase timeouts
2. **Make configurable** via env vars (optional)
3. **Update Helm values** for ws-server

### Phase 3: Testing & Validation

1. **Unit tests** for gateway ping/pong
2. **Integration test** - verify client receives pings from gateway
3. **Loadtest** - verify connections survive >19 minutes
4. **Metrics** - add ping/pong latency tracking (optional)

---

## Testing Plan

### Unit Tests

```go
// ws/internal/gateway/proxy_test.go

func TestProxy_ClientPingPong(t *testing.T) {
    // 1. Create mock client and backend connections
    // 2. Start proxy
    // 3. Verify client receives ping within PingPeriod
    // 4. Send pong response
    // 5. Verify connection stays alive
}

func TestProxy_ClientPingTimeout(t *testing.T) {
    // 1. Create mock client that doesn't respond to pings
    // 2. Start proxy with short timeout (1s)
    // 3. Verify connection closes after PongWait
}

func TestProxy_PingPongNotForwarded(t *testing.T) {
    // 1. Create mock client and backend
    // 2. Client sends ping
    // 3. Verify gateway sends pong to client
    // 4. Verify backend does NOT receive ping
}
```

### Integration Tests

```bash
# Run loadtest for >30 minutes to verify stability
task local:build
task local:deploy
task local:loadtest:run CONNECTIONS=1 DURATION=60m

# Monitor for disconnects
kubectl logs -n odin-local -l app.kubernetes.io/name=ws-gateway -f | grep -E "(ping|pong|disconnect)"
```

### Verification Checklist

- [ ] Client receives ping from gateway (not shard)
- [ ] Ping/pong frames not forwarded to backend
- [ ] Connection survives >30 minutes
- [ ] Metrics show gateway ping/pong activity
- [ ] Shard logs show reduced ping/pong activity
- [ ] Backend connection stays alive via gateway pings

---

## Rollback Plan

If issues arise:

1. **Revert gateway changes** - remove ping/pong handling
2. **Restore shard timeouts** - back to 30s/27s or use quick fix (60s/45s)
3. **Deploy previous version** - `task local:deploy` with reverted code

---

## Metrics & Observability

### New Metrics (Optional Enhancement)

```go
// Gateway metrics
gateway_client_pings_sent_total        // Counter: pings sent to clients
gateway_client_pongs_received_total    // Counter: pongs received from clients
gateway_client_ping_timeout_total      // Counter: clients that didn't respond
gateway_backend_pings_sent_total       // Counter: pings sent to backend
gateway_ping_latency_seconds           // Histogram: ping round-trip time
```

### Logging

```go
// Debug level - enable for troubleshooting
p.logger.Debug().Msg("Sending ping to client")
p.logger.Debug().Msg("Received pong from client")
p.logger.Debug().Msg("Client ping timeout - closing connection")
```

---

## Sequence Diagrams

### Current Flow (Problematic)

```
Client          Gateway         ws-server       Shard
   |               |               |              |
   |               |               |              |---> Start ping ticker
   |               |               |              |
   |               |    (27s)      |              |
   |               |               |              |<--- Send PING
   |               |               |<--PING-------|
   |               |<----PING------|              |
   |<----PING------|               |              |
   |----PONG------>|               |              |  (8 hops!)
   |               |----PONG------>|              |
   |               |               |----PONG----->|
   |               |               |              |---> Reset deadline
   |               |               |              |
   ...             ...             ...            ...
   |               |               |              |
   |               |    (cycle 39 - latency spike)    |
   |               |               |              |<--- Send PING
   |<----PING------|               |              |
   |               |               |              |
   |               |    (>3s delay due to GC/network) |
   |               |               |              |
   |               |               |              |---> TIMEOUT! Close
```

### Target Flow (Fixed)

```
Client          Gateway         ws-server       Shard
   |               |               |              |
   |               |---> Start client ping ticker |
   |               |---> Start backend ping ticker|
   |               |               |              |
   |    (45s)      |               |              |
   |<----PING------|               |              |  Gateway pings client
   |----PONG------>|               |              |  Client responds
   |               |---> Reset client deadline    |
   |               |               |              |
   |    (20s)      |               |              |
   |               |----PING------>|              |  Gateway pings backend
   |               |<----PONG------|              |  Backend responds
   |               |---> Reset backend deadline   |
   |               |               |              |
   ...             ...             ...            ...
   |               |               |              |
   |    (latency spike - no problem!)             |
   |               |               |              |
   |<----PING------|               |              |  Local - fast
   |----PONG------>|               |              |  Client responds
   |               |---> Reset deadline (15s buffer)|
   |               |               |              |  Connection survives!
```

---

## Alternative Considered: Quick Fix

Instead of this architectural change, we could just increase shard timeouts:

```go
// pump.go
pongWait := 60 * time.Second    // Was 30s
pingPeriod := 45 * time.Second  // Was 27s (now 75% ratio = 15s buffer)
```

**Pros:**
- Simple change
- No architectural refactor

**Cons:**
- Doesn't fix root cause
- Ping still travels 8 hops
- Vulnerable to sustained latency
- Not industry standard

**Recommendation:** Implement gateway ping/pong for long-term stability. Use quick fix only as temporary measure if gateway changes take too long.

---

## References

- [WebSocket RFC 6455 - Ping/Pong](https://datatracker.ietf.org/doc/html/rfc6455#section-5.5.2)
- [AWS API Gateway WebSocket](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-overview.html)
- [gobwas/ws Documentation](https://pkg.go.dev/github.com/gobwas/ws)
- Previous analysis: `2026-02-09_PLAN_PUMP_TIMEOUT_FIX.md`
- Investigation: `2026-02-09_PLAN_19MIN_DISCONNECT_INVESTIGATION.md`

---

## Appendix: Current Code References

### Gateway Proxy (Current)
- `ws/internal/gateway/proxy.go:141-202` - proxyClientToBackend (pass-through)
- `ws/internal/gateway/proxy.go:204-262` - proxyBackendToClient (pass-through)
- `ws/internal/gateway/proxy.go:41-62` - Proxy struct (no keepalive fields)

### Shard Pump (Current)
- `ws/internal/server/pump.go:44-56` - DefaultPumpConfig (30s/27s)
- `ws/internal/server/pump.go:349-361` - Ping sending
- `ws/internal/server/pump.go:163-184` - Pong handling

### Gateway Config (Current)
- `ws/internal/shared/platform/gateway_config.go:23` - MessageTimeout (unused)
