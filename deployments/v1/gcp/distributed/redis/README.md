# Redis Deployment for WebSocket BroadcastBus

Redis Pub/Sub infrastructure for cross-instance message broadcasting in multi-instance WebSocket server architecture.

## Architecture

**Purpose**: Enable horizontal scaling by broadcasting Kafka messages to ALL WebSocket server instances via Redis Pub/Sub.

**Flow**:
```
Kafka → KafkaConsumerPool → BroadcastBus.Publish()
                           ↓
                      Redis PUBLISH
                           ↓
                    (Broadcasts to ALL)
                           ↓
        ┌──────────────────┼──────────────────┐
        ↓                  ↓                  ↓
   Instance 1         Instance 2         Instance N
   Shard 0-2          Shard 0-2          Shard 0-2
   (filters)          (filters)          (filters)
        ↓                  ↓                  ↓
   Subscribed         Subscribed         Subscribed
   clients only       clients only       clients only
```

**Key Point**: Redis does NOT do targeted delivery. ALL shards receive ALL messages and filter locally (O(1) hash lookup).

## Deployment Modes

### 1. Single Node (Current)
- **Use case**: Development, initial production, <10K connections
- **Cost**: ~$62/month (e2-standard-2 + disk)
- **HA**: None (manual restart if Redis crashes)
- **Setup**: See "Quick Start" below

### 2. Sentinel Cluster (Future)
- **Use case**: Production with HA requirements, automatic failover
- **Cost**: ~$185/month (3× e2-standard-2)
- **HA**: Automatic failover <5s
- **Setup**: See `docker-compose.sentinel.yml` (future)

### 3. GCP Memorystore (Future)
- **Use case**: Managed service, reduce ops burden
- **Cost**: ~$180/month (5GB standard HA)
- **HA**: Built-in automatic failover
- **Setup**: `gcloud redis instances create`

## Quick Start

### Prerequisites
- GCP project configured
- `gcloud` CLI authenticated
- Docker installed on local machine

### 1. Generate Redis Password

```bash
# Generate strong password
openssl rand -base64 32 | tr -d "=+/" | cut -c1-32

# Store in GCP Secret Manager
echo -n "YOUR_GENERATED_PASSWORD" | gcloud secrets create redis-password --data-file=-

# Save to .env
cp .env.example .env
# Edit .env and set REDIS_PASSWORD=YOUR_GENERATED_PASSWORD
```

### 2. Deploy Redis VM

```bash
# From project root
cd /Volumes/Dev/Codev/Toniq/ws_poc

# Create VM, firewall, deploy Redis (all in one)
task redis:create-vm
task redis:setup-firewall
task redis:deploy-single

# Verify
task redis:health
```

### 3. Update WS Server Configuration

```bash
# Get Redis internal IP
REDIS_IP=$(gcloud compute instances describe ws-poc-redis \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].networkIP)')

# Update ws-server .env
cd ws
cat >> .env <<EOF
# Redis Configuration (auto-detects single node mode)
REDIS_SENTINEL_ADDRS=${REDIS_IP}:6379
REDIS_PASSWORD=YOUR_GENERATED_PASSWORD
REDIS_DB=0
REDIS_CHANNEL=ws.broadcast
EOF
```

### 4. Test Local Connection

```bash
# Start local Redis (for testing)
docker-compose -f docker-compose.redis-local.yml up -d

# Create ws/.env for local testing
cd ws
cat > .env <<EOF
REDIS_SENTINEL_ADDRS=localhost:6379
REDIS_PASSWORD=testpassword
REDIS_DB=0
REDIS_CHANNEL=ws.broadcast
EOF

# Run ws-server
go run cmd/multi/main.go

# Look for these logs:
# [INFO] Connecting to Redis (direct mode)
# [INFO] Successfully connected to Redis
# [INFO] BroadcastBus started (Redis Pub/Sub)
```

## Configuration Files

### docker-compose.single.yml
Single-node Redis deployment with:
- Redis 7.2 Alpine
- Redis Exporter (Prometheus metrics on :9121)
- Health checks
- Automatic restart

### redis.conf
Production-optimized configuration:
- Persistence disabled (pub/sub workload, ephemeral data)
- 4GB max memory (allkeys-lru eviction)
- Pub/sub buffer limits tuned
- Latency monitoring enabled
- Dangerous commands disabled (FLUSHALL, FLUSHDB)

### .env.example
Template for environment variables (copy to `.env`)

## Task Commands

All Redis operations are managed via Taskfile:

```bash
# Deployment
task redis:create-vm           # Create GCP VM
task redis:setup-firewall      # Configure firewall rules
task redis:deploy-single       # Deploy single-node Redis

# Health & Monitoring
task redis:health              # Run comprehensive health check
task redis:logs                # Tail Redis logs
task redis:metrics             # Show real-time metrics
task redis:cli                 # Open Redis CLI (debug only)

# Testing
task redis:load-test           # Redis benchmark
task redis:pubsub-test         # Pub/sub latency test

# Maintenance
task redis:restart             # Graceful restart
task redis:stop                # Stop Redis
task redis:start               # Start Redis
task redis:backup              # Create snapshot (for debugging)

# Future: Sentinel
task redis:deploy-sentinel     # Deploy 3-node HA cluster
task redis:failover-test       # Test automatic failover
```

## Monitoring

### Metrics (Prometheus)

Redis Exporter exposes metrics on `:9121/metrics`:

**Critical Metrics**:
- `redis_up` - Instance availability (alert if 0)
- `redis_connected_clients` - Should match WS server count (2-10)
- `redis_commands_processed_per_sec` - Throughput (target: 1K-10K)
- `redis_latency_percentiles_usec{quantile="0.99"}` - p99 latency (target: <5000µs)
- `redis_memory_used_bytes / redis_memory_max_bytes` - Memory usage (alert if >90%)

**Important Metrics**:
- `redis_pubsub_channels` - Active broadcast channels
- `redis_slowlog_length` - Slow commands (investigate if >10)
- `redis_evicted_keys_total` - Should be 0 (pub/sub workload)
- `redis_rejected_connections_total` - Connection limit issues

### Grafana Dashboard

Import dashboard from: `docs/monitoring/redis-dashboard.json` (to be created)

**Panels**:
1. Overview: uptime, clients, commands/sec, memory %
2. Pub/Sub: active channels, messages/sec, buffer usage
3. Performance: p50/p95/p99 latency, slow log, network throughput
4. Health: evicted keys, rejected connections, errors

### Alerts

Configure in Prometheus (see `taskfiles/v1/gcp/redis.yml`):
- Redis down (>1min)
- p99 latency >10ms (>5min)
- Memory >90% (>5min)
- Client count mismatch (not 2-10)
- Slow commands detected

## Debugging

### Health Check Script

```bash
# Comprehensive health check
./scripts/v1/gcp/redis/healthcheck.sh

# Checks:
# - Connectivity (PING)
# - Version, uptime
# - Memory usage
# - Connected clients (expected: 2-10)
# - Pub/sub channels/patterns
# - Latency (5-ping test)
# - Slow log entries
# - Evicted/rejected keys
```

### Common Issues

**Issue**: `redis_up == 0`
```bash
# Check if Redis container is running
docker ps | grep redis

# Check logs
docker logs redis-single --tail=100

# Restart if needed
docker-compose restart redis
```

**Issue**: High latency (>10ms)
```bash
# Check slow log
redis-cli SLOWLOG GET 10

# Check CPU usage
top

# Check network latency
ping <REDIS_IP>

# Run latency doctor
redis-cli --latency-doctor
```

**Issue**: Memory at 100%
```bash
# Check memory details
redis-cli INFO memory

# Check eviction policy
redis-cli CONFIG GET maxmemory-policy

# Flush (CAUTION: deletes all data)
redis-cli FLUSHALL

# Or increase maxmemory in redis.conf
```

**Issue**: Client count mismatch
```bash
# List connected clients
redis-cli CLIENT LIST

# Expected: 1 connection per WS server instance
# If more: Check for connection leaks in ws-server
# If less: Check if ws-servers are running
```

### CLI Access

```bash
# Via task command (recommended)
task redis:cli

# Or directly
docker exec -it redis-single redis-cli -a $REDIS_PASSWORD

# Useful commands:
127.0.0.1:6379> INFO stats
127.0.0.1:6379> PUBSUB CHANNELS
127.0.0.1:6379> PUBSUB NUMSUB ws.broadcast
127.0.0.1:6379> CLIENT LIST
127.0.0.1:6379> SLOWLOG GET 10
127.0.0.1:6379> LATENCY DOCTOR
127.0.0.1:6379> MONITOR  # Real-time command stream (careful in prod!)
```

## Security

### Firewall Rules

- **redis-internal**: tcp:6379 from `ws-server` tag only
- **redis-exporter**: tcp:9121 from `monitoring` tag only
- **redis-admin**: tcp:6379 from your IP (temporary, for setup only)

### Best Practices

✅ **Implemented**:
- Strong password (32+ chars, random)
- `requirepass` enabled
- `protected-mode yes`
- Dangerous commands disabled (FLUSHALL, FLUSHDB, CONFIG)
- Firewall: VPC-only access
- No public IP

⚠️ **Future Hardening**:
- Rotate password quarterly
- TLS encryption (when supported by go-redis)
- VPC Service Controls
- Audit logging

## Backup & Recovery

### Backup (Optional)

```bash
# Create snapshot (for debugging, not critical for pub/sub)
task redis:backup

# Backup stored in: ./backups/redis_backup_TIMESTAMP.rdb.gz
```

### Restore

```bash
# Stop Redis
docker-compose stop redis

# Extract backup
gunzip backups/redis_backup_TIMESTAMP.rdb.gz

# Copy to Redis data directory
docker cp backups/redis_backup_TIMESTAMP.rdb redis-single:/data/dump.rdb

# Start Redis
docker-compose start redis
```

**Note**: For pub/sub workload, backups are not critical. Data is ephemeral and regenerated from Kafka on restart.

## Upgrading to Sentinel

When you need automatic failover:

### 1. Deploy Additional Nodes

```bash
# Create 2 more VMs (total 3)
task redis:create-sentinel-cluster
```

### 2. Update Configuration

```bash
# ws-server .env (change 1 address to 3)
REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379
```

### 3. Rolling Restart

```bash
# ws-servers automatically detect Sentinel mode
# Code uses len(addrs) to choose connection type
task services:restart-ws-servers
```

### 4. Test Failover

```bash
# Trigger manual failover
task redis:failover-test

# Expected: <5s downtime, automatic recovery
```

**Zero code changes needed!** The BroadcastBus auto-detects Sentinel mode.

## Cost Analysis

| Mode | Resources | Cost/Month | Downtime on Redis Failure |
|------|-----------|------------|---------------------------|
| **Single Node** | 1× e2-standard-2 | ~$62 | Manual restart (~30s) |
| **Sentinel** | 3× e2-standard-2 | ~$185 | Auto-failover (<5s) |
| **Memorystore** | 5GB Standard HA | ~$180 | Auto-failover (<5s) |

**Recommendation**: Start with single node, upgrade when:
- Connections >10K (critical traffic)
- Downtime <5s unacceptable
- Need 99.9% uptime SLA

## Related Documentation

- **Architecture**: `docs/architecture/REDIS_BROADCAST_BUS_ARCHITECTURE.md`
- **Runbooks**: `docs/runbooks/REDIS_TROUBLESHOOTING.md`
- **Implementation**: `ws/internal/multi/broadcast.go`
- **Deployment Guide**: `docs/deployment/REDIS_SINGLE_NODE.md`

## Support

For issues:
1. Check `task redis:health`
2. Review `task redis:logs`
3. Check Grafana dashboard
4. See "Debugging" section above
5. Review `docs/runbooks/REDIS_TROUBLESHOOTING.md`
