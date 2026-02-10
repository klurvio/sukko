# Investigation: WebSocket Disconnect After ~19 Minutes

**Date:** 2026-02-09
**Status:** Findings Documented

---

## Symptom

Load test connection drops after ~19 minutes (1140 seconds) with:
- `initiated_by=client reason=read_error` (from shard's perspective)
- Messages stopped ~70 seconds before disconnect
- `messages_received` stayed at 337 from ~18min to disconnect

---

## Architecture Overview

```
Loadtest Client
    ↓ (WebSocket via port-forward)
kubectl port-forward :3006 → :30000
    ↓
ws-gateway (gobwas/ws proxy)
    ↓ (WebSocket)
ws-server ShardProxy (gobwas/ws proxy)
    ↓ (WebSocket)
Shard (pump.go - actual WebSocket handler)
```

---

## Timeout Configuration

| Component | Setting | Value |
|-----------|---------|-------|
| Shard (pump.go) | pongWait | 30 seconds |
| Shard (pump.go) | pingPeriod | 27 seconds |
| Loadtest | pongWait | 60 seconds |
| Loadtest | pingPeriod | 54 seconds |
| Gateway config | idle_timeout | 60 seconds (HTTP only) |
| Gateway config | read_timeout | 15 seconds (HTTP only) |
| Gateway config | message_timeout | Configured but **not used** |
| Kafka consumer | session_timeout | 30 seconds |

---

## Key Findings

### 1. Shard's Ping/Pong Mechanism Works
The shard (pump.go) sends pings every 27 seconds and refreshes read deadline on ANY frame (including pong). This should keep connections alive indefinitely.

### 2. Proxies Forward All Frames
Both gateway and ws-server proxies forward ALL WebSocket frames transparently, including ping/pong.

### 3. No 19-Minute Timeout Found
No explicit timeout matches 1140 seconds anywhere in the codebase.

### 4. Kafka Consumer Session Error (Later)
At 14:19:12 (18 minutes AFTER disconnect), saw:
```
UNKNOWN_MEMBER_ID: The coordinator is not aware of this member.
```
This is separate from the WebSocket disconnect but indicates Kafka consumer instability.

### 5. Gateway messageTimeout Not Used
The gateway has a `messageTimeout` config but it's stored without being applied to the proxy.

---

## Potential Causes

### 1. Port-Forward Flakiness (Most Likely)
`kubectl port-forward` is not designed for long-running, high-volume connections. It can drop connections due to:
- TCP keepalive issues
- Network hiccups
- Resource constraints

### 2. Missing TCP Keepalives in Proxies
Neither gateway nor ws-server proxy enables TCP keepalives on connections. Long-idle TCP connections can be dropped by intermediate routers/NAT.

### 3. Shard Read Deadline Expiry
If pongs don't reach the shard within 30 seconds, the read deadline expires. This could happen if:
- Proxy delays pong forwarding
- Network congestion
- CPU contention causing delays

### 4. Kafka Consumer Timing
Messages come in batches. If no messages for extended period, the only activity is ping/pong. Any disruption to ping/pong chain causes disconnect.

---

## Recommendations

### Short-Term: Test Without Port-Forward
Use NodePort directly to eliminate port-forward as variable:
```bash
# Get node IP
kubectl get nodes -o wide

# Connect directly to NodePort
ws://localhost:30000/ws  # If using Docker Desktop/kind
```

### Medium-Term: Add TCP Keepalives
Add TCP keepalives to proxy connections:

**File:** `ws/internal/gateway/proxy.go`
```go
// Enable TCP keepalive on backend connection
if tcpConn, ok := backendConn.(*net.TCPConn); ok {
    tcpConn.SetKeepAlive(true)
    tcpConn.SetKeepAlivePeriod(30 * time.Second)
}
```

### Long-Term: Add Proxy Read Deadlines
Consider adding read deadlines to proxies that get refreshed on each frame, similar to pump.go:

```go
// In proxy read loop
conn.SetReadDeadline(time.Now().Add(60 * time.Second))
// Read frame...
conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // Refresh
```

---

## Testing Plan

1. **Rerun test with direct NodePort access** (skip port-forward)
2. **Add verbose logging** to track ping/pong frames through proxies
3. **Monitor Kafka consumer** group status during test
4. **Check TCP connection state** with `netstat` or `ss`

---

## Files to Review

| File | Concern |
|------|---------|
| `ws/internal/gateway/proxy.go` | No TCP keepalive, messageTimeout unused |
| `ws/internal/server/orchestration/proxy.go` | No TCP keepalive, no read deadline |
| `ws/internal/server/pump.go` | 30s pongWait may be too aggressive |
| `wsloadtest/connection.go` | 60s pongWait vs server's 30s mismatch |

---

## Next Steps

1. Run another test connecting directly to NodePort (bypass port-forward)
2. If still fails at ~19 min, add debug logging to track ping/pong through proxies
3. If port-forward was the issue, document as known limitation for local dev
