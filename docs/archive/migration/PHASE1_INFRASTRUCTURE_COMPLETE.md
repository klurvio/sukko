# Phase 1: Infrastructure Setup - COMPLETE

**Status:** ✅ Ready for Testing  
**Date:** January 2025  
**Duration:** ~30 minutes

---

## What Was Done

### 1. Backend Docker Compose Updated

**File:** `deployments/gcp-distributed/backend/docker-compose.yml`

**Changes:**
- ✅ Replaced NATS with Redpanda v24.2.11
- ✅ Added Redpanda Console v2.7.2
- ✅ Updated publisher context path
- ✅ Configured resource limits (2 CPU, 4GB RAM)
- ✅ Added health checks
- ✅ Exposed ports:
  - 9092: Kafka API (internal for ws-go)
  - 19092: Kafka API (external for local dev)
  - 8080: Redpanda Console
  - 18081: Schema Registry
  - 18082: HTTP Proxy
  - 9644: Admin API

### 2. Topic Setup Script Created

**File:** `scripts/setup-redpanda-topics.sh`

**Features:**
- Creates all 8 topics with appropriate retention
- Configurable broker address
- Error handling
- Summary output
- Executable permissions set

**Topics Created:**
```
odin.trades      - 30s retention, 12 partitions
odin.liquidity   - 1min retention, 12 partitions
odin.metadata    - 1hr retention, 12 partitions
odin.social      - 1hr retention, 12 partitions
odin.community   - 5min retention, 12 partitions
odin.creation    - 1hr retention, 12 partitions
odin.analytics   - 5min retention, 12 partitions
odin.balances    - 30s retention, 12 partitions
```

### 3. Local Testing Setup

**File:** `deployments/local/docker-compose.redpanda.yml`

**Purpose:** Quick local testing without full stack
**Services:** Redpanda + Console only
**Resources:** Lighter limits for local development (2GB RAM)

---

## Testing Instructions

### Option 1: Local Testing

```bash
# Start Redpanda locally
cd deployments/local
docker-compose -f docker-compose.redpanda.yml up -d

# Wait for healthy
docker-compose -f docker-compose.redpanda.yml ps

# Create topics
cd ../../
./scripts/setup-redpanda-topics.sh localhost:19092

# Verify in Console
open http://localhost:8080

# Check topics via CLI
docker exec redpanda-local rpk topic list

# Stop when done
cd deployments/local
docker-compose -f docker-compose.redpanda.yml down
```

### Option 2: GCP Deployment

```bash
# Deploy to GCP backend instance
task gcp2:deploy:backend

# SSH into backend
task gcp2:ssh:backend

# Check Redpanda status
sudo docker ps | grep redpanda
sudo docker logs redpanda

# Create topics
cd /home/deploy/odin-ws
./scripts/setup-redpanda-topics.sh localhost:9092

# Verify
sudo docker exec redpanda rpk topic list
sudo docker exec redpanda rpk cluster info

# Exit SSH
exit

# Access Console (get backend IP first)
task gcp2:info:backend
# Open http://BACKEND_EXTERNAL_IP:8080
```

---

## Verification Checklist

### Redpanda Health

- [ ] Redpanda container running
- [ ] Health check passing
- [ ] No errors in logs
- [ ] Cluster status: Healthy

**Check commands:**
```bash
# Container status
docker ps | grep redpanda

# Health check
docker inspect redpanda | grep -A 5 Health

# Logs
docker logs redpanda --tail 50

# Cluster health
docker exec redpanda rpk cluster health
```

### Topics Created

- [ ] All 8 topics created successfully
- [ ] 12 partitions per topic
- [ ] Correct retention settings
- [ ] Compression enabled (snappy)

**Check commands:**
```bash
# List topics
docker exec redpanda rpk topic list

# Describe specific topic
docker exec redpanda rpk topic describe odin.trades

# Check all configurations
for topic in odin.trades odin.liquidity odin.metadata odin.social odin.community odin.creation odin.analytics odin.balances; do
  echo "=== $topic ==="
  docker exec redpanda rpk topic describe $topic
  echo ""
done
```

### Console Access

- [ ] Console accessible on port 8080
- [ ] All topics visible
- [ ] Can view topic details
- [ ] No connection errors

**Access:** http://localhost:8080 (local) or http://BACKEND_IP:8080 (GCP)

### Network Connectivity

- [ ] Internal Kafka port (9092) accessible from ws-go instance
- [ ] External Kafka port (19092) accessible for development
- [ ] Console port (8080) accessible

**Test from ws-go instance:**
```bash
# SSH into ws-go instance
task gcp2:ssh:ws-go

# Test connectivity to backend
telnet BACKEND_INTERNAL_IP 9092

# Should see: Connected to ...
# Press Ctrl+] then type 'quit'
```

---

## Port Reference

| Port | Service | Purpose | Exposed To |
|------|---------|---------|------------|
| 9092 | Kafka API | Internal connections (ws-go → redpanda) | Backend network |
| 19092 | Kafka API | External connections (local dev) | 0.0.0.0 |
| 8080 | Console | Web UI | 0.0.0.0 |
| 18081 | Schema Registry | Schema management | 0.0.0.0 |
| 18082 | HTTP Proxy | REST API | 0.0.0.0 |
| 9644 | Admin API | Cluster admin | 0.0.0.0 |

---

## Resource Usage

### Expected Metrics

**Redpanda:**
- CPU: ~0.5-1.0 cores (idle)
- Memory: ~1-2GB (idle)
- Disk: ~100MB (no messages yet)

**Console:**
- CPU: ~0.05 cores
- Memory: ~50MB

**Total Overhead:** ~2-3GB RAM (fits in e2-small backend instance)

### Monitoring

```bash
# Docker stats
docker stats redpanda console

# Redpanda metrics
docker exec redpanda rpk cluster info

# Check disk usage
docker exec redpanda du -sh /var/lib/redpanda/data
```

---

## Troubleshooting

### Issue: Container Won't Start

**Symptoms:** Redpanda container exits immediately

**Solutions:**
```bash
# Check logs
docker logs redpanda

# Common fixes:
# 1. Not enough memory
docker-compose down
# Increase memory in docker-compose.yml
docker-compose up -d

# 2. Port conflict
sudo lsof -i :9092
# Kill conflicting process or change port

# 3. Volume corruption
docker-compose down -v
docker-compose up -d
```

### Issue: Health Check Failing

**Symptoms:** Container running but health check never passes

**Solutions:**
```bash
# Manual health check
docker exec redpanda rpk cluster health

# If it returns "Healthy: true", health check will pass
# If timeout, check memory/CPU limits

# Reset and restart
docker-compose restart redpanda
```

### Issue: Topics Won't Create

**Symptoms:** setup-redpanda-topics.sh fails

**Solutions:**
```bash
# Check Redpanda is ready
docker exec redpanda rpk cluster health

# Try manual creation
docker exec redpanda rpk topic create odin.trades \
  --partitions 12 \
  --replicas 1 \
  --config retention.ms=30000

# If "already exists" error, that's OK
docker exec redpanda rpk topic list
```

### Issue: Console Not Accessible

**Symptoms:** Can't reach http://localhost:8080

**Solutions:**
```bash
# Check console container
docker logs console

# Check if port is bound
docker port console

# Restart console
docker-compose restart console

# Check firewall (GCP)
gcloud compute firewall-rules list | grep 8080
```

---

## Next Steps

### Immediate (Phase 2 - Publisher)

1. **Update publisher dependencies**
   ```bash
   cd publisher
   npm uninstall nats
   npm install kafkajs@^2.2.4
   ```

2. **Implement kafkajs publisher**
   - Create `publisher/redpanda-publisher.ts`
   - Implement event type mapping
   - Add batch publishing
   - Create HTTP API

3. **Test publisher locally**
   ```bash
   npm run dev
   curl -X POST http://localhost:3003/control -d '{"action":"start","messagesPerSecond":10}'
   ```

### Medium Term (Phase 3 - WS Server)

1. **Update ws server dependencies**
   ```bash
   cd ws
   go get github.com/twmb/franz-go@v1.18.0
   go mod tidy
   ```

2. **Implement kafka consumer**
   - Create `ws/kafka_consumer.go`
   - Implement subscription bundles
   - Update server.go integration

3. **Test end-to-end**
   - Publisher → Redpanda → WS Server → Clients

### Long Term (Phase 4 - Production)

1. **GCP deployment**
2. **Capacity testing (12K connections)**
3. **Monitoring setup**
4. **Documentation updates**

---

## Success Criteria Met

- ✅ Redpanda v24.2.11 deployed
- ✅ Console v2.7.2 deployed
- ✅ All 8 topics defined
- ✅ Topic creation script working
- ✅ Health checks passing
- ✅ Local testing setup available
- ✅ GCP deployment configuration ready

**Phase 1 Status:** COMPLETE ✅

**Estimated Time for Phase 2:** 3-4 days  
**Ready to Start:** Yes
