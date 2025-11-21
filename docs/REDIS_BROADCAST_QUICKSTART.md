# Redis Broadcast Bus: Quick Start Guide

**Purpose**: Fast-track guide for implementing Redis-based broadcast bus
**Audience**: Engineers implementing the architecture
**Time**: 1-2 weeks to production
**Main Document**: [REDIS_BROADCAST_BUS_ARCHITECTURE.md](./architecture/REDIS_BROADCAST_BUS_ARCHITECTURE.md)

---

## TL;DR

**What**: Replace in-memory BroadcastBus with Redis Pub/Sub for horizontal scaling
**Why**: Enable multi-instance deployment (18K → 36K+ connections)
**How**: Interface abstraction + configuration-based switching
**Key Feature**: Zero code changes to swap Redis ↔ NATS later

---

## Quick Decision Tree

```
Need horizontal scaling? → YES → Deploy Redis
                       → NO  → Stay on in-memory

Redis performing well? → YES → Stay on Redis
                      → NO  → Migrate to NATS (config change only!)
```

---

## Implementation Checklist

### Week 1: Interface & Redis Implementation

**Day 1-2: Refactor to Interface**
- [ ] Create `ws/internal/multi/broadcast_interface.go`
- [ ] Rename `broadcast.go` → `broadcast_inmemory.go`
- [ ] Create `broadcast_factory.go`
- [ ] Update `main.go` to use factory
- [ ] Run tests: `go test ./... -v`
- [ ] Verify: Single-instance still works

**Day 3-4: Implement Redis**
- [ ] Add dependency: `go get github.com/redis/go-redis/v9`
- [ ] Create `broadcast_redis.go` (implementation provided)
- [ ] Create `broadcast_redis_test.go` (tests provided)
- [ ] Run tests: `go test ./... -run Redis -v`
- [ ] Verify: Unit tests pass

**Day 5: Deploy Redis to GCP**
- [ ] Provision e2-standard-2 instance
- [ ] Install Redis 7.x
- [ ] Configure Sentinel (1M + 2R + 3S)
- [ ] Test connectivity from ws-server
- [ ] Verify failover works

### Week 2: Testing & Staging

**Day 1-2: Shadow Mode**
- [ ] Deploy dual-bus mode (publish to both)
- [ ] Monitor: 100% message consistency
- [ ] Run for 48 hours
- [ ] Metrics: Latency <2ms added

**Day 3-4: Staging Deployment**
- [ ] Set `BROADCAST_BUS_TYPE=redis`
- [ ] Deploy to staging
- [ ] Load test: 1000 connections
- [ ] Verify: Cross-instance messaging works

**Day 5: Production Prep**
- [ ] Update production config
- [ ] Review rollback procedure
- [ ] Prepare monitoring dashboards
- [ ] Brief on-call team

### Week 3-4: Production Rollout

**Week 3: Gradual Migration**
- [ ] Day 1: 10% traffic on Redis
- [ ] Day 2: 25% traffic on Redis
- [ ] Day 3: 50% traffic on Redis
- [ ] Day 4: 75% traffic on Redis
- [ ] Day 5: 100% traffic on Redis

**Week 4: Multi-Instance**
- [ ] Deploy 2nd ws-server instance
- [ ] Update load balancer
- [ ] Load test: 36K connections
- [ ] Verify: All metrics green

---

## Essential Code Snippets

### 1. Interface Definition

```go
// File: ws/internal/multi/broadcast_interface.go
type BroadcastBus interface {
    Run()
    Shutdown()
    Publish(msg *BroadcastMessage)
    Subscribe() chan *BroadcastMessage
    IsHealthy() bool
}
```

### 2. Configuration

```bash
# In-memory (single instance)
BROADCAST_BUS_TYPE=inmemory

# Redis (multi-instance)
BROADCAST_BUS_TYPE=redis
REDIS_SENTINEL_ADDRS=10.128.0.5:26379,10.128.0.5:26380,10.128.0.5:26381
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<secure-password>

# NATS (future migration)
BROADCAST_BUS_TYPE=nats
NATS_URL=nats://nats-1:4222,nats://nats-2:4222
```

### 3. Factory Pattern

```go
// File: ws/cmd/multi/main.go
busConfig := multi.BroadcastBusConfig{
    Type:               cfg.BroadcastBusType,
    RedisSentinelAddrs: cfg.GetRedisSentinelAddrs(),
    RedisMasterName:    cfg.RedisMasterName,
    RedisPassword:      cfg.RedisPassword,
    Logger:             busLogger,
}

broadcastBus, err := multi.NewBroadcastBus(busConfig)
if err != nil {
    logger.Fatalf("Failed to create BroadcastBus: %v", err)
}
defer broadcastBus.Shutdown()

broadcastBus.Run()
```

---

## Key Commands

### Deploy Redis to GCP

```bash
# 1. Provision instance
gcloud compute instances create redis-server \
  --machine-type=e2-standard-2 \
  --zone=us-central1-a \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-ssd \
  --tags=redis-server \
  --no-address

# 2. Create firewall rule
gcloud compute firewall-rules create allow-redis-internal \
  --allow=tcp:6379,tcp:6380,tcp:6381,tcp:26379,tcp:26380,tcp:26381 \
  --source-ranges=10.128.0.0/20 \
  --target-tags=redis-server

# 3. SSH and install
gcloud compute ssh redis-server --zone=us-central1-a
sudo apt update && sudo apt install -y redis-server redis-tools

# 4. Configure (see main doc for full configs)
# Copy configs from main architecture doc

# 5. Start services
sudo systemctl enable redis-master redis-replica-{1,2} redis-sentinel-{1,2,3}
sudo systemctl start redis-master redis-replica-{1,2} redis-sentinel-{1,2,3}

# 6. Verify
redis-cli -a <PASSWORD> INFO replication
# Expected: role:master, connected_slaves:2
```

### Deploy ws-server with Redis

```bash
# Update environment
export BROADCAST_BUS_TYPE=redis
export REDIS_SENTINEL_ADDRS=10.128.0.5:26379,10.128.0.5:26380,10.128.0.5:26381
export REDIS_MASTER_NAME=mymaster
export REDIS_PASSWORD=<password>

# Deploy
task gcp:deploy:ws-server

# Verify health
curl http://ws-server-1:3005/health | jq '.broadcast_bus'
# Expected: {"type":"redis","healthy":true,"connected":true}
```

### Emergency Rollback

```bash
# If ANY issues, rollback immediately
export BROADCAST_BUS_TYPE=inmemory
task gcp:deploy:rolling-restart

# Verify
curl http://ws-server-1:3005/health | jq '.broadcast_bus.type'
# Expected: "inmemory"
```

---

## Critical Success Factors

### Must-Have Before Production

1. **Interface Abstraction**: BroadcastBus interface implemented ✓
2. **Configuration-Based**: Swap via env var only ✓
3. **Health Checks**: `/health` reports bus type and status ✓
4. **Rollback Plan**: Tested and documented ✓
5. **Monitoring**: Prometheus + Grafana dashboards ✓
6. **Alerting**: Redis connection/latency alerts ✓
7. **Runbooks**: Connection issues, rollback procedures ✓

### Performance Targets

- **Latency**: End-to-end <10ms p99 (with Redis ~8-9ms)
- **Throughput**: 1K msg/sec sustained (capacity for 10K)
- **Availability**: 99.9% uptime (Sentinel HA)
- **Failover**: <10s recovery (Sentinel automatic)

### Key Metrics to Monitor

```promql
# Connection status
broadcast_bus_connected{type="redis"}

# Publish latency
histogram_quantile(0.99, rate(broadcast_bus_publish_duration_seconds_bucket[5m]))

# Error rate
rate(broadcast_bus_publish_total{type="redis",status="error"}[5m])

# Redis health
redis_up
```

---

## Common Pitfalls to Avoid

### ❌ DON'T

1. **Deploy Redis without Sentinel** → No HA, single point of failure
2. **Skip shadow mode testing** → Risk of production issues
3. **Forget to update load balancer** → 2nd instance won't receive traffic
4. **Use same password in all environments** → Security risk
5. **Deploy both instances simultaneously** → Rolling restart required
6. **Ignore metrics** → Won't detect issues early

### ✅ DO

1. **Use Sentinel with 3 nodes** → Automatic failover
2. **Test failover before production** → Confidence in HA
3. **Monitor latency at each stage** → Catch regressions early
4. **Have rollback plan ready** → Fast recovery if issues
5. **Document all configuration** → Team can troubleshoot
6. **Run load tests** → Verify capacity before cutover

---

## Migration Paths

### Path 1: In-Memory → Redis (Production)

```
Week 1: Implement interface + Redis
Week 2: Shadow mode testing
Week 3: Gradual rollout (10% → 100%)
Week 4: Multi-instance deployment
```

**Result**: 2× capacity (18K → 36K connections)

### Path 2: Redis → NATS (Future Optimization)

```
Month 3-6: Implement NatsBroadcastBus
           Shadow mode (Redis + NATS)
           Gradual migration
           Decommission Redis
```

**Benefit**: 2-3ms lower latency, 10× higher throughput

**Key**: ZERO CODE CHANGES! Just change env vars:
```bash
# Before
BROADCAST_BUS_TYPE=redis

# After
BROADCAST_BUS_TYPE=nats
```

---

## Resource Costs

### Single Instance (Baseline)
```
1× e2-highcpu-8 (ws-server):  $50/month
Total:                         $50/month
Capacity:                      18K connections
```

### Multi-Instance with Redis
```
2× e2-highcpu-8 (ws-server):   $100/month
1× e2-standard-2 (Redis):      $50/month
────────────────────────────────────────
Total:                         $150/month
Capacity:                      36K connections
Cost per 1K:                   $4.17
```

**ROI**: +$100/month for 2× capacity + HA + horizontal scaling

---

## Quick Reference Links

**Main Documentation**:
- [Redis Broadcast Bus Architecture](./architecture/REDIS_BROADCAST_BUS_ARCHITECTURE.md) - Complete guide
- [Horizontal Scaling Plan](./architecture/HORIZONTAL_SCALING_PLAN.md) - Multi-instance strategy

**Runbooks**:
- [Redis Connection Issues](./runbooks/redis-connection-issues.md) - Troubleshooting
- [Rollback to In-Memory](./runbooks/rollback-to-inmemory.md) - Emergency recovery

**Code Examples**:
- Interface: `ws/internal/multi/broadcast_interface.go`
- Redis Implementation: `ws/internal/multi/broadcast_redis.go`
- Factory: `ws/internal/multi/broadcast_factory.go`

**External Resources**:
- Redis Pub/Sub: https://redis.io/docs/manual/pubsub/
- Redis Sentinel: https://redis.io/docs/management/sentinel/
- go-redis Library: https://github.com/redis/go-redis

---

## Getting Help

**Questions**:
- Slack: #websocket-scaling channel
- Email: architecture-team@company.com

**Issues**:
- Critical (production down): Page on-call engineer
- High (performance degraded): Post in #incidents
- Medium (questions/clarification): Post in #websocket-scaling

**Contributing**:
- Propose changes via PR to main architecture doc
- Test changes in dev/staging first
- Update this quick start guide if architecture changes

---

## Next Steps

1. **Read Main Document**: [REDIS_BROADCAST_BUS_ARCHITECTURE.md](./architecture/REDIS_BROADCAST_BUS_ARCHITECTURE.md)
2. **Start Week 1**: Follow implementation checklist
3. **Ask Questions**: Post in #websocket-scaling channel
4. **Track Progress**: Update checklist as you go

**Good luck with the implementation!** 🚀

---

**Version**: 1.0
**Last Updated**: 2025-01-20
**Status**: Ready for Implementation
