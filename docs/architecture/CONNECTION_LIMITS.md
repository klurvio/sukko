# WebSocket Connection Limits

This document describes the connection limit configuration for the odin-ws WebSocket infrastructure, including industry standards, our implementation, and environment-specific settings.

## Overview

WebSocket connection limits are configured at three levels:

| Level | What | Purpose |
|-------|------|---------|
| **Node (Kernel)** | sysctl parameters | Remove infrastructure ceiling |
| **Pod (Container)** | ulimits | Define per-instance target |
| **Application** | Code-level limits | Safety valve |

## Industry Standards

### Connections Per Instance Benchmarks

| Company/Technology | Connections/Instance | Language | Notes |
|--------------------|---------------------|----------|-------|
| Discord | ~100K | Elixir/Erlang | Designed for massive concurrency |
| Slack | 10-50K | Java/Go | Conservative, high reliability |
| Pusher/Ably | 10-50K | Go | WebSocket PaaS providers |
| Phoenix (Elixir) | 2M+ | Elixir | Famous benchmark, extreme case |
| Socket.io | 5-15K | Node.js | Single-threaded limitation |
| Go (typical) | 10-50K | Go | Sweet spot for most use cases |

### Industry Consensus

- **Node level**: Set limits very high to remove infrastructure bottlenecks
- **Pod level**: Define target capacity (10-20K for Go servers)
- **Scale horizontally**: Many small pods preferred over few large pods

### Limiting Factors

1. **File Descriptors**: 1 connection = 1 FD (default 1024, must be raised)
2. **Memory**: ~10-50KB per connection (buffers, state)
3. **Ephemeral Ports**: ~64K ports per IP (client-side limit)
4. **CPU**: Message processing (not connection holding) is usually the bottleneck

## Configuration Levels

### 1. Node Level (Kernel Tuning)

Applied via privileged DaemonSet in Kubernetes (runs on every node):

```bash
# File descriptor limits
sysctl -w fs.file-max=2097152

# TCP/Network tuning
sysctl -w net.ipv4.ip_local_port_range="1024 65535"
sysctl -w net.ipv4.tcp_tw_reuse=1
sysctl -w net.core.somaxconn=65535
sysctl -w net.ipv4.tcp_max_syn_backlog=65535
sysctl -w net.core.netdev_max_backlog=65535

# TCP memory tuning
sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
```

**Location**: `deployments/k8s/helm/odin/templates/kernel-tuning-daemonset.yaml`

**Configuration**: `kernelTuning.enabled` in values.yaml (default: true)

### 2. Pod Level (Container ulimits)

Applied via Kubernetes securityContext in Helm charts:

```yaml
securityContext:
  capabilities:
    add: ["SYS_RESOURCE"]

# Pod spec
containers:
  - name: ws-server
    securityContext:
      capabilities:
        drop: ["ALL"]
    # ulimits set via init container or runtime
```

**Location**: `deployments/k8s/helm/odin/charts/ws-server/values.yaml`

### 3. Application Level

Go server configuration for connection limits:

```go
// Max concurrent connections per shard
maxConnections: 10000

// Connection buffer sizes
readBufferSize: 4096
writeBufferSize: 4096
```

## Environment Configuration

### Develop

| Setting | Value | Rationale |
|---------|-------|-----------|
| Node type | e2-standard-4 | Cost-effective for testing |
| Node count | 2 | Minimal HA |
| Kernel tuning | Enabled | Full capacity testing |
| Pod ulimit (nofile) | 65536 | Standard high limit |
| Target connections/pod | 10,000 | Conservative for dev |
| Total cluster capacity | ~40,000 | 2 nodes × 2 pods × 10K |

### Staging

| Setting | Value | Rationale |
|---------|-------|-----------|
| Node type | e2-standard-4 | Match production specs |
| Node count | 2-3 | Test scaling |
| Kernel tuning | Enabled | Production parity |
| Pod ulimit (nofile) | 65536 | Standard high limit |
| Target connections/pod | 15,000 | Near-production load |
| Total cluster capacity | ~90,000 | 3 nodes × 2 pods × 15K |

### Production

| Setting | Value | Rationale |
|---------|-------|-----------|
| Node type | e2-standard-4 | Balanced cost/performance |
| Node count | 3+ (autoscaling) | High availability |
| Kernel tuning | Enabled | Maximum capacity |
| Pod ulimit (nofile) | 65536 | Standard high limit |
| Target connections/pod | 20,000 | Optimal for Go |
| Total cluster capacity | ~120,000+ | Scales with nodes |

## Capacity Planning

### Memory Requirements

| Connections | Memory per Pod | Notes |
|-------------|----------------|-------|
| 10,000 | ~1-2 GB | Development workloads |
| 20,000 | ~2-4 GB | Production workloads |
| 50,000 | ~4-8 GB | High-density deployment |

Formula: `connections × 50KB buffer + base overhead (~500MB)`

### Pod Sizing Recommendations

```yaml
# Development
resources:
  requests:
    cpu: "500m"
    memory: "1Gi"
  limits:
    cpu: "2"
    memory: "2Gi"

# Production
resources:
  requests:
    cpu: "1"
    memory: "2Gi"
  limits:
    cpu: "2"
    memory: "4Gi"
```

### Horizontal vs Vertical Scaling

**Recommended: Horizontal Scaling**

| Approach | Pros | Cons |
|----------|------|------|
| Horizontal (many small pods) | Easy failure recovery, better bin-packing, simpler deploys | More pods to manage |
| Vertical (few large pods) | Fewer components, simpler routing | Larger blast radius on failure |

**Target: 10-20K connections per pod, scale pod count as needed**

## Monitoring

### Key Metrics

| Metric | Alert Threshold | Action |
|--------|-----------------|--------|
| `ws_connections_active` | >80% of limit | Scale horizontally |
| `container_memory_usage_bytes` | >80% of limit | Increase memory or scale |
| `container_cpu_usage_seconds` | >70% sustained | Check message throughput |
| `ws_connection_errors_total` | >1% of attempts | Investigate errors |

### Grafana Dashboard Queries

```promql
# Active connections per pod
sum(ws_connections_active) by (pod)

# Connection utilization percentage
ws_connections_active / ws_connections_limit * 100

# Memory per connection
container_memory_usage_bytes / ws_connections_active
```

## Implementation Files

| File | Purpose |
|------|---------|
| `deployments/k8s/helm/odin/templates/kernel-tuning-daemonset.yaml` | Node kernel tuning via DaemonSet |
| `deployments/k8s/helm/odin/charts/ws-server/values.yaml` | Pod connection limits, resources |
| `deployments/k8s/helm/odin/charts/ws-gateway/values.yaml` | Gateway connection limits |
| `deployments/k8s/helm/odin/values.yaml` | Base configuration |
| `deployments/k8s/helm/odin/values-{env}.yaml` | Environment-specific overrides |

## Troubleshooting

### "Too many open files" Error

1. Check kernel tuning DaemonSet is running:
   ```bash
   kubectl get daemonset -l app.kubernetes.io/name=kernel-tuning
   ```

2. Check node kernel tuning is applied:
   ```bash
   kubectl debug node/<node-name> -it --image=busybox -- cat /proc/sys/fs/file-max
   # Expected: 2097152
   ```

3. Check sysctl values on node:
   ```bash
   kubectl debug node/<node-name> -it --image=busybox -- sysctl net.core.somaxconn
   # Expected: 65535
   ```

4. Check DaemonSet logs:
   ```bash
   kubectl logs -l app.kubernetes.io/name=kernel-tuning -c kernel-tuning
   ```

### Connection Limit Not Reached

1. Check memory pressure (OOM before connection limit)
2. Check CPU throttling (processing bottleneck)
3. Check client-side limits (ephemeral port exhaustion)

### High Memory Usage

1. Reduce `readBufferSize` / `writeBufferSize`
2. Reduce connections per pod, scale horizontally
3. Check for connection leaks (connections not properly closed)
