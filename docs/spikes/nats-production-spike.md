# Spike: NATS Production Best Practices

**Date:** 2025-12-30
**Author:** Engineering Team
**Status:** Research Complete
**Time-box:** 2 hours

---

## Objective

Evaluate best practices for running NATS in production on Kubernetes, including whether to use an operator, the official Helm chart, or continue with the current custom subchart.

---

## Background

### Current State

| Environment | NATS Deployment | Replicas | Cluster Mode | JetStream |
|-------------|-----------------|----------|--------------|-----------|
| Local (kind) | Custom Helm subchart | 1 | Disabled | Disabled |
| Develop (GKE) | Custom Helm subchart | 1 | Disabled | Disabled |
| Staging | Custom Helm subchart | 1 | Disabled | Disabled |
| Production | Custom Helm subchart | 1 | Disabled | Disabled |

### Current Resource Allocation

```yaml
resources:
  requests:
    cpu: "100m"
    memory: "64Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"
```

### How NATS is Used in Odin

NATS serves as the **BroadcastBus** for ws-server:
- Pub/sub messaging between ws-server replicas
- No persistence required (ephemeral messages)
- Low latency requirement for real-time updates
- Message types: connection state, subscription updates, broadcast fan-out

---

## Research Findings

### 1. NATS Operator Status

**The NATS Operator is officially deprecated.**

From the [nats-io/nats-operator](https://github.com/nats-io/nats-operator) repository:

> "The recommended way of running NATS on Kubernetes is by using the Helm charts. If looking for JetStream support, this is supported in the Helm charts."

**Recommendation:** Do not use the NATS Operator for new deployments.

### 2. Deployment Options

| Option | Status | Recommendation |
|--------|--------|----------------|
| NATS Operator | Deprecated | Do not use |
| Official NATS Helm Chart | Active, v1.0.0+ | Recommended |
| Custom Helm Subchart | Current | Migrate away |
| Bitnami NATS Chart | Active | Alternative option |

### 3. Official NATS Helm Chart Features

The [official NATS Helm chart](https://github.com/nats-io/k8s) (v1.0.0+) provides:

- **Clustering support** - Built-in HA configuration
- **JetStream** - Persistent streaming with proper health checks
- **GOMAXPROCS tuning** - Automatic CPU optimization
- **GOMEMLIMIT** - Memory limit configuration
- **TLS** - Certificate management helpers
- **Topology spread** - Pod distribution across nodes/zones
- **Merge/Patch model** - Flexible configuration overrides

### 4. NACK: Kubernetes-Native Stream Management

[NACK](https://github.com/nats-io/nack) (NATS Controllers for Kubernetes) provides CRDs for JetStream resources:

```yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Stream
metadata:
  name: orders
spec:
  subjects: ["orders.*"]
  retention: limits
  storage: file
  replicas: 3
---
apiVersion: jetstream.nats.io/v1beta2
kind: Consumer
metadata:
  name: order-processor
spec:
  streamName: orders
  deliverPolicy: all
  ackPolicy: explicit
```

**Note:** NACK is only needed if using JetStream for persistent streams. For pure pub/sub (BroadcastBus), it's not required.

### 5. Resource Configuration Best Practices

#### CPU Configuration

NATS automatically sets `GOMAXPROCS` based on CPU limits. Mismatched requests/limits cause performance issues.

```yaml
# BAD - GOMAXPROCS will be set to 4, but only 0.5 CPU available
resources:
  requests:
    cpu: "500m"
  limits:
    cpu: "4"

# GOOD - Guaranteed QoS, predictable performance
resources:
  requests:
    cpu: "2"
  limits:
    cpu: "2"
```

#### Memory Configuration

Set `GOMEMLIMIT` to ~80% of memory limit to prevent OOM kills:

```yaml
container:
  env:
    GOMEMLIMIT: 6GiB
  merge:
    resources:
      requests:
        memory: 8Gi
      limits:
        memory: 8Gi
```

#### Production Sizing Recommendations

| Use Case | CPU | Memory | Notes |
|----------|-----|--------|-------|
| Development | 100m | 64Mi | Single replica, no clustering |
| Light Production | 500m | 256Mi | 3 replicas, clustering |
| Standard Production | 1-2 cores | 2-4Gi | 3 replicas, JetStream |
| High Throughput | 2-4 cores | 8Gi+ | 5 replicas, JetStream |

### 6. Cluster Configuration

#### Minimum HA Setup (3 replicas)

```yaml
config:
  cluster:
    enabled: true
    replicas: 3
```

#### Why 3 or 5 Replicas?

JetStream uses RAFT consensus algorithm:
- Requires odd number of nodes for quorum
- 3 nodes: tolerates 1 failure
- 5 nodes: tolerates 2 failures

For pure pub/sub without JetStream, 3 replicas still provides HA.

### 7. Topology Spread Constraints

Distribute NATS pods across nodes and zones:

```yaml
podTemplate:
  topologySpreadConstraints:
    kubernetes.io/hostname:
      maxSkew: 1
      whenUnsatisfiable: DoNotSchedule
    topology.kubernetes.io/zone:
      maxSkew: 1
      whenUnsatisfiable: ScheduleAnyway
```

### 8. Health Checks

The official Helm chart v1.0.0+ includes improved health checks:

- **Liveness:** Basic NATS server health
- **Readiness:** JetStream catch-up complete (if enabled)
- **Startup:** Initial boot sequence

Custom health check endpoints:
- `/healthz` - Basic health
- `/healthz?js-enabled-only=true` - JetStream ready

### 9. TLS Configuration

For production with TLS:

```yaml
config:
  nats:
    tls:
      enabled: true
      secretName: nats-server-tls
      # SANs should cover:
      # - nats.<namespace>.svc.cluster.local
      # - *.nats-headless.<namespace>.svc.cluster.local
```

### 10. Monitoring

NATS exposes Prometheus metrics on port 7777:

```yaml
config:
  monitor:
    enabled: true
    port: 7777

promExporter:
  enabled: true
  # Scrape endpoint: :7777/metrics
```

Key metrics:
- `nats_connz_total` - Total connections
- `nats_routez_num_routes` - Cluster routes
- `nats_varz_cpu` - CPU usage
- `nats_varz_mem` - Memory usage
- `jetstream_*` - JetStream metrics (if enabled)

---

## Comparison: Current vs Recommended

| Aspect | Current Setup | Recommended (Staging/Prod) |
|--------|---------------|---------------------------|
| Helm Chart | Custom subchart | Official nats/nats |
| Replicas | 1 | 3 |
| Cluster Mode | Disabled | Enabled |
| JetStream | Disabled | Optional (not needed for BroadcastBus) |
| CPU Request | 100m | 500m - 1 core |
| CPU Limit | 500m | Same as request |
| Memory Request | 64Mi | 256Mi - 512Mi |
| Memory Limit | 256Mi | Same as request |
| QoS Class | Burstable | Guaranteed |
| Topology Spread | None | Configured |
| TLS | Disabled | Optional (internal traffic) |
| Monitoring | Basic | Prometheus metrics |

---

## Key Questions Answered

### Q1: Should we use NATS Operator?

**No.** The NATS Operator is deprecated. Use the official Helm chart instead.

### Q2: Do we need JetStream?

**No, for current use case.** BroadcastBus uses pure pub/sub with ephemeral messages. JetStream adds persistence overhead that isn't needed.

However, JetStream could be useful for:
- Message replay on reconnection
- Guaranteed delivery
- Stream processing

### Q3: Do we need NACK (CRDs)?

**No, for current use case.** NACK is for managing JetStream streams/consumers declaratively. Since we're using pure pub/sub, it's not needed.

### Q4: What's the migration path from custom chart?

1. Deploy official Helm chart alongside existing NATS
2. Update ws-server to use new NATS service
3. Verify connectivity and message flow
4. Remove custom NATS deployment

Minimal downtime if done with proper service DNS updates.

### Q5: What resources do we actually need?

For BroadcastBus with ~50k connections across cluster:

| Environment | CPU | Memory | Replicas |
|-------------|-----|--------|----------|
| Local | 100m | 64Mi | 1 |
| Develop | 100m | 64Mi | 1 |
| Staging | 250m | 128Mi | 3 |
| Production | 500m | 256Mi | 3 |

These are conservative estimates. NATS is very efficient for pub/sub.

---

## Recommendation

### Environments

| Environment | Action | Rationale |
|-------------|--------|-----------|
| Local | Keep custom chart | Simplicity, minimal resources |
| Develop | Keep custom chart | Simplicity, single replica OK |
| Staging | Migrate to official chart | Test HA before production |
| Production | Migrate to official chart | HA, proper resource management |

### Priority

**Medium priority.** Current NATS setup works, but lacks HA. Migration is straightforward and low-risk.

Recommended order:
1. Migrate Redpanda to Operator (higher complexity, higher value)
2. Migrate NATS to official chart (lower complexity, medium value)

### Implementation Approach

#### Option A: Use Official Chart as Dependency

Add to `Chart.yaml`:
```yaml
dependencies:
  - name: nats
    version: "1.2.x"
    repository: "https://nats-io.github.io/k8s/helm/charts/"
    condition: nats.enabled
```

#### Option B: Keep Custom Chart, Add HA

Update existing custom chart with:
- Clustering support
- Proper resource configuration
- Topology spread constraints

**Recommendation:** Option A - Use official chart. It's maintained, tested, and follows best practices.

---

## Example Production Configuration

```yaml
# values/standard/production.yaml (using official chart)
nats:
  enabled: true

  config:
    cluster:
      enabled: true
      replicas: 3

    # Pure pub/sub, no JetStream needed for BroadcastBus
    jetstream:
      enabled: false

  container:
    merge:
      resources:
        requests:
          cpu: "500m"
          memory: "256Mi"
        limits:
          cpu: "500m"
          memory: "256Mi"

  podTemplate:
    topologySpreadConstraints:
      kubernetes.io/hostname:
        maxSkew: 1
        whenUnsatisfiable: DoNotSchedule

  # Prometheus metrics
  promExporter:
    enabled: true
```

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Service discovery change | Medium | Medium | Update NATS_URLS before migration |
| Message loss during migration | Low | Low | BroadcastBus is ephemeral, acceptable |
| Resource sizing wrong | Low | Medium | Monitor and adjust post-migration |
| Cluster split-brain | Low | High | Use 3+ replicas, proper topology spread |

---

## Estimated Effort

| Task | Effort |
|------|--------|
| Add official chart as dependency | 1 hour |
| Configure for staging | 1 hour |
| Test in staging | 2 hours |
| Configure for production | 1 hour |
| Migration + validation | 2 hours |
| Documentation | 1 hour |
| **Total** | ~8 hours |

---

## Next Steps

1. [ ] Decide: Use official chart vs enhance custom chart
2. [ ] Update Chart.yaml with official NATS dependency
3. [ ] Create staging values with clustering enabled
4. [ ] Test ws-server connectivity with clustered NATS
5. [ ] Deploy to staging, run load tests
6. [ ] Create production values
7. [ ] Schedule production migration

---

## References

- [NATS Operator (Deprecated)](https://github.com/nats-io/nats-operator)
- [Official NATS Helm Charts](https://github.com/nats-io/k8s)
- [NATS Kubernetes Documentation](https://docs.nats.io/running-a-nats-service/nats-kubernetes)
- [NATS Helm Chart values.yaml](https://github.com/nats-io/k8s/blob/main/helm/charts/nats/values.yaml)
- [NACK - JetStream Controllers](https://github.com/nats-io/nack)
- [NATS 1.0.0 Helm Chart Release](https://nats.io/blog/nats-helm-chart-1.0.0-rc/)
- [Production NATS + JetStream Guide](https://thamizhelango.medium.com/deploying-production-ready-real-time-messaging-with-nats-and-jetstream-on-kubernetes-0a611fa2f6f4)
