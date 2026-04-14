# Sukko

Multi-tenant WebSocket infrastructure platform for real-time data distribution. Built for trading and market data — delivers messages from Kafka/Redpanda through WebSocket, SSE, and push notification channels with JWT authentication, per-tenant isolation, and edition-based feature gating.

## Architecture

```
Client SDKs ──┐
              ├──▶ ws-gateway ──▶ ws-server shards
              │      (auth,        (Kafka consumer,
              │       proxy,        NATS broadcast,
              │       rate limit)   per-client delivery)
              │
              └──▶ provisioning
                    (tenants, keys,
                     rules, license)
```

**Services:**
- **ws-gateway** — WebSocket/SSE/REST reverse proxy with JWT auth, tenant isolation, rate limiting, connection tracking, token revocation
- **ws-server** — Core WebSocket server with sharded connections, Kafka/Redpanda consumption, NATS broadcast
- **provisioning** — Multi-tenant management API (tenants, signing keys, API keys, routing rules, channel rules, license, token revocation)
- **push** — Push notification service (Web Push, FCM, APNs) with Kafka-driven delivery

**Transports:**
- WebSocket (`/ws`) — bidirectional, real-time
- SSE (`/sse`) — server-sent events, read-only stream
- REST Publish (`POST /api/v1/publish`) — HTTP message injection
- Push Notifications — offline delivery via Web Push, FCM, APNs

## Quick Start

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/)
- [Task](https://taskfile.dev/installation/) (optional — for remote K8s operations)

### Local Development

```bash
# Install sukko-cli (manages local Docker Compose environment)
go install github.com/klurvio/sukko-cli@latest

# Initialize and start the platform
sukko init --defaults
sukko up

# Create a tenant and generate credentials
sukko tenant create --id my-app --name "My App"
sukko key create --tenant my-app --generate

# Generate a JWT and connect
sukko token generate --tenant my-app --sub user1
sukko subscribe orders.new --token <jwt>
```

**Local URLs:**

| Service | URL | Description |
|---------|-----|-------------|
| Gateway | `ws://localhost:3000/ws` | WebSocket endpoint |
| Gateway | `http://localhost:3000/sse` | SSE endpoint |
| Gateway | `http://localhost:3000/api/v1/publish` | REST publish |
| Provisioning | `http://localhost:8080` | Admin API |

### Observability (optional)

```bash
sukko up --profile observability    # Adds Prometheus, Grafana, Tempo
```

| Service | URL |
|---------|-----|
| Grafana | `http://localhost:3010` |
| Prometheus | `http://localhost:9090` |

## Development

```bash
# Run tests
cd ws && go test -race ./...

# Build binaries
cd ws && go build ./cmd/server
cd ws && go build ./cmd/gateway
cd ws && go build ./cmd/push

# Lint
cd ws && golangci-lint run ./...

# Proto codegen
cd ws && buf generate
```

## Remote Kubernetes

```bash
# Deploy to DOKS
task k8s:setup PLATFORM=doks ENV=demo

# Deploy to GKE
task k8s:setup PLATFORM=gke ENV=demo

# Operations
task k8s:status ENV=demo
task k8s:logs ENV=demo
task k8s:deploy ENV=demo      # Helm upgrade
task k8s:reload ENV=demo      # Restart pods
```

After deployment, provision tenants via sukko-cli:
```bash
sukko tenant create --id <tenant> --name "<name>"
sukko key create --tenant <tenant> --generate
sukko rules routing set --tenant <tenant> --file routing.json
sukko rules channels set --tenant <tenant> --public "*"
```

## Editions

| Feature | Community | Pro | Enterprise |
|---------|-----------|-----|------------|
| WebSocket transport | Yes | Yes | Yes |
| JWT + API key auth | Yes | Yes | Yes |
| Tenant isolation | Yes | Yes | Yes |
| Tenants | 3 | 50 | Unlimited |
| Connections | 100 | 10,000 | Unlimited |
| SSE transport | - | Yes | Yes |
| REST publish | - | Yes | Yes |
| Kafka/Redpanda backend | - | Yes | Yes |
| Token revocation | - | Yes | Yes |
| Push notifications | - | - | Yes |

## Key Technologies

- **Go 1.26+** with modern features
- **franz-go** for Kafka/Redpanda consumption
- **NATS** for inter-pod broadcast
- **gRPC + protobuf** for internal service communication
- **gorilla/websocket** for WebSocket connections
- **zerolog** for structured logging
- **Prometheus** for metrics
- **Helm 3** for Kubernetes deployments
- **Terraform** for cloud infrastructure (DOKS, GKE)

## Documentation

- [CLAUDE.md](CLAUDE.md) — Architecture, source structure, constitution
- [sukko-cli README](https://github.com/klurvio/sukko-cli) — CLI reference
