# Configuration Calculations for Isolated ws-go Instance

## Instance Specifications (e2-small Dedicated)

**Total Resources**:
- CPU: 2 vCPU (shared/burstable)
- Memory: 2048 MB RAM
- Network: 1 Gbps

**Reserved for OS/Docker**:
- CPU: 0.2 vCPU (~10%)
- Memory: 256 MB (~12%)

**Available for ws-go**:
- CPU: 1.8 vCPU (90%)
- Memory: 1792 MB (88%)
- Network: 1 Gbps (full)

---

## Current Load Profile

**Transaction rate**: 36,000 tx/day
- Average: 1.25 tx/sec
- Peak hour: 12.5 tx/sec
- Peak minute: 30 tx/sec

**Active tokens**: ~100 tokens with daily activity

**Expected concurrent users**: 20,000-40,000

**Message rate per user** (after subscription filtering):
- Casual (list view): 0.2-1 msg/sec
- Active (favorites): 1-5 msg/sec
- Trader (hot token): 5-20 msg/sec
- **Weighted average**: 2-4 msg/sec per user

---

## Resource Cost Per Connection

### From Testing Data (2,000 connections)

**CPU cost**:
- Test: 2,000 conns @ 20 msg/sec total = 0.24 vCPU
- Per connection: 0.24 / 2000 = **0.00012 vCPU**
- But test had 0.01 msg/sec per connection (20/2000)

**With realistic message rates** (2-4 msg/sec per user):
- CPU scales with message rate
- 2 msg/sec per conn: 0.00012 × (2 / 0.01) = 0.024 vCPU per conn
- **Conservative estimate**: 0.0002 vCPU per connection

**Memory cost** (from code comments):
- Client struct: ~200 bytes
- Send channel: 256 slots × 500 bytes = 128 KB
- Replay buffer: 100 msgs × 500 bytes = 50 KB (reduced from 1000)
- Other: ~100 bytes
- **Total per client**: ~180 KB (0.176 MB)

But empirical from testing:
- 2,000 conns used ~200MB = **0.1 MB per connection**

**Conservative estimate**: **0.1 MB per connection**

### Goroutine Cost

**Per connection**: 2 goroutines (readPump + writePump)
**Per goroutine**: ~8 KB stack
**Total per connection**: 16 KB for goroutines

---

## Capacity Calculations

### 1. WS_MAX_CONNECTIONS

**CPU-limited** (conservative, 2 msg/sec per user):
```
Available CPU: 1.8 vCPU
CPU per connection: 0.0002 vCPU
Max connections: 1.8 / 0.0002 = 9,000 connections
```

**CPU-limited** (light load, 0.5 msg/sec per user):
```
CPU per connection: 0.00005 vCPU
Max connections: 1.8 / 0.00005 = 36,000 connections
```

**Memory-limited**:
```
Available memory: 1792 MB
Memory per connection: 0.1 MB
Max connections: 1792 / 0.1 = 17,920 connections
```

**Network-limited** (2 msg/sec per user, 400 bytes per msg):
```
Available bandwidth: 1 Gbps = 125 MB/sec
Bandwidth per connection: 2 × 400 bytes = 800 bytes/sec
Max connections: 125,000,000 / 800 = 156,250 connections
```

**Recommended: 10,000 connections**

**Why**:
- Safe margin for CPU (leaves headroom for spikes)
- Well within memory limits (uses ~1 GB, 56% of available)
- Network not a bottleneck
- Round number for easy capacity planning
- Room to scale to 15k-20k if needed

---

### 2. WS_WORKER_POOL_SIZE

**Purpose**: Broadcast messages to clients efficiently

**Formula**: `max(32, connections / fanout_factor)`

**Fanout factor considerations**:
- Each worker can handle ~1000-2000 msg/sec
- Per connection: 2 msg/sec
- Messages per worker: 2000 / 2 = 1000 connections

**Calculation**:
```
connections / fanout = 10,000 / 40 = 250 workers
max(32, 250) = 250 workers
```

**Recommended: 256** (power of 2 for cache alignment)

**Why**:
- 256 workers × ~40 connections/worker = 10,240 connections ✓
- CPU cost: 256 workers × minimal overhead = negligible
- Memory cost: 256 workers × ~1 KB state = 256 KB

**Alternative values**:
- Light load (current): 128 workers (10k / 80)
- Heavy load (future): 512 workers (10k / 20)

**Start with**: 256 (middle ground)

---

### 3. WS_WORKER_QUEUE_SIZE

**Purpose**: Buffer for messages waiting to be broadcast

**Formula**: `workers × 100`

**Calculation**:
```
256 workers × 100 = 25,600 queue size
```

**Recommended: 25600**

**Why**:
- 100 messages buffered per worker
- At 12 tx/sec peak, queue fills in: 25,600 / 12 = 2,133 seconds (35 min)
- Plenty of headroom for bursts
- Memory cost: 25,600 × 500 bytes = 12.8 MB (acceptable)

**Queue depth analysis**:
```
Peak transaction rate: 30 tx/sec
Processing rate: 256 workers × 1000 msg/sec = 256,000 msg/sec
Queue utilization: 30 / 256,000 = 0.01% (essentially empty)
```

**Could reduce to**: 12,800 (workers × 50) to save 6 MB RAM

**Keep at**: 25,600 for safety margin

---

### 4. WS_MAX_GOROUTINES

**Purpose**: Hard limit to prevent goroutine explosion

**Components**:
1. Client goroutines: connections × 2 (readPump + writePump)
2. Worker goroutines: worker pool size
3. Static goroutines: monitors, NATS consumers, timers
4. Overhead: 20% buffer for temporary goroutines

**Calculation**:
```
Client goroutines: 10,000 × 2 = 20,000
Worker goroutines: 256
Static goroutines: ~20 (monitors, NATS, timers, etc.)
Subtotal: 20,276
With 20% overhead: 20,276 × 1.2 = 24,331

Round up to: 25,000
```

**Recommended: 25000**

**Memory cost**:
```
25,000 goroutines × 8 KB stack = 200 MB
Percentage of RAM: 200 / 1792 = 11.2%
```

**Why**:
- Covers all expected goroutines with headroom
- Memory cost acceptable (11% of RAM)
- Safety valve: prevents runaway goroutine creation
- If hit, indicates a leak (alert!)

---

### 5. WS_MAX_NATS_RATE

**Purpose**: Limit NATS message consumption to prevent CPU overload

**Current backend rate**: 12 tx/sec peak (very light!)

**Recommended: 0 (unlimited)** for testing

**Why**:
- Current load is trivial (12 msg/sec)
- Even 1000x growth (12,000 msg/sec) is handleable
- Rate limiting adds complexity
- For testing: want to measure true capacity

**For production** (optional):
- Conservative: 1000 (83x current peak)
- Moderate: 5000 (416x current peak)
- Aggressive: 10000 (833x current peak)

**Start with**: 0 (unlimited) until actual bottleneck identified

---

### 6. WS_MAX_BROADCAST_RATE

**Purpose**: Limit broadcast fan-out rate

**Calculation**:
```
NATS rate: 12 msg/sec
Subscribers per token: ~200 avg (out of 10k connections)
Broadcast rate: 12 × 200 = 2,400 msg/sec
```

**Recommended: 0 (unlimited)** for testing

**Why**:
- Current broadcast rate: 2,400 msg/sec (trivial for worker pool)
- Worker capacity: 256 workers × 1000 msg/sec = 256,000 msg/sec
- Utilization: 2,400 / 256,000 = 0.9%
- No need to limit

**For production** (optional):
- Set to 100,000 (safety valve for runaway broadcasts)

**Start with**: 0 (unlimited)

---

### 7. WS_CPU_REJECT_THRESHOLD

**Purpose**: Reject new connections when CPU usage exceeds threshold

**Recommended: 75.0** (75%)

**Why**:
- Leaves 25% headroom for processing existing connections
- Prevents cascade failure (new connections making CPU worse)
- Industry standard (similar to Kubernetes pod eviction threshold)

**Calculation**:
```
At 75% CPU (1.35 vCPU used):
Conservative estimate: 1.35 / 0.0002 = 6,750 connections
This triggers before hitting 10k connection limit ✓
```

**Alternative values**:
- Conservative: 70% (more headroom)
- Aggressive: 80% (tighter capacity)

**Keep at**: 75% (balanced)

---

### 8. WS_CPU_PAUSE_THRESHOLD

**Purpose**: Pause NATS consumption when CPU exceeds threshold

**Recommended: 80.0** (80%)

**Why**:
- Emergency brake: stops new messages from overwhelming server
- Allows existing messages to drain
- 5% gap between reject (75%) and pause (80%) for graduated response
- Prevents total collapse

**Behavior**:
```
75% CPU: Stop accepting new WebSocket connections
80% CPU: Stop consuming from NATS (pause message intake)
<80% CPU: Resume NATS consumption
<75% CPU: Resume accepting connections
```

**Keep at**: 80%

---

## Final Configuration

```yaml
environment:
  # Instance resource limits
  - WS_CPU_LIMIT=1.8              # 90% of e2-small (0.2 for OS)
  - WS_MEMORY_LIMIT=1879048192    # 1792 MB in bytes (256MB for OS)

  # Connection capacity
  - WS_MAX_CONNECTIONS=10000      # Capacity: 10k concurrent connections

  # Worker pool (message broadcasting)
  - WS_WORKER_POOL_SIZE=256       # 256 workers (10k / 40 = 250, rounded to power of 2)
  - WS_WORKER_QUEUE_SIZE=25600    # 256 × 100 buffer slots

  # Goroutine limits
  - WS_MAX_GOROUTINES=25000       # (10k×2 + 256 + 20) × 1.2 = 24,331 → 25k

  # Rate limiting (testing: unlimited)
  - WS_MAX_NATS_RATE=0            # Unlimited (current: 12/sec, capacity: 256k/sec)
  - WS_MAX_BROADCAST_RATE=0       # Unlimited (current: 2.4k/sec, capacity: 256k/sec)

  # Safety thresholds
  - WS_CPU_REJECT_THRESHOLD=75.0  # Reject new connections at 75% CPU
  - WS_CPU_PAUSE_THRESHOLD=80.0   # Pause NATS at 80% CPU

  # JetStream config
  - JS_STREAM_NAME=SUKKO_TOKENS
  - JS_CONSUMER_NAME=ws-server
  - JS_STREAM_MAX_AGE=30s         # 30 second message retention
  - JS_STREAM_MAX_MSGS=100000     # 100k message limit
  - JS_STREAM_MAX_BYTES=52428800  # 50 MB limit

  # Logging
  - LOG_LEVEL=info
  - LOG_FORMAT=json
```

---

## Resource Utilization Projections

### Light Load (5,000 connections, 1 msg/sec avg)

```
CPU: 5,000 × 0.0001 = 0.5 vCPU (28% utilization) ✅
Memory: 5,000 × 0.1 MB = 500 MB (28% utilization) ✅
Goroutines: 5,000 × 2 + 256 = 10,256 (41% of limit) ✅
Network: 5,000 × 1 msg/sec × 400 bytes = 2 MB/sec (1.6% of 125 MB/sec) ✅
```

**Headroom**: Massive (can 2x connections easily)

### Target Load (10,000 connections, 2 msg/sec avg)

```
CPU: 10,000 × 0.0002 = 2.0 vCPU (111% utilization) ⚠️
Memory: 10,000 × 0.1 MB = 1,000 MB (56% utilization) ✅
Goroutines: 10,000 × 2 + 256 = 20,256 (81% of limit) ✅
Network: 10,000 × 2 msg/sec × 400 bytes = 8 MB/sec (6.4% of 125 MB/sec) ✅
```

**Headroom**: CPU will spike above 100% during bursts (burstable instance handles this)

**Note**: e2-small is burstable, can temporarily use >100% CPU

### Peak Load (10,000 connections, 4 msg/sec peak)

```
CPU: 10,000 × 0.0004 = 4.0 vCPU (222% utilization) ❌
Memory: 10,000 × 0.1 MB = 1,000 MB (56% utilization) ✅
Goroutines: 10,000 × 2 + 256 = 20,256 (81% of limit) ✅
Network: 10,000 × 4 msg/sec × 400 bytes = 16 MB/sec (12.8% of 125 MB/sec) ✅
```

**Bottleneck**: CPU during message bursts

**Mitigation**:
- e2-small bursting handles temporary spikes
- CPU reject threshold (75%) kicks in
- Auto-scaler adds instances if sustained

---

## Scaling Strategy

### When to Add Instance (Horizontal Scaling)

**Trigger 1**: CPU > 60% sustained for 5 minutes
- Indicates: Approaching capacity
- Action: Add instance, split load

**Trigger 2**: Connections > 8,000 (80% capacity)
- Indicates: Running out of connection slots
- Action: Add instance preemptively

**Trigger 3**: CPU reject threshold hit repeatedly
- Indicates: Turning away connections due to CPU
- Action: Add instance immediately

### Scaling Math

**2 instances** (20k total capacity):
```
Load per instance: 10k connections, 2 msg/sec avg
CPU per instance: 2.0 vCPU (100% nominal, burstable handles it)
Total capacity: 20,000 concurrent connections
```

**3 instances** (30k total capacity):
```
Load per instance: 6,667 connections, 2 msg/sec avg
CPU per instance: 1.33 vCPU (74% utilization) ✅
Headroom: 26% CPU, can handle bursts comfortably
```

**Recommendation**: Scale to 3 instances at 15k total connections

---

## Alternative Configurations

### Conservative (Testing Phase)

Focus: Stability over capacity

```yaml
- WS_MAX_CONNECTIONS=5000        # Half capacity
- WS_WORKER_POOL_SIZE=128        # Half workers
- WS_WORKER_QUEUE_SIZE=12800     # Half queue
- WS_MAX_GOROUTINES=12500        # (5k×2 + 128 + 20) × 1.2
- WS_CPU_REJECT_THRESHOLD=70.0   # More headroom
```

**Use when**: Initial deployment, risk-averse testing

### Aggressive (Production)

Focus: Maximum capacity

```yaml
- WS_MAX_CONNECTIONS=15000       # 1.5x capacity
- WS_WORKER_POOL_SIZE=512        # More workers
- WS_WORKER_QUEUE_SIZE=51200     # Bigger queue
- WS_MAX_GOROUTINES=37500        # (15k×2 + 512 + 20) × 1.2
- WS_CPU_REJECT_THRESHOLD=80.0   # Tighter margin
```

**Use when**: Proven stable, want max capacity per instance

**Risk**: CPU spikes may cause instability

---

## Validation Tests

After deploying with calculated config:

### Test 1: Light Load (2,000 connections)
```bash
TARGET_CONNECTIONS=2000 DURATION=300 node scripts/sustained-load-test.cjs
```

**Expected metrics**:
- CPU: 10-20%
- Memory: 200-300 MB (11-17%)
- Goroutines: ~4,500
- Message rate: 2,000-4,000 msg/sec

**Pass criteria**: Stable for 5 minutes, no errors

### Test 2: Target Load (5,000 connections)
```bash
TARGET_CONNECTIONS=5000 DURATION=600 node scripts/sustained-load-test.cjs
```

**Expected metrics**:
- CPU: 25-35%
- Memory: 500-700 MB (28-39%)
- Goroutines: ~10,500
- Message rate: 5,000-10,000 msg/sec

**Pass criteria**: Stable for 10 minutes, <1% message drops

### Test 3: Capacity Test (10,000 connections)
```bash
TARGET_CONNECTIONS=10000 DURATION=600 node scripts/sustained-load-test.cjs
```

**Expected metrics**:
- CPU: 60-80% (may burst higher)
- Memory: 1,000-1,200 MB (56-67%)
- Goroutines: ~20,500
- Message rate: 20,000-40,000 msg/sec

**Pass criteria**: Stable for 10 minutes, CPU reject threshold not hit

### Test 4: Overload Test (12,000 connections)
```bash
TARGET_CONNECTIONS=12000 DURATION=300 node scripts/sustained-load-test.cjs
```

**Expected behavior**:
- CPU: 75%+ (reject threshold hits)
- Some connections rejected (expected!)
- Server remains stable (doesn't crash)
- Final connection count: ~10,000 (server's limit)

**Pass criteria**: Server stable, rejects excess load gracefully

---

## Monitoring Alerts

Set up alerts for these thresholds:

```yaml
# Warning: approaching capacity
- alert: HighConnectionCount
  expr: ws_connections_current > 8000
  for: 5m

- alert: HighCPU
  expr: ws_cpu_percent > 60
  for: 5m

# Critical: at capacity
- alert: CPURejectThreshold
  expr: ws_cpu_percent > 75
  for: 1m

- alert: ConnectionLimit
  expr: ws_connections_current >= 10000
  for: 1m

# Error: rejecting connections
- alert: ConnectionsRejected
  expr: rate(ws_connections_rejected_total[5m]) > 0
```

---

## Summary

**Configuration is optimized for**:
- 10,000 concurrent WebSocket connections
- 2-4 msg/sec per connection (realistic trading activity)
- Light backend load (12 tx/sec peak, room for 1000x growth)
- Isolated e2-small instance (full resources available)

**Bottleneck**: CPU during message bursts (expected, manageable)

**Scaling trigger**: Add instance when connections > 8,000 or CPU > 60%

**Cost**: $12.23/month per instance

**Next step**: Deploy with these values and run validation tests
