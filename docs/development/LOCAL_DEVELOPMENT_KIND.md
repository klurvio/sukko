# Local Development with Kind

This guide covers setting up a local Kubernetes development environment using [Kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker) for the Odin WebSocket server.

## Prerequisites

### Required Tools

```bash
# macOS
brew install kind kubectl helm go-task docker

# Verify installations
kind version        # v0.20.0+
kubectl version     # v1.28+
helm version        # v3.12+
task --version      # v3.0+
docker --version    # v24+
```

### Optional Tools

```bash
# WebSocket testing
npm install -g wscat

# JSON formatting
brew install jq

# Kubernetes context management
brew install kubectx
```

## Quick Start

```bash
# One command to create cluster and deploy everything
task k8s:local:setup
```

This command:
1. Creates a Kind cluster named `odin-local`
2. Builds the ws-server Docker image
3. Loads the image into Kind
4. Deploys the full stack via Helm

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Kind Cluster (odin-local)                │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                  Namespace: odin-local                  │ │
│  │                                                         │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │ │
│  │  │  ws-server  │  │   Valkey    │  │    Redpanda     │ │ │
│  │  │  (Go)       │──│  (PubSub)   │  │  (Kafka compat) │ │ │
│  │  │  Port 3005  │  │  Port 6379  │  │  Port 9092      │ │ │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘ │ │
│  │         │                                               │ │
│  │  ┌──────┴──────┐  ┌─────────────┐  ┌─────────────────┐ │ │
│  │  │  Publisher  │  │  Prometheus │  │     Grafana     │ │ │
│  │  │  (test data)│  │  Port 9090  │  │   Port 3010     │ │ │
│  │  └─────────────┘  └─────────────┘  └─────────────────┘ │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## Exposed Ports

| Service | Host Port | Description |
|---------|-----------|-------------|
| ws-server | 3005 | WebSocket endpoint + health |
| Grafana | 3010 | Monitoring dashboards |
| Prometheus | 9090 | Metrics collection |
| Redpanda Console | 8080 | Kafka topic browser |

## Task Commands Reference

### Cluster Lifecycle

```bash
# Create cluster + deploy full stack
task k8s:local:setup

# Create cluster only (no deployment)
task k8s:local:cluster:create

# Delete cluster
task k8s:local:cluster:delete
# or
task k8s:local:destroy
```

### Deployment

```bash
# Deploy/upgrade Helm release
task k8s:local:up

# Uninstall Helm release (keeps cluster)
task k8s:local:down

# Rebuild images and restart pods
task k8s:local:rebuild
```

### Image Management

```bash
# Build ws-server image and load into Kind
task k8s:local:images:load
```

### Monitoring & Debugging

```bash
# Show pod and service status
task k8s:local:status

# Tail ws-server logs
task k8s:local:logs

# Tail all pod logs
task k8s:local:logs:all

# Open shell in ws-server pod
task k8s:local:shell
```

### Testing

```bash
# Test health endpoint
task k8s:local:test:health

# Connect via WebSocket (requires wscat)
task k8s:local:test:ws
```

### Publisher Management

The Publisher generates test market data and publishes to Redpanda (Kafka). It's enabled by default in local development.

```bash
# View publisher pod status
kubectl get pods -n odin-local -l app.kubernetes.io/name=publisher

# Tail publisher logs
kubectl logs -f -l app.kubernetes.io/name=publisher -n odin-local
```

#### Configuration

Default settings in `values-local.yaml`:
- **Rate**: 5 messages/second
- **Tokens**: BTC, ETH, SOL, DOGE

To customize, edit `deployments/k8s/helm/odin/values-local.yaml`:

```yaml
publisher:
  enabled: true          # Set false to disable
  config:
    ratePerSecond: 50    # Adjust publish rate
    tokens:
      - BTC
      - ETH
```

Apply changes:

```bash
task k8s:local:up
```

## Development Workflow

### 1. Initial Setup

```bash
# Create cluster and deploy
task k8s:local:setup

# Wait for pods to be ready
kubectl get pods -n odin-local -w
```

### 2. Make Code Changes

Edit files in `ws/` directory.

### 3. Rebuild and Redeploy

```bash
# Rebuild image and restart pods
task k8s:local:rebuild

# Watch logs
task k8s:local:logs
```

### 4. Test Changes

```bash
# Health check
curl http://localhost:3005/health | jq

# WebSocket connection
wscat -c ws://localhost:3005/ws
```

### 5. Iterate

Repeat steps 2-4 as needed.

## Configuration

### Kind Cluster Config

Located at: `deployments/k8s/environments/local/kind-config.yaml`

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: odin-local

nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30080
        hostPort: 3005      # ws-server
      - containerPort: 30300
        hostPort: 3010      # Grafana
      - containerPort: 30090
        hostPort: 9090      # Prometheus
      - containerPort: 30800
        hostPort: 8080      # Redpanda Console
```

### Helm Values (Local)

Located at: `deployments/k8s/helm/odin/values-local.yaml`

Key settings for local development:
- Single shard (M1 Mac optimized)
- Debug logging with pretty format
- Reduced resource limits
- No Valkey authentication
- Minimal Redpanda memory

## Helm Charts

The Odin umbrella chart includes:

| Chart | Purpose |
|-------|---------|
| ws-server | WebSocket server (Go) |
| redpanda | Kafka-compatible message broker |
| valkey | Redis-compatible pub/sub |
| publisher | Test data generator |
| monitoring | Prometheus + Grafana + Loki |

### Enabling/Disabling Components

Edit `values-local.yaml`:

```yaml
ws-server:
  enabled: true

redpanda:
  enabled: true

valkey:
  enabled: true

publisher:
  enabled: true    # Set false to disable test data

monitoring:
  enabled: true    # Set false for minimal setup
```

## Local Testing Limitations

### Why Local Can't Match Production Scale

Local machines (especially macOS) have kernel-level constraints:

| Constraint | macOS Default | Linux Default | Production |
|------------|---------------|---------------|------------|
| File descriptors | 256 | 1024 | 65535+ |
| Ephemeral ports | ~16K | ~28K | 60K+ |
| TCP buffers | Limited | Limited | Tuned |
| Memory | Shared | Shared | Dedicated |

**Result:** Local testing is limited to ~500-1000 connections max.

### Pragmatic Local Testing Strategy

**What to test locally:**
- Functionality and correctness
- Message flow (Kafka → ws-server → WebSocket)
- Subscription/unsubscription logic
- Health endpoint responses
- Log output and debugging
- Code changes before pushing

**What to test on GCP:**
- Connection capacity (10K-100K+)
- CPU/memory under load
- Broadcast performance
- Backpressure behavior
- Multi-shard coordination

### Local Testing Examples

#### 1. Manual WebSocket Test

```bash
# Single connection with wscat
wscat -c ws://localhost:3005/ws

# Send subscription
> {"action":"subscribe","channel":"odin.trades"}
```

#### 2. Health Check

```bash
# Check health while connections are active
curl -s http://localhost:3005/health | jq '{
  status: .status,
  connections: .checks.capacity.current,
  cpu: .checks.cpu.percentage
}'
```

#### 3. Local Load Test (Go Tool)

```bash
# Navigate to loadtest directory
cd loadtest

# Build the loadtest binary (first time or after code changes)
go build -o sustained-load-test

# Run local load test (100 connections, 5 ramp/sec, 5 min duration)
./sustained-load-test \
  -url ws://localhost:3005/ws \
  -health http://localhost:3005/health \
  -connections 100 \
  -ramp-rate 5 \
  -duration 300

# Quick smoke test (10 connections, 30 seconds)
./sustained-load-test \
  -url ws://localhost:3005/ws \
  -health http://localhost:3005/health \
  -connections 10 \
  -ramp-rate 5 \
  -duration 30
```

**Note:** Ramp rates 1-9 will create ~10 connections/sec (minimum batch size).

## Troubleshooting

### Pods Not Starting

```bash
# Check pod status
kubectl get pods -n odin-local

# Describe failing pod
kubectl describe pod <pod-name> -n odin-local

# Check events
kubectl get events -n odin-local --sort-by='.lastTimestamp'
```

### Image Not Found

```bash
# Ensure image is loaded into Kind
task k8s:local:images:load

# Verify image exists
docker exec -it odin-local-control-plane crictl images | grep odin
```

### Port Already in Use

```bash
# Check what's using the port
lsof -i :3005

# Delete and recreate cluster
task k8s:local:destroy
task k8s:local:setup
```

### Cluster Not Responding

```bash
# Check Docker is running
docker ps

# Check Kind cluster
kind get clusters

# Recreate if needed
task k8s:local:destroy
task k8s:local:setup
```

### Helm Dependency Issues

```bash
# Update Helm dependencies
task k8s:common:helm:deps

# Lint charts
task k8s:common:helm:lint
```

## Comparing Environments

| Setting | Local (Kind) | Develop (GKE) | Production (GKE) |
|---------|--------------|---------------|------------------|
| Replicas | 1 | 2 | 3+ (autoscale) |
| Shards | 1 | 2 | 3 |
| Log Level | debug | debug | warn |
| Log Format | pretty | json | json |
| Resources | Minimal | Medium | Full |
| Autoscaling | Disabled | Enabled | Enabled |

## Tips

### Faster Iteration

```bash
# Skip full rebuild if only config changed
task k8s:local:up

# For Go code changes, rebuild is required
task k8s:local:rebuild
```

### Watch Multiple Resources

```bash
# Terminal 1: Watch pods
kubectl get pods -n odin-local -w

# Terminal 2: Watch logs
task k8s:local:logs

# Terminal 3: Health checks
watch -n 2 'curl -s http://localhost:3005/health | jq'
```

### Clean State

```bash
# Full reset
task k8s:local:destroy
task k8s:local:setup
```

### Resource Usage

Monitor Kind resource consumption:

```bash
docker stats odin-local-control-plane
```

## File Locations

| File | Purpose |
|------|---------|
| `deployments/k8s/environments/local/kind-config.yaml` | Kind cluster configuration |
| `deployments/k8s/helm/odin/values-local.yaml` | Local Helm values |
| `deployments/k8s/helm/odin/Chart.yaml` | Umbrella chart definition |
| `deployments/k8s/helm/odin/charts/*/` | Sub-charts |
| `taskfiles/k8s/local.yml` | Local task definitions |
| `ws/Dockerfile` | ws-server container image |

## Related Documentation

- [Terraform Infrastructure](./TERRAFORM_PLAN.md)
- [GCP Deployment](./deployment/)
- [Architecture Overview](./architecture/)
