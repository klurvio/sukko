# GCP Kafka/Redpanda Deployments

## Overview

This directory contains Kafka-based GCP production deployments that have replaced the previous NATS-based architecture.

**Migration Note:** Old NATS-based deployments are archived in `deployments/old/`.

## Architecture

### Distributed (2 Instances)

**Backend Instance (odin-backend - e2-small)**
- Redpanda (Kafka-compatible broker)
- Redpanda Console (web UI)
- Publisher (event generator)
- Prometheus, Grafana, Loki, Promtail (observability)

**WS Server Instance (odin-ws-go - e2-standard-4)**
- WebSocket server (Kafka consumer)
- Promtail (log shipping to backend Loki)

**Benefits:**
- Isolated resources for WS server
- Full capacity: 12K connections
- Scalable message broker (Kafka)
- Centralized monitoring on backend

**Cost:**
- Backend: e2-small (~$12/month)
- WS Server: e2-standard-4 (~$97/month)
- Total: ~$109/month

## Deployment

See [distributed/README.md](./distributed/README.md) for detailed deployment instructions.

## Key Changes from NATS

### Message Broker
- **Before:** NATS JetStream (in-memory)
- **After:** Redpanda/Kafka (persistent, partitioned)

### Topics
8 Kafka topics with 12 partitions each:
- `odin.trades`
- `odin.liquidity`
- `odin.balances`
- `odin.metadata`
- `odin.social`
- `odin.community`
- `odin.creation`
- `odin.analytics`

### Consumer Model
- Consumer group: `ws-server-production`
- Subscribes to all 8 topics (96 partitions total)
- Auto-commit enabled with 5s interval

### Configuration Changes
- `NATS_URL` → `KAFKA_BROKERS`
- `WS_MAX_NATS_RATE` → `WS_MAX_KAFKA_RATE`
- Added `KAFKA_GROUP_ID` and `KAFKA_TOPICS`

## Monitoring

All monitoring centralized on backend instance:

- **Grafana:** `http://<BACKEND_IP>:3010` (admin/admin)
- **Prometheus:** `http://localhost:9091` (backend local only)
- **Redpanda Console:** `http://<BACKEND_IP>:8080`
- **Loki:** `http://<BACKEND_IP>:3100` (API endpoint)

Pre-configured dashboards:
- WebSocket Performance (metrics from remote WS server)
- System Logs (logs from both instances)

## Scaling

To scale WS server instances:

1. Create additional e2-standard-4 instances
2. Deploy `ws-server` compose on each with same `KAFKA_GROUP_ID`
3. Kafka automatically distributes partitions across consumers
4. Add load balancer in front for WebSocket connections

Each instance will consume a subset of the 96 partitions.

## Related Documentation

- [Capacity Planning](../../docs/CAPACITY_PLANNING.md) - Resource allocations
- [Deployments Reorganization Plan](../../docs/DEPLOYMENTS_REORGANIZATION_PLAN.md) - Migration details
- [Local Development](../local/README.md) - Local Kafka setup
