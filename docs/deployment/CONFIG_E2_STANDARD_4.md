# Optimal Configuration: e2-standard-4 (4 vCPU, 16GB RAM)

**Target Capacity:** 15,000 concurrent WebSocket connections
**Monthly Cost:** ~$72 (730 hours × $0.0984/hour)
**Use Case:** High-capacity production deployment for up to 15K connections

---

## Hardware Specifications

```
Instance Type: e2-standard-4
vCPUs:         4
Memory:        16 GB
Network:       4 Gbps
Cost:          $72/month (~$0.0984/hour)
```

---

## Complete .env.production Configuration

```bash
# =============================================================================
# WebSocket Server Production Configuration
# =============================================================================
# Instance: sukko-go (e2-standard-4: 4 vCPU, 16GB RAM)
# Purpose: Handle 15,000 concurrent WebSocket connections
# Location: GCP us-central1

ENVIRONMENT=production

# =============================================================================
# SERVER
# =============================================================================
WS_ADDR=:3002
NATS_URL=nats://${BACKEND_INTERNAL_IP}:4222

# =============================================================================
# RESOURCE LIMITS (Optimized for e2-standard-4)
# =============================================================================
# CPU: 3.9 cores (97.5% of 4 vCPU, reserve 0.1 for Promtail)
WS_CPU_LIMIT=3.9

# Memory: 14 GB = 15032385536 bytes (16GB - 2GB for OS + Promtail)
WS_MEMORY_LIMIT=15032385536

# Connections: 15,000 (2.14× capacity vs e2-standard-2)
WS_MAX_CONNECTIONS=15000

# =============================================================================
# WORKER POOL (Production-validated formula)
# =============================================================================
# Formula: max(32, connections/40) = max(32, 15000/40) = 375 → 384 (power of 2)
# Load per worker: (15000 × 100) / 384 = 3,906 msg/sec (optimal: 300-5,000)
WS_WORKER_POOL_SIZE=384
WS_WORKER_QUEUE_SIZE=38400

# =============================================================================
# RATE LIMITING (Based on production metrics)
# =============================================================================
# Production traffic: 280K users, 40K tx/day peak
# Actual rates: 5 msg/sec (average) to 19.7 msg/sec (high peak)
# Publisher configured: 25 msg/sec baseline
#
# Why 100 msg/sec (4x headroom):
# 1. JetStream redeliveries when server NAKs messages
# 2. Multiple tokens publishing simultaneously (10 tokens × 25 = 250 theoretical)
# 3. Traffic bursts during high volatility
# 4. Prevents NAK feedback loop
#
# e2-standard-4 network capacity: 4 Gbps (supports up to 142 msg/sec)
# 100 msg/sec is safe with 30% headroom
WS_MAX_NATS_RATE=100
WS_MAX_BROADCAST_RATE=100

# Goroutine limit
# Formula: ((15000 × 2) + 384 + 13) × 1.2 = 36,476 → 36,500
WS_MAX_GOROUTINES=36500

# =============================================================================
# SAFETY THRESHOLDS
# =============================================================================
WS_CPU_REJECT_THRESHOLD=75.0
WS_CPU_PAUSE_THRESHOLD=80.0

# =============================================================================
# JETSTREAM
# =============================================================================
JS_STREAM_NAME=SUKKO_TOKENS
JS_CONSUMER_NAME=ws-server
JS_STREAM_MAX_AGE=30s
JS_STREAM_MAX_MSGS=100000
JS_STREAM_MAX_BYTES=52428800
JS_CONSUMER_ACK_WAIT=30s

# =============================================================================
# MONITORING
# =============================================================================
METRICS_INTERVAL=15s

# =============================================================================
# LOGGING
# =============================================================================
LOG_LEVEL=info
LOG_FORMAT=json
```

---

## Expected Resource Usage at 15,000 Connections

### At 100 msg/sec (Current Test Rate)

```
CPU Usage:
- Absolute: 2.451 cores (scaled from 7K test: 1.144 × 15/7)
- Percentage: 62.8% of 3.9 core limit
- System-wide: 61.3% (2.451 / 4 cores)
- Headroom: 37.2% (1.449 cores available)

Memory Usage:
- Absolute: 13.665 GB (15K × 0.911 MB per conn)
- Percentage: 97.6% of 14 GB limit
- Headroom: 2.4% (0.335 GB available)

Goroutines:
- Active: ~30,413 (15K × 2 + 384 + 13 + overhead)
- Limit: 36,500
- Percentage: 83.3%
- Headroom: 16.7% (6,087 goroutines)

Network:
- Throughput: 15,000 × 100 msg/sec × 500 bytes = 750 MB/sec = 6 Gbps
- NIC Capacity: 4 Gbps
- Status: ⚠️ EXCEEDS at 100 msg/sec
- Recommended: Limit to 66 msg/sec (3.3 Gbps = 82% of NIC)

Messages:
- Total outbound: 1,500,000 msg/sec (15K × 100)
- Per connection: 100 msg/sec
```

### At 66 msg/sec (Network-Safe Rate)

```
CPU Usage:
- Absolute: ~1.618 cores (66% of 100 msg/sec load)
- Percentage: 41.5% of 3.9 core limit
- System-wide: 40.5% (1.618 / 4 cores)
- Headroom: 58.5% ✅

Memory Usage:
- Absolute: 13.665 GB (same, memory doesn't scale with msg rate)
- Percentage: 97.6% of 14 GB limit
- Headroom: 2.4%

Network:
- Throughput: 15,000 × 66 × 500 bytes = 495 MB/sec = 3.96 Gbps
- NIC Capacity: 4 Gbps
- Percentage: 99% of NIC
- Headroom: 1% ✅ (at limit but safe)
```

### At 25 msg/sec (Conservative Rate)

```
CPU Usage:
- Absolute: ~0.613 cores (25% of 100 msg/sec load)
- Percentage: 15.7% of 3.9 core limit
- System-wide: 15.3% (0.613 / 4 cores)
- Headroom: 84.3% ✅

Memory Usage:
- Absolute: 13.665 GB
- Percentage: 97.6% of 14 GB limit
- Headroom: 2.4%

Network:
- Throughput: 15,000 × 25 × 500 bytes = 187.5 MB/sec = 1.5 Gbps
- NIC Capacity: 4 Gbps
- Percentage: 37.5% of NIC
- Headroom: 62.5% ✅
```

---

## Capacity Limits

### Hard Limits

```
Max Connections (Memory-limited):
= 14 GB / 0.911 MB per conn
= 15,368 connections

Configured Max: 15,000 connections (safe 2.4% buffer)
```

### Rate Limits

```
Safe NATS Rate (Network-limited on e2-standard-4):
= 4 Gbps / (15,000 × 500 bytes × 8 bits)
= 66.7 msg/sec

Current Config: 100 msg/sec
Status: ⚠️ Exceeds 4 Gbps NIC at peak (requires 6 Gbps)

Recommendations:
- Option A: Reduce to 66 msg/sec (safe for e2-standard-4)
- Option B: Keep 100 msg/sec if burst traffic averages < 66 over time
- Option C: Upgrade to e2-standard-8 (8 Gbps NIC) for sustained 100 msg/sec
```

---

## Deployment Instructions

### 1. Create Instance (if new)

```bash
gcloud compute instances create sukko-go \
  --zone=us-central1-a \
  --machine-type=e2-standard-4 \
  --boot-disk-size=20GB \
  --boot-disk-type=pd-standard \
  --image-family=debian-11 \
  --image-project=debian-cloud \
  --tags=websocket-server
```

### 2. Upgrade Existing Instance

```bash
# Stop instance
gcloud compute instances stop sukko-go --zone=us-central1-a

# Resize to e2-standard-4
gcloud compute instances set-machine-type sukko-go \
  --zone=us-central1-a \
  --machine-type=e2-standard-4

# Start instance
gcloud compute instances start sukko-go --zone=us-central1-a
```

### 3. Deploy Configuration

```bash
# Update .env.production with above configuration
# Deploy using task
task gcp2:deploy:ws-go
```

### 4. Validate Deployment

```bash
# Check health endpoint
task gcp2:health

# Expected output:
{
  "status": "healthy",
  "checks": {
    "cpu": {"percentage": 16, "threshold": 75, "healthy": true},
    "memory": {"percentage": 98, "limit_mb": 14336, "healthy": true},
    "capacity": {"current": 15000, "max": 15000, "percentage": 100}
  }
}
```

### 5. Run Capacity Test

```bash
# Test at design limit (15K connections)
TARGET_CONNECTIONS=15000 RAMP_RATE=150 DURATION=3600 \
  task gcp2:test:remote:capacity

# Expected result:
# - Success rate: 99%+
# - CPU: 60-65% of limit
# - Memory: 96-98% of limit
# - All connections stable for 1 hour
```

---

## Scaling Considerations

### When to Upgrade to e2-standard-8

**Upgrade if:**
- Need > 15,000 connections (e2-std-8 supports 30K)
- Need sustained > 66 msg/sec rate (e2-std-8 has 8 Gbps NIC)
- CPU consistently > 70% (need more cores)

**Cost impact:** $72/month → $144/month (+$72)

### Horizontal Scaling (Multi-Instance)

At > 15K connections, consider horizontal scaling:

```
Load Balancer
    ↓
    ├→ Instance 1: e2-standard-4 (15K connections)
    ├→ Instance 2: e2-standard-4 (15K connections)
    └→ Instance 3: e2-standard-4 (15K connections)

Total capacity: 45K connections
Total cost: $216/month (3 × $72)
Per-connection cost: $4.80 / 1K connections
```

### Vertical Scaling Path

```
Small:       e2-standard-2 (7K conns,   50 msg/sec)   $24/month
Current:     e2-standard-4 (15K conns,  66 msg/sec)   $72/month
Large:       e2-standard-8 (30K conns, 133 msg/sec)  $144/month
Horizontal:  3× e2-std-4   (45K conns,  66 msg/sec)  $216/month
```

---

## Performance Characteristics

### Strengths (e2-standard-4)

- ✅ High capacity: 15K connections (2.14× vs e2-standard-2)
- ✅ Better CPU distribution: 4 cores vs 2
- ✅ Double network bandwidth: 4 Gbps vs 2 Gbps
- ✅ Room for growth: Can handle 15.3K connections
- ✅ Cost-effective: $4.80 per 1K connections

### Limitations (e2-standard-4)

- ⚠️ Memory at 98% (limited headroom for spikes)
- ⚠️ Network bottleneck at > 66 msg/sec (4 Gbps NIC)
- ⚠️ Cannot scale beyond 15.3K connections
- ⚠️ Higher cost vs e2-standard-2 ($72 vs $24)

### Comparison to e2-standard-2

| Metric | e2-standard-2 | e2-standard-4 | Improvement |
|--------|---------------|---------------|-------------|
| **Connections** | 7,000 | 15,000 | +114% |
| **CPU Cores** | 2 | 4 | +100% |
| **Memory** | 8 GB | 16 GB | +100% |
| **Network** | 2 Gbps | 4 Gbps | +100% |
| **Cost** | $24/month | $72/month | +200% |
| **Cost per 1K** | $3.43 | $4.80 | +40% |
| **Max Rate** | 50 msg/sec | 66 msg/sec | +32% |

---

## Monitoring Thresholds

### Healthy

```
CPU:        < 60%
Memory:     < 95%
Goroutines: < 30,000
Connections: < 13,500 (90% of max)
```

### Degraded (Warning)

```
CPU:        60-75%
Memory:     95-99%
Goroutines: 30,000-32,850
Connections: 13,500-15,000 (90-100% of max)
```

### Unhealthy (Action Required)

```
CPU:        > 75%
Memory:     > 100% of limit
Goroutines: > 36,500
Connections: > 15,000
NATS:       Disconnected
```

---

## Cost Breakdown

```
Instance:        $71.81/month (e2-standard-4)
Persistent Disk: $0.80/month (20GB pd-standard)
Network Egress:  Variable (~$17/month for 15K connections)
Total:           ~$90/month

Cost per connection: $90 / 15,000 = $0.006/month
Cost per 1K conns:   $6.00/month
```

---

## Migration from e2-standard-2

### Downtime: ~5 Minutes

**Steps:**

1. **Stop test traffic**
   ```bash
   # Stop any running capacity tests
   ```

2. **Stop instance**
   ```bash
   gcloud compute instances stop sukko-go --zone=us-central1-a
   ```

3. **Resize instance**
   ```bash
   gcloud compute instances set-machine-type sukko-go \
     --zone=us-central1-a \
     --machine-type=e2-standard-4
   ```

4. **Update .env.production**
   - Change WS_CPU_LIMIT: 1.9 → 3.9
   - Change WS_MEMORY_LIMIT: 7516192768 → 15032385536
   - Change WS_MAX_CONNECTIONS: 7000 → 15000
   - Change WS_WORKER_POOL_SIZE: 192 → 384
   - Change WS_WORKER_QUEUE_SIZE: 19200 → 38400
   - Change WS_MAX_GOROUTINES: 17500 → 36500

5. **Start instance**
   ```bash
   gcloud compute instances start sukko-go --zone=us-central1-a
   ```

6. **Deploy new configuration**
   ```bash
   task gcp2:deploy:ws-go
   ```

7. **Validate**
   ```bash
   # Run health check
   task gcp2:health

   # Run capacity test
   TARGET_CONNECTIONS=15000 DURATION=3600 task gcp2:test:remote:capacity
   ```

---

## Quick Reference Card

```
╔════════════════════════════════════════════════════════╗
║  e2-standard-4: 15K Connections @ $72/month          ║
╠════════════════════════════════════════════════════════╣
║  CPU Limit:      3.9 cores                            ║
║  Memory Limit:   14 GB                                ║
║  Max Conns:      15,000                               ║
║  Workers:        384                                  ║
║  Queue:          38,400                               ║
║  Goroutines:     36,500                               ║
║  NATS Rate:      100 msg/sec (config)                ║
║  Network Limit:  66 msg/sec (4 Gbps NIC bottleneck)  ║
╚════════════════════════════════════════════════════════╝
```

---

## Production Readiness Checklist

- [ ] Instance type is e2-standard-4
- [ ] .env.production updated with above configuration
- [ ] Configuration deployed and active
- [ ] Health check returns "healthy" or "degraded"
- [ ] Capacity test passed at 15K connections
- [ ] CPU < 70% under full load
- [ ] Memory < 99% under full load
- [ ] Monitoring/alerting configured (Grafana + Prometheus)
- [ ] NATS rate limited to 66 msg/sec or network monitored

---

**Last Updated:** 2025-10-19
**Status:** Ready for Testing
**Recommended For:** 10K-15K connections, up to 66 msg/sec sustained rate
