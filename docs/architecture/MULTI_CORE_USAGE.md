# Multi-Core WebSocket Server - Usage Guide

## Overview

The WebSocket server now supports two deployment modes via a single set of Taskfile commands:

- **Single-core**: Traditional single-process deployment (good for development)
- **Multi-core**: Sharded deployment with load balancer (for production capacity: 12K+ connections)

## Quick Start

### 1. Set Deployment Mode

Edit `/deployments/v1/local/.env.local`:

```bash
# For single-core (default)
WS_MODE=single

# For multi-core
WS_MODE=multi
```

### 2. Deploy

Same commands work for both modes:

```bash
# Full setup from scratch
task local:deploy:setup

# Or quick start (if already set up)
task up
```

The taskfiles automatically detect `WS_MODE` and use the appropriate:
- Docker Compose file (`docker-compose.yml` vs `docker-compose.multi.yml`)
- Container name (`sukko-local` vs `sukko-multi-local`)
- Configuration (`overrides.env` vs `overrides.multi.env`)

## Architecture Comparison

### Single-Core Mode
```
┌─────────────────┐
│  WS Server      │
│  (1 core)       │
│  Port: 3005     │
│  12K max conns  │
└─────────────────┘
```

### Multi-Core Mode
```
┌──────────────────────┐
│  Load Balancer       │
│  Port: 3005          │  ← Public endpoint
└──────────┬───────────┘
           │
     ┌─────┴──────┬──────────┐
     ▼            ▼          ▼
┌─────────┐  ┌─────────┐  ┌─────────┐
│ Shard 0 │  │ Shard 1 │  │ Shard 2 │
│ 3002    │  │ 3003    │  │ 3004    │
│ 4K max  │  │ 4K max  │  │ 4K max  │
└────┬────┘  └────┬────┘  └────┬────┘
     │            │            │
     └────────────┼────────────┘
                  ▼
          ┌───────────────┐
          │ BroadcastBus  │
          │ (Redis Pub/Sub)│  ← Enables multi-VM scaling
          └───────────────┘
```

## Common Commands (Mode-Aware)

All commands automatically adapt to the current `WS_MODE`:

```bash
# Deployment
task local:deploy:setup        # Complete setup
task local:deploy:quick-start  # Quick start
task local:deploy:stop         # Stop all

# Service control
task local:start:ws            # Start WS server
task local:restart:ws          # Restart WS server
task local:rebuild:ws          # Rebuild after code changes

# Monitoring
task local:logs:ws             # Tail logs
task local:health:ws           # Health check
task local:stats:ws            # Show stats

# Load testing
task local:test:sustained      # Run capacity test
```

## Configuration Files

### Single-Core
- **Compose**: `deployments/v1/local/docker-compose.yml`
- **Dockerfile**: `ws/Dockerfile` (builds `cmd/single`)
- **Overrides**: `deployments/v1/local/overrides.env`
- **Resources**: 1 CPU, 14GB RAM
- **Max Connections**: 12,000 (CPU-bound at ~30%)

### Multi-Core
- **Compose**: `deployments/v1/local/docker-compose.multi.yml`
- **Dockerfile**: `ws/Dockerfile.multi` (builds `cmd/multi`)
- **Overrides**: `deployments/v1/local/overrides.multi.env`
- **Resources**: 3 CPUs (1 per shard), 14GB RAM
- **Max Connections**: 12,000 (4K per shard)
- **Architecture**: 3 shards + load balancer + broadcast bus

## Switching Between Modes

To switch modes:

1. **Update .env.local**:
   ```bash
   # Change this line
   WS_MODE=multi  # or single
   ```

2. **Restart deployment**:
   ```bash
   task local:deploy:stop
   task local:deploy:quick-start
   ```

The taskfiles will automatically use the correct compose file and container names.

## Load Balancing Strategy

**Multi-core mode only**:

- **Algorithm**: Least Connections
- **Selection**: LoadBalancer queries all shards and forwards to the shard with fewest active connections
- **Capacity-Aware**: Shards at max capacity (4K each) are excluded from selection
- **Overload Handling**: If all shards are full, returns HTTP 503

## Message Broadcasting

**Multi-core mode only**:

1. Kafka consumer in each shard receives messages for its partition(s)
2. Shard publishes to central **BroadcastBus** (Redis Pub/Sub, channel: `ws.broadcast`)
3. Redis broadcasts to ALL WS instances (multi-VM support)
4. Each instance's BroadcastBus fans out to its local shard subscriber channels
5. Each shard broadcasts locally to its connected clients

This ensures all clients across all shards **and all VM instances** receive every message.

**Redis BroadcastBus Features:**
- Auto-detection: 1 address = direct mode, 3+ addresses = Sentinel HA mode
- Automatic reconnection with exponential backoff
- Health monitoring (10s PING interval)
- Non-blocking publish (100ms timeout)
- See [Redis BroadcastBus Quick Start](../REDIS_BROADCAST_QUICKSTART.md) for deployment details

## Observability

Both modes expose the same metrics at `:3005/metrics`:

- `ws_current_connections` - Active WebSocket connections
- `ws_total_messages` - Total messages broadcast
- `ws_cpu_percent` - CPU usage
- `ws_memory_bytes` - Memory usage
- (Multi-core adds) `ws_shard_connections{shard_id="N"}` - Per-shard connection counts

View in Grafana: http://localhost:3011 (admin/admin)

## Performance Comparison

| Metric | Single-Core | Multi-Core |
|--------|-------------|------------|
| Max Connections | 12K | 12K+ |
| CPU Usage (12K) | ~30% (1 core) | ~30% (distributed) |
| Memory (12K) | ~3.6GB | ~3.6GB (distributed) |
| Latency | < 1ms | < 2ms (LB overhead) |
| Complexity | Low | Medium |
| Use Case | Development, testing | Production, high capacity |

## Troubleshooting

### Check Current Mode
```bash
# In container
docker exec sukko-local env | grep WS_MODE      # Single-core
docker exec sukko-multi-local env | grep WS_MODE  # Multi-core
```

### Verify Architecture (Multi-core)
```bash
# Check shard processes
docker exec sukko-multi-local ps aux | grep sukko-multi

# Check internal ports (should see 3002, 3003, 3004, 3005)
docker exec sukko-multi-local netstat -tlnp
```

### Switch Not Working?
- Ensure `.env.local` has `WS_MODE` set correctly
- Stop all containers: `task local:deploy:stop`
- Verify no orphaned containers: `docker ps -a | grep sukko`
- Restart: `task local:deploy:quick-start`

## GCP Production Deployment

### Setting Mode on GCP

SSH into your GCP ws-server instance and edit `.env.production`:

```bash
# SSH into instance
gcloud compute ssh sukko-go --zone=us-central1-a

# Edit .env.production
sudo su - deploy
cd /home/deploy/sukko/deployments/v1/gcp/distributed/ws-server
nano .env.production

# Change this line:
WS_MODE=multi  # or single
```

### Deploy on GCP

Same commands work for both modes:

```bash
# From your local machine

# Full deployment
task gcp:deploy

# Or individual components
task gcp:services:start:ws      # Start ws-server (mode-aware)
task gcp:deployment:rebuild:ws  # Rebuild after code changes
task gcp:services:restart:ws    # Restart ws-server
```

The taskfiles automatically:
- Read `WS_MODE` from `.env.production`
- Use `docker-compose.multi.yml` for multi-core
- Use `docker-compose.yml` for single-core

### GCP Architecture Notes

**Single-Core (WS_MODE=single)**:
- Instance: e2-standard-4 (4 vCPU, 16GB RAM)
- Used: 1 CPU, 14.5GB RAM
- Container: `sukko-go`
- Max Connections: 12,000

**Multi-Core (WS_MODE=multi)**:
- Instance: e2-standard-4 (4 vCPU, 16GB RAM)
- Used: 3 CPUs (1 per shard), 14.5GB RAM
- Container: `sukko-multi`
- Max Connections: 12,000+ (4K per shard)

Both modes use the same external port (3004) and connect to the same backend instance.

## Dependencies Updated

- **Redpanda**: v25.2.10 (was v24.2.11)
- **Redpanda Console**: v3.2.2 (was v2.7.2)
- **franz-go**: v1.20.3 (was v1.18.0)

All updated to latest stable versions as of Nov 2025.
