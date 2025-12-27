# GKE Standard - Staging Environment

Deploy the Odin WebSocket infrastructure to GKE Standard staging cluster. Production-like configuration with Spot VMs for cost savings.

## Quick Start

```bash
# Deploy to staging
task k8s:standard:deploy:staging

# Or full setup including infrastructure
task k8s:standard:setup GKE_STD_ENV=staging
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
│                    GKE Standard Cluster (odin-ws-staging)                    │
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
│  │                         Namespace: odin-std-staging                     │ │
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
  Publisher ──▶ Redpanda LB (:9092) ──▶ ws-server ──▶ NATS ──▶ ws-server
  Clients ◀──── ws-gateway LB (:443) ◀──────────────────────────┘
```

## Environment Configuration

| Setting | Value |
|---------|-------|
| Cluster Name | `odin-ws-staging` |
| Namespace | `odin-std-staging` |
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
| Persistent Storage | No (emptyDir) |
| Autoscaling | Disabled |
| Estimated Cost | ~$99/mo |

## Staging vs Develop

| Aspect | Develop | Staging |
|--------|---------|---------|
| Purpose | Development & testing | Pre-production validation |
| Data Persistence | Yes (2Gi) | No (ephemeral) |
| External Redpanda IP | Static (Terraform-managed) | Dynamic |
| Intended Use | Active development | QA, integration testing |

## Task Commands

### Infrastructure (Terraform)

```bash
# Initialize Terraform
task k8s:standard:tf:init GKE_STD_ENV=staging

# Plan changes
task k8s:standard:tf:plan GKE_STD_ENV=staging

# Apply infrastructure
task k8s:standard:tf:apply GKE_STD_ENV=staging

# Show outputs
task k8s:standard:tf:output GKE_STD_ENV=staging

# Destroy (DANGEROUS)
task k8s:standard:tf:destroy GKE_STD_ENV=staging
```

### Connection

```bash
# Connect kubectl to staging cluster
task k8s:standard:connect GKE_STD_ENV=staging

# Or using Terraform output
task k8s:standard:connect:tf GKE_STD_ENV=staging
```

### Build & Deploy

```bash
# Build and push images
task k8s:standard:build

# Deploy to staging
task k8s:standard:deploy:staging

# Build and restart pods
task k8s:standard:build:reload GKE_STD_ENV=staging

# Show diff before upgrade
task k8s:standard:diff GKE_STD_ENV=staging

# Rollback to previous release
task k8s:standard:rollback GKE_STD_ENV=staging

# Uninstall Helm release
task k8s:standard:down GKE_STD_ENV=staging
```

### Observability

```bash
# Show deployment status
task k8s:standard:status GKE_STD_ENV=staging

# Show Spot node details
task k8s:standard:nodes GKE_STD_ENV=staging

# Get external IPs
task k8s:standard:external-ips GKE_STD_ENV=staging

# Tail ws-server logs
task k8s:standard:logs GKE_STD_ENV=staging

# Tail ws-gateway logs
task k8s:standard:logs:gateway GKE_STD_ENV=staging

# Show recent events
task k8s:standard:events GKE_STD_ENV=staging
```

### Accessing Services

**External services (use external IP directly):**

```bash
# Get external IPs for gateway and Redpanda
task k8s:standard:external-ips GKE_STD_ENV=staging

# Connect to WebSocket (no port-forward needed)
wscat -c wss://<GATEWAY_EXTERNAL_IP>/ws

# Connect publisher to Redpanda (no port-forward needed)
KAFKA_BROKERS=<REDPANDA_EXTERNAL_IP>:9092
```

**Internal services (port-forward required):**

```bash
# Grafana dashboards - localhost:3000
task k8s:standard:port-forward:grafana GKE_STD_ENV=staging

# Prometheus metrics - localhost:9090
task k8s:standard:port-forward:prometheus GKE_STD_ENV=staging
```

Port-forward runs in foreground. Press Ctrl+C to stop.

## Resource Allocation

| Service | CPU Request | CPU Limit | Memory Request | Memory Limit |
|---------|-------------|-----------|----------------|--------------|
| ws-server | 500m | 1 | 256Mi | 512Mi |
| ws-gateway | 250m | 500m | 128Mi | 256Mi |
| Redpanda | 250m | 500m | 256Mi | 384Mi |
| NATS | 50m | 100m | 64Mi | 128Mi |
| Prometheus | 100m | 250m | 256Mi | 512Mi |
| Grafana | 50m | 100m | 128Mi | 256Mi |
| Loki | 50m | 100m | 128Mi | 256Mi |
| Promtail | 25m | 50m | 32Mi | 64Mi |

## Deployment Workflow

### 1. Connect to Staging

```bash
task k8s:standard:connect GKE_STD_ENV=staging
```

### 2. Deploy

```bash
# Build and push latest images
task k8s:standard:build

# Deploy to staging
task k8s:standard:deploy:staging
```

### 3. Verify

```bash
# Check status
task k8s:standard:status GKE_STD_ENV=staging

# Get external IP
task k8s:standard:external-ips GKE_STD_ENV=staging

# Test connection
wscat -c wss://<GATEWAY_IP>/ws
```

### 4. Test

Run integration tests against the staging environment before promoting to production.

## Testing with Publisher & Loadtest

Publisher and loadtest run on a **separate GCP VM** and connect to the K8s cluster's external IPs.

### 1. Get K8s External IPs

```bash
task k8s:standard:external-ips GKE_STD_ENV=staging
```

### 2. Configure Environment

Edit `deployments/gcp/v2/environments/staging.env` with the K8s external IPs:

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
task gcp:v2:create ENV=staging
task gcp:v2:setup ENV=staging
task gcp:v2:sync-env ENV=staging
```

### 4. Run Tests

```bash
# Start publisher
task gcp:v2:publisher:start ENV=staging RATE=25

# Run load test
task gcp:v2:loadtest:run ENV=staging CONNECTIONS=500 DURATION=300

# Monitor
task k8s:standard:port-forward:grafana GKE_STD_ENV=staging
```

### 5. Cleanup

```bash
task gcp:v2:publisher:stop ENV=staging
task gcp:v2:stop ENV=staging
```

See `deployments/gcp/v2/README.md` for full documentation.

## No Persistent Storage

Staging uses `emptyDir` for Redpanda storage:
- Data is lost when pods restart
- Intentional design to test fresh-start scenarios
- Matches production behavior after node replacement

If you need persistent data for testing, use the develop environment instead.

## Spot VM Considerations

Same as develop - Spot VMs provide 60-90% cost savings with potential preemption.

**Mitigations:**
- PodDisruptionBudget (minAvailable: 1)
- 30s termination grace period
- Auto-rescheduling

## Troubleshooting

### Pods not scheduling

```bash
kubectl get nodes
kubectl describe nodes
kubectl get events -n odin-std-staging
```

### No data after restart

This is expected - staging uses ephemeral storage. Kafka topics are recreated automatically, but historical data is lost.

### Connection refused to Redpanda

```bash
# Check Redpanda pod status
kubectl get pods -n odin-std-staging -l app.kubernetes.io/name=redpanda

# Check service
kubectl get svc -n odin-std-staging | grep redpanda
```

## File Locations

| File | Purpose |
|------|---------|
| `deployments/k8s/helm/odin/values/standard/staging.yaml` | Helm values |
| `deployments/terraform/gke-standard/staging/` | Terraform config |
| `taskfiles/k8s/standard.yml` | Task definitions |

## Related Documentation

- [GKE Standard Deployment Overview](./GKE_STANDARD_DEPLOYMENT.md)
- [GKE Standard Develop](./GKE_STANDARD_DEVELOP.md)
- [GKE Standard Production](./GKE_STANDARD_PRODUCTION.md)
- [Local Development (Kind)](../development/LOCAL_DEVELOPMENT_KIND.md)
