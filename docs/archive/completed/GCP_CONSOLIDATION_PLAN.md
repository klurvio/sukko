# GCP Configuration Consolidation Plan

## Executive Summary

This document outlines the plan to consolidate GCP deployment configurations from the legacy NATS-based setup (`deployments/old/gcp-distributed-nats/`) to the new Kafka-based versioned structure (`deployments/v1/gcp/`).

**Status**: Configuration files partially migrated, taskfiles created, ready for validation.

**Key Objectives**:
1. Preserve all production-validated configuration values from old deployment
2. Apply shared configuration pattern (base.env + overrides.env) to GCP
3. Create comprehensive taskfile structure for GCP operations
4. Ensure backward compatibility and zero downtime migration path

---

## 1. Current State Analysis

### 1.1 Old Structure (NATS-based)

**Location**: `/Volumes/Dev/Codev/Toniq/odin-ws/deployments/old/gcp-distributed-nats/`

**Components**:
- `ws-server/` - WebSocket server (e2-standard-4, NATS connection)
  - `docker-compose.yml` - Single monolithic env file pattern
  - `.env.production` - All configuration in one file
  - Resource limits: 1.0 CPU, 14.5 GB memory, 12K connections
  
**Key Configuration Values (NATS)**:
```bash
# Instance
MACHINE_TYPE=e2-standard-4  # 4 vCPU, 16GB RAM

# Resource Limits
WS_CPU_LIMIT=1                    # 1 core (GOMAXPROCS=1)
WS_MEMORY_LIMIT=15569256448       # 14.5 GB (90% of 16GB)
WS_MAX_CONNECTIONS=12000          # Production capacity

# Worker Pool
WS_WORKER_POOL_SIZE=192           # Production-validated
WS_WORKER_QUEUE_SIZE=19200        # 100x worker pool size

# Rate Limiting
WS_MAX_NATS_RATE=25               # Based on 40K tx/day peak
WS_MAX_BROADCAST_RATE=25

# Goroutine Limit
WS_MAX_GOROUTINES=30000           # ((12K × 2) + 192 + 13) × 1.2

# Connection
NATS_URL=nats://${BACKEND_INTERNAL_IP}:4222

# Promtail Resources
promtail:
  cpus: 1.0
  memory: 512M
```

**Old Taskfiles**: `/Volumes/Dev/Codev/Toniq/odin-ws/taskfiles/old/`
- `deploy-gcp.yml` - Infrastructure and deployment tasks
- `test.yml` - Load testing tasks with specific parameters
  - capacity tests: 2000 connections (old local limit)
  - stress tests: 3000-5000 connections
  - throughput tests: Message-only testing
  - connection tests: Connection-only testing

### 1.2 New Structure (Kafka-based, v1)

**Location**: `/Volumes/Dev/Codev/Toniq/odin-ws/deployments/v1/gcp/`

**Architecture**: Distributed (2 instances)
- `backend/` - Redpanda/Kafka, Publisher, Monitoring (e2-small)
- `ws-server/` - WebSocket server (e2-standard-4)

**Configuration Pattern**: Shared + Overrides
- `../../shared/base.env` - Common defaults
- `../../shared/kafka-topics.env` - Topic configuration
- `../../shared/ports.env` - Port mappings
- `overrides.env` - GCP production-specific values
- `.env.production` - Secrets (gitignored)

**Current Configuration Values (Kafka)**:
```bash
# From overrides.env
ENVIRONMENT=production
WS_ADDR=:3002

# Kafka Connection (migrated from NATS)
KAFKA_BROKERS=${BACKEND_INTERNAL_IP}:9092
KAFKA_GROUP_ID=ws-server-production

# Resource Limits (PRESERVED from old)
WS_CPU_LIMIT=1
WS_MEMORY_LIMIT=15569256448       # 14.5 GB
WS_MAX_CONNECTIONS=12000

# Worker Pool (PRESERVED from old)
WS_WORKER_POOL_SIZE=256           # Updated from 192
WS_WORKER_QUEUE_SIZE=25600        # Updated from 19200

# Rate Limiting (PRESERVED from old)
WS_MAX_KAFKA_RATE=25              # Migrated from WS_MAX_NATS_RATE
WS_MAX_BROADCAST_RATE=25

# Goroutine Limit (PRESERVED from old)
WS_MAX_GOROUTINES=30000
```

**New Taskfiles**: `/Volumes/Dev/Codev/Toniq/odin-ws/taskfiles/v1/gcp/`
- `Taskfile.yml` - Main orchestrator (✅ CREATED)
- `services.yml` - Service start/stop/restart (✅ CREATED)
- `deployment.yml` - Infrastructure and deployment (✅ CREATED)
- `health.yml` - Health checks and logs (✅ CREATED)
- `stats.yml` - Service URLs and info (✅ CREATED)
- `load-test.yml` - Load testing (✅ CREATED)

---

## 2. Configuration Comparison

### 2.1 Values That Changed (NATS → Kafka)

| Configuration | Old (NATS) | New (Kafka) | Notes |
|---------------|------------|-------------|-------|
| Message Broker | `NATS_URL` | `KAFKA_BROKERS` | Protocol change |
| Group ID | N/A (NATS) | `KAFKA_GROUP_ID` | Kafka consumer groups |
| Rate Limit Var | `WS_MAX_NATS_RATE` | `WS_MAX_KAFKA_RATE` | Same value (25) |
| Worker Pool Size | 192 | 256 | Upgraded for Kafka |
| Worker Queue Size | 19,200 | 25,600 | Upgraded for Kafka |

### 2.2 Values Preserved (Production-Validated)

| Configuration | Value | Rationale |
|---------------|-------|-----------|
| `WS_CPU_LIMIT` | 1.0 | Single-threaded, maximized RAM strategy |
| `WS_MEMORY_LIMIT` | 14.5 GB | 90% of 16GB instance |
| `WS_MAX_CONNECTIONS` | 12,000 | Production capacity validated |
| `WS_MAX_KAFKA_RATE` | 25 | Based on 40K tx/day peak (was NATS_RATE) |
| `WS_MAX_BROADCAST_RATE` | 25 | Same as message rate |
| `WS_MAX_GOROUTINES` | 30,000 | Formula validated for 12K connections |
| Promtail CPU | 1.0 | Log shipping performance |
| Promtail Memory | 512M | Increased from 256M for performance |

### 2.3 Architecture Changes

**Old (NATS)**:
- Single backend instance with NATS
- WS server connects via NATS

**New (Kafka)**:
- Separate backend instance (e2-small) with Redpanda/Kafka
- WS server connects via Kafka protocol
- Internal network communication (10.128.0.0/20)
- Monitoring centralized on backend

---

## 3. What's Already Consolidated

### ✅ Completed

1. **Shared Configuration Structure**
   - `deployments/v1/shared/base.env` - Common defaults
   - `deployments/v1/shared/kafka-topics.env` - Topic definitions
   - `deployments/v1/shared/ports.env` - Port mappings
   - `deployments/v1/shared/resource-formulas.yml` - Calculation formulas

2. **GCP Deployment Files**
   - `deployments/v1/gcp/distributed/ws-server/docker-compose.yml`
   - `deployments/v1/gcp/distributed/ws-server/overrides.env`
   - `deployments/v1/gcp/distributed/ws-server/.env.production` (template)
   - `deployments/v1/gcp/distributed/backend/docker-compose.yml`
   - All Grafana, Prometheus, Loki configurations

3. **GCP Taskfiles**
   - `taskfiles/v1/gcp/Taskfile.yml` - Main orchestrator
   - `taskfiles/v1/gcp/services.yml` - Service management
   - `taskfiles/v1/gcp/deployment.yml` - Infrastructure + deployment
   - `taskfiles/v1/gcp/health.yml` - Health checks + logs
   - `taskfiles/v1/gcp/stats.yml` - URLs + info
   - `taskfiles/v1/gcp/load-test.yml` - Load testing

4. **Production Values Migrated**
   - All resource limits preserved from old deployment
   - Production-validated worker pool sizes (upgraded for Kafka)
   - Rate limiting values preserved (25 msg/sec)
   - Goroutine limits preserved (30,000)
   - Promtail resource allocations preserved

5. **Documentation**
   - `deployments/v1/gcp/distributed/README.md` - Comprehensive deployment guide
   - `deployments/v1/shared/README.md` - Configuration documentation

---

## 4. What Needs Validation

### 🔍 Requires Testing

1. **Load Test Parameters**
   - Old: Tested with 2000 connections (local dev limit)
   - New: Testing with 12,000 connections (GCP production limit)
   - **Action**: Run `task gcp:load-test:capacity` to validate 12K connections
   - **Expected**: All 12K connections succeed, stable performance

2. **Worker Pool Sizing**
   - Old: 192 workers (NATS-validated)
   - New: 256 workers (Kafka-optimized)
   - **Action**: Monitor worker queue metrics during capacity test
   - **Expected**: Queue depth < 50%, utilization 60-80%

3. **Kafka vs NATS Performance**
   - Old: NATS JetStream (single instance)
   - New: Redpanda/Kafka (partitioned, scalable)
   - **Action**: Compare message latency and throughput
   - **Expected**: Similar or better latency, higher throughput potential

4. **Grafana Dashboard Compatibility**
   - Changed metrics from NATS to Kafka
   - **Action**: Verify all dashboard panels show data
   - **Expected**: All panels functional (NATS panels removed)

---

## 5. Migration Strategy

### 5.1 Phase 1: Validation (Current Phase)

**Objective**: Validate new configuration matches old performance

**Steps**:
1. ✅ Create GCP taskfile structure
2. ✅ Document configuration consolidation
3. ⏳ **Next**: Deploy to GCP test instance
4. ⏳ **Next**: Run capacity test (12K connections)
5. ⏳ **Next**: Compare metrics with old NATS deployment

**Commands**:
```bash
# 1. Review configuration
cat deployments/v1/gcp/distributed/ws-server/overrides.env

# 2. Review old configuration for comparison
cat deployments/old/gcp-distributed-nats/ws-server/.env.production

# 3. Deploy backend
task gcp:deployment:create:backend
task gcp:deployment:firewall:backend
task gcp:deployment:setup:backend

# 4. Deploy WS server
task gcp:deployment:create:ws
task gcp:deployment:firewall:ws
task gcp:deployment:reserve-ip:ws
task gcp:deployment:setup:ws

# 5. Verify health
task gcp:health:all
task gcp:stats:urls

# 6. Run capacity test
task gcp:load-test:capacity:short  # 5 min test
task gcp:load-test:capacity        # 30 min test
```

### 5.2 Phase 2: Production Cutover

**Objective**: Replace old NATS deployment with new Kafka deployment

**Prerequisites**:
- ✅ Phase 1 validation complete
- ✅ Capacity test shows 12K connections stable
- ✅ Monitoring dashboards functional
- ✅ No performance regressions

**Steps**:
1. Deploy new instances (backend + ws-server)
2. Run parallel testing (old NATS + new Kafka)
3. Compare metrics side-by-side
4. Route 10% traffic to new deployment
5. Gradually increase to 100%
6. Decommission old instances

### 5.3 Phase 3: Optimization

**Objective**: Fine-tune based on production data

**Potential Optimizations**:
- Adjust worker pool size based on actual load
- Tune Kafka consumer settings
- Optimize Promtail log shipping
- Scale horizontally if needed (add more WS instances)

---

## 6. Testing Checklist

### 6.1 Configuration Validation

- [x] All old configuration values documented
- [x] Resource limits preserved (CPU, memory, connections)
- [x] Worker pool sizes validated (upgraded for Kafka)
- [x] Rate limiting values preserved
- [x] Goroutine limits preserved
- [x] Promtail resources preserved

### 6.2 Functionality Testing

- [ ] Backend services start successfully
  - [ ] Redpanda healthy
  - [ ] Publisher generating events
  - [ ] Prometheus scraping metrics
  - [ ] Grafana showing dashboards
  - [ ] Loki receiving logs

- [ ] WebSocket server starts successfully
  - [ ] Connects to Kafka
  - [ ] Health endpoint responds
  - [ ] Accepts WebSocket connections
  - [ ] Receives broadcast messages
  - [ ] Metrics exported to Prometheus

### 6.3 Load Testing

- [ ] Capacity test (12,000 connections)
  - [ ] All connections established
  - [ ] No rejections or errors
  - [ ] CPU usage ~30%
  - [ ] Memory usage ~46%
  - [ ] Message delivery latency < 100ms

- [ ] Stress test (18,000 connections)
  - [ ] Server rejects after 12K
  - [ ] Server remains stable
  - [ ] Active connections stay at 12K
  - [ ] No crashes or restarts

- [ ] Sustained test (30 minutes)
  - [ ] Connections remain stable
  - [ ] No slow client disconnects
  - [ ] No memory leaks
  - [ ] Consistent performance

### 6.4 Monitoring Validation

- [ ] Grafana dashboards functional
  - [ ] Active Connections panel
  - [ ] Message Rate panel
  - [ ] CPU/Memory panels
  - [ ] Worker Queue panels
  - [ ] Broadcast metrics panels

- [ ] Logs flowing to Loki
  - [ ] WS server logs visible
  - [ ] Backend service logs visible
  - [ ] Log filtering works
  - [ ] No missing log entries

### 6.5 Operational Testing

- [ ] Service restart works
  - [ ] `task gcp:services:restart:backend`
  - [ ] `task gcp:services:restart:ws`
  - [ ] `task gcp:restart` (restart all)
  
- [ ] Code rebuilds work
  - [ ] `task gcp:deployment:rebuild:backend`
  - [ ] `task gcp:deployment:rebuild:ws`
  - [ ] `task gcp:deployment:rebuild:all`
  
- [ ] Health checks accurate
  - [ ] `task gcp:health:backend`
  - [ ] `task gcp:health:ws`
  - [ ] `task gcp:verify` (verify all)

---

## 7. Rollback Plan

If issues are discovered during migration:

### 7.1 Immediate Rollback

**Trigger**: Critical issues affecting service availability

**Steps**:
1. Route all traffic back to old NATS deployment
2. Stop new Kafka instances: `task gcp:down`
3. Investigate issues offline
4. Fix and re-test before retry

### 7.2 Configuration Rollback

**Trigger**: Performance regressions but service functional

**Steps**:
1. Revert worker pool sizes to old values (192/19200)
2. Adjust Kafka consumer settings
3. Re-run capacity tests
4. Compare metrics

---

## 8. Key Differences Summary

### 8.1 What Stayed the Same

- Instance types (e2-standard-4 for WS server)
- Resource limits (1 CPU, 14.5 GB memory)
- Max connections (12,000)
- Rate limiting (25 msg/sec)
- Goroutine limits (30,000)
- Promtail resources (1 CPU, 512MB)

### 8.2 What Changed

- Message broker: NATS → Kafka/Redpanda
- Worker pool: 192 → 256 (optimized for Kafka)
- Worker queue: 19,200 → 25,600 (scaled proportionally)
- Configuration structure: Monolithic → Shared + Overrides
- Deployment: Single backend → Distributed (backend + ws-server)
- Task organization: Flat → Modular (services, deployment, health, etc.)

### 8.3 What Was Added

- Backend instance (e2-small) for Redpanda/Monitoring
- Redpanda Console (web UI for Kafka)
- Centralized monitoring (Prometheus, Grafana, Loki)
- Shared configuration library
- Comprehensive taskfile structure
- Load testing at production scale (12K connections)

---

## 9. Next Steps

### Immediate Actions

1. **Review this plan** ✅
2. **Validate taskfile structure**
   ```bash
   task gcp:default  # Show available tasks
   ```

3. **Set GCP configuration**
   - Update `GCP_PROJECT` in taskfiles/v1/gcp/Taskfile.yml (default: `odin-ws-server`)
   - Update `GIT_BRANCH` if deploying from non-main branch (default: `main`)
   - Update instance names if needed (defaults: `odin-backend`, `odin-ws-go`)
   - Verify region/zone settings (defaults: `us-central1`, `us-central1-a`)

4. **Deploy to GCP**
   
   **Option A: Complete first-time deployment (single command)**
   ```bash
   task gcp:deploy
   # Calls gcp:deployment:setup which guides you through:
   # 1. Infrastructure setup (instances, networking, IP)
   # 2. Application setup (manual SSH steps with instructions)
   # 3. Health verification
   ```

   **Option B: Step-by-step deployment**
   ```bash
   # Step 1: Setup infrastructure
   task gcp:deployment:infrastructure
   
   # Step 2: Setup applications (manual SSH steps)
   task gcp:deployment:guided-setup
   
   # Step 3: Verify deployment
   task gcp:verify
   ```

   **Option C: Manual deployment (advanced)**
   ```bash
   # 1. Create instances
   task gcp:deployment:create:backend
   task gcp:deployment:create:ws
   
   # 2. Setup networking
   task gcp:deployment:firewall:backend
   task gcp:deployment:firewall:ws
   task gcp:deployment:reserve-ip:ws
   
   # 3. Deploy applications
   task gcp:deployment:setup:backend  # Shows manual SSH steps
   task gcp:deployment:setup:ws       # Shows manual SSH steps
   
   # 4. Verify
   task gcp:verify
   ```

5. **Daily operations**
   ```bash
   task gcp:up              # Start all services (quick start)
   task gcp:down            # Stop all services
   task gcp:restart         # Restart all services
   task gcp:status          # Check deployment status
   task gcp:verify          # Verify health
   ```

6. **Run validation tests**
   ```bash
   task gcp:load-test:capacity:short  # 12K connections, 5 min
   task gcp:load-test:capacity        # 12K connections, 30 min
   ```

### Future Work

1. **Horizontal Scaling**
   - Add load balancer support
   - Multi-instance WS server deployment
   - Kafka partition distribution

2. **Auto-scaling**
   - GCP managed instance groups
   - Auto-scale based on CPU/connections
   - Pre-warming strategies

3. **Disaster Recovery**
   - Backup strategies for Kafka data
   - Multi-region deployment
   - Failover procedures

---

## 10. Configuration Reference

### 10.1 Complete Configuration Mapping

| Variable | Old (NATS) | New (Kafka) | Location |
|----------|------------|-------------|----------|
| `ENVIRONMENT` | production | production | overrides.env |
| `WS_ADDR` | :3002 | :3002 | overrides.env |
| `NATS_URL` | nats://..:4222 | ❌ Removed | - |
| `KAFKA_BROKERS` | ❌ N/A | ...:9092 | overrides.env |
| `KAFKA_GROUP_ID` | ❌ N/A | ws-server-production | overrides.env |
| `WS_CPU_LIMIT` | 1 | 1 | overrides.env |
| `WS_MEMORY_LIMIT` | 15569256448 | 15569256448 | overrides.env |
| `WS_MAX_CONNECTIONS` | 12000 | 12000 | overrides.env |
| `WS_WORKER_POOL_SIZE` | 192 | 256 | overrides.env |
| `WS_WORKER_QUEUE_SIZE` | 19200 | 25600 | overrides.env |
| `WS_MAX_NATS_RATE` | 25 | ❌ Removed | - |
| `WS_MAX_KAFKA_RATE` | ❌ N/A | 25 | overrides.env |
| `WS_MAX_BROADCAST_RATE` | 25 | 25 | overrides.env |
| `WS_MAX_GOROUTINES` | 30000 | 30000 | overrides.env |
| `LOG_LEVEL` | info | info | base.env |
| `LOG_FORMAT` | json | json | base.env |
| `METRICS_INTERVAL` | 15s | 15s | base.env |

### 10.2 Load Test Parameters

| Test Type | Old (Local) | New (GCP) | Task Command |
|-----------|-------------|-----------|--------------|
| Capacity | 2,000 | 12,000 | `task gcp:load-test:capacity` |
| Stress | 3,000 | 18,000 | `task gcp:load-test:stress` |
| Heavy Stress | 5,000 | 24,000 | `task gcp:load-test:stress:heavy` |
| Ramp Rate | 100/sec | 100/sec | (same) |
| Duration | 30 min | 30 min | (same) |

---

## Appendix A: File Structure

### Old Structure
```
deployments/old/gcp-distributed-nats/
└── ws-server/
    ├── docker-compose.yml
    ├── .env.production
    └── promtail-config.yml

taskfiles/old/
├── deploy-gcp.yml
└── test.yml
```

### New Structure
```
deployments/v1/
├── shared/
│   ├── base.env
│   ├── kafka-topics.env
│   ├── ports.env
│   └── resource-formulas.yml
└── gcp/distributed/
    ├── backend/
    │   ├── docker-compose.yml
    │   ├── prometheus.yml
    │   ├── loki-config.yml
    │   └── grafana/
    └── ws-server/
        ├── docker-compose.yml
        ├── overrides.env
        ├── .env.production (template)
        └── promtail-config.yml

taskfiles/v1/gcp/
├── Taskfile.yml
├── services.yml
├── deployment.yml
├── health.yml
├── stats.yml
└── load-test.yml
```

---

## Appendix B: Task Command Reference

### High-Level Workflows (Recommended for Daily Use)
```bash
# First-time deployment
task gcp:deploy                         # Complete guided deployment

# Daily operations
task gcp:up                             # Start all services (quick start)
task gcp:down                           # Stop all services
task gcp:restart                        # Restart all services
task gcp:status                         # Show deployment status
task gcp:verify                         # Verify health

# Deployment workflows
task gcp:deployment:setup               # Complete setup (infra + app)
task gcp:deployment:infrastructure      # Create infrastructure
task gcp:deployment:guided-setup        # Guided app setup
task gcp:deployment:quick-start         # Quick start (daily use)
task gcp:deployment:stop                # Stop all services
task gcp:deployment:reset               # Reset deployment
task gcp:deployment:rebuild-code        # Rebuild after code changes
task gcp:deployment:show-urls           # Show service URLs
```

### Service Management (Low-level Control)
```bash
# Start/Stop/Restart
task gcp:services:start:backend         # Start backend services
task gcp:services:start:ws              # Start WebSocket server
task gcp:services:start:all             # Start all services
task gcp:services:stop:backend          # Stop backend services
task gcp:services:stop:ws               # Stop WebSocket server
task gcp:services:stop:all              # Stop all services
task gcp:services:restart:backend       # Restart backend
task gcp:services:restart:ws            # Restart WebSocket server
task gcp:services:restart:all           # Restart all services

# Status & Logs
task gcp:services:ps                    # Show all containers (shortcut)
task gcp:services:ps:all                # Show all containers
task gcp:services:ps:backend            # Show backend containers
task gcp:services:ps:ws                 # Show WS containers
task gcp:services:logs:backend          # Tail backend logs (follow)
task gcp:services:logs:ws               # Tail WS logs (follow)
task gcp:services:logs:backend:tail     # Last 100 lines (backend)
task gcp:services:logs:ws:tail          # Last 100 lines (WS)
```

### Infrastructure & Code Rebuilds
```bash
# Infrastructure creation
task gcp:deployment:create:backend      # Create backend instance
task gcp:deployment:create:ws           # Create WS server instance
task gcp:deployment:firewall:backend    # Setup backend firewall
task gcp:deployment:firewall:ws         # Setup WS firewall
task gcp:deployment:reserve-ip:ws       # Reserve static IP

# Application setup (manual SSH steps)
task gcp:deployment:setup:backend       # Setup backend app
task gcp:deployment:setup:ws            # Setup WS app

# Code rebuilds (pull + build + restart)
task gcp:deployment:rebuild:backend     # Rebuild backend with latest code
task gcp:deployment:rebuild:ws          # Rebuild WS server with latest code
task gcp:deployment:rebuild:all         # Rebuild all services

# Legacy aliases (deprecated - use rebuild instead)
task gcp:deployment:update:backend      # → use rebuild:backend
task gcp:deployment:update:ws           # → use rebuild:ws
task gcp:deployment:update:all          # → use rebuild:all

# SSH access
task gcp:deployment:ssh:backend         # SSH to backend
task gcp:deployment:ssh:ws              # SSH to WS server

# Deploy from specific branch
GIT_BRANCH=working-12k task gcp:deployment:rebuild:backend
GIT_BRANCH=working-12k task gcp:deployment:rebuild:ws

# Override multiple variables
GCP_PROJECT=my-project GIT_BRANCH=feature-xyz task gcp:deployment:rebuild:ws
```

### Health & Monitoring
```bash
task gcp:health:backend                 # Check backend health
task gcp:health:ws                      # Check WS server health
task gcp:health:all                     # Check all health
```

### Stats & Info
```bash
task gcp:stats:urls                     # Show all service URLs
task gcp:stats:ip:backend               # Show backend IPs
task gcp:stats:ip:ws                    # Show WS server IPs
task gcp:stats:info                     # Show deployment info
```

### Load Testing
```bash
task gcp:load-test:capacity        # 12K connections, 30 min
task gcp:load-test:capacity:short  # 12K connections, 5 min
task gcp:load-test:stress          # 18K connections (1.5x)
task gcp:load-test:stress:heavy    # 24K connections (2x)
task gcp:load-test:custom          # Custom (use CONNECTIONS/RAMP_RATE/DURATION vars)
task gcp:load-test:stop            # Stop all load tests
```

---

**Document Status**: ✅ Complete  
**Last Updated**: 2025-11-06  
**Author**: Claude (Session Continuation)  
**Review Required**: Yes - Before GCP deployment
