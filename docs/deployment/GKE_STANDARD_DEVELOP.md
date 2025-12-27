# GKE Standard - Develop Environment

Deploy the Odin WebSocket infrastructure to GKE Standard develop cluster with Spot VMs for cost-optimized development and testing.

## Quick Start

```bash
# Deploy to develop (assumes infrastructure already exists)
task k8s:standard:deploy:develop

# Or full setup including infrastructure
task k8s:standard:setup GKE_STD_ENV=develop
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
│                    GKE Standard Cluster (odin-ws-develop)                    │
│                         Zone: us-central1-a                                  │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │              Node Pool (2x e2-standard-4 Spot VMs)                     │  │
│  │                    8 vCPU, 32 GB RAM total                             │  │
│  │                                                                        │  │
│  │   Node 1                           Node 2                              │  │
│  │   ┌────────────────────┐          ┌────────────────────────┐          │  │
│  │   │ ws-gateway pod     │          │ ws-server pod          │          │  │
│  │   │ ws-server pod      │          │ redpanda pod           │          │  │
│  │   │ nats pod           │          │ loki pod               │          │  │
│  │   │ prometheus pod     │          │ grafana pod            │          │  │
│  │   └────────────────────┘          └────────────────────────┘          │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                         Namespace: odin-std-develop                     │ │
│  │                                                                         │ │
│  │  ┌─────────────┐      ┌─────────────┐      ┌───────────────┐           │ │
│  │  │ ws-gateway  │─────▶│  ws-server  │◀─────│   Redpanda    │           │ │
│  │  │  (2 pods)   │      │  (2 pods)   │      │   (1 pod)     │           │ │
│  │  │ LB :443     │      │ ClusterIP   │      │ LB :9092      │           │ │
│  │  └─────────────┘      └──────┬──────┘      └───────────────┘           │ │
│  │        │                     │                                          │ │
│  │        │              ┌──────┴──────┐                                   │ │
│  │        │              │    NATS     │                                   │ │
│  │        │              │  (1 pod)    │                                   │ │
│  │        │              └─────────────┘                                   │ │
│  │        │                                                                 │ │
│  │  ┌─────┴─────────────────────────────────────────────────────────────┐  │ │
│  │  │                      Monitoring Stack                              │  │ │
│  │  │  Prometheus  │  Grafana  │  Loki  │  Promtail (DaemonSet)         │  │ │
│  │  └────────────────────────────────────────────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────┘

External Access:
  Publisher ──▶ Redpanda LB (static IP:9092) ──▶ ws-server ──▶ NATS ──▶ ws-server
  Clients ◀──── ws-gateway LB (:443) ◀──────────────────────────┘
```

## Environment Configuration

| Setting | Value |
|---------|-------|
| Cluster Name | `odin-ws-develop` |
| Namespace | `odin-std-develop` |
| Zone | `us-central1-a` |
| Node Count | 2 (fixed) |
| Instance Type | e2-standard-4 (Spot) |
| ws-server Replicas | 2 |
| ws-gateway Replicas | 2 |
| Shards | 2 |
| Log Level | `info` |
| Log Format | `json` |
| NATS Cluster | Disabled (single node) |
| Redpanda Replicas | 1 |
| Persistent Storage | Yes (2Gi) |
| Autoscaling | Disabled |
| Estimated Cost | ~$99/mo |

## Task Commands

### Infrastructure (Terraform)

```bash
# Initialize Terraform
task k8s:standard:tf:init GKE_STD_ENV=develop

# Plan changes
task k8s:standard:tf:plan GKE_STD_ENV=develop

# Apply infrastructure
task k8s:standard:tf:apply GKE_STD_ENV=develop

# Show outputs (cluster info, IPs)
task k8s:standard:tf:output GKE_STD_ENV=develop

# Destroy (DANGEROUS)
task k8s:standard:tf:destroy GKE_STD_ENV=develop
```

### Connection

```bash
# Connect kubectl to develop cluster
task k8s:standard:connect GKE_STD_ENV=develop

# Or using Terraform output
task k8s:standard:connect:tf GKE_STD_ENV=develop
```

### Build & Deploy

```bash
# Build and push images to Artifact Registry
task k8s:standard:build

# Deploy/upgrade Helm release
task k8s:standard:deploy:develop

# Build and restart pods
task k8s:standard:build:reload GKE_STD_ENV=develop

# Show diff before upgrade
task k8s:standard:diff GKE_STD_ENV=develop

# Rollback to previous release
task k8s:standard:rollback GKE_STD_ENV=develop

# Uninstall Helm release
task k8s:standard:down GKE_STD_ENV=develop
```

### Observability

```bash
# Show deployment status (nodes, pods, services, HPA)
task k8s:standard:status GKE_STD_ENV=develop

# Show Spot node details
task k8s:standard:nodes GKE_STD_ENV=develop

# Get external IPs (ws-gateway, redpanda)
task k8s:standard:external-ips GKE_STD_ENV=develop

# Tail ws-server logs
task k8s:standard:logs GKE_STD_ENV=develop

# Tail ws-gateway logs
task k8s:standard:logs:gateway GKE_STD_ENV=develop

# Show recent events (Spot preemption, etc.)
task k8s:standard:events GKE_STD_ENV=develop
```

### Accessing Services

**External services (use external IP directly):**

```bash
# Get external IPs for gateway and Redpanda
task k8s:standard:external-ips GKE_STD_ENV=develop

# Connect to WebSocket (no port-forward needed)
wscat -c wss://<GATEWAY_EXTERNAL_IP>/ws

# Connect publisher to Redpanda (no port-forward needed)
KAFKA_BROKERS=<REDPANDA_EXTERNAL_IP>:9092
```

**Internal services (port-forward required):**

```bash
# Grafana dashboards - localhost:3000
task k8s:standard:port-forward:grafana GKE_STD_ENV=develop

# Prometheus metrics - localhost:9090
task k8s:standard:port-forward:prometheus GKE_STD_ENV=develop
```

Port-forward runs in foreground. Press Ctrl+C to stop.

### Health Check

```bash
# Check health endpoint
task k8s:standard:health GKE_STD_ENV=develop
```

## Resource Allocation

| Service | CPU Request | CPU Limit | Memory Request | Memory Limit |
|---------|-------------|-----------|----------------|--------------|
| ws-server | 500m | 1 | 256Mi | 512Mi |
| ws-gateway | 250m | 500m | 128Mi | 256Mi |
| Redpanda | 250m | 500m | 768Mi | 1Gi |
| NATS | 50m | 100m | 64Mi | 128Mi |
| Prometheus | 100m | 250m | 256Mi | 512Mi |
| Grafana | 50m | 100m | 128Mi | 256Mi |
| Loki | 50m | 100m | 128Mi | 256Mi |
| Promtail | 25m | 50m | 32Mi | 64Mi |

## Deployment Workflow

### 1. Initial Setup

```bash
# Full setup (infrastructure + deploy)
task k8s:standard:setup GKE_STD_ENV=develop

# Wait for pods
kubectl get pods -n odin-std-develop -w
```

### 2. Code Changes

Edit files in `ws/` directory and rebuild:

```bash
# Rebuild and restart
task k8s:standard:build:reload GKE_STD_ENV=develop
```

### 3. Verify Deployment

```bash
# Check status
task k8s:standard:status GKE_STD_ENV=develop

# Get external IPs
task k8s:standard:external-ips GKE_STD_ENV=develop

# Test WebSocket connection
wscat -c wss://<GATEWAY_IP>/ws
```

## Testing with Publisher & Loadtest

Publisher and loadtest run on a **separate GCP VM** and connect to the K8s cluster's external IPs.

```
┌─────────────────────────────────┐         ┌─────────────────────────────┐
│   GCP VM (odin-tools-dev)       │         │   K8s Cluster (develop)     │
│   e2-standard-8                 │         │                             │
│                                 │         │                             │
│  ┌───────────┐  ┌────────────┐  │         │  ┌─────────────────────┐   │
│  │ loadtest  │  │ publisher  │  │         │  │ ws-gateway (LB)     │   │
│  │   (Go)    │  │ (Node.js)  │  │         │  │ Redpanda (LB)       │   │
│  └─────┬─────┘  └──────┬─────┘  │         │  └─────────────────────┘   │
│        │               │        │         │                             │
└────────┼───────────────┼────────┘         └─────────────────────────────┘
         │               │                              ▲
         │  WebSocket    │  Kafka                       │
         └───────────────┴──────────────────────────────┘
```

### 1. Get K8s External IPs

```bash
task k8s:standard:external-ips GKE_STD_ENV=develop
```

Note the EXTERNAL-IP for `odin-ws-gateway` and `odin-redpanda-external`.

### 2. Configure Environment

Edit `deployments/gcp/v2/environments/develop.env` with the K8s external IPs:

```env
# GCP settings (already configured)
GCP_PROJECT=trim-array-480700-j7
GCP_ZONE=us-central1-a

# Update these with IPs from step 1
WS_URL=ws://<WS_GATEWAY_IP>:443/ws
HEALTH_URL=http://<WS_GATEWAY_IP>:443/health
KAFKA_BROKERS=<REDPANDA_IP>:9092
```

> **Note:** `GCP_PROJECT` and `GCP_ZONE` are automatically read from the env file by all `gcp:v2:*` tasks.

### 3. Create and Setup VM

```bash
task gcp:v2:create ENV=develop
task gcp:v2:setup ENV=develop
task gcp:v2:sync-env ENV=develop
```

### 4. Start Publisher

```bash
task gcp:v2:publisher:start ENV=develop RATE=25
task gcp:v2:publisher:logs ENV=develop
task gcp:v2:publisher:stop ENV=develop
```

### 5. Run Load Test

```bash
# Low-volume test (100 connections)
task gcp:v2:loadtest:run ENV=develop CONNECTIONS=100 RAMP_RATE=5 DURATION=300

# Capacity test (18K connections)
task gcp:v2:loadtest:capacity ENV=develop

# View logs
task gcp:v2:loadtest:logs ENV=develop
```

### 6. Monitor

```bash
# Grafana dashboards
task k8s:standard:port-forward:grafana GKE_STD_ENV=develop
# Open http://localhost:3000

# K8s logs
task k8s:standard:logs GKE_STD_ENV=develop
```

### 7. Cleanup

```bash
task gcp:v2:publisher:stop ENV=develop
task gcp:v2:stop ENV=develop  # Stop VM to save costs
```

See `deployments/gcp/v2/README.md` for full documentation.

## Spot VM Considerations

Develop uses Spot VMs for 60-90% cost savings. Pods may be preempted with 30s notice.

**Mitigations in place:**
- PodDisruptionBudget (minAvailable: 1)
- 30s termination grace period
- Kubernetes auto-reschedules to new nodes

**Monitoring preemption:**

```bash
# Watch for preemption events
task k8s:standard:events GKE_STD_ENV=develop
```

## Troubleshooting

### Pods not scheduling

```bash
kubectl get nodes
kubectl describe nodes
kubectl get events -n odin-std-develop
```

### Spot preemption issues

1. Check node status: `task k8s:standard:nodes GKE_STD_ENV=develop`
2. Events show preemption notices
3. Pods auto-reschedule within ~1-2 minutes

### Image pull errors

```bash
# Ensure authenticated to Artifact Registry
gcloud auth configure-docker us-central1-docker.pkg.dev

# Rebuild and push
task k8s:standard:build
```

### Terraform state issues

```bash
# Refresh state
terraform -chdir=deployments/terraform/gke-standard/develop refresh

# Re-initialize
task k8s:standard:tf:init GKE_STD_ENV=develop
```

## File Locations

| File | Purpose |
|------|---------|
| `deployments/k8s/helm/odin/values/standard/develop.yaml` | Helm values |
| `deployments/terraform/gke-standard/develop/` | Terraform config |
| `taskfiles/k8s/standard.yml` | Task definitions |

## Related Documentation

- [GKE Standard Deployment Overview](./GKE_STANDARD_DEPLOYMENT.md)
- [GKE Standard Staging](./GKE_STANDARD_STAGING.md)
- [GKE Standard Production](./GKE_STANDARD_PRODUCTION.md)
- [Local Development (Kind)](../development/LOCAL_DEVELOPMENT_KIND.md)
