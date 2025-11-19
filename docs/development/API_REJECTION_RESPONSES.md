# WebSocket API: Client Rejection Responses

**Last Updated:** 2025-11-19
**Branch:** fix/tcp-backlog-tuning

---

## Overview

This document describes all scenarios where client WebSocket connections are rejected, and what responses clients receive. Understanding these rejection scenarios is critical for implementing proper client-side error handling and retry logic.

## Key Concepts

### Two Response Types

**1. HTTP Error Responses** (Pre-WebSocket)
- Happen during the initial HTTP upgrade request
- Client's WebSocket connection never establishes
- Trigger `onerror` event in browser WebSocket API
- Limited error details available to client-side JavaScript (browser security)

**2. WebSocket Close Frames** (Post-WebSocket)
- Happen after WebSocket upgrade succeeds
- Client receives a close frame with code and reason
- Trigger `onclose` event with detailed `CloseEvent` object
- Includes numeric code and text reason for debugging

---

## Complete Rejection Scenarios

| # | Scenario | Component | Response Type | Status/Code | Message | Code Reference |
|---|----------|-----------|---------------|-------------|---------|----------------|
| 1 | Server Shutdown | Shard | HTTP | 503 | "Server is shutting down" | handlers_ws.go:36 |
| 2 | Rate Limit Exceeded | Shard | HTTP | 429 | "Rate limit exceeded" | handlers_ws.go:50 |
| 3 | ResourceGuard Rejection | Shard | HTTP | 503 | "Server overloaded" | handlers_ws.go:68 |
| 4 | LoadBalancer Overloaded | LoadBalancer | HTTP | 503 | "Server overloaded" | loadbalancer.go:145 |
| 5 | Shard No Slots | Proxy | WebSocket Close | 1012 | "Server overloaded" | proxy.go:83-84 |
| 6 | Backend Dial Failed | Proxy | WebSocket Close | 1011 | "Backend unavailable" | proxy.go:152-153 |

---

## Detailed Breakdown

### 1. Server Shutdown (HTTP 503)

**When:** Server is in graceful shutdown mode, rejecting new connections

**Flow:**
```
Client → LoadBalancer → Shard (checks shuttingDown flag)
                              ↓
                         HTTP 503 rejection
```

**Client Receives:**
```http
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8

Server is shutting down
```

**Code Location:** `ws/internal/shared/handlers_ws.go:32-37`

**Trigger Condition:** `atomic.LoadInt32(&s.shuttingDown) == 1`

**Client-Side Behavior:**
- WebSocket constructor throws error or triggers `onerror`
- No CloseEvent available (connection never established)
- Browser provides minimal error details

**Recommended Action:** Retry after delay (server may be restarting)

---

### 2. Rate Limit Exceeded (HTTP 429)

**When:** Client IP exceeds connection rate limit (DoS protection)

**Flow:**
```
Client → LoadBalancer → Shard (checks rate limiter)
                              ↓
                         HTTP 429 rejection
```

**Client Receives:**
```http
HTTP/1.1 429 Too Many Requests
Content-Type: text/plain; charset=utf-8

Rate limit exceeded
```

**Code Location:** `ws/internal/shared/handlers_ws.go:44-52`

**Trigger Condition:** `!s.connectionRateLimiter.CheckConnectionAllowed(clientIP)`

**Important:** Localhost (127.0.0.1) bypasses rate limiting for internal LoadBalancer traffic

**Client-Side Behavior:**
- WebSocket constructor throws error or triggers `onerror`
- No retry should be attempted immediately

**Recommended Action:** Exponential backoff (start with 5s, max 60s)

---

### 3. ResourceGuard Rejection (HTTP 503)

**When:** Server resource limits exceeded (goroutines, CPU, memory)

**Flow:**
```
Client → LoadBalancer → Shard (checks ResourceGuard)
                              ↓
                         HTTP 503 rejection
```

**Client Receives:**
```http
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8

Server overloaded
```

**Code Location:** `ws/internal/shared/handlers_ws.go:56-69`

**Trigger Condition:** `!s.resourceGuard.ShouldAcceptConnection()`

**ResourceGuard Checks:**
- Goroutine count < WS_MAX_GOROUTINES (100,000)
- CPU usage < 90%
- Memory usage < 90%
- Connection count < WS_MAX_CONNECTIONS (18,000)

**Logged Details:**
```json
{
  "level": "warn",
  "client_ip": "...",
  "current_connections": 17710,
  "max_connections": 18000,
  "reason": "goroutine limit exceeded (95000 > 100000)",
  "msg": "Connection rejected by ResourceGuard"
}
```

**Client-Side Behavior:**
- WebSocket constructor throws error or triggers `onerror`
- Server is at capacity

**Recommended Action:** Retry with exponential backoff (server may scale or shed load)

---

### 4. LoadBalancer Overloaded (HTTP 503)

**When:** All shards are at full capacity

**Flow:**
```
Client → LoadBalancer (checks all shard slots)
              ↓
         All shards full
              ↓
         HTTP 503 rejection
```

**Client Receives:**
```http
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8

Server overloaded
```

**Code Location:** `ws/internal/multi/loadbalancer.go:145`

**Trigger Condition:** No shard has available slots

**Client-Side Behavior:**
- WebSocket constructor throws error or triggers `onerror`
- System is at full capacity (all 3 shards full)

**Recommended Action:** Retry with longer backoff (30-60s)

---

### 5. Shard No Slots (WebSocket Close 1012)

**When:** LoadBalancer upgraded client successfully, but shard has no slots

**Flow:**
```
Client → LoadBalancer (upgrades to WebSocket ✓)
              ↓
         Proxy → Shard (TryAcquireSlot fails)
                      ↓
                 WebSocket Close Frame
```

**Client Receives:**
```javascript
CloseEvent {
  code: 1012,  // Service Restart
  reason: "Server overloaded",
  wasClean: true
}
```

**Code Location:** `ws/internal/multi/proxy.go:78-86`

**Trigger Condition:** `!p.shard.TryAcquireSlot()`

**Why This Happens:**
- Race condition: LoadBalancer saw available slot, but shard filled before proxy could acquire
- Normal in high-concurrency scenarios
- Rare (should be <1% of connections)

**Client-Side Behavior:**
- WebSocket connection establishes briefly, then closes
- `onopen` may fire, then `onclose` immediately
- CloseEvent provides code 1012 and reason

**Recommended Action:** Immediate retry (likely to succeed on next attempt)

---

### 6. Backend Dial Failed (WebSocket Close 1011)

**When:** LoadBalancer cannot connect to backend shard

**Flow:**
```
Client → LoadBalancer (upgrades to WebSocket ✓)
              ↓
         Proxy → Shard (dial fails)
                      ↓
                 WebSocket Close Frame
```

**Client Receives:**
```javascript
CloseEvent {
  code: 1011,  // Internal Server Error
  reason: "Backend unavailable",
  wasClean: true
}
```

**Code Location:** `ws/internal/multi/proxy.go:132-154`

**Trigger Condition:** `p.dialer.DialContext(ctx, p.backendURL.String(), nil)` fails

**Common Causes:**
- Shard crashed or restarting
- Network connectivity issue
- TCP backlog saturation (transient during burst load)
- Dial timeout exceeded (10s)

**Logged Details:**
```json
{
  "level": "error",
  "error": "websocket: bad handshake",
  "backend_url": "ws://127.0.0.1:3004/ws",
  "dial_duration_ms": 1609.467,
  "http_status": 503,
  "http_status_text": "503 Service Unavailable",
  "shard_id": 2,
  "msg": "Backend dial failed"
}
```

**Client-Side Behavior:**
- WebSocket connection establishes briefly, then closes
- `onopen` fires, then `onclose` with code 1011
- CloseEvent provides detailed reason

**Recommended Action:** Retry with backoff (may be transient issue)

---

## Client-Side Implementation Guide

### JavaScript WebSocket Client

```javascript
class RobustWebSocketClient {
  constructor(url) {
    this.url = url;
    this.ws = null;
    this.retryCount = 0;
    this.maxRetries = 10;
    this.connect();
  }

  connect() {
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      console.log('Connected');
      this.retryCount = 0; // Reset on successful connection
    };

    this.ws.onerror = (error) => {
      // HTTP upgrade failures (503, 429) trigger this
      // Browser doesn't expose detailed error info
      console.error('Connection error:', error);
      // Error event doesn't provide status code or message
      // Must rely on onclose for retry logic
    };

    this.ws.onclose = (event) => {
      console.log('Closed:', event.code, event.reason);
      this.handleClose(event);
    };

    this.ws.onmessage = (event) => {
      // Handle messages
      console.log('Message:', event.data);
    };
  }

  handleClose(event) {
    // Categorize close reasons and apply appropriate retry strategy
    switch (event.code) {
      case 1011: // Backend unavailable
        console.log('Backend unavailable, retrying...');
        this.retryWithBackoff(1000); // Quick retry (likely transient)
        break;

      case 1012: // Service restart / Server overloaded
        console.log('Server overloaded, retrying...');
        this.retryWithBackoff(2000); // Slower retry
        break;

      case 1000: // Normal closure
      case 1001: // Going away
        console.log('Connection closed normally');
        // Don't retry
        break;

      default:
        // HTTP errors (503, 429) also trigger onclose but without specific codes
        // Assume server issue and retry with backoff
        console.log('Connection failed, retrying...');
        this.retryWithBackoff(5000);
        break;
    }
  }

  retryWithBackoff(baseDelay) {
    if (this.retryCount >= this.maxRetries) {
      console.error('Max retries reached, giving up');
      return;
    }

    // Exponential backoff with jitter
    const delay = baseDelay * Math.pow(2, this.retryCount) + Math.random() * 1000;
    const maxDelay = 60000; // Cap at 60 seconds
    const actualDelay = Math.min(delay, maxDelay);

    console.log(`Retrying in ${actualDelay}ms (attempt ${this.retryCount + 1}/${this.maxRetries})`);

    this.retryCount++;
    setTimeout(() => this.connect(), actualDelay);
  }

  send(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data);
    } else {
      console.warn('WebSocket not connected, cannot send:', data);
    }
  }

  close() {
    if (this.ws) {
      this.ws.close(1000, 'Client closing');
    }
  }
}

// Usage
const client = new RobustWebSocketClient('ws://34.70.240.105:3004/ws');
client.send(JSON.stringify({action: 'subscribe', tokenId: 'BTC'}));
```

---

## Recommended Retry Strategies

### By Rejection Type

| Rejection | Code | Retry? | Initial Delay | Max Delay | Strategy |
|-----------|------|--------|---------------|-----------|----------|
| Server Shutdown | HTTP 503 | ✅ Yes | 5s | 60s | Exponential backoff |
| Rate Limit | HTTP 429 | ✅ Yes | 10s | 120s | Exponential backoff + jitter |
| ResourceGuard | HTTP 503 | ✅ Yes | 5s | 60s | Exponential backoff |
| LB Overloaded | HTTP 503 | ✅ Yes | 30s | 120s | Long exponential backoff |
| Shard No Slots | WS 1012 | ✅ Yes | 1s | 10s | Quick retry (race condition) |
| Backend Dial Failed | WS 1011 | ✅ Yes | 2s | 30s | Medium backoff (transient) |

### Exponential Backoff Formula

```javascript
// Delay = baseDelay × 2^retryCount + jitter
// Jitter = random(0, 1000ms) to prevent thundering herd
function calculateBackoff(baseDelay, retryCount, maxDelay = 60000) {
  const exponentialDelay = baseDelay * Math.pow(2, retryCount);
  const jitter = Math.random() * 1000;
  return Math.min(exponentialDelay + jitter, maxDelay);
}
```

---

## Monitoring and Debugging

### Server-Side Logs

**Grep for rejection events:**

```bash
# Rate limit rejections
docker logs odin-ws-multi 2>&1 | grep "rate limit exceeded"

# ResourceGuard rejections
docker logs odin-ws-multi 2>&1 | grep "rejected by ResourceGuard"

# Backend dial failures
docker logs odin-ws-multi 2>&1 | grep "Backend dial failed"

# Shard slot exhaustion
docker logs odin-ws-multi 2>&1 | grep "No available slots in shard"
```

### Prometheus Metrics

**Track rejection rates:**

```promql
# Connection failures by type
rate(ws_connections_failed_total[5m])

# Current connection count vs limit
ws_connections_current / ws_connections_max
```

---

## FAQ

### Q: Why do I see both HTTP 503 and WebSocket 1012 for "Server overloaded"?

**A:** The timing matters:
- **HTTP 503**: Server rejected during initial handshake (before WebSocket upgrade)
- **WebSocket 1012**: Server accepted handshake but couldn't allocate resources afterward

### Q: What's the difference between 1011 and 1012 close codes?

**A:**
- **1011 (Internal Server Error)**: Unexpected server error (backend unavailable)
- **1012 (Service Restart)**: Server is restarting or overloaded (temporary condition)

### Q: Why can't I see detailed HTTP error codes in browser JavaScript?

**A:** Browser security: WebSocket API doesn't expose HTTP response details in `onerror`. You only get a generic error event. Must rely on `onclose` event and close codes for details.

### Q: Should I retry immediately or use backoff?

**A:** Always use exponential backoff:
- Prevents thundering herd (many clients retrying at once)
- Gives server time to recover
- Reduces load during capacity issues

### Q: What's the max connection capacity?

**A:**
- **Total:** 18,000 connections (3 shards × 6,000 each)
- **Current tested:** 17,710 connections (98.4% capacity)
- **Available slots:** ~290 at peak capacity

---

## WebSocket Close Code Reference

Standard WebSocket close codes used in this system:

| Code | Name | Meaning | Usage |
|------|------|---------|-------|
| 1000 | Normal Closure | Clean disconnect | Client/server intentional close |
| 1001 | Going Away | Endpoint going away | Server shutdown, page navigation |
| 1011 | Internal Server Error | Server encountered error | Backend dial failures |
| 1012 | Service Restart | Server restarting | Overload, no slots available |

Full spec: [RFC 6455 Section 7.4](https://datatracker.ietf.org/doc/html/rfc6455#section-7.4)

---

## Related Documentation

- **Architecture:** `docs/ARCHITECTURE.md`
- **Load Testing:** `docs/sessions/SESSION_HANDOFF_2025-11-19_CAPACITY_TEST_SUCCESS.md`
- **Rate Limiting:** `ws/internal/shared/limits/connection_rate_limiter.go`
- **ResourceGuard:** `ws/internal/shared/limits/resource_guard.go`
- **LoadBalancer:** `ws/internal/multi/loadbalancer.go`
- **Proxy:** `ws/internal/multi/proxy.go`

---

**Document Version:** 1.0
**Last Reviewed:** 2025-11-19
**Maintainer:** WebSocket Server Team
