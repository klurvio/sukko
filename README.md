# Sukko

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
| `k8s` | Remote DOKS | `demo` |

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

## Remote Kubernetes (DOKS)

### Deployment

```bash
task k8s:deploy ENV=demo     # Deploy to demo
```

### Operations

```bash
task k8s:status ENV=demo     # Show pods and services
task k8s:logs ENV=demo       # Tail ws-server logs
task k8s:health ENV=demo     # Check health endpoint
task k8s:reload ENV=demo     # Restart pods (no rebuild)
task k8s:rollback ENV=demo   # Rollback to previous release
```

### Provisioning

```bash
task k8s:provision:create ENV=demo   # Create topic + tenant
task k8s:provision:status ENV=demo   # List topics
```

### Terraform

```bash
task k8s:tf:init ENV=demo    # Initialize Terraform
task k8s:tf:plan ENV=demo    # Plan changes
task k8s:tf:apply ENV=demo   # Apply infrastructure
task k8s:tf:destroy ENV=demo # Destroy infrastructure
```

### Port Forwarding

```bash
task k8s:port-forward:grafana ENV=demo    # View Grafana locally
task k8s:port-forward:prometheus ENV=demo # View Prometheus locally
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
{"type":"subscribe","channels":["sukko.all.trade"]}
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
kubectl exec -n sukko-local -it $(kubectl get pods -n sukko-local -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}') -- psql -U sukko -d sukko_provisioning

# Query tenants
SELECT * FROM tenants;
```

## Cleanup

```bash
task local:destroy            # Delete Kind cluster
task local:port-forward:stop  # Stop port-forwards
```
