# Taskfile & Scripts Reorganization Plan

**Status**: Ready for Implementation  
**Created**: November 5, 2025  
**Timeline**: 2-3 hours for local implementation

---

## 🎯 Objectives

1. **Composable Tasks**: Individual service tasks that compose into aggregate tasks
2. **Environment Separation**: Clear local vs GCP separation  
3. **Consistent Interface**: Same task names, different implementations
4. **Clean Structure**: Organized by purpose, archived old files

---

## 📁 New Directory Structure

```
sukko/
├── Taskfile.yml                    # Main orchestrator (updated)
├── taskfiles/
│   ├── old/                        # Archive old NATS-based taskfiles
│   │   ├── build.yml
│   │   ├── deploy-gcp.yml
│   │   ├── dev.yml
│   │   ├── docker.yml
│   │   ├── isolated-setup.yml
│   │   ├── monitor.yml
│   │   ├── publisher.yml
│   │   ├── test.yml
│   │   └── utils.yml
│   │
│   ├── local/                      # Local development (Kafka/Redpanda)
│   │   ├── services.yml            # Individual service control
│   │   ├── deployment.yml          # Full deployment workflows
│   │   ├── health.yml              # Health checks (composed)
│   │   ├── load-test.yml           # Capacity testing
│   │   └── stats.yml               # Stats & monitoring
│   │
│   ├── gcp/                        # GCP production (Kafka/Redpanda)
│   │   ├── services.yml            # GCP service control
│   │   ├── deployment.yml          # GCP deployment workflows
│   │   ├── health.yml              # GCP health checks
│   │   └── load-test.yml           # GCP capacity testing
│   │
│   └── shared/                     # Shared across environments
│       ├── build.yml               # Build Go + TypeScript
│       └── utils.yml               # Common utilities
│
└── scripts/
    ├── old/                        # Archive old NATS-based scripts
    │   ├── collect-metrics.sh
    │   ├── start-dev.sh
    │   └── stop-dev.sh
    │
    ├── local/                      # Local deployment scripts
    │   ├── setup-redpanda-topics.sh
    │   └── setup-local.sh
    │
    └── gcp/                        # GCP deployment scripts (future)
        └── (GCP-specific scripts)
```

---

## 🔧 Key Design Principles

### 1. Composability: Small → Aggregate → Workflow

```
Individual Tasks         → Aggregate Tasks      → Workflow Tasks
start:redpanda          → start:all            → deploy:setup
start:publisher                                    ↓
start:ws                                       (calls multiple aggregates)
start:monitoring
```

### 2. Consistent Interface Across Environments

```bash
# Local
task local:start:all
task local:health:all
task local:test:sustained

# GCP (same interface, different implementation)
task gcp:start:all
task gcp:health:all
task gcp:test:sustained
```

### 3. Task Dependencies

```yaml
start:ws:
  deps: [start:redpanda]  # WS server needs Redpanda first

start:grafana:
  deps: [start:prometheus, start:loki]  # Grafana needs both
```

---

## 📋 Task Specifications

### Service Management (`local/services.yml`)

**DELETE Tasks** - For config changes
- `delete:redpanda` - Delete with volumes
- `delete:publisher` - Delete container  
- `delete:ws` - Delete WS server
- `delete:monitoring` - Delete all monitoring
- `delete:all` - Nuclear option (all + volumes)

**START Tasks** - Launch services
- `start:redpanda` - Start Redpanda
- `start:publisher` - Start Publisher (depends on redpanda)
- `start:ws` - Start WS server (depends on redpanda)
- `start:monitoring` - Start Prometheus, Grafana, Loki
- `start:all` - Start everything in order

**RESTART Tasks** - For code/config changes
- `restart:redpanda` - Delete + Start (config changes)
- `restart:publisher` - Quick restart (code changes)
- `restart:ws` - Quick restart
- `restart:all` - Restart everything

**REBUILD Tasks** - For code changes
- `rebuild:publisher` - Rebuild Docker image + restart
- `rebuild:ws` - Rebuild Docker image + restart
- `rebuild:all` - Rebuild all buildable services

**LOGS Tasks**
- `logs:redpanda`, `logs:publisher`, `logs:ws`, `logs:all`

### Health Checks (`local/health.yml`)

**Individual Checks**
- `health:redpanda` - Check cluster health via rpk
- `health:publisher` - Check /health endpoint
- `health:ws` - Check /health endpoint
- `health:prometheus` - Check /-/healthy
- `health:grafana` - Check /api/health

**Aggregate Check**
- `health:all` - Runs all individual checks, shows summary
- `health:detailed` - Full JSON responses

### Deployment Workflows (`local/deployment.yml`)

- `setup` - Complete from scratch (check → start → topics → health)
- `reset` - Delete all + setup (for config changes)
- `rebuild-code` - Rebuild services + health check

### Load Testing (`local/load-test.yml`)

- `sustained` - Indefinite test at 1000 events/sec
- `stop` - Stop current test
- `burst` - 5min at 2000 events/sec
- `ramp` - Gradual increase 100→2000 events/sec

### Stats & Monitoring (`local/stats.yml`)

- `publisher` - Get publisher stats JSON
- `ws` - Get WS server stats
- `ips` - Show all container IP addresses
- `topics` - List Kafka topics
- `consume` - Consume messages from topic
- `monitor:live` - Live dashboard (watch mode)

---

## 🚀 Implementation Steps

### Step 1: Archive (30 min)
```bash
# Move old taskfiles
mkdir -p taskfiles/old
mv taskfiles/{build,deploy-gcp,dev,docker,isolated-setup,monitor,publisher,test,utils}.yml taskfiles/old/

# Move old scripts
mkdir -p scripts/old
mv scripts/{collect-metrics,start-dev,stop-dev}.sh scripts/old/

# Organize new scripts
mkdir -p scripts/local scripts/gcp
mv scripts/setup-redpanda-topics.sh scripts/local/
mv deployments/local/setup-local.sh scripts/local/
```

### Step 2: Create Structure (10 min)
```bash
mkdir -p taskfiles/{local,gcp,shared}
touch taskfiles/local/{services,deployment,health,load-test,stats}.yml
touch taskfiles/gcp/{services,deployment,health}.yml
touch taskfiles/shared/{build,utils}.yml
```

### Step 3: Implement Local Tasks (1-2 hours)
See full task specifications in `/tmp/taskfile_reorganization_plan.md`

### Step 4: Test (30 min)
```bash
task local:deploy:setup
task local:health:all
task local:test:sustained
task local:stats:ips
```

---

## 🎯 Usage Examples

**First Time Setup**
```bash
task local:deploy:setup
# → Complete setup + health check + shows URLs
```

**Config Changed** (e.g., memory limit)
```bash
# Edit docker-compose.yml
task local:deploy:reset
# → Delete all + redeploy
```

**Code Changed** (e.g., WS server)
```bash
# Edit ws/server.go
task local:rebuild:ws
# → Rebuild + restart + health check
```

**Run Load Test**
```bash
task local:test:sustained
task local:stats:publisher
task local:test:stop
```

**Individual Service Control**
```bash
task local:restart:redpanda
task local:logs:ws
task local:health:publisher
```

**Get Container IPs**
```bash
task local:stats:ips
```

---

## ✅ Success Criteria

- ✅ Single command deployment: `task local:deploy:setup`
- ✅ Individual service control: `task local:start:ws`
- ✅ Composed health checks: `task local:health:all`
- ✅ Clean reset: `task local:deploy:reset`
- ✅ Sustained testing: `task local:test:sustained`
- ✅ Stats visibility: `task local:stats:publisher`, `task local:stats:ips`
- ✅ Same interface for GCP: `task gcp:*` (future)
- ✅ All old files archived, not deleted
- ✅ Scripts organized by environment

---

## 📊 Command Reference

### Quick Commands
```bash
task up              # Quick start
task down            # Quick stop
task health          # Quick health check
task test            # Quick load test
```

### Full Commands
```bash
# Deployment
task local:deploy:setup
task local:deploy:reset
task local:deploy:rebuild-code

# Services
task local:start:all
task local:delete:all
task local:restart:all
task local:rebuild:all

# Individual Services
task local:start:redpanda
task local:restart:publisher
task local:rebuild:ws
task local:logs:ws

# Health
task local:health:all
task local:health:redpanda
task local:health:detailed

# Testing
task local:test:sustained
task local:test:burst
task local:test:ramp
task local:test:stop

# Stats
task local:stats:publisher
task local:stats:ws
task local:stats:ips
task local:stats:topics
task local:stats:consume TOPIC=sukko.trades NUM=10
task local:stats:monitor:live
```

---

## 🔄 Migration Timeline

| Phase | Duration | Tasks |
|-------|----------|-------|
| 1. Archive & Structure | 30 min | Move old files, create dirs |
| 2. Core Services | 1 hour | Implement services.yml, deployment.yml |
| 3. Health & Stats | 30 min | Implement health.yml, stats.yml |
| 4. Load Testing | 30 min | Implement load-test.yml |
| 5. Testing | 30 min | End-to-end testing |
| **Total** | **2-3 hours** | **Full local implementation** |

---

## 📝 Next Steps

1. Review this plan
2. Approve implementation approach
3. Execute Step 1 (Archive)
4. Execute Step 2 (Structure)
5. Implement taskfiles one by one
6. Test each taskfile as implemented
7. Update main Taskfile.yml
8. Test full workflow
9. Document in README

---

**Note**: Full task specifications and code are available in:
- `/tmp/taskfile_reorganization_plan.md` (comprehensive version)
- This document (high-level overview)
