# Load Balancing & Routing Architecture

This document explains how WebSocket connections are routed from external clients to ws-server shards, the two-layer load balancing strategy, and why session affinity is disabled.

## Overview

Traffic flows through two routing layers, each with its own load balancing strategy:

```
┌──────────────┐     ┌──────────────┐     ┌─────────────────────────────────────────────┐
│              │     │              │     │           ws-server Pod                      │
│   External   │     │  ws-gateway  │     │  ┌─────────────────────────────────────────┐ │
│   Clients    │────▶│   Pods       │────▶│  │    Internal LoadBalancer (port 3001)   │ │
│              │     │              │     │  │                                         │ │
└──────────────┘     └──────────────┘     │  │   Algorithm: Least Connections          │ │
                                          │  │                                         │ │
                            ▲             │  └──────────────┬──────────────────────────┘ │
                            │             │                 │                            │
                     K8s Service          │     ┌───────────┴───────────┐                │
                     Algorithm:           │     │                       │                │
                     Round-Robin          │     ▼                       ▼                │
                                          │  ┌──────────┐         ┌──────────┐          │
                                          │  │ Shard 0  │         │ Shard 1  │          │
                                          │  │(port 3002)│        │(port 3003)│          │
                                          │  └──────────┘         └──────────┘          │
                                          └─────────────────────────────────────────────┘
```

| Layer | Algorithm | Scope |
|-------|-----------|-------|
| **K8s Service → Pod** | Round-robin | Cluster-wide, stateless |
| **Internal LB → Shard** | Least-connections | Pod-local, stateful |

## Layer 1: Kubernetes Service Routing

### How It Works

When `sessionAffinity: None` (default), kube-proxy distributes traffic using round-robin:

```
Gateway request 1 ──▶ ws-server pod 1
Gateway request 2 ──▶ ws-server pod 2
Gateway request 3 ──▶ ws-server pod 1
Gateway request 4 ──▶ ws-server pod 2
...
```

### Why Round-Robin?

| Property | Benefit |
|----------|---------|
| **Stateless** | No connection count tracking across cluster |
| **Fast** | O(1) selection - just pick next pod |
| **Distributed** | Works at iptables/IPVS level, no coordination |
| **Simple** | No failure modes from stale state |

### Why Not Least-Connections at K8s Level?

Least-connections would require synchronizing connection counts across all nodes:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Node A    │     │   Node B    │     │   Node C    │
│  kube-proxy │◄───►│  kube-proxy │◄───►│  kube-proxy │
│             │     │             │     │             │
│ "Pod 1: 500"│     │ "Pod 1: 500"│     │ "Pod 1: 500"│
│ "Pod 2: 300"│     │ "Pod 2: 300"│     │ "Pod 2: 300"│
└─────────────┘     └─────────────┘     └─────────────┘
```

Problems:
- Sync latency adds routing delay
- Stale data causes wrong decisions
- Race conditions when multiple nodes route simultaneously
- Complexity outweighs benefit for long-lived connections

## Layer 2: Internal Shard Routing

### How It Works

Each ws-server pod runs an internal LoadBalancer that uses **least-connections** to route to shards.

From `ws/internal/orchestration/loadbalancer.go`:

```go
// SelectShardByMetrics implements the "least connections" selection algorithm.
func SelectShardByMetrics(shards []ShardMetrics) int {
    var (
        leastConnections int64 = math.MaxInt64
        selectedIndex          = -1
    )

    for i, shard := range shards {
        currentConns := shard.GetCurrentConnections()
        maxConns := int64(shard.GetMaxConnections())

        // Skip shards that are at or over capacity
        if currentConns >= maxConns {
            continue
        }

        // Select shard with fewest connections
        if currentConns < leastConnections {
            leastConnections = currentConns
            selectedIndex = i
        }
    }

    return selectedIndex
}
```

### Algorithm Properties

1. **Real-time**: Uses live connection counts from each shard
2. **Capacity-aware**: Skips shards at max connections
3. **Deterministic**: First shard with lowest count wins (stable ordering)
4. **Fail-safe**: Returns 503 if all shards full

## Why This Two-Layer Design Works

### For Long-Lived WebSocket Connections

Round-robin at the K8s level is effective because:

1. **New connections arrive steadily** - Each gets round-robin'd to different pods
2. **Connection lifetimes are similar** - Users stay connected for similar durations
3. **Natural balancing over time** - 1000 connections distributed evenly

```
Time 0:    Connect → Pod 1
Time 1:    Connect → Pod 2
Time 2:    Connect → Pod 1
...
Time 1000: Pod 1 has ~500, Pod 2 has ~500
```

### Defense in Depth

Even if K8s routing is imperfect, the internal least-connections compensates:

```
K8s Round-Robin (coarse):     Pod 1: ~50%    Pod 2: ~50%
                                 │               │
Internal Least-Conn (fine):   Shard 0: ~25%   Shard 0: ~25%
                              Shard 1: ~25%   Shard 1: ~25%
```

## Session Affinity: Why It's Disabled

### What Session Affinity Does

`sessionAffinity: ClientIP` routes all requests from the same source IP to the same pod.

### The Problem with Gateway Proxy

The ws-gateway acts as a reverse proxy. From ws-server's perspective, the "client" is the gateway pod IP, not the actual user:

```
                        Gateway sees:           ws-server sees:
User 1.2.3.4    ─┐
User 5.6.7.8    ─┼──▶ Gateway pod A ──────────▶ Source: 10.0.1.5
User 9.10.11.12 ─┘    (IP: 10.0.1.5)            (gateway's IP)
```

With session affinity enabled:

```
Gateway pod A (IP 10.0.1.5) ──▶ ws-server pod 1 (ALWAYS)
Gateway pod B (IP 10.0.1.6) ──▶ ws-server pod 2 (ALWAYS)
```

**Result**: 1:1 mapping between gateway and ws-server pods. If most traffic goes through Gateway A, ws-server pod 1 gets overloaded while pod 2 sits idle.

### Observed Symptoms

```
ws-server pod h574k: 0 connections      ← Idle (starving)
ws-server pod p74l2: 2,190 connections  ← Overloaded (503 errors)
```

### The Fix

Disable session affinity in `values.yaml`:

```yaml
sessionAffinity:
  enabled: false  # Disabled - gateway proxy makes this counterproductive
```

Now traffic distributes evenly via round-robin.

### When Session Affinity IS Useful

Session affinity works when:
- Clients connect **directly** to the service (no proxy)
- Application stores session state **in-memory** on the pod
- You need to avoid reconnection overhead

It does NOT work when:
- A proxy/gateway sits in front (all traffic appears from proxy IP)
- State is shared via external store (NATS, Redis, database)

## Reconnection Behavior

With session affinity disabled, reconnecting clients may hit different pods/shards:

1. Client disconnects
2. Client reconnects
3. K8s routes to **any** ws-server pod (round-robin)
4. Internal LB routes to **least-loaded shard**

**Does this cause issues?** No, because:
- Subscriptions are stateless (client re-subscribes on connect)
- No shard-local state persists across connections
- Broadcast messages reach ALL shards via NATS anyway

## Alternatives for Advanced Load Balancing

If round-robin proves insufficient for your workload:

### IPVS Mode with Least-Connection

```bash
# Configure kube-proxy to use IPVS
kube-proxy --proxy-mode=ipvs --ipvs-scheduler=lc
```

IPVS supports least-connection at kernel level.

### Ingress Controller

NGINX or Envoy can do application-aware load balancing:

```yaml
# NGINX Ingress annotation
nginx.ingress.kubernetes.io/load-balance: "least_conn"
```

### Service Mesh

Istio or Linkerd provide:
- Least-requests routing
- Weighted distribution
- Circuit breaking
- Retry policies

## Configuration Reference

### Helm Values

```yaml
# deployments/k8s/helm/sukko/charts/ws-server/values.yaml
sessionAffinity:
  enabled: false        # MUST be false when using gateway proxy
  timeoutSeconds: 10800 # Only applies if enabled

config:
  shards: 2             # Number of shards per pod
  basePort: 3002        # Shard 0 on 3002, Shard 1 on 3003, etc.
  lbAddr: ":3001"       # Internal load balancer address
```

### Key Source Files

| File | Purpose |
|------|---------|
| `ws/internal/orchestration/loadbalancer.go` | Internal LB, least-connections algorithm |
| `ws/internal/orchestration/proxy.go` | ShardProxy WebSocket forwarding |
| `ws/internal/orchestration/shard.go` | Shard connection tracking |

## Summary

| Aspect | Design Choice | Rationale |
|--------|---------------|-----------|
| K8s routing | Round-robin | Simple, stateless, effective for long-lived connections |
| Session affinity | Disabled | Gateway proxy breaks ClientIP-based routing |
| Shard routing | Least-connections | Fine-grained balancing within each pod |
| Reconnection | Stateless | Re-subscribe on connect, no persistent shard state |
