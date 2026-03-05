# Instance Isolation Plan V2: Testing Architecture for 10K Capacity

## Document Info

- **Version**: 2.0
- **Date**: 2025-10-11
- **Supersedes**: [INSTANCE_ISOLATION_PLAN.md](./INSTANCE_ISOLATION_PLAN.md)
- **Purpose**: **Testing architecture** to validate 10K connection capacity
- **Target Capacity**: 10,000 connections per ws-go instance
- **Instance Type**: e2-standard-2 (2 vCPU, 8 GB RAM)

## Executive Summary

This plan details the **isolated testing architecture** for validating WebSocket server capacity using **e2-standard-2** instances to achieve **10,000 concurrent connections** with **subscription filtering**.

**Purpose**: Create a clean testing environment where:
- ws-go runs on dedicated instance (no resource interference)
- Test runner runs on separate instance (eliminates client bottlenecks)
- Backend services isolated (no metric pollution)
- Accurate measurement of ws-go's true capacity

**Key Changes from V1:**
- ✅ **Instance upgrade**: e2-medium → e2-standard-2 (+4GB RAM) for ws-go
- ✅ **NEW: Subscription filtering** implementation (critical for performance)
- ✅ **Capacity target**: 10,000 connections (realistic with current 0.7MB/conn)
- ✅ **Testing-focused**: Clean metrics, isolated resources, accurate benchmarks
- ✅ **Based on real data**: 0.7MB/conn, CPU measurements from actual tests

## Objective

**Create isolated testing infrastructure** where ws-go runs on dedicated e2-standard-2 instance to accurately measure capacity, performance, and resource usage at 10,000 concurrent connections.

---

## Architecture Overview

### Current State Analysis (from Testing)

**Test Results** (e2-medium without subscription filtering):
```
5K connections + 20 msg/sec publisher:
- Success rate: 52.5% (2,625/5,000)
- CPU: 99.5% (rejection threshold exceeded)
- Memory: 0.7 MB per connection
- Writes/sec: 52,000 (20 msg/sec × 2,600 connections)
- CPU cost: 0.0000365 cores per write

5K connections without publisher:
- Success rate: 100% (5,000/5,000)
- CPU: 0.5-6%
- Memory: 3,500 MB total
- Bottleneck: Memory (3,584 MB limit)
```

**Root Cause**: No subscription filtering → every NATS message broadcasts to ALL clients

**Solution**: Implement subscription filtering + upgrade to e2-standard-2

---

## Testing Architecture (3-Instance Setup)

```
┌─────────────────────────────────────────────────────────────────────┐
│                  GCP Testing Environment (us-central1-a)             │
│                                                                      │
│  ┌──────────────────────────┐          ┌─────────────────────────┐ │
│  │  sukko-go              │          │  sukko-backend           │ │
│  │  (e2-standard-2)         │          │  (e2-small)             │ │
│  │  2 vCPU, 8 GB RAM        │          │  2 vCPU, 2 GB RAM       │ │
│  │                          │          │                         │ │
│  │  ┌────────────────────┐ │          │  ┌──────────────────┐  │ │
│  │  │ ws-go ONLY         │ │◄─NATS────┤  │ NATS (JetStream) │  │ │
│  │  │ 1.9 CPU, 7 GB      │ │  4222    │  │ 0.5 CPU, 256M    │  │ │
│  │  │                    │ │          │  └──────────────────┘  │ │
│  │  │ TARGET:            │ │          │                         │ │
│  │  │ 10K connections    │ │          │  ┌──────────────────┐  │ │
│  │  │ SUBSCRIPTION       │ │  metrics │  │ Prometheus       │  │ │
│  │  │ FILTERING ✅       │ ├─────────►│  │ (scrapes ws-go)  │  │ │
│  │  └────────────────────┘ │  3004    │  │ 0.3 CPU, 256M    │  │ │
│  │                          │          │  └──────────────────┘  │ │
│  │  ┌────────────────────┐ │          │                         │ │
│  │  │ Promtail           │ ├─logs────►│  ┌──────────────────┐  │ │
│  │  │ 0.1 CPU, 256M      │ │  3100    │  │ Loki             │  │ │
│  │  └────────────────────┘ │          │  │ 0.2 CPU, 256M    │  │ │
│  └──────────────────────────┘          │  └──────────────────┘  │ │
│         ▲                               │                         │ │
│         │                               │  ┌──────────────────┐  │ │
│         │ internal VPC                  │  │ Grafana          │  │ │
│         │ (low latency)                 │  │ (monitoring UI)  │  │ │
│         │                               │  │ 0.2 CPU, 512M    │  │ │
│         │                               │  └──────────────────┘  │ │
│         │                               │         ▲               │ │
│         │                               │         │ http://:3010  │ │
│  ┌──────┴───────────────────────────┐  │         │               │ │
│  │  sukko-test-runner                │  │  ┌──────────────────┐  │ │
│  │  (e2-micro)                      │  │  │ Publisher        │  │ │
│  │  0.25-2 vCPU burstable, 1GB RAM  │  │  │ (test traffic)   │  │ │
│  │                                   │  │  │ 0.2 CPU, 128M    │  │ │
│  │  ┌─────────────────────────────┐ │  │  └──────────────────┘  │ │
│  │  │ Load Test Container         │ │  └─────────────────────────┘ │
│  │  │ (Dockerized)                │ │              ▲                │
│  │  │                             │ │              │ http://:3003   │
│  │  │ • test-connection-rate.cjs  │ │              │                │
│  │  │ • test-message-throughput   │ │                               │
│  │  │ • sustained-load-test       │ │                               │
│  │  │                             │ │                               │
│  │  │ Runs 10K concurrent WS      │ │                               │
│  │  │ connections to ws-go        │ │                               │
│  │  └─────────────────────────────┘ │                               │
│  └──────────────────────────────────┘                               │
│                                                                      │
│                            Monitoring                                │
│                                 │                                    │
│                                 ▼                                    │
│                        http://:3010/grafana                          │
│                        (clean ws-go metrics)                         │
└─────────────────────────────────────────────────────────────────────┘

ISOLATION BENEFITS:
✅ ws-go: Full resources, no interference → accurate capacity measurement
✅ test-runner: Dedicated instance → eliminates local client bottlenecks
✅ backend: Separate instance → no metric pollution from monitoring services
✅ Internal VPC: <1ms latency → realistic production-like networking
```

---

## Resource Calculations

### ws-go Instance (e2-standard-2)

**Total Resources**: 2 vCPU, 8 GB RAM (8,192 MB)

**Allocation**:
```yaml
ws-go:
  CPU: 1.9 cores (95%)
  Memory: 7,168 MB (7 GB, 87.5%)

promtail:
  CPU: 0.1 cores (5%)
  Memory: 256 MB (3%)

system_overhead:
  CPU: ~0.0 cores
  Memory: ~768 MB (OS, Docker, buffers)
```

**Capacity Analysis**:

```
Memory Limit:
  Available: 7,168 MB
  Per connection: 0.7 MB (measured from tests)
  Max connections: 7,168 / 0.7 = 10,240 connections ✅

CPU Limit (with subscription filtering):
  Available CPU: 1.9 cores @ 75% threshold = 1.425 cores
  Cost per write: 0.0000365 cores (measured)

  Scenario 1: Average load (50% home page users)
    Avg subscriptions: (30 + 1) / 2 = 15.5 tokens per user
    Tokens in system: 200 (assumed)
    Subscribers per token: (10,000 × 15.5) / 200 = 775
    Message rate: 12 msg/sec
    Writes/sec: 12 × 775 = 9,300 writes/sec
    CPU used: 9,300 × 0.0000365 = 0.34 cores (17.8%) ✅

  Scenario 2: Peak load (80% home page users)
    Avg subscriptions: (30 × 0.8) + (1 × 0.2) = 24.2 tokens
    Subscribers per token: (10,000 × 24.2) / 200 = 1,210
    Writes/sec: 12 × 1,210 = 14,520 writes/sec
    CPU used: 14,520 × 0.0000365 = 0.53 cores (27.9%) ✅

RESULT: 10,000 connections supported with headroom ✅
```

### Backend Instance (e2-small - unchanged)

**Total Resources**: 2 vCPU, 2 GB RAM

**Services**: NATS, Publisher, Prometheus, Grafana, Loki, Promtail
**CPU Allocation**: 1.5 cores (75%)
**Memory Allocation**: 1,536 MB (75%)

*No changes from V1 - backend is not a bottleneck*

---

## Subscription Filtering Implementation

### Architecture

**Current Behavior** (NO FILTERING):
```go
// server.go:717 - Broadcasts to EVERYONE
func (s *Server) broadcast(message []byte) {
    s.clients.Range(func(key, value interface{}) bool {
        client.send <- data  // ❌ Sends to ALL clients
        return true
    })
}
```

**Target Behavior** (WITH FILTERING):
```go
// server.go - New subscription-aware broadcast
func (s *Server) broadcast(subject string, message []byte) {
    // Extract channel from NATS subject: "sukko.token.BTC.trade" → "BTC.trade"
    channel := extractChannel(subject)

    s.clients.Range(func(key, value interface{}) bool {
        client, ok := key.(*Client)
        if !ok {
            return true
        }

        // ✅ Only send if client subscribed to this channel
        if client.subscriptions.Has(channel) {
            envelope, _ := WrapMessage(message, "price:update", PRIORITY_HIGH, client.seqGen)
            client.replayBuffer.Add(envelope)
            data, _ := envelope.Serialize()

            select {
            case client.send <- data:
                // Sent successfully
            default:
                // Client slow - handle backpressure
            }
        }
        return true
    })
}
```

### Data Structures

**Add to Client struct** (src/client.go):
```go
type Client struct {
    // ... existing fields ...

    // NEW: Subscription management
    subscriptions     *SubscriptionSet  // Thread-safe set of channels
    subscriptionMutex sync.RWMutex      // Protects subscription operations
}

// Thread-safe set implementation
type SubscriptionSet struct {
    channels map[string]struct{}
    mu       sync.RWMutex
}

func (s *SubscriptionSet) Add(channel string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.channels[channel] = struct{}{}
}

func (s *SubscriptionSet) Remove(channel string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.channels, channel)
}

func (s *SubscriptionSet) Has(channel string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    _, exists := s.channels[channel]
    return exists
}
```

### WebSocket Message Protocol

**Client → Server (Subscribe)** - Hierarchical format `{SYMBOL}.{EVENT_TYPE}`:
```json
{
  "type": "subscribe",
  "channels": ["BTC.trade", "ETH.trade", "SOL.liquidity"]
}
```

**Client → Server (Unsubscribe)**:
```json
{
  "type": "unsubscribe",
  "channels": ["BTC.trade"]
}
```

**Server → Client (Subscription Acknowledgment)**:
```json
{
  "type": "subscription_ack",
  "subscribed": ["BTC.trade", "ETH.trade", "SOL.liquidity"],
  "count": 3
}
```

### Implementation Steps

**Phase 1: Core Subscription Logic** (2-3 hours)

1. **Add SubscriptionSet to Client**:
   - Modify `src/client.go`: Add subscription fields
   - Initialize subscriptions in `NewClient()`

2. **Implement subscribe/unsubscribe handlers**:
   - Modify `src/client.go:readPump()`: Parse subscription messages
   - Add `handleSubscribe()` and `handleUnsubscribe()` methods

3. **Update broadcast to check subscriptions**:
   - Modify `src/server.go:broadcast()`: Add channel filtering
   - Add `extractChannel()` helper to parse NATS subject

**Phase 2: Message Handlers** (1-2 hours)

4. **Parse incoming WebSocket messages**:
```go
// In readPump()
var msg struct {
    Type     string   `json:"type"`
    Channels []string `json:"channels"`
}
json.Unmarshal(messageData, &msg)

switch msg.Type {
case "subscribe":
    c.handleSubscribe(msg.Channels)
case "unsubscribe":
    c.handleUnsubscribe(msg.Channels)
case "heartbeat":
    c.handleHeartbeat()
}
```

5. **Send acknowledgments**:
```go
func (c *Client) handleSubscribe(channels []string) {
    for _, ch := range channels {
        c.subscriptions.Add(ch)
    }

    ack := SubscriptionAck{
        Type:       "subscription_ack",
        Subscribed: channels,
        Count:      c.subscriptions.Count(),
    }
    c.sendJSON(ack)
}
```

**Phase 3: NATS Subject Parsing** (1 hour)

6. **Extract channel from NATS subject**:
```go
// Extract "BTC.trade" from "sukko.token.BTC.trade"
func extractChannel(subject string) string {
    parts := strings.Split(subject, ".")
    if len(parts) >= 4 {
        return parts[2] + "." + parts[3]  // Return symbol.event_type
    }
    return ""
}
```

7. **Update NATS message handler**:
```go
// In handleNATSMessage()
msg, err := subscription.NextMsg(1 * time.Second)
if err == nil {
    subject := msg.Subject  // e.g., "sukko.token.BTC.trade"
    s.broadcast(subject, msg.Data)  // Pass subject to broadcast
}
```

**Phase 4: Testing & Validation** (2-3 hours)

8. **Unit tests**:
   - Test SubscriptionSet thread safety
   - Test subscribe/unsubscribe message parsing
   - Test channel extraction from NATS subjects

9. **Integration tests**:
   - Connect 100 clients with varying subscription patterns
   - Publish to specific channels
   - Verify only subscribed clients receive messages

10. **Load test**:
    - 5,000 connections with subscription filtering
    - Expected: 100% success vs 52.5% without filtering
    - Monitor CPU usage (should be <30% vs 99% before)

**Total Estimated Time**: 6-9 hours

---

## Configuration Changes

### ws-go docker-compose.yml

**Update**: `isolated/ws-go/docker-compose.yml`

```yaml
services:
  ws-go:
    build:
      context: ./src
      dockerfile: Dockerfile
    container_name: sukko-go
    ports:
      - "0.0.0.0:3004:3002"
    command:
      - "./sukko-server"
      - "-addr"
      - ":3002"
      - "-nats"
      - "nats://${BACKEND_INTERNAL_IP}:4222"
    environment:
      # Resource limits (e2-standard-2: 2 vCPU, 8GB RAM)
      # Allocation: 1.9 CPU, 7GB (leaves 0.1 CPU + 256M for Promtail)
      - WS_CPU_LIMIT=1.9
      - WS_MEMORY_LIMIT=7516192768  # 7 GB in bytes (7168 MB)

      # Capacity: 10,000 connections (realistic with 0.7MB per conn)
      - WS_MAX_CONNECTIONS=10000

      # Worker pool sizing
      # Formula: max(32, connections/40) = max(32, 10000/40) = 250 → 256 (power of 2)
      # Load per worker: (10000 × 12) / 256 = 468 msg/sec per worker
      - WS_WORKER_POOL_SIZE=256
      - WS_WORKER_QUEUE_SIZE=25600  # 100x workers

      # Rate limiting (safety valves)
      # With filtering: ~1,210 avg subscribers per message
      # 12 msg/sec × 1,210 = 14,520 writes/sec
      - WS_MAX_NATS_RATE=1000
      - WS_MAX_BROADCAST_RATE=1000

      # Goroutine limit
      # Formula: ((10000 × 2) + 256 + 13) × 1.2 = 24,323
      # Rounded: 25,000
      - WS_MAX_GOROUTINES=25000

      # Safety thresholds
      - WS_CPU_REJECT_THRESHOLD=75.0
      - WS_CPU_PAUSE_THRESHOLD=80.0

      # Logging
      - LOG_LEVEL=info
      - LOG_FORMAT=json

    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.9"
          memory: 7168M
    ulimits:
      nofile:
        soft: 200000
        hard: 200000

  promtail:
    image: grafana/promtail:3.3.2
    container_name: sukko-promtail
    volumes:
      - ./promtail-config.yml:/etc/promtail/promtail-config.yml
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock
    command: -config.file=/etc/promtail/promtail-config.yml
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.1"
          memory: 256M
```

### Taskfile Updates

**Update**: `taskfiles/isolated-setup.yml` line 28

```yaml
vars:
  # Machine types
  WS_GO_MACHINE_TYPE: e2-standard-2  # Changed from e2-medium
  BACKEND_MACHINE_TYPE: e2-small
  TEST_RUNNER_MACHINE_TYPE: e2-micro
```

---

## Migration Path

### Option A: In-Place Upgrade (Requires Downtime)

**Steps**:
1. Stop current ws-go container: `task -t taskfiles/isolated-setup.yml stop:ws-go`
2. Delete e2-medium instance: `gcloud compute instances delete sukko-go --zone=us-central1-a`
3. Create e2-standard-2 instance: Update taskfile, run `task create:ws-go`
4. Deploy updated code with subscription filtering: `task deploy:ws-go`
5. Test capacity: `task test:remote:capacity:10k`

**Downtime**: ~5-10 minutes

### Option B: Blue-Green Deployment (Zero Downtime)

**Steps**:
1. Create new instance `sukko-go-v2` (e2-standard-2)
2. Deploy code with subscription filtering
3. Test new instance thoroughly
4. Switch DNS/load balancer to new instance
5. Monitor for issues
6. Delete old instance once stable

**Downtime**: 0 minutes (recommended for production)

### Option C: Rolling Deployment (Multiple Instances)

**Steps**:
1. Create 2× e2-standard-2 instances for 20K users
2. Put load balancer in front
3. Deploy subscription filtering to instance 1
4. Test and monitor
5. Deploy to instance 2
6. Full capacity with redundancy

**Downtime**: 0 minutes (best for production at scale)

---

## Cost Analysis

### Monthly Costs (us-central1-a pricing)

**Per Instance**:
- e2-standard-2: ~$60/month (2 vCPU, 8 GB RAM)
- e2-small: ~$30/month (2 vCPU, 2 GB RAM)
- e2-micro: ~$6/month (0.25-2 vCPU burstable, 1 GB RAM)

**Testing Environment Costs**:

| Configuration | Instances | Monthly Cost | Purpose |
|--------------|-----------|--------------|---------|
| **V1 (current)** | 1× e2-medium<br>1× e2-small<br>1× e2-micro | $24 + $30 + $6<br>**= $60/month** | Max 5K connections tested |
| **V2 (this plan)** | 1× e2-standard-2<br>1× e2-small<br>1× e2-micro | $60 + $30 + $6<br>**= $96/month** | **Max 10K connections tested**<br>Clean isolation |

**Cost Impact**: +$36/month for testing environment to validate 10K capacity

**Why the Upgrade is Worth It**:
- ✅ **Accurate metrics**: No interference from monitoring/publisher
- ✅ **True capacity**: Eliminate client bottlenecks with dedicated test-runner
- ✅ **Production validation**: Prove 10K target before production deployment
- ✅ **Memory headroom**: 8GB allows testing subscription patterns realistically

**Production Implications** (after testing):
- This architecture validates that production can use **e2-standard-2** instances
- Production deployment: 2× e2-standard-2 for 20K users = $120/month (ws-go only)
- Alternative without filtering: 10× e2-medium = $240/month (much more complex)
- **Testing proves**: 50% cost savings in production with subscription filtering

---

## Testing Strategy

### Phase 1: Subscription Filtering Validation

**Test 1: Basic Subscribe/Unsubscribe** (5 min)
```bash
# Connect client, subscribe to hierarchical channels, verify messages
wscat -c ws://34.61.200.145:3004/ws

# Send: {"type":"subscribe","channels":["BTC.trade","ETH.trade"]}
# Publish to sukko.token.BTC.trade → should receive
# Publish to sukko.token.SOL.trade → should NOT receive
# Publish to sukko.token.BTC.liquidity → should NOT receive (different event type)
```

**Test 2: Load Test with Filtering** (10 min)
```bash
# 1,000 connections, varied subscription patterns
task -t taskfiles/isolated-setup.yml test:remote:capacity:1k

# Expected: 100% success, low CPU (<10%)
```

### Phase 2: Capacity Validation

**Test 3: 5K Connections** (15 min)
```bash
task -t taskfiles/isolated-setup.yml test:remote:capacity:5k

# Expected:
# - Success: 100% (5,000/5,000)
# - CPU: <25%
# - Memory: ~3,500 MB
```

**Test 4: 10K Connections** (20 min)
```bash
task -t taskfiles/isolated-setup.yml test:remote:capacity:10k

# Expected:
# - Success: 100% (10,000/10,000)
# - CPU: <30%
# - Memory: ~7,000 MB
```

### Phase 3: Load Testing with Publisher

**Test 5: 10K + 12 msg/sec** (30 min)
```bash
# Start publisher at normal load
task -t taskfiles/isolated-setup.yml publisher:start

# Run capacity test
task -t taskfiles/isolated-setup.yml test:remote:capacity:10k

# Monitor CPU and memory in Grafana
# Expected: CPU <30%, Memory ~7GB, 100% success
```

**Test 6: 10K + 30 msg/sec peak** (15 min)
```bash
# Start publisher at peak load
task -t taskfiles/isolated-setup.yml publisher:start:high

# Monitor for CPU spikes
# Expected: CPU 50-70%, no rejections
```

---

## Monitoring & Validation

### Key Metrics to Track

**Prometheus Queries**:

```promql
# Connection count
ws_connections_current

# CPU usage
ws_cpu_usage_percent

# Memory usage
ws_memory_usage_bytes / (7 * 1024 * 1024 * 1024) * 100  # Percentage of 7GB

# Broadcast efficiency (with filtering)
rate(ws_messages_sent_total[1m]) / ws_connections_current

# Rejection rate (should be 0%)
rate(ws_connections_rejected_total[1m])

# Subscription distribution
ws_subscription_count_per_client
```

### Alerts

```yaml
# Grafana alerts
- name: HighCPUUsage
  condition: ws_cpu_usage_percent > 80
  for: 2m
  severity: warning

- name: HighMemoryUsage
  condition: (ws_memory_usage_bytes / (7 * 1024^3)) > 0.90
  for: 2m
  severity: warning

- name: ConnectionRejections
  condition: rate(ws_connections_rejected_total[5m]) > 0
  for: 1m
  severity: critical
```

---

## Rollback Plan

If issues occur after deployment:

### Scenario 1: Subscription Filtering Bug

1. **Feature flag**: Add `ENABLE_SUBSCRIPTION_FILTERING=false` env var
2. **Fallback**: Code falls back to broadcast-to-all behavior
3. **Time to rollback**: <1 minute (restart container)

### Scenario 2: Performance Degradation

1. **Revert to V1**: Redeploy e2-medium configuration
2. **Scale horizontally**: Add more instances at lower capacity
3. **Time to rollback**: ~5 minutes

### Scenario 3: Critical Bug

1. **Stop ws-go**: `task stop:ws-go`
2. **Deploy previous Docker image**: `docker compose up -d --force-recreate`
3. **Time to rollback**: ~2 minutes

---

## Production Readiness Checklist

**Before deploying to production**:

- [ ] Subscription filtering implemented and tested
- [ ] Load tests passing at 10K connections
- [ ] CPU usage <30% under normal load (12 msg/sec)
- [ ] Memory usage ~7GB at 10K connections
- [ ] No connection rejections during peak load
- [ ] Grafana dashboards configured and tested
- [ ] Alerts configured in Grafana
- [ ] Loki logs flowing and searchable
- [ ] Backup/rollback procedure documented
- [ ] Feature flag for subscription filtering added
- [ ] e2-standard-2 instances created in GCP
- [ ] Load balancer configured (if using multiple instances)
- [ ] DNS updated to point to new instances
- [ ] Monitoring in place for 24 hours post-deploy

---

## Future Optimizations

### Memory Optimization (for 18K target)

**Current**: 0.7 MB per connection
**Target**: 0.2 MB per connection
**Reduction needed**: 71%

**Optimization strategies**:
1. **Goroutine pooling**: Reuse goroutines instead of 2 per connection
2. **Buffer tuning**: Reduce client.send channel size (currently 256)
3. **Replay buffer optimization**: Circular buffer instead of slice
4. **Connection state**: Move to shared memory pool

**Expected impact**: 18K connections on e2-standard-2 (7GB / 0.4MB = 17,500)

### CPU Optimization

**Broadcast hot path**:
1. **Lock contention**: Replace sync.Map with sharded maps
2. **JSON marshaling**: Pre-serialize common messages
3. **Channel operations**: Batch sends where possible

**Expected impact**: 20-30% CPU reduction

---

## Summary

**V2 Testing Architecture Delivers**:
- ✅ **Isolated testing environment** for accurate capacity measurement
- ✅ **10,000 connection validation** (realistic with current 0.7MB/conn memory footprint)
- ✅ **Subscription filtering** (10-20x performance improvement - must implement first)
- ✅ **Clean metrics** (no interference from monitoring/publisher/test client)
- ✅ **Production-ready validation** (proves e2-standard-2 viable for production)

**Testing Infrastructure**:
- **ws-go**: Dedicated e2-standard-2 (full resources, clean measurement)
- **test-runner**: Dedicated e2-micro (eliminates client bottlenecks)
- **backend**: Isolated e2-small (monitoring/NATS without interference)

**Implementation Timeline**:
- Subscription filtering: 6-9 hours (MUST DO FIRST)
- Upgrade ws-go instance: 2-3 hours (e2-medium → e2-standard-2)
- Testing & validation: 4-6 hours (5K → 10K progression)
- **Total**: 12-18 hours (1.5-2 days)

**Next Steps**:
1. **Implement subscription filtering** (CRITICAL - test won't reach 10K without it)
2. **Upgrade ws-go to e2-standard-2** (testing infrastructure change)
3. **Run progressive tests**: 1K → 5K → 10K connections
4. **Document results** (prove 10K capacity achieved)
5. **Use learnings for production deployment** (2 instances for 20K users)

**Key Insight**: This is a **testing architecture** to validate capacity, not production deployment. Once 10K is proven, production can confidently use the same instance sizing.

Ready to proceed with subscription filtering implementation!
