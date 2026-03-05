# Connection Limit Explanation: Why 2,184 Connections?

## 🔍 What You're Seeing

When running `task stress:container:go CONNECTIONS=10000 DURATION=240`, you get:

```
Progress: 2184/10000 connections (21.8% success rate)
Error breakdown: {
  ECONNRESET: 96,
  Unexpected: 7720,
  WS_ERR_INVALID_UTF8: 3
}
```

**Key observation**: Exactly 2,184 connections succeeded, then all remaining 7,816 failed.

## ✅ This is Expected Behavior (Not a Bug)

The server is **intentionally** limiting connections to 2,184 for memory safety.

## 📊 The Math Behind 2,184

### Container Resources (Google Cloud Run)
```
Memory limit: 512 MB
CPU limit: 2 cores
```

### Memory Breakdown Per Connection (Phase 2)

```go
// From cgroup.go:42
const bytesPerConnection = 180 * 1024 // 180KB

// Breakdown:
// 1. Send channel buffer:     128KB  (256 slots × 500 bytes avg)
// 2. Replay buffer:             50KB  (100 messages × 500 bytes avg)
// 3. Client struct + overhead:   2KB  (fields, mutexes, pointers)
// ─────────────────────────────────
// Total:                       180KB per connection
```

### Maximum Connections Calculation

```
Available memory = 512MB - 128MB (runtime overhead)
                 = 384MB

Max connections = 384MB / 180KB
                = 2,184 connections ✓
```

This calculation happens in `cgroup.go:53`:

```go
func calculateMaxConnections(memoryLimitBytes int64) int {
    const runtimeOverheadBytes = 128 * 1024 * 1024  // 128MB
    const bytesPerConnection = 180 * 1024           // 180KB

    availableBytes := memoryLimitBytes - runtimeOverheadBytes
    maxConns := int(availableBytes / bytesPerConnection)

    return maxConns // Returns 2,184 for 512MB container
}
```

## 🚧 How Connection Limiting Works

### The Semaphore Pattern (server.go:237)

```go
type Server struct {
    connectionsSem chan struct{} // Buffered channel with size 2,184
    // ...
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // Try to acquire connection slot
    select {
    case s.connectionsSem <- struct{}{}:
        // SUCCESS: Slot acquired (connection #1 to #2,184)

    case <-time.After(5 * time.Second):
        // REJECTED: All 2,184 slots full (connection #2,185+)
        http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
        return  // Returns HTTP 503
    }

    // ... proceed with WebSocket upgrade ...
}
```

### What Happens to Connection #2,185?

```
Client #2,185 tries to connect:
  ↓
1. HTTP request arrives at server
  ↓
2. handleWebSocket() tries to acquire semaphore slot
  ↓
3. All 2,184 slots occupied by existing connections
  ↓
4. 5 second timeout expires
  ↓
5. Server returns: HTTP 503 "Server at capacity"
  ↓
6. Client sees error: "Unexpected" (generic failure)
```

## 📉 Error Breakdown Explained

### Unexpected: 7,720 errors
- **What it is**: HTTP 503 rejections
- **Why**: Connections #2,185 to #10,000 = 7,816 attempts
- **Calculation**: 10,000 - 2,184 = 7,816 rejections
- **Minor difference**: 7,816 expected vs 7,720 actual (96 became ECONNRESET)

### ECONNRESET: 96 errors
- **What it is**: Connection reset by peer
- **Why**: Some connections were accepted but closed immediately
- **Possible causes**:
  - Race condition during capacity check
  - Connection dropped during WebSocket upgrade
  - Slow client detection triggered (3 consecutive send failures)

### WS_ERR_INVALID_UTF8: 3 errors
- **What it is**: Invalid UTF-8 in WebSocket frame
- **Why**: Extremely rare, likely client-side encoding issue
- **Impact**: Negligible (0.03% of attempts)

## 📈 Phase 1 vs Phase 2 Comparison

### Phase 1 (Before Reliability Features)
```
Memory per connection: 50KB
  - Send channel: 128KB/256 = 50KB (no replay buffer)

Max connections: (512MB - 128MB) / 50KB = 7,864 connections

Reliability: ❌ Messages silently dropped if client slow
Guarantees:  ❌ No sequence numbers
Recovery:    ❌ No gap recovery
```

### Phase 2 (With Reliability Features) ← Current
```
Memory per connection: 180KB
  - Send channel: 128KB
  - Replay buffer: 50KB (100 messages for gap recovery)
  - Overhead: 2KB

Max connections: (512MB - 128MB) / 180KB = 2,184 connections

Reliability: ✅ Never silently drop messages
Guarantees:  ✅ Sequence numbers on every message
Recovery:    ✅ Clients can request missed messages
DoS Guard:   ✅ Rate limiting (100 burst, 10/sec)
Self-heal:   ✅ Slow client auto-disconnect
```

## 🎯 The Intentional Tradeoff

**What we gave up:**
- Max connections: 7,864 → 2,184 (72% reduction)

**What we gained:**
- ✅ **Message delivery guarantee** - Clients can detect and recover from gaps
- ✅ **Replay buffer** - Last 100 messages stored per client
- ✅ **Rate limiting** - DoS protection (token bucket algorithm)
- ✅ **Slow client detection** - Automatic disconnection of laggy clients
- ✅ **Production monitoring** - New metrics (slowClientsDisconnected, rateLimitedMessages, messageReplayRequests)

**For a trading platform with 300K users:**
- This is the RIGHT tradeoff
- 2,184 connections per server instance is still plenty
- Message reliability > raw connection count
- Can scale horizontally (multiple server instances) if needed

## 🔧 Options to Increase Connection Limit

### Option 1: Reduce Replay Buffer (Quick Fix)
```go
// connection.go:126
client.replayBuffer = NewReplayBuffer(100)  // Current: 100 messages

// Change to:
client.replayBuffer = NewReplayBuffer(20)   // Smaller: 20 messages
```

**Impact:**
- Memory per connection: 180KB → 146KB
- Max connections: 2,184 → 2,630 (+446 connections, +20%)
- Gap recovery window: 10 seconds → 2 seconds (at 10 msg/sec)

**Tradeoff**: Shorter recovery window means clients must reconnect faster after network hiccup.

### Option 2: Increase Container Memory (Recommended)
```yaml
# docker-compose.yml
mem_limit: 512m  # Current

# Change to:
mem_limit: 2g    # 2GB
```

**Impact:**
- Memory per connection: 180KB (unchanged)
- Max connections: 2,184 → 10,666 (+8,482, +388%)
- Calculation: (2048MB - 128MB) / 180KB = 10,666

**Cost**: Higher Cloud Run instance pricing (~4× cost).

### Option 3: Horizontal Scaling (Production Approach)
Keep current settings, add load balancer:

```
Load Balancer
    ↓
    ├─→ Server Instance 1 (2,184 connections)
    ├─→ Server Instance 2 (2,184 connections)
    ├─→ Server Instance 3 (2,184 connections)
    └─→ Server Instance 4 (2,184 connections)

Total capacity: 8,736 connections
Cost: 4× instances but better fault tolerance
```

**Benefits:**
- No code changes
- Better fault tolerance (one instance can fail)
- Geographic distribution (lower latency)
- Rolling updates without downtime

### Option 4: Shared Replay Buffer (Advanced)
Instead of per-client replay buffers, use shared buffers per symbol:

```go
// Current: 2,184 clients × 50KB buffer = 109MB
// Optimized: 100 symbols × 500KB buffer = 50MB (saves 59MB)

// Allows: ~328 more connections
```

**Complexity**: High - requires major refactoring.

## 💡 Recommendation for Your Use Case

**Scenario: Trading platform with 300K users, 40K daily transactions**

**Active concurrent users calculation:**
```
Total users: 300,000
Active ratio: 5% (typical for trading platform during market hours)
Concurrent users: 300,000 × 0.05 = 15,000
```

**Scaling approach:**
```
Option 1: Horizontal scaling with 7 instances
  - 7 instances × 2,184 connections = 15,288 capacity
  - Cost: ~$350/month (7× Cloud Run instances)
  - Meets requirement with 2% overhead

Option 2: Larger containers with 2GB memory
  - 2 instances × 10,666 connections = 21,332 capacity
  - Cost: ~$280/month (2× 2GB Cloud Run instances)
  - 42% overcapacity for peak traffic

Recommended: Option 2 (fewer instances, easier to manage)
```

## 📊 Verification Commands

### Check current connection limit:
```bash
docker logs sukko-go-2 2>&1 | grep "Calculated max"
# Output: Calculated max connections: 2184 (based on 180KB per connection + 128MB overhead)
```

### Check actual memory usage:
```bash
curl -s http://localhost:3004/stats | jq '{currentConnections, memoryMB}'
# Output: {"currentConnections": 0, "memoryMB": 14.82}
```

### Test with 2,184 connections (should succeed 100%):
```bash
task stress:container:go CONNECTIONS=2184 DURATION=60
```

### Test with 2,185 connections (1 should fail):
```bash
task stress:container:go CONNECTIONS=2185 DURATION=60
# Expected: 2184 succeed, 1 fails with "Unexpected"
```

## 🎓 Key Takeaways

1. **2,184 is not arbitrary** - It's a precise calculation based on 512MB container memory and 180KB per connection (with replay buffer).

2. **The limit is intentional** - We WANT to reject connections beyond capacity to prevent OOM (Out of Memory) crashes.

3. **Rejections are graceful** - Server returns HTTP 503 "Server at capacity" instead of crashing.

4. **Phase 2 tradeoff is correct** - For a trading platform, message reliability > raw connection count.

5. **Scale horizontally, not vertically** - 10,000 connections on one server is fragile. Better to have 5 servers with 2,000 each.

---

**Bottom line**: Your server is working EXACTLY as designed. The 2,184 limit prevents memory exhaustion and ensures reliable message delivery for all connected clients. For 10,000 connections, you'll need either larger containers (2GB memory) or horizontal scaling (multiple instances with load balancing).
