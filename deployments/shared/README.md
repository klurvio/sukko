# Shared Configuration Files

## Overview

This directory contains shared configuration that is used across all deployment environments (local, GCP, etc.).

**Purpose:** Single source of truth for common configuration values, eliminating duplication.

## Files

### base.env
**Common configuration defaults for all environments**

Contains:
- Rate limiting (Kafka, broadcast)
- Safety thresholds (CPU reject/pause)
- Logging configuration
- Monitoring intervals
- Worker pool auto-calculation settings

**Load priority:** Lowest (can be overridden by environment-specific configs)

### kafka-topics.env
**Kafka topic configuration**

Contains:
- Complete list of topics
- Partition count
- Replication factor
- Per-topic retention policies

**Used by:** Publisher, WS Server, topic creation scripts

### ports.env
**Standard port mappings**

Contains:
- Internal ports (container-side)
- External ports for local development
- External ports for GCP production

**Used by:** Docker Compose, Prometheus, Grafana, documentation

### resource-formulas.yml
**Resource calculation formulas (documentation)**

Contains:
- Worker pool sizing formulas
- Goroutine limit calculations
- Memory per connection breakdown
- CPU capacity estimates
- Reference capacity targets

**Purpose:** Documentation and future automation (not loaded by services)

## Usage

### In Docker Compose

```yaml
services:
  ws-server:
    env_file:
      - ../../shared/base.env          # Tier 1: Shared defaults
      - ../../shared/kafka-topics.env  # Tier 1: Topics
      - ../../shared/ports.env         # Tier 1: Ports
      - ./overrides.env                # Tier 2: Environment overrides
      - .env.local                     # Tier 3: Secrets (gitignored)
```

### Load Order (Priority)

1. **`shared/base.env`** - Lowest priority (defaults)
2. **`shared/kafka-topics.env`** - Topic definitions
3. **`shared/ports.env`** - Standard ports
4. **`local/overrides.env`** or **`gcp/overrides.env`** - Environment-specific
5. **`.env.local`** or **`.env.production`** - Secrets & machine-specific (gitignored)
6. **`docker-compose.yml` environment section** - Highest priority (inline overrides)

**Rule:** Later files override earlier files for the same variable.

## Configuration Hierarchy

### Tier 1: Shared Base (This Directory)
- Common defaults that work for all environments
- Rate limits, thresholds, logging, ports, topics
- **Change frequency:** Rare (only when tuning production)

### Tier 2: Environment Overlays
- `local/overrides.env` - Local development overrides
- `gcp/distributed/ws-server/overrides.env` - Production overrides
- **Change frequency:** Occasional (environment-specific tuning)

### Tier 3: Secrets & Personal
- `.env.local` (gitignored) - Local secrets and personal settings
- `.env.production` (gitignored) - Production secrets
- **Change frequency:** Per-developer, per-deployment

## What Goes Where?

### ✅ Put in `shared/base.env`
Values that are **identical across all environments**:
- `WS_ADDR=:3002` - Server listen address (same everywhere)
- `WS_MAX_BROADCAST_RATE=25` - Broadcast rate limit (same everywhere)
- `WS_CPU_REJECT_THRESHOLD=75.0` - Safety thresholds (same everywhere)
- `LOG_FORMAT=json` - Default log format (same everywhere)
- `METRICS_INTERVAL=15s` - Metrics collection interval (same everywhere)

**Rule:** If local and GCP would use the same value, put it in `base.env`.

### ✅ Put in `overrides.env` (environment-specific)
Values that **differ between environments**:
- `ENVIRONMENT` - Local vs production
- `KAFKA_BROKERS` - redpanda:9092 vs ${BACKEND_INTERNAL_IP}:9092
- `WS_MEMORY_LIMIT` - 4GB (laptop) vs 14.5GB (server)
- `WS_MAX_CONNECTIONS` - 1,000 (dev) vs 12,000 (production)
- `WS_MAX_KAFKA_RATE` - 1,000 (testing) vs 25 (real traffic)
- `LOG_LEVEL` - debug (dev) vs info (production)
- `LOG_FORMAT` - pretty (dev) vs json (production)

**Rule:** If local and GCP need different values, put it in each `overrides.env`.

### ✅ Put in `.env.production` (gitignored secrets)
Values that are **sensitive or machine-specific**:
- `BACKEND_INTERNAL_IP=10.128.0.2` - GCP internal networking
- `API_KEY=secret-key-here` - API credentials
- `DATABASE_PASSWORD=secret-pass` - Database credentials

**Rule:** Never commit secrets. Use gitignored `.env.production`.

## Examples

### Example 1: Using Default Value

**File: `shared/base.env`**
```bash
WS_MAX_KAFKA_RATE=1000
```

**File: `local/overrides.env`**
```bash
# No override
```

**Result:** Both local and GCP use `1000 msg/sec`

### Example 2: Environment-Specific Override

**File: `shared/base.env`**
```bash
WS_MAX_CONNECTIONS=10000
```

**File: `local/overrides.env`**
```bash
WS_MAX_CONNECTIONS=1000  # Lower for dev
```

**File: `gcp/overrides.env`**
```bash
WS_MAX_CONNECTIONS=12000  # Full capacity
```

**Result:** Local=1000, GCP=12000

### Example 3: Personal Override

**File: `shared/base.env`**
```bash
LOG_LEVEL=info
```

**File: `local/overrides.env`**
```bash
LOG_LEVEL=debug  # Default for all devs
```

**File: `.env.local` (your machine)**
```bash
LOG_LEVEL=trace  # Personal preference
```

**Result:** You get `trace`, other devs get `debug`, GCP gets `info`

## Updating Configuration

### To Change a Common Value

1. **Edit `shared/base.env`** (or relevant shared file)
2. **Commit and push** (affects all environments)
3. **Redeploy** environments to pick up changes

**Example:**
```bash
# Change broadcast rate from 25 to 30
vim deployments/shared/base.env
# Change: WS_MAX_BROADCAST_RATE=25 → WS_MAX_BROADCAST_RATE=30

git add deployments/shared/base.env
git commit -m "Increase broadcast rate to 30 msg/sec"
git push

# Redeploy local
task local:deploy:quick-start

# Redeploy GCP (if needed)
task gcp:deploy:restart
```

### To Change an Environment-Specific Value

1. **Edit `local/overrides.env`** or **`gcp/overrides.env`**
2. **Commit and push**
3. **Redeploy** affected environment only

**Example:**
```bash
# Increase local connections for testing
vim deployments/local/overrides.env
# Change: WS_MAX_CONNECTIONS=1000 → WS_MAX_CONNECTIONS=2000

# Redeploy local only
task local:deploy:quick-start
```

### To Add a Personal Override (No Commit)

1. **Edit `.env.local`** (gitignored)
2. **Redeploy** (affects your machine only)

**Example:**
```bash
# Enable trace logging for debugging
echo "LOG_LEVEL=trace" >> deployments/local/.env.local

# Restart
task local:restart:ws
```

## Benefits

### Before Shared Configs
```
Update WS_MAX_KAFKA_RATE:
1. deployments/local/.env.local
2. deployments/local/.env.local.example
3. deployments/gcp/distributed/ws-server/.env.production
4. ws/config.go
5. docs/CAPACITY_PLANNING.md

Total: 5 files, ~10 minutes, high error risk
```

### After Shared Configs
```
Update WS_MAX_KAFKA_RATE:
1. deployments/shared/base.env

Total: 1 file, 1 minute, no error risk
```

**Improvement:** 80% reduction in time and error risk

## Validation

### View Merged Configuration

**Local:**
```bash
cd deployments/local
docker-compose config
```

**GCP:**
```bash
cd deployments/gcp/distributed/ws-server
export BACKEND_INTERNAL_IP=10.128.0.2
docker-compose config
```

### Check Specific Value

```bash
# What value will WS server see?
docker-compose config | grep WS_MAX_KAFKA_RATE
```

## Troubleshooting

### Value Not Being Used

**Symptoms:** Changed a value but service still uses old value

**Check load order:**
```bash
# See all env_file entries
docker-compose config | grep -A 20 "ws-server:"

# Verify file exists
ls -la ../../shared/base.env

# Check if overridden later
grep WS_MAX_KAFKA_RATE local/overrides.env
grep WS_MAX_KAFKA_RATE .env.local
```

**Solution:** Remember later files override earlier files

### Configuration Drift

**Symptoms:** Local and GCP behave differently unexpectedly

**Check differences:**
```bash
# Compare merged configs
cd deployments/local && docker-compose config > /tmp/local.yml
cd ../gcp/distributed/ws-server && docker-compose config > /tmp/gcp.yml
diff /tmp/local.yml /tmp/gcp.yml
```

**Solution:** Ensure environment-specific overrides are intentional

## Related Documentation

- [Configuration Consolidation Plan](../../docs/CONFIGURATION_CONSOLIDATION_PLAN.md) - Full design
- [Capacity Planning](../../docs/CAPACITY_PLANNING.md) - Resource calculations
- [Local Deployment Guide](../local/README.md) - Local usage
- [GCP Deployment Guide](../gcp/README.md) - GCP usage

## Future Enhancements

### Phase 7: Automated Configuration Generation

**Vision:** Generate all configs from a master YAML file

```bash
# Edit master config
vim deployments/config.master.yml

# Generate all environment configs
./scripts/generate-configs.py

# Review and commit
git diff deployments/
```

**See:** `docs/CONFIGURATION_CONSOLIDATION_PLAN.md` (Phase 7)
