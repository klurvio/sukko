# Horizontal Scaling Plan for the Sharded WebSocket Server

## 1. Overview

This document outlines the plan to evolve the sharded WebSocket server from a vertically-scaled, single-machine architecture to a horizontally-scaled, multi-machine cluster. The goal is to create a system that can handle massive numbers of concurrent connections (50,000+) by distributing the load across multiple `e2-standard-4` instances.

## 2. Prerequisites

This plan assumes the successful completion of the **Sharded WebSocket Server Implementation Plan**. The single-machine, multi-core architecture is the foundation upon which this horizontal scaling plan is built.

## 3. Architecture

The horizontally-scaled architecture introduces two key new components: an **External Load Balancer** and an **External Message Bus**.

```
+--------------------------------+
| Google Cloud Load Balancer     |
+--------------------------------+
               |
(Distributes connections to instances)
               |
+--------------------------------+--------------------------------+
|                                |                                |
|      +------------------+      |      +------------------+      |
|      | e2-standard-4 #1 |      |      | e2-standard-4 #2 |      |
|      +------------------+      |      +------------------+      |
|      | - Shard 1        |      |      | - Shard 4        |      |
|      | - Shard 2        |      |      | - Shard 5        |      |
|      | - Shard 3        |      |      | - Shard 6        |      |
|      +------------------+      |      +------------------+      |
|               ^                |                ^               |
|               | (Pub/Sub)      |                | (Pub/Sub)     |
|               v                |                v               |
+--------------------------------+--------------------------------+
               |                                |
               +--------------------------------+
                               |
                 +-----------------------------+
                 |   External Message Bus      |
                 |   (e.g., Redis or NATS)     |
                 +-----------------------------+
```

### Component Roles:

*   **External Load Balancer (e.g., Google Cloud Load Balancer):**
    *   The single entry point for all client connections.
    *   Distributes incoming WebSocket connections across the available server instances.
    *   Performs health checks on the instances and routes traffic only to healthy ones.

*   **WebSocket Server Instances:**
    *   Multiple `e2-standard-4` machines, each running the same multi-core `ws-server` application.
    *   Each instance runs its own set of shards (e.g., 3 shards per instance).
    *   Each shard's Kafka consumer publishes messages to the external message bus.
    *   Each shard subscribes to the external message bus to receive broadcast messages.

*   **External Message Bus (e.g., Redis Pub/Sub or NATS):**
    *   The new backbone for inter-shard communication, replacing the in-memory `BroadcastBus`.
    *   When a shard on any instance receives a message from Kafka, it publishes it to the bus.
    *   All shards on *all* instances are subscribed to the bus, so they all receive the message and can broadcast it to their local clients.

## 4. Implementation Steps

### Step 1: Set up the External Message Bus

> **See**: `docs/spikes/SPIKE_EXTERNAL_BROADCAST_BUS.md` for comprehensive comparison and implementation details.

#### 1.1 Technology Selection: NATS (Recommended)

**Decision**: Use **NATS** over Redis for the external broadcast bus.

**Rationale**:
- **Lower Latency**: <0.5ms p99 (vs 1-2ms for Redis)
- **Simpler HA**: 3-node cluster with automatic failover (vs Redis Sentinel complexity)
- **Higher Throughput**: 10M msg/sec (vs 1M msg/sec for Redis)
- **Purpose-Built**: Designed for messaging (vs general-purpose cache)
- **Lower Resource Usage**: ~300MB RAM (vs ~1.6GB for Redis with Sentinel)

**Cost Comparison**:
```
Phase 1A (Self-Hosted):
  e2-standard-small (2 vCPU, 2GB):   $14/month
  Headroom: 4× CPU, 10× memory

Phase 1B (Managed):
  Synadia Cloud Growth Plan:         $299/month
  Features: 99.95% SLA, auto-scaling, expert support

Alternative (Self-Hosted 3-Node):
  3× e2-standard-small:               $42/month
  Trade-off: Save $257/month but increase ops burden
```

#### 1.2 Phase 1A: Deploy Self-Hosted NATS (Week 1-2)

**Strategy**: Start with a single-node NATS server on e2-standard-small for low-risk validation.

**Deployment Steps**:

```bash
# 1. Provision GCP Instance
gcloud compute instances create nats-server \
  --machine-type=e2-standard-small \
  --zone=us-central1-a \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=20GB \
  --boot-disk-type=pd-standard \
  --tags=nats-server

# 2. Create Firewall Rule
gcloud compute firewall-rules create allow-nats \
  --allow=tcp:4222 \
  --target-tags=nats-server \
  --source-ranges=10.0.0.0/8  # Internal network only

# 3. SSH and Install NATS
gcloud compute ssh nats-server --zone=us-central1-a

# Download and install NATS server
wget https://github.com/nats-io/nats-server/releases/download/v2.10.7/nats-server-v2.10.7-linux-amd64.tar.gz
tar xzf nats-server-v2.10.7-linux-amd64.tar.gz
sudo mv nats-server-v2.10.7-linux-amd64/nats-server /usr/local/bin/

# 4. Create systemd Service
sudo useradd -r -s /bin/false nats
sudo tee /etc/systemd/system/nats.service <<EOF
[Unit]
Description=NATS Server
After=network.target

[Service]
Type=simple
User=nats
ExecStart=/usr/local/bin/nats-server \
  --addr 0.0.0.0 \
  --port 4222 \
  --http_port 8222 \
  --max_payload 1MB \
  --max_pending 100MB \
  --max_connections 10000 \
  --write_deadline 10s
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# 5. Start NATS
sudo systemctl daemon-reload
sudo systemctl enable nats
sudo systemctl start nats

# 6. Verify
curl http://localhost:8222/varz
```

**Monitoring Setup**:

```bash
# Install Prometheus Node Exporter
wget https://github.com/prometheus/node_exporter/releases/download/v1.7.0/node_exporter-1.7.0.linux-amd64.tar.gz
tar xzf node_exporter-1.7.0.linux-amd64.tar.gz
sudo mv node_exporter-1.7.0.linux-amd64/node_exporter /usr/local/bin/

sudo useradd -r -s /bin/false prometheus
sudo tee /etc/systemd/system/node_exporter.service <<EOF
[Unit]
Description=Node Exporter
After=network.target

[Service]
Type=simple
User=prometheus
ExecStart=/usr/local/bin/node_exporter
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable node_exporter
sudo systemctl start node_exporter
```

**Success Criteria**:
- [ ] NATS server running on e2-standard-small
- [ ] ws-server instances can connect to NATS
- [ ] Latency <0.5ms p99 for publish operations
- [ ] Metrics visible in Grafana
- [ ] Handles 2× current load (2K msg/sec) without issues
- [ ] Memory usage <500MB under load

#### 1.3 Phase 1B: Migrate to Managed NATS (Week 6-7)

**Timing**: After 4-6 weeks of validation with self-hosted NATS.

**Option 1: Synadia Cloud (Recommended)**

```bash
# 1. Sign up at https://www.synadia.com/cloud
# 2. Create account and select region (us-central1)
# 3. Create "ws-broadcast" account
# 4. Download connection credentials

# 5. Update environment variables
export NATS_URL="nats://connect.ngs.global:4222"
export NATS_CREDS="/path/to/synadia-credentials.creds"
```

**Features**:
- Multi-region deployment with auto-failover
- 99.95% SLA
- Auto-scaling based on load
- Built-in monitoring and alerting
- Expert support from NATS team

**Migration Steps**:

1. **Parallel Deployment (Day 1-3)**
   - Keep self-hosted NATS running
   - Deploy Synadia Cloud account
   - Configure dual-connection mode in ws-server

2. **Traffic Split (Day 4-5)**
   - 10% → Synadia, 90% → Self-hosted
   - Monitor for 24 hours (latency, errors, delivery)

3. **Gradual Migration (Day 6-7)**
   - Day 6: 25% Synadia, 75% self-hosted
   - Day 7: 50% Synadia, 50% self-hosted

4. **Full Cutover (Week 7, Day 1)**
   - 100% → Synadia Cloud
   - Keep self-hosted running 48 hours as fallback

5. **Decommission (Week 7, Day 3)**
   - Shutdown e2-standard-small instance
   - Net cost increase: $285/month for managed service

**Option 2: Self-Hosted 3-Node Cluster**

If cost-conscious and team has strong ops experience:

```yaml
# Deploy 3 NATS nodes on separate e2-standard-small instances
# Cost: $42/month vs $299/month managed
# Trade-off: Save $257/month but increase ops complexity

# Configuration (each node):
cluster {
  name: ws-broadcast-cluster
  listen: 0.0.0.0:6222
  routes = [
    nats://nats-1.c.project.internal:6222
    nats://nats-2.c.project.internal:6222
    nats://nats-3.c.project.internal:6222
  ]
}

# Client connection string:
export NATS_URL="nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222"
```

### Step 2: Implement NATS BroadcastBus (Week 2)

> **See**: `docs/spikes/SPIKE_EXTERNAL_BROADCAST_BUS.md` lines 680-797 for complete implementation code.

#### 2.1 Create BroadcastBus Interface

Create `ws/internal/multi/broadcast_interface.go`:

```go
package multi

// BroadcastBus defines the interface for broadcasting messages across shards.
// Implementations can be in-memory (single instance) or external (NATS, Redis) for multi-instance.
type BroadcastBus interface {
    // Run starts the broadcast bus's main loop
    Run()

    // Shutdown gracefully stops the broadcast bus
    Shutdown()

    // Publish sends a message to all subscribers
    Publish(msg *BroadcastMessage)

    // Subscribe returns a channel that can be listened on for broadcast messages
    Subscribe() chan *BroadcastMessage
}
```

#### 2.2 Refactor Existing BroadcastBus

Rename `ws/internal/multi/broadcast.go` → `broadcast_inmemory.go`:

```go
package multi

// InMemoryBroadcastBus is the original in-memory implementation.
// Used for single-instance deployments.
type InMemoryBroadcastBus struct {
    publishCh   chan *BroadcastMessage
    subscribers []chan *BroadcastMessage
    // ... existing fields
}

func NewInMemoryBroadcastBus(bufferSize int, logger zerolog.Logger) *InMemoryBroadcastBus {
    // Existing NewBroadcastBus implementation
}

// Ensure InMemoryBroadcastBus implements BroadcastBus interface
var _ BroadcastBus = (*InMemoryBroadcastBus)(nil)
```

#### 2.3 Implement NatsBroadcastBus

Create `ws/internal/multi/broadcast_nats.go`:

```go
package multi

import (
    "context"
    "encoding/json"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/rs/zerolog"
)

type NatsBroadcastBus struct {
    conn    *nats.Conn
    subject string
    logger  zerolog.Logger
    ctx     context.Context
    cancel  context.CancelFunc
}

func NewNatsBroadcastBus(natsURL string, logger zerolog.Logger) (*NatsBroadcastBus, error) {
    // Connect to NATS cluster with auto-reconnect
    conn, err := nats.Connect(natsURL,
        nats.MaxReconnects(-1),              // Infinite reconnects
        nats.ReconnectWait(1*time.Second),
        nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
        nats.PingInterval(20*time.Second),
        nats.MaxPingsOutstanding(2),

        // Handlers for connection state changes
        nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
            logger.Warn().Err(err).Msg("NATS disconnected")
        }),
        nats.ReconnectHandler(func(nc *nats.Conn) {
            logger.Info().
                Str("url", nc.ConnectedUrl()).
                Msg("NATS reconnected")
        }),
        nats.ClosedHandler(func(nc *nats.Conn) {
            logger.Error().Msg("NATS connection closed")
        }),
    )
    if err != nil {
        return nil, err
    }

    ctx, cancel := context.WithCancel(context.Background())

    return &NatsBroadcastBus{
        conn:    conn,
        subject: "ws.broadcast",
        logger:  logger,
        ctx:     ctx,
        cancel:  cancel,
    }, nil
}

func (n *NatsBroadcastBus) Publish(msg *BroadcastMessage) {
    // Serialize message
    data, err := json.Marshal(msg)
    if err != nil {
        n.logger.Warn().Err(err).Msg("Failed to marshal message")
        return
    }

    // Publish to NATS (fire-and-forget, very fast)
    if err := n.conn.Publish(n.subject, data); err != nil {
        n.logger.Warn().Err(err).Msg("Failed to publish to NATS")
    }
}

func (n *NatsBroadcastBus) Subscribe() chan *BroadcastMessage {
    msgCh := make(chan *BroadcastMessage, 1024)

    // Subscribe to NATS subject
    _, err := n.conn.Subscribe(n.subject, func(m *nats.Msg) {
        var msg BroadcastMessage
        if err := json.Unmarshal(m.Data, &msg); err != nil {
            n.logger.Warn().Err(err).Msg("Failed to unmarshal NATS message")
            return
        }

        select {
        case msgCh <- &msg:
            // Message sent to channel
        case <-n.ctx.Done():
            return
        default:
            // Channel full, drop message (backpressure)
            n.logger.Warn().Msg("NATS subscriber channel full, dropping message")
        }
    })

    if err != nil {
        n.logger.Error().Err(err).Msg("Failed to subscribe to NATS")
        close(msgCh)
    }

    return msgCh
}

func (n *NatsBroadcastBus) Run() {
    // NATS handles everything internally, no run loop needed
    n.logger.Info().
        Str("subject", n.subject).
        Str("url", n.conn.ConnectedUrl()).
        Msg("NATS BroadcastBus ready")
}

func (n *NatsBroadcastBus) Shutdown() {
    n.logger.Info().Msg("Shutting down NATS BroadcastBus")
    n.cancel()
    n.conn.Drain() // Gracefully drain pending messages
    n.conn.Close()
}

// Ensure NatsBroadcastBus implements BroadcastBus interface
var _ BroadcastBus = (*NatsBroadcastBus)(nil)
```

#### 2.4 Update Configuration

Add to `ws/internal/shared/platform/config.go`:

```go
type Config struct {
    // ... existing fields

    // Broadcast Bus Configuration
    BroadcastBusType string `env:"BROADCAST_BUS_TYPE" default:"inmemory"` // "inmemory" or "nats"
    NatsURL          string `env:"NATS_URL"`                              // "nats://nats-server:4222"
    NatsCreds        string `env:"NATS_CREDS"`                            // Path to credentials file (for managed NATS)
}
```

#### 2.5 Update Main Entrypoint

Modify `ws/cmd/multi/main.go`:

```go
// Initialize BroadcastBus (select implementation based on config)
var broadcastBus multi.BroadcastBus
busLogger := monitoring.NewLogger(monitoring.LoggerConfig{
    Level:  types.LogLevel(cfg.LogLevel),
    Format: types.LogFormat(cfg.LogFormat),
})

switch cfg.BroadcastBusType {
case "nats":
    logger.Printf("Using NATS BroadcastBus (URL: %s)", cfg.NatsURL)
    natsBus, err := multi.NewNatsBroadcastBus(cfg.NatsURL, busLogger)
    if err != nil {
        logger.Fatalf("Failed to create NATS BroadcastBus: %v", err)
    }
    broadcastBus = natsBus
case "inmemory":
    logger.Printf("Using In-Memory BroadcastBus")
    broadcastBus = multi.NewInMemoryBroadcastBus(1024, busLogger)
default:
    logger.Fatalf("Unknown broadcast bus type: %s", cfg.BroadcastBusType)
}

broadcastBus.Run()
```

#### 2.6 Add Go Module Dependency

```bash
# Add NATS library to go.mod
go get github.com/nats-io/nats.go@latest

# Expected version: v1.31.0 or later
```

**Success Criteria**:
- [ ] BroadcastBus interface defined
- [ ] InMemoryBroadcastBus refactored (backward compatible)
- [ ] NatsBroadcastBus implemented
- [ ] Configuration supports both types
- [ ] main.go selects implementation based on config
- [ ] Unit tests passing for both implementations
- [ ] No breaking changes to existing single-instance deployment

### Step 2.5: Shadow Mode Testing & Gradual Rollout (Week 3-4)

**Critical**: Do NOT skip this step. Shadow mode testing validates NATS without risking production traffic.

#### Phase 1: Shadow Mode (Week 3)

**Objective**: Run NATS in parallel with in-memory bus to verify 100% consistency.

**Implementation**:

```go
// Dual-bus mode configuration
type DualBroadcastBus struct {
    primary   BroadcastBus  // In-memory (current production)
    secondary BroadcastBus  // NATS (under test)
    logger    zerolog.Logger
}

func (d *DualBroadcastBus) Publish(msg *BroadcastMessage) {
    // Publish to both buses
    d.primary.Publish(msg)

    // Publish to NATS (errors logged but don't affect primary)
    go func() {
        d.secondary.Publish(msg)
    }()
}

func (d *DualBroadcastBus) Subscribe() chan *BroadcastMessage {
    // Subscribe to PRIMARY only (in-memory)
    // NATS subscription is just for validation metrics
    return d.primary.Subscribe()
}
```

**Validation Metrics**:

```go
// Add to NATS implementation
type NatsMetrics struct {
    publishLatency   prometheus.Histogram
    publishErrors    prometheus.Counter
    subscribeLatency prometheus.Histogram
    messagesReceived prometheus.Counter
}

// Track latency for every publish
start := time.Now()
err := n.conn.Publish(n.subject, data)
n.metrics.publishLatency.Observe(time.Since(start).Seconds())
```

**Monitoring**:

```bash
# Add Grafana dashboard for shadow mode
# Compare:
# - In-memory latency vs NATS latency
# - Message delivery consistency
# - Error rates

# Expected:
# - NATS latency: <0.5ms p99
# - Error rate: 0%
# - Message consistency: 100%
```

**Success Criteria**:
- [ ] Dual-bus mode running for 48+ hours in dev
- [ ] NATS receives 100% of messages sent to in-memory bus
- [ ] NATS latency <0.5ms p99
- [ ] Zero NATS connection drops
- [ ] Zero errors in NATS logs

#### Phase 2: Gradual Rollout (Week 4)

**Objective**: Gradually shift production traffic from in-memory to NATS.

**Day 1: 10% NATS**

```go
func (bus *HybridBroadcastBus) Publish(msg *BroadcastMessage) {
    // Random selection: 10% NATS, 90% in-memory
    if rand.Intn(100) < 10 {
        bus.nats.Publish(msg)
    } else {
        bus.inmemory.Publish(msg)
    }
}
```

**Monitoring**: Watch for 24 hours. Rollback if:
- Error rate >1%
- Latency >10ms p99
- Any client disconnections

**Day 2: 25% NATS** (if Day 1 successful)
**Day 3: 50% NATS** (if Day 2 successful)
**Day 4: 75% NATS** (if Day 3 successful)
**Day 5: 100% NATS** (if Day 4 successful)

**Rollback Procedure**:

```bash
# If ANY issues detected, immediate rollback
# 1. Set environment variable
export BROADCAST_BUS_TYPE=inmemory

# 2. Restart ws-server instances (zero downtime)
task gcp:deploy:rolling-restart

# 3. Verify in-memory bus is active
curl http://ws-server:3005/health | jq '.broadcast_bus'
# Expected: {"type": "inmemory", "status": "healthy"}

# 4. Post-mortem: Analyze NATS logs and metrics
```

**Success Criteria**:
- [ ] 100% traffic on NATS for 72+ hours
- [ ] No increase in error rates
- [ ] Latency remains <10ms end-to-end
- [ ] No message loss detected
- [ ] No client disconnections due to bus issues
- [ ] In-memory bus can be cleanly disabled

### Step 3: Configure the External Load Balancer

1.  **Provision a Load Balancer:**
    *   Create a Google Cloud Load Balancer (or equivalent in your cloud provider).
    *   Configure it as a global external TCP/SSL load balancer.

2.  **Configure Health Checks:**
    *   Create a health check that targets the `/health` endpoint of your WebSocket server instances.
    *   The load balancer will use this to determine which instances are healthy and can receive traffic.

3.  **Configure Backend Service and Frontend:**
    *   Create a backend service that includes all your WebSocket server instances.
    *   Create a frontend forwarding rule that directs incoming traffic on the public port (e.g., 443) to the backend service.

### Step 3.5: Configuration & Environment Variables

#### Environment Variables

Create `.env.horizontal` or update existing `.env` files:

```bash
# ========================================
# BROADCAST BUS CONFIGURATION
# ========================================

# Broadcast bus type: "inmemory" (single instance) or "nats" (multi-instance)
BROADCAST_BUS_TYPE=nats

# NATS connection URL
# Single node: nats://nats-server.c.project.internal:4222
# Cluster: nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222
NATS_URL=nats://nats-server.c.project.internal:4222

# NATS credentials file (for managed NATS like Synadia Cloud)
# Leave empty for self-hosted NATS without auth
NATS_CREDS=/path/to/credentials.creds

# ========================================
# KAFKA CONFIGURATION (unchanged)
# ========================================
KAFKA_BROKERS=kafka-1:9092,kafka-2:9092,kafka-3:9092
CONSUMER_GROUP=ws-server-group  # SAME group for all instances!
KAFKA_PARTITIONS=12

# ========================================
# SERVER CONFIGURATION
# ========================================
MAX_CONNECTIONS=18000  # Per instance
MAX_KAFKA_RATE=100000
MAX_BROADCAST_RATE=100000
```

#### Docker Compose Configuration

Create `deployments/v1/docker-compose.horizontal.yml`:

```yaml
version: '3.8'

services:
  ws-multi-1:
    image: ws-server:latest
    environment:
      - BROADCAST_BUS_TYPE=nats
      - NATS_URL=nats://nats-server:4222
      - KAFKA_BROKERS=redpanda:9092
      - CONSUMER_GROUP=ws-server-group
      - LOG_LEVEL=info
    ports:
      - "3005:3005"
    depends_on:
      - nats-server
      - redpanda
    networks:
      - ws-network

  ws-multi-2:
    image: ws-server:latest
    environment:
      - BROADCAST_BUS_TYPE=nats
      - NATS_URL=nats://nats-server:4222
      - KAFKA_BROKERS=redpanda:9092
      - CONSUMER_GROUP=ws-server-group  # Same group!
      - LOG_LEVEL=info
    ports:
      - "3006:3005"
    depends_on:
      - nats-server
      - redpanda
    networks:
      - ws-network

  nats-server:
    image: nats:2.10-alpine
    command:
      - "--addr"
      - "0.0.0.0"
      - "--port"
      - "4222"
      - "--http_port"
      - "8222"
    ports:
      - "4222:4222"
      - "8222:8222"
    networks:
      - ws-network

networks:
  ws-network:
    driver: bridge
```

#### GCP Deployment Configuration

For production GCP deployment, update instance startup script:

```bash
#!/bin/bash
# /etc/systemd/system/ws-server.service

[Unit]
Description=WebSocket Server
After=network.target

[Service]
Type=simple
User=ws-server
WorkingDirectory=/opt/ws-server

# Environment variables
Environment="BROADCAST_BUS_TYPE=nats"
Environment="NATS_URL=nats://nats-server.c.project.internal:4222"
Environment="KAFKA_BROKERS=kafka-1.c.project.internal:9092,kafka-2.c.project.internal:9092"
Environment="CONSUMER_GROUP=ws-server-group"
Environment="LOG_LEVEL=info"

ExecStart=/opt/ws-server/ws-multi \
  --shards=3 \
  --base-port=3002 \
  --lb-addr=:3005

Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

#### Health Check Configuration

Update health endpoint to include broadcast bus status:

```go
// Add to health check endpoint
type HealthResponse struct {
    Status        string `json:"status"`
    BroadcastBus  string `json:"broadcast_bus"`  // "inmemory" or "nats"
    NatsConnected bool   `json:"nats_connected,omitempty"`
    Uptime        string `json:"uptime"`
}

// In health handler
if cfg.BroadcastBusType == "nats" {
    response.NatsConnected = natsBus.conn.IsConnected()
}
```

**Success Criteria**:
- [ ] Environment variables documented and validated
- [ ] Docker Compose supports multi-instance deployment
- [ ] GCP systemd service configured for NATS
- [ ] Health endpoint reports broadcast bus type and status
- [ ] Configuration can switch between inmemory/nats without code changes

### Step 4: Deployment

1.  **Create `docker-compose.horizontal.yml`:**
    *   A new Docker Compose file for deploying the server instances.
    *   This will be similar to `docker-compose.multi.yml`, but will include the necessary environment variables to connect to the external message bus.

2.  **Update `Taskfile.yml`:**
    *   Add tasks for deploying and managing the horizontally-scaled cluster (e.g., `task gcp:deploy:horizontal`, `task gcp:scale:up`).

## 5. Monitoring, Metrics & Alerting

### 5.1 NATS Metrics

**Prometheus Scraping**:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'nats'
    static_configs:
      - targets: ['nats-server:8222']
    metrics_path: '/varz'
    scheme: http
    scrape_interval: 15s
```

**Key Metrics to Monitor**:

```promql
# NATS Server Metrics
nats_server_mem_bytes                # Memory usage
nats_server_cpu_percent              # CPU usage
nats_server_connections              # Active connections
nats_server_in_msgs_total            # Messages received
nats_server_out_msgs_total           # Messages sent
nats_server_in_bytes_total           # Bytes received
nats_server_out_bytes_total          # Bytes sent

# Custom Application Metrics (add to NatsBroadcastBus)
broadcast_bus_publish_latency_seconds{type="nats"}      # Histogram
broadcast_bus_publish_errors_total{type="nats"}         # Counter
broadcast_bus_messages_published_total{type="nats"}     # Counter
broadcast_bus_messages_received_total{type="nats"}      # Counter
broadcast_bus_connection_status{type="nats"}            # Gauge (1=connected, 0=disconnected)
```

### 5.2 Grafana Dashboards

**Dashboard 1: NATS Health Overview**

Panels:
- Connection status (green=healthy, red=disconnected)
- Messages per second (line chart)
- Publish latency p50/p95/p99 (line chart)
- Memory usage (line chart)
- Error rate (counter)

**Dashboard 2: Broadcast Bus Comparison** (for shadow mode)

Panels:
- In-memory vs NATS latency comparison
- Message delivery consistency (%)
- Error rates by bus type
- Throughput comparison (msg/sec)

**Dashboard 3: Multi-Instance Overview**

Panels:
- Total connections across all instances
- Messages/sec across all instances
- Kafka partition distribution
- NATS messages routed between instances

### 5.3 Alerting Rules

Create `alerts/broadcast_bus.yml`:

```yaml
groups:
  - name: broadcast_bus_alerts
    interval: 30s
    rules:
      # NATS Connection Down
      - alert: NATSConnectionDown
        expr: broadcast_bus_connection_status{type="nats"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "NATS connection down on {{ $labels.instance }}"
          description: "BroadcastBus cannot connect to NATS for 1+ minute"

      # High Publish Latency
      - alert: NATSHighLatency
        expr: histogram_quantile(0.99, broadcast_bus_publish_latency_seconds{type="nats"}) > 0.005
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "NATS publish latency high (p99 > 5ms)"
          description: "p99 latency: {{ $value }}s on {{ $labels.instance }}"

      # High Error Rate
      - alert: NATSHighErrorRate
        expr: rate(broadcast_bus_publish_errors_total{type="nats"}[5m]) > 0.01
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "NATS error rate > 1%"
          description: "Error rate: {{ $value }} errors/sec on {{ $labels.instance }}"

      # Message Delivery Inconsistency (shadow mode)
      - alert: BroadcastBusInconsistency
        expr: |
          abs(
            rate(broadcast_bus_messages_published_total{type="inmemory"}[5m]) -
            rate(broadcast_bus_messages_published_total{type="nats"}[5m])
          ) / rate(broadcast_bus_messages_published_total{type="inmemory"}[5m]) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Broadcast bus message inconsistency detected"
          description: "In-memory and NATS message rates differ by >5%"

      # NATS Memory High
      - alert: NATSMemoryHigh
        expr: nats_server_mem_bytes > 500000000  # 500MB
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "NATS memory usage high (>500MB)"
          description: "Memory: {{ humanize $value }} on {{ $labels.instance }}"
```

### 5.4 Logging

**Structured Logging Format**:

```go
// In NatsBroadcastBus
n.logger.Info().
    Str("subject", n.subject).
    Str("url", n.conn.ConnectedUrl()).
    Int("servers", len(n.conn.Servers())).
    Msg("NATS BroadcastBus connected")

// On disconnect
n.logger.Warn().
    Err(err).
    Str("last_url", n.conn.ConnectedUrl()).
    Msg("NATS disconnected, will auto-reconnect")

// On publish error
n.logger.Warn().
    Err(err).
    Str("subject", n.subject).
    Int("msg_size", len(data)).
    Msg("Failed to publish to NATS")
```

**Log Aggregation**:

```bash
# Use GCP Cloud Logging or ELK stack
# Filter by component: "broadcast_bus"
# Alert on:
# - ERROR level logs
# - "NATS disconnected" warnings (if sustained >1 min)
# - "Failed to publish" warnings (if rate >10/min)
```

**Success Criteria**:
- [ ] Prometheus scraping NATS metrics
- [ ] Grafana dashboards created and deployed
- [ ] Alerting rules configured in Prometheus/Alertmanager
- [ ] Structured logging implemented in NatsBroadcastBus
- [ ] Alerts tested (trigger intentional disconnect, verify alert fires)
- [ ] Runbooks created for each alert type

## 6. Testing & Validation

### 6.1 Unit Tests

**Test Coverage Requirements**:

```go
// ws/internal/multi/broadcast_nats_test.go
func TestNatsBroadcastBus_PublishSubscribe(t *testing.T)
func TestNatsBroadcastBus_Reconnection(t *testing.T)
func TestNatsBroadcastBus_MessageSerialization(t *testing.T)
func TestNatsBroadcastBus_Shutdown(t *testing.T)

// ws/internal/multi/broadcast_inmemory_test.go
func TestInMemoryBroadcastBus_BackwardCompatibility(t *testing.T)

// Target: >80% coverage for broadcast_*.go files
```

### 6.2 Integration Tests

**Multi-Instance Message Delivery**:

```bash
# Test scenario: 2 instances, 1 NATS server
# 1. Start NATS server
# 2. Start ws-multi instance 1 (connects 1000 clients)
# 3. Start ws-multi instance 2 (connects 1000 clients)
# 4. Publish message to Kafka topic
# 5. Verify ALL 2000 clients receive the message

# Expected:
# - Instance 1 receives message from Kafka (48 partitions)
# - Instance 1 publishes to NATS
# - Instance 2 receives from NATS
# - All clients on both instances get the message
```

**Test Script**:

```bash
#!/bin/bash
# scripts/test/multi-instance-delivery.sh

set -e

echo "Starting multi-instance delivery test..."

# Start infrastructure
task local:up

# Start 2 ws-server instances
BROADCAST_BUS_TYPE=nats NATS_URL=nats://localhost:4222 ./ws-multi --shards=3 --base-port=3002 --lb-addr=:3005 &
BROADCAST_BUS_TYPE=nats NATS_URL=nats://localhost:4222 ./ws-multi --shards=3 --base-port=3012 --lb-addr=:3015 &

sleep 5

# Connect test clients
./test-client --addr=ws://localhost:3005 --count=1000 --subscribe=odin.BTC.trades &
./test-client --addr=ws://localhost:3015 --count=1000 --subscribe=odin.BTC.trades &

sleep 2

# Publish test message to Kafka
echo '{"symbol":"BTC","price":50000}' | kafka-console-producer --topic odin.trades

# Verify all 2000 clients received message
RECEIVED=$(grep "Received message" test-client.log | wc -l)
if [ "$RECEIVED" -eq 2000 ]; then
  echo "✅ SUCCESS: All 2000 clients received message"
else
  echo "❌ FAILURE: Only $RECEIVED/2000 clients received message"
  exit 1
fi
```

### 6.3 Load Tests

**Test 1: Single Instance Baseline**

```bash
# Verify no performance regression
task test:load:12k

# Expected (unchanged from before NATS):
# - 12K connections: ✅
# - Latency <10ms p99: ✅
# - CPU <50%: ✅
# - Memory <2GB: ✅
```

**Test 2: Multi-Instance Scale** (Week 5)

```bash
# 2 instances, 18K connections each = 36K total
task test:load:multi-instance

# Setup:
# - Instance 1: 18K connections
# - Instance 2: 18K connections
# - NATS: e2-standard-small
# - Kafka: 96 partitions distributed 48/48

# Expected:
# - Total: 36K connections
# - Latency <10ms p99 (unchanged)
# - NATS latency <0.5ms p99
# - No message loss
# - Equal distribution: ~18K per instance
```

**Test 3: Massive Scale** (Week 6)

```bash
# 4 instances, 18K connections each = 72K total
task test:load:massive-scale

# Expected:
# - Total: 72K connections
# - Throughput: 4K msg/sec
# - Latency <10ms p99
# - NATS handling 4K msg/sec easily
# - CPU per instance: <50%
# - NATS CPU: <20%
```

### 6.4 Failure Scenarios

**Test 1: NATS Server Restart**

```bash
# While traffic is running:
sudo systemctl restart nats

# Expected:
# - ws-server detects disconnect (<1s)
# - ws-server auto-reconnects (<2s)
# - Total downtime: ~2-3s
# - No client disconnections
# - Message buffer holds messages during reconnect
```

**Test 2: ws-server Instance Failure**

```bash
# Kill one ws-server instance
kill -9 <instance-1-pid>

# Expected:
# - GCP Load Balancer detects failure (health check)
# - New connections route to Instance 2 only
# - Kafka rebalances partitions (all 96 → Instance 2)
# - No message loss during rebalance
# - Clients on failed instance reconnect to Instance 2
```

**Test 3: Kafka Partition Rebalance**

```bash
# Add a 3rd ws-server instance during traffic
BROADCAST_BUS_TYPE=nats ./ws-multi --shards=3 --base-port=3022 --lb-addr=:3025 &

# Expected:
# - Kafka rebalances: 96 partitions → 32/32/32
# - Rebalance completes in <15s
# - No message loss during rebalance
# - NATS continues routing messages across all 3 instances
```

### 6.5 End-to-End Validation

**Acceptance Criteria** (must all pass before production):

- [ ] **Correctness**: 100% message delivery across all instances
- [ ] **Performance**: No latency regression (<10ms p99)
- [ ] **Scalability**: 36K+ connections across 2 instances
- [ ] **Resilience**: Survives NATS restart with <3s recovery
- [ ] **Resilience**: Survives instance failure with zero message loss
- [ ] **Monitoring**: All metrics collecting, alerts firing correctly
- [ ] **Configuration**: Can switch between inmemory/nats via env var
- [ ] **Backward Compatibility**: Single-instance mode still works with inmemory bus
- [ ] **Load Test**: 72K connections sustained for 1+ hour
- [ ] **Failure Test**: Chaos testing (random instance/NATS kills) passes
- [ ] **Rollback Test**: Can rollback to in-memory bus in <2 minutes

**Test Execution Timeline**:

```
Week 3: Unit tests + Integration tests
Week 4: Load test (single instance baseline)
Week 5: Load test (multi-instance scale)
Week 6: Massive scale + Failure scenarios
Week 7: End-to-end validation + Production readiness review
```

## 7. Implementation Timeline

### Recommended 7-Week Phased Rollout

```
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 1-2: Infrastructure Setup (Phase 1A)                          │
├─────────────────────────────────────────────────────────────────────┤
│ • Deploy NATS on e2-standard-small ($14/month)                      │
│ • Setup monitoring (Prometheus, Grafana)                            │
│ • Verify NATS health and metrics                                    │
└─────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 2: Code Implementation                                         │
├─────────────────────────────────────────────────────────────────────┤
│ • Create BroadcastBus interface                                     │
│ • Refactor to InMemoryBroadcastBus                                  │
│ • Implement NatsBroadcastBus                                        │
│ • Add configuration support                                         │
│ • Update main.go for bus selection                                  │
│ • Unit tests (>80% coverage)                                        │
└─────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 3: Shadow Mode Testing                                         │
├─────────────────────────────────────────────────────────────────────┤
│ • Deploy dual-bus mode (publish to both)                            │
│ • Run for 48+ hours in dev                                          │
│ • Verify 100% message consistency                                   │
│ • Monitor latency (<0.5ms target)                                   │
│ • Integration tests                                                  │
└─────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 4: Gradual Rollout                                             │
├─────────────────────────────────────────────────────────────────────┤
│ • Day 1: 10% NATS, 90% in-memory → monitor 24h                     │
│ • Day 2: 25% NATS, 75% in-memory → monitor 24h                     │
│ • Day 3: 50% NATS, 50% in-memory → monitor 48h                     │
│ • Day 4: 75% NATS, 25% in-memory → monitor 24h                     │
│ • Day 5: 100% NATS → monitor 72h                                    │
│ • Rollback plan tested and ready                                    │
└─────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 5: Multi-Instance Deployment                                   │
├─────────────────────────────────────────────────────────────────────┤
│ • Deploy GCP Load Balancer                                          │
│ • Deploy 2nd ws-server instance                                     │
│ • Verify Kafka partition split (48+48)                              │
│ • End-to-end message delivery test                                  │
│ • Load test: 36K connections (18K per instance)                     │
└─────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 6: Massive Scale Testing & Phase 1B Planning                   │
├─────────────────────────────────────────────────────────────────────┤
│ • Load test: 72K connections (4 instances)                          │
│ • Failure scenario testing                                          │
│ • Chaos testing (random failures)                                   │
│ • Evaluate: Self-hosted NATS performance                            │
│ • Decision: Migrate to Synadia Cloud? ($299/month)                  │
└─────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────┐
│ WEEK 7: Phase 1B Migration (Optional) & Production Validation       │
├─────────────────────────────────────────────────────────────────────┤
│ • If migrating: Parallel deployment with Synadia Cloud              │
│ • If staying: Deploy 3-node NATS cluster                            │
│ • Final end-to-end validation                                       │
│ • Production readiness review                                       │
│ • Go/No-Go decision                                                  │
└─────────────────────────────────────────────────────────────────────┘
```

### Decision Points

**Week 2**: Code review and architecture validation
- **Go**: Implementation follows spike document, tests passing
- **No-Go**: Refactor if interface design issues found

**Week 3**: Shadow mode consistency check
- **Go**: 100% message consistency, latency <0.5ms
- **No-Go**: Debug NATS or implementation issues

**Week 4**: Each gradual rollout stage
- **Go**: Error rate <1%, no latency increase
- **No-Go**: Rollback to previous percentage, debug

**Week 6**: Managed NATS decision
- **Go to Synadia**: Team wants SLA, budget approved ($299/month)
- **Go to 3-Node**: Cost-conscious, team has ops capacity
- **Stay Single-Node**: Performance sufficient, low scale needs

### Cost Tracking

```
Phase 1A (Self-Hosted NATS):
  e2-standard-small:              $14/month
  No additional infrastructure costs

Phase 1B Options:
  Option A - Synadia Cloud:       $299/month (managed)
  Option B - 3× e2-standard-small: $42/month (self-hosted HA)
  Option C - Keep single-node:     $14/month (adequate for <50K connections)

Total Infrastructure (2-Instance Deployment):
  2× e2-standard-4 (ws-server):   ~$100/month
  NATS (Phase 1A):                 $14/month
  Kafka (existing):                $0 (already deployed)
  ──────────────────────────────
  Total:                           $114/month

  vs. Single Instance:             $50/month
  Cost increase:                   $64/month (+128%) for 2× capacity
```

## 8. Key Architectural Decisions

### Why NATS Over Redis?

| Criteria | NATS | Redis Pub/Sub |
|----------|------|---------------|
| **Latency** | <0.5ms p99 | 1-2ms p99 |
| **Throughput** | 10M msg/sec | 1M msg/sec |
| **HA Setup** | 3 nodes, auto-cluster | Master+Replicas+Sentinel (complex) |
| **Failover** | <1s automatic | 5-15s (Sentinel election) |
| **Resource Usage** | 300MB RAM | 1.6GB RAM (with Sentinel) |
| **Purpose** | Purpose-built for messaging | General-purpose cache |
| **Decision** | ✅ **Recommended** | ❌ More complex, slower |

### Why Two-Phase Deployment?

**Phase 1A** (self-hosted e2-standard-small):
- **Low risk**: $14/month investment
- **Validation**: Proves NATS works for your workload
- **Learning**: Team gains operational experience
- **Flexibility**: Can stay here if sufficient

**Phase 1B** (managed NATS):
- **Production-ready**: 99.95% SLA
- **Scalability**: Auto-scaling, multi-region
- **Reduced ops**: Focus on application, not infrastructure
- **Expert support**: Direct access to NATS team

### Why Shadow Mode + Gradual Rollout?

**Shadow Mode** (Week 3):
- Validates NATS without risk
- Catches implementation bugs early
- Builds confidence in metrics

**Gradual Rollout** (Week 4):
- Limits blast radius of issues
- Provides rollback points
- Validates at increasing scale

**Alternative** (NOT recommended):
- Direct cutover (high risk)
- All-or-nothing deployment
- No rollback plan

## 9. Success Metrics

### Technical Metrics

- **Latency**: End-to-end <10ms p99 (unchanged from single-instance)
- **NATS Latency**: Publish <0.5ms p99
- **Availability**: 99.9% uptime (NATS reconnect <3s)
- **Scalability**: 36K+ connections across 2 instances
- **Message Delivery**: 100% (zero loss)
- **Error Rate**: <0.01% for NATS operations

### Business Metrics

- **Capacity**: 2× increase (18K → 36K connections)
- **Cost Efficiency**: <$2 per 1000 connections per month
- **Time to Scale**: Add instance in <10 minutes
- **Recovery Time**: Instance failure recovery <30s (via load balancer)

### Operational Metrics

- **MTTR** (Mean Time To Recovery): <5 minutes for NATS issues
- **Alert Response**: <15 minutes for critical alerts
- **Deployment Time**: <2 minutes for rolling restart
- **Rollback Time**: <2 minutes to switch to in-memory bus

## 10. Runbooks & Operations

### Runbook 1: NATS Connection Issues

**Symptoms**: `broadcast_bus_connection_status == 0`, alert firing

**Steps**:
1. Check NATS server status: `systemctl status nats`
2. Check NATS logs: `journalctl -u nats -f`
3. Verify network connectivity: `nc -zv nats-server 4222`
4. If NATS down: `systemctl restart nats` (auto-reconnect <3s)
5. If network issue: Check firewall rules, DNS
6. Escalate if unresolved in 5 minutes

**Rollback**: Switch to in-memory bus if NATS unrecoverable

### Runbook 2: High NATS Latency

**Symptoms**: `broadcast_bus_publish_latency > 5ms p99`

**Steps**:
1. Check NATS CPU/memory: `curl http://nats-server:8222/varz`
2. If high CPU: Check for message rate spike
3. If high memory: Check for client backpressure
4. Review recent Kafka traffic changes
5. Consider scaling NATS (e2-standard-small → e2-standard-2)

**Mitigation**: Traffic shaping, rate limiting

### Runbook 3: Rollback to In-Memory Bus

**Trigger**: Any critical NATS issue, error rate >1%

**Steps**:
```bash
# 1. Update environment variable (all instances)
export BROADCAST_BUS_TYPE=inmemory

# 2. Rolling restart (zero downtime)
task gcp:deploy:rolling-restart

# 3. Verify health (each instance)
curl http://instance-1:3005/health | jq '.broadcast_bus'
# Expected: {"type":"inmemory","status":"healthy"}

# 4. Monitor for 10 minutes
# - No errors
# - Latency normal
# - All connections stable

# 5. Post-mortem
# - Analyze NATS logs
# - Review metrics pre-failure
# - Document root cause
```

**Recovery**: Fix NATS issue, test in dev, redeploy gradually

## 11. Future Considerations

### Autoscaling

Configure GCP instance group autoscaling:
- **Trigger**: CPU >60% for 5 minutes
- **Scale up**: Add instance (Kafka auto-rebalances)
- **Scale down**: Remove instance (drain connections first)
- **Limits**: Min 2, Max 10 instances

### Geo-Distributed Deployment

For global low-latency:
- **US-Central**: 2 instances + NATS cluster
- **EU-West**: 2 instances + NATS cluster
- **Asia-East**: 2 instances + NATS cluster
- **Global**: NATS super-cluster (cross-region sync)

### Multi-Tenancy

Shard NATS subjects by customer:
- `ws.broadcast.customer-a`
- `ws.broadcast.customer-b`
- Isolate traffic, independent scaling

### Advanced Features

- **Message Replay**: JetStream for persistent message history
- **Message Filtering**: NATS subject hierarchy (e.g., `ws.trades.BTC`)
- **Priority Queues**: Separate subjects for high-priority messages
- **A/B Testing**: Route specific users to experimental instances

---

## Summary

This plan evolves the WebSocket server from a single-instance architecture to a horizontally-scaled multi-instance deployment capable of handling 36K-100K+ concurrent connections.

**Key Changes**:
1. **External Message Bus**: NATS replaces in-memory BroadcastBus for cross-instance communication
2. **Interface Abstraction**: BroadcastBus interface enables switching between in-memory/NATS
3. **Phased Rollout**: Shadow mode → gradual rollout → multi-instance deployment
4. **Cost-Effective Start**: $14/month self-hosted NATS → optional $299/month managed service
5. **Zero Downtime**: Rollback capability, auto-reconnection, graceful failover

**Timeline**: 7 weeks from infrastructure setup to production validation

**Cost**: ~$114/month for 2-instance deployment (2× capacity of single instance)

**Risk Mitigation**: Shadow mode testing, gradual rollout, comprehensive monitoring, documented rollback procedures

**Next Steps**: Review this plan, approve Phase 1A deployment, begin Week 1 infrastructure setup.
