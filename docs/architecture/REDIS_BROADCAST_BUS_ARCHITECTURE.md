# Redis-Based Broadcast Bus: Comprehensive Architecture & Implementation Guide

**Status**: Implementation Ready
**Date**: 2025-01-20
**Author**: Architecture Team
**Goal**: Implement Redis Pub/Sub broadcast bus for horizontal scaling with zero-code-change migration path to NATS

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Architecture Decision Record](#architecture-decision-record)
3. [Interface Design](#interface-design)
4. [Redis Deployment Architecture](#redis-deployment-architecture)
5. [Configuration Strategy](#configuration-strategy)
6. [Implementation Plan](#implementation-plan)
7. [Migration Paths](#migration-paths)
8. [Performance Considerations](#performance-considerations)
9. [Error Handling & Resilience](#error-handling--resilience)
10. [Monitoring & Observability](#monitoring--observability)
11. [Risk Analysis & Mitigation](#risk-analysis--mitigation)
12. [Cost Analysis](#cost-analysis)
13. [Decision Matrix](#decision-matrix)

---

## Executive Summary

### Problem Statement

Current `BroadcastBus` uses in-memory Go channels (works for single instance only). To support horizontal scaling with multiple GCP instances, we need an external broadcast bus accessible across all instances.

**Current State**:
```
Single Instance:
  Kafka → InMemoryBroadcastBus → 3 Shards → 18K connections ✓

Multiple Instances (BROKEN):
  Instance 1: Kafka[48 parts] → Bus#1 → Shards 0-2 → 18K clients
  Instance 2: Kafka[48 parts] → Bus#2 → Shards 3-5 → 18K clients

  Problem: Bus#1 ≠ Bus#2 → 50% message loss
```

**Target State**:
```
Multiple Instances:
  Instance 1: Kafka → Redis → Shards 0-2 → 18K clients ✓
  Instance 2: Kafka → Redis → Shards 3-5 → 18K clients ✓

  All instances share Redis → 100% message delivery
```

### Solution Architecture

**Approach**: Implement Redis Pub/Sub with interface abstraction enabling zero-code-change migration to NATS.

**Key Principles**:
1. **Interface-First Design**: `BroadcastBus` interface abstracts all implementations
2. **Configuration-Based Switching**: `BROADCAST_BUS_TYPE` env var selects implementation
3. **No Code Changes**: Swap Redis ↔ NATS ↔ InMemory by changing config only
4. **Backward Compatible**: Existing single-instance deployments continue working
5. **Production-Ready**: Health checks, metrics, error handling, reconnection logic

### Why Redis First (with NATS Migration Path)

| Criteria | Redis Pub/Sub | NATS | Decision |
|----------|---------------|------|----------|
| **Familiarity** | Very high | Lower | Redis ⭐ |
| **Operational Risk** | Lower (team expertise) | Higher (new tech) | Redis ⭐ |
| **Deployment Speed** | Faster (existing knowledge) | Slower (learning curve) | Redis ⭐ |
| **Latency** | 1-2ms p99 | <0.5ms p99 | NATS ⭐ |
| **Throughput** | 1M msg/sec | 10M msg/sec | NATS ⭐ |
| **Resource Usage** | 1.6GB (Sentinel HA) | 300MB (Cluster HA) | NATS ⭐ |
| **Long-term Performance** | Good | Excellent | NATS ⭐ |

**Strategy**:
- **Phase 1** (Now): Deploy Redis (faster to production, lower risk)
- **Phase 2** (Later): Migrate to NATS (better performance, when team ready)
- **Migration**: Zero code changes, config-only swap

---

## Architecture Decision Record

### ADR-001: Use Redis Pub/Sub Initially, Migrate to NATS Later

**Context**:
- Need external broadcast bus for multi-instance deployment
- Current throughput: 1K msg/sec, target: 10K msg/sec
- Latency SLA: <10ms p99 end-to-end
- Team has Redis experience, no NATS experience

**Decision**: Implement Redis Pub/Sub first, with interface abstraction for zero-code migration to NATS.

**Rationale**:

**Redis Advantages (Now)**:
1. **Team Familiarity**: Team has operational Redis experience
2. **Faster Deployment**: Can deploy in days vs weeks (no learning curve)
3. **Lower Risk**: Well-understood failure modes and recovery procedures
4. **Ecosystem**: Rich monitoring tools (RedisInsight, Grafana dashboards)
5. **Sufficient Performance**: 1M msg/sec >> 1K current load (1000× headroom)

**NATS Advantages (Later)**:
1. **Lower Latency**: <0.5ms vs 1-2ms (important as scale increases)
2. **Higher Throughput**: 10M msg/sec vs 1M msg/sec (10× better)
3. **Simpler HA**: 3-node cluster vs Redis Sentinel complexity
4. **Lower Resources**: 300MB vs 1.6GB RAM
5. **Purpose-Built**: Designed for messaging vs general-purpose cache

**Migration Path**:
```
Week 1-2:  Deploy Redis (production-ready)
Month 1-3: Operate at scale, learn patterns
Month 3-6: Evaluate NATS, test in shadow mode
Month 6:   Migrate to NATS (config change only)
```

**Consequences**:
- **Positive**: Fast time-to-production, low risk, team confidence
- **Negative**: Not optimal long-term performance (but acceptable for current scale)
- **Mitigating**: Zero-code migration path means easy upgrade when ready

### ADR-002: Use BroadcastBus Interface for All Implementations

**Context**: Need to support 3 implementations (InMemory, Redis, NATS) without code changes.

**Decision**: Define `BroadcastBus` interface with factory pattern.

**Rationale**:
- **Abstraction**: Hide implementation details behind interface
- **Flexibility**: Swap implementations via configuration
- **Testability**: Mock implementations for unit tests
- **Backward Compatibility**: Existing InMemory implementation unchanged

**Interface Design**:
```go
type BroadcastBus interface {
    Run()                                        // Start bus
    Shutdown()                                   // Stop bus
    Publish(msg *BroadcastMessage)               // Send message
    Subscribe() chan *BroadcastMessage           // Receive messages
    IsHealthy() bool                             // Health check
}
```

### ADR-003: Deploy Redis on Separate GCP Instance

**Context**: Redis can run on ws-server instances or separate VM.

**Decision**: Deploy Redis on dedicated e2-standard-2 instance.

**Rationale**:

**Separate Instance** ✓:
- Failure isolation (Redis crash ≠ ws-server crash)
- Independent scaling (scale Redis ≠ scale ws-server)
- Clear resource allocation (no CPU/memory contention)
- Easier monitoring (dedicated metrics)

**Co-located** ✗:
- Lower latency (~0.1ms saved) - negligible vs 10ms SLA
- Cost savings (~$50/month) - not worth operational risk
- Resource contention risk (Redis memory spike → ws-server OOM)

**Configuration**:
```
Instance Type: e2-standard-2 (2 vCPU, 8GB RAM)
Region: us-central1-a (same as ws-server)
Network: Internal VPC (no public IP)
Cost: ~$50/month
```

### ADR-004: Use Redis Sentinel for High Availability

**Context**: Redis deployment options: single-node, Sentinel, Cluster.

**Decision**: Deploy Redis Sentinel (1 Master + 2 Replicas + 3 Sentinels).

**Rationale**:

**Redis Sentinel** ✓:
- Automatic failover (5-15s recovery)
- Good for <1M msg/sec workload
- Simpler than Redis Cluster
- Well-tested in production

**Single Node** ✗:
- No HA (single point of failure)
- Unacceptable for production

**Redis Cluster** ✗:
- Overkill for Pub/Sub (designed for sharding)
- More complex operations
- Pub/Sub doesn't benefit from sharding

**Failover Time**:
- Detection: 5s (Sentinel polling interval)
- Election: 1-2s (Sentinel quorum vote)
- Promotion: 1-2s (Replica → Master)
- **Total**: 7-10s downtime (acceptable)

### ADR-005: Use Environment Variables for Configuration Switching

**Context**: How to enable zero-code-change switching between implementations?

**Decision**: Use `BROADCAST_BUS_TYPE` environment variable with factory pattern.

**Rationale**:
- **Simple**: Single config change to swap implementations
- **Explicit**: Clear which implementation is active
- **Testable**: Easy to test all implementations locally
- **Deployable**: Standard GCP deployment pattern

**Configuration**:
```bash
# In-memory (single instance, default)
BROADCAST_BUS_TYPE=inmemory

# Redis (multi-instance)
BROADCAST_BUS_TYPE=redis
REDIS_URL=redis://redis-master:6379

# NATS (future migration)
BROADCAST_BUS_TYPE=nats
NATS_URL=nats://nats-1:4222,nats://nats-2:4222
```

**Validation**:
- Startup: Log active implementation type
- Health endpoint: Report current bus type
- Metrics: Label by bus type

---

## Interface Design

### Core BroadcastBus Interface

```go
// File: ws/internal/multi/broadcast_interface.go
package multi

import "context"

// BroadcastBus defines the interface for inter-shard message broadcasting.
// Implementations: InMemoryBroadcastBus (single instance), RedisBroadcastBus, NatsBroadcastBus.
type BroadcastBus interface {
	// Run starts the broadcast bus's main loop.
	// For in-memory: starts goroutine to fan out messages.
	// For Redis/NATS: may be no-op if client handles internally.
	Run()

	// Shutdown gracefully stops the broadcast bus.
	// Drains pending messages, closes connections, cleans up resources.
	Shutdown()

	// Publish sends a message to all subscribers across all instances.
	// Non-blocking: drops message if bus is full (backpressure).
	Publish(msg *BroadcastMessage)

	// Subscribe returns a channel for receiving broadcast messages.
	// Each shard calls this once to get its subscription channel.
	// Channel is buffered (1024+ capacity) to handle bursts.
	Subscribe() chan *BroadcastMessage

	// IsHealthy returns true if the broadcast bus is operational.
	// For in-memory: always true (unless shutdown).
	// For Redis/NATS: checks connection status, recent publish success.
	IsHealthy() bool
}

// BroadcastMessage is the payload sent through the broadcast bus.
type BroadcastMessage struct {
	Subject string `json:"subject"` // e.g., "odin.token.BTC.trade"
	Message []byte `json:"message"` // Raw message bytes (pre-serialized)
}
```

### Why This Interface Works for All Implementations

**Design Principles**:

1. **Simple Surface Area**: Only 5 methods (Run, Shutdown, Publish, Subscribe, IsHealthy)
2. **Implementation-Agnostic**: No Redis/NATS-specific methods
3. **Channel-Based**: Uses Go idioms (familiar to team)
4. **Health-Aware**: Supports circuit breakers and monitoring
5. **Non-Blocking**: Publish doesn't wait (matches Pub/Sub semantics)

**Compatibility Matrix**:

| Method | InMemory | Redis Pub/Sub | NATS Core | NATS JetStream |
|--------|----------|---------------|-----------|----------------|
| `Run()` | Starts goroutine | No-op | No-op | No-op |
| `Shutdown()` | Stops goroutine, closes channels | Closes Redis conn | Drains, closes conn | Drains, closes conn |
| `Publish()` | Send to channel | PUBLISH command | Publish() | Publish() |
| `Subscribe()` | Return channel | SUBSCRIBE + goroutine | Subscribe + goroutine | Subscribe + goroutine |
| `IsHealthy()` | Always true | PING check | IsConnected() | IsConnected() |

**Trade-offs**:

✅ **Pros**:
- Works identically across all implementations
- Easy to test (mock with simple struct)
- Clear ownership (bus owns pub/sub logic)
- Go-idiomatic (channels everywhere)

⚠️ **Cons**:
- Channel overhead (but negligible: <1μs per send)
- Not zero-copy (message copied into channel)
- Backpressure = drop (acceptable for real-time data)

### Extended Interface (Optional, for Metrics)

```go
// BroadcastBusMetrics extends BroadcastBus with metrics exposure.
// Not required by core logic, used for monitoring/debugging.
type BroadcastBusMetrics interface {
	BroadcastBus

	// GetMetrics returns current metrics snapshot.
	GetMetrics() BusMetrics
}

type BusMetrics struct {
	Type              string    // "inmemory", "redis", "nats"
	MessagesPublished uint64    // Total messages published
	MessagesReceived  uint64    // Total messages received
	PublishErrors     uint64    // Failed publishes
	SubscribeErrors   uint64    // Failed receives
	LastPublishTime   time.Time // Last successful publish
	LastError         error     // Most recent error
	IsConnected       bool      // Connection status (Redis/NATS)
}
```

---

## Redis Deployment Architecture

### Recommended Deployment: Redis Sentinel (1M + 2R + 3S)

```
┌─────────────────────────────────────────────────────────────┐
│                    GCP us-central1-a                         │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  e2-standard-2 (Redis Server)                          │ │
│  │  - 2 vCPU, 8GB RAM                                     │ │
│  │  - Internal IP: 10.128.0.5                             │ │
│  │                                                        │ │
│  │  ┌─────────────┐  ┌───────────────┐  ┌──────────────┐│ │
│  │  │   Master    │  │  Replica-1    │  │  Replica-2   ││ │
│  │  │  (Port 6379)│─▶│  (Port 6380)  │  │  (Port 6381) ││ │
│  │  │             │  │               │  │              ││ │
│  │  │  RW Traffic │  │  Async Repl   │  │  Async Repl  ││ │
│  │  └─────────────┘  └───────────────┘  └──────────────┘│ │
│  │         ▲                                              │ │
│  │         │ (monitors)                                   │ │
│  │  ┌──────┴─────────┬─────────────────┬─────────────┐  │ │
│  │  │  Sentinel-1    │  Sentinel-2     │  Sentinel-3 │  │ │
│  │  │  (Port 26379)  │  (Port 26380)   │  (Port 26381)│  │ │
│  │  │                │                 │             │  │ │
│  │  │  Quorum: 2/3 for failover        │             │  │ │
│  │  └────────────────┴─────────────────┴─────────────┘  │ │
│  └────────────────────────────────────────────────────────┘ │
│                         ▲                                   │
│                         │ (Pub/Sub)                         │
│         ┌───────────────┴────────────────┐                 │
│         │                                │                 │
│  ┌──────┴──────┐                  ┌──────┴──────┐          │
│  │ ws-server-1 │                  │ ws-server-2 │          │
│  │ e2-highcpu-8│                  │ e2-highcpu-8│          │
│  │ 3 shards    │                  │ 3 shards    │          │
│  │ 18K conns   │                  │ 18K conns   │          │
│  └─────────────┘                  └─────────────┘          │
└─────────────────────────────────────────────────────────────┘
```

### Instance Specifications

**Redis Server Instance: e2-standard-2**
```
CPU:              2 vCPU (shared, burstable)
Memory:           8 GB
Disk:             50 GB SSD (persistent, for AOF)
Network:          Up to 10 Gbps
Cost:             ~$50/month (us-central1)
OS:               Ubuntu 22.04 LTS

Rationale:
- 8GB RAM: Comfortable for 512MB Master + 512MB Replicas + OS overhead
- 2 vCPU: Sufficient for 1K msg/sec (<10% CPU expected)
- SSD: Fast AOF writes for durability
- Burstable: Handles traffic spikes without over-provisioning
```

**Resource Allocation**:
```
Master Redis:     512MB RAM limit (maxmemory policy: volatile-lru)
Replica-1:        512MB RAM limit
Replica-2:        512MB RAM limit
Sentinel-1:       50MB RAM
Sentinel-2:       50MB RAM
Sentinel-3:       50MB RAM
OS + overhead:    ~6.5GB
───────────────────────────────────────
Total:            ~7.7GB / 8GB (96% utilization)

CPU Distribution (under load):
Master:           0.5-1.0 cores (Pub/Sub + persistence)
Replicas:         0.2-0.4 cores each (replication + reads)
Sentinels:        <0.1 cores total (monitoring only)
───────────────────────────────────────
Total:            1.0-1.8 cores / 2 cores (50-90% utilization)
```

### Redis Configuration

**File: `/etc/redis/redis-master.conf`**
```conf
# Network
bind 0.0.0.0
port 6379
protected-mode yes
requirepass <REDIS_PASSWORD>  # Change this!

# Memory
maxmemory 512mb
maxmemory-policy volatile-lru  # Evict keys with TTL when full

# Persistence (AOF for durability)
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec           # Good balance: performance + durability
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb

# Replication (for replicas to connect)
masterauth <REDIS_PASSWORD>

# Pub/Sub specific
client-output-buffer-limit pubsub 32mb 8mb 60
# Disconnect clients if:
# - Hard limit: 32MB buffered
# - Soft limit: 8MB for 60 seconds

# Logging
loglevel notice
logfile /var/log/redis/redis-master.log

# Snapshotting (disabled, AOF is primary)
save ""  # Disable RDB snapshots

# Performance
tcp-backlog 511
timeout 300
tcp-keepalive 300
```

**File: `/etc/redis/redis-replica-1.conf`**
```conf
# Inherit master config
include /etc/redis/redis-master.conf

# Override port
port 6380

# Replication
replicaof 127.0.0.1 6379
replica-read-only yes

# Logging
logfile /var/log/redis/redis-replica-1.log
```

**File: `/etc/redis/sentinel.conf`**
```conf
port 26379
dir /var/lib/redis/sentinel

# Monitor master
sentinel monitor mymaster 127.0.0.1 6379 2  # Quorum: 2/3 Sentinels
sentinel auth-pass mymaster <REDIS_PASSWORD>

# Timeouts
sentinel down-after-milliseconds mymaster 5000   # 5s to detect failure
sentinel parallel-syncs mymaster 1               # 1 replica syncs at a time
sentinel failover-timeout mymaster 10000         # 10s max failover time

# Logging
sentinel logfile /var/log/redis/sentinel-1.log
```

### Deployment Steps (GCP)

**Step 1: Provision Instance**
```bash
# Create e2-standard-2 instance
gcloud compute instances create redis-server \
  --machine-type=e2-standard-2 \
  --zone=us-central1-a \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-ssd \
  --tags=redis-server \
  --no-address  # No public IP, internal only

# Create firewall rule (internal VPC only)
gcloud compute firewall-rules create allow-redis-internal \
  --allow=tcp:6379,tcp:6380,tcp:6381,tcp:26379,tcp:26380,tcp:26381 \
  --source-ranges=10.128.0.0/20 \
  --target-tags=redis-server \
  --description="Allow Redis traffic from internal VPC"
```

**Step 2: Install Redis**
```bash
# SSH to instance
gcloud compute ssh redis-server --zone=us-central1-a

# Install Redis 7.x (latest stable)
sudo apt update
sudo apt install -y redis-server=6:7.0.12-1rl1~jammy1 redis-tools

# Verify version
redis-server --version
# Expected: Redis server v=7.0.12
```

**Step 3: Configure Redis Processes**
```bash
# Create directories
sudo mkdir -p /etc/redis /var/lib/redis/sentinel /var/log/redis
sudo chown -R redis:redis /var/lib/redis /var/log/redis

# Generate password (secure random)
REDIS_PASSWORD=$(openssl rand -base64 32)
echo "REDIS_PASSWORD=$REDIS_PASSWORD" | sudo tee /etc/redis/password.txt
sudo chmod 600 /etc/redis/password.txt

# Copy configs from above to /etc/redis/
sudo nano /etc/redis/redis-master.conf
# (paste master config, replace <REDIS_PASSWORD>)

sudo nano /etc/redis/redis-replica-1.conf
# (paste replica-1 config, replace <REDIS_PASSWORD>)

sudo nano /etc/redis/redis-replica-2.conf
# (same as replica-1, but port=6381, logfile=redis-replica-2.log)

sudo nano /etc/redis/sentinel-1.conf
# (paste sentinel config, replace <REDIS_PASSWORD>)

# Copy sentinel configs for sentinel-2, sentinel-3
# (same config, but port=26380, port=26381, different logfile)
```

**Step 4: Create systemd Services**
```bash
# Master service
sudo tee /etc/systemd/system/redis-master.service <<'EOF'
[Unit]
Description=Redis Master
After=network.target

[Service]
Type=notify
User=redis
Group=redis
ExecStart=/usr/bin/redis-server /etc/redis/redis-master.conf
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Replica-1 service (similar structure)
sudo tee /etc/systemd/system/redis-replica-1.service <<'EOF'
[Unit]
Description=Redis Replica 1
After=network.target redis-master.service

[Service]
Type=notify
User=redis
Group=redis
ExecStart=/usr/bin/redis-server /etc/redis/redis-replica-1.conf
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Replica-2 service (same as replica-1, different config file)
# ... (similar)

# Sentinel services (×3)
sudo tee /etc/systemd/system/redis-sentinel-1.service <<'EOF'
[Unit]
Description=Redis Sentinel 1
After=network.target redis-master.service

[Service]
Type=notify
User=redis
Group=redis
ExecStart=/usr/bin/redis-server /etc/redis/sentinel-1.conf --sentinel
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Sentinel-2, Sentinel-3 (similar)
```

**Step 5: Start Services**
```bash
# Reload systemd
sudo systemctl daemon-reload

# Start master
sudo systemctl enable redis-master
sudo systemctl start redis-master

# Start replicas
sudo systemctl enable redis-replica-1 redis-replica-2
sudo systemctl start redis-replica-1 redis-replica-2

# Verify replication
redis-cli -p 6379 -a "$REDIS_PASSWORD" INFO replication
# Expected:
# role:master
# connected_slaves:2
# slave0:ip=127.0.0.1,port=6380,state=online
# slave1:ip=127.0.0.1,port=6381,state=online

# Start sentinels
sudo systemctl enable redis-sentinel-{1,2,3}
sudo systemctl start redis-sentinel-{1,2,3}

# Verify sentinel status
redis-cli -p 26379 SENTINEL masters
# Expected: master name, IP, status=ok, slaves=2, sentinels=3
```

**Step 6: Test Failover**
```bash
# Simulate master failure
sudo systemctl stop redis-master

# Watch sentinels detect and failover
redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
# Expected: Will switch from 6379 to 6380 or 6381 within 10 seconds

# Verify new master
redis-cli -p 6380 -a "$REDIS_PASSWORD" INFO replication
# Expected: role:master

# Restart original master (becomes replica)
sudo systemctl start redis-master
redis-cli -p 6379 -a "$REDIS_PASSWORD" INFO replication
# Expected: role:slave
```

### Connection Configuration (ws-server)

**Environment Variables**:
```bash
# For ws-server instances
BROADCAST_BUS_TYPE=redis

# Redis Sentinel connection (client auto-discovers master)
REDIS_SENTINEL_ADDRS=10.128.0.5:26379,10.128.0.5:26380,10.128.0.5:26381
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<from /etc/redis/password.txt>

# Connection pool settings
REDIS_POOL_SIZE=10        # Max connections
REDIS_MIN_IDLE_CONNS=5    # Keep-alive connections
REDIS_DIAL_TIMEOUT=5s
REDIS_READ_TIMEOUT=3s
REDIS_WRITE_TIMEOUT=3s
```

### Monitoring Setup

**Prometheus Exporter**:
```bash
# Install redis_exporter on Redis instance
wget https://github.com/oliver006/redis_exporter/releases/download/v1.55.0/redis_exporter-v1.55.0.linux-amd64.tar.gz
tar xzf redis_exporter-v1.55.0.linux-amd64.tar.gz
sudo mv redis_exporter-v1.55.0.linux-amd64/redis_exporter /usr/local/bin/

# Create systemd service
sudo tee /etc/systemd/system/redis_exporter.service <<'EOF'
[Unit]
Description=Redis Exporter
After=network.target

[Service]
Type=simple
User=prometheus
ExecStart=/usr/local/bin/redis_exporter \
  --redis.addr=redis://localhost:6379 \
  --redis.password-file=/etc/redis/password.txt

Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable redis_exporter
sudo systemctl start redis_exporter

# Verify metrics
curl http://localhost:9121/metrics | grep redis_up
# Expected: redis_up 1
```

**Prometheus Scrape Config**:
```yaml
# Add to prometheus.yml
scrape_configs:
  - job_name: 'redis'
    static_configs:
      - targets: ['redis-server.c.project.internal:9121']
    scrape_interval: 15s
```

---

## Configuration Strategy

### Environment Variable Design

**Goal**: Zero-code-change switching between implementations.

**Configuration Schema**:
```bash
# ========================================
# BROADCAST BUS CONFIGURATION
# ========================================

# Bus type: "inmemory" (single instance), "redis" (multi-instance), "nats" (future)
BROADCAST_BUS_TYPE=redis

# ========================================
# REDIS CONFIGURATION (when BUS_TYPE=redis)
# ========================================

# Sentinel connection (recommended for HA)
REDIS_SENTINEL_ADDRS=10.128.0.5:26379,10.128.0.5:26380,10.128.0.5:26381
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<password>

# OR: Direct connection (single-node, dev only)
# REDIS_URL=redis://:password@10.128.0.5:6379/0

# Pub/Sub channel name
REDIS_CHANNEL=ws:broadcast

# Connection pool
REDIS_POOL_SIZE=10          # Max connections (1 per shard + overhead)
REDIS_MIN_IDLE_CONNS=5      # Keep warm connections
REDIS_MAX_RETRIES=3         # Retry failed commands
REDIS_DIAL_TIMEOUT=5s       # Connection timeout
REDIS_READ_TIMEOUT=3s       # Read timeout
REDIS_WRITE_TIMEOUT=3s      # Write timeout

# Reconnection settings
REDIS_RECONNECT_DELAY=1s    # Wait between reconnect attempts
REDIS_MAX_RECONNECTS=-1     # -1 = infinite reconnects

# ========================================
# NATS CONFIGURATION (when BUS_TYPE=nats)
# ========================================

# NATS cluster URLs
NATS_URL=nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222

# NATS credentials (for managed NATS like Synadia Cloud)
NATS_CREDS=/path/to/credentials.creds

# Pub/Sub subject
NATS_SUBJECT=ws.broadcast

# Reconnection settings
NATS_MAX_RECONNECTS=-1       # -1 = infinite
NATS_RECONNECT_WAIT=1s
NATS_PING_INTERVAL=20s

# ========================================
# INMEMORY CONFIGURATION (when BUS_TYPE=inmemory)
# ========================================

# Buffer size for in-memory channel
BROADCAST_BUS_BUFFER_SIZE=1024

# Batching (performance optimization)
BROADCAST_BUS_BATCH_SIZE=100  # 0 = disabled
```

### Factory Pattern Implementation

**File: `ws/internal/multi/broadcast_factory.go`**
```go
package multi

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// BroadcastBusConfig holds configuration for creating a BroadcastBus.
type BroadcastBusConfig struct {
	// Type: "inmemory", "redis", "nats"
	Type string

	// InMemory config
	BufferSize int
	BatchSize  int

	// Redis config
	RedisSentinelAddrs []string
	RedisMasterName    string
	RedisPassword      string
	RedisURL           string  // Alternative to Sentinel (single-node)
	RedisChannel       string
	RedisPoolSize      int
	RedisMinIdleConns  int
	RedisDialTimeout   time.Duration
	RedisReadTimeout   time.Duration
	RedisWriteTimeout  time.Duration

	// NATS config
	NatsURL     string
	NatsCreds   string
	NatsSubject string

	// Logger
	Logger zerolog.Logger
}

// NewBroadcastBus creates a BroadcastBus based on config.Type.
// Returns error if type is unknown or configuration is invalid.
func NewBroadcastBus(cfg BroadcastBusConfig) (BroadcastBus, error) {
	switch cfg.Type {
	case "inmemory", "":
		return NewInMemoryBroadcastBus(cfg.BufferSize, cfg.Logger), nil

	case "redis":
		return NewRedisBroadcastBus(RedisBroadcastBusConfig{
			SentinelAddrs:  cfg.RedisSentinelAddrs,
			MasterName:     cfg.RedisMasterName,
			Password:       cfg.RedisPassword,
			DirectURL:      cfg.RedisURL,
			Channel:        cfg.RedisChannel,
			PoolSize:       cfg.RedisPoolSize,
			MinIdleConns:   cfg.RedisMinIdleConns,
			DialTimeout:    cfg.RedisDialTimeout,
			ReadTimeout:    cfg.RedisReadTimeout,
			WriteTimeout:   cfg.RedisWriteTimeout,
			Logger:         cfg.Logger,
		})

	case "nats":
		return NewNatsBroadcastBus(NatsBroadcastBusConfig{
			URL:     cfg.NatsURL,
			Creds:   cfg.NatsCreds,
			Subject: cfg.NatsSubject,
			Logger:  cfg.Logger,
		})

	default:
		return nil, fmt.Errorf("unknown broadcast bus type: %s (valid: inmemory, redis, nats)", cfg.Type)
	}
}
```

### Configuration Loading

**File: `ws/internal/shared/platform/config.go` (additions)**
```go
// Add to existing ServerConfig struct
type Config struct {
	// ... existing fields ...

	// Broadcast Bus Configuration
	BroadcastBusType         string        `env:"BROADCAST_BUS_TYPE" default:"inmemory"`
	BroadcastBusBufferSize   int           `env:"BROADCAST_BUS_BUFFER_SIZE" default:"1024"`
	BroadcastBusBatchSize    int           `env:"BROADCAST_BUS_BATCH_SIZE" default:"100"`

	// Redis Configuration
	RedisSentinelAddrs       string        `env:"REDIS_SENTINEL_ADDRS"`  // Comma-separated
	RedisMasterName          string        `env:"REDIS_MASTER_NAME" default:"mymaster"`
	RedisPassword            string        `env:"REDIS_PASSWORD"`
	RedisURL                 string        `env:"REDIS_URL"`  // Alternative to Sentinel
	RedisChannel             string        `env:"REDIS_CHANNEL" default:"ws:broadcast"`
	RedisPoolSize            int           `env:"REDIS_POOL_SIZE" default:"10"`
	RedisMinIdleConns        int           `env:"REDIS_MIN_IDLE_CONNS" default:"5"`
	RedisDialTimeout         time.Duration `env:"REDIS_DIAL_TIMEOUT" default:"5s"`
	RedisReadTimeout         time.Duration `env:"REDIS_READ_TIMEOUT" default:"3s"`
	RedisWriteTimeout        time.Duration `env:"REDIS_WRITE_TIMEOUT" default:"3s"`

	// NATS Configuration
	NatsURL                  string        `env:"NATS_URL"`
	NatsCreds                string        `env:"NATS_CREDS"`
	NatsSubject              string        `env:"NATS_SUBJECT" default:"ws.broadcast"`
}

// Helper to parse comma-separated addresses
func (c *Config) GetRedisSentinelAddrs() []string {
	if c.RedisSentinelAddrs == "" {
		return nil
	}
	addrs := strings.Split(c.RedisSentinelAddrs, ",")
	for i := range addrs {
		addrs[i] = strings.TrimSpace(addrs[i])
	}
	return addrs
}
```

### Main Entrypoint Integration

**File: `ws/cmd/multi/main.go` (modifications)**
```go
func main() {
	// ... existing config loading ...

	// Create BroadcastBus based on configuration
	logger.Printf("Creating BroadcastBus (type: %s)", cfg.BroadcastBusType)

	busConfig := multi.BroadcastBusConfig{
		Type:               cfg.BroadcastBusType,
		BufferSize:         cfg.BroadcastBusBufferSize,
		BatchSize:          cfg.BroadcastBusBatchSize,
		RedisSentinelAddrs: cfg.GetRedisSentinelAddrs(),
		RedisMasterName:    cfg.RedisMasterName,
		RedisPassword:      cfg.RedisPassword,
		RedisURL:           cfg.RedisURL,
		RedisChannel:       cfg.RedisChannel,
		RedisPoolSize:      cfg.RedisPoolSize,
		RedisMinIdleConns:  cfg.RedisMinIdleConns,
		RedisDialTimeout:   cfg.RedisDialTimeout,
		RedisReadTimeout:   cfg.RedisReadTimeout,
		RedisWriteTimeout:  cfg.RedisWriteTimeout,
		NatsURL:            cfg.NatsURL,
		NatsCreds:          cfg.NatsCreds,
		NatsSubject:        cfg.NatsSubject,
		Logger:             busLogger,
	}

	broadcastBus, err := multi.NewBroadcastBus(busConfig)
	if err != nil {
		logger.Fatalf("Failed to create BroadcastBus: %v", err)
	}
	defer broadcastBus.Shutdown()

	// Verify health before starting
	if !broadcastBus.IsHealthy() {
		logger.Fatalf("BroadcastBus unhealthy on startup")
	}

	broadcastBus.Run()
	logger.Printf("BroadcastBus started successfully (type: %s)", cfg.BroadcastBusType)

	// ... rest of initialization ...
}
```

### Health Check Endpoint

**File: `ws/internal/shared/handlers_http.go` (additions)**
```go
type HealthResponse struct {
	Status        string            `json:"status"`
	BroadcastBus  BroadcastBusHealth `json:"broadcast_bus"`
	// ... existing fields ...
}

type BroadcastBusHealth struct {
	Type      string `json:"type"`      // "inmemory", "redis", "nats"
	Healthy   bool   `json:"healthy"`   // Overall health
	Connected bool   `json:"connected"` // For Redis/NATS
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// ... existing health checks ...

	// Add broadcast bus health
	busHealth := BroadcastBusHealth{
		Type:    s.config.BroadcastBusType,
		Healthy: s.broadcastBus.IsHealthy(),
	}

	// For Redis/NATS, add connection status
	if busHealth.Type == "redis" || busHealth.Type == "nats" {
		busHealth.Connected = s.broadcastBus.IsHealthy()
	} else {
		busHealth.Connected = true  // In-memory always "connected"
	}

	response := HealthResponse{
		Status:       "healthy",
		BroadcastBus: busHealth,
		// ... existing fields ...
	}

	// ... return response ...
}
```

---

## Implementation Plan

### Phase 1: Interface & In-Memory Refactoring (Week 1)

**Goal**: Refactor existing code to use BroadcastBus interface without changing behavior.

**Tasks**:

1. **Create Interface** (1 day)
   - `ws/internal/multi/broadcast_interface.go`: Define `BroadcastBus` interface
   - Add `BroadcastMessage` struct (already exists, formalize)
   - Document interface contract

2. **Refactor Existing BroadcastBus** (1 day)
   - Rename `ws/internal/multi/broadcast.go` → `broadcast_inmemory.go`
   - Rename `BroadcastBus` struct → `InMemoryBroadcastBus`
   - Implement `IsHealthy()` method (return `true` if not shutdown)
   - Add interface assertion: `var _ BroadcastBus = (*InMemoryBroadcastBus)(nil)`

3. **Create Factory** (1 day)
   - `ws/internal/multi/broadcast_factory.go`: Implement `NewBroadcastBus(config)`
   - Support `type=inmemory` only initially
   - Add comprehensive tests

4. **Update Main Entrypoint** (1 day)
   - `ws/cmd/multi/main.go`: Use factory instead of direct instantiation
   - Add `BROADCAST_BUS_TYPE` env var (default: `inmemory`)
   - Test backward compatibility

5. **Testing** (1 day)
   - Unit tests: Interface compliance
   - Integration tests: Single-instance still works
   - Load test: No performance regression

**Success Criteria**:
- ✅ All existing tests pass
- ✅ Single-instance deployment works identically
- ✅ `BROADCAST_BUS_TYPE=inmemory` explicit config works
- ✅ Interface documented with examples

**Deliverables**:
```
ws/internal/multi/
  broadcast_interface.go     (NEW: BroadcastBus interface)
  broadcast_inmemory.go      (RENAMED: existing implementation)
  broadcast_factory.go       (NEW: factory pattern)
  broadcast_inmemory_test.go (NEW: unit tests)
  broadcast_factory_test.go  (NEW: factory tests)

ws/cmd/multi/main.go
  (MODIFIED: use factory)

ws/internal/shared/platform/config.go
  (MODIFIED: add BROADCAST_BUS_TYPE)
```

### Phase 2: Redis Implementation (Week 2)

**Goal**: Implement `RedisBroadcastBus` with Sentinel support.

**Tasks**:

1. **Add Go Dependencies** (1 hour)
   ```bash
   go get github.com/redis/go-redis/v9@latest
   # Expected: v9.4.0 or later
   ```

2. **Implement RedisBroadcastBus** (2 days)
   - File: `ws/internal/multi/broadcast_redis.go`
   - Connection: Failover client with Sentinel support
   - Publish: `PUBLISH channel message`
   - Subscribe: `SUBSCRIBE channel` with goroutine feeding Go channel
   - Reconnection: Auto-retry with exponential backoff
   - Health: PING check + track last successful publish

3. **Add Configuration** (1 day)
   - Update `platform.Config` with Redis fields
   - Validation: Sentinel addrs OR direct URL (not both)
   - Defaults: Channel name, pool size, timeouts

4. **Unit Tests** (1 day)
   - Mock Redis with `miniredis`
   - Test: Publish/Subscribe flow
   - Test: Reconnection on disconnect
   - Test: Sentinel failover simulation

5. **Integration Tests** (1 day)
   - Docker Compose: Redis + Sentinel
   - Test: Message delivery across 2 ws-server instances
   - Test: Failover scenario (kill master)

**Code Structure** (see detailed implementation in next section)

**Success Criteria**:
- ✅ RedisBroadcastBus implements BroadcastBus interface
- ✅ Connects to Redis Sentinel successfully
- ✅ Messages published by Instance 1 received by Instance 2
- ✅ Handles Redis master failover with <10s recovery
- ✅ Unit tests >80% coverage

**Deliverables**:
```
ws/internal/multi/
  broadcast_redis.go       (NEW: Redis implementation)
  broadcast_redis_test.go  (NEW: unit tests)

ws/internal/shared/platform/config.go
  (MODIFIED: add Redis config fields)

go.mod
  (MODIFIED: add redis/go-redis/v9)

docker-compose.redis-test.yml  (NEW: testing infrastructure)
```

### Phase 3: Testing & Validation (Week 3)

**Goal**: Validate Redis implementation in isolated environment.

**Tasks**:

1. **Shadow Mode Testing** (2 days)
   - Dual-bus implementation: Publish to both in-memory AND Redis
   - Subscribe from in-memory only (production traffic unchanged)
   - Compare message counts: in-memory == Redis (100% consistency)
   - Metrics: Latency comparison, error rates

2. **Local Multi-Instance Testing** (1 day)
   - Docker Compose: Redis + 2 ws-server instances
   - Load test: 1000 connections per instance
   - Verify: All clients receive all messages
   - Metrics: Latency, throughput, error rate

3. **Failure Scenario Testing** (1 day)
   - Simulate Redis master crash during traffic
   - Verify: Sentinel failover completes <10s
   - Verify: No message loss during failover
   - Verify: Clients stay connected (ws-server reconnects transparently)

4. **Performance Benchmarking** (1 day)
   - Baseline: In-memory latency
   - Redis: End-to-end latency with Redis
   - Target: <2ms added latency (total <10ms p99)
   - Load: 1K msg/sec sustained

**Success Criteria**:
- ✅ Shadow mode: 100% message consistency
- ✅ Multi-instance: All clients receive all messages
- ✅ Failover: <10s recovery, zero message loss
- ✅ Performance: <10ms p99 end-to-end latency

**Deliverables**:
```
scripts/test/
  shadow-mode-test.sh       (NEW: shadow mode testing script)
  multi-instance-test.sh    (NEW: multi-instance testing)
  failover-test.sh          (NEW: failover simulation)
  benchmark-redis.sh        (NEW: performance benchmarks)

docs/testing/
  REDIS_TEST_RESULTS.md     (NEW: test results documentation)
```

### Phase 4: GCP Deployment (Week 4)

**Goal**: Deploy Redis Sentinel to GCP and configure ws-servers.

**Tasks**:

1. **Provision Redis Instance** (1 day)
   - Follow deployment steps in "Redis Deployment Architecture" section
   - Instance type: e2-standard-2
   - Configuration: 1 Master + 2 Replicas + 3 Sentinels
   - Testing: Verify failover works

2. **Update ws-server Configuration** (1 day)
   - Environment variables for staging instances
   - `BROADCAST_BUS_TYPE=redis`
   - Sentinel addresses: Internal IPs
   - Deploy to staging, verify health

3. **Gradual Rollout** (2 days)
   - Day 1: 1 ws-server instance on Redis, 1 on in-memory
   - Verify: Cross-instance message delivery
   - Day 2: Both instances on Redis
   - Monitor for 24 hours

4. **Production Deployment** (1 day)
   - Update production config
   - Rolling restart: Zero downtime
   - Monitor: Latency, error rates, failover readiness

**Success Criteria**:
- ✅ Redis Sentinel running on GCP
- ✅ Both staging instances using Redis
- ✅ Latency <10ms p99
- ✅ Zero errors for 24 hours
- ✅ Production deployed with zero downtime

**Deliverables**:
```
deployments/v1/gcp/redis/
  terraform/               (NEW: Infrastructure as Code)
  redis-master.conf        (NEW: Redis config)
  sentinel.conf            (NEW: Sentinel config)
  deploy.sh                (NEW: deployment script)

.env.staging
  BROADCAST_BUS_TYPE=redis
  REDIS_SENTINEL_ADDRS=...

docs/deployment/
  REDIS_DEPLOYMENT.md      (NEW: deployment runbook)
```

### Phase 5: Monitoring & Documentation (Week 5)

**Goal**: Production-grade monitoring and comprehensive documentation.

**Tasks**:

1. **Prometheus Metrics** (1 day)
   - Redis exporter deployment
   - Custom metrics: `broadcast_bus_publish_latency`, etc.
   - Scrape configuration

2. **Grafana Dashboards** (1 day)
   - Redis health overview
   - Broadcast bus performance (latency, throughput)
   - Multi-instance comparison

3. **Alerting Rules** (1 day)
   - Redis connection down
   - High latency (>5ms p99)
   - High error rate (>1%)
   - Sentinel failover events

4. **Runbooks** (1 day)
   - Runbook: Redis connection issues
   - Runbook: High latency troubleshooting
   - Runbook: Rollback to in-memory bus
   - Runbook: Manual failover

5. **Documentation** (1 day)
   - Architecture overview
   - Configuration reference
   - Deployment guide
   - Migration guide (in-memory → Redis → NATS)

**Success Criteria**:
- ✅ All metrics collecting correctly
- ✅ Grafana dashboards deployed
- ✅ Alerts tested and firing correctly
- ✅ Runbooks validated by on-call team
- ✅ Documentation reviewed and approved

**Deliverables**:
```
grafana/dashboards/
  redis-broadcast-bus.json  (NEW)

prometheus/
  redis-alerts.yml          (NEW)

docs/
  REDIS_BROADCAST_BUS_ARCHITECTURE.md  (THIS DOCUMENT)
  runbooks/
    redis-connection-issues.md         (NEW)
    high-latency.md                    (NEW)
    rollback-to-inmemory.md            (NEW)
```

---

## Implementation: Redis BroadcastBus

### File: `ws/internal/multi/broadcast_redis.go`

```go
package multi

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// RedisBroadcastBusConfig holds configuration for Redis Pub/Sub.
type RedisBroadcastBusConfig struct {
	// Sentinel configuration (recommended for HA)
	SentinelAddrs []string // e.g., ["10.128.0.5:26379", "10.128.0.5:26380"]
	MasterName    string   // e.g., "mymaster"
	Password      string   // Redis password

	// Direct connection (alternative, single-node only)
	DirectURL string // e.g., "redis://:password@10.128.0.5:6379/0"

	// Pub/Sub channel name
	Channel string // e.g., "ws:broadcast"

	// Connection pool settings
	PoolSize      int           // Default: 10
	MinIdleConns  int           // Default: 5
	DialTimeout   time.Duration // Default: 5s
	ReadTimeout   time.Duration // Default: 3s
	WriteTimeout  time.Duration // Default: 3s

	Logger zerolog.Logger
}

// RedisBroadcastBus implements BroadcastBus using Redis Pub/Sub.
type RedisBroadcastBus struct {
	client  redis.UniversalClient // Supports both single-node and Sentinel
	pubsub  *redis.PubSub
	channel string
	logger  zerolog.Logger

	// Subscriber channels (one per shard)
	subscribers []chan *BroadcastMessage
	subMu       sync.RWMutex

	// Health tracking
	lastPublishSuccess atomic.Value // time.Time
	lastError          atomic.Value // error
	connected          atomic.Bool

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRedisBroadcastBus creates a new Redis-based BroadcastBus.
func NewRedisBroadcastBus(cfg RedisBroadcastBusConfig) (*RedisBroadcastBus, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Set defaults
	if cfg.Channel == "" {
		cfg.Channel = "ws:broadcast"
	}
	if cfg.PoolSize == 0 {
		cfg.PoolSize = 10
	}
	if cfg.MinIdleConns == 0 {
		cfg.MinIdleConns = 5
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 3 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 3 * time.Second
	}

	// Create client (Sentinel or single-node)
	var client redis.UniversalClient
	if len(cfg.SentinelAddrs) > 0 {
		// Sentinel mode (recommended for HA)
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.MasterName,
			SentinelAddrs: cfg.SentinelAddrs,
			Password:      cfg.Password,

			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,

			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			MaxRetries:   3,
		})

		cfg.Logger.Info().
			Strs("sentinel_addrs", cfg.SentinelAddrs).
			Str("master_name", cfg.MasterName).
			Msg("Connecting to Redis via Sentinel")
	} else if cfg.DirectURL != "" {
		// Direct connection (single-node)
		opt, err := redis.ParseURL(cfg.DirectURL)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("invalid Redis URL: %w", err)
		}

		opt.DialTimeout = cfg.DialTimeout
		opt.ReadTimeout = cfg.ReadTimeout
		opt.WriteTimeout = cfg.WriteTimeout
		opt.PoolSize = cfg.PoolSize
		opt.MinIdleConns = cfg.MinIdleConns

		client = redis.NewClient(opt)

		cfg.Logger.Info().
			Str("url", cfg.DirectURL).
			Msg("Connecting to Redis (direct connection)")
	} else {
		cancel()
		return nil, fmt.Errorf("must provide either SentinelAddrs or DirectURL")
	}

	// Test connection
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	bus := &RedisBroadcastBus{
		client:      client,
		channel:     cfg.Channel,
		logger:      cfg.Logger.With().Str("component", "redis_broadcast_bus").Logger(),
		subscribers: make([]chan *BroadcastMessage, 0),
		ctx:         ctx,
		cancel:      cancel,
	}

	bus.lastPublishSuccess.Store(time.Now())
	bus.connected.Store(true)

	bus.logger.Info().
		Str("channel", cfg.Channel).
		Msg("Redis BroadcastBus created successfully")

	return bus, nil
}

// Run starts the Redis Pub/Sub listener.
// Subscribes to the configured channel and distributes messages to subscriber channels.
func (r *RedisBroadcastBus) Run() {
	r.logger.Info().Msg("Starting Redis Pub/Sub listener")

	// Subscribe to Redis channel
	r.pubsub = r.client.Subscribe(r.ctx, r.channel)

	// Start listener goroutine
	r.wg.Add(1)
	go r.runListener()

	r.logger.Info().
		Str("channel", r.channel).
		Msg("Redis Pub/Sub listener started")
}

// runListener is the main loop that receives messages from Redis and distributes them.
func (r *RedisBroadcastBus) runListener() {
	defer r.wg.Done()
	defer r.pubsub.Close()

	ch := r.pubsub.Channel()

	for {
		select {
		case redisMsg := <-ch:
			if redisMsg == nil {
				r.logger.Warn().Msg("Redis Pub/Sub channel closed, exiting listener")
				r.connected.Store(false)
				return
			}

			// Deserialize message
			var msg BroadcastMessage
			if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
				r.logger.Warn().
					Err(err).
					Str("payload", redisMsg.Payload).
					Msg("Failed to unmarshal Redis message")
				continue
			}

			// Distribute to all subscribers
			r.fanOut(&msg)

		case <-r.ctx.Done():
			r.logger.Info().Msg("Redis listener shutting down")
			return
		}
	}
}

// fanOut sends a message to all subscriber channels.
func (r *RedisBroadcastBus) fanOut(msg *BroadcastMessage) {
	r.subMu.RLock()
	defer r.subMu.RUnlock()

	for _, subCh := range r.subscribers {
		select {
		case subCh <- msg:
			// Message sent
		case <-r.ctx.Done():
			return
		default:
			// Subscriber channel full, drop message (backpressure)
			r.logger.Warn().
				Str("subject", msg.Subject).
				Msg("Subscriber channel full, dropping message")
		}
	}
}

// Publish sends a message to Redis Pub/Sub channel.
// Non-blocking: logs error but doesn't block on failure.
func (r *RedisBroadcastBus) Publish(msg *BroadcastMessage) {
	// Serialize message
	data, err := json.Marshal(msg)
	if err != nil {
		r.logger.Warn().
			Err(err).
			Str("subject", msg.Subject).
			Msg("Failed to marshal message for Redis")
		r.lastError.Store(err)
		return
	}

	// Publish to Redis (fire-and-forget with timeout)
	publishCtx, cancel := context.WithTimeout(r.ctx, 100*time.Millisecond)
	defer cancel()

	if err := r.client.Publish(publishCtx, r.channel, data).Err(); err != nil {
		r.logger.Warn().
			Err(err).
			Str("channel", r.channel).
			Msg("Failed to publish to Redis")
		r.lastError.Store(err)
		r.connected.Store(false)
		return
	}

	// Track success
	r.lastPublishSuccess.Store(time.Now())
	r.connected.Store(true)
}

// Subscribe returns a channel for receiving broadcast messages.
// Called once per shard during initialization.
func (r *RedisBroadcastBus) Subscribe() chan *BroadcastMessage {
	subCh := make(chan *BroadcastMessage, 1024)

	r.subMu.Lock()
	r.subscribers = append(r.subscribers, subCh)
	r.subMu.Unlock()

	r.logger.Info().
		Int("subscriber_count", len(r.subscribers)).
		Msg("New subscriber registered")

	return subCh
}

// IsHealthy returns true if Redis is connected and publishing successfully.
func (r *RedisBroadcastBus) IsHealthy() bool {
	// Check connection status
	if !r.connected.Load() {
		return false
	}

	// Check last successful publish (within last 10 seconds)
	lastSuccess, ok := r.lastPublishSuccess.Load().(time.Time)
	if !ok || time.Since(lastSuccess) > 10*time.Second {
		// No publishes in last 10s might indicate issue
		// (or might be legitimate low traffic - check PING as fallback)
		pingCtx, cancel := context.WithTimeout(r.ctx, 1*time.Second)
		defer cancel()

		if err := r.client.Ping(pingCtx).Err(); err != nil {
			r.logger.Warn().Err(err).Msg("Redis PING failed, marking unhealthy")
			r.connected.Store(false)
			return false
		}
	}

	return true
}

// Shutdown gracefully stops the Redis BroadcastBus.
func (r *RedisBroadcastBus) Shutdown() {
	r.logger.Info().Msg("Shutting down Redis BroadcastBus")

	r.cancel()       // Signal listener to stop
	r.wg.Wait()      // Wait for listener to exit
	r.client.Close() // Close Redis connection

	// Close all subscriber channels
	r.subMu.Lock()
	for _, subCh := range r.subscribers {
		close(subCh)
	}
	r.subscribers = nil
	r.subMu.Unlock()

	r.logger.Info().Msg("Redis BroadcastBus shut down")
}

// Ensure RedisBroadcastBus implements BroadcastBus interface
var _ BroadcastBus = (*RedisBroadcastBus)(nil)
```

### File: `ws/internal/multi/broadcast_redis_test.go`

```go
package multi

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisBroadcastBus_PublishSubscribe(t *testing.T) {
	// Setup mock Redis server
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis broadcast bus
	logger := zerolog.Nop()
	bus, err := NewRedisBroadcastBus(RedisBroadcastBusConfig{
		DirectURL: "redis://" + mr.Addr(),
		Channel:   "test:broadcast",
		Logger:    logger,
	})
	require.NoError(t, err)
	defer bus.Shutdown()

	// Start bus
	bus.Run()

	// Subscribe
	subCh := bus.Subscribe()

	// Publish message
	testMsg := &BroadcastMessage{
		Subject: "test.subject",
		Message: []byte("test message"),
	}
	bus.Publish(testMsg)

	// Receive message
	select {
	case msg := <-subCh:
		assert.Equal(t, testMsg.Subject, msg.Subject)
		assert.Equal(t, testMsg.Message, msg.Message)
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestRedisBroadcastBus_Reconnection(t *testing.T) {
	// Setup mock Redis server
	mr, err := miniredis.Run()
	require.NoError(t, err)

	// Create Redis broadcast bus
	logger := zerolog.Nop()
	bus, err := NewRedisBroadcastBus(RedisBroadcastBusConfig{
		DirectURL: "redis://" + mr.Addr(),
		Channel:   "test:broadcast",
		Logger:    logger,
	})
	require.NoError(t, err)
	defer bus.Shutdown()

	bus.Run()

	// Bus should be healthy
	assert.True(t, bus.IsHealthy())

	// Simulate Redis server restart
	mr.Close()
	time.Sleep(100 * time.Millisecond)

	// Bus should detect disconnect
	assert.False(t, bus.IsHealthy())

	// Restart Redis
	mr, err = miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Note: In production, go-redis automatically reconnects.
	// This test demonstrates health check behavior.
}

func TestRedisBroadcastBus_MultipleSubscribers(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	logger := zerolog.Nop()
	bus, err := NewRedisBroadcastBus(RedisBroadcastBusConfig{
		DirectURL: "redis://" + mr.Addr(),
		Channel:   "test:broadcast",
		Logger:    logger,
	})
	require.NoError(t, err)
	defer bus.Shutdown()

	bus.Run()

	// Create 3 subscribers (simulating 3 shards)
	sub1 := bus.Subscribe()
	sub2 := bus.Subscribe()
	sub3 := bus.Subscribe()

	// Publish message
	testMsg := &BroadcastMessage{
		Subject: "test.multi",
		Message: []byte("multi-subscriber test"),
	}
	bus.Publish(testMsg)

	// All subscribers should receive message
	for i, subCh := range []chan *BroadcastMessage{sub1, sub2, sub3} {
		select {
		case msg := <-subCh:
			assert.Equal(t, testMsg.Subject, msg.Subject, "Subscriber %d", i)
		case <-time.After(1 * time.Second):
			t.Fatalf("Subscriber %d timeout", i)
		}
	}
}
```

---

## Migration Paths

### Path 1: In-Memory → Redis (Production Deployment)

**Scenario**: Deploy multi-instance architecture with Redis.

**Timeline**: 4 weeks

**Steps**:

**Week 1: Preparation**
1. Deploy Redis Sentinel on GCP (e2-standard-2)
2. Test Redis connectivity from ws-server instances
3. Update configuration with Redis settings
4. Deploy to staging environment

**Week 2: Shadow Mode**
1. Enable dual-bus mode (publish to both in-memory and Redis)
2. Subscribe from in-memory only (no behavior change)
3. Monitor message consistency (should be 100%)
4. Run for 1 week to validate

**Week 3: Gradual Rollout**
```
Day 1: 10% Redis, 90% in-memory
Day 2: 25% Redis, 75% in-memory
Day 3: 50% Redis, 50% in-memory
Day 4: 75% Redis, 25% in-memory
Day 5: 100% Redis
```
Monitor at each stage, rollback if errors >1%

**Week 4: Multi-Instance Deployment**
1. Update GCP load balancer to include 2nd ws-server instance
2. Both instances configured with `BROADCAST_BUS_TYPE=redis`
3. Verify end-to-end message delivery
4. Load test: 36K connections (18K per instance)

**Rollback Plan**:
```bash
# If ANY issues at ANY stage:
export BROADCAST_BUS_TYPE=inmemory
task gcp:deploy:rolling-restart
# Verify health checks pass
# Post-mortem analysis
```

### Path 2: Redis → NATS (Performance Upgrade)

**Scenario**: After 3-6 months on Redis, migrate to NATS for better performance.

**Timeline**: 3 weeks

**Prerequisite**: Team has 3+ months operational Redis experience.

**Steps**:

**Week 1: NATS Deployment**
1. Deploy NATS cluster (self-hosted or Synadia Cloud)
2. Implement `NatsBroadcastBus` (following existing Redis pattern)
3. Add `BROADCAST_BUS_TYPE=nats` config support
4. Test in dev environment

**Week 2: Shadow Mode**
1. Publish to BOTH Redis AND NATS
2. Subscribe from Redis only (production unchanged)
3. Compare latency: expect NATS <0.5ms vs Redis 1-2ms
4. Verify 100% message consistency

**Week 3: Migration**
```bash
# Day 1: Single instance on NATS (test)
BROADCAST_BUS_TYPE=nats task gcp:deploy:instance-1

# Day 2: Monitor for 24 hours
# - Latency should improve
# - Error rate should be zero

# Day 3: Deploy to all instances
BROADCAST_BUS_TYPE=nats task gcp:deploy:rolling-restart

# Day 4: Decommission Redis
task gcp:redis:stop
```

**Zero Code Changes**:
```diff
# .env.production (BEFORE)
BROADCAST_BUS_TYPE=redis
REDIS_SENTINEL_ADDRS=10.128.0.5:26379,10.128.0.5:26380,10.128.0.5:26381
REDIS_MASTER_NAME=mymaster

# .env.production (AFTER)
BROADCAST_BUS_TYPE=nats
NATS_URL=nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222

# NO CODE CHANGES NEEDED!
```

### Path 3: Redis → In-Memory (Rollback/Downgrade)

**Scenario**: Discovered issue with Redis, need emergency rollback.

**Timeline**: <5 minutes

**Trigger**: Error rate >1%, latency >20ms, Redis complete failure

**Emergency Rollback**:
```bash
# 1. Update environment variable (all instances)
export BROADCAST_BUS_TYPE=inmemory

# 2. Rolling restart (zero downtime, <2 min)
task gcp:deploy:rolling-restart

# 3. Verify health
for instance in ws-server-{1,2}; do
  curl http://$instance:3005/health | jq '.broadcast_bus'
  # Expected: {"type":"inmemory","healthy":true,"connected":true}
done

# 4. Monitor for 10 minutes
# - No errors
# - Latency normal
# - Connections stable

# 5. Post-mortem
# - Analyze Redis logs
# - Review metrics pre-failure
# - Document root cause
# - Fix issue
# - Re-deploy Redis when ready
```

**Limitations After Rollback**:
- ⚠️ Back to single-instance architecture
- ⚠️ Cannot scale horizontally (in-memory bus)
- ⚠️ Must scale vertically (larger instance type)

---

## Performance Considerations

### Latency Breakdown

**End-to-End Message Flow**:
```
Kafka → ws-server-1 → Redis → ws-server-2 → Client
  ↓         ↓          ↓          ↓           ↓
1-2ms     <1ms       1-2ms     1-2ms       1-2ms
```

**Expected Latencies**:

**With Redis**:
```
Component                    Latency (p99)
─────────────────────────────────────────
Kafka fetch:                 1-2ms
Consumer processing:         <1ms
Redis PUBLISH:               0.5-1ms
Redis propagation:           0.5-1ms
Redis SUBSCRIBE:             0.5-1ms
Shard broadcast:             1-2ms
WebSocket write:             1-2ms
─────────────────────────────────────────
Total:                       6-10ms ✓
```

**With NATS (future)**:
```
Component                    Latency (p99)
─────────────────────────────────────────
Kafka fetch:                 1-2ms
Consumer processing:         <1ms
NATS Publish:                0.1ms
NATS propagation:            0.2ms
NATS Subscribe:              0.1ms
Shard broadcast:             1-2ms
WebSocket write:             1-2ms
─────────────────────────────────────────
Total:                       5-8ms ✓
```

**Comparison**:
- Redis: Acceptable (meets <10ms SLA)
- NATS: Better (2-3ms faster, but not critical for current load)

### Throughput Analysis

**Redis Pub/Sub Capacity**:
```
Single Redis server:     ~1M msg/sec (pub+sub combined)
Our workload:            1K msg/sec
Headroom:                1000× (plenty of room)

At 10× growth:           10K msg/sec
Redis capacity:          Still 100× headroom

At 100× growth:          100K msg/sec
Redis capacity:          10× headroom (consider sharding or NATS)
```

**Network Bandwidth**:
```
Message size:            ~1 KB average
Rate:                    1K msg/sec
Bandwidth:               1 MB/sec = 8 Mbps

GCP e2-standard-2:       Up to 10 Gbps
Utilization:             0.08% (negligible)
```

**CPU Impact**:
```
Redis Pub/Sub:           Very efficient (C implementation)
Expected CPU:            <10% on e2-standard-2 (2 vCPU)

Breakdown:
- PUBLISH commands:      2-5% CPU
- SUBSCRIBE delivery:    2-5% CPU
- AOF persistence:       2-5% CPU
- Replication:           <2% CPU
─────────────────────────────────────
Total:                   <15% CPU
```

### Memory Considerations

**Redis Memory Usage**:
```
Master Redis:
  Base:                  ~50 MB (Redis server)
  Pub/Sub buffers:       32 MB (client-output-buffer)
  AOF buffer:            8 MB (appendfsync everysec)
  Connection overhead:   10 MB (10 connections × 1MB)
  OS overhead:           50 MB
  ─────────────────────────────────────
  Total:                 ~150 MB

Per Replica:             Same as master (~150 MB)

Sentinels (×3):          ~50 MB each
```

**Buffer Sizing**:
```go
// Subscriber channel buffer (Go)
subCh := make(chan *BroadcastMessage, 1024)

At 1024 buffer:
  Message size:          ~1 KB
  Buffer capacity:       1024 × 1 KB = 1 MB per subscriber

For 6 shards:            6 × 1 MB = 6 MB total
```

### Performance Optimization Strategies

**1. Connection Pooling**
```go
RedisPoolSize:           10   // Max connections
RedisMinIdleConns:       5    // Keep-alive connections

Rationale:
- 3 shards × 2 instances = 6 active publishers
- Pool of 10 provides headroom for bursts
- Keep-alive reduces connection latency
```

**2. Pipelining** (Future Optimization)
```go
// Batch multiple PUBLISH commands
pipe := r.client.Pipeline()
for _, msg := range batch {
    pipe.Publish(ctx, r.channel, msg)
}
pipe.Exec(ctx)

// Reduces round-trips: N commands → 1 RTT
// Improvement: ~30-50% latency reduction for bursts
```

**3. Batching** (Already Implemented in BroadcastBus)
```go
// In-memory bus already batches (see broadcast.go line 74-97)
// Redis inherits this batching via Publish() calls

Benefit:
- Reduces PUBLISH calls/sec
- Amortizes serialization cost
- Better CPU cache utilization
```

**4. Compression** (Optional, for Large Messages)
```go
// If messages >10 KB, consider compression
import "compress/gzip"

func (r *RedisBroadcastBus) Publish(msg *BroadcastMessage) {
    // Compress large messages
    if len(msg.Message) > 10240 {
        compressed := gzipCompress(msg.Message)
        // ... publish compressed ...
    }
}

// Trade-off: CPU for bandwidth (only if network constrained)
```

### Benchmarking Approach

**Benchmark 1: Baseline (In-Memory)**
```bash
# Measure current latency
task benchmark:inmemory

# Metrics:
# - p50 latency: <5ms
# - p99 latency: <10ms
# - Throughput: 1K msg/sec
```

**Benchmark 2: Redis (Local)**
```bash
# Docker Compose with Redis
task benchmark:redis-local

# Compare to baseline:
# - Expect +1-2ms added latency
# - Throughput should be same
```

**Benchmark 3: Redis (GCP)**
```bash
# Production-like environment
task benchmark:redis-gcp

# Metrics:
# - p99 latency: <10ms (target)
# - Throughput: 1K msg/sec sustained
# - Error rate: 0%
```

**Benchmark 4: Failover Impact**
```bash
# Measure failover recovery time
task benchmark:redis-failover

# Metrics:
# - Detection time: <5s (Sentinel down-after-milliseconds)
# - Failover time: <10s (Sentinel failover-timeout)
# - Message loss: 0 (replicas have data)
```

---

## Error Handling & Resilience

### Error Categories & Responses

**1. Connection Errors (Network/Redis Down)**

**Scenario**: Redis server unreachable, network partition.

**Detection**:
```go
// In Publish()
if err := r.client.Publish(ctx, r.channel, data).Err(); err != nil {
    if err == context.DeadlineExceeded {
        // Timeout (Redis slow or unreachable)
    } else if err == io.EOF || errors.Is(err, net.ErrClosed) {
        // Connection closed
    }
    // ...
}
```

**Response**:
1. Log error with context
2. Mark unhealthy: `r.connected.Store(false)`
3. Auto-reconnect: `go-redis` handles automatically
4. Alert: "Redis connection down" (critical)

**Client Impact**: None (messages buffered in ws-server, replayed after reconnect)

**2. Sentinel Failover (Master Crash)**

**Scenario**: Redis master crashes, Sentinel promotes replica.

**Timeline**:
```
T+0s:  Master crashes
T+5s:  Sentinels detect failure (down-after-milliseconds)
T+6s:  Sentinels vote (quorum)
T+7s:  Replica promoted to master
T+8s:  Clients discover new master
T+10s: Normal operation resumed
```

**Detection**:
```go
// Sentinel client handles automatically
// Publishes fail to old master → auto-switch to new master
// No application code needed
```

**Response**: Automatic (go-redis Sentinel client handles failover)

**Client Impact**: 5-10s degraded (messages queued, then delivered)

**3. Pub/Sub Channel Full (Slow Subscriber)**

**Scenario**: Subscriber (shard) can't keep up, channel full.

**Detection**:
```go
// In fanOut()
select {
case subCh <- msg:
    // Success
default:
    // Channel full!
    r.logger.Warn().Msg("Subscriber channel full, dropping message")
}
```

**Response**:
1. Drop message (acceptable for real-time data)
2. Log warning (track frequency)
3. Metric: `broadcast_bus_messages_dropped_total`
4. Alert if sustained (>10 drops/min)

**Root Cause**: Slow shard (overloaded CPU, slow disk, bug)

**Mitigation**: Identify slow shard, investigate resource usage

**4. Serialization Errors (Corrupt Message)**

**Scenario**: Invalid JSON from Redis (unlikely, but possible).

**Detection**:
```go
// In runListener()
if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
    r.logger.Warn().Err(err).Msg("Failed to unmarshal Redis message")
    continue  // Skip this message
}
```

**Response**:
1. Log error with payload snippet
2. Skip message (continue processing others)
3. Metric: `broadcast_bus_unmarshal_errors_total`
4. Alert if >1% error rate

**Root Cause**: Bug in publisher, Redis corruption (rare), schema mismatch

**5. Publish Timeout (Redis Overloaded)**

**Scenario**: Redis server overloaded, PUBLISH takes >100ms.

**Detection**:
```go
publishCtx, cancel := context.WithTimeout(r.ctx, 100*time.Millisecond)
defer cancel()

if err := r.client.Publish(publishCtx, r.channel, data).Err(); err != nil {
    if err == context.DeadlineExceeded {
        // Timeout!
    }
}
```

**Response**:
1. Log warning
2. Metric: `broadcast_bus_publish_timeout_total`
3. Alert: "Redis high latency" (warning)

**Investigation**:
- Check Redis CPU/memory
- Check Kafka message rate spike
- Check slow clients (backpressure)

### Reconnection Strategy

**go-redis Built-In Reconnection**:
```go
// Sentinel client automatically reconnects
client := redis.NewFailoverClient(&redis.FailoverOptions{
    MaxRetries:   3,   // Retry failed commands
    // Auto-reconnect on disconnect (infinite)
})
```

**Application-Level Resilience**:
```go
// Publish with context timeout
publishCtx, cancel := context.WithTimeout(r.ctx, 100*time.Millisecond)
defer cancel()

err := r.client.Publish(publishCtx, r.channel, data).Err()
if err != nil {
    // Log but don't crash
    // go-redis will reconnect automatically
    r.logger.Warn().Err(err).Msg("Publish failed, will retry on next message")
}
```

**Health Check Reconnection**:
```go
func (r *RedisBroadcastBus) IsHealthy() bool {
    // Periodic PING to verify connection
    pingCtx, cancel := context.WithTimeout(r.ctx, 1*time.Second)
    defer cancel()

    if err := r.client.Ping(pingCtx).Err(); err != nil {
        r.logger.Warn().Err(err).Msg("Redis PING failed")
        return false
    }
    return true
}
```

### Circuit Breaker Pattern (Optional)

**When to Use**: If Redis has persistent failures (>1 min down).

**Implementation**:
```go
import "github.com/sony/gobreaker"

// Create circuit breaker
cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
    Name:        "redis_publish",
    MaxRequests: 3,
    Interval:    60 * time.Second,
    Timeout:     60 * time.Second,
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
        return counts.Requests >= 3 && failureRatio >= 0.6
    },
})

// Wrap Publish
func (r *RedisBroadcastBus) Publish(msg *BroadcastMessage) {
    _, err := r.cb.Execute(func() (interface{}, error) {
        return nil, r.client.Publish(r.ctx, r.channel, data).Err()
    })

    if err == gobreaker.ErrOpenState {
        // Circuit open, Redis unhealthy, skip publish
        r.logger.Warn().Msg("Circuit breaker open, skipping publish")
        return
    }
}
```

**Behavior**:
- **Closed**: Normal operation, all publishes attempted
- **Open**: Redis failing, skip publishes (fail fast)
- **Half-Open**: Testing recovery, limited publishes

**Trade-off**: Complexity vs reliability (only add if needed)

### Graceful Degradation

**Scenario**: Redis completely unavailable for extended period.

**Options**:

**Option A: Fail Fast**
```go
// If Redis unhealthy, reject new connections
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
    if !s.broadcastBus.IsHealthy() {
        http.Error(w, "Service degraded: broadcast unavailable", 503)
        return
    }
    // ... continue upgrade ...
}
```

**Option B: Degrade to Local-Only**
```go
// If Redis unhealthy, broadcast only to local shards
func (s *Shard) Broadcast(msg *BroadcastMessage) {
    if s.broadcastBus.IsHealthy() {
        // Normal: publish to Redis (all instances)
        s.broadcastBus.Publish(msg)
    } else {
        // Degraded: broadcast locally only
        s.localBroadcast(msg)
    }
}
```

**Recommendation**: **Option A** (fail fast) - clearer to clients that service is degraded.

---

## Monitoring & Observability

### Prometheus Metrics

**Custom Application Metrics**:

```go
// File: ws/internal/multi/broadcast_redis_metrics.go
package multi

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Publish metrics
	broadcastBusPublishTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "broadcast_bus_publish_total",
			Help: "Total messages published to broadcast bus",
		},
		[]string{"type", "status"}, // type=redis, status=success|error
	)

	broadcastBusPublishDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "broadcast_bus_publish_duration_seconds",
			Help:    "Latency of broadcast bus publish operations",
			Buckets: []float64{.0001, .0005, .001, .002, .005, .010, .020, .050, .100},
		},
		[]string{"type"}, // type=redis
	)

	// Subscribe metrics
	broadcastBusMessagesReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "broadcast_bus_messages_received_total",
			Help: "Total messages received from broadcast bus",
		},
		[]string{"type"}, // type=redis
	)

	broadcastBusMessagesDropped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "broadcast_bus_messages_dropped_total",
			Help: "Messages dropped due to full subscriber channel",
		},
		[]string{"type", "reason"}, // type=redis, reason=channel_full
	)

	// Health metrics
	broadcastBusHealthy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "broadcast_bus_healthy",
			Help: "Health status of broadcast bus (1=healthy, 0=unhealthy)",
		},
		[]string{"type"}, // type=redis
	)

	broadcastBusConnected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "broadcast_bus_connected",
			Help: "Connection status (1=connected, 0=disconnected)",
		},
		[]string{"type"}, // type=redis
	)

	// Error metrics
	broadcastBusErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "broadcast_bus_errors_total",
			Help: "Total errors by category",
		},
		[]string{"type", "error_type"}, // error_type=publish_timeout|unmarshal|connection
	)

	// Subscriber count
	broadcastBusSubscribers = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "broadcast_bus_subscribers",
			Help: "Number of active subscribers (shards)",
		},
		[]string{"type"}, // type=redis
	)
)

// Instrumented wrapper for RedisBroadcastBus
type InstrumentedRedisBroadcastBus struct {
	*RedisBroadcastBus
}

func (i *InstrumentedRedisBroadcastBus) Publish(msg *BroadcastMessage) {
	start := time.Now()

	i.RedisBroadcastBus.Publish(msg)

	duration := time.Since(start)
	broadcastBusPublishDuration.WithLabelValues("redis").Observe(duration.Seconds())

	if i.connected.Load() {
		broadcastBusPublishTotal.WithLabelValues("redis", "success").Inc()
	} else {
		broadcastBusPublishTotal.WithLabelValues("redis", "error").Inc()
	}

	// Update health gauge
	if i.IsHealthy() {
		broadcastBusHealthy.WithLabelValues("redis").Set(1)
		broadcastBusConnected.WithLabelValues("redis").Set(1)
	} else {
		broadcastBusHealthy.WithLabelValues("redis").Set(0)
		broadcastBusConnected.WithLabelValues("redis").Set(0)
	}
}
```

**Redis Server Metrics** (via redis_exporter):

```promql
# Connection status
redis_up

# Commands per second
rate(redis_commands_processed_total[5m])

# Memory usage
redis_memory_used_bytes

# Pub/Sub specific
redis_pubsub_channels         # Number of active channels
redis_pubsub_patterns         # Number of pattern subscriptions

# Replication
redis_connected_slaves        # Number of connected replicas
redis_master_repl_offset      # Replication offset (lag indicator)

# Latency
redis_commands_duration_seconds_total
```

### Grafana Dashboards

**Dashboard 1: Broadcast Bus Overview**

**Panels**:

1. **Health Status** (Gauge)
   ```promql
   broadcast_bus_healthy{type="redis"}
   ```
   Display: Green (1) / Red (0)

2. **Connection Status** (Stat)
   ```promql
   broadcast_bus_connected{type="redis"}
   ```
   Display: "Connected" / "Disconnected"

3. **Publish Rate** (Graph)
   ```promql
   rate(broadcast_bus_publish_total{type="redis"}[5m])
   ```
   Display: Messages/sec over time

4. **Publish Latency** (Graph)
   ```promql
   histogram_quantile(0.50, rate(broadcast_bus_publish_duration_seconds_bucket{type="redis"}[5m]))
   histogram_quantile(0.95, rate(broadcast_bus_publish_duration_seconds_bucket{type="redis"}[5m]))
   histogram_quantile(0.99, rate(broadcast_bus_publish_duration_seconds_bucket{type="redis"}[5m]))
   ```
   Legend: p50, p95, p99

5. **Error Rate** (Graph)
   ```promql
   rate(broadcast_bus_publish_total{type="redis",status="error"}[5m])
   ```
   Threshold: >0.01 (red)

6. **Messages Dropped** (Counter)
   ```promql
   increase(broadcast_bus_messages_dropped_total{type="redis"}[5m])
   ```
   Alert: >10 drops in 5 min

**Dashboard 2: Redis Server Health**

**Panels**:

1. **Redis Uptime** (Stat)
   ```promql
   redis_uptime_in_seconds
   ```

2. **CPU Usage** (Graph)
   ```promql
   redis_cpu_sys_seconds_total
   redis_cpu_user_seconds_total
   ```

3. **Memory Usage** (Graph)
   ```promql
   redis_memory_used_bytes
   redis_memory_max_bytes
   ```
   Show: Used / Max ratio

4. **Commands/sec** (Graph)
   ```promql
   rate(redis_commands_processed_total[5m])
   ```

5. **Pub/Sub Channels** (Stat)
   ```promql
   redis_pubsub_channels
   ```

6. **Replication Lag** (Graph)
   ```promql
   redis_master_repl_offset - on(instance) group_right redis_slave_repl_offset
   ```
   Display: Bytes behind master

**Dashboard 3: Multi-Instance Comparison**

**Panels**:

1. **Messages Published per Instance** (Graph)
   ```promql
   sum by (instance) (rate(broadcast_bus_publish_total[5m]))
   ```

2. **Messages Received per Instance** (Graph)
   ```promql
   sum by (instance) (rate(broadcast_bus_messages_received_total[5m]))
   ```

3. **Latency Comparison** (Graph)
   ```promql
   histogram_quantile(0.99, sum by (instance) (rate(broadcast_bus_publish_duration_seconds_bucket[5m])))
   ```
   Compare: instance-1 vs instance-2

### Alerting Rules

**File: `prometheus/alerts/broadcast_bus.yml`**

```yaml
groups:
  - name: broadcast_bus_alerts
    interval: 30s
    rules:
      # Critical: Redis connection down
      - alert: BroadcastBusDisconnected
        expr: broadcast_bus_connected{type="redis"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Broadcast bus disconnected from Redis"
          description: "Instance {{ $labels.instance }} cannot connect to Redis for 1+ minute"
          runbook: "docs/runbooks/redis-connection-issues.md"

      # Critical: High error rate
      - alert: BroadcastBusHighErrorRate
        expr: rate(broadcast_bus_publish_total{type="redis",status="error"}[5m]) > 0.01
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Broadcast bus error rate >1%"
          description: "Error rate: {{ $value }} errors/sec on {{ $labels.instance }}"
          runbook: "docs/runbooks/high-error-rate.md"

      # Warning: High latency
      - alert: BroadcastBusHighLatency
        expr: histogram_quantile(0.99, rate(broadcast_bus_publish_duration_seconds_bucket{type="redis"}[5m])) > 0.005
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Broadcast bus p99 latency >5ms"
          description: "p99 latency: {{ $value }}s on {{ $labels.instance }} (threshold: 5ms)"
          runbook: "docs/runbooks/high-latency.md"

      # Warning: Messages dropping
      - alert: BroadcastBusMessagesDropping
        expr: rate(broadcast_bus_messages_dropped_total{type="redis"}[5m]) > 1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Broadcast bus dropping messages"
          description: "Dropping {{ $value }} messages/sec on {{ $labels.instance }}"
          runbook: "docs/runbooks/messages-dropping.md"

      # Critical: Redis server down
      - alert: RedisServerDown
        expr: redis_up == 0
        for: 30s
        labels:
          severity: critical
        annotations:
          summary: "Redis server down"
          description: "Redis server {{ $labels.instance }} is unreachable"
          runbook: "docs/runbooks/redis-server-down.md"

      # Warning: Redis memory high
      - alert: RedisMemoryHigh
        expr: redis_memory_used_bytes > 450000000  # 450MB (out of 512MB limit)
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Redis memory usage high (>450MB)"
          description: "Memory: {{ humanize $value }} on {{ $labels.instance }}"
          runbook: "docs/runbooks/redis-memory-high.md"

      # Warning: Sentinel failover
      - alert: RedisSentinelFailover
        expr: changes(redis_master_repl_offset[5m]) > 0 and redis_master_repl_offset == 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Redis Sentinel failover detected"
          description: "Master changed on {{ $labels.instance }}, verify health"
          runbook: "docs/runbooks/sentinel-failover.md"
```

### Logging Strategy

**Structured Logging**:

```go
// Success (info level)
r.logger.Info().
    Str("channel", r.channel).
    Dur("latency", duration).
    Msg("Published to Redis")

// Connection event (info level)
r.logger.Info().
    Strs("sentinel_addrs", cfg.SentinelAddrs).
    Str("master_name", cfg.MasterName).
    Msg("Connected to Redis via Sentinel")

// Publish error (warn level)
r.logger.Warn().
    Err(err).
    Str("channel", r.channel).
    Str("subject", msg.Subject).
    Msg("Failed to publish to Redis")

// Unmarshal error (warn level)
r.logger.Warn().
    Err(err).
    Str("payload_preview", redisMsg.Payload[:100]).  // First 100 chars
    Msg("Failed to unmarshal Redis message")

// Health check failure (warn level)
r.logger.Warn().
    Err(err).
    Dur("last_success", time.Since(lastSuccess)).
    Msg("Redis health check failed")

// Shutdown (info level)
r.logger.Info().
    Int("active_subscribers", len(r.subscribers)).
    Msg("Redis BroadcastBus shutting down")
```

**Log Aggregation** (GCP Cloud Logging or ELK):

```bash
# Filter by component
component="redis_broadcast_bus"

# Find errors
level="error" OR level="warn"

# Search for specific issues
message:"Failed to publish" OR message:"connection refused"

# Time range analysis
timestamp >= "2025-01-20T10:00:00Z" AND timestamp <= "2025-01-20T11:00:00Z"
```

**Alert on Log Patterns**:

```yaml
# GCP Logging Alert (example)
- alert: RedisBroadcastBusErrors
  condition: |
    resource.type="gce_instance"
    AND jsonPayload.component="redis_broadcast_bus"
    AND jsonPayload.level="error"
  threshold: >5 errors in 5 minutes
  notification: PagerDuty
```

---

## Risk Analysis & Mitigation

### Risk Matrix

| Risk | Probability | Impact | Severity | Mitigation |
|------|------------|--------|----------|------------|
| **Redis Complete Failure** | Low | High | Critical | Sentinel HA, Auto-reconnect, Rollback plan |
| **Sentinel Failover Delay** | Medium | Medium | High | Tuned timeouts, Pre-test failover, Monitoring |
| **Network Partition** | Low | High | Critical | Redis Sentinel quorum, Health checks, Alerts |
| **Memory Leak in Redis** | Very Low | Medium | Medium | Memory limits, Eviction policy, Monitoring |
| **Performance Regression** | Medium | Medium | High | Shadow mode testing, Gradual rollout, Benchmarks |
| **Configuration Error** | Medium | High | Critical | Config validation, Staged deployment, Rollback |
| **Pub/Sub Backpressure** | Medium | Medium | Medium | Channel buffers, Drop monitoring, Shard scaling |
| **Redis Security Breach** | Low | High | Critical | Password auth, Firewall rules, Internal VPC only |

### Risk 1: Redis Complete Failure (All Replicas Down)

**Scenario**: Data center outage, simultaneous hardware failure, catastrophic bug.

**Impact**:
- Broadcast bus unavailable
- Multi-instance messaging stops
- Cross-instance clients don't receive messages

**Probability**: Low (with Sentinel HA)

**Detection**:
- Alert: `RedisServerDown` fires for all Redis instances
- Alert: `BroadcastBusDisconnected` fires on all ws-servers
- Health checks fail: `/health` returns 503

**Mitigation**:

**Immediate** (<5 min):
```bash
# Emergency rollback to in-memory bus
export BROADCAST_BUS_TYPE=inmemory
task gcp:deploy:rolling-restart

# Limitations:
# - Single-instance only (no horizontal scaling)
# - Must remove 2nd ws-server instance from load balancer
```

**Short-term** (<1 hour):
```bash
# Provision new Redis instance
task gcp:redis:provision-emergency

# Restore from backup (if applicable)
# Note: Pub/Sub doesn't need persistence, just reconnect
```

**Long-term** (1-7 days):
```bash
# Root cause analysis
# - Why did all replicas fail?
# - Infrastructure issue? Software bug?

# Implement additional safeguards
# - Multi-zone deployment
# - Chaos engineering testing
# - Circuit breaker pattern
```

### Risk 2: Sentinel Failover Takes >15 Seconds

**Scenario**: Sentinel misconfiguration, network latency, slow replica.

**Impact**:
- 15s+ messaging degradation
- Messages queued (not lost)
- Clients see temporary delays

**Probability**: Medium (if not tuned properly)

**Detection**:
- Alert: `RedisSentinelFailover` fires
- Metrics: Spike in `broadcast_bus_publish_duration`

**Mitigation**:

**Pre-Deployment**:
1. Test failover in staging: `task redis:test-failover`
2. Tune Sentinel timeouts:
   ```conf
   sentinel down-after-milliseconds mymaster 5000  # 5s detection
   sentinel failover-timeout mymaster 10000        # 10s max failover
   ```
3. Verify replica sync is fast (<1s replication lag)

**During Failover**:
1. Monitor Sentinel logs: `journalctl -u redis-sentinel-1 -f`
2. If timeout exceeded, manual intervention:
   ```bash
   # Force failover
   redis-cli -p 26379 SENTINEL failover mymaster
   ```

**Post-Failover**:
1. Analyze logs to identify bottleneck
2. Adjust timeouts if needed
3. Add chaos testing to CI/CD

### Risk 3: Performance Regression (Latency >10ms)

**Scenario**: Redis overloaded, network congestion, configuration issue.

**Impact**:
- Violates <10ms p99 SLA
- Degraded user experience
- Potential cascading failures

**Probability**: Medium (especially under load)

**Detection**:
- Alert: `BroadcastBusHighLatency` fires
- Metrics: p99 >10ms sustained

**Mitigation**:

**Investigation**:
1. Check Redis CPU: `redis-cli INFO stats | grep used_cpu`
2. Check network latency: `ping -c 100 <redis-ip>`
3. Check message rate: Spike in Kafka traffic?
4. Check slow clients: Any shards with backpressure?

**Solutions**:

**High CPU**:
```bash
# Scale Redis instance
gcloud compute instances stop redis-server
gcloud compute instances set-machine-type redis-server --machine-type=e2-standard-4
gcloud compute instances start redis-server
```

**Network Congestion**:
```bash
# Verify instances in same zone
gcloud compute instances list | grep us-central1-a

# Move Redis to same zone if not
```

**Configuration Tuning**:
```conf
# Increase client output buffer (if dropping messages)
client-output-buffer-limit pubsub 64mb 16mb 60  # Double limits
```

**Permanent Fix**:
```bash
# If persistent issue, migrate to NATS (faster)
export BROADCAST_BUS_TYPE=nats
task gcp:deploy:rolling-restart
```

### Risk 4: Configuration Error (Wrong Redis URL)

**Scenario**: Typo in env var, wrong Sentinel address, password mismatch.

**Impact**:
- ws-server fails to start
- Deployment blocked
- Potential downtime if rolling restart

**Probability**: Medium (human error)

**Detection**:
- Startup failure: "Failed to connect to Redis"
- Health check fails immediately

**Mitigation**:

**Pre-Deployment Validation**:
```go
// In NewRedisBroadcastBus
func NewRedisBroadcastBus(cfg RedisBroadcastBusConfig) (*RedisBroadcastBus, error) {
    // Validate configuration
    if len(cfg.SentinelAddrs) == 0 && cfg.DirectURL == "" {
        return nil, fmt.Errorf("must provide SentinelAddrs or DirectURL")
    }

    if cfg.Password == "" {
        return nil, fmt.Errorf("Redis password is required (security)")
    }

    // Test connection immediately
    if err := client.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis: %w", err)
    }

    return bus, nil
}
```

**Staged Deployment**:
```bash
# Test in dev first
export BROADCAST_BUS_TYPE=redis
export REDIS_SENTINEL_ADDRS=...
./ws-multi --shards=1

# If successful, deploy to staging
task gcp:deploy:staging

# If successful, deploy to production
task gcp:deploy:production
```

**Rollback**:
```bash
# If production deployment fails
task gcp:deploy:rollback

# Reverts to previous config (in-memory)
```

### Risk 5: Pub/Sub Backpressure (Slow Shards)

**Scenario**: One shard overloaded, can't consume messages fast enough.

**Impact**:
- Subscriber channel full
- Messages dropped for that shard
- Partial client message loss

**Probability**: Medium (especially under load)

**Detection**:
- Alert: `BroadcastBusMessagesDropping` fires
- Logs: "Subscriber channel full, dropping message"

**Mitigation**:

**Identify Slow Shard**:
```bash
# Check shard metrics
curl http://ws-server-1:3005/stats | jq '.shards[] | select(.dropped_broadcasts > 0)'
# Output: Shard 2 has 150 dropped messages
```

**Root Cause Analysis**:
```bash
# Check shard resource usage
curl http://ws-server-1:3005/stats | jq '.shards[2]'
# Check: CPU, memory, goroutines, client count
```

**Solutions**:

**Temporary**:
```bash
# Increase buffer size (requires restart)
export BROADCAST_BUS_BUFFER_SIZE=2048  # Double from 1024
task gcp:deploy:shard-restart shard=2
```

**Permanent**:
```bash
# Rebalance load (move clients from Shard 2 to other shards)
# OR
# Scale horizontally (add more ws-server instances)
task gcp:deploy:scale-up instances=3
```

---

## Cost Analysis

### Infrastructure Costs (Monthly, us-central1)

**Redis Deployment**:
```
e2-standard-2 (Redis server):
  - vCPU: 2 shared cores
  - Memory: 8 GB
  - Disk: 50 GB SSD
  - Cost: ~$50/month

Total Redis: $50/month
```

**ws-server Instances** (unchanged):
```
2× e2-highcpu-8 (ws-servers):
  - Each: ~$50/month
  - Total: $100/month
```

**Total Infrastructure**:
```
Redis:              $50/month
ws-servers (×2):    $100/month
Kafka (existing):   $0 (already deployed)
Monitoring (existing): $0 (already deployed)
─────────────────────────────────────
Total:              $150/month

vs. Single Instance:
  - 1× e2-highcpu-8:  $50/month
  - Cost increase:    $100/month (+200%)
  - Capacity increase: 2× (+100%)
```

### Cost-Benefit Analysis

**Benefits of Multi-Instance**:
- **Capacity**: 18K → 36K connections (+100%)
- **Redundancy**: Instance failure → automatic failover
- **Scalability**: Add instances → linear capacity increase
- **Performance**: Distribute load → lower per-instance CPU

**Cost Efficiency**:
```
Cost per 1000 connections:
  - Single instance: $50 / 18 = $2.78/1K connections
  - Multi-instance:  $150 / 36 = $4.17/1K connections

Delta: +50% cost per connection
Rationale: HA and scalability premium (acceptable)
```

### Cost Optimization Strategies

**Option 1: Use Smaller Redis Instance** (Cost-Conscious)
```
e2-standard-2 → e2-micro
  - vCPU: 2 → 0.5-2 (burstable)
  - Memory: 8GB → 1GB
  - Cost: $50 → $6/month
  - Savings: $44/month (88% reduction)

Limitations:
  - No HA (single-node Redis only)
  - 1GB RAM limit (sufficient for Pub/Sub)
  - Burstable CPU (may throttle under load)

Recommendation: Development/staging only
```

**Option 2: Redis Cluster on ws-server Instances** (Co-location)
```
Deploy Redis on ws-server instances (no separate VM)
  - Cost: $0 (use existing instances)
  - Savings: $50/month

Limitations:
  - Resource contention (Redis vs ws-server)
  - Failure correlation (instance down → Redis down)
  - Harder to scale independently

Recommendation: Not recommended for production
```

**Option 3: Managed Redis** (Premium)
```
GCP Memorystore (Redis-compatible):
  - Basic tier (no HA): ~$40/month (1GB)
  - Standard tier (HA): ~$80/month (1GB)

vs. Self-Hosted (e2-standard-2): $50/month

Delta: +$30/month for managed HA

Benefits:
  - Automatic backups
  - Automatic patching
  - Google SLA (99.9%)
  - Managed failover

Recommendation: Consider for production if ops burden high
```

### Cost Projection (Scaling)

**Scenario 1: 36K → 72K connections** (2× growth)
```
Current: 2 ws-server instances + 1 Redis instance
Future: 4 ws-server instances + 1 Redis instance (same)

Cost Change:
  - Redis: $50 (no change)
  - ws-servers: $100 → $200 (+$100)
  ─────────────────────────────────────
  Total: $150 → $250 (+$100/month)

Efficiency:
  - Cost per 1K: $4.17 → $3.47 (-17% per connection)
  - Redis cost amortized across more instances
```

**Scenario 2: 72K → 144K connections** (4× growth)
```
Current: 2 ws-server instances + 1 Redis instance
Future: 8 ws-server instances + 1 Redis instance (Redis upgrade needed)

Cost Change:
  - Redis: $50 → $100 (upgrade to e2-standard-4)
  - ws-servers: $100 → $400 (+$300)
  ─────────────────────────────────────
  Total: $150 → $500 (+$350/month)

Efficiency:
  - Cost per 1K: $4.17 → $3.47 (same as 72K scenario)
  - Redis still not bottleneck at 144K
```

**Scenario 3: >144K connections** (>4× growth)
```
At this scale, consider migrating to NATS:
  - NATS handles 10M msg/sec (vs Redis 1M)
  - Lower latency (<0.5ms vs 1-2ms)
  - More efficient (300MB RAM vs 1.6GB)

Cost (Synadia Cloud):
  - Growth Plan: $299/month
  - vs. Self-hosted Redis: $100/month
  - Delta: +$199/month

ROI: Better performance + lower ops burden = justified
```

---

## Decision Matrix

### When to Use Each Implementation

| Deployment Scenario | Recommended Bus | Rationale |
|---------------------|----------------|-----------|
| **Single instance, <18K connections** | InMemory | Simplest, lowest latency, no external deps |
| **2-4 instances, <100K connections** | Redis | Familiar tech, sufficient performance, HA with Sentinel |
| **4+ instances, 100K-1M connections** | NATS | Lower latency, higher throughput, lower resource usage |
| **Global deployment (multi-region)** | NATS + JetStream | Geo-distributed, durable messaging, multi-region sync |
| **Dev/Staging (any scale)** | InMemory → Redis | Match production architecture, test failover |
| **Emergency rollback** | InMemory | Fast rollback, zero external deps, known-good state |

### Technology Selection Criteria

**Choose InMemory if**:
- ✓ Single ws-server instance
- ✓ No horizontal scaling needed
- ✓ Want lowest latency (<0.1ms)
- ✓ Minimal operational complexity

**Choose Redis if**:
- ✓ Multi-instance deployment (2-10 instances)
- ✓ Team has Redis operational experience
- ✓ Need HA but not extreme performance
- ✓ <1M msg/sec throughput
- ✓ Want gradual migration path to NATS later

**Choose NATS if**:
- ✓ Multi-instance deployment (4+ instances)
- ✓ Need <0.5ms p99 latency
- ✓ Need >1M msg/sec throughput
- ✓ Want simpler HA (cluster vs Sentinel)
- ✓ Team ready to learn new technology
- ✓ Long-term scalability priority

### Migration Decision Tree

```
┌───────────────────────────────────┐
│ Current: In-Memory BroadcastBus   │
│ Scale: Single instance            │
└────────────┬──────────────────────┘
             │
             ▼
      Need horizontal scaling?
             │
      ┌──────┴──────┐
      │             │
     No            Yes
      │             │
      ▼             ▼
   Stay on      Deploy Redis
   InMemory     (familiar tech)
      │             │
      │             ▼
      │      Operate 3-6 months
      │      Learn patterns
      │             │
      │             ▼
      │      Performance sufficient?
      │             │
      │      ┌──────┴──────┐
      │      │             │
      │     Yes           No
      │      │             │
      │      ▼             ▼
      │   Stay on      Migrate to NATS
      │   Redis        (better perf)
      │      │             │
      └──────┴─────────────┴──────────▶
                   │
                   ▼
            Production Stable
            Monitor & Optimize
```

---

## Summary & Next Steps

### Summary

This architecture document provides a comprehensive plan for implementing a Redis-based broadcast bus with a zero-code-change migration path to NATS.

**Key Achievements**:
1. ✅ **Interface Abstraction**: `BroadcastBus` interface works for InMemory, Redis, and NATS
2. ✅ **Configuration-Based Switching**: Change `BROADCAST_BUS_TYPE` env var → swap implementations
3. ✅ **Production-Ready Redis**: Sentinel HA, health checks, metrics, error handling
4. ✅ **Gradual Migration Path**: InMemory → Redis → NATS with rollback at each stage
5. ✅ **Comprehensive Monitoring**: Prometheus metrics, Grafana dashboards, alerting rules
6. ✅ **Risk Mitigation**: Failure scenarios analyzed, runbooks documented

**Trade-offs**:
- **Redis vs NATS Performance**: +1-2ms latency (acceptable for <10ms SLA)
- **Complexity vs Familiarity**: Redis more familiar → lower operational risk
- **Cost vs Scalability**: +$100/month for 2× capacity (acceptable ROI)

### Immediate Next Steps (Week 1)

**Day 1-2: Interface Refactoring**
```bash
# Create interface and refactor existing code
cd ws/internal/multi
touch broadcast_interface.go
mv broadcast.go broadcast_inmemory.go
touch broadcast_factory.go

# Update main.go to use factory
vim ../cmd/multi/main.go

# Run tests (should all pass)
go test ./...
```

**Day 3-4: Redis Implementation**
```bash
# Add Redis dependency
go get github.com/redis/go-redis/v9

# Implement RedisBroadcastBus
touch ws/internal/multi/broadcast_redis.go
touch ws/internal/multi/broadcast_redis_test.go

# Unit tests
go test ./... -v -run Redis
```

**Day 5: Deploy Redis to GCP**
```bash
# Provision instance
task gcp:redis:provision

# Configure Sentinel
task gcp:redis:configure-sentinel

# Test connectivity
task gcp:redis:test-connection
```

**Week 2-5**: Follow implementation plan in detail (Phase 2-5)

### Success Criteria Checklist

**Phase 1 Complete** (Week 1):
- [ ] `BroadcastBus` interface defined and documented
- [ ] `InMemoryBroadcastBus` refactored (backward compatible)
- [ ] Factory pattern implemented
- [ ] All existing tests pass
- [ ] Config supports `BROADCAST_BUS_TYPE` selection

**Phase 2 Complete** (Week 2):
- [ ] `RedisBroadcastBus` implements `BroadcastBus` interface
- [ ] Connects to Redis Sentinel successfully
- [ ] Unit tests >80% coverage
- [ ] Integration tests with Docker Compose pass

**Phase 3 Complete** (Week 3):
- [ ] Shadow mode: 100% message consistency (in-memory == Redis)
- [ ] Multi-instance test: Both instances receive all messages
- [ ] Failover test: <10s recovery, zero message loss
- [ ] Performance: <10ms p99 end-to-end latency

**Phase 4 Complete** (Week 4):
- [ ] Redis Sentinel deployed to GCP
- [ ] Both staging instances using Redis successfully
- [ ] Production deployed with zero downtime
- [ ] No errors for 24+ hours

**Phase 5 Complete** (Week 5):
- [ ] Prometheus metrics collecting
- [ ] Grafana dashboards deployed
- [ ] Alerting rules tested and working
- [ ] Runbooks reviewed by team
- [ ] Documentation complete

### Future Enhancements

**Phase 6: NATS Migration** (Month 3-6)
- Implement `NatsBroadcastBus`
- Shadow mode testing (Redis + NATS)
- Gradual migration (Redis → NATS)
- Decommission Redis

**Phase 7: Advanced Features** (Month 6-12)
- Message compression (if needed)
- Pipelining/batching optimization
- Circuit breaker pattern
- Multi-region deployment
- Chaos engineering testing

---

## References

**Internal Documentation**:
- `/Volumes/Dev/Codev/Toniq/ws_poc/docs/spikes/SPIKE_EXTERNAL_BROADCAST_BUS.md` - Original NATS vs Redis comparison
- `/Volumes/Dev/Codev/Toniq/ws_poc/docs/architecture/HORIZONTAL_SCALING_PLAN.md` - Multi-instance scaling strategy
- `/Volumes/Dev/Codev/Toniq/ws_poc/ws/internal/multi/broadcast.go` - Current in-memory implementation

**External Resources**:
- Redis Pub/Sub Documentation: https://redis.io/docs/manual/pubsub/
- Redis Sentinel Documentation: https://redis.io/docs/management/sentinel/
- go-redis Library: https://github.com/redis/go-redis
- Prometheus Best Practices: https://prometheus.io/docs/practices/
- NATS Documentation (future): https://docs.nats.io/

**Architecture Decisions**:
- ADR-001: Use Redis first, migrate to NATS later
- ADR-002: BroadcastBus interface for all implementations
- ADR-003: Deploy Redis on separate GCP instance
- ADR-004: Use Redis Sentinel for HA
- ADR-005: Environment variables for configuration switching

---

**Document Version**: 1.0
**Last Updated**: 2025-01-20
**Next Review**: After Phase 2 completion (Week 2)
**Owner**: Architecture Team
**Status**: Approved for Implementation
