# Valkey Deployment for WebSocket BroadcastBus

Valkey Pub/Sub infrastructure for cross-instance message broadcasting in multi-instance WebSocket server architecture.

## Architecture

**Purpose**: Enable horizontal scaling by broadcasting Kafka messages to ALL WebSocket server instances via Valkey Pub/Sub.

**Flow**:
```
Kafka → KafkaConsumerPool → BroadcastBus.Publish()
                           ↓
                      Valkey PUBLISH
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

**Key Point**: Valkey does NOT do targeted delivery. ALL shards receive ALL messages and filter locally (O(1) hash lookup).

## Deployment Modes

### 1. Single Node (Current)
- **Use case**: Development, initial production, <10K connections
- **Cost**: ~$62/month (e2-standard-2 + disk)
- **HA**: None (manual restart if Valkey crashes)
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
- **Setup**: `gcloud valkey instances create`

## Quick Start

### Prerequisites
- GCP project configured
- `gcloud` CLI authenticated
- Docker installed on local machine

### 1. Generate Valkey Password

```bash
# Generate strong password
openssl rand -base64 32 | tr -d "=+/" | cut -c1-32

# Store in GCP Secret Manager
echo -n "YOUR_GENERATED_PASSWORD" | gcloud secrets create valkey-password --data-file=-

# Save to .env
cp .env.example .env
# Edit .env and set VALKEY_PASSWORD=YOUR_GENERATED_PASSWORD
```

### 2. Deploy Valkey VM

```bash
# From project root
cd /Volumes/Dev/Codev/Toniq/ws_poc

# Create VM, firewall, deploy Valkey (all in one)
task valkey:create-vm
task valkey:setup-firewall
task valkey:deploy-single

# Verify
task valkey:health
```

### 3. Update WS Server Configuration

```bash
# Get Valkey internal IP
VALKEY_IP=$(gcloud compute instances describe ws-valkey \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].networkIP)')

# Update ws-server .env
cd ws
cat >> .env <<EOF
# Valkey Configuration (auto-detects single node mode)
VALKEY_ADDRS=${VALKEY_IP}:6379
VALKEY_PASSWORD=YOUR_GENERATED_PASSWORD
VALKEY_DB=0
VALKEY_CHANNEL=ws.broadcast
EOF
```

### 4. Test Local Connection

```bash
# Start local Valkey (for testing)
docker-compose -f docker-compose.valkey-local.yml up -d

# Create ws/.env for local testing
cd ws
cat > .env <<EOF
VALKEY_ADDRS=localhost:6379
VALKEY_PASSWORD=testpassword
VALKEY_DB=0
VALKEY_CHANNEL=ws.broadcast
EOF

# Run ws-server
go run cmd/multi/main.go

# Look for these logs:
# [INFO] Connecting to Valkey (direct mode)
# [INFO] Successfully connected to Valkey
# [INFO] BroadcastBus started (Valkey Pub/Sub)
```

## Configuration Files

### docker-compose.single.yml
Single-node Valkey deployment with:
- Valkey 7.2 Alpine
- Valkey Exporter (Prometheus metrics on :9121)
- Health checks
- Automatic restart

### valkey.conf
Production-optimized configuration:
- Persistence disabled (pub/sub workload, ephemeral data)
- 4GB max memory (allkeys-lru eviction)
- Pub/sub buffer limits tuned
- Latency monitoring enabled
- Dangerous commands disabled (FLUSHALL, FLUSHDB)

### .env.example
Template for environment variables (copy to `.env`)

## Task Commands

All Valkey operations are managed via Taskfile:

```bash
# Deployment
task valkey:create-vm           # Create GCP VM
task valkey:setup-firewall      # Configure firewall rules
task valkey:deploy-single       # Deploy single-node Valkey

# Health & Monitoring
task valkey:health              # Run comprehensive health check
task valkey:logs                # Tail Valkey logs
task valkey:metrics             # Show real-time metrics
task valkey:cli                 # Open Valkey CLI (debug only)

# Testing
task valkey:load-test           # Valkey benchmark
task valkey:pubsub-test         # Pub/sub latency test

# Maintenance
task valkey:restart             # Graceful restart
task valkey:stop                # Stop Valkey
task valkey:start               # Start Valkey
task valkey:backup              # Create snapshot (for debugging)

# Future: Sentinel
task valkey:deploy-sentinel     # Deploy 3-node HA cluster
task valkey:failover-test       # Test automatic failover
```

## Monitoring

### Metrics (Prometheus)

Valkey Exporter exposes metrics on `:9121/metrics`:

**Critical Metrics**:
- `valkey_up` - Instance availability (alert if 0)
- `valkey_connected_clients` - Should match WS server count (2-10)
- `valkey_commands_processed_per_sec` - Throughput (target: 1K-10K)
- `valkey_latency_percentiles_usec{quantile="0.99"}` - p99 latency (target: <5000µs)
- `valkey_memory_used_bytes / valkey_memory_max_bytes` - Memory usage (alert if >90%)

**Important Metrics**:
- `valkey_pubsub_channels` - Active broadcast channels
- `valkey_slowlog_length` - Slow commands (investigate if >10)
- `valkey_evicted_keys_total` - Should be 0 (pub/sub workload)
- `valkey_rejected_connections_total` - Connection limit issues

### Grafana Dashboard

Import dashboard from: `docs/monitoring/valkey-dashboard.json` (to be created)

**Panels**:
1. Overview: uptime, clients, commands/sec, memory %
2. Pub/Sub: active channels, messages/sec, buffer usage
3. Performance: p50/p95/p99 latency, slow log, network throughput
4. Health: evicted keys, rejected connections, errors

### Alerts

Configure in Prometheus (see `taskfiles/v1/gcp/valkey.yml`):
- Valkey down (>1min)
- p99 latency >10ms (>5min)
- Memory >90% (>5min)
- Client count mismatch (not 2-10)
- Slow commands detected

## Debugging

### Health Check Script

```bash
# Comprehensive health check
./scripts/v1/gcp/valkey/healthcheck.sh

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

**Issue**: `valkey_up == 0`
```bash
# Check if Valkey container is running
docker ps | grep valkey

# Check logs
docker logs valkey-single --tail=100

# Restart if needed
docker-compose restart valkey
```

**Issue**: High latency (>10ms)
```bash
# Check slow log
valkey-cli SLOWLOG GET 10

# Check CPU usage
top

# Check network latency
ping <VALKEY_IP>

# Run latency doctor
valkey-cli --latency-doctor
```

**Issue**: Memory at 100%
```bash
# Check memory details
valkey-cli INFO memory

# Check eviction policy
valkey-cli CONFIG GET maxmemory-policy

# Flush (CAUTION: deletes all data)
valkey-cli FLUSHALL

# Or increase maxmemory in valkey.conf
```

**Issue**: Client count mismatch
```bash
# List connected clients
valkey-cli CLIENT LIST

# Expected: 1 connection per WS server instance
# If more: Check for connection leaks in ws-server
# If less: Check if ws-servers are running
```

### CLI Access

```bash
# Via task command (recommended)
task valkey:cli

# Or directly
docker exec -it valkey-single valkey-cli -a $VALKEY_PASSWORD

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

- **valkey-internal**: tcp:6379 from `ws-server` tag only
- **valkey-exporter**: tcp:9121 from `monitoring` tag only
- **valkey-admin**: tcp:6379 from your IP (temporary, for setup only)

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
- TLS encryption (when supported by go-valkey)
- VPC Service Controls
- Audit logging

## Backup & Recovery

### Backup (Optional)

```bash
# Create snapshot (for debugging, not critical for pub/sub)
task valkey:backup

# Backup stored in: ./backups/valkey_backup_TIMESTAMP.rdb.gz
```

### Restore

```bash
# Stop Valkey
docker-compose stop valkey

# Extract backup
gunzip backups/valkey_backup_TIMESTAMP.rdb.gz

# Copy to Valkey data directory
docker cp backups/valkey_backup_TIMESTAMP.rdb valkey-single:/data/dump.rdb

# Start Valkey
docker-compose start valkey
```

**Note**: For pub/sub workload, backups are not critical. Data is ephemeral and regenerated from Kafka on restart.

## Upgrading to Sentinel

When you need automatic failover:

### 1. Deploy Additional Nodes

```bash
# Create 2 more VMs (total 3)
task valkey:create-sentinel-cluster
```

### 2. Update Configuration

```bash
# ws-server .env (change 1 address to 3)
VALKEY_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379
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
task valkey:failover-test

# Expected: <5s downtime, automatic recovery
```

**Zero code changes needed!** The BroadcastBus auto-detects Sentinel mode.

## Cost Analysis

| Mode | Resources | Cost/Month | Downtime on Valkey Failure |
|------|-----------|------------|---------------------------|
| **Single Node** | 1× e2-standard-2 | ~$62 | Manual restart (~30s) |
| **Sentinel** | 3× e2-standard-2 | ~$185 | Auto-failover (<5s) |
| **Memorystore** | 5GB Standard HA | ~$180 | Auto-failover (<5s) |

**Recommendation**: Start with single node, upgrade when:
- Connections >10K (critical traffic)
- Downtime <5s unacceptable
- Need 99.9% uptime SLA

## Related Documentation

- **Architecture**: `docs/architecture/VALKEY_BROADCAST_BUS_ARCHITECTURE.md`
- **Runbooks**: `docs/runbooks/VALKEY_TROUBLESHOOTING.md`
- **Implementation**: `ws/internal/multi/broadcast.go`
- **Deployment Guide**: `docs/deployment/VALKEY_SINGLE_NODE.md`

## Support

For issues:
1. Check `task valkey:health`
2. Review `task valkey:logs`
3. Check Grafana dashboard
4. See "Debugging" section above
5. Review `docs/runbooks/VALKEY_TROUBLESHOOTING.md`
