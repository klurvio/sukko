# GKE Standard Deployment with Spot VMs

Deploy odin-ws to GKE Standard cluster with Spot VMs for 60-90% cost savings compared to GKE Autopilot.

## Cost Comparison

| Setup | Monthly Cost | Savings |
|-------|-------------|---------|
| GKE Autopilot | ~$275-375 | Baseline |
| GKE Standard (Spot) | ~$135-165 | **~$150-200/mo** |

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
| `task k8s:gke-standard:build` | Build all images |
| `task k8s:gke-standard:build:ws` | Build ws-server only |
| `task k8s:gke-standard:build:gateway` | Build ws-gateway only |
| `task k8s:gke-standard:build:reload` | Build and restart pods |

### Observability

| Command | Description |
|---------|-------------|
| `task k8s:gke-standard:status` | Show deployment status |
| `task k8s:gke-standard:nodes` | Show Spot node status |
| `task k8s:gke-standard:logs` | Tail ws-server logs |
| `task k8s:gke-standard:logs:gateway` | Tail ws-gateway logs |
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

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    GKE Standard Cluster                          │
│                                                                   │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │              Node Pool (Spot VMs - 70% savings)          │   │
│   │   Dev/Staging: 2 fixed nodes | Prod: 1-5 autoscaling     │   │
│   │                                                           │   │
│   │   ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐    │   │
│   │   │ws-gateway│  │ws-server│  │  NATS   │  │Redpanda │    │   │
│   │   │   (2)   │  │   (2)   │  │   (1)   │  │   (1)   │    │   │
│   │   └─────────┘  └─────────┘  └─────────┘  └─────────┘    │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                   │
│   Self-hosted NATS + Redpanda (minimal resources)                │
└─────────────────────────────────────────────────────────────────┘
```
