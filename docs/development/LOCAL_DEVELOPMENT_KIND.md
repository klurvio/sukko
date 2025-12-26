# Local Development with Kind

This guide covers setting up a local Kubernetes development environment using [Kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker) for the Odin WebSocket infrastructure.

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
1. Creates a Kind cluster named `odin-ws-local`
2. Switches kubectl context to the Kind cluster
3. Builds ws-server and ws-gateway Docker images
4. Loads images into Kind
5. Deploys the full stack via Helm

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                    Kind Cluster (odin-ws-local)                       │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │                    Namespace: odin-local                        │  │
│  │                                                                 │  │
│  │  ┌─────────────┐      ┌─────────────┐      ┌───────────────┐   │  │
│  │  │ ws-gateway  │─────▶│  ws-server  │◀─────│   Redpanda    │   │  │
│  │  │  (Go)       │      │   (Go)      │      │ (Kafka compat)│   │  │
│  │  │ Port 30000  │      │ Port 30080  │      │  Port 9092    │   │  │
│  │  └─────────────┘      └──────┬──────┘      └───────────────┘   │  │
│  │        │                     │                                  │  │
│  │        │              ┌──────┴──────┐                          │  │
│  │        │              │    NATS     │                          │  │
│  │        │              │ (Broadcast) │                          │  │
│  │        │              │  Port 4222  │                          │  │
│  │        │              └─────────────┘                          │  │
│  │        │                                                        │  │
│  │  ┌─────┴─────────────────────────────────────────────────────┐ │  │
│  │  │                    Monitoring Stack                        │ │  │
│  │  │  Prometheus:9090  │  Grafana:30300  │  Loki  │  Promtail  │ │  │
│  │  └────────────────────────────────────────────────────────────┘ │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘

Data Flow:
  Publisher ──▶ Redpanda ──▶ ws-server ──▶ NATS (broadcast) ──▶ ws-server instances
                                │
  Clients ◀──── ws-gateway ◀───┘
```

## Exposed Ports

| Service | Host Port | Container Port | Description |
|---------|-----------|----------------|-------------|
| ws-gateway | 3006 (port-forward) | 30000 | WebSocket endpoint (auth + filtering) |
| ws-server | 3005 | 30080 | Internal WebSocket server |
| Grafana | 3010 | 30300 | Monitoring dashboards |
| Prometheus | 9090 | 30090 | Metrics collection |
| Redpanda Console | 8080 | 30800 | Kafka topic browser |

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
task k8s:local:deploy

# Uninstall Helm release (keeps cluster)
task k8s:local:down

# Rebuild images and restart pods
task k8s:local:build:reload
```

### Image Management

```bash
# Build all images and load into Kind
task k8s:local:build
```

### Monitoring & Debugging

```bash
# Show pod and service status
task k8s:local:status

# Tail ws-server logs
task k8s:local:logs

# Tail ws-gateway logs
task k8s:local:logs:gateway

# Tail all pod logs
task k8s:local:logs:all

# Open shell in ws-server pod
task k8s:local:shell
```

### Port Forwarding

Since Kind uses NodePorts mapped to host ports, you may need port-forwarding for some services:

```bash
# Start all port-forwards (gateway + redpanda)
task k8s:local:port-forward:start

# Stop all port-forwards
task k8s:local:port-forward:stop

# Individual port-forwards
task k8s:local:port-forward:gateway   # localhost:3006 → gateway:3000
task k8s:local:port-forward:redpanda  # localhost:9092 → redpanda:9092
```

### Testing

```bash
# Test health endpoint (requires port-forward)
task k8s:local:health

# Connect via WebSocket (requires wscat + port-forward)
task k8s:local:test:ws
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

Edit files in `ws/` directory:
- `ws/cmd/server/` - ws-server code
- `ws/cmd/gateway/` - ws-gateway code
- `ws/internal/` - shared internal packages

### 3. Rebuild and Redeploy

```bash
# Rebuild images and restart pods
task k8s:local:build:reload

# Watch logs
task k8s:local:logs
```

### 4. Test Changes

```bash
# Start port-forward
task k8s:local:port-forward:start

# Health check
curl http://localhost:3006/health | jq

# WebSocket connection
wscat -c ws://localhost:3006/ws
```

### 5. Iterate

Repeat steps 2-4 as needed.

## Configuration

### Kind Cluster Config

Located at: `deployments/k8s/environments/local/kind-config.yaml`

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: odin-ws-local

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

Located at: `deployments/k8s/helm/odin/values/local.yaml`

Key settings for local development:
- Single shard (M1 Mac optimized)
- Debug logging with pretty format
- Reduced resource limits
- NATS for broadcast bus (no auth)
- Minimal Redpanda memory
- Valkey disabled (using NATS instead)

## Helm Charts

The Odin umbrella chart includes:

| Chart | Purpose | Enabled |
|-------|---------|---------|
| ws-gateway | Auth + permission filtering gateway | Yes |
| ws-server | WebSocket broadcaster (no auth) | Yes |
| redpanda | Kafka-compatible message broker | Yes |
| nats | High-performance broadcast bus | Yes |
| valkey | Redis-compatible (disabled) | No |
| monitoring | Prometheus + Grafana + Loki | Yes |

### Enabling/Disabling Components

Edit `deployments/k8s/helm/odin/values/local.yaml`:

```yaml
ws-gateway:
  enabled: true

ws-server:
  enabled: true

redpanda:
  enabled: true

nats:
  enabled: true

valkey:
  enabled: false    # Using NATS for broadcast

monitoring:
  enabled: true     # Set false for minimal setup
```

## Local Testing with Load Test Tool

### Build and Run Load Test

```bash
# Navigate to loadtest directory
cd loadtest

# Build the loadtest binary
go build -o loadtest .

# Start port-forward first
task k8s:local:port-forward:start

# Run load test (100 connections, 5 ramp/sec, 5 min duration)
./loadtest \
  -url ws://localhost:3006/ws \
  -health http://localhost:3006/health \
  -connections 100 \
  -ramp-rate 5 \
  -duration 300

# Quick smoke test (10 connections, 30 seconds)
./loadtest \
  -url ws://localhost:3006/ws \
  -health http://localhost:3006/health \
  -connections 10 \
  -ramp-rate 5 \
  -duration 30
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
- Message flow (Kafka → ws-server → NATS → WebSocket)
- Subscription/unsubscription logic
- Gateway auth and permission filtering
- Health endpoint responses
- Log output and debugging
- Code changes before pushing

**What to test on GKE:**
- Connection capacity (10K-100K+)
- CPU/memory under load
- Broadcast performance
- Backpressure behavior
- Multi-shard coordination
- Gateway-to-server communication at scale

## Troubleshooting

### Wrong Kubectl Context

If pods deploy to GKE instead of Kind:

```bash
# Check current context
kubectl config current-context

# Switch to Kind context
kubectl config use-context kind-odin-ws-local

# Or re-run setup (auto-switches context)
task k8s:local:deploy
```

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
task k8s:local:build

# Verify image exists
docker exec -it odin-ws-local-control-plane crictl images | grep odin
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

# Check if Kind container is running
docker ps --filter name=odin-ws-local

# Recreate if needed
task k8s:local:destroy
task k8s:local:setup
```

### Helm Dependency Issues

```bash
# Update Helm dependencies
helm dependency update deployments/k8s/helm/odin

# Lint charts
task k8s:common:helm:lint
```

## Comparing Environments

| Setting | Local (Kind) | Develop (GKE) | Production (GKE) |
|---------|--------------|---------------|------------------|
| Cluster Type | Kind | GKE Standard | GKE Standard |
| Replicas | 1 | 1-2 | 3+ (autoscale) |
| Shards | 1 | 2 | 2 |
| Log Level | debug | debug | warn |
| Log Format | pretty | json | json |
| Resources | Minimal | Medium | Full |
| Autoscaling | Disabled | Enabled | Enabled |
| Broadcast Bus | NATS | NATS | NATS |

## Tips

### Faster Iteration

```bash
# Skip full rebuild if only config changed
task k8s:local:deploy

# For Go code changes, rebuild is required
task k8s:local:build:reload
```

### Watch Multiple Resources

```bash
# Terminal 1: Watch pods
kubectl get pods -n odin-local -w

# Terminal 2: Watch logs
task k8s:local:logs

# Terminal 3: Health checks (requires port-forward)
watch -n 2 'curl -s http://localhost:3006/health | jq'
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
docker stats odin-ws-local-control-plane
```

## File Locations

| File | Purpose |
|------|---------|
| `deployments/k8s/environments/local/kind-config.yaml` | Kind cluster configuration |
| `deployments/k8s/helm/odin/values/local.yaml` | Local Helm values |
| `deployments/k8s/helm/odin/Chart.yaml` | Umbrella chart definition |
| `deployments/k8s/helm/odin/charts/*/` | Sub-charts |
| `taskfiles/k8s/local.yml` | Local task definitions |
| `ws/build/server/Dockerfile` | ws-server container image |
| `ws/build/gateway/Dockerfile` | ws-gateway container image |

## Related Documentation

- [GKE Standard Deployment](../deployment/GKE_STANDARD_DEPLOYMENT.md)
- [Architecture Overview](../architecture/)
- [Monitoring Setup](../monitoring/MONITORING_SETUP.md)
