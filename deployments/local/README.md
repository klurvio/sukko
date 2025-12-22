# Odin WebSocket - Local Development

Complete local development environment mirroring the GCP distributed production setup.

**For GCP production deployments, see [../gcp/](../gcp/)**

## Overview

This setup combines all services from the two-instance GCP deployment into a single Docker Compose environment for local development and testing.

### Services Included

| Service | Purpose | Port | Resources |
|---------|---------|------|-----------|
| **Redpanda** | Kafka/streaming platform | 19092 | 2GB, 1 CPU |
| **Redpanda Console** | Web UI for Redpanda | 8080 | 256MB, 0.2 CPU |
| **Publisher** | Event generator | 3003 | 256MB, 0.3 CPU |
| **WS Server** | WebSocket server (Go) | 3004 | 4GB, 1 CPU |
| **Prometheus** | Metrics collection | 9092 | 256MB, 0.3 CPU |
| **Grafana** | Dashboards & visualization | 3011 | 512MB, 0.2 CPU |
| **Loki** | Log aggregation | 3100 | 256MB, 0.2 CPU |
| **Promtail** | Log shipping | - | 256MB, 0.2 CPU |

**Total Resources**: ~8GB RAM, ~3.7 CPUs

## Prerequisites

- **Docker Desktop**: Version 4.0+ with Docker Compose V2
- **System Resources**: 
  - Minimum: 8GB RAM, 4 CPU cores
  - Recommended: 16GB RAM, 6+ CPU cores
- **Disk Space**: ~5GB for images and volumes
- **OS**: macOS, Linux, or Windows with WSL2

## Quick Start

### 1. Setup (One Command)

```bash
cd deployments/local
./setup-local.sh
```

This script will:
- ✅ Check Docker is running
- ✅ Create `.env.local` from example
- ✅ Start all services
- ✅ Wait for Redpanda to be healthy
- ✅ Create all 8 Redpanda topics
- ✅ Display service URLs

### 2. Start Publishing Events

```bash
# Start publishing 10 events/sec for 3 tokens
curl -X POST http://localhost:3006/start \
  -H "Content-Type: application/json" \
  -d '{"rate": 10, "tokenIds": ["BTC", "ETH", "SOL"]}'
```

### 3. Verify Messages Flowing

```bash
# Check WS server health
curl http://localhost:3005/health

# View WS server logs
docker-compose logs -f ws-server

# Check Redpanda topics
docker exec redpanda-local rpk topic list

# Consume messages from trades topic
docker exec redpanda-local rpk topic consume odin.trades --num 5
```

### 4. Connect WebSocket Client

```javascript
// JavaScript example
const ws = new WebSocket('ws://localhost:3005/ws');

ws.onopen = () => {
  console.log('Connected!');
  // Subscribe to token
  ws.send(JSON.stringify({
    type: 'subscribe',
    data: { symbols: ['BTC'] }
  }));
};

ws.onmessage = (event) => {
  console.log('Message:', JSON.parse(event.data));
};
```

## Manual Setup

If you prefer manual control:

### Step 1: Create Configuration

```bash
cp .env.local.example .env.local
# Edit .env.local as needed
```

### Step 2: Start Services

```bash
docker-compose up -d
```

### Step 3: Create Topics

```bash
# Wait for Redpanda to be ready (30s)
sleep 30

# Create topics
bash ../../scripts/setup-redpanda-topics.sh redpanda-local
```

## Configuration

### Environment Variables

Edit `.env.local` to customize:

```bash
# Capacity
WS_MAX_CONNECTIONS=1000      # Scale up/down based on testing needs

# Resources
WS_CPU_LIMIT=1.0             # CPU cores
WS_MEMORY_LIMIT=4294967296   # 4GB

# Logging
LOG_LEVEL=debug              # debug, info, warn, error
LOG_FORMAT=pretty            # pretty (console) or json (Loki)

# Kafka
KAFKA_BROKERS=redpanda:9092
KAFKA_CONSUMER_GROUP=ws-server-local
```

### Resource Scaling

**For 8GB Machines**:
```bash
WS_MEMORY_LIMIT=2147483648  # 2GB
WS_MAX_CONNECTIONS=500
```

**For 32GB Machines**:
```bash
WS_MEMORY_LIMIT=8589934592  # 8GB
WS_MAX_CONNECTIONS=5000
```

## Service Access

### Web Interfaces

- **Redpanda Console**: http://localhost:8080
  - View topics, messages, consumer groups
  - Monitor cluster health
  
- **Grafana**: http://localhost:3011 (admin/admin)
  - Pre-configured datasources (Prometheus, Loki)
  - Create dashboards for WS server metrics
  
- **Prometheus**: http://localhost:9092
  - Query metrics directly
  - View targets: http://localhost:9092/targets

### APIs

- **Publisher**: http://localhost:3006
  - `POST /start` - Start generating events
  - `POST /stop` - Stop generating events
  - `POST /rate` - Update event rate
  - `GET /status` - Get current status
  - `GET /health` - Health check

- **WS Server**: http://localhost:3005
  - `GET /health` - Health check
  - `GET /metrics` - Prometheus metrics
  - `WS /ws` - WebSocket endpoint

## Common Commands

### View Logs

```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f ws-server
docker-compose logs -f publisher
docker-compose logs -f redpanda

# Follow last 100 lines
docker-compose logs -f --tail=100 ws-server
```

### Check Service Health

```bash
# WS Server
curl http://localhost:3005/health | jq

# Publisher
curl http://localhost:3006/health | jq

# Redpanda
docker exec redpanda-local rpk cluster health
```

### Manage Publisher

```bash
# Start events (10/sec, 3 tokens)
curl -X POST http://localhost:3006/start \
  -H "Content-Type: application/json" \
  -d '{"rate": 10, "tokenIds": ["BTC", "ETH", "SOL"]}'

# Check status
curl http://localhost:3006/status | jq

# Update rate to 100/sec
curl -X POST http://localhost:3006/rate \
  -H "Content-Type: application/json" \
  -d '{"rate": 100}'

# Stop events
curl -X POST http://localhost:3006/stop
```

### Redpanda Operations

```bash
# List topics
docker exec redpanda-local rpk topic list

# Describe topic
docker exec redpanda-local rpk topic describe odin.trades

# Consume messages
docker exec redpanda-local rpk topic consume odin.trades --num 10

# Consumer groups
docker exec redpanda-local rpk group list
docker exec redpanda-local rpk group describe ws-server-local
```

### Restart Services

```bash
# Restart all
docker-compose restart

# Restart specific service
docker-compose restart ws-server

# Rebuild and restart
docker-compose up -d --build ws-server
```

### Stop & Clean Up

```bash
# Stop services (keep data)
docker-compose down

# Stop and remove volumes (clean slate)
docker-compose down -v

# Remove images too
docker-compose down -v --rmi all
```

## Monitoring

### Prometheus Metrics

Key metrics to monitor:
- `ws_active_connections` - Current connections
- `ws_messages_sent_total` - Total messages sent
- `ws_kafka_connected` - Kafka consumer status
- `ws_cpu_usage_percent` - CPU usage
- `ws_memory_usage_bytes` - Memory usage

Query examples:
```promql
# Connection rate
rate(ws_connections_total[5m])

# Message throughput
rate(ws_messages_sent_total[1m])

# CPU usage
ws_cpu_usage_percent

# Memory usage (MB)
ws_memory_usage_bytes / 1024 / 1024
```

### Loki Logs

View logs in Grafana Explore:
```logql
# All WS server logs
{container_name="odin-ws-local"}

# Error logs only
{container_name="odin-ws-local"} |= "ERROR"

# Connection events
{container_name="odin-ws-local"} |= "connection"

# Kafka-related logs
{container_name="odin-ws-local"} |= "kafka"
```

## Troubleshooting

### Services Won't Start

```bash
# Check Docker is running
docker info

# Check resource availability
docker stats

# View specific service logs
docker-compose logs redpanda
docker-compose logs ws-server
```

### Redpanda Unhealthy

```bash
# Check Redpanda logs
docker-compose logs redpanda

# Check cluster health
docker exec redpanda-local rpk cluster health

# Restart Redpanda
docker-compose restart redpanda
```

### WS Server Can't Connect to Redpanda

```bash
# Check if Redpanda is healthy
docker exec redpanda-local rpk cluster health

# Check network
docker network inspect odin-local

# Verify environment variables
docker-compose exec ws-server env | grep KAFKA
```

### Topics Not Created

```bash
# Manually create topics
bash ../../scripts/setup-redpanda-topics.sh redpanda-local

# Verify topics exist
docker exec redpanda-local rpk topic list
```

### Out of Memory

Reduce resource limits in `.env.local`:
```bash
WS_MEMORY_LIMIT=2147483648  # 2GB
WS_MAX_CONNECTIONS=500
```

Or increase Docker Desktop memory allocation:
- Docker Desktop → Settings → Resources → Memory

### Port Conflicts

If ports are already in use, modify `docker-compose.yml`:
```yaml
ports:
  - "8081:8080"  # Change 8080 to 8081
```

## Testing

### Load Testing

```bash
# Start high-rate event generation
curl -X POST http://localhost:3006/start \
  -H "Content-Type: application/json" \
  -d '{"rate": 1000, "tokenIds": ["BTC", "ETH", "SOL", "DOGE", "ADA"]}'

# Monitor in Grafana
open http://localhost:3011

# Check metrics
curl http://localhost:3005/metrics
```

### Stress Testing

Use the load test tool:
```bash
cd loadtest
go run main.go \
  -url ws://localhost:3005/ws \
  -connections 1000 \
  -duration 60s
```

## Differences from GCP Production

### Same as Production
- ✅ All services present
- ✅ Same architecture & relationships
- ✅ Same monitoring stack
- ✅ Same port numbers (internally)

### Different from Production
- ⚠️ Lower resource limits (8GB vs 20GB)
- ⚠️ Lower capacity (1K vs 12K connections)
- ⚠️ Single Docker network (vs GCP internal IPs)
- ⚠️ Pretty logs (vs JSON)
- ⚠️ Debug logging (vs Info)

### Not Included
- ❌ Multi-instance deployment
- ❌ GCP-specific networking
- ❌ Production TLS/certificates
- ❌ External load balancers

## Development Workflow

### 1. Make Code Changes

```bash
# Edit WS server code
vim ../../ws/server.go

# Edit publisher code
vim ../../publisher/index.ts
```

### 2. Rebuild & Restart

```bash
# Rebuild specific service
docker-compose build ws-server

# Restart with new build
docker-compose up -d ws-server

# View logs
docker-compose logs -f ws-server
```

### 3. Test Changes

```bash
# Health check
curl http://localhost:3005/health

# Start events
curl -X POST http://localhost:3006/start \
  -H "Content-Type: application/json" \
  -d '{"rate": 10, "tokenIds": ["BTC"]}'

# Connect client and verify
```

### 4. Monitor

- Check logs in real-time
- View metrics in Prometheus
- Create Grafana dashboards
- Query Loki logs

## Migration to GCP

When ready to deploy to GCP:

1. **Update resource limits** in `.env.production`
2. **Split into two docker-compose files** (backend + ws-server)
3. **Update networking** (use internal IPs)
4. **Change log format** to JSON
5. **Reduce log level** to Info
6. **Scale connection capacity** to 12K

See `../gcp-distributed/` for production configuration.

## Support

### Issues

- **Logs**: `docker-compose logs -f [service]`
- **Health**: Check all `/health` endpoints
- **Metrics**: http://localhost:9092/targets
- **Topics**: `docker exec redpanda-local rpk topic list`

### Resources

- **Docker Compose Docs**: https://docs.docker.com/compose/
- **Redpanda Docs**: https://docs.redpanda.com/
- **Prometheus Docs**: https://prometheus.io/docs/
- **Grafana Docs**: https://grafana.com/docs/

## Summary

This local setup provides a complete, production-like environment for:
- ✅ Development & testing
- ✅ Integration testing
- ✅ Load testing
- ✅ Debugging
- ✅ Learning the system

**Start developing**: `./setup-local.sh` and you're ready to go! 🚀
