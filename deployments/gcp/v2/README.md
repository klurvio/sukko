# GCP v2 Deployment - Loadtest & Publisher

Deploy loadtest and publisher tools to a GCP VM for testing against K8s or v1 GCP backends.

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │     GCP VM (e2-standard-8)          │
                    │     8 vCPU, 32GB RAM                │
                    │                                     │
                    │  ┌───────────┐  ┌───────────────┐  │
                    │  │ loadtest  │  │  publisher    │  │
                    │  │  (Go)     │  │  (Node.js)    │  │
                    │  └─────┬─────┘  └───────┬───────┘  │
                    │        │                 │          │
                    └────────┼─────────────────┼──────────┘
                             │                 │
                   WebSocket │       Kafka     │
                             ▼                 ▼
            ┌────────────────────────────────────────────────┐
            │           K8s Cluster or v1 GCP Backend        │
            │  (ws-gateway, ws-server, Redpanda)             │
            └────────────────────────────────────────────────┘
```

## Quick Start

### 1. Create VM

```bash
task gcp:v2:create ENV=develop
```

### 2. Setup VM (clone repo, build images)

```bash
task gcp:v2:setup ENV=develop
```

### 3. Configure Endpoints

Edit the environment file on the VM:
```bash
task gcp:v2:ssh ENV=develop
vim ~/odin-ws/deployments/gcp/v2/environments/develop.env
```

Update `WS_URL` and `KAFKA_BROKERS` to point to your backend.

### 4. Run Tests

```bash
# Start publisher
task gcp:v2:publisher:start ENV=develop

# Run load test
task gcp:v2:loadtest:capacity ENV=develop
```

## Environments

| Environment | VM Type | Instance Name | Use Case |
|-------------|---------|---------------|----------|
| local | None (Docker) | - | Local testing |
| develop | e2-standard-8 | odin-tools-dev | Dev testing |
| staging | e2-standard-8 | odin-tools-staging | Staging tests |

## Commands

### Infrastructure

```bash
task gcp:v2:create ENV=develop    # Create VM
task gcp:v2:delete ENV=develop    # Delete VM
task gcp:v2:setup ENV=develop     # Setup VM
task gcp:v2:ssh ENV=develop       # SSH to VM
task gcp:v2:status ENV=develop    # Show status
```

### Loadtest

```bash
task gcp:v2:loadtest:start ENV=develop
task gcp:v2:loadtest:stop ENV=develop
task gcp:v2:loadtest:logs ENV=develop
task gcp:v2:loadtest:capacity ENV=develop    # 18K connections
task gcp:v2:loadtest:stress ENV=develop      # Overload test
```

### Publisher

```bash
task gcp:v2:publisher:start ENV=develop RATE=25
task gcp:v2:publisher:stop ENV=develop
task gcp:v2:publisher:status ENV=develop
task gcp:v2:publisher:logs ENV=develop
```

### Local (no GCP)

```bash
task gcp:v2:local:up
task gcp:v2:local:down
task gcp:v2:local:loadtest:run CONNECTIONS=100
```

## Kernel Tuning

The VM setup script (`scripts/setup-vm.sh`) configures kernel parameters for high connection counts:

- `fs.file-max = 2097152` - Maximum file descriptors
- `net.ipv4.ip_local_port_range = 1024 65535` - Ephemeral port range
- `net.ipv4.tcp_tw_reuse = 1` - TIME_WAIT socket reuse
- `net.core.somaxconn = 65535` - Listen backlog
- `net.ipv4.tcp_max_syn_backlog = 65535` - SYN backlog

These settings allow the VM to handle 18K+ WebSocket connections.

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `WS_URL` | WebSocket server URL | ws://localhost:3000/ws |
| `HEALTH_URL` | Health check URL | http://localhost:3000/health |
| `KAFKA_BROKERS` | Kafka/Redpanda brokers | localhost:9092 |
| `TARGET_CONNECTIONS` | Max connections for loadtest | 18000 |
| `RAMP_RATE` | Connections per second | 100 |
| `DURATION` | Test duration in seconds | 1800 |
| `PUBLISH_RATE` | Events per second | 25 |

## Cost

| Component | Monthly Cost (Spot) | Monthly Cost (On-Demand) |
|-----------|---------------------|--------------------------|
| e2-standard-8 | ~$70 | ~$195 |

Use Spot VMs for cost savings if preemption is acceptable for testing.
