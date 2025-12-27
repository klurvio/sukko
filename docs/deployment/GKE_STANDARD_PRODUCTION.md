# GKE Standard - Production Environment

Deploy the Odin WebSocket infrastructure to GKE Standard production cluster with autoscaling, NATS clustering, and persistent storage.

## Quick Start

```bash
# Deploy to production (has confirmation prompt)
task k8s:standard:deploy:production

# Or full setup including infrastructure
task k8s:standard:setup GKE_STD_ENV=production
```

## Prerequisites

- GCP Project with billing enabled
- `gcloud` CLI configured (`gcloud auth login`)
- `terraform` >= 1.5.0
- `kubectl`
- `helm` 3.x
- Docker (for building images)

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                   GKE Standard Cluster (odin-ws-production)                  │
│                         Zone: us-central1-a                                  │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │        Node Pool (1-5 e2-standard-4 Spot VMs, Autoscaling)             │  │
│  │                    4-20 vCPU, 16-80 GB RAM                             │  │
│  │                                                                        │  │
│  │   Node 1           Node 2           Node 3           Node 4+           │  │
│  │   ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐        │  │
│  │   │ws-gateway│    │ws-server │    │ redpanda │    │ (scaled) │        │  │
│  │   │ws-server │    │ws-server │    │ redpanda │    │          │        │  │
│  │   │nats      │    │nats      │    │ redpanda │    │          │        │  │
│  │   │prometheus│    │nats      │    │ grafana  │    │          │        │  │
│  │   └──────────┘    └──────────┘    └──────────┘    └──────────┘        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                       Namespace: odin-std-production                    │ │
│  │                                                                         │ │
│  │  ┌─────────────┐      ┌─────────────┐      ┌───────────────┐           │ │
│  │  │ ws-gateway  │─────▶│  ws-server  │◀─────│   Redpanda    │           │ │
│  │  │  (3+ pods)  │      │  (3+ pods)  │      │  (3 pods)     │           │ │
│  │  │ LB :443     │      │ ClusterIP   │      │ LB :9092      │           │ │
│  │  │ HPA: 2-6    │      │ HPA: 2-8    │      │ 10Gi storage  │           │ │
│  │  └─────────────┘      └──────┬──────┘      └───────────────┘           │ │
│  │        │                     │                                          │ │
│  │        │              ┌──────┴──────┐                                   │ │
│  │        │              │ NATS Cluster│                                   │ │
│  │        │              │  (3 pods)   │                                   │ │
│  │        │              │  Auth: On   │                                   │ │
│  │        │              └─────────────┘                                   │ │
│  │        │                                                                 │ │
│  │  ┌─────┴─────────────────────────────────────────────────────────────┐  │ │
│  │  │                      Monitoring Stack                              │  │ │
│  │  │  Prometheus (20Gi)  │  Grafana  │  Loki (10Gi)  │  Promtail       │  │ │
│  │  └────────────────────────────────────────────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────┘

External Access:
  Publisher ──▶ Redpanda LB (:9092) ──▶ ws-server ──▶ NATS Cluster ──▶ ws-server
  Clients ◀──── ws-gateway LB (:443) ◀──────────────────────────────────┘
```

## Environment Configuration

| Setting | Value |
|---------|-------|
| Cluster Name | `odin-ws-production` |
| Namespace | `odin-std-production` |
| Zone | `us-central1-a` |
| Node Count | 1-5 (autoscaling) |
| Instance Type | e2-standard-4 (Spot) |
| ws-server Replicas | 3 (HPA: 2-8) |
| ws-gateway Replicas | 3 (HPA: 2-6) |
| Shards | 4 |
| Log Level | `warn` |
| Log Format | `json` |
| NATS Cluster | Enabled (3 nodes, auth on) |
| Redpanda Replicas | 3 |
| Persistent Storage | Yes (Redpanda: 10Gi, Prometheus: 20Gi, Loki: 10Gi) |
| Autoscaling | Enabled (CPU 70%) |
| PDB minAvailable | 2 |
| Estimated Cost | ~$70-186/mo |

## Production vs Other Environments

| Aspect | Develop | Staging | Production |
|--------|---------|---------|------------|
| Node Scaling | Fixed (2) | Fixed (2) | Autoscale (1-5) |
| ws-server HPA | No | No | Yes (2-8) |
| ws-gateway HPA | No | No | Yes (2-6) |
| NATS | Single | Single | Cluster (3) |
| NATS Auth | Off | Off | On |
| Redpanda | 1 replica | 1 replica | 3 replicas |
| Replication Factor | 1 | 1 | 2 |
| Persistent Storage | 2Gi | None | 10-20Gi |
| Log Level | info | info | warn |
| PDB minAvailable | 1 | 1 | 2 |

## Task Commands

### Infrastructure (Terraform)

```bash
# Initialize Terraform
task k8s:standard:tf:init GKE_STD_ENV=production

# Plan changes
task k8s:standard:tf:plan GKE_STD_ENV=production

# Apply infrastructure
task k8s:standard:tf:apply GKE_STD_ENV=production

# Show outputs
task k8s:standard:tf:output GKE_STD_ENV=production

# Destroy (DANGEROUS - requires confirmation)
task k8s:standard:tf:destroy GKE_STD_ENV=production
```

### Connection

```bash
# Connect kubectl to production cluster
task k8s:standard:connect GKE_STD_ENV=production

# Or using Terraform output
task k8s:standard:connect:tf GKE_STD_ENV=production
```

### Build & Deploy

```bash
# Build and push images
task k8s:standard:build

# Deploy to production (has confirmation prompt)
task k8s:standard:deploy:production

# Show diff before upgrade
task k8s:standard:diff GKE_STD_ENV=production

# Rollback to previous release
task k8s:standard:rollback GKE_STD_ENV=production

# Uninstall Helm release (DANGEROUS)
task k8s:standard:down GKE_STD_ENV=production
```

### Observability

```bash
# Show deployment status (includes HPA)
task k8s:standard:status GKE_STD_ENV=production

# Show Spot node details
task k8s:standard:nodes GKE_STD_ENV=production

# Get external IPs
task k8s:standard:external-ips GKE_STD_ENV=production

# Tail ws-server logs
task k8s:standard:logs GKE_STD_ENV=production

# Tail ws-gateway logs
task k8s:standard:logs:gateway GKE_STD_ENV=production

# Show recent events
task k8s:standard:events GKE_STD_ENV=production
```

### Accessing Services

**External services (use external IP directly):**

```bash
# Get external IPs for gateway and Redpanda
task k8s:standard:external-ips GKE_STD_ENV=production

# Connect to WebSocket (no port-forward needed)
wscat -c wss://<GATEWAY_EXTERNAL_IP>/ws

# Connect publisher to Redpanda (no port-forward needed)
KAFKA_BROKERS=<REDPANDA_EXTERNAL_IP>:9092
```

**Internal services (port-forward required):**

```bash
# Grafana dashboards - localhost:3000
task k8s:standard:port-forward:grafana GKE_STD_ENV=production

# Prometheus metrics - localhost:9090
task k8s:standard:port-forward:prometheus GKE_STD_ENV=production
```

Port-forward runs in foreground. Press Ctrl+C to stop.

## Resource Allocation

| Service | CPU Request | CPU Limit | Memory Request | Memory Limit |
|---------|-------------|-----------|----------------|--------------|
| ws-server | 1 | 2 | 512Mi | 1Gi |
| ws-gateway | 500m | 1 | 256Mi | 512Mi |
| Redpanda | 500m | 1 | 1Gi | 1.5Gi |
| NATS | 100m | 250m | 128Mi | 256Mi |
| Prometheus | 250m | 500m | 512Mi | 1Gi |
| Grafana | 100m | 250m | 256Mi | 512Mi |
| Loki | 100m | 250m | 256Mi | 512Mi |
| Promtail | 50m | 100m | 64Mi | 128Mi |

## Autoscaling Configuration

### Horizontal Pod Autoscaler (HPA)

| Service | Min Replicas | Max Replicas | Target CPU |
|---------|--------------|--------------|------------|
| ws-server | 2 | 8 | 70% |
| ws-gateway | 2 | 6 | 70% |

```bash
# Check HPA status
kubectl get hpa -n odin-std-production
```

### Cluster Autoscaler

- **Min Nodes:** 1
- **Max Nodes:** 5
- **Scale Down:** After 10 minutes of underutilization

## High Availability Features

### NATS Cluster

Production runs a 3-node NATS cluster with authentication:

```yaml
nats:
  replicaCount: 3
  cluster:
    enabled: true
    replicas: 3
  auth:
    enabled: true
```

### Redpanda Replication

Topics use replication factor 2 for durability:

```yaml
topics:
  - name: odin.trades
    partitions: 4
    replicationFactor: 2
```

### Pod Disruption Budgets

Ensure minimum availability during node maintenance or Spot preemption:

| Service | minAvailable |
|---------|--------------|
| ws-server | 2 |
| ws-gateway | 2 |
| NATS | 2 |
| Redpanda | 2 |

## Deployment Workflow

### 1. Pre-deployment Checklist

- [ ] Changes tested in staging
- [ ] Images built and pushed
- [ ] Backup current state: `helm history odin -n odin-std-production`
- [ ] Team notified

### 2. Deploy

```bash
# Connect to production
task k8s:standard:connect GKE_STD_ENV=production

# Review diff
task k8s:standard:diff GKE_STD_ENV=production

# Deploy (requires confirmation)
task k8s:standard:deploy:production
```

### 3. Monitor

```bash
# Watch rollout
kubectl rollout status deployment -n odin-std-production

# Check HPA scaling
kubectl get hpa -n odin-std-production -w

# Monitor events
task k8s:standard:events GKE_STD_ENV=production
```

### 4. Verify

```bash
# Check status
task k8s:standard:status GKE_STD_ENV=production

# Health check
task k8s:standard:health GKE_STD_ENV=production
```

### 5. Rollback (if needed)

```bash
task k8s:standard:rollback GKE_STD_ENV=production
```

## Publishing Events

Production receives events from the actual odin-api, not test publisher.

### Get Redpanda External IP

```bash
task k8s:standard:external-ips GKE_STD_ENV=production
```

Configure odin-api to publish to `<REDPANDA_IP>:9092`.

## Load Testing Production

**Warning:** Only run load tests during maintenance windows or with team coordination.

Publisher and loadtest run on a **separate GCP VM** (not locally).

### Pre-Load Test Checklist

- [ ] Team notified
- [ ] Monitoring dashboards open
- [ ] Rollback plan ready
- [ ] Start with low connection count

### Setup Test VM

```bash
# Get K8s external IPs
task k8s:standard:external-ips GKE_STD_ENV=production
```

Configure `deployments/gcp/v2/environments/production.env` with the K8s external IPs:

```env
# GCP settings
GCP_PROJECT=trim-array-480700-j7
GCP_ZONE=us-central1-a

# Update these with IPs from above
WS_URL=ws://<WS_GATEWAY_IP>:443/ws
HEALTH_URL=http://<WS_GATEWAY_IP>:443/health
KAFKA_BROKERS=<REDPANDA_IP>:9092
```

> **Note:** `GCP_PROJECT` and `GCP_ZONE` are automatically read from the env file by all `gcp:v2:*` tasks.

```bash
# Create and setup VM
task gcp:v2:create ENV=production
task gcp:v2:setup ENV=production
task gcp:v2:sync-env ENV=production
```

### Run Load Test

```bash
# Start conservatively (50 connections)
task gcp:v2:loadtest:run ENV=production CONNECTIONS=50 DURATION=60

# Gradually increase if stable
task gcp:v2:loadtest:run ENV=production CONNECTIONS=500 DURATION=300

# View logs
task gcp:v2:loadtest:logs ENV=production
```

### Monitor During Test

```bash
# Watch HPA scaling
kubectl get hpa -n odin-std-production -w

# Grafana dashboards
task k8s:standard:port-forward:grafana GKE_STD_ENV=production
# Open http://localhost:3000
```

### Cleanup

```bash
task gcp:v2:stop ENV=production
```

See `deployments/gcp/v2/README.md` for full documentation.

## Spot VM Considerations for Production

Production uses Spot VMs for cost savings. This is acceptable because:

1. **Multiple replicas** - PDB ensures 2+ pods always running
2. **Graceful shutdown** - 30s termination for clean disconnect
3. **Fast recovery** - Kubernetes reschedules within 1-2 minutes
4. **Stateless workloads** - ws-server/ws-gateway have no local state

### Monitoring Preemption

```bash
# Watch for preemption events
kubectl get events -n odin-std-production --field-selector reason=Preempted

# Check node churn
kubectl get nodes -o wide
```

### Alternative: On-Demand VMs

If you need guaranteed uptime, modify Terraform:

```hcl
# terraform.tfvars
use_spot_vms = false  # Uses on-demand VMs (higher cost)
```

## Persistent Storage

| Component | Size | Storage Class | Purpose |
|-----------|------|---------------|---------|
| Redpanda | 10Gi | standard-rwo | Kafka data |
| Prometheus | 20Gi | standard-rwo | Metrics (15d retention) |
| Loki | 10Gi | standard-rwo | Logs (7d retention) |

## Cost Optimization

### Estimated Monthly Costs

| Component | Cost |
|-----------|------|
| Compute (1-5 Spot nodes) | $29-145 |
| LoadBalancers (2) | ~$36 |
| Storage (~40Gi) | ~$4 |
| Artifact Registry | ~$5 |
| **Base Total** | **$70-186/mo** |

### Cost Monitoring

```bash
# Check Spot savings
task k8s:standard:cost:info GKE_STD_ENV=production
```

## Troubleshooting

### HPA not scaling

```bash
# Check HPA metrics
kubectl describe hpa -n odin-std-production

# Verify metrics-server
kubectl top pods -n odin-std-production
```

### Pods pending (no nodes)

```bash
# Check cluster autoscaler
kubectl get events -n kube-system | grep cluster-autoscaler

# Check node pool
gcloud container node-pools describe default-pool \
  --cluster=odin-ws-production \
  --zone=us-central1-a
```

### NATS cluster issues

```bash
# Check NATS cluster status
kubectl exec -n odin-std-production odin-nats-0 -- nats-server --help
kubectl logs -n odin-std-production -l app.kubernetes.io/name=nats
```

### Redpanda replication lag

```bash
# Check partition status
kubectl exec -n odin-std-production odin-redpanda-0 -- \
  rpk cluster partitions status
```

### Storage issues

```bash
# Check PVCs
kubectl get pvc -n odin-std-production

# Describe for events
kubectl describe pvc -n odin-std-production
```

## File Locations

| File | Purpose |
|------|---------|
| `deployments/k8s/helm/odin/values/standard/production.yaml` | Helm values |
| `deployments/terraform/gke-standard/production/` | Terraform config |
| `taskfiles/k8s/standard.yml` | Task definitions |

## Related Documentation

- [GKE Standard Deployment Overview](./GKE_STANDARD_DEPLOYMENT.md)
- [GKE Standard Develop](./GKE_STANDARD_DEVELOP.md)
- [GKE Standard Staging](./GKE_STANDARD_STAGING.md)
- [Local Development (Kind)](../development/LOCAL_DEVELOPMENT_KIND.md)
- [Monitoring Setup](../monitoring/MONITORING_SETUP.md)
