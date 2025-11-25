# Redis Broadcast Bus: Quick Start Guide

**Purpose**: Fast-track guide for deploying Redis-based broadcast bus
**Audience**: Engineers deploying horizontal scaling
**Time**: 1-2 weeks to production
**Status**: ✅ **IMPLEMENTED** - Code complete, ready for deployment
**Branch**: `feature/redis-broadcast-bus`

---

## TL;DR

**What**: Redis Pub/Sub BroadcastBus for horizontal scaling (direct implementation, no abstraction layer)
**Why**: Enable multi-instance deployment (18K → 36K+ connections)
**How**: Direct Redis integration with auto-detection (single-node or Sentinel)
**Key Feature**: Zero code changes when upgrading from single-node → Sentinel or Sentinel → Memorystore

---

## Quick Decision Tree

```
Need horizontal scaling? → YES → Deploy Redis (start with single node)
                       → NO  → Keep in-memory (single instance)

Need 99.9% uptime?     → YES → Upgrade to Sentinel (3 nodes)
                       → NO  → Single node is sufficient

Want managed solution? → YES → Migrate to GCP Memorystore
                       → NO  → Keep self-hosted
```

---

## Implementation Status

### ✅ **COMPLETED** (Code Ready on Branch)

**Redis Implementation** (`ws/internal/multi/broadcast.go` - 489 lines):
- ✅ Redis Pub/Sub direct implementation
- ✅ Auto-detection: 1 address = direct mode, 3+ addresses = Sentinel mode
- ✅ Automatic reconnection with exponential backoff
- ✅ Health monitoring (10s PING interval)
- ✅ Graceful shutdown with 5s timeout
- ✅ Non-blocking publish (100ms timeout)

**Configuration** (three-tier pattern):
- ✅ Shared defaults: `deployments/v1/shared/base.env`
- ✅ Local config: `deployments/v1/local/.env.local.example`
- ✅ GCP config: `deployments/v1/gcp/distributed/ws-server/.env.production`

**GCP Deployment Automation**:
- ✅ Task commands: `taskfiles/v1/gcp/redis.yml` (15+ commands)
- ✅ Docker Compose: `deployments/v1/gcp/distributed/redis/docker-compose.single.yml`
- ✅ Scripts: `scripts/v1/gcp/redis/` (health check, backup, failover test)

**Monitoring**:
- ✅ Prometheus scrape config for Redis Exporter
- ✅ Grafana dashboard (13 panels, 3 alerts)

**Local Development**:
- ✅ `deployments/v1/local/docker-compose.redis.yml`
- ✅ Local testing validated successfully

---

## Deployment Checklist

### Week 1: Local Testing & GCP Deployment

**Day 1: Local Testing (DONE)**
- [x] Start local Redis: `docker-compose -f docker-compose.redis.yml up -d`
- [x] Run ws-server locally
- [x] Verify Redis connection logs
- [x] Test successful

**Day 2-3: GCP Deployment**
- [ ] Deploy with automated tasks: `task gcp:deploy`
- [ ] Verify Redis health: `task gcp:redis:health`
- [ ] Get Redis IP: `task gcp:redis:get-ip`
- [ ] Update ws-server config with Redis IP
- [ ] Verify ws-server connects to Redis

**Day 4-5: Multi-Instance Testing**
- [ ] Deploy 2nd ws-server instance
- [ ] Test cross-instance messaging (100% delivery)
- [ ] Load test: 1000 connections per instance
- [ ] Monitor Redis metrics in Grafana

### Week 2: Production Rollout

**Day 1-3: Production Deployment**
- [ ] Update production .env with Redis config
- [ ] Rolling deployment (zero downtime)
- [ ] Monitor for 48 hours
- [ ] Validate <15ms p99 latency

**Day 4-5: Validation**
- [ ] Run load tests (target: 36K connections)
- [ ] Verify all metrics green
- [ ] Document lessons learned

### Optional: Upgrade to Sentinel HA

**When Needed**: 99.9% uptime requirement, automatic failover needed

**Steps**:
- [ ] Deploy 2 more Redis nodes (3 total)
- [ ] Configure replication and Sentinel
- [ ] Update `REDIS_SENTINEL_ADDRS` from 1 to 3 addresses
- [ ] Rolling restart - code auto-detects Sentinel!
- [ ] Test failover (kill master, verify <10s recovery)

---

## Essential Configuration

### Configuration (Auto-Detection)

```bash
# Local Development
REDIS_SENTINEL_ADDRS=localhost:6379
REDIS_PASSWORD=testpassword
REDIS_MASTER_NAME=mymaster
REDIS_DB=0
REDIS_CHANNEL=ws.broadcast

# GCP Single Node (Direct Connection)
REDIS_SENTINEL_ADDRS=10.128.0.X:6379
REDIS_PASSWORD=<generated-secure-password>
# Code auto-detects: 1 address = direct mode

# GCP Sentinel Cluster (HA with Failover)
REDIS_SENTINEL_ADDRS=node1:26379,node2:26379,node3:26379
REDIS_PASSWORD=<generated-secure-password>
# Code auto-detects: 3+ addresses = Sentinel mode

# GCP Memorystore (Managed)
REDIS_SENTINEL_ADDRS=10.x.x.x:6379
REDIS_PASSWORD=<gcp-generated-password>
# Code auto-detects: 1 address = direct mode
```

### Implementation Details

```go
// File: ws/internal/multi/broadcast.go (489 lines)
// Direct Redis implementation - NO interface abstraction

// Auto-detection logic:
if len(cfg.SentinelAddrs) == 1 {
    // Single address → Direct connection (standalone or Memorystore)
    client = redis.NewClient(&redis.Options{
        Addr:     cfg.SentinelAddrs[0],
        Password: cfg.Password,
        DB:       cfg.DB,
    })
} else {
    // Multiple addresses → Sentinel failover cluster
    client = redis.NewFailoverClient(&redis.FailoverOptions{
        MasterName:    cfg.MasterName,
        SentinelAddrs: cfg.SentinelAddrs,
        Password:      cfg.Password,
        DB:            cfg.DB,
    })
}
```

---

## Key Commands

### Local Testing

```bash
# 1. Start main stack (creates odin-local network)
cd deployments/v1/local
docker-compose up -d

# 2. Start Redis
docker-compose -f docker-compose.redis.yml up -d

# 3. Verify Redis
docker exec redis-local redis-cli -a testpassword ping
# Expected: PONG

# 4. Run ws-server
cd ../../..
go run ws/cmd/multi/main.go

# 5. Verify logs show:
# [INFO] Connecting to Redis (direct mode)
# [INFO] Successfully connected to Redis
# [INFO] BroadcastBus started (Redis Pub/Sub)
```

### GCP Deployment (Automated)

```bash
# Full deployment (includes Redis + WS servers + monitoring)
task gcp:deploy

# Or step-by-step:
task gcp:redis:create-vm
task gcp:redis:setup-firewall
task gcp:redis:deploy-single

# Get Redis internal IP for ws-server config
task gcp:redis:get-ip

# Health check
task gcp:redis:health

# Real-time metrics
task gcp:redis:metrics

# View logs
task gcp:redis:logs

# Check system status
task gcp:status
```

### Monitoring

```bash
# View Grafana dashboard
# URL: http://<backend-ip>:3005
# Login: admin / admin
# Dashboard: Redis BroadcastBus Monitoring

# Prometheus queries
# URL: http://<backend-ip>:9090
# Queries:
#   redis_up
#   redis_connected_clients
#   redis_latency_percentiles_usec{quantile="0.99"}
```

---

## Critical Success Factors

### Must-Have Before Production

1. **Redis Implementation**: Direct Redis Pub/Sub (489 lines) ✅
2. **Auto-Detection**: Single-node or Sentinel mode (zero config) ✅
3. **Health Monitoring**: Periodic PING, publish tracking ✅
4. **Graceful Shutdown**: 5s timeout, connection cleanup ✅
5. **Monitoring**: Prometheus + Grafana (13 panels, 3 alerts) ✅
6. **Task Automation**: 15+ deployment/ops commands ✅
7. **Runbooks**: Connection issues, health checks ✅

### Performance Targets

- **Latency**: End-to-end <15ms p99 (current baseline: ~10ms, Redis adds ~2-5ms)
- **Throughput**: 1K msg/sec sustained (Redis capacity: 1M msg/sec)
- **Availability**:
  - Single node: Manual restart (~30s downtime)
  - Sentinel HA: 99.9% uptime, <10s automatic failover
- **Memory**: Redis <90% (4GB max, configurable)
- **CPU**: Redis <20% under load

### Key Metrics to Monitor

```promql
# Redis availability
redis_up

# Connected clients (should equal number of ws-server instances)
redis_connected_clients

# Latency percentiles
redis_latency_percentiles_usec{quantile="0.99"}

# Memory usage
redis_memory_used_bytes / redis_memory_max_bytes

# Pub/Sub channels (should be 1: ws.broadcast)
redis_pubsub_channels

# Slow commands (should be <10)
redis_slowlog_length

# Error indicators (should be 0)
redis_evicted_keys_total
redis_rejected_connections_total
```

---

## Common Pitfalls to Avoid

### ❌ DON'T

1. **Forget main stack first** → Start `docker-compose.yml` before `docker-compose.redis.yml` (creates network)
2. **Use same password in all environments** → Generate unique secure passwords
3. **Skip local testing** → Always test locally before GCP deployment
4. **Deploy both instances simultaneously** → Use rolling deployment
5. **Ignore Redis memory limits** → Monitor, configure maxmemory with eviction policy
6. **Forget to get Redis IP** → Run `task gcp:redis:get-ip` and update ws-server config

### ✅ DO

1. **Start with single node** → Simple, cheap ($53/month), upgrade to Sentinel later if needed
2. **Test locally first** → Use `docker-compose.redis.yml` for local validation
3. **Use task commands** → Automated deployment/ops: `task gcp:deploy`, `task gcp:redis:health`
4. **Monitor from day 1** → Grafana dashboard ready, watch latency/memory/errors
5. **Generate strong passwords** → Use `openssl rand -base64 32 | tr -d "=+/" | cut -c1-32`
6. **Document your config** → Save Redis IP, passwords securely

---

## Deployment Modes & Upgrade Paths

### Mode 1: Local Development
```bash
REDIS_SENTINEL_ADDRS=localhost:6379
REDIS_PASSWORD=testpassword
```
- **Use**: Local testing
- **Cost**: $0 (Docker)
- **Availability**: Dev only

### Mode 2: GCP Single Node
```bash
REDIS_SENTINEL_ADDRS=10.128.0.X:6379
REDIS_PASSWORD=<generated>
```
- **Use**: Initial production, <10K connections
- **Cost**: $53/month (1× e2-standard-2)
- **Availability**: Manual restart (~30s downtime)
- **Upgrade**: Add 2 more nodes → Sentinel (change to 3 addresses)

### Mode 3: GCP Sentinel Cluster (HA)
```bash
REDIS_SENTINEL_ADDRS=node1:26379,node2:26379,node3:26379
REDIS_PASSWORD=<generated>
```
- **Use**: Production with 99.9% uptime SLA
- **Cost**: $158/month (3× e2-standard-2)
- **Availability**: Automatic failover <10s
- **Upgrade**: Migrate to Memorystore (change to 1 managed address)

### Mode 4: GCP Memorystore (Managed)
```bash
REDIS_SENTINEL_ADDRS=10.x.x.x:6379
REDIS_PASSWORD=<gcp-generated>
```
- **Use**: Production, fully managed, minimal ops
- **Cost**: $180/month (5GB Standard HA)
- **Availability**: 99.9% SLA, automatic failover
- **Benefit**: Zero maintenance, GCP handles upgrades/failover

### Zero-Code Upgrade Path

**All upgrades are just config changes:**
```bash
# Single → Sentinel: Change 1 address to 3
# Sentinel → Memorystore: Change 3 addresses to 1 managed endpoint
# Self-hosted → Managed: Same as above

# Rolling restart - code auto-detects mode!
```

---

## Resource Costs

### Single Instance (Baseline)
```
1× e2-highcpu-8 (ws-server):  $50/month
Total:                         $50/month
Capacity:                      18K connections
```

### Multi-Instance with Redis (Single Node)
```
2× e2-highcpu-8 (ws-server):   $100/month
1× e2-standard-2 (Redis):      $53/month
────────────────────────────────────────
Total:                         $153/month
Capacity:                      36K connections
Cost per 1K:                   $4.25
```

### Multi-Instance with Redis (Sentinel HA)
```
2× e2-highcpu-8 (ws-server):   $100/month
3× e2-standard-2 (Redis):      $158/month
────────────────────────────────────────
Total:                         $258/month
Capacity:                      36K connections
Cost per 1K:                   $7.17
Benefit:                       99.9% uptime, <10s failover
```

**ROI**: Start with single node ($153/month), upgrade to Sentinel when traffic justifies HA cost

---

## Quick Reference Links

**Implementation Details**:
- Implementation: `ws/internal/multi/broadcast.go` (489 lines)
- Configuration: `ws/internal/shared/platform/config.go`
- Integration: `ws/cmd/multi/main.go`

**Deployment Files**:
- GCP Redis: `deployments/v1/gcp/distributed/redis/`
- Local Redis: `deployments/v1/local/docker-compose.redis.yml`
- Task automation: `taskfiles/v1/gcp/redis.yml`
- Scripts: `scripts/v1/gcp/redis/`

**Documentation**:
- [Infrastructure Diagrams](./architecture/infrastructure-diagram.md) - System architecture (4 Mermaid diagrams)
- [Redis Single Node Deployment](./deployment/REDIS_SINGLE_NODE.md) - Quick deploy guide
- [Simplified Implementation](./implementation/REDIS_BROADCAST_BUS_SIMPLIFIED.md) - Detailed plan

**Runbooks**:
- [Redis Connection Issues](./runbooks/redis-connection-issues.md) - Troubleshooting
- [Rollback to In-Memory](./runbooks/rollback-to-inmemory.md) - Emergency recovery

**Monitoring**:
- Grafana Dashboard: `deployments/v1/gcp/distributed/backend/grafana/provisioning/dashboards/redis.json`
- Prometheus Config: `deployments/v1/gcp/distributed/backend/prometheus.yml`

**External Resources**:
- Redis Pub/Sub: https://redis.io/docs/manual/pubsub/
- Redis Sentinel: https://redis.io/docs/management/sentinel/
- go-redis Library: https://github.com/redis/go-redis

---

## Next Steps

### ✅ **Code Complete** - Ready to Deploy!

**Branch**: `feature/redis-broadcast-bus` (pushed to remote)

**Immediate Actions**:
1. ✅ Local testing complete (Redis running, ws-server verified)
2. 📋 Create Pull Request (`feature/redis-broadcast-bus` → `main`)
3. 📋 Deploy to GCP: `task gcp:deploy`
4. 📋 Multi-instance testing (2+ ws-server instances)

**Reference Session Handoffs**:
- Latest: `sessions/handoff-2025-11-22-1144.md` (local testing success)
- Previous: `sessions/handoff-2025-11-21-2154.md` (implementation complete)

**Useful Commands**:
```bash
# View all task commands
task --list-all | grep redis

# Check implementation
git log --oneline feature/redis-broadcast-bus

# Review configuration
cat deployments/v1/shared/base.env | grep REDIS
cat deployments/v1/local/.env.local.example | grep REDIS
```

**Ready to ship!** 🚀

---

**Version**: 2.0
**Last Updated**: 2025-11-23 (Corrected to reflect actual implementation)
**Status**: ✅ Implementation Complete - Deployment Ready
**Branch**: `feature/redis-broadcast-bus`
