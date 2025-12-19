# Single Redis Node Deployment (Simplified)

**Purpose**: Deploy single Redis instance for BroadcastBus (development/initial production)

**Architecture**: 1 standalone Redis server (no Sentinel, no replication)

**Estimated Time**: 15-30 minutes

**When to use this**:
- ✅ Development/staging environments
- ✅ Initial production (simple, fast to deploy)
- ✅ Budget-conscious deployments
- ✅ Learning/testing Redis integration

**When to upgrade to Sentinel**:
- You need high availability (automatic failover)
- Downtime during Redis restart is unacceptable
- Production traffic is critical (no tolerance for Redis failures)

---

## Architecture

```
┌─────────────────────────────────────────┐
│         GCP us-central1-a               │
│                                         │
│  ┌──────────────────────────────────┐  │
│  │       redis-node                 │  │
│  │  ─────────────────────────────   │  │
│  │  Redis Server (Standalone)       │  │
│  │    :6379                         │  │
│  │                                  │  │
│  │  10.128.0.10                     │  │
│  └──────────────────────────────────┘  │
│              │                          │
└──────────────┼──────────────────────────┘
               │
    ┌──────────┴──────────┐
    │  ws-server fleet    │
    │  (1+ instances)     │
    └─────────────────────┘
```

**Trade-offs**:
- ✅ **Pros**: Simple, cheap ($49/month), fast to deploy, easy to debug
- ⚠️ **Cons**: No automatic failover, downtime during Redis restart (~5s), single point of failure

**Important**: The ws-server code already supports both single-node and Sentinel modes. Upgrading later is just a config change.

---

## Quick Start (15 minutes)

### Step 1: Generate Password

```bash
# Generate strong password
export REDIS_PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-32)
echo "REDIS_PASSWORD=$REDIS_PASSWORD"

# Save securely
echo "REDIS_PASSWORD=$REDIS_PASSWORD" >> ~/.redis-credentials
chmod 600 ~/.redis-credentials
```

---

### Step 2: Create GCP Instance

```bash
# Set GCP project
export GCP_PROJECT_ID="your-project-id"
export GCP_ZONE="us-central1-a"

gcloud config set project $GCP_PROJECT_ID
gcloud config set compute/zone $GCP_ZONE

# Create firewall rule for Redis (internal VPC only)
gcloud compute firewall-rules create allow-redis-internal \
  --direction=INGRESS \
  --priority=1000 \
  --network=default \
  --action=ALLOW \
  --rules=tcp:6379 \
  --source-ranges=10.128.0.0/20 \
  --target-tags=redis-node

# Create Redis VM
gcloud compute instances create redis-node \
  --machine-type=e2-standard-2 \
  --zone=$GCP_ZONE \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=20GB \
  --boot-disk-type=pd-standard \
  --tags=redis-node \
  --metadata=startup-script='#!/bin/bash
    apt-get update
    apt-get install -y redis-server
    systemctl stop redis-server
  '

# Wait for instance to be ready (1-2 minutes)
sleep 120

# Get internal IP
export REDIS_IP=$(gcloud compute instances describe redis-node \
  --zone=$GCP_ZONE \
  --format='get(networkInterfaces[0].networkIP)')

echo "Redis internal IP: $REDIS_IP"
# Expected: 10.128.0.10 or similar
```

---

### Step 3: Configure Redis

```bash
# SSH into the Redis VM
gcloud compute ssh redis-node --zone=$GCP_ZONE

# Once inside the VM, configure Redis
sudo tee /etc/redis/redis.conf > /dev/null <<'EOF'
# Network
bind 0.0.0.0
protected-mode no
port 6379

# Security
requirepass PLACEHOLDER_PASSWORD

# Persistence (disabled for performance)
# For market data, prioritize latency over durability
save ""
appendonly no

# Performance
maxmemory 1gb
maxmemory-policy allkeys-lru
tcp-backlog 511
tcp-keepalive 300

# Connection pooling
maxclients 10000
timeout 0

# Logging
loglevel notice
logfile /var/log/redis/redis-server.log
EOF

# IMPORTANT: Replace placeholder with actual password
sudo sed -i "s/PLACEHOLDER_PASSWORD/$REDIS_PASSWORD/" /etc/redis/redis.conf

# Set permissions
sudo chown redis:redis /etc/redis/redis.conf
sudo chmod 640 /etc/redis/redis.conf

# Enable and start Redis
sudo systemctl enable redis-server
sudo systemctl start redis-server

# Check status
sudo systemctl status redis-server

# Verify Redis is running
redis-cli -a $REDIS_PASSWORD ping
# Expected: PONG

# Check memory and connections
redis-cli -a $REDIS_PASSWORD INFO server | grep redis_version
redis-cli -a $REDIS_PASSWORD INFO memory | grep used_memory_human
redis-cli -a $REDIS_PASSWORD INFO clients | grep connected_clients

# Exit SSH
exit
```

---

### Step 4: Test Redis

**From your local machine**:

```bash
# Test connection from outside (if firewall allows your IP)
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD ping
# Expected: PONG

# Test write
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD SET test-key "Hello Redis"
# Expected: OK

# Test read
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD GET test-key
# Expected: "Hello Redis"

# Test Pub/Sub
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD PUBLISH ws.broadcast '{"subject":"test","message":"hello"}'
# Expected: (integer) 0  (no subscribers yet, that's OK)

# Check Redis stats
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD INFO stats | grep total_commands_processed
```

✅ **If all tests pass, Redis is ready!**

---

## Connect ws-server to Redis

### Update Configuration

**Single Redis node uses this format**:
```bash
# IMPORTANT: Single address (no commas)
REDIS_SENTINEL_ADDRS=10.128.0.10:6379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<your-password>
REDIS_DB=0
REDIS_CHANNEL=ws.broadcast
```

**The code automatically detects single address and uses direct connection** (not Sentinel mode).

### Update .env file

```bash
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws

# Create/update .env
cat >> .env <<EOF
# Redis Configuration (Single Node)
REDIS_SENTINEL_ADDRS=$REDIS_IP:6379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=$REDIS_PASSWORD
REDIS_DB=0
REDIS_CHANNEL=ws.broadcast
EOF
```

### Test Connection

```bash
# Run ws-server locally
cd /Volumes/Dev/Codev/Toniq/odin-ws/ws
go run cmd/multi/main.go

# Expected logs:
# [INFO] Connecting to Redis (direct mode)
# [INFO] mode="direct" addr="10.128.0.10:6379"
# [INFO] Successfully connected to Redis
# [INFO] BroadcastBus initialized (Redis mode: 1 sentinel addrs)
# [INFO] BroadcastBus started (Redis Pub/Sub)
# [INFO] Redis receive loop started
```

✅ **If you see these logs, ws-server is connected to Redis!**

---

## Test Multi-Instance Broadcast

### Deploy 2 ws-server Instances

**Instance 1** (local or GCP VM):
```bash
export REDIS_SENTINEL_ADDRS="10.128.0.10:6379"
export REDIS_PASSWORD="<your-password>"

go run cmd/multi/main.go -lb-addr=:3005
# Listens on :3005 for WebSocket connections
```

**Instance 2** (different terminal or VM):
```bash
export REDIS_SENTINEL_ADDRS="10.128.0.10:6379"
export REDIS_PASSWORD="<your-password>"

go run cmd/multi/main.go -lb-addr=:3006
# Listens on :3006 for WebSocket connections
```

### Test Cross-Instance Messaging

**Terminal 1** (connect to Instance 1):
```bash
wscat -c ws://localhost:3005
Connected to ws://localhost:3005

# Subscribe to a topic
> {"action":"subscribe","topic":"odin.token.BTC.trade"}
```

**Terminal 2** (connect to Instance 2):
```bash
wscat -c ws://localhost:3006
Connected to ws://localhost:3006

# Subscribe to same topic
> {"action":"subscribe","topic":"odin.token.BTC.trade"}
```

**Terminal 3** (simulate Kafka message to Instance 1):
```bash
# Publish to Redis (simulating Kafka → Instance 1 → Redis)
redis-cli -h 10.128.0.10 -a $REDIS_PASSWORD PUBLISH ws.broadcast \
  '{"subject":"odin.token.BTC.trade","message":{"price":50000,"volume":1.5}}'
```

**Expected Result**:
- Both Terminal 1 (Instance 1) AND Terminal 2 (Instance 2) receive the message ✅
- This proves cross-instance broadcast is working!

---

## Monitoring

### Daily Health Checks

```bash
# Check Redis is running
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="systemctl status redis-server"

# Check memory usage
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="redis-cli -a $REDIS_PASSWORD INFO memory | grep used_memory_human"

# Check connected clients (should match number of ws-server instances)
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="redis-cli -a $REDIS_PASSWORD INFO clients | grep connected_clients"

# Check total commands processed
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="redis-cli -a $REDIS_PASSWORD INFO stats | grep total_commands_processed"
```

### Set Up GCP Monitoring Alerts

```bash
# Alert if Redis is down
gcloud alpha monitoring policies create \
  --notification-channels=<your-channel> \
  --display-name="Redis Down" \
  --condition-display-name="Redis process not running" \
  --condition-threshold-value=1 \
  --condition-threshold-duration=60s

# Alert if Redis memory >900MB (approaching 1GB limit)
gcloud alpha monitoring policies create \
  --notification-channels=<your-channel> \
  --display-name="Redis Memory High" \
  --condition-threshold-value=943718400 \
  --condition-threshold-duration=300s
```

---

## Maintenance

### Restart Redis (Planned Downtime)

```bash
# Restart Redis (causes ~5 seconds downtime for ws-server)
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="sudo systemctl restart redis-server"

# ws-server will automatically reconnect within 5-10 seconds
# Check ws-server logs for "Successfully reconnected to Redis Pub/Sub"
```

### Update Redis Configuration

```bash
# SSH into Redis VM
gcloud compute ssh redis-node --zone=$GCP_ZONE

# Edit config
sudo nano /etc/redis/redis.conf

# Apply changes
sudo systemctl restart redis-server

# Verify
redis-cli -a $REDIS_PASSWORD CONFIG GET maxmemory
```

### Backup Redis Data (Optional)

```bash
# Manual backup
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="redis-cli -a $REDIS_PASSWORD SAVE"

# Download backup
gcloud compute scp redis-node:/var/lib/redis/dump.rdb ./redis-backup.rdb \
  --zone=$GCP_ZONE
```

---

## Troubleshooting

### Problem: ws-server can't connect to Redis

**Check 1**: Verify Redis is running
```bash
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="systemctl status redis-server"
```

**Check 2**: Verify password is correct
```bash
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD ping
```

**Check 3**: Check firewall rules
```bash
gcloud compute firewall-rules describe allow-redis-internal
# Verify source-ranges includes ws-server IPs
```

**Check 4**: Test connectivity from ws-server
```bash
# From ws-server VM
telnet 10.128.0.10 6379
# Should connect successfully
```

---

### Problem: Redis memory usage at 100%

```bash
# Check memory
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD INFO memory | grep maxmemory

# Flush old data (CAUTION: deletes all data)
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD FLUSHALL

# Or increase maxmemory limit
sudo sed -i 's/maxmemory 1gb/maxmemory 2gb/' /etc/redis/redis.conf
sudo systemctl restart redis-server
```

---

### Problem: High latency (>10ms)

**Check 1**: Redis CPU usage
```bash
gcloud compute ssh redis-node --zone=$GCP_ZONE \
  --command="top -bn1 | grep redis-server"
```

**Check 2**: Network latency
```bash
# From ws-server VM
ping -c 10 10.128.0.10
# Should be <1ms within same zone
```

**Check 3**: Redis slowlog
```bash
redis-cli -h $REDIS_IP -a $REDIS_PASSWORD SLOWLOG GET 10
# Shows commands taking >10ms
```

**Solution**: Usually needs vertical scaling (upgrade to e2-standard-4)

---

## Upgrading to Redis Sentinel (3-Node HA)

**When you're ready for high availability**:

1. **Deploy 2 more Redis nodes** (redis-node-2, redis-node-3)
2. **Configure replication** (node-2 and node-3 as replicas)
3. **Deploy Sentinel** on all 3 nodes
4. **Update ws-server config**:
   ```bash
   # Change from:
   REDIS_SENTINEL_ADDRS=10.128.0.10:6379

   # To:
   REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379
   ```
5. **Rolling restart ws-server** - code automatically switches to Sentinel mode!

**Zero code changes needed** - the detection logic handles it automatically.

---

## Cost Analysis

### Single Node Costs (GCP us-central1)

| Resource | Specs | Cost/Month |
|----------|-------|------------|
| **e2-standard-2** | 2 vCPU, 8GB RAM | $49.35 |
| **Persistent Disk** | 20GB SSD | $3.40 |
| **Network** | Internal only | $0 |
| **Total** | | **$52.75/month** |

**Annual**: $633/year

**vs 3-Node Sentinel**: Save $105/month (67% cheaper)

---

## Security Checklist

✅ **Password protection**: Strong random password (32 chars)
✅ **Firewall**: VPC-only access (no public internet)
✅ **Network**: `bind 0.0.0.0` with firewall protection (not `bind 127.0.0.1`)
✅ **Encryption**: Internal GCP network is encrypted by default

**Optional hardening**:
- Disable SSH password auth (key-only)
- Set up Cloud Armor (DDoS protection)
- Enable VPC Service Controls (isolate Redis in secure perimeter)

---

## Docker Compose Alternative (Local Development)

**For local testing before GCP deployment**:

```yaml
# docker-compose.redis.yml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    container_name: redis-local
    ports:
      - "6379:6379"
    command: >
      redis-server
      --requirepass testpassword
      --maxmemory 256mb
      --maxmemory-policy allkeys-lru
      --save ""
      --appendonly no
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "testpassword", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
```

**Usage**:
```bash
# Start Redis locally
docker-compose -f docker-compose.redis.yml up -d

# Configure ws-server
export REDIS_SENTINEL_ADDRS=localhost:6379
export REDIS_PASSWORD=testpassword

# Run ws-server
go run cmd/multi/main.go
```

---

## Summary

✅ **Deployment Time**: 15-30 minutes (simple, fast)
✅ **Cost**: ~$53/month (67% cheaper than 3-node Sentinel)
✅ **Complexity**: Low (1 VM, no Sentinel, no replication)
✅ **Upgrade Path**: Can add Sentinel later with zero code changes
✅ **Best For**: Development, staging, initial production, budget deployments

**Trade-off**: No automatic failover (manual restart needed if Redis crashes)

---

## Next Steps

### Option 1: Test Locally First
```bash
# Use Docker Compose
docker-compose -f docker-compose.redis.yml up -d

# Run ws-server locally
go run cmd/multi/main.go

# Test with wscat
```

### Option 2: Deploy to GCP Immediately
```bash
# Follow Steps 1-4 above (~15 minutes)
# Update ws-server .env
# Deploy ws-server to GCP
# Test cross-instance messaging
```

### Week 3: Multi-Instance Production
- Deploy 2+ ws-server instances on GCP
- Point both to single Redis node
- Load test: 1000 connections per instance
- Monitor Redis CPU/memory

**Ready to deploy!** 🚀

---

**Decision Point**: Start with single node now, upgrade to Sentinel when:
- You have >10K concurrent connections (critical traffic)
- Downtime during Redis restart is unacceptable (<5s is too much)
- You need 99.9% uptime SLA
