# Configuration Consolidation Plan

## Executive Summary

**Problem:** Configuration values are duplicated across multiple files, requiring changes in 5-8 places when updating a single value.

**Solution:** Implement a 3-tier configuration hierarchy using:
1. **Shared base configs** (common defaults)
2. **Environment-specific overlays** (local/gcp differences)
3. **Instance-specific overrides** (individual service customization)

---

## Current State Analysis

### Configuration Files (Current)

```
deployments/
├── local/
│   ├── .env.local                    # 80+ lines of config
│   ├── .env.local.example            # 80+ lines (duplicate)
│   ├── docker-compose.yml            # Resource limits, ports, volumes
│   └── configs/                      
│       ├── prometheus.yml            # Scrape configs
│       ├── loki-config.yml           # Loki settings
│       └── promtail-config.yml       # Log shipping
├── gcp/distributed/
│   ├── backend/
│   │   ├── docker-compose.yml        # Resource limits, ports
│   │   ├── prometheus.yml            # Different scrape targets
│   │   ├── loki-config.yml           # Same as local
│   │   └── promtail-config.yml       # Same as local
│   └── ws-server/
│       ├── .env.production           # 80+ lines (mostly duplicate)
│       ├── docker-compose.yml        # Different resources
│       └── promtail-config.yml       # Remote Loki URL
└── ws/
    └── config.go                     # Go defaults (3rd copy)
```

### Duplication Problem Matrix

| Configuration Value | Appears In | Total Copies |
|-------------------|------------|--------------|
| `WS_MAX_KAFKA_RATE` | .env.local, .env.production, config.go defaults | 3 places |
| `WS_MAX_BROADCAST_RATE` | .env.local, .env.production, config.go defaults | 3 places |
| `WS_CPU_REJECT_THRESHOLD` | .env.local, .env.production, config.go defaults | 3 places |
| `WS_CPU_PAUSE_THRESHOLD` | .env.local, .env.production, config.go defaults | 3 places |
| `KAFKA_TOPICS` list | Backend compose, WS .env files, scripts | 4 places |
| Resource limits (CPU/Memory) | Docker compose files, .env files | 6 places |
| Prometheus scrape config | local/prometheus.yml, gcp/prometheus.yml | 2 places |
| Loki retention | local/loki-config.yml, gcp/loki-config.yml | 2 places |
| Port mappings | Docker compose files, READMEs, Taskfiles | 8+ places |

**Total Duplication Estimate:** 40-50 configuration values duplicated across 2-8 locations each

---

## Consolidation Strategy

### Tier 1: Shared Base Configuration

**Goal:** Single source of truth for common configuration

#### Option A: Environment Variable Templating (Recommended)

```
deployments/
├── shared/
│   ├── base.env                     # Base configuration
│   ├── kafka-topics.env             # Kafka topic list
│   ├── resource-formulas.yml        # Calculation formulas as YAML
│   └── ports.env                    # Standard port mappings
```

**base.env:**
```bash
# Shared configuration for all environments
# Override in environment-specific .env files

# Rate limits (production-validated defaults)
WS_MAX_KAFKA_RATE=1000
WS_MAX_BROADCAST_RATE=25

# Safety thresholds
WS_CPU_REJECT_THRESHOLD=75.0
WS_CPU_PAUSE_THRESHOLD=80.0

# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Monitoring
METRICS_INTERVAL=15s
```

**kafka-topics.env:**
```bash
# Kafka topics (single source of truth)
KAFKA_TOPICS=sukko.trades,sukko.liquidity,sukko.balances,sukko.metadata,sukko.social,sukko.community,sukko.creation,sukko.analytics
KAFKA_PARTITIONS=12
KAFKA_REPLICAS=1
```

**ports.env:**
```bash
# Standard port mappings
WS_SERVER_INTERNAL_PORT=3002
KAFKA_INTERNAL_PORT=9092
PROMETHEUS_PORT=9090
GRAFANA_PORT=3000
LOKI_PORT=3100
```

**resource-formulas.yml:**
```yaml
# Resource calculation formulas (documentation + automation)
worker_pool_size:
  formula: "max(32, CPU_LIMIT * 2)"
  description: "Minimum 32 workers, or 2x CPU cores"
  
worker_queue_size:
  formula: "WORKER_POOL_SIZE * 100"
  description: "100 tasks per worker"
  
max_goroutines:
  formula: "((MAX_CONNECTIONS * 2) + WORKER_POOL_SIZE + 13) * 1.2"
  description: "Connection handlers + workers + overhead with 20% buffer"

memory_per_connection:
  value: 180000  # 180KB
  breakdown:
    client_struct: 200
    send_channel: 131072  # 256 slots * 512 bytes
    replay_buffer: 51200  # 100 * 512 bytes
    overhead: 2048
```

#### Option B: YAML-Based Configuration (Alternative)

```
deployments/
├── shared/
│   └── config.yml
```

**config.yml:**
```yaml
# Shared configuration across all deployments
shared:
  rate_limits:
    kafka_rate: 1000
    broadcast_rate: 25
  
  safety_thresholds:
    cpu_reject: 75.0
    cpu_pause: 80.0
  
  kafka:
    topics:
      - sukko.trades
      - sukko.liquidity
      - sukko.balances
      - sukko.metadata
      - sukko.social
      - sukko.community
      - sukko.creation
      - sukko.analytics
    partitions: 12
    replicas: 1
  
  ports:
    ws_server: 3002
    kafka: 9092
    prometheus: 9090
    grafana: 3000
    loki: 3100
```

### Tier 2: Environment-Specific Overlays

**Goal:** Override base config for local vs GCP

```
deployments/
├── local/
│   ├── .env                         # Extends shared/base.env
│   └── overrides.env                # Local-specific only
└── gcp/distributed/
    ├── ws-server/.env               # Extends shared/base.env
    └── backend/.env                 # Backend-specific
```

**deployments/local/overrides.env:**
```bash
# Local development overrides
WS_MAX_CONNECTIONS=1000          # Lower for dev
WS_MAX_KAFKA_RATE=1000           # Higher for testing
LOG_LEVEL=debug                   # Verbose for development
```

**deployments/gcp/distributed/ws-server/overrides.env:**
```bash
# Production GCP overrides
WS_MAX_CONNECTIONS=12000          # Full capacity
LOG_LEVEL=info                    # Production logging
```

### Tier 3: Composite Configuration Loading

#### Docker Compose Enhancement

```yaml
# deployments/local/docker-compose.yml
services:
  ws-server:
    env_file:
      - ../../shared/base.env          # Tier 1: Shared defaults
      - ../../shared/kafka-topics.env  # Tier 1: Topics
      - ../../shared/ports.env         # Tier 1: Ports
      - ./overrides.env                # Tier 2: Local overrides
      - .env.local                     # Tier 3: Secrets (gitignored)
    environment:
      # Tier 4: Service-specific overrides in compose
      WS_ADDR: ":${WS_SERVER_INTERNAL_PORT:-3002}"
```

**Load Order (precedence):**
1. `shared/base.env` - Lowest priority (defaults)
2. `shared/kafka-topics.env` - Topic definitions
3. `shared/ports.env` - Standard ports
4. `local/overrides.env` - Environment overrides
5. `.env.local` - Secrets & local machine config
6. `docker-compose.yml` environment section - Highest priority

---

## Implementation Plan

### Phase 1: Create Shared Configuration Structure

**Tasks:**
1. Create `deployments/shared/` directory
2. Extract common values from all `.env` files into `shared/base.env`
3. Create `shared/kafka-topics.env` with topic list
4. Create `shared/ports.env` with port mappings
5. Document `shared/resource-formulas.yml` for future automation

**Files to create:**
- `deployments/shared/base.env` (~30 lines)
- `deployments/shared/kafka-topics.env` (~10 lines)
- `deployments/shared/ports.env` (~15 lines)
- `deployments/shared/resource-formulas.yml` (~50 lines, documentation)
- `deployments/shared/README.md` (usage guide)

### Phase 2: Refactor Environment Files

**Local:**
1. Rename `.env.local` → `.env.local.full` (backup)
2. Create `local/overrides.env` with ONLY local-specific values
3. Keep `.env.local` for secrets (GRAFANA_PASSWORD, etc.)
4. Update `docker-compose.yml` to use `env_file` array

**GCP:**
1. Rename `.env.production` → `.env.production.full` (backup)
2. Create `gcp/distributed/ws-server/overrides.env` with ONLY gcp-specific values
3. Keep `.env.production` for secrets
4. Update `docker-compose.yml` to use `env_file` array

### Phase 3: Update Docker Compose Files

**Changes:**
- Replace single `env_file:` with array of files
- Remove hardcoded values that now come from shared configs
- Use `${VAR:-default}` syntax for port mappings

**Example transformation:**
```yaml
# Before
env_file:
  - .env.local
ports:
  - "3005:3002"

# After
env_file:
  - ../../shared/base.env
  - ../../shared/kafka-topics.env
  - ../../shared/ports.env
  - ./overrides.env
  - .env.local
ports:
  - "${WS_SERVER_EXTERNAL_PORT:-3005}:${WS_SERVER_INTERNAL_PORT:-3002}"
```

### Phase 4: Create Configuration Scripts

**Automate common tasks:**

**scripts/shared/validate-config.sh:**
```bash
#!/bin/bash
# Validates that all required env vars are set
# Checks for conflicts between env files
# Outputs final merged configuration
```

**scripts/shared/calculate-resources.sh:**
```bash
#!/bin/bash
# Calculates worker pool size from CPU
# Calculates max goroutines from connections
# Based on formulas in resource-formulas.yml
```

**scripts/shared/sync-configs.sh:**
```bash
#!/bin/bash
# Ensures Prometheus configs are in sync
# Ensures Loki configs match
# Updates all docker-compose files with new shared values
```

### Phase 5: Update Taskfiles

**Add tasks for configuration management:**

**taskfiles/shared/config.yml:**
```yaml
version: '3'

tasks:
  validate:
    desc: Validate all configuration files
    cmds:
      - ./scripts/shared/validate-config.sh

  show:local:
    desc: Show merged local configuration
    cmds:
      - |
        docker-compose -f deployments/local/docker-compose.yml \
          config --services ws-server --no-interpolate

  show:gcp:
    desc: Show merged GCP configuration
    cmds:
      - |
        export BACKEND_INTERNAL_IP=10.128.0.2
        docker-compose -f deployments/gcp/distributed/ws-server/docker-compose.yml \
          config --services ws-go --no-interpolate

  diff:
    desc: Show configuration differences between local and GCP
    cmds:
      - task: show:local > /tmp/local-config.txt
      - task: show:gcp > /tmp/gcp-config.txt
      - diff /tmp/local-config.txt /tmp/gcp-config.txt || true
```

### Phase 6: Documentation Updates

1. Update all READMEs to reference shared configs
2. Create `deployments/shared/README.md` with usage guide
3. Update `docs/CAPACITY_PLANNING.md` to reference `resource-formulas.yml`
4. Add configuration management section to main README

---

## Benefit Analysis

### Before Consolidation

**Scenario:** Change `WS_MAX_KAFKA_RATE` from 1000 to 2000

**Files to edit:**
1. `deployments/local/.env.local`
2. `deployments/local/.env.local.example`
3. `deployments/gcp/distributed/ws-server/.env.production`
4. `ws/config.go` (default value)
5. `docs/CAPACITY_PLANNING.md` (documentation)

**Total:** 5 files, ~10 minutes

### After Consolidation

**Scenario:** Change `WS_MAX_KAFKA_RATE` from 1000 to 2000

**Files to edit:**
1. `deployments/shared/base.env` (1 line)

**Total:** 1 file, 1 minute

**Improvement:** 80% reduction in time and error risk

---

## Configuration Hierarchy Examples

### Example 1: Rate Limiting

**Inheritance chain:**
```
shared/base.env:
  WS_MAX_KAFKA_RATE=1000

local/overrides.env:
  # No override - uses 1000

gcp/distributed/ws-server/overrides.env:
  # No override - uses 1000

Result: Both local and GCP use 1000 msg/sec
```

### Example 2: Connections (Environment-Specific)

**Inheritance chain:**
```
shared/base.env:
  WS_MAX_CONNECTIONS=10000    # Reasonable default

local/overrides.env:
  WS_MAX_CONNECTIONS=1000     # Override for dev

gcp/distributed/ws-server/overrides.env:
  WS_MAX_CONNECTIONS=12000    # Override for production

Result: Local=1000, GCP=12000
```

### Example 3: Log Level (Per-Developer Override)

**Inheritance chain:**
```
shared/base.env:
  LOG_LEVEL=info

local/overrides.env:
  LOG_LEVEL=debug             # Default for all devs

.env.local (gitignored):
  LOG_LEVEL=trace             # Personal override

Result: Developer gets trace, others get debug
```

---

## Migration Strategy

### Backward Compatibility

**Approach:** Keep old files during transition

```
deployments/local/
├── .env.local.full          # Old file (backup, not loaded)
├── .env.local               # Secrets only
└── overrides.env            # NEW: Environment-specific
```

**Transition period:** 2 weeks
- Old `.env.local.full` kept for reference
- Can rollback by renaming files
- Update Taskfiles to use new structure

### Rollback Plan

```bash
# If issues arise, quick rollback:
cd deployments/local
mv .env.local .env.local.secrets
mv .env.local.full .env.local
git checkout docker-compose.yml
```

---

## Advanced: Automated Configuration Sync

### Vision: Single Source Generator

**Future enhancement (Phase 7):**

```
scripts/
└── generate-configs.py      # Generates all configs from YAML
```

**Usage:**
```bash
# Edit master config
vim deployments/config.master.yml

# Regenerate all environment configs
./scripts/generate-configs.py

# Review changes
git diff deployments/
```

**Master config:**
```yaml
# deployments/config.master.yml
environments:
  local:
    max_connections: 1000
    cpu_limit: 1.0
    memory_limit: 4G
  
  gcp_distributed:
    ws_server:
      max_connections: 12000
      cpu_limit: 1.0
      memory_limit: 14.5G
    backend:
      cpu_limit: 2.0
      memory_limit: 2G

shared:
  rate_limits:
    kafka: 1000
    broadcast: 25
  
  kafka:
    topics:
      - name: sukko.trades
        retention: 30s
      - name: sukko.liquidity
        retention: 60s
      # ... etc
```

**Generator outputs:**
- All `.env` files
- All `docker-compose.yml` resource limits
- Prometheus scrape configs
- Documentation files

---

## Recommended Immediate Actions

### Quick Win (1 hour)

1. **Create `shared/base.env`** with 10 most common values:
   - `WS_MAX_KAFKA_RATE`
   - `WS_MAX_BROADCAST_RATE`
   - `WS_CPU_REJECT_THRESHOLD`
   - `WS_CPU_PAUSE_THRESHOLD`
   - `LOG_FORMAT`
   - `METRICS_INTERVAL`
   - (+ 4 more)

2. **Update `local/docker-compose.yml`** to include it:
   ```yaml
   env_file:
     - ../../shared/base.env
     - .env.local
   ```

3. **Test:** `cd deployments/local && docker-compose config`

4. **Verify values are loaded correctly**

**Result:** Immediate 50% reduction in duplication for those 10 values

### Full Implementation (1-2 days)

Follow Phase 1-6 above for complete consolidation.

---

## Summary

**Problem:** 40-50 configuration values duplicated across 2-8 locations

**Solution:** 3-tier configuration hierarchy
1. Shared base configs (single source of truth)
2. Environment overlays (local vs gcp)
3. Instance overrides (per-developer, per-service)

**Benefits:**
- **80% reduction** in configuration maintenance time
- **Eliminates** configuration drift between environments
- **Single source of truth** for common values
- **Easier onboarding** (one file to understand)
- **Safer updates** (change once, apply everywhere)

**Recommendation:** Start with Quick Win, then implement Phase 1-3 over next sprint.
