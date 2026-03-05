# Instance Isolation Plan: ws-go Dedicated Instance

## Objective

**Move ws-go to dedicated e2-small instance** for accurate performance metrics and resource isolation.

## Current State (Single Instance)

```
┌─────────────────────────────────────────────────────┐
│  sukko-server (e2-small, us-central1-a)          │
│  All services on one instance                       │
│                                                      │
│  ┌──────────────────────────────────────────────┐  │
│  │  ws-go         (1.5 CPU, 768M)               │  │
│  │  NATS          (0.25 CPU, 128M)              │  │
│  │  Prometheus    (0.15 CPU, 128M)              │  │
│  │  Grafana       (0.1 CPU, 256M)               │  │
│  │  Loki          (0.1 CPU, 128M)               │  │
│  │  Promtail      (0.1 CPU, 64M)                │  │
│  │  Publisher     (0.1 CPU, 64M)                │  │
│  └──────────────────────────────────────────────┘  │
│                                                      │
│  Total: 2.3 CPU (115%), 1536M (75%)                 │
└─────────────────────────────────────────────────────┘
```

**Issues**:
- Can't measure ws-go performance in isolation
- Resource contention (CPU/memory shared)
- Monitoring services affect ws-go metrics
- Hard to identify bottlenecks

## Target State (Two Instances)

```
┌────────────────────────────────────────────┐
│  sukko-go (e2-small, us-central1-a)     │
│  Dedicated WebSocket server                │
│                                             │
│  ┌─────────────────────────────────────┐  │
│  │  ws-go ONLY                         │  │
│  │  (1.8 CPU, 1792M)                   │  │
│  │  Clean metrics, no interference     │  │
│  └─────────────────────────────────────┘  │
│                                             │
│  Connects to NATS on other instance        │
│  (internal network, low latency)           │
└────────────────────────────────────────────┘
                    │
                    │ Internal VPC
                    │ (NATS client connection)
                    │
                    ▼
┌────────────────────────────────────────────┐
│  sukko-backend (e2-small, us-central1-a)   │
│  Supporting services                       │
│                                             │
│  ┌─────────────────────────────────────┐  │
│  │  NATS          (0.5 CPU, 256M)      │  │
│  │  Prometheus    (0.3 CPU, 256M)      │  │
│  │  Grafana       (0.2 CPU, 512M)      │  │
│  │  Loki          (0.2 CPU, 256M)      │  │
│  │  Promtail      (0.1 CPU, 128M)      │  │
│  │  Publisher     (0.2 CPU, 128M)      │  │
│  └─────────────────────────────────────┘  │
│                                             │
│  Total: 1.5 CPU (75%), 1536M (75%)         │
└────────────────────────────────────────────┘
```

**Benefits**:
- ✅ Clean ws-go metrics (no interference)
- ✅ Full instance resources for ws-go (1.8 CPU, 1792M)
- ✅ Easy to measure: "this instance = ws-go performance"
- ✅ Can scale ws-go independently later (just clone instance)
- ✅ Monitoring services have more resources (no starvation)

---

## Migration Steps

### Prerequisites

1. **Verify current setup**:
   ```bash
   # SSH into current instance
   gcloud compute ssh sukko-server --zone=us-central1-a

   # Check running containers
   docker compose ps

   # Verify NATS connectivity
   docker exec sukko-nats nats server list
   ```

2. **Backup current config**:
   ```bash
   # From local machine
   mkdir -p backups/$(date +%Y%m%d)
   scp sukko-server:/path/to/docker-compose.yml backups/$(date +%Y%m%d)/
   ```

### Step 1: Create New ws-go Instance (30 min)

**1.1 Create instance**:
```bash
gcloud compute instances create sukko-go \
  --machine-type=e2-small \
  --zone=us-central1-a \
  --network=default \
  --subnet=default \
  --tags=ws-server,allow-websocket \
  --boot-disk-size=10GB \
  --boot-disk-type=pd-standard \
  --metadata=startup-script='#!/bin/bash
    # Install Docker
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh

    # Install Docker Compose
    curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
    chmod +x /usr/local/bin/docker-compose
  '
```

**1.2 Wait for instance ready**:
```bash
# Check instance status
gcloud compute instances describe sukko-go --zone=us-central1-a

# Get internal IP (for NATS connection)
WS_GO_INTERNAL_IP=$(gcloud compute instances describe sukko-go \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].networkIP)')

echo "ws-go internal IP: $WS_GO_INTERNAL_IP"

# Get external IP (for testing)
WS_GO_EXTERNAL_IP=$(gcloud compute instances describe sukko-go \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

echo "ws-go external IP: $WS_GO_EXTERNAL_IP"
```

**1.3 Configure firewall rules**:
```bash
# Allow WebSocket traffic (port 3004)
gcloud compute firewall-rules create allow-websocket \
  --network=default \
  --allow=tcp:3004 \
  --source-ranges=0.0.0.0/0 \
  --target-tags=ws-server \
  --description="Allow WebSocket connections to ws-go"

# Allow Prometheus scraping (port 3002 metrics)
gcloud compute firewall-rules create allow-prometheus-scrape \
  --network=default \
  --allow=tcp:3002 \
  --source-tags=monitoring \
  --target-tags=ws-server \
  --description="Allow Prometheus to scrape ws-go metrics"

# Allow internal NATS communication (port 4222)
gcloud compute firewall-rules create allow-nats-internal \
  --network=default \
  --allow=tcp:4222 \
  --source-tags=ws-server \
  --target-tags=backend \
  --description="Allow ws-go to connect to NATS"
```

### Step 2: Update Backend Instance (15 min)

**2.1 SSH into backend instance**:
```bash
gcloud compute ssh sukko-server --zone=us-central1-a
```

**2.2 Add network tags**:
```bash
# From local machine
gcloud compute instances add-tags sukko-server \
  --tags=backend,monitoring \
  --zone=us-central1-a
```

**2.3 Update docker-compose.yml** (remove ws-go, expose NATS):
```bash
# On backend instance
cd /path/to/project
cp docker-compose.yml docker-compose.yml.backup

cat > docker-compose.backend.yml <<'EOF'
services:
  # NATS - now exposed on 0.0.0.0 for external access
  nats:
    image: nats:2.12-alpine
    container_name: sukko-nats
    ports:
      - "0.0.0.0:4222:4222"  # Changed from 127.0.0.1 to allow external access
      - "127.0.0.1:8222:8222"  # Monitoring (local only)
    command:
      - "--jetstream"
      - "--store_dir=/data"
      - "--http_port=8222"
    volumes:
      - nats_data:/data
    networks:
      - sukko-network
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.5"          # Increased (was 0.25)
          memory: 256M         # Increased (was 128M)

  # Publisher - test traffic generator
  publisher:
    build:
      context: ./publisher
      dockerfile: Dockerfile
    container_name: sukko-publisher
    ports:
      - "127.0.0.1:3003:3003"
    environment:
      - NATS_URL=nats://nats:4222
      - PORT=3003
    networks:
      - sukko-network
    depends_on:
      - nats
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.2"
          memory: 128M

  # Prometheus - scrapes ws-go on different instance
  prometheus:
    image: prom/prometheus:v3.6.0
    container_name: sukko-prometheus
    ports:
      - "127.0.0.1:9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus_data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--storage.tsdb.retention.time=7d'
    networks:
      - sukko-network
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.3"          # Increased (was 0.15)
          memory: 256M         # Increased (was 128M)

  # Grafana
  grafana:
    image: grafana/grafana:12.2.0
    container_name: sukko-grafana
    ports:
      - "0.0.0.0:3010:3000"  # External access for UI
    volumes:
      - grafana_data:/var/lib/grafana
      - ./grafana/provisioning:/etc/grafana/provisioning
    environment:
      - GF_SECURITY_ADMIN_USER=admin
      - GF_SECURITY_ADMIN_PASSWORD=admin
    networks:
      - sukko-network
    depends_on:
      - prometheus
      - loki
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.2"
          memory: 512M         # Increased (was 256M)

  # Loki
  loki:
    image: grafana/loki:3.3.2
    container_name: sukko-loki
    ports:
      - "127.0.0.1:3101:3100"
    volumes:
      - ./loki-config.yml:/etc/loki/loki-config.yml
      - loki_data:/loki
    command: -config.file=/etc/loki/loki-config.yml
    networks:
      - sukko-network
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.2"
          memory: 256M

  # Promtail - collects logs from ws-go via network
  promtail:
    image: grafana/promtail:3.3.2
    container_name: sukko-promtail
    volumes:
      - ./promtail-config.yml:/etc/promtail/promtail-config.yml
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - /var/run/docker.sock:/var/run/docker.sock
    command: -config.file=/etc/promtail/promtail-config.yml
    networks:
      - sukko-network
    depends_on:
      - loki
    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "0.1"
          memory: 128M

networks:
  sukko-network:
    driver: bridge

volumes:
  nats_data:
  prometheus_data:
  grafana_data:
  loki_data:
EOF
```

**2.4 Update Prometheus config** to scrape ws-go remotely:
```bash
cat > prometheus.yml <<EOF
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  # Scrape ws-go on different instance
  - job_name: 'ws-go'
    static_configs:
      - targets: ['${WS_GO_INTERNAL_IP}:3002']
        labels:
          instance: 'sukko-go'
          service: 'websocket'

  # Local services
  - job_name: 'nats'
    static_configs:
      - targets: ['nats:8222']

  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
EOF
```

**2.5 Restart backend services**:
```bash
# Stop old compose (includes ws-go)
docker compose down

# Start new compose (without ws-go)
docker compose -f docker-compose.backend.yml up -d

# Verify NATS is accessible
docker exec sukko-nats nats server list

# Test NATS from external (should work)
# From another terminal on local machine:
curl http://${BACKEND_EXTERNAL_IP}:8222/varz
```

### Step 3: Deploy ws-go on New Instance (30 min)

**3.1 SSH into new ws-go instance**:
```bash
gcloud compute ssh sukko-go --zone=us-central1-a
```

**3.2 Create project directory**:
```bash
mkdir -p ~/ws-poc
cd ~/ws-poc
```

**3.3 Copy source code from local machine**:
```bash
# From local machine
gcloud compute scp --recurse \
  /Volumes/Dev/Codev/Toniq/sukko/src \
  sukko-go:~/ws-poc/ \
  --zone=us-central1-a

gcloud compute scp \
  /Volumes/Dev/Codev/Toniq/sukko/docker-compose.yml \
  sukko-go:~/ws-poc/ \
  --zone=us-central1-a
```

**3.4 Create ws-go-only docker-compose**:
```bash
# On ws-go instance
cd ~/ws-poc

# Get backend internal IP
BACKEND_INTERNAL_IP=$(gcloud compute instances describe sukko-server \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].networkIP)')

cat > docker-compose.wsgo.yml <<EOF
services:
  ws-go:
    build:
      context: ./src
      dockerfile: Dockerfile
    container_name: sukko-go
    ports:
      - "0.0.0.0:3004:3002"  # WebSocket + metrics port
    command:
      ["./sukko-server", "-addr", ":3002", "-nats", "nats://${BACKEND_INTERNAL_IP}:4222"]
    environment:
      # Full instance resources available
      - WS_CPU_LIMIT=1.8
      - WS_MEMORY_LIMIT=1879048192      # 1792MB
      - WS_MAX_CONNECTIONS=10000
      - WS_WORKER_POOL_SIZE=256
      - WS_WORKER_QUEUE_SIZE=25600
      - WS_MAX_GOROUTINES=25000

      # No rate limiting (test configuration)
      - WS_MAX_NATS_RATE=0
      - WS_MAX_BROADCAST_RATE=0

      # Thresholds
      - WS_CPU_REJECT_THRESHOLD=75.0
      - WS_CPU_PAUSE_THRESHOLD=80.0

      # Logging
      - LOG_LEVEL=info
      - LOG_FORMAT=json

    restart: unless-stopped
    deploy:
      resources:
        limits:
          cpus: "1.8"
          memory: 1792M
        reservations:
          cpus: "1.8"
          memory: 1792M
    ulimits:
      nofile:
        soft: 200000
        hard: 200000
EOF
```

**3.5 Build and start ws-go**:
```bash
# Build image
docker compose -f docker-compose.wsgo.yml build

# Start ws-go
docker compose -f docker-compose.wsgo.yml up -d

# Check logs
docker compose -f docker-compose.wsgo.yml logs -f

# Should see:
# ✅ Connected to NATS at nats://10.x.x.x:4222
# ✅ Subscribed to JetStream: sukko.token.>
# ✅ Server listening on :3002
```

**3.6 Verify connectivity**:
```bash
# Check NATS connection
docker exec sukko-go sh -c "netstat -an | grep 4222"
# Should see: ESTABLISHED connection to backend IP:4222

# Test health endpoint
curl http://localhost:3002/health

# Test from external
# From local machine:
curl http://${WS_GO_EXTERNAL_IP}:3004/health
```

### Step 4: Update Test Scripts (15 min)

**4.1 Update test script to point to new instance**:
```bash
# From local machine
cd /Volumes/Dev/Codev/Toniq/sukko

# Get ws-go external IP
WS_GO_IP=$(gcloud compute instances describe sukko-go \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

# Test connection
WS_URL=ws://${WS_GO_IP}:3004/ws \
TARGET_CONNECTIONS=1000 \
DURATION=60 \
node scripts/sustained-load-test.cjs
```

**4.2 Update Taskfile** (if you have one):
```yaml
# taskfiles/test.yml
vars:
  WS_GO_IP:
    sh: gcloud compute instances describe sukko-go --zone=us-central1-a --format='get(networkInterfaces[0].accessConfigs[0].natIP)'

tasks:
  test:light:
    desc: Test against isolated ws-go instance
    cmds:
      - WS_URL=ws://{{.WS_GO_IP}}:3004/ws TARGET_CONNECTIONS=1000 node scripts/sustained-load-test.cjs
```

### Step 5: Verify Metrics Collection (15 min)

**5.1 Check Prometheus scraping**:
```bash
# Access Grafana
# Get backend external IP
BACKEND_IP=$(gcloud compute instances describe sukko-server \
  --zone=us-central1-a \
  --format='get(networkInterfaces[0].accessConfigs[0].natIP)')

echo "Grafana: http://${BACKEND_IP}:3010"
echo "Prometheus: http://${BACKEND_IP}:9091"

# Open Grafana, check that ws-go metrics are appearing
# Look for: ws_connections_current, ws_messages_sent_total
```

**5.2 Verify log collection**:
```bash
# Check Loki is receiving ws-go logs
# In Grafana → Explore → Loki
# Query: {instance="sukko-go"}
```

---

## Validation Checklist

After migration, verify:

- [ ] ws-go container running on new instance
- [ ] ws-go connected to NATS on backend instance
- [ ] Health endpoint accessible: `http://${WS_GO_IP}:3004/health`
- [ ] WebSocket connections work: `wscat -c ws://${WS_GO_IP}:3004/ws`
- [ ] Prometheus scraping ws-go metrics (check Grafana)
- [ ] Loki collecting ws-go logs (check Grafana Explore)
- [ ] Test script connects successfully
- [ ] NATS messages flowing (publisher → NATS → ws-go → clients)

---

## Rollback Plan

If issues occur:

1. **Stop ws-go on new instance**:
   ```bash
   gcloud compute ssh sukko-go --zone=us-central1-a
   docker compose -f docker-compose.wsgo.yml down
   ```

2. **Restart original setup**:
   ```bash
   gcloud compute ssh sukko-server --zone=us-central1-a
   docker compose -f docker-compose.yml up -d
   ```

3. **Revert test scripts**:
   ```bash
   # Point back to original instance
   WS_URL=ws://${BACKEND_IP}:3004/ws
   ```

---

## Cost Impact

**Before** (1 instance):
- 1× e2-small: $12.23/month
- **Total: $12.23/month**

**After** (2 instances):
- 1× e2-small (ws-go): $12.23/month
- 1× e2-small (backend): $12.23/month
- **Total: $24.46/month** (+$12.23)

**Worth it**: Yes, for accurate metrics and testing isolation. Can consolidate later if needed.

---

## Performance Expectations

**Before** (shared instance):
- ws-go limited to 1.5 CPU, 768M
- Resource contention with monitoring
- Metrics polluted by other services

**After** (dedicated instance):
- ws-go gets full 1.8 CPU, 1792M
- No resource contention
- Clean metrics: "this instance = ws-go only"
- Expected capacity: 10,000 connections easily

**Network latency** (ws-go ↔ NATS):
- Same zone, internal network: <1ms
- No performance impact vs local

---

## Next Steps

1. **Execute migration** (follow steps above)
2. **Run capacity test**:
   ```bash
   TARGET_CONNECTIONS=5000 DURATION=300 task test:capacity
   ```
3. **Collect clean metrics** (no interference)
4. **Document baseline performance** for future scaling decisions
5. **Consider**: If performance is great, this becomes your production template

Ready to start? Let me know if you want me to help with any specific step!
