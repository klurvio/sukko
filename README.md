# Odin WebSocket Server

Multi-tenant WebSocket server with Kafka/Redpanda pub/sub for real-time data streaming.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Task](https://taskfile.dev/installation/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) (Kubernetes in Docker)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)

## Quick Start (Local)

```bash
# Full setup (creates Kind cluster, deploys everything, starts port-forwards)
task local

# Check status
task local:status

# Create topics + tenant
task local:provision:create

# Run publisher (sends messages)
task local:publisher:run RATE=1

# Run load test
task local:loadtest:smoke
```

## Command Pattern

All commands follow the pattern: `{domain}:{service}:{action} [ENV=value]`

| Domain | Description | ENV |
|--------|-------------|-----|
| `local` | Kind cluster | Not needed |
| `k8s` | Remote GKE | `dev`, `stg`, `prod` |
| `gce` | GCE VMs | `dev`, `stg`, `prod` |

## Local Development

### Core Operations

```bash
task local:setup              # Full setup (create + deploy)
task local:destroy            # Delete Kind cluster
task local:deploy             # Deploy/upgrade Helm release
task local:status             # Show pods and services
task local:logs               # Tail ws-server logs
task local:logs:gateway       # Tail ws-gateway logs
task local:build              # Build and reload images
task local:health             # Check health endpoint
```

### Provisioning

```bash
task local:provision:create   # Create Kafka topics + tenant
task local:provision:status   # List topics + tenants
task local:provision:delete   # Delete topics
```

### Load Testing

```bash
task local:loadtest:smoke     # Quick test (10 connections, 30s)
task local:loadtest:run       # Full test (CONNECTIONS=100 DURATION=1m)
task local:loadtest:stop      # Stop load test
```

### Publisher

```bash
task local:publisher:run      # Run publisher (RATE=10 msg/sec)
task local:publisher:stop     # Stop publisher
```

### Port Forwarding

```bash
task local:port-forward:start # Start all port-forwards
task local:port-forward:stop  # Stop all port-forwards
```

**Local Ports:**

| Service | Port | URL |
|---------|------|-----|
| ws-gateway | 3006 | `ws://localhost:3006/ws` |
| Redpanda | 9092 | `localhost:9092` |
| Grafana | 3010 | `http://localhost:3010` |
| Prometheus | 9090 | `http://localhost:9090` |

## Remote Kubernetes (GKE)

### Deployment

```bash
task k8s:deploy ENV=dev       # Deploy to dev
task k8s:deploy ENV=stg       # Deploy to staging
task k8s:deploy ENV=prod      # Deploy to production
```

### Operations

```bash
task k8s:status ENV=dev       # Show pods and services
task k8s:logs ENV=dev         # Tail ws-server logs
task k8s:health ENV=dev       # Check health endpoint
task k8s:reload ENV=dev       # Restart pods (no rebuild)
task k8s:rollback ENV=dev     # Rollback to previous release
```

### Provisioning

```bash
task k8s:provision:create ENV=dev   # Create topic + tenant
task k8s:provision:loadtest ENV=dev # Create 24 load test topics
task k8s:provision:status ENV=dev   # List topics
```

### Terraform

```bash
task k8s:tf:init ENV=dev      # Initialize Terraform
task k8s:tf:plan ENV=dev      # Plan changes
task k8s:tf:apply ENV=dev     # Apply infrastructure
task k8s:tf:destroy ENV=dev   # Destroy infrastructure
```

### Port Forwarding

```bash
task k8s:port-forward:grafana ENV=dev    # View Grafana locally
task k8s:port-forward:prometheus ENV=dev # View Prometheus locally
```

## GCE VMs

### Load Test VM

```bash
task gce:loadtest:deploy ENV=dev   # Deploy VM
task gce:loadtest:run ENV=dev      # Run load test
task gce:loadtest:stop ENV=dev     # Stop load test
task gce:loadtest:logs ENV=dev     # View logs
task gce:loadtest:ssh ENV=dev      # SSH into VM
task gce:loadtest:destroy ENV=dev  # Destroy VM
```

### Publisher VM

```bash
task gce:publisher:deploy ENV=dev  # Deploy VM
task gce:publisher:run ENV=dev     # Run publisher
task gce:publisher:stop ENV=dev    # Stop publisher
task gce:publisher:logs ENV=dev    # View logs
task gce:publisher:ssh ENV=dev     # SSH into VM
task gce:publisher:destroy ENV=dev # Destroy VM
```

## Testing

```bash
task test                     # Run unit tests
task test:unit                # Run unit tests
task test:integration         # Run integration tests
```

## WebSocket Usage

Connect to WebSocket:
```bash
wscat -c ws://localhost:3006/ws
```

Subscribe to a channel:
```json
{"type":"subscribe","channels":["odin.all.trade"]}
```

## Troubleshooting

### Check logs

```bash
task local:logs               # ws-server
task local:logs:gateway       # ws-gateway
task local:logs:all           # All pods
```

### Check topics

```bash
task local:provision:status
```

### Check database

```bash
kubectl exec -n odin-local -it $(kubectl get pods -n odin-local -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}') -- psql -U odin -d odin_provisioning

# Query tenants
SELECT * FROM tenants;
```

## Cleanup

```bash
task local:destroy            # Delete Kind cluster
task local:port-forward:stop  # Stop port-forwards
```
