# GKE Autopilot Cost Estimate

**Last Updated:** 2025-12-06
**Region:** us-central1
**Based on:** Helm values-production.yaml, values-staging.yaml

---

## Resource Summary

Based on your Helm chart configurations:

| Service | Replicas | CPU Request | Memory Request | Storage |
|---------|----------|-------------|----------------|---------|
| **ws-server** | 2-10 (HPA) | 1 vCPU × N | 512Mi × N | - |
| **auth-service** | 1-2 | 0.1 vCPU × N | 64Mi × N | - |
| **redpanda** | 1 | 0.5 vCPU | 512Mi | 50Gi SSD (prod) |
| **valkey** | 1 | 0.25 vCPU | 256Mi | - |
| **nats** (alternative) | 1-3 | 0.1 vCPU × N | 128Mi × N | - |
| **prometheus** | 1 | 0.25 vCPU | 256Mi | - |
| **grafana** | 1 | 0.125 vCPU | 256Mi | - |
| **loki** | 1 | 0.25 vCPU | 256Mi | - |
| **promtail** | 1/node | 0.05 vCPU | 64Mi | - |

---

## GKE Autopilot Pricing (us-central1)

| Resource | Hourly Rate | Monthly (730 hrs) |
|----------|-------------|-------------------|
| vCPU | $0.0445 | $32.44/vCPU |
| Memory | $0.00494/GB | $3.61/GB |
| SSD Storage | - | $0.17/GB |
| Cluster Management | $0.10 | $73.00 |

---

## NATS vs Redis/Valkey for BroadcastBus

NATS can replace Redis/Valkey as the BroadcastBus for cross-pod message synchronization.

### Feature Comparison

| Aspect | Redis/Valkey | NATS |
|--------|--------------|------|
| **Primary Purpose** | Cache + Pub/Sub + Data Structures | Pure Messaging |
| **Clustering** | Sentinel (complex) or Cluster mode | Native, simple |
| **Memory Footprint** | ~256MB baseline | ~50-100MB baseline |
| **Latency** | ~0.5-1ms | ~0.1-0.3ms |
| **HA Setup** | Sentinel (3 nodes) or Cluster (6+) | 3 NATS nodes |
| **Ops Complexity** | Medium-High | Low |
| **Caching Support** | Yes (primary use) | No |
| **Data Structures** | Yes (lists, sets, hashes) | No |
| **Go Client** | `go-redis/redis` | `nats-io/nats.go` |

### Cost Comparison: NATS vs Redis/Valkey

| Environment | Redis/Valkey (Self) | NATS (Self) | Savings |
|-------------|---------------------|-------------|---------|
| **Staging** (1 node) | $9/mo | $6/mo | -$3/mo |
| **Production HA** (3 nodes) | $27/mo | $18/mo | -$9/mo |

**Resource per node:**
- Redis/Valkey: 0.25 vCPU / 256Mi
- NATS: 0.1 vCPU / 128Mi

### When to Choose NATS

- You only need pub/sub (no caching)
- Simpler HA is a priority
- Lower resource footprint matters
- You want faster messaging latency

### When to Keep Redis/Valkey

- You might need caching later
- You want data structures (rate limiting, sessions)
- Team already knows Redis
- Single-node staging is fine

---

## Cost Comparison: Self-Managed vs Managed Services

### Staging Environment

| Component | Self-Managed | With NATS | Managed Service | Provider Options |
|-----------|--------------|-----------|-----------------|------------------|
| GKE Autopilot Base | $73 | $73 | $73 | - |
| ws-server (1 pod) | $36 | $36 | $36 | - |
| auth-service | $4 | $4 | $4 | - |
| Kafka/Redpanda | $24 | $24 | $60-80 | Upstash Kafka, Confluent Basic |
| Redis/Valkey | $9 | - | $10-36 | Upstash Pro, Memorystore Basic |
| NATS | - | $6 | $15-30 | Synadia NGS |
| Monitoring Stack | $22 | $22 | $0 | GCP Cloud Monitoring (free tier) |
| Storage + Egress | $15 | $15 | $10 | - |
| **TOTAL** | **~$183/mo** | **~$180/mo** | **~$213/mo** | NATS saves $3/mo |

### Production Environment

| Component | Self-Managed | With NATS | Managed Service | Provider Options |
|-----------|--------------|-----------|-----------------|------------------|
| GKE Autopilot Base | $73 | $73 | $73 | - |
| ws-server (2-3 avg) | $108 | $108 | $108 | HPA at 70% CPU |
| auth-service (HA) | $8 | $8 | $8 | 2 replicas |
| Kafka/Redpanda | $35 | $35 | $150-300 | Confluent Standard, Redpanda Dedicated |
| Redis/Valkey (HA) | $27 | - | $72-100 | Memorystore Standard (2GB HA) |
| NATS (HA) | - | $18 | $50-80 | Synadia NGS Pro |
| Monitoring Stack | $22 | $22 | $50-150 | Managed Prometheus |
| Storage + Egress | $45 | $45 | $30 | - |
| Load Balancer | $18 | $18 | $18 | - |
| **TOTAL (min)** | **~$336/mo** | **~$327/mo** | **~$509/mo** | NATS saves $9/mo |
| **TOTAL (scaled)** | **~$574/mo** | **~$565/mo** | **~$800/mo** | NATS saves $9/mo |

---

## Summary Table

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                         MONTHLY COST COMPARISON (USD)                                     │
├─────────────────────┬───────────────┬───────────────┬───────────────┬────────────────────┤
│ Component           │ Self-Managed  │ With NATS     │ Fully Managed │ Notes              │
│                     │ (Redis)       │ (no Redis)    │ Services      │                    │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ ** STAGING **       │               │               │               │                    │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ GKE Autopilot       │     $73       │     $73       │     $73       │ Base cluster fee   │
│ ws-server (1 pod)   │     $36       │     $36       │     $36       │ 1 vCPU + 512Mi     │
│ auth-service        │      $4       │      $4       │      $4       │ 0.1 vCPU + 64Mi    │
│ Kafka/Redpanda      │     $24       │     $24       │     $70       │ Upstash/Confluent  │
│ Redis/Valkey        │      $9       │      -        │     $20       │ Upstash/Memorystore│
│ NATS                │      -        │      $6       │     $20       │ Synadia NGS        │
│ Monitoring          │     $22       │     $22       │      $0       │ GCP free tier      │
│ Storage + Egress    │     $15       │     $15       │     $10       │                    │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ STAGING TOTAL       │   ~$183/mo    │   ~$180/mo    │   ~$213/mo    │ NATS saves $3/mo   │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ ** PRODUCTION **    │               │               │               │                    │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ GKE Autopilot       │     $73       │     $73       │     $73       │ Base cluster fee   │
│ ws-server (2-3 avg) │    $108       │    $108       │    $108       │ HPA, 70% target    │
│ auth-service (HA)   │      $8       │      $8       │      $8       │ 2 replicas         │
│ Kafka/Redpanda      │     $35       │     $35       │    $200       │ Confluent Std      │
│ Redis/Valkey (HA)   │     $27       │      -        │     $72       │ Memorystore HA     │
│ NATS (HA)           │      -        │     $18       │     $60       │ Synadia NGS Pro    │
│ Monitoring          │     $22       │     $22       │     $80       │ Managed Prometheus │
│ Storage + Egress    │     $45       │     $45       │     $30       │                    │
│ Load Balancer       │     $18       │     $18       │     $18       │                    │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ PRODUCTION TOTAL    │   ~$336/mo    │   ~$327/mo    │   ~$589/mo    │ NATS saves $9/mo   │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ ** COMBINED **      │               │               │               │                    │
├─────────────────────┼───────────────┼───────────────┼───────────────┼────────────────────┤
│ Staging + Prod      │   ~$519/mo    │   ~$507/mo    │   ~$802/mo    │ NATS saves $12/mo  │
│ Annual              │  ~$6,228/yr   │  ~$6,084/yr   │  ~$9,624/yr   │ NATS saves $144/yr │
└─────────────────────┴───────────────┴───────────────┴───────────────┴────────────────────┘
```

---

## Trade-off Analysis

| Factor | Self-Managed (Redis) | Self-Managed (NATS) | Fully Managed |
|--------|----------------------|---------------------|---------------|
| **Monthly Cost** | ~$520 | ~$507 | ~$800 |
| **Annual Cost** | ~$6,240 | ~$6,084 | ~$9,600 |
| **Ops Overhead** | High | Medium | Low |
| **HA Complexity** | Sentinel/Cluster | Native clustering | Built-in |
| **Scaling** | Manual / custom HPA | Native | Automatic |
| **SLA** | You own it | You own it | 99.9-99.99% |
| **Caching Support** | Yes | No | Yes |
| **Best For** | Need caching + pub/sub | Pure messaging | Speed, reliability |

---

## Hybrid Approach (Recommended)

A balanced approach using managed services only where DIY is complex:

### Option A: Keep Redis (if you need caching later)

| Component | Choice | Monthly Cost | Rationale |
|-----------|--------|--------------|-----------|
| GKE Autopilot | Self-managed | $73 | Required |
| ws-server | Self-managed | $108 | Core app, you control it |
| auth-service | Self-managed | $8 | Simple stateless service |
| Kafka/Redpanda | **Self-managed** | $35 | Simple pub/sub, your Helm works |
| Redis/Valkey | **Memorystore HA** | $72 | Redis HA is complex to DIY |
| Monitoring | **GCP Cloud Monitoring** | $0-50 | Free tier + managed |
| Storage + LB + Egress | - | $63 | Fixed costs |
| **HYBRID TOTAL (Redis)** | - | **~$409/mo** | Best if you need caching |

### Option B: Use NATS (pure messaging, lower cost)

| Component | Choice | Monthly Cost | Rationale |
|-----------|--------|--------------|-----------|
| GKE Autopilot | Self-managed | $73 | Required |
| ws-server | Self-managed | $108 | Core app, you control it |
| auth-service | Self-managed | $8 | Simple stateless service |
| Kafka/Redpanda | **Self-managed** | $35 | Simple pub/sub, your Helm works |
| NATS (HA) | **Self-managed** | $18 | Native clustering, simpler ops |
| Monitoring | **GCP Cloud Monitoring** | $0-50 | Free tier + managed |
| Storage + LB + Egress | - | $63 | Fixed costs |
| **HYBRID TOTAL (NATS)** | - | **~$355/mo** | Lowest cost, pure messaging |

---

## Managed Service Options

### Kafka/Streaming

| Provider | Tier | Monthly Cost | Notes |
|----------|------|--------------|-------|
| Upstash Kafka | Serverless | $0.60/100K msgs | Pay-per-use |
| Upstash Kafka | Pro | ~$60 | Fixed + usage |
| Confluent Cloud | Basic | ~$80 + usage | $0.11/GB |
| Confluent Cloud | Standard | ~$200 + usage | Multi-AZ |
| Redpanda Cloud | Serverless | Pay-per-use | $0.10/GB ingested |
| Redpanda Cloud | Dedicated | ~$500+ | Full cluster |

### Redis/Cache

| Provider | Tier | Monthly Cost | Notes |
|----------|------|--------------|-------|
| Upstash Redis | Pay-as-you-go | $0.20/100K cmds | Serverless |
| Upstash Redis | Pro (256MB) | ~$10 | Fixed |
| Google Memorystore | Basic (1GB) | ~$12 | Single node |
| Google Memorystore | Standard (1GB) | ~$36 | HA, replication |
| Redis Cloud | Essentials | ~$7 | 30MB free |
| Redis Cloud | Pro | ~$100+ | Full features |

### NATS (Alternative to Redis for Pub/Sub)

| Provider | Tier | Monthly Cost | Notes |
|----------|------|--------------|-------|
| Self-managed | Single node | ~$6 | 0.1 vCPU + 128Mi |
| Self-managed | 3-node HA cluster | ~$18 | Native clustering |
| Synadia NGS | Free | $0 | 1GB data/mo, dev use |
| Synadia NGS | Developer | ~$15 | 10GB data/mo |
| Synadia NGS | Pro | ~$60+ | Unlimited, HA |

---

## Scaling Scenarios

### Low Traffic (MVP/Early Stage)
- 1 ws-server pod, 1 auth-service pod
- Self-managed everything
- **With Redis: ~$180/mo**
- **With NATS: ~$177/mo**

### Medium Traffic (Growth)
- 2-3 ws-server pods (HPA)
- Hybrid approach recommended
- **With Managed Redis: ~$350-450/mo**
- **With Self-Managed NATS: ~$320-400/mo**

### High Traffic (Scale)
- 5-10 ws-server pods
- Consider managed services for reduced ops burden
- **With Managed Redis + Kafka: ~$600-900/mo**
- **With NATS + Self-Managed Kafka: ~$500-700/mo**

---

## Notes

1. **Egress costs** assume ~50GB staging, ~200GB production monthly
2. **Storage costs** based on SSD persistent disks
3. **HPA scaling** assumes average utilization, not peak
4. **GCP free tier** includes 150GB logs, basic metrics
5. **NATS** requires implementing a new BroadcastBus adapter (~8-12 hours effort)
6. **NATS savings** are modest (~$12/mo) but ops simplicity may justify the switch
7. Prices as of December 2025, subject to change

---

## References

- [GKE Autopilot Pricing](https://cloud.google.com/kubernetes-engine/pricing#autopilot_mode)
- [Memorystore Pricing](https://cloud.google.com/memorystore/docs/redis/pricing)
- [Confluent Cloud Pricing](https://www.confluent.io/confluent-cloud/pricing/)
- [Upstash Pricing](https://upstash.com/pricing)
- [NATS.io](https://nats.io/)
- [Synadia NGS Pricing](https://www.synadia.com/ngs/pricing)
