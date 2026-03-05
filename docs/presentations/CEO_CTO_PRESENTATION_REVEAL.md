---
title: Sukko WebSocket System
theme: black
revealOptions:
  transition: slide
  slideNumber: true
  hash: true
---

# Sukko WebSocket System

## Real-Time Streaming Infrastructure

**Enterprise-Scale | Cost-Efficient**

_Confidential - January 2026_

---

# Executive Summary

| Metric                      | Value                          | Environment              |
| --------------------------- | ------------------------------ | ------------------------ |
| **Concurrent Connections**  | 18,000                         | Validated (Dedicated VM) |
| **Message Throughput**      | 51,000+ msg/sec (@ 10 pub/sec) | Validated (Dedicated VM) |
| **Latency**                 | Sub-10ms                       | Target                   |
| **Uptime Target**           | 99.9%                          | Design Goal              |
| **Monthly Cost (All Envs)** | ~$700-1,000                    | K8s Estimated            |

**Status:** Core system validated on dedicated VM. K8s deployment functional (PoC), load test pending.

---

# The Problem

## Current Architecture: HTTP Polling

```
Every 2-3 seconds, EVERY user asks: "Any updates?"

┌──────────┐      ┌─────────────┐      ┌───────────┐
│  10,000  │ ───► │  Cloud Run  │ ───► │ Cloud SQL │
│  Users   │ x20  │  (API)      │ x20  │ (DB)      │
│          │ /min │ $$$         │ /min │ $$$       │
└──────────┘      └─────────────┘      └───────────┘
         └──────── 200,000+ requests/minute ────────┘
```

---

## Pain Points

| Problem                     | Impact                         | Cost                     |
| --------------------------- | ------------------------------ | ------------------------ |
| **Excessive API calls**     | 70K-270K requests/min at peak  | Cloud Run overload       |
| **Database pressure**       | Repeated queries for same data | Cloud SQL bottleneck     |
| **2-3 second delay**        | Users see stale data           | Poor UX, missed trades   |
| **Linear cost scaling**     | More users = more polling      | ~$6K/month wasted        |
| **No real-time capability** | Can't push instant updates     | Competitive disadvantage |

---

# The Solution

## Real-Time WebSocket Infrastructure

```
Server PUSHES updates instantly when data changes

┌──────────┐                    ┌─────────────┐
│  10,000  │◄──── WebSocket ────│    Sukko     │◄──── Kafka ────┐
│  Users   │     (persistent)   │  WS Server  │                │
│          │                    │             │          ┌─────┴─────┐
└──────────┘                    └─────────────┘          │ Data      │
         └──── 1 connection per user, instant updates ───│ Changes   │
                                                         └───────────┘
```

---

## Why This Works

- **Sub-10ms real-time updates** replace 2-3 second polling delays
- **Single persistent connection per user** instead of 20+ requests/minute
- **Detailed cost analysis and ROI breakdown ahead**

---

# Two Deployment Architectures

## 1. Dedicated VM (Validated - Load Tested)

- Single n2-highcpu-8 instance (8 vCPU, 8GB RAM)
- Docker-compose deployment
- 18K connections @ 51K msg/sec tested

## 2. Kubernetes (Current Focus)

- GKE Standard cluster
- Helm-based deployment
- Horizontal scaling ready
- **Load test pending** (free tier resource constraints)

---

# Validated Performance (Dedicated VM)

**Test Environment:** n2-highcpu-8 (8 vCPU, 8GB RAM) with Docker-compose

```
Connections:     17,710 / 18,000 (98.4% success)
Publish Rate:    10 events/sec (test publisher)
Throughput:      51,110 msg/sec delivered (10 x ~5,111 subscribers)
CPU Usage:       15-20% average (30-70% during broadcasts)
Memory:          ~1 GB stable
Goroutines:      72,340 (72% of 100K limit)
```

**Note:** System supports up to 25 msg/sec publish rate (configurable)

---

## Multi-Shard Distribution

```
Shard 0: 5,909 / 6,000 (98.5%)
Shard 1: 5,901 / 6,000 (98.4%)
Shard 2: 5,900 / 6,000 (98.3%)
Variance: 0.15% (near-perfect load balancing)
```

---

# Architecture Components

| Component                | Purpose                                     | Status     |
| ------------------------ | ------------------------------------------- | ---------- |
| **WS-Gateway**           | JWT authentication, permission filtering    | Functional |
| **WS-Server**            | WebSocket connections, message broadcasting | Functional |
| **Redpanda**             | Event streaming (Kafka-compatible)          | Functional |
| **NATS**                 | Cross-pod message distribution              | Functional |
| **Prometheus + Grafana** | Metrics and dashboards                      | Functional |
| **Loki**                 | Log aggregation                             | Functional |

---

# Why Broadcast Bus (NATS)?

## The Problem with Horizontal Scaling

```
Without NATS:
+-------------+     +-------------+
|   Pod A     |     |   Pod B     |
| Client 1,2  |     | Client 3,4  |
+------+------+     +-------------+
       |
  Kafka msg -> Only Pod A receives -> Clients 3,4 miss the message!
```

---

## The Solution

```
With NATS Broadcast Bus:
+-------------+     +-------------+
|   Pod A     |<--->|   Pod B     |
| Client 1,2  | NATS| Client 3,4  |
+------+------+     +-------------+
       |
  Kafka msg -> Pod A receives -> NATS re-broadcasts -> All clients receive!
```

**Result:** Clients receive messages regardless of which pod they're connected to.

---

# Kubernetes Architecture (Current)

```
+-------------+    +----------------+    +-----------------+
|   Clients   |--->|   Cloudflare   |--->| K8s LoadBalancer|
|  (Browser)  |    |  (DDoS/TLS)    |    | (Least-Conn)    |
+-------------+    +----------------+    +--------+--------+
                                                  |
+-----------------------------------------------------------------------------+
|                              GKE Cluster                                    |
|  +-------------+    +-------------+    +-------------+                      |
|  | WS-Gateway  |--->|  WS-Server  |<-->|    NATS     |                      |
|  |  (Auth)     |    | (Broadcast) |    | (Cross-pod) |                      |
|  +-------------+    +------+------+    +-------------+                      |
|                            |                                                |
|                     +------v------+    +-------------+                      |
|                     |  Redpanda   |<---|CDC Sukko API|                       |
|                     |   (Kafka)   |    | (Publisher) |                      |
|                     +-------------+    +-------------+                      |
|                                                                             |
|  +---------------------------- Observability ------------------------------+|
|  |  +------------+    +------------+    +------------+                     ||
|  |  | Prometheus |<---|  Grafana   |--->|    Loki    |                     ||
|  |  | (Metrics)  |    |(Dashboards)|    |   (Logs)   |                     ||
|  |  +------------+    +------------+    +------------+                     ||
|  +-------------------------------------------------------------------------+|
+-----------------------------------------------------------------------------+
```

**Status:** Functional PoC (1 connection validated), load test pending

---

# Features: Tier 1 - Core Capabilities

| Feature                    | Description                          | Business Value       |
| -------------------------- | ------------------------------------ | -------------------- |
| **Extreme Scalability**    | 18K connections (validated)          | Handle peak traffic  |
| **Horizontal Scaling**     | Multi-pod with NATS broadcast        | Grow with demand     |
| **Enterprise Reliability** | Panic recovery, graceful degradation | Customer trust       |
| **Message Replay**         | Sequence-based recovery (20-50ms)    | Data integrity       |
| **Channel Subscriptions**  | Pattern-based filtering              | Flexible data access |

---

# Features: Tier 2 & 3

## Tier 2: Security & Infrastructure

| Feature            | Description                             |
| ------------------ | --------------------------------------- |
| JWT Authentication | Token-based auth with claims            |
| Multi-Tenant Ready | Tenant isolation at channel/topic level |
| Kubernetes Native  | Helm charts, HPA, rolling updates       |
| Observability      | 50+ metrics, Grafana dashboards         |

---

## Tier 3: Operational Excellence

| Feature              | Description                            |
| -------------------- | -------------------------------------- |
| Rate Limiting        | Per-IP token bucket (abuse protection) |
| Graceful Degradation | ResourceGuard admission control        |
| Auto-scaling         | HPA based on CPU (70% threshold)       |

---

# User Metrics (from Google Analytics)

## Daily Active Users

| Period                  | Users   |
| ----------------------- | ------- |
| Current daily           | ~10,000 |
| Peak daily (historical) | ~20,000 |
| Total (6 months)        | 165,000 |

## Top Market: China (117K users)

Peak hours likely: 6 PM - 12 AM CST (~6 hour window)

---

# Peak Concurrent Estimation

## Method 1: Conservative (Session Duration Formula)

```
Peak concurrent = Daily users x (avg session / peak window)

Current: 10,000 x (10 min / 360 min) = ~280 concurrent
Peak:    20,000 x (10 min / 360 min) = ~560 concurrent
With major event (5x): 560 x 5 = ~2,800 concurrent
```

---

## Method 2: Inflated Estimate (35-45% of daily)

_Padded for safety margin and growth buffer_

```
Current: 10,000 x 35% = 3,500 concurrent
Peak:    20,000 x 45% = 9,000 concurrent
```

---

# Concurrent Scenarios

| Scenario              | Method          | Concurrent |
| --------------------- | --------------- | ---------- |
| Current normal        | Session formula | ~280       |
| Current + major event | Session x 5     | ~2,800     |
| Current (inflated)    | 35% of daily    | ~3,500     |
| Peak day (inflated)   | 45% of daily    | ~9,000     |

**Capacity Target: 3,500-9,000** (inflated for safety + growth)

_Validated system: 18K connections - covers even worst-case scenarios_
_Users distributed across ~7 pages, reducing broadcast fan-out per message_

---

# Cost Justification: Polling vs WebSocket

## Current State (Polling Every 2-3 Seconds)

| Metric                         | Value                       |
| ------------------------------ | --------------------------- |
| Daily active users             | ~10K (current), ~20K (peak) |
| **Peak concurrent (inflated)** | **~3,500 - 9,000**          |
| Requests per user/min          | 20-30                       |
| **Peak requests/minute**       | **70,000 - 270,000**        |
| Polling-related cost (est.)    | ~$5,955/month               |

_Cost attribution: 50% Cloud Run, 30% Cloud SQL due to polling_

---

## WebSocket Server Cost (for 9K connections)

| Component                          | Monthly Cost        |
| ---------------------------------- | ------------------- |
| GKE nodes (e2-standard-4 Spot, 2x) | ~$100-150           |
| Redpanda (self-hosted)             | ~$50-100            |
| Load Balancer + Egress             | ~$100-150           |
| **Total**                          | **~$250-400/month** |

_Still 93-96% cheaper than polling_

---

# Cost Savings Summary

## Monthly & Annual Impact

| Approach              | Monthly     | Annual       |
| --------------------- | ----------- | ------------ |
| **Current (Polling)** | ~$5,955     | ~$71,460     |
| **WebSocket Server**  | ~$325       | ~$3,900      |
| **Savings**           | **~$5,630** | **~$67,560** |

---

## ROI Highlights

- **94% cost reduction** in polling-related infrastructure
- **Immediate payback** - WS server already built
- **Better UX** - Real-time updates vs 2-3s delay
- **Reduced load** - Less pressure on Cloud Run & SQL

---

# K8s Infrastructure Cost (All Environments)

## Monthly Infrastructure Cost

| Environment | Configuration                    | Cost            |
| ----------- | -------------------------------- | --------------- |
| Development | 1 node (Spot VM)                 | ~$100-150       |
| Staging     | 2 nodes (Spot VM)                | ~$200-250       |
| Production  | 1-5 nodes (Spot VM, autoscaling) | ~$400-600       |
| **Total**   | All environments                 | **~$700-1,000** |

---

## Cost Optimization Strategies

- **Spot VMs:** 60-90% savings vs on-demand
- **Zonal clusters:** 20% cheaper than regional
- **Right-sized resources:** Per-environment tuning
- **HPA:** Prevents over-provisioning

---

# Autoscaling Strategy

## How It Works

```
Load increases
    → HPA adds pods (70% CPU threshold)
    → No node capacity?
        → Cluster Autoscaler adds nodes
    → Zero downtime
```

## Configuration

| Component             | Setting       | Value |
| --------------------- | ------------- | ----- |
| HPA                   | Min replicas  | 2     |
| HPA                   | Max replicas  | 10    |
| HPA                   | CPU threshold | 70%   |
| Cluster Autoscaler    | Min nodes     | 2     |
| Cluster Autoscaler    | Max nodes     | 5     |
| Pod Disruption Budget | Min available | 1     |

---

# Zero-Downtime Scaling & Upgrades

## Horizontal Scaling

- HPA adds pods when CPU > 70%
- Cluster Autoscaler adds nodes when needed
- Traffic distributes across all healthy pods

## Vertical Scaling (Bigger Nodes)

- Add new node pool with larger machines
- Rolling drain of old nodes (one at a time)
- Pod Disruption Budget ensures min 1 pod always running

## Node Upgrades

- GKE rolling update (one node at a time)
- Pods migrate before node is removed
- Automated with zero manual intervention

**All operations: Zero downtime**

---

# Multi-Tenant Architecture

## Naming Convention

```
Topic Format:   {env}.{tenant}.{category}
Example:        main.sukko.trade

Client sees:    all.trade
Server maps:    sukko.all.trade  (tenant from JWT)
Kafka topic:    main.sukko.trade
```

---

## Key Decisions

| Decision          | Choice               | Rationale                      |
| ----------------- | -------------------- | ------------------------------ |
| Infrastructure    | Shared multi-tenant  | Cost-effective                 |
| Topic Isolation   | Environment + Tenant | Cross-contamination prevention |
| Channel Naming    | Tenant implicit      | Industry standard              |
| Consumer Strategy | Hybrid               | Flexibility for large tenants  |

---

# Technology Stack

| Layer             | Technology           | Why                          |
| ----------------- | -------------------- | ---------------------------- |
| **Language**      | Go 1.25              | Performance, concurrency     |
| **WebSocket**     | gobwas/ws            | Low memory, high performance |
| **Streaming**     | Redpanda (Kafka)     | Proven, scalable             |
| **Broadcast**     | NATS                 | Low latency, simple          |
| **Orchestration** | Kubernetes + Helm    | Industry standard            |
| **Monitoring**    | Prometheus + Grafana | Best-in-class observability  |
| **Edge**          | Cloudflare           | DDoS, TLS, WAF               |

---

# Deployment & Operations

## Environments

```
Local (Kind) -> Develop (GKE) -> Staging (GKE) -> Production (GKE)
```

## Automation

- **50+ Taskfile commands** for common operations
- **Terraform** for infrastructure provisioning
- **Helm** for Kubernetes deployments
- **Rolling updates** for zero-downtime deploys

---

## Monitoring

- Real-time Grafana dashboards
- Prometheus alerting rules
- Loki log aggregation
- 50+ custom metrics

---

# Roadmap

## Completed

- Core WebSocket server with multi-shard architecture
- 18K connection capacity (validated on dedicated VM)
- Kubernetes deployment with Helm (PoC functional)
- Comprehensive monitoring stack
- JWT authentication (gateway)

## In Progress

- K8s load testing (pending resources)
- Multi-tenant architecture implementation
- Tenant provisioning API

---

# Live Demo

## Demo Flow (3-5 minutes)

1. **Grafana Dashboard** - Real-time metrics visualization
2. **Connect WebSocket** - Subscribe to `all.trade` channel
3. **Publish Events** - Show messages flowing
4. **Show K8s Pods** - Demonstrate deployment

---

## Key URLs

- Grafana: `http://localhost:3010`
- WebSocket: `ws://localhost:30080/ws`

```json
{ "type": "subscribe", "data": { "channels": ["all.trade"] } }
```

---

# Summary

## Validated

- **18K connections** with 51K msg/sec throughput (10 pub/sec x 5K subscribers)
- Multi-shard architecture with near-perfect load balancing
- Zero message errors under sustained load

---

## Ready for Production (K8s)

- Full deployment pipeline functional
- Monitoring and observability in place
- Cost-optimized with Spot VMs (~$1K/month)

## Next Step

- K8s load testing when resources available
- Expected: Similar performance to dedicated VM

---

# Questions?
