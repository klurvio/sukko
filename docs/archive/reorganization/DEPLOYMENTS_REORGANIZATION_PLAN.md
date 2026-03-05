# Deployments Directory Reorganization Plan

## Executive Summary

Reorganize `deployments/` to separate:
1. **Old NATS-based deployments** → `deployments/old/`
2. **Current Kafka-based local deployment** → `deployments/local/` (cleaned up)
3. **Future Kafka-based GCP deployments** → `deployments/gcp/` (to be created)

**Goals:**
- Remove duplicate/outdated config files
- Archive NATS-based GCP deployments
- Establish clear environment separation (local vs gcp)
- Provide migration templates for GCP Kafka deployment

---

## Current State Analysis

### Directory Structure (Before)
```
deployments/
├── gcp-distributed/
│   ├── backend/
│   │   ├── docker-compose.yml      ✅ UPDATED to Kafka/Redpanda
│   │   └── prometheus.yml          ⚠️  Still references NATS
│   └── ws-server/
│       ├── .env.production         ❌ NATS-based (needs migration)
│       ├── docker-compose.yml      ❌ NATS-based (needs migration)
│       └── promtail-config.yml     ⚠️  May need updates
├── gcp-single/
│   └── docker-compose.yml          ❌ NATS-based (overlay file)
└── local/
    ├── docker-compose.yml          ✅ Kafka/Redpanda
    ├── docker-compose.redpanda.yml ✅ Redpanda standalone
    ├── .env.local                  ✅ Current
    ├── .env.local.example          ✅ Current
    ├── loki-config.yml             ✅ Current (v13 schema)
    ├── prometheus.yml              ✅ Current (Kafka-based)
    ├── promtail-config.yml         ✅ Current
    ├── configs/                    ❌ DUPLICATE/OUTDATED
    │   ├── loki-config.yml         ❌ Old (v11 schema)
    │   ├── prometheus.yml          ❌ Old (NATS-based)
    │   └── promtail-config.yml     ❌ Old
    ├── grafana/                    ✅ Current
    ├── README.md                   ✅ Current
    └── DEPLOYMENT_SUCCESS.md       ✅ Current
```

### Key Findings

1. **gcp-distributed/backend/** - PARTIALLY MIGRATED
   - ✅ docker-compose.yml updated to Kafka/Redpanda
   - ❌ prometheus.yml still references NATS (needs update)
   - ❌ Missing: loki-config.yml, promtail-config.yml, grafana/ directory

2. **gcp-distributed/ws-server/** - NEEDS MIGRATION
   - All files are NATS-based
   - Needs complete rewrite for Kafka

3. **gcp-single/** - OUTDATED
   - Overlay file referencing old local docker-compose.yml
   - NATS-based
   - Should be archived and recreated when needed

4. **local/configs/** - DUPLICATE & OUTDATED
   - All three files are old NATS-based versions
   - Not used by current docker-compose.yml
   - Safe to archive

---

## Target State (After Reorganization)

### New Directory Structure
```
deployments/
├── old/                           # Archived NATS-based deployments
│   ├── gcp-distributed-nats/     # Original distributed setup
│   │   ├── backend/
│   │   └── ws-server/
│   ├── gcp-single-nats/          # Original single-server setup
│   └── local-configs-nats/       # Old config files from local/configs/
├── local/                         # Kafka-based local development (CLEANED)
│   ├── docker-compose.yml
│   ├── docker-compose.redpanda.yml
│   ├── .env.local
│   ├── .env.local.example
│   ├── loki-config.yml
│   ├── prometheus.yml
│   ├── promtail-config.yml
│   ├── grafana/
│   │   └── provisioning/
│   ├── README.md
│   └── DEPLOYMENT_SUCCESS.md
└── gcp/                           # Kafka-based GCP deployments (NEW)
    ├── distributed/               # Multi-instance Kafka setup
    │   ├── backend/
    │   │   ├── docker-compose.yml (from gcp-distributed/backend)
    │   │   ├── prometheus.yml     (UPDATED - remove NATS)
    │   │   ├── loki-config.yml    (COPY from local)
    │   │   ├── promtail-config.yml (COPY from local)
    │   │   └── grafana/           (COPY from local)
    │   ├── ws-server/
    │   │   ├── docker-compose.yml (TO BE CREATED - Kafka version)
    │   │   ├── .env.production    (TO BE CREATED - Kafka version)
    │   │   └── promtail-config.yml
    │   └── README.md              (Deployment instructions)
    ├── single/                    # Single-instance Kafka setup
    │   ├── docker-compose.yml     (TO BE CREATED - Kafka overlay)
    │   └── README.md
    └── README.md                  # GCP deployment overview
```

---

## Reorganization Steps

### Phase 1: Archive Old NATS Deployments

#### Step 1.1: Create old/ directory structure
```bash
mkdir -p deployments/old/gcp-distributed-nats
mkdir -p deployments/old/gcp-single-nats
mkdir -p deployments/old/local-configs-nats
```

#### Step 1.2: Archive gcp-distributed ws-server (NATS-based)
```bash
# Move entire ws-server directory to old/
mv deployments/gcp-distributed/ws-server \
   deployments/old/gcp-distributed-nats/
```

**Files archived:**
- `.env.production` (NATS URL configuration)
- `docker-compose.yml` (NATS consumer)
- `promtail-config.yml`

#### Step 1.3: Archive gcp-single (NATS-based)
```bash
# Move entire directory
mv deployments/gcp-single \
   deployments/old/gcp-single-nats
```

#### Step 1.4: Archive local/configs/ (outdated)
```bash
# Move configs directory
mv deployments/local/configs \
   deployments/old/local-configs-nats
```

**Files archived:**
- `loki-config.yml` (old v11 schema)
- `prometheus.yml` (NATS-based scrape config)
- `promtail-config.yml` (old configuration)

### Phase 2: Create GCP Kafka Deployment Structure

#### Step 2.1: Create gcp/ directory structure
```bash
mkdir -p deployments/gcp/distributed/backend
mkdir -p deployments/gcp/distributed/ws-server
mkdir -p deployments/gcp/single
```

#### Step 2.2: Move backend (already Kafka-migrated)
```bash
# Move backend directory to new location
mv deployments/gcp-distributed/backend \
   deployments/gcp/distributed/
```

#### Step 2.3: Copy missing backend config files from local/
```bash
# Copy Loki config
cp deployments/local/loki-config.yml \
   deployments/gcp/distributed/backend/

# Copy Promtail config
cp deployments/local/promtail-config.yml \
   deployments/gcp/distributed/backend/

# Copy Grafana provisioning
cp -r deployments/local/grafana \
      deployments/gcp/distributed/backend/
```

#### Step 2.4: Update backend prometheus.yml
**File:** `deployments/gcp/distributed/backend/prometheus.yml`

**Change:**
```yaml
# Remove NATS job
- job_name: "nats"
  static_configs:
    - targets: ["nats:8222"]

# Add Redpanda job (if metrics needed)
- job_name: "redpanda"
  static_configs:
    - targets: ["redpanda:9644"]
      labels:
        service: "kafka"
```

#### Step 2.5: Remove empty gcp-distributed/ directory
```bash
# Should be empty now, remove it
rmdir deployments/gcp-distributed/backend
rmdir deployments/gcp-distributed
```

### Phase 3: Create Template Files for GCP WS Server Migration

#### Step 3.1: Create ws-server docker-compose.yml template
**File:** `deployments/gcp/distributed/ws-server/docker-compose.yml`

**Template:**
```yaml
# WebSocket Server - Kafka/Redpanda Setup
# Instance: sukko-go (dedicated e2-standard-4)
# Connects to Kafka on backend instance via internal network
#
# DEPLOYMENT:
#   1. Set BACKEND_INTERNAL_IP environment variable
#   2. Run: export BACKEND_INTERNAL_IP=10.128.0.2 && docker-compose up -d

services:
  ws-go:
    build:
      context: ../../../ws
      dockerfile: Dockerfile
    container_name: sukko-go
    ports:
      - "0.0.0.0:3004:3002"
    env_file:
      - .env.production
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 14848M # 14.5 GB
    ulimits:
      nofile:
        soft: 200000
        hard: 200000

  promtail:
    image: grafana/promtail:3.3.2
    container_name: sukko-promtail
    volumes:
      - ./promtail-config.yml:/etc/promtail/promtail-config.yml
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock
    command: -config.file=/etc/promtail/promtail-config.yml
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
```

#### Step 3.2: Create ws-server .env.production template
**File:** `deployments/gcp/distributed/ws-server/.env.production`

**Template:**
```bash
# WebSocket Server Production Configuration (Kafka/Redpanda)
# Instance: sukko-go (4 vCPU, 16GB RAM)
# Configuration: 12K connections @ 1 core

ENVIRONMENT=production

# SERVER
WS_ADDR=:3002

# KAFKA CONNECTION (will be substituted by deployment script)
KAFKA_BROKERS=${BACKEND_INTERNAL_IP}:9092

# RESOURCE LIMITS (e2-standard-4 - 12K connections @ 1 core)
WS_CPU_LIMIT=1
WS_MEMORY_LIMIT=15569256448  # 14.5 GB

# CONNECTIONS
WS_MAX_CONNECTIONS=12000

# WORKER POOL
WS_WORKER_POOL_SIZE=192
WS_WORKER_QUEUE_SIZE=19200

# RATE LIMITING
WS_MAX_KAFKA_RATE=25
WS_MAX_BROADCAST_RATE=25

# GOROUTINE LIMIT
WS_MAX_GOROUTINES=30000

# SAFETY THRESHOLDS
WS_CPU_REJECT_THRESHOLD=75.0
WS_CPU_PAUSE_THRESHOLD=80.0

# KAFKA CONSUMER
KAFKA_GROUP_ID=ws-server-production
KAFKA_TOPICS=sukko.trades,sukko.liquidity,sukko.balances,sukko.metadata,sukko.social,sukko.community,sukko.creation,sukko.analytics

# MONITORING
METRICS_INTERVAL=15s

# LOGGING
LOG_LEVEL=info
LOG_FORMAT=json
```

#### Step 3.3: Copy promtail config
```bash
# Copy from old location
cp deployments/old/gcp-distributed-nats/ws-server/promtail-config.yml \
   deployments/gcp/distributed/ws-server/
```

### Phase 4: Create README Documentation

#### Step 4.1: Create main GCP README
**File:** `deployments/gcp/README.md`

```markdown
# GCP Kafka/Redpanda Deployments

## Overview

This directory contains Kafka-based GCP production deployments that have replaced the previous NATS-based architecture.

## Architectures

### Distributed (2 instances)

**Backend Instance (sukko-backend - e2-small)**
- Redpanda (Kafka-compatible broker)
- Publisher (event generator)
- Prometheus, Grafana, Loki, Promtail (observability)

**WS Server Instance (sukko-go - e2-standard-4)**
- WebSocket server (Kafka consumer)
- Promtail (log shipping to backend Loki)

**Benefits:**
- Isolated resources for WS server
- Full capacity: 12K connections
- Scalable message broker

### Single (1 instance) - TO BE IMPLEMENTED

All services on one e2-small instance (similar to local dev).

## Migration from NATS

Old NATS deployments archived in `deployments/old/`.

Key changes:
- NATS → Kafka/Redpanda message broker
- 8 topics with 12 partitions each
- Consumer groups for scaling
- Idempotent producers

## Deployment

See respective README files in subdirectories.
```

#### Step 4.2: Create distributed/README.md
**File:** `deployments/gcp/distributed/README.md`

```markdown
# Distributed GCP Deployment (Kafka)

## Architecture

**Two GCP instances:**
1. Backend (sukko-backend - e2-small): Kafka, Publisher, Monitoring
2. WS Server (sukko-go - e2-standard-4): WebSocket server

## Prerequisites

1. Two GCP e2 instances created
2. Internal network connectivity between instances
3. Docker and docker-compose installed
4. Firewall rules configured

## Deployment Steps

### 1. Deploy Backend

SSH into sukko-backend:

```bash
# Set variables
export EXTERNAL_IP=$(curl -s ifconfig.me)

# Deploy
cd /path/to/sukko/deployments/gcp/distributed/backend
docker-compose up -d

# Create Kafka topics
docker exec redpanda rpk topic create \\
  sukko.trades sukko.liquidity sukko.balances sukko.metadata \\
  sukko.social sukko.community sukko.creation sukko.analytics \\
  --partitions 12 --replicas 1

# Verify
docker ps
docker exec redpanda rpk topic list
```

### 2. Get Backend Internal IP

```bash
# On backend instance
hostname -I | awk '{print $1}'
# Example: 10.128.0.2
```

### 3. Deploy WS Server

SSH into sukko-go:

```bash
# Set backend internal IP
export BACKEND_INTERNAL_IP=10.128.0.2

# Deploy
cd /path/to/sukko/deployments/gcp/distributed/ws-server
docker-compose up -d

# Verify
docker ps
curl localhost:3004/health
```

### 4. Access Services

- WS Server: `http://<WS_EXTERNAL_IP>:3004`
- Grafana: `http://<BACKEND_EXTERNAL_IP>:3010`
- Redpanda Console: `http://<BACKEND_EXTERNAL_IP>:8080`
- Prometheus: `http://<BACKEND_EXTERNAL_IP>:9091` (localhost only)

## Monitoring

Access Grafana at port 3010 (admin/admin).

Pre-configured dashboards:
- WebSocket Performance
- System Logs

## Scaling

To scale WS server instances:
1. Create additional e2-standard-4 instances
2. Deploy ws-server compose on each
3. Each joins same Kafka consumer group
4. Load balancer distributes WebSocket connections
```

#### Step 4.3: Update local/README.md header
Add note about GCP deployments:

```markdown
# Local Development Deployment

Kafka/Redpanda-based local development environment.

For GCP production deployments, see `../gcp/`.
```

### Phase 5: Cleanup and Verification

#### Step 5.1: Remove setup.log if exists
```bash
rm -f deployments/local/setup.log
```

#### Step 5.2: Verify directory structure
```bash
tree -L 3 deployments/
```

Expected output:
```
deployments/
├── old/
│   ├── gcp-distributed-nats/
│   │   └── ws-server/
│   ├── gcp-single-nats/
│   │   └── docker-compose.yml
│   └── local-configs-nats/
│       ├── loki-config.yml
│       ├── prometheus.yml
│       └── promtail-config.yml
├── local/
│   ├── docker-compose.yml
│   ├── docker-compose.redpanda.yml
│   ├── .env.local
│   ├── .env.local.example
│   ├── loki-config.yml
│   ├── prometheus.yml
│   ├── promtail-config.yml
│   ├── grafana/
│   ├── README.md
│   └── DEPLOYMENT_SUCCESS.md
└── gcp/
    ├── README.md
    ├── distributed/
    │   ├── README.md
    │   ├── backend/
    │   └── ws-server/
    └── single/
        └── README.md
```

---

## File Inventory

### Files to Archive (Move to old/)

**gcp-distributed/ws-server/** (3 files):
- `.env.production` → `old/gcp-distributed-nats/ws-server/`
- `docker-compose.yml` → `old/gcp-distributed-nats/ws-server/`
- `promtail-config.yml` → `old/gcp-distributed-nats/ws-server/`

**gcp-single/** (1 file):
- `docker-compose.yml` → `old/gcp-single-nats/`

**local/configs/** (3 files):
- `loki-config.yml` → `old/local-configs-nats/`
- `prometheus.yml` → `old/local-configs-nats/`
- `promtail-config.yml` → `old/local-configs-nats/`

### Files to Move (to gcp/)

**gcp-distributed/backend/** (2 files) → `gcp/distributed/backend/`:
- `docker-compose.yml` (already Kafka)
- `prometheus.yml` (needs NATS removal)

### Files to Copy (from local/)

**To gcp/distributed/backend/**:
- `loki-config.yml`
- `promtail-config.yml`
- `grafana/` (entire directory)

### Files to Create (new)

**gcp/distributed/ws-server/**:
- `docker-compose.yml` (Kafka version)
- `.env.production` (Kafka version)
- `promtail-config.yml` (copy from old/)

**gcp/single/**:
- `docker-compose.yml` (overlay for local compose)
- `README.md`

**Documentation**:
- `gcp/README.md`
- `gcp/distributed/README.md`

---

## Risk Assessment

### Low Risk
- Archiving old NATS deployments (not used in production)
- Moving local/configs/ (not referenced by docker-compose.yml)
- Creating new gcp/ structure (no impact on existing)

### Medium Risk
- Moving gcp-distributed/backend/ (if deployed on GCP)
  - **Mitigation**: Document new path, update deployment scripts

### No Risk
- local/ stays mostly unchanged (only configs/ removed)
- All current docker-compose.yml files remain functional

---

## Rollback Plan

If issues arise:

### Rollback Step 1: Restore from old/
```bash
# Restore GCP distributed ws-server
cp -r deployments/old/gcp-distributed-nats/ws-server \
      deployments/gcp-distributed/

# Restore GCP single
cp -r deployments/old/gcp-single-nats \
      deployments/gcp-single

# Restore local configs
cp -r deployments/old/local-configs-nats \
      deployments/local/configs
```

### Rollback Step 2: Remove new gcp/ structure
```bash
rm -rf deployments/gcp
```

### Rollback Step 3: Restore gcp-distributed structure
```bash
mkdir -p deployments/gcp-distributed/backend
# Move files back if needed
```

---

## Post-Reorganization Tasks

### Immediate (Phase 5)
1. ✅ Verify all docker-compose files can be parsed
2. ✅ Test local deployment still works
3. ✅ Update any deployment scripts referencing old paths

### Short-term (Next Sprint)
1. ⏳ Implement gcp/single/ overlay file
2. ⏳ Complete ws-server Kafka migration for GCP
3. ⏳ Deploy and test gcp/distributed/ on GCP instances
4. ⏳ Update CI/CD pipelines if they reference old paths

### Long-term
1. ⏳ Create deployment automation (Terraform/Ansible)
2. ⏳ Multi-region GCP deployments
3. ⏳ Auto-scaling WS server instances

---

## Success Criteria

- ✅ All NATS-based deployments archived in old/
- ✅ local/ deployment cleaned up (no configs/ directory)
- ✅ gcp/ structure created with Kafka-based configs
- ✅ Backend fully migrated with all necessary configs
- ✅ WS server templates created for Kafka migration
- ✅ Documentation complete for all environments
- ✅ Directory structure matches taskfiles/ pattern
- ✅ No broken references in docker-compose files
- ✅ Local deployment remains functional

---

## Related Documentation

- [Taskfile Reorganization Plan](./TASKFILE_REORGANIZATION_PLAN.md) - Similar pattern
- [Capacity Planning](./CAPACITY_PLANNING.md) - Resource allocations
- [Deployment Success](../deployments/local/DEPLOYMENT_SUCCESS.md) - Local setup
