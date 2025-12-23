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

Get the K8s service IPs (after Helm deployment):
```bash
kubectl get svc -n odin-std-develop odin-ws-gateway odin-redpanda-external
```

Update `deployments/gcp/v2/environments/develop.env` with the EXTERNAL-IP values:
```env
WS_URL=ws://<WS_GATEWAY_EXTERNAL_IP>:443/ws
HEALTH_URL=http://<WS_GATEWAY_EXTERNAL_IP>:443/health
KAFKA_BROKERS=<REDPANDA_EXTERNAL_IP>:9092
```

Sync to VM:
```bash
task gcp:v2:sync-env ENV=develop
```

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

## Test Guide: K8s Develop Cluster

Step-by-step guide for testing the K8s develop cluster with low-volume traffic.

### Test Configuration

| Parameter | Loadtest | Publisher |
|-----------|----------|-----------|
| Target endpoint | ws://WS_GATEWAY_IP/ws | REDPANDA_IP:9092 |
| Rate | 5 conn/sec (RAMP_RATE) | 1 msg/sec (RATE) |
| Connections | 100 total | N/A |
| Duration | 300s (5 min) | Continuous |

### Step 1: Get K8s Endpoints

```bash
kubectl get svc -n odin-std-develop odin-ws-gateway odin-redpanda-external
```

Note the EXTERNAL-IP values for ws-gateway and redpanda-external.

### Step 2: Update develop.env

Edit `deployments/gcp/v2/environments/develop.env` with the IPs from Step 1:

```env
# Replace placeholders with actual IPs from kubectl output
WS_URL=ws://<WS_GATEWAY_EXTERNAL_IP>:443/ws
HEALTH_URL=http://<WS_GATEWAY_EXTERNAL_IP>:443/health
KAFKA_BROKERS=<REDPANDA_EXTERNAL_IP>:9092
```

The other settings (connections, rate, channels) have sensible defaults for testing.

### Step 3: Create and Setup VM

```bash
task gcp:v2:create ENV=develop
task gcp:v2:setup ENV=develop
task gcp:v2:sync-env ENV=develop
```

### Step 4: Start Publisher

```bash
task gcp:v2:publisher:start ENV=develop RATE=1
task gcp:v2:publisher:logs ENV=develop
```

### Step 5: Run Loadtest

```bash
task gcp:v2:loadtest:run ENV=develop CONNECTIONS=100 RAMP_RATE=5 DURATION=300
task gcp:v2:loadtest:logs ENV=develop
```

### Step 6: Verify End-to-End Flow

```bash
# Check ws-server receiving messages
kubectl logs -n odin-std-develop -l app.kubernetes.io/name=ws-server -f --tail=50

# Check ws-gateway proxying connections
kubectl logs -n odin-std-develop -l app.kubernetes.io/name=ws-gateway -f --tail=50
```

### Step 7: Cleanup

```bash
task gcp:v2:publisher:stop ENV=develop
task gcp:v2:stop ENV=develop  # Stop VM to save costs
```

### Expected Flow

```
Publisher (VM)                    K8s Cluster                      Loadtest (VM)
     │                                 │                                │
     │ ──── Kafka (1 msg/sec) ────────►│                                │
     │                           Redpanda                               │
     │                                 │                                │
     │                           ws-server                              │
     │                                 │                                │
     │                           ws-gateway ◄──── WS (5 conn/sec) ──────│
     │                                 │                                │
     │                                 │ ────── Messages ───────────────►│
```
