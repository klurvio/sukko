# GCP v2 - Command Reference

Deploy loadtest and publisher tools to a GCP VM for testing against K8s or v1 GCP backends.

## Quick Reference

```bash
# Infrastructure
task gcp:v2:create ENV=develop       # Create VM
task gcp:v2:setup ENV=develop        # Setup VM (Docker, kernel tuning)
task gcp:v2:ssh ENV=develop          # SSH into VM
task gcp:v2:delete ENV=develop       # Delete VM

# Loadtest
task gcp:v2:loadtest:start ENV=develop
task gcp:v2:loadtest:capacity ENV=develop    # 18K connections
task gcp:v2:loadtest:logs ENV=develop

# Publisher
task gcp:v2:publisher:start ENV=develop RATE=25
task gcp:v2:publisher:logs ENV=develop
task gcp:v2:publisher:stop ENV=develop

# Local (no GCP)
task gcp:v2:local:up
task gcp:v2:local:loadtest:run CONNECTIONS=100
task gcp:v2:local:down
```

## Infrastructure Commands

| Command | Description |
|---------|-------------|
| `task gcp:v2:create ENV=<env>` | Create VM for environment |
| `task gcp:v2:create-spot ENV=<env>` | Create Spot VM (cheaper) |
| `task gcp:v2:delete ENV=<env>` | Delete VM |
| `task gcp:v2:setup ENV=<env>` | Setup VM (Docker, kernel, clone repo) |
| `task gcp:v2:start ENV=<env>` | Start stopped VM |
| `task gcp:v2:stop ENV=<env>` | Stop running VM |
| `task gcp:v2:ssh ENV=<env>` | SSH into VM |
| `task gcp:v2:status ENV=<env>` | Show VM and container status |
| `task gcp:v2:ip ENV=<env>` | Get VM external IP |
| `task gcp:v2:sync-env ENV=<env>` | Sync environment file to VM |

## Loadtest Commands

| Command | Description |
|---------|-------------|
| `task gcp:v2:loadtest:start ENV=<env>` | Start loadtest container |
| `task gcp:v2:loadtest:stop ENV=<env>` | Stop loadtest container |
| `task gcp:v2:loadtest:restart ENV=<env>` | Restart loadtest container |
| `task gcp:v2:loadtest:logs ENV=<env>` | View loadtest logs |
| `task gcp:v2:loadtest:status ENV=<env>` | Show container status |
| `task gcp:v2:loadtest:run ENV=<env> CONNECTIONS=X DURATION=Y` | Run with custom params |
| `task gcp:v2:loadtest:quick ENV=<env>` | Quick test (100 conn, 60s) |
| `task gcp:v2:loadtest:capacity ENV=<env>` | Capacity test (18K conn, 30m) |
| `task gcp:v2:loadtest:stress ENV=<env>` | Stress test (25K conn) |

### Loadtest Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `CONNECTIONS` | 18000 | Target WebSocket connections |
| `RAMP_RATE` | 100 | Connections per second |
| `DURATION` | 1800 | Test duration in seconds |

## Publisher Commands

| Command | Description |
|---------|-------------|
| `task gcp:v2:publisher:start ENV=<env> RATE=X` | Start publisher |
| `task gcp:v2:publisher:stop ENV=<env>` | Stop publisher |
| `task gcp:v2:publisher:restart ENV=<env>` | Restart publisher |
| `task gcp:v2:publisher:logs ENV=<env>` | View publisher logs |
| `task gcp:v2:publisher:status ENV=<env>` | Show container status |
| `task gcp:v2:publisher:set-rate ENV=<env> RATE=X` | Update publish rate |
| `task gcp:v2:publisher:health ENV=<env>` | Check health endpoint |

### Publisher Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `RATE` | 25 | Events per second to publish |

## Local Commands (No GCP)

| Command | Description |
|---------|-------------|
| `task gcp:v2:local:up` | Start both services locally |
| `task gcp:v2:local:down` | Stop local services |
| `task gcp:v2:local:loadtest:run CONNECTIONS=X` | Run loadtest locally |
| `task gcp:v2:local:publisher:start RATE=X` | Start publisher locally |

## Environments

| Environment | VM Instance | Use Case |
|-------------|-------------|----------|
| `local` | None (Docker) | Local development testing |
| `develop` | odin-tools-dev | Development backend testing |
| `staging` | odin-tools-staging | Staging backend testing |

## Configuration Files

Environment files are located at `deployments/gcp/v2/environments/`:

- `local.env` - Local Docker endpoints
- `develop.env` - Development endpoints
- `staging.env` - Staging endpoints

### Key Variables

| Variable | Description |
|----------|-------------|
| `GCP_PROJECT` | GCP project ID |
| `GCP_ZONE` | GCP zone (e.g., us-central1-a) |
| `WS_URL` | WebSocket server URL |
| `HEALTH_URL` | Health check URL |
| `KAFKA_BROKERS` | Kafka/Redpanda broker addresses |
| `TARGET_CONNECTIONS` | Default connection count |
| `PUBLISH_RATE` | Default publish rate |

## Examples

### Full Test Workflow

```bash
# 1. Create and setup VM
task gcp:v2:create ENV=develop
task gcp:v2:setup ENV=develop

# 2. Configure endpoints (edit on VM or sync)
vim deployments/gcp/v2/environments/develop.env
task gcp:v2:sync-env ENV=develop

# 3. Start publisher
task gcp:v2:publisher:start ENV=develop RATE=25

# 4. Run capacity test
task gcp:v2:loadtest:capacity ENV=develop

# 5. View logs
task gcp:v2:loadtest:logs ENV=develop
task gcp:v2:publisher:logs ENV=develop

# 6. Cleanup
task gcp:v2:publisher:stop ENV=develop
task gcp:v2:delete ENV=develop
```

### Quick Local Test

```bash
# Start local services
task gcp:v2:local:up

# Run quick test
task gcp:v2:local:loadtest:run CONNECTIONS=100 DURATION=60

# Cleanup
task gcp:v2:local:down
```

## Cost Estimates

| VM Type | Spot (Monthly) | On-Demand (Monthly) |
|---------|----------------|---------------------|
| e2-standard-8 | ~$70 | ~$195 |

Use `task gcp:v2:create-spot` for cost savings if preemption is acceptable.
