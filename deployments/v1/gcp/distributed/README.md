# Distributed GCP Deployment (Kafka)

## Architecture

**Two GCP instances:**
1. **Backend (odin-backend - e2-small):** Kafka, Publisher, Monitoring
2. **WS Server (odin-ws-go - e2-standard-4):** WebSocket server

Communication via internal network (10.128.0.0/20).

## Prerequisites

### 1. GCP Instances Created

**Backend Instance:**
- Name: `odin-backend`
- Type: `e2-small` (2 vCPU, 2GB RAM)
- Region: Same as WS server
- OS: Ubuntu 22.04 LTS

**WS Server Instance:**
- Name: `odin-ws-go`
- Type: `e2-standard-4` (4 vCPU, 16GB RAM)
- Region: Same as backend
- OS: Ubuntu 22.04 LTS

### 2. Firewall Rules

**Backend (odin-backend):**
- Port 9092 (Kafka) - Allow from WS server internal IP
- Port 3100 (Loki) - Allow from WS server internal IP
- Port 3010 (Grafana) - Allow from your IP (management)
- Port 8080 (Redpanda Console) - Allow from your IP (management)

**WS Server (odin-ws-go):**
- Port 3004 (WS + Health) - Allow from 0.0.0.0/0 (public)

### 3. Software Installed on Both Instances

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER

# Install Docker Compose
sudo apt install docker-compose-plugin -y

# Reboot (to apply docker group membership)
sudo reboot
```

## Deployment Steps

### Step 1: Deploy Backend

SSH into `odin-backend`:

```bash
# Clone repo (or copy deployment files)
cd ~
git clone <your-repo> odin-ws
cd odin-ws/deployments/gcp/distributed/backend

# Get external IP (for Grafana URL)
export EXTERNAL_IP=$(curl -s ifconfig.me)
echo "Backend External IP: $EXTERNAL_IP"

# Start services
docker compose up -d

# Wait for Redpanda to be healthy
docker ps
docker exec redpanda rpk cluster health
```

### Step 2: Create Kafka Topics

```bash
# Create all 8 topics with 12 partitions each
docker exec redpanda rpk topic create \
  odin.trades odin.liquidity odin.balances odin.metadata \
  odin.social odin.community odin.creation odin.analytics \
  --partitions 12 --replicas 1

# Verify
docker exec redpanda rpk topic list
```

Expected output:
```
NAME            PARTITIONS  REPLICAS
odin.trades     12          1
odin.liquidity  12          1
...
```

### Step 3: Get Backend Internal IP

```bash
# On backend instance
hostname -I | awk '{print $1}'
# Example: 10.128.0.2
```

**Important:** Copy this IP for next step.

### Step 4: Deploy WS Server

SSH into `odin-ws-go`:

```bash
# Clone repo (or copy deployment files)
cd ~
git clone <your-repo> odin-ws
cd odin-ws/deployments/gcp/distributed/ws-server

# Set backend internal IP (use the IP from Step 3)
export BACKEND_INTERNAL_IP=10.128.0.2

# Verify environment variable
echo "BACKEND_INTERNAL_IP: $BACKEND_INTERNAL_IP"

# Start services
docker compose up -d

# Wait for startup
sleep 10
```

### Step 5: Verify Deployment

**On WS Server:**
```bash
# Check containers
docker ps

# Check health
curl localhost:3004/health | jq '.'

# Check logs
docker logs odin-ws-go --tail 50
```

Expected health response:
```json
{
  "healthy": true,
  "checks": {
    "kafka": { "healthy": true },
    "cpu": { "healthy": true, "percentage": 5.2 },
    "memory": { "healthy": true, "percentage": 0.4 },
    ...
  }
}
```

**On Backend:**
```bash
# Check publisher
curl localhost:3003/status | jq '.'

# Check Kafka topics have messages
docker exec redpanda rpk topic consume odin.trades --num 5
```

### Step 6: Access Services

**From your local machine:**

```bash
# Get backend external IP
BACKEND_IP=<backend-external-ip>

# Get WS server external IP
WS_IP=<ws-server-external-ip>

# Open in browser:
# - Grafana: http://$BACKEND_IP:3010 (admin/admin)
# - Redpanda Console: http://$BACKEND_IP:8080
# - WS Health: http://$WS_IP:3004/health
```

## Testing WebSocket Connections

From your local machine:

```javascript
// Test WebSocket connection
const ws = new WebSocket('ws://<WS_IP>:3004/ws?token=BTC');

ws.onopen = () => console.log('Connected');
ws.onmessage = (e) => console.log('Message:', e.data);
ws.onerror = (e) => console.error('Error:', e);
```

## Starting/Stopping Services

### Start All Services

**Backend:**
```bash
cd ~/odin-ws/deployments/gcp/distributed/backend
docker compose up -d
```

**WS Server:**
```bash
cd ~/odin-ws/deployments/gcp/distributed/ws-server
export BACKEND_INTERNAL_IP=10.128.0.2
docker compose up -d
```

### Stop All Services

**Backend:**
```bash
docker compose down
```

**WS Server:**
```bash
docker compose down
```

### Restart After Code Changes

**Rebuild WS server:**
```bash
cd ~/odin-ws
git pull
cd deployments/gcp/distributed/ws-server
export BACKEND_INTERNAL_IP=10.128.0.2
docker compose up -d --build
```

## Monitoring

### Grafana Dashboards

Access: `http://<BACKEND_IP>:3010`
- Username: `admin`
- Password: `admin`

Dashboards:
- **WebSocket Performance:** Real-time metrics from WS server
- **System Logs:** Aggregated logs from both instances

### Prometheus Metrics

Access: `http://localhost:9091` (backend instance only)

Scrape targets:
- WS Server (remote): `<WS_INTERNAL_IP>:3004`
- Redpanda: `redpanda:9644`
- Publisher: `publisher:3003`
- Prometheus: `localhost:9090`

### Logs

**View WS server logs:**
```bash
# On ws-server instance
docker logs -f odin-ws-go

# Or via Grafana Explore
```

**View publisher logs:**
```bash
# On backend instance
docker logs -f odin-publisher
```

## Troubleshooting

### WS Server Can't Connect to Kafka

**Symptom:** WS server logs show "connection refused" errors

**Check:**
```bash
# On WS server instance
echo $BACKEND_INTERNAL_IP
# Should print: 10.128.0.2

# Test connectivity
ping <BACKEND_INTERNAL_IP>
telnet <BACKEND_INTERNAL_IP> 9092
```

**Fix:**
- Verify firewall allows port 9092 from WS server IP
- Verify BACKEND_INTERNAL_IP is set correctly
- Restart WS server with correct IP

### Publisher Not Generating Events

**Check:**
```bash
# On backend instance
curl localhost:3003/status

# Should show:
# { "isRunning": true, "currentRate": 50 }
```

**Start publisher:**
```bash
curl -X POST localhost:3003/start -H "Content-Type: application/json" \
  -d '{"rate": 50, "tokenIds": ["BTC", "ETH", "SOL"]}'
```

### No Messages in Kafka

**Check topics:**
```bash
docker exec redpanda rpk topic list
docker exec redpanda rpk topic consume odin.trades --num 5
```

**Recreate topics if needed:**
```bash
# Delete topics
docker exec redpanda rpk topic delete odin.trades
# ... repeat for all topics

# Recreate
docker exec redpanda rpk topic create odin.trades --partitions 12
# ... repeat for all topics
```

### Grafana Not Showing Metrics

**Check Prometheus targets:**
```bash
# On backend instance
curl localhost:9091/api/v1/targets | jq '.data.activeTargets[] | {job: .labels.job, health: .health}'
```

All targets should show `"health": "up"`.

**Check WS server metrics endpoint:**
```bash
# From backend instance
curl http://<WS_INTERNAL_IP>:3004/metrics
```

Should return Prometheus metrics.

## Scaling

### Adding More WS Server Instances

1. Create additional e2-standard-4 instances
2. Install Docker and docker-compose
3. Deploy same ws-server compose file
4. Use same `KAFKA_GROUP_ID=ws-server-production`
5. Kafka automatically distributes partitions

### Load Balancing

Use GCP Load Balancer:
- Type: TCP Load Balancer (Layer 4)
- Backend: All WS server instances (port 3004)
- Health check: HTTP on `/health`
- Session affinity: Client IP (5-tuple hash)

## Cost Optimization

### Reduce Backend Resources

If Redpanda/Publisher using too much memory:
- Reduce Redpanda memory: `--memory 2G` → `--memory 1G`
- Reduce publisher rate: 50 events/sec → 10 events/sec

### Preemptible Instances

For non-production:
- Use preemptible instances (60-91% cheaper)
- Backend can tolerate restarts
- WS server needs graceful shutdown handling

## Backup and Disaster Recovery

### Kafka Data

Redpanda data stored in Docker volume `redpanda_data`.

**Backup:**
```bash
docker run --rm -v redpanda_data:/data -v $(pwd):/backup \
  ubuntu tar czf /backup/redpanda-backup.tar.gz /data
```

**Restore:**
```bash
docker run --rm -v redpanda_data:/data -v $(pwd):/backup \
  ubuntu tar xzf /backup/redpanda-backup.tar.gz -C /
```

### Grafana Dashboards

Stored in Docker volume `grafana_data`.

Same backup/restore process as Redpanda.

## Related Documentation

- [GCP Overview](../README.md)
- [Capacity Planning](../../../docs/CAPACITY_PLANNING.md)
- [Local Development](../../local/README.md)
