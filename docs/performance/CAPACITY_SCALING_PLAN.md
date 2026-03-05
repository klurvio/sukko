# WebSocket Server Capacity Scaling Plan

**Date:** 2025-11-16
**Status:** In Progress
**Target:** 1,000 connections/sec (from current ~100 conn/sec)
**Branch:** `fix/tcp-backlog-tuning`

---

## Executive Summary

Current capacity testing reveals the server can sustain **9,000-9,800 connections at 100 conn/sec ramp rate**, but struggles to maintain connections when ramping faster. To achieve **1,000 conn/sec** capacity required for production trading platform, we need a **multi-layered approach**:

1. **Graceful failure handling** (fix root cause first)
2. **Horizontal scaling** (proven architecture pattern)
3. **Continuous optimization** (improve single-instance performance)

**Key Insight:** Rate limiting provides beneficial backpressure. The bottleneck is connection establishment burst tolerance, not steady-state capacity.

---

## Problem Analysis

### Current Performance

| Ramp Rate | Success Rate | Active Connections | Status |
|-----------|--------------|-------------------|---------|
| 50 conn/sec | ~82% | 9,870 / 12,000 | ✅ Stable |
| 100 conn/sec | 77% | 9,250 / 12,000 | ⚠️ Degraded |
| 1000 conn/sec | Unknown | Estimated <5,000 | ❌ Projected failure |

### Root Cause Analysis

**The problem is NOT:**
- ❌ Total connection capacity (18K slots available)
- ❌ CPU overload (40% usage during tests)
- ❌ Memory exhaustion (732 MB / ∞)
- ❌ Rate limiting (proven to HELP, not hurt)
- ❌ TCP stack limits (heavily tuned)

**The problem IS:**
- ✅ **Connection establishment burst tolerance**
- ✅ **Handshake failures during rapid ramp-up**
- ✅ **23% of connections disconnect after successful creation**
- ✅ **System overwhelmed by rapid connection establishment rate**

### Evidence: Rate Limiting A/B Test

**Historical Test (Commit 3d0c309):**
```
WITH rate limiting:    82.4% success
WITHOUT rate limiting: 79.9% success
Difference:            -2.5% (300 MORE failures)
```

**Current Session Results:**
```
WITH rate limiting:    82.2% success (9,870/12,000)
WITHOUT rate limiting: 77.1% success (9,250/12,000)
Difference:            -5.1% (620 MORE failures)
```

**Conclusion:** Rate limiting provides orderly FIFO queuing that helps with burst tolerance rather than creating a bottleneck.

---

## What We Tried (And Learned)

### Attempt 1: Localhost Rate Limiting Bypass ❌

**Hypothesis:** Backend shards rate-limiting internal LoadBalancer connections (127.0.0.1) was causing capacity plateau.

**Implementation:**
- Commit `2a53ea4`: Added localhost bypass to connection rate limiter
- Deployed to GCP
- Ran capacity test

**Results:**
- Performance DEGRADED from 82.2% to 77.1%
- 620 more connection failures
- Rate limiting was actually helping!

**Action Taken:**
- Commit `ac34471`: Reverted localhost bypass
- Rate limiting now applies to ALL connections

**Key Learning:** Rate limiting acts as beneficial backpressure mechanism that prevents system overload during bursts.

### Current Understanding

**Connection Lifecycle Issues:**
1. ✅ Connections CREATE successfully (100% creation rate)
2. ❌ But 23% DISCONNECT shortly after creation
3. ⚠️ This happens during rapid ramp-up phase

**Bottleneck Location:**
- LoadBalancer → Backend WebSocket handshake
- During 100 conn/sec ramp:
  - 100 simultaneous backend dials/sec
  - 100 WebSocket handshakes/sec
  - 200 goroutines spawned/sec (readPump + writePump)
  - Kafka subscriptions for all channels
  - Memory allocation pressure → GC pressure

---

## Solution Strategy

### Phase 1: Graceful Handshake Failure Handling (1-3 Days)

**Goal:** Improve single-instance reliability from 77% to 90-95% at 100 conn/sec

#### 1.1 Multi-Shard Retry (Priority 1 - Immediate)

**Problem:** When backend handshake fails, connection is dropped.

**Solution:** Try all shards before failing.

```go
func (lb *LoadBalancer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    var lastErr error
    maxAttempts := len(lb.shards) // Try all shards

    for attempt := 0; attempt < maxAttempts; attempt++ {
        shard := lb.selectNextShard() // Round-robin

        err := lb.proxyToShard(w, r, shard)
        if err == nil {
            lb.metrics.HandshakeSuccess(shard.id)
            return // Success!
        }

        lb.logger.Warn().
            Int("shard_id", shard.id).
            Err(err).
            Int("attempt", attempt+1).
            Msg("Backend handshake failed, trying next shard")

        lastErr = err
        time.Sleep(10 * time.Millisecond) // Small delay
    }

    // All shards failed
    lb.metrics.HandshakeFailedAll()
    http.Error(w, "All backends temporarily unavailable",
               http.StatusServiceUnavailable)
}
```

**Expected Impact:**
- Shard 1 overloaded → automatically try Shard 2 or 3
- Distribute load more evenly during bursts
- Success rate: 77% → 85-90%

**Implementation:**
- File: `ws/internal/multi/loadbalancer.go`
- Add retry loop in `handleWebSocket()`
- Add metrics for success/failure per shard
- Add circuit breaker state tracking

#### 1.2 Circuit Breaker Per Shard (Priority 2)

**Problem:** Continuing to hammer failed shards wastes time and resources.

**Solution:** Temporarily skip unhealthy shards.

```go
type CircuitBreaker struct {
    state         CircuitState // Closed, Open, HalfOpen
    failures      int
    failureThresh int          // Open after N failures (e.g., 10)
    timeout       time.Duration // Retry after timeout (e.g., 30s)
}

func (lb *LoadBalancer) selectHealthyShard() *Shard {
    // Prefer closed circuits (healthy)
    for _, sc := range lb.shardsWithCircuits {
        if sc.circuit.state == CircuitClosed {
            return sc.shard
        }
    }

    // Try half-open circuits (recovery)
    for _, sc := range lb.shardsWithCircuits {
        if sc.circuit.state == CircuitHalfOpen ||
           (sc.circuit.state == CircuitOpen &&
            time.Since(sc.circuit.lastFailure) > sc.circuit.timeout) {
            return sc.shard
        }
    }

    return lb.selectLeastRecentlyFailed()
}
```

**Expected Impact:**
- Prevent cascade failures
- Faster failover to healthy shards
- Automatic recovery detection
- Success rate: 85-90% → 90-95%

**Configuration:**
```bash
# Environment variables
CIRCUIT_BREAKER_FAILURE_THRESHOLD=10     # Open after 10 failures
CIRCUIT_BREAKER_TIMEOUT=30s              # Try again after 30s
CIRCUIT_BREAKER_HALF_OPEN_MAX_CALLS=5    # Test with 5 calls
```

#### 1.3 Graceful Client Notification (Priority 3)

**Problem:** Clients get hard disconnect with no context.

**Solution:** Send WebSocket close frame with retry guidance.

```go
func (lb *LoadBalancer) notifyClientFailure(conn net.Conn, err error) {
    closeCode := ws.StatusTryAgainLater
    closeReason := "Server temporarily overloaded, please retry"

    if errors.Is(err, ErrAllShardsUnavailable) {
        closeCode = ws.StatusServiceRestart
        closeReason = "All backend servers temporarily unavailable"
    }

    closeFrame := ws.NewCloseFrameBody(closeCode, closeReason)
    ws.WriteFrame(conn, ws.NewCloseFrame(closeFrame))
    conn.Close()
}
```

**Expected Impact:**
- Better client UX
- Enables client-side retry logic
- Helpful debugging information

#### 1.4 Health-Based Shard Selection (Priority 4)

**Problem:** Round-robin doesn't account for shard load.

**Solution:** Select shard based on health score.

```go
func (lb *LoadBalancer) calculateHealthScore(shard *Shard) float64 {
    utilization := float64(shard.currentConnections) / float64(shard.maxConnections)
    recentFailureRate := float64(shard.recentFailures) / 100.0

    // Lower utilization = better
    utilizationScore := 1.0 - utilization

    // Fewer failures = better
    failureScore := 1.0 - recentFailureRate

    // Weighted average
    return (utilizationScore * 0.6) + (failureScore * 0.4)
}
```

**Expected Impact:**
- Automatic load balancing to healthiest shards
- Avoid overloaded shards proactively
- Better burst tolerance

### Phase 2: Horizontal Scaling (1 Day)

**Goal:** Achieve 1,000 conn/sec capacity

#### 2.1 Architecture

**Current (Single Instance):**
```
External Clients
    ↓
LoadBalancer :3001 (100 conn/sec max)
    ↓
3 Backend Shards :3002-3004
```

**Target (Multi-Instance):**
```
External Clients
    ↓
GCP HTTP(S) Load Balancer (Global)
    ↓
┌────────────┼────────────┐
↓            ↓            ↓
LB-1:3001   LB-2:3001   ... LB-10:3001 (each @ 100 conn/sec)
└────────────┼────────────┘
             ↓
┌────────────┼────────────┐
↓            ↓            ↓
Shard-1:3002 Shard-2:3003 Shard-3:3004 (shared backend pool)
```

**Capacity Calculation:**
- 10 LoadBalancer instances × 100 conn/sec = **1,000 conn/sec**
- With graceful handling (90% success): **900 successful conn/sec**
- With graceful handling (95% success): **950 successful conn/sec**

#### 2.2 Deployment Strategy

**Option A: Separate Tiers (Recommended)**

1. **Backend Tier:** Scale up to 7-10 shards
   - More shards = more capacity
   - Each shard: 6,000 connections
   - 10 shards = 60,000 total capacity

2. **LoadBalancer Tier:** Deploy 10 instances
   - Each runs LoadBalancer only
   - Point to shared backend shard IPs
   - Stateless (easy to scale)

3. **External Load Balancer:** GCP HTTP(S) LB
   - Health checks on :3004/health
   - Automatic failover
   - WebSocket support

**Option B: Replicate Current Setup (Simpler)**

1. Clone `sukko-go` instance 10 times
2. Each runs LoadBalancer + 3 shards
3. GCP LB distributes traffic
4. Independent shard pools per instance

**Pros:**
- Simpler deployment
- No code changes
- Fault isolation

**Cons:**
- Less efficient (30 shards total vs 10)
- Higher resource usage

#### 2.3 Implementation Steps

**Step 1: Create LoadBalancer-only mode**
```bash
# New environment variable
WS_MODE=loadbalancer  # Run LB only, no shards

# Command line
./sukko-multi --mode=loadbalancer --lb-addr=:3001 \
  --backend-urls=ws://shard-1:3002/ws,ws://shard-2:3003/ws,...
```

**Step 2: Create shard-only mode**
```bash
WS_MODE=shard  # Run shards only, no LB

./sukko-multi --mode=shard --shards=7 --base-port=3002
```

**Step 3: Deploy with GCP managed instance group**
```bash
# Create instance template for LoadBalancers
gcloud compute instance-templates create ws-loadbalancer-template \
  --machine-type=e2-medium \
  --image-family=cos-stable \
  --metadata=startup-script='...'

# Create managed instance group (auto-scaling)
gcloud compute instance-groups managed create ws-loadbalancer-group \
  --template=ws-loadbalancer-template \
  --size=10 \
  --zone=us-central1-a

# Create HTTP(S) load balancer
gcloud compute backend-services create ws-backend \
  --protocol=HTTP \
  --health-checks=ws-health-check \
  --global

# Add instance group to backend
gcloud compute backend-services add-backend ws-backend \
  --instance-group=ws-loadbalancer-group \
  --instance-group-zone=us-central1-a \
  --global
```

### Phase 3: Continuous Optimization (Ongoing)

**Goal:** Improve single-instance performance beyond 100 conn/sec

#### 3.1 Connection Establishment Optimizations

**Areas to profile:**
1. WebSocket handshake latency
2. Goroutine creation overhead
3. Kafka subscription delays
4. Memory allocation patterns

**Potential Improvements:**
- Goroutine pooling (reuse instead of create)
- Connection pooling to backends
- Async Kafka subscription
- Pre-allocated buffers

**Target:** 150-200 conn/sec per LoadBalancer instance

#### 3.2 Resource Optimization

**Current Limits:**
```
File descriptors: 1,048,576
somaxconn: 65,535
tcp_max_syn_backlog: 65,535
```

**Monitor:**
- GC pause times
- Goroutine count
- Memory allocation rate
- CPU scheduler latency

#### 3.3 Network Optimization

**Test:**
- TCP_NODELAY for WebSocket
- SO_REUSEPORT for listener
- Larger send/receive buffers
- Connection keep-alive tuning

---

## Metrics and Monitoring

### Key Metrics to Track

**Connection Metrics:**
```
ws_handshake_attempts_total{shard_id}       # Total attempts
ws_handshake_success_total{shard_id}        # Successful
ws_handshake_failures_total{shard_id}       # Failed
ws_handshake_duration_seconds{shard_id}     # Latency
ws_handshake_retry_count{attempt}           # Retry distribution
```

**Circuit Breaker Metrics:**
```
ws_circuit_breaker_state{shard_id,state}    # Closed/Open/HalfOpen
ws_circuit_breaker_transitions_total{shard_id,from,to}
ws_circuit_breaker_failures_total{shard_id}
```

**Health Metrics:**
```
ws_shard_health_score{shard_id}             # 0.0-1.0
ws_shard_utilization{shard_id}              # Percentage
ws_shard_recent_failures{shard_id}          # Count
```

### Alerting Thresholds

```yaml
# High failure rate
alert: HighHandshakeFailureRate
expr: rate(ws_handshake_failures_total[5m]) > 0.15
severity: warning

# All circuits open
alert: AllCircuitsOpen
expr: sum(ws_circuit_breaker_state{state="open"}) == count(ws_circuit_breaker_state)
severity: critical

# Low health score
alert: ShardUnhealthy
expr: ws_shard_health_score < 0.3
severity: warning
```

---

## Testing Plan

### Test 1: Baseline (Current Performance)

**Configuration:**
- 1 LoadBalancer instance
- 3 backend shards
- Rate limiting enabled
- No graceful handling

**Test:**
```bash
task gcp:load-test:capacity
# Target: 12,000 connections @ 100 conn/sec
```

**Expected:** ~9,250 active (77%)

### Test 2: Multi-Shard Retry

**Configuration:**
- Add retry logic to LoadBalancer
- Try all 3 shards before failing

**Test:**
```bash
task gcp:load-test:capacity
```

**Expected:** ~10,200-10,800 active (85-90%)

### Test 3: Circuit Breaker

**Configuration:**
- Add circuit breaker per shard
- Failure threshold: 10
- Timeout: 30s

**Test:**
```bash
task gcp:load-test:capacity
```

**Expected:** ~10,800-11,400 active (90-95%)

### Test 4: Horizontal Scaling

**Configuration:**
- 10 LoadBalancer instances
- GCP HTTP(S) Load Balancer
- All graceful handling enabled

**Test:**
```bash
# From load test instance, target GCP LB IP
WS_URL=ws://<GCP_LB_IP>/ws \
TARGET_CONNECTIONS=10000 \
RAMP_RATE=1000 \
./loadtest
```

**Expected:** ~9,000-9,500 active (90-95% of 10,000)

---

## Rollout Plan

### Week 1: Graceful Handling

**Day 1-2: Multi-Shard Retry**
- Implement retry logic
- Add metrics
- Deploy to staging
- Run capacity tests
- Deploy to production

**Day 3-4: Circuit Breaker**
- Implement circuit breaker
- Add health scoring
- Deploy to staging
- Run load tests
- Deploy to production

**Day 5: Monitoring & Tuning**
- Set up Grafana dashboards
- Configure alerts
- Fine-tune thresholds
- Document runbooks

### Week 2: Horizontal Scaling

**Day 1-2: Architecture Separation**
- Implement loadbalancer-only mode
- Implement shard-only mode
- Test locally
- Deploy to staging

**Day 3: GCP Infrastructure**
- Create instance templates
- Create managed instance groups
- Configure health checks
- Set up HTTP(S) load balancer

**Day 4: Deployment**
- Deploy backend tier (shards)
- Deploy loadbalancer tier (10 instances)
- Verify health checks
- Run smoke tests

**Day 5: Load Testing**
- Progressive load test: 200, 500, 1000 conn/sec
- Monitor all metrics
- Verify failover behavior
- Performance tuning

---

## Success Criteria

### Phase 1 Success (Graceful Handling)

- ✅ Handshake success rate ≥ 90% at 100 conn/sec
- ✅ Circuit breakers functioning (state transitions observed)
- ✅ Retry logic working (attempts logged per shard)
- ✅ Health-based selection active (traffic to healthy shards)
- ✅ Metrics dashboard showing all data

### Phase 2 Success (Horizontal Scaling)

- ✅ 10 LoadBalancer instances running
- ✅ GCP HTTP(S) LB distributing traffic evenly
- ✅ Health checks passing (all instances green)
- ✅ Capacity: 900-950 successful connections at 1000 conn/sec
- ✅ Failover working (kill 1 instance, traffic redistributes)

### Phase 3 Success (Optimization)

- ✅ Single instance performance: 150-200 conn/sec
- ✅ Total capacity: 1,500-2,000 conn/sec (10 instances)
- ✅ Handshake latency: p99 < 100ms
- ✅ Resource utilization: CPU < 70%, Memory stable

---

## Risk Mitigation

### Risk 1: Circuit Breaker Too Aggressive

**Symptom:** Shards marked unhealthy too quickly, traffic concentrated on few shards.

**Mitigation:**
- Increase failure threshold (10 → 20)
- Increase timeout (30s → 60s)
- Monitor `ws_circuit_breaker_transitions_total`

### Risk 2: GCP LB WebSocket Support Issues

**Symptom:** Connections work directly but fail through GCP LB.

**Mitigation:**
- Use TCP load balancer instead of HTTP(S)
- Configure WebSocket-specific settings
- Test with small load first

### Risk 3: Backend Shard Capacity Insufficient

**Symptom:** 10 LoadBalancers saturate backend shards.

**Mitigation:**
- Scale shards: 3 → 7 or 10
- Monitor shard utilization
- Add auto-scaling for shard tier

### Risk 4: Performance Regression

**Symptom:** Retry logic overhead reduces single-instance performance.

**Mitigation:**
- Profile before/after
- Optimize retry path
- Consider async retry for edge cases

---

## Related Documents

- **Previous Session:** [SESSION_HANDOFF_2025-11-16_RATE_LIMIT_FIX_DEPLOYED.md](sessions/SESSION_HANDOFF_2025-11-16_RATE_LIMIT_FIX_DEPLOYED.md)
- **TCP Tuning:** [TCP_TUNING_IMPLEMENTATION.md](TCP_TUNING_IMPLEMENTATION.md)
- **Slot Leak Fix:** [SESSION_HANDOFF_2025-11-16_SLOT_LEAK_FIX_SUCCESS.md](sessions/SESSION_HANDOFF_2025-11-16_SLOT_LEAK_FIX_SUCCESS.md)

---

**Document Version:** 1.0
**Last Updated:** 2025-11-16
**Owner:** Engineering Team
**Status:** Active Development
