# GKE Standard Deployment with Spot VMs

Deploy odin-ws to GKE Standard cluster with Spot VMs for 60-90% cost savings compared to GKE Autopilot.

## Cost Comparison

| Setup | Monthly Cost | Savings |
|-------|-------------|---------|
| GKE Autopilot | ~$275-375 | Baseline |
| GKE Standard (Spot) | ~$99-120 | **~$175-255/mo** |

## Node Pool Architecture

In Kubernetes (GKE), services don't get individual VMs. Instead, all pods share nodes in a **node pool**:

```
                                    ┌─────────────────┐
                                    │    loadtest     │  (External - not in cluster)
                                    │   Go CLI tool   │
                                    └────────┬────────┘
                                             │ WebSocket connections
                                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           GKE Standard Cluster                               │
│                                                                              │
│   ┌───────────────────────────────────────────────────────────────────┐     │
│   │              Node Pool (e.g., 2x e2-standard-4 nodes)              │     │
│   │                                                                    │     │
│   │   Node 1 (e2-standard-4)         Node 2 (e2-standard-4)           │     │
│   │   ┌────────────────────┐        ┌────────────────────────┐        │     │
│   │   │ ws-gateway pod     │        │ ws-server pod          │        │     │
│   │   │ ws-server pod      │        │ redpanda pod           │        │     │
│   │   │ nats pod           │        │ publisher pod (test)   │        │     │
│   │   │ prometheus pod     │        │ grafana pod            │        │     │
│   │   └────────────────────┘        └────────────────────────┘        │     │
│   └───────────────────────────────────────────────────────────────────┘     │
│                                                                              │
│   Total: 8 vCPU, 32 GB RAM shared across all pods                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key concepts:**
- **Node Pool** = a group of identical VMs (all same instance type)
- **Kubernetes scheduler** distributes pods across nodes automatically
- **Resource requests/limits** in Helm values control CPU/RAM per pod
- The instance type applies to the **entire node pool**, not per service

## Instance Type Selection

### Available Instance Types

| Instance | vCPU | RAM | On-Demand/mo | Spot/mo | Notes |
|----------|------|-----|--------------|---------|-------|
| **e2-standard-4** | 4 | 16 GB | ~$98 | ~$29 | Recommended - balanced |
| **e2-highcpu-4** | 4 | 4 GB | ~$72 | ~$22 | CPU-heavy, low memory |
| **n2-highcpu-4** | 4 | 4 GB | ~$100 | ~$30 | Dedicated CPU, low memory |
| **n2-standard-4** | 4 | 16 GB | ~$140 | ~$42 | Premium, consistent performance |

### E2 vs N2 Series

| Aspect | E2 | N2 |
|--------|----|----|
| CPU | Variable (shared pool) | Dedicated Intel Cascade/Ice Lake |
| Performance | Good for bursty loads | Consistent, predictable |
| Networking | Up to 16 Gbps | Up to 32 Gbps |
| Sustained discount | No | Yes (up to 20%) |
| Best for | Cost-sensitive, dev/staging | Production, consistent load |

### Recommendation

**e2-standard-4 with Spot VMs** (~$29/mo per node) is recommended for all environments:

- **ws-server** uses 2 shards = needs 2 CPU cores for parallel processing
- 16GB RAM per node provides headroom for all services
- Multiple services share nodes, so memory matters
- Each WebSocket connection uses ~10-15KB memory

| Environment | Instance Type | Node Count | Monthly Cost (Spot) |
|-------------|---------------|------------|---------------------|
| **Develop** | e2-standard-4 | 2 fixed | ~$58/mo |
| **Staging** | e2-standard-4 | 2 fixed | ~$58/mo |
| **Production** | e2-standard-4 | 1-5 autoscaling | ~$29-145/mo |

### Per-Service Resource Allocation

Resources are controlled via Helm values, not instance types:

**Production (`values/standard/production.yaml`):**

| Service | CPU Request | CPU Limit | Memory Request | Memory Limit |
|---------|-------------|-----------|----------------|--------------|
| ws-server | 1 core | 2 cores | 512Mi | 1Gi |
| ws-gateway | 500m | 1 core | 256Mi | 512Mi |
| Redpanda | 500m | 1 core | 1Gi | 1.5Gi |
| NATS | 100m | 250m | 128Mi | 256Mi |
| Publisher | 100m | 250m | 128Mi | 256Mi |
| Prometheus | 250m | 500m | 512Mi | 1Gi |
| Grafana | 100m | 250m | 256Mi | 512Mi |

**Develop/Staging (minimal):**

| Service | CPU Request | CPU Limit | Memory Request | Memory Limit |
|---------|-------------|-----------|----------------|--------------|
| ws-server | 500m | 1 core | 256Mi | 512Mi |
| ws-gateway | 250m | 500m | 128Mi | 256Mi |
| Redpanda | 250m | 500m | 256Mi | 384Mi |
| NATS | 50m | 100m | 64Mi | 128Mi |
| Publisher | 125m | 250m | 256Mi | 512Mi |

With 2 nodes of e2-standard-4 (total 8 vCPU, 32GB), all services fit comfortably with room for autoscaling.

**Note:** `loadtest` is not deployed to the cluster. It runs externally (local machine, Docker, or GCP VM) and connects to ws-gateway to simulate thousands of WebSocket clients. See `loadtest/README.md` for usage.

## Prerequisites

- GCP Project with billing enabled
- `gcloud` CLI configured (`gcloud auth login`)
- `terraform` >= 1.5.0
- `kubectl`
- `helm` 3.x
- Docker (for building images)

## Quick Start

### 1. Configure Terraform Variables

```bash
cd deployments/terraform/gke-standard
cp terraform.tfvars.example terraform.tfvars
vim terraform.tfvars  # Set your project_id
```

### 2. Deploy Everything

```bash
# Full setup (infrastructure + application)
task k8s:gke-standard:setup
```

This runs:
1. `tf:init` - Initialize Terraform
2. `tf:plan` - Plan infrastructure
3. `tf:apply` - Create GKE cluster
4. `connect` - Configure kubectl
5. `build` - Build and push images
6. `deploy` - Deploy Helm release
7. `status` - Show deployment status

### 3. Verify Deployment

```bash
# Check status
task k8s:gke-standard:status

# Check Spot node status
task k8s:gke-standard:nodes

# Health check
task k8s:gke-standard:health
```

## Step-by-Step Deployment

### Initialize Terraform

```bash
task k8s:gke-standard:tf:init
```

### Plan and Review

```bash
task k8s:gke-standard:tf:plan
```

### Apply Infrastructure

```bash
task k8s:gke-standard:tf:apply
```

### Connect to Cluster

```bash
task k8s:gke-standard:connect
# or using Terraform output:
task k8s:gke-standard:connect:tf
```

### Build and Push Images

```bash
task k8s:gke-standard:build
```

### Deploy Application

```bash
# Deploy to develop (default)
task k8s:gke-standard:deploy

# Or deploy to specific environment
task k8s:gke-standard:deploy:develop
task k8s:gke-standard:deploy:staging
task k8s:gke-standard:deploy:production
```

## Configuration

### Scaling Options

**Dev/Staging (Fixed 2 nodes)**
```hcl
# terraform.tfvars
node_count         = 2
enable_autoscaling = false
```

**Production (Autoscaling 1-5 nodes)**
```hcl
# terraform.tfvars
enable_autoscaling = true
min_node_count     = 1
max_node_count     = 5
```

### Spot VMs

**What are Spot VMs?**

Spot VMs are discounted compute instances that Google can reclaim (preempt) when they need the capacity back.

| Aspect | Spot VMs | Regular (On-Demand) VMs |
|--------|----------|------------------------|
| **Cost** | 60-90% cheaper | Full price |
| **Availability** | Can be preempted with 30s notice | Always available |
| **Best for** | Dev/staging, stateless workloads | Production with strict uptime |

**Why Spot VMs work well for odin-ws:**
- WebSocket servers can gracefully disconnect clients during preemption
- Kubernetes automatically reschedules pods to new nodes
- The Helm chart includes `PodDisruptionBudgets` to maintain minimum availability
- 30-second termination grace period allows clean connection shutdown

**Recommendation:**
- `use_spot_vms = true` for develop/staging (saves ~$120/mo)
- Consider `use_spot_vms = false` for production if you need guaranteed uptime

**Configuration:**

```hcl
# terraform.tfvars
use_spot_vms     = true   # Default - 60-90% cost savings
taint_spot_nodes = false  # Set true for dedicated Spot handling
```

### Resource Allocation

Values files are located in `deployments/k8s/helm/odin/values/standard/`:
- `develop.yaml` - Minimal resources, fixed replicas
- `staging.yaml` - Production-like config, fixed replicas
- `production.yaml` - Autoscaling, persistent storage, NATS cluster

**Develop/Staging** (minimal resources):

| Component | CPU Request | Memory Request |
|-----------|-------------|----------------|
| ws-gateway | 250m | 128Mi |
| ws-server | 500m | 256Mi |
| NATS | 50m | 64Mi |
| Redpanda | 250m | 256Mi |
| Publisher | 125m | 256Mi |

## Handling Spot Preemption

Spot VMs can be preempted with 30-second notice. The deployment includes:

1. **Pod Disruption Budgets** - Ensure minimum availability
2. **Tolerations** - Allow pods on Spot nodes
3. **Graceful termination** - 30s grace period for WebSocket connections

### Monitoring Preemption

```bash
# Watch events for preemption notices
task k8s:gke-standard:events

# Check node status
task k8s:gke-standard:nodes
```

## Commands Reference

### Infrastructure

| Command | Description |
|---------|-------------|
| `task k8s:gke-standard:tf:init` | Initialize Terraform |
| `task k8s:gke-standard:tf:plan` | Plan infrastructure |
| `task k8s:gke-standard:tf:apply` | Apply infrastructure |
| `task k8s:gke-standard:tf:destroy` | Destroy infrastructure |
| `task k8s:gke-standard:tf:output` | Show outputs |
| `task k8s:gke-standard:tf:validate` | Validate config |

### Deployment

| Command | Description |
|---------|-------------|
| `task k8s:gke-standard:setup` | Full setup (infra + deploy) |
| `task k8s:gke-standard:deploy` | Deploy/upgrade (default: develop) |
| `task k8s:gke-standard:deploy:develop` | Deploy to develop |
| `task k8s:gke-standard:deploy:staging` | Deploy to staging |
| `task k8s:gke-standard:deploy:production` | Deploy to production |
| `task k8s:gke-standard:down` | Uninstall Helm release |
| `task k8s:gke-standard:destroy` | Complete teardown |
| `task k8s:gke-standard:rollback` | Rollback to previous |

### Build

| Command | Description |
|---------|-------------|
| `task k8s:gke-standard:build` | Build all images (ws-server, ws-gateway, publisher) |
| `task k8s:gke-standard:build:ws` | Build ws-server only |
| `task k8s:gke-standard:build:gateway` | Build ws-gateway only |
| `task k8s:gke-standard:build:publisher` | Build publisher only |
| `task k8s:gke-standard:build:reload` | Build and restart pods |

### Load Testing

| Command | Description |
|---------|-------------|
| `cd loadtest && go build -o loadtest` | Build loadtest CLI locally |
| `./loadtest -url ws://<GATEWAY_IP>/ws -connections 1000` | Run load test |

See `loadtest/README.md` for full options including JWT auth, ramp rates, and subscription modes.

### Observability

| Command | Description |
|---------|-------------|
| `task k8s:gke-standard:status` | Show deployment status |
| `task k8s:gke-standard:nodes` | Show Spot node status |
| `task k8s:gke-standard:logs` | Tail ws-server logs |
| `task k8s:gke-standard:logs:gateway` | Tail ws-gateway logs |
| `task k8s:gke-standard:logs:publisher` | Tail publisher logs |
| `task k8s:gke-standard:events` | Show recent events |

### Port Forwarding

| Command | Description |
|---------|-------------|
| `task k8s:gke-standard:port-forward:ws` | Forward ws-server:3001 |
| `task k8s:gke-standard:port-forward:gateway` | Forward ws-gateway:3000 |
| `task k8s:gke-standard:port-forward:grafana` | Forward Grafana:3000 |

## GKE Standard vs Autopilot

| Feature | GKE Standard | GKE Autopilot |
|---------|-------------|---------------|
| Node management | Terraform (you control) | Automatic |
| Spot VM support | Full control | Limited |
| Cost | Lower with Spot | Higher base |
| Complexity | Higher | Lower |
| Best for | Cost-sensitive | Simplicity |

## Troubleshooting

### Pods not scheduling

Check node pool status:
```bash
kubectl get nodes
kubectl describe nodes
```

### Spot preemption issues

1. Increase `node_count` in terraform.tfvars
2. Enable autoscaling for production
3. Review PodDisruptionBudgets

### Terraform state issues

```bash
# Refresh state
terraform -chdir=deployments/terraform/gke-standard refresh

# Import existing resources
terraform -chdir=deployments/terraform/gke-standard import <resource> <id>
```

### Build/Push failures

Ensure you're authenticated to Artifact Registry:
```bash
gcloud auth configure-docker us-central1-docker.pkg.dev
```

## Expected Deployed Resources

After deployment, the following resources are created:

| Service | Replicas | Service Type | External Access |
|---------|----------|--------------|-----------------|
| ws-gateway | 2 | LoadBalancer | Yes - external IP :443 |
| ws-server | 2 | ClusterIP | No - internal only (gateway routes to it) |
| redpanda | 1 | ClusterIP + LoadBalancer | Yes - port 9092 (for external publishers) |
| nats | 1 | ClusterIP | No |
| publisher | 1 | - (Deployment only) | No - publishes to Kafka internally |
| prometheus | 1 | ClusterIP | No (port-forward) |
| grafana | 1 | ClusterIP | No (port-forward) |
| loki | 1 | ClusterIP | No |
| promtail | DaemonSet | - | No |

**Note:** The `loadtest` tool is NOT deployed to Kubernetes. It's a standalone Go CLI that runs externally (locally or from a GCP VM) to load test the WebSocket servers.

Get external IPs after deployment:
```bash
task k8s:gke-standard:external-ips
```

## Estimated Monthly Cost

### Fixed Infrastructure Costs

| Component | Develop | Staging | Production |
|-----------|---------|---------|------------|
| Compute (2x e2-standard-4 Spot) | ~$58 | ~$58 | ~$29-145 (1-5 nodes) |
| LoadBalancer x2 (ws-gateway + redpanda) | ~$36 | ~$36 | ~$36 |
| Artifact Registry | ~$5 | ~$5 | ~$5 |
| **Base Total** | **~$99/mo** | **~$99/mo** | **~$70-186/mo** |

### Variable Costs (traffic-based)

| Traffic Type | Cost |
|--------------|------|
| Egress (first 1TB) | $0.12/GB |
| LoadBalancer data processing | $0.008/GB |

**Examples:**
- Low-traffic develop (<10GB egress): ~$99-102/mo
- High-traffic production (100GB egress): ~$112/mo additional

