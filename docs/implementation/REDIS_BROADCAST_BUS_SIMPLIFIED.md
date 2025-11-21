# Redis Broadcast Bus - Simplified Implementation Plan

**Last Updated**: 2025-11-21
**Status**: Implementation Plan (Revised)

---

## Overview

**Goal**: Replace in-memory BroadcastBus with Redis Pub/Sub for multi-instance horizontal scaling.

**Key Requirements**:
1. **Redis only** - No interface abstraction, direct Redis implementation
2. **High Availability** - Redis Sentinel for automatic failover
3. **Zero-code-change migration** - Self-hosted Redis → GCP Memorystore (connection string change only)

**Why No Interface?**
- Not planning NATS migration (Redis is the long-term solution)
- Simpler codebase (no abstraction overhead)
- Faster implementation (skip factory pattern, interface tests)
- **Still supports managed migration** (Redis Sentinel pattern works for both self-hosted and managed)

---

## Architecture: Redis Sentinel HA

### Topology

```
┌──────────────────────────────────────────────────────────────┐
│                     Redis Sentinel Cluster                    │
│                                                               │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐     │
│  │   Sentinel  │    │   Sentinel  │    │   Sentinel  │     │
│  │   (Node 1)  │    │   (Node 2)  │    │   (Node 3)  │     │
│  │   :26379    │    │   :26379    │    │   :26379    │     │
│  └──────┬──────┘    └──────┬──────┘    └──────┬──────┘     │
│         │                   │                   │            │
│         └───────────────────┼───────────────────┘            │
│                            │                                 │
│         ┌──────────────────┴──────────────────┐             │
│         │                                      │             │
│  ┌──────▼──────┐    ┌─────────────┐    ┌─────▼───────┐    │
│  │    Master   │───▶│   Replica   │◀───│   Replica   │    │
│  │   (Node 1)  │    │   (Node 2)  │    │   (Node 3)  │    │
│  │   :6379     │    │   :6379     │    │   :6379     │    │
│  └─────────────┘    └─────────────┘    └─────────────┘    │
│         ▲                                                    │
│         │ (Automatic Failover)                              │
└─────────┼──────────────────────────────────────────────────┘
          │
    ┌─────┴─────┐
    │ ws-server │
    │ instances │
    └───────────┘
```

**How It Works**:
1. **Sentinel monitors master** - Detects failure within 5 seconds
2. **Quorum vote** - 2/3 Sentinels agree master is down
3. **Automatic promotion** - Sentinel promotes replica to master (7-10 seconds)
4. **Client reconnects** - go-redis auto-discovers new master (no code changes)

**Redundancy**:
- Master crashes: Sentinel promotes replica (<10s downtime)
- Replica crashes: No impact (master still serving)
- Sentinel crashes: Other Sentinels continue monitoring (need 2/3 quorum)

---

## Zero-Code-Change Migration Path

### Self-Hosted Redis Sentinel → GCP Memorystore

**Key Insight**: Both use the **exact same Redis protocol and Sentinel pattern**.

#### Self-Hosted Configuration

```go
// ws/internal/multi/broadcast_redis.go
client := redis.NewFailoverClient(&redis.FailoverOptions{
    MasterName:    "mymaster",
    SentinelAddrs: []string{
        "redis-sentinel-1:26379",
        "redis-sentinel-2:26379",
        "redis-sentinel-3:26379",
    },
    Password: os.Getenv("REDIS_PASSWORD"),
    DB:       0,
})
```

**Environment Variables** (self-hosted):
```bash
REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<secret>
```

#### GCP Memorystore Configuration

**EXACT SAME CODE**. Only env vars change:

```bash
# GCP Memorystore Standard Tier (managed)
REDIS_SENTINEL_ADDRS=10.0.0.3:6379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<gcp-generated-password>
```

**Migration Steps**:
1. Provision GCP Memorystore instance (Terraform or console)
2. Update env vars with new connection string
3. Rolling restart ws-server instances
4. Decommission self-hosted Redis cluster

**Total Time**: 30 minutes
**Code Changes**: ZERO
**Risk**: Very Low (same protocol, tested failover)

---

## Implementation Plan (Revised)

### Phase 1: Implement RedisBroadcastBus (Week 1 - 5 days)

**Goal**: Replace in-memory BroadcastBus with Redis Pub/Sub implementation.

#### Day 1: Refactor Existing Code

**Task**: Prepare existing `broadcast.go` for Redis integration.

**File**: `ws/internal/multi/broadcast.go` → Direct Redis implementation (no rename needed)

**Changes**:
```go
// BEFORE (In-Memory)
type BroadcastBus struct {
	inChan  chan *BroadcastMessage
	outChan chan *BroadcastMessage
	logger  zerolog.Logger
}

func NewBroadcastBus(bufferSize int, logger zerolog.Logger) *BroadcastBus {
	return &BroadcastBus{
		inChan:  make(chan *BroadcastMessage, bufferSize),
		outChan: make(chan *BroadcastMessage, bufferSize),
		logger:  logger,
	}
}

func (b *BroadcastBus) Run() {
	// Fan-out from inChan to outChan
	for msg := range b.inChan {
		select {
		case b.outChan <- msg:
		default:
			b.logger.Warn().Msg("Dropped message (bus full)")
		}
	}
}
```

```go
// AFTER (Redis Pub/Sub)
package multi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type BroadcastBus struct {
	// Redis client (Sentinel failover)
	client *redis.Client
	pubsub *redis.PubSub

	// Pub/Sub config
	channel string // e.g., "ws.broadcast"

	// Subscription management
	subscribers []chan *BroadcastMessage
	subMu       sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Health tracking
	healthy       atomic.Bool
	lastPublish   atomic.Int64 // Unix timestamp
	publishErrors atomic.Uint64

	logger zerolog.Logger
}

type BroadcastMessage struct {
	Subject string `json:"subject"` // e.g., "odin.token.BTC.trade"
	Message []byte `json:"message"` // Raw message bytes
}

// Config for creating BroadcastBus
type BroadcastBusConfig struct {
	// Sentinel configuration
	SentinelAddrs []string // e.g., ["redis-1:26379", "redis-2:26379"]
	MasterName    string   // e.g., "mymaster"
	Password      string
	DB            int

	// Pub/Sub configuration
	Channel    string // e.g., "ws.broadcast"
	BufferSize int    // Subscriber channel buffer

	Logger zerolog.Logger
}

func NewBroadcastBus(cfg BroadcastBusConfig) (*BroadcastBus, error) {
	// Validate config
	if len(cfg.SentinelAddrs) == 0 {
		return nil, fmt.Errorf("REDIS_SENTINEL_ADDRS is required")
	}
	if cfg.MasterName == "" {
		return nil, fmt.Errorf("REDIS_MASTER_NAME is required")
	}
	if cfg.Channel == "" {
		cfg.Channel = "ws.broadcast" // Default channel
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1024 // Default buffer
	}

	// Create Redis Sentinel client
	client := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    cfg.MasterName,
		SentinelAddrs: cfg.SentinelAddrs,
		Password:      cfg.Password,
		DB:            cfg.DB,

		// Connection pooling
		PoolSize:     50,  // Max connections
		MinIdleConns: 10,  // Keep-alive connections

		// Timeouts
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,

		// Retry policy
		MaxRetries:      3,
		MinRetryBackoff: 100 * time.Millisecond,
		MaxRetryBackoff: 1 * time.Second,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis Sentinel: %w", err)
	}

	cfg.Logger.Info().
		Strs("sentinel_addrs", cfg.SentinelAddrs).
		Str("master_name", cfg.MasterName).
		Str("channel", cfg.Channel).
		Msg("Connected to Redis Sentinel")

	// Create context for lifecycle management
	busCtx, busCancel := context.WithCancel(context.Background())

	bus := &BroadcastBus{
		client:      client,
		channel:     cfg.Channel,
		subscribers: make([]chan *BroadcastMessage, 0, 3), // Pre-allocate for 3 shards
		ctx:         busCtx,
		cancel:      busCancel,
		logger:      cfg.Logger,
	}

	bus.healthy.Store(true)
	bus.lastPublish.Store(time.Now().Unix())

	return bus, nil
}
```

**Key Design Decisions**:

1. **Sentinel Failover Client**: Auto-discovers master, handles failover transparently
2. **Connection Pooling**: 50 max connections, 10 keep-alive (reduces latency)
3. **Retry Policy**: 3 retries with exponential backoff (handles transient failures)
4. **Health Tracking**: Atomic counters for publish success/failure (metrics)

---

#### Day 2: Implement Publish Method

**Task**: Implement `Publish()` to send messages to Redis.

```go
// Publish sends a message to Redis Pub/Sub channel
func (b *BroadcastBus) Publish(msg *BroadcastMessage) {
	// Serialize message
	payload, err := json.Marshal(msg)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to serialize message")
		b.publishErrors.Add(1)
		return
	}

	// Publish to Redis (non-blocking with timeout)
	ctx, cancel := context.WithTimeout(b.ctx, 100*time.Millisecond)
	defer cancel()

	if err := b.client.Publish(ctx, b.channel, payload).Err(); err != nil {
		b.logger.Error().
			Err(err).
			Str("channel", b.channel).
			Msg("Failed to publish message to Redis")

		b.publishErrors.Add(1)
		b.healthy.Store(false)
		return
	}

	// Update health tracking
	b.lastPublish.Store(time.Now().Unix())
	b.healthy.Store(true)
}
```

**Why 100ms Timeout?**
- Redis Pub/Sub is fast (<1ms typically)
- 100ms allows for network hiccups
- Prevents blocking Kafka consumer if Redis is slow

---

#### Day 3: Implement Subscribe Method

**Task**: Implement `Subscribe()` to receive messages from Redis.

```go
// Subscribe returns a channel for receiving broadcast messages
// Each shard calls this once to get its subscription channel
func (b *BroadcastBus) Subscribe() chan *BroadcastMessage {
	ch := make(chan *BroadcastMessage, 1024) // Buffered for bursts

	b.subMu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.subMu.Unlock()

	return ch
}

// Run starts the Redis Pub/Sub listener
// Must be called after all Subscribe() calls
func (b *BroadcastBus) Run() {
	// Subscribe to Redis channel
	b.pubsub = b.client.Subscribe(b.ctx, b.channel)

	b.logger.Info().
		Str("channel", b.channel).
		Int("subscribers", len(b.subscribers)).
		Msg("BroadcastBus started (Redis Pub/Sub)")

	b.wg.Add(1)
	go b.receiveLoop()

	// Start health check goroutine
	b.wg.Add(1)
	go b.healthCheckLoop()
}

// receiveLoop receives messages from Redis and fans out to subscribers
func (b *BroadcastBus) receiveLoop() {
	defer b.wg.Done()

	ch := b.pubsub.Channel()

	for {
		select {
		case <-b.ctx.Done():
			b.logger.Info().Msg("BroadcastBus receive loop stopping")
			return

		case redisMsg, ok := <-ch:
			if !ok {
				b.logger.Warn().Msg("Redis Pub/Sub channel closed, reconnecting...")
				b.reconnect()
				continue
			}

			// Deserialize message
			var msg BroadcastMessage
			if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
				b.logger.Error().
					Err(err).
					Str("payload", redisMsg.Payload).
					Msg("Failed to deserialize Redis message")
				continue
			}

			// Fan out to all subscribers (non-blocking)
			b.subMu.RLock()
			for _, sub := range b.subscribers {
				select {
				case sub <- &msg:
					// Success
				default:
					// Subscriber buffer full, drop message
					b.logger.Warn().
						Str("subject", msg.Subject).
						Msg("Dropped message (subscriber buffer full)")
				}
			}
			b.subMu.RUnlock()
		}
	}
}

// reconnect attempts to resubscribe after connection loss
func (b *BroadcastBus) reconnect() {
	backoff := 100 * time.Millisecond
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-time.After(backoff):
			b.logger.Info().Msg("Attempting to reconnect to Redis Pub/Sub...")

			// Close old subscription
			if b.pubsub != nil {
				b.pubsub.Close()
			}

			// Create new subscription
			b.pubsub = b.client.Subscribe(b.ctx, b.channel)

			// Test subscription
			if err := b.pubsub.Ping(b.ctx); err != nil {
				b.logger.Error().Err(err).Msg("Redis Pub/Sub reconnection failed")
				b.healthy.Store(false)

				// Exponential backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			b.logger.Info().Msg("Successfully reconnected to Redis Pub/Sub")
			b.healthy.Store(true)
			return
		}
	}
}
```

**Key Features**:
1. **Automatic Reconnection**: If Redis connection drops, exponentially back off and retry
2. **Non-Blocking Fan-Out**: If shard buffer is full, drop message (real-time data, no blocking)
3. **Health Tracking**: Sets `healthy=false` if reconnection fails

---

#### Day 4: Implement Shutdown and Health Check

**Task**: Graceful shutdown and health monitoring.

```go
// Shutdown gracefully stops the BroadcastBus
func (b *BroadcastBus) Shutdown() {
	b.logger.Info().Msg("Shutting down BroadcastBus...")

	// Stop receiving new messages
	b.cancel()

	// Wait for goroutines to finish (with timeout)
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		b.logger.Info().Msg("All BroadcastBus goroutines stopped")
	case <-time.After(5 * time.Second):
		b.logger.Warn().Msg("BroadcastBus shutdown timeout (5s)")
	}

	// Close Redis connections
	if b.pubsub != nil {
		if err := b.pubsub.Close(); err != nil {
			b.logger.Error().Err(err).Msg("Failed to close Redis Pub/Sub")
		}
	}

	if err := b.client.Close(); err != nil {
		b.logger.Error().Err(err).Msg("Failed to close Redis client")
	}

	// Close all subscriber channels
	b.subMu.Lock()
	for _, sub := range b.subscribers {
		close(sub)
	}
	b.subMu.Unlock()

	b.logger.Info().Msg("BroadcastBus shutdown complete")
}

// IsHealthy returns true if Redis connection is healthy
func (b *BroadcastBus) IsHealthy() bool {
	// Check atomic health flag
	if !b.healthy.Load() {
		return false
	}

	// Check if last publish was recent (within 30 seconds)
	lastPub := b.lastPublish.Load()
	if time.Since(time.Unix(lastPub, 0)) > 30*time.Second {
		b.logger.Warn().Msg("No successful Redis publish in 30 seconds")
		return false
	}

	return true
}

// healthCheckLoop periodically pings Redis to verify connectivity
func (b *BroadcastBus) healthCheckLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
			err := b.client.Ping(ctx).Err()
			cancel()

			if err != nil {
				b.logger.Error().Err(err).Msg("Redis health check failed")
				b.healthy.Store(false)
			} else {
				b.healthy.Store(true)
			}
		}
	}
}

// GetMetrics returns current bus metrics (for monitoring)
func (b *BroadcastBus) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"type":             "redis",
		"healthy":          b.IsHealthy(),
		"channel":          b.channel,
		"subscribers":      len(b.subscribers),
		"publish_errors":   b.publishErrors.Load(),
		"last_publish_ago": time.Since(time.Unix(b.lastPublish.Load(), 0)).Seconds(),
	}
}
```

---

#### Day 5: Update Configuration and Main

**Task**: Integrate RedisBroadcastBus into main application.

**File**: `ws/internal/shared/platform/config.go`

```go
type Config struct {
	// ... existing fields ...

	// Redis Sentinel Configuration
	RedisSentinelAddrs []string `env:"REDIS_SENTINEL_ADDRS" envSeparator:"," required:"true"`
	RedisMasterName    string   `env:"REDIS_MASTER_NAME" envDefault:"mymaster"`
	RedisPassword      string   `env:"REDIS_PASSWORD"`
	RedisDB            int      `env:"REDIS_DB" envDefault:"0"`
	RedisChannel       string   `env:"REDIS_CHANNEL" envDefault:"ws.broadcast"`
}
```

**File**: `ws/cmd/multi/main.go`

```go
func main() {
	// ... existing setup ...

	// Load configuration
	cfg := platform.LoadConfig()

	// Create logger
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Create BroadcastBus (Redis)
	busConfig := multi.BroadcastBusConfig{
		SentinelAddrs: cfg.RedisSentinelAddrs,
		MasterName:    cfg.RedisMasterName,
		Password:      cfg.RedisPassword,
		DB:            cfg.RedisDB,
		Channel:       cfg.RedisChannel,
		BufferSize:    1024,
		Logger:        logger.With().Str("component", "broadcast-bus").Logger(),
	}

	broadcastBus, err := multi.NewBroadcastBus(busConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create BroadcastBus")
	}

	logger.Info().
		Str("type", "redis").
		Strs("sentinel_addrs", cfg.RedisSentinelAddrs).
		Msg("BroadcastBus initialized")

	// ... create shards (they call Subscribe()) ...

	// Start BroadcastBus (after all Subscribe() calls)
	broadcastBus.Run()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info().Msg("Shutdown signal received")

	// Shutdown broadcast bus first (stops consuming messages)
	broadcastBus.Shutdown()

	// ... shutdown other components ...

	logger.Info().Msg("Server shutdown complete")
}
```

**Environment Variables** (example):

```bash
# Self-hosted Redis Sentinel
REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=your-secure-password
REDIS_DB=0
REDIS_CHANNEL=ws.broadcast
```

---

### Phase 2: Deploy Self-Hosted Redis Sentinel (Week 2 - 5 days)

**Goal**: Deploy Redis Sentinel cluster on GCP for HA.

#### Day 1-2: Provision GCP Infrastructure

**Option A: Manual Setup** (if no Terraform)

```bash
# Create 3 GCP instances for Redis nodes
for i in {1..3}; do
  gcloud compute instances create redis-node-$i \
    --machine-type=e2-standard-2 \
    --zone=us-central1-a \
    --image-family=ubuntu-2204-lts \
    --image-project=ubuntu-os-cloud \
    --tags=redis-server \
    --boot-disk-size=20GB
done

# SSH into each instance and install Redis
for i in {1..3}; do
  gcloud compute ssh redis-node-$i --zone=us-central1-a -- \
    'sudo apt update && sudo apt install -y redis-server redis-sentinel'
done
```

**Option B: Terraform** (recommended)

Create `terraform/redis-sentinel/main.tf`:

```hcl
resource "google_compute_instance" "redis_nodes" {
  count        = 3
  name         = "redis-node-${count.index + 1}"
  machine_type = "e2-standard-2"
  zone         = "us-central1-a"

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 20
    }
  }

  network_interface {
    network = "default"
    access_config {} # Ephemeral IP
  }

  tags = ["redis-server"]

  metadata_startup_script = <<-EOF
    #!/bin/bash
    apt update
    apt install -y redis-server redis-sentinel
    systemctl enable redis-server redis-sentinel
  EOF
}

output "redis_node_ips" {
  value = google_compute_instance.redis_nodes[*].network_interface[0].network_ip
}
```

Deploy:
```bash
cd terraform/redis-sentinel
terraform init
terraform apply

# Get internal IPs
terraform output redis_node_ips
# Expected: ["10.128.0.10", "10.128.0.11", "10.128.0.12"]
```

---

#### Day 3: Configure Redis Master and Replicas

**Node 1** (Master): `/etc/redis/redis.conf`
```conf
bind 0.0.0.0
protected-mode no
port 6379
requirepass your-secure-password
masterauth your-secure-password

# Persistence (optional, adds latency)
save ""
appendonly no

# Performance
maxmemory 1gb
maxmemory-policy allkeys-lru
```

**Node 2 & 3** (Replicas): `/etc/redis/redis.conf`
```conf
bind 0.0.0.0
protected-mode no
port 6379
requirepass your-secure-password
masterauth your-secure-password

# Replication
replicaof 10.128.0.10 6379

# Performance
maxmemory 1gb
maxmemory-policy allkeys-lru
```

Restart Redis on all nodes:
```bash
sudo systemctl restart redis-server
```

Verify replication:
```bash
# On master (Node 1)
redis-cli -a your-secure-password INFO replication
# Expected: role:master, connected_slaves:2
```

---

#### Day 4: Configure Redis Sentinel

**All 3 Nodes**: `/etc/redis/sentinel.conf`

```conf
bind 0.0.0.0
protected-mode no
port 26379

# Monitor master
sentinel monitor mymaster 10.128.0.10 6379 2
sentinel auth-pass mymaster your-secure-password

# Failover configuration
sentinel down-after-milliseconds mymaster 5000
sentinel parallel-syncs mymaster 1
sentinel failover-timeout mymaster 10000

# Logging
logfile /var/log/redis/sentinel.log
```

**Configuration Explained**:
- `monitor mymaster 10.128.0.10 6379 2`: Monitor master, quorum = 2 (2/3 Sentinels must agree for failover)
- `down-after-milliseconds 5000`: Consider master down after 5s of no response
- `parallel-syncs 1`: Only 1 replica syncs at a time (prevents thundering herd)
- `failover-timeout 10000`: Failover must complete within 10s

Start Sentinel on all nodes:
```bash
sudo systemctl restart redis-sentinel
```

Verify Sentinel cluster:
```bash
redis-cli -p 26379 SENTINEL masters
# Expected: Shows master info

redis-cli -p 26379 SENTINEL sentinels mymaster
# Expected: Shows 2 other Sentinels
```

---

#### Day 5: Test Failover

**Manual Failover Test**:

```bash
# 1. Check current master
redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
# Expected: ["10.128.0.10", "6379"]

# 2. Kill master
gcloud compute instances stop redis-node-1 --zone=us-central1-a

# 3. Wait 10 seconds, check new master
redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
# Expected: ["10.128.0.11", "6379"] or ["10.128.0.12", "6379"]

# 4. Verify ws-server reconnected automatically
curl http://ws-instance-1:3005/health | jq '.broadcast_bus'
# Expected: {"type":"redis","healthy":true}

# 5. Restart old master (becomes replica)
gcloud compute instances start redis-node-1 --zone=us-central1-a
```

**Expected Timeline**:
- T+0s: Master crashes
- T+5s: Sentinels detect master down
- T+7s: Quorum reached, failover initiated
- T+10s: New master promoted, ws-server reconnects
- **Total: 10 seconds downtime** ✅

---

### Phase 3: Testing & Validation (Week 3)

#### Shadow Mode Testing (Optional)

If you want to validate Redis without impacting production, implement dual-bus mode:

```go
// Temporary: Publish to both in-memory AND Redis
func (kp *KafkaPool) processMessage(msg *kgo.Record) {
	// ... existing message processing ...

	broadcastMsg := &BroadcastMessage{
		Subject: subject,
		Message: payload,
	}

	// Publish to in-memory (production)
	inMemoryBus.Publish(broadcastMsg)

	// Shadow: Publish to Redis (validation only)
	redisBus.Publish(broadcastMsg)

	// Compare: Verify 100% consistency
	// (metrics show both buses receive same messages)
}
```

**Remove shadow mode after 1 week of validation.**

---

#### Multi-Instance Testing

**Local Testing with Docker Compose**:

Create `docker-compose.redis-test.yml`:

```yaml
version: '3.8'

services:
  redis-master:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    command: redis-server --requirepass testpass

  redis-sentinel:
    image: redis:7-alpine
    ports:
      - "26379:26379"
    command: redis-sentinel /etc/redis/sentinel.conf
    volumes:
      - ./sentinel.conf:/etc/redis/sentinel.conf

  ws-instance-1:
    build: .
    environment:
      REDIS_SENTINEL_ADDRS: redis-sentinel:26379
      REDIS_MASTER_NAME: mymaster
      REDIS_PASSWORD: testpass
    ports:
      - "3005:3005"

  ws-instance-2:
    build: .
    environment:
      REDIS_SENTINEL_ADDRS: redis-sentinel:26379
      REDIS_MASTER_NAME: mymaster
      REDIS_PASSWORD: testpass
    ports:
      - "3006:3005"
```

**Test**:
```bash
# Start services
docker-compose -f docker-compose.redis-test.yml up

# Connect client to instance 1
wscat -c ws://localhost:3005

# Connect client to instance 2
wscat -c ws://localhost:3006

# Publish message (simulated Kafka event)
# Both clients should receive the same message ✅
```

---

### Phase 4: Production Deployment (Week 4)

#### Day 1: Update Staging Environment

```bash
# Update env vars for staging ws-servers
gcloud compute instances update ws-staging-1 \
  --metadata=REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379

gcloud compute instances update ws-staging-2 \
  --metadata=REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379

# Rolling restart
gcloud compute instances stop ws-staging-1 --zone=us-central1-a
gcloud compute instances start ws-staging-1 --zone=us-central1-a

# Wait 5 minutes, monitor logs, then restart instance 2
```

#### Day 2-3: Monitor Staging

**Metrics to Watch**:
- Latency: Should be <15ms p99 (in-memory was <10ms, +2ms Redis overhead is acceptable)
- Error rate: Should be 0%
- Redis CPU: Should be <20%
- Failover test: Kill Redis master, verify <10s recovery

#### Day 4: Production Rollout

```bash
# Same process as staging, but with production instances
# Rolling restart to avoid downtime
```

---

## Future Migration: Self-Hosted → GCP Memorystore

**When to Migrate**:
- After 3-6 months of stable self-hosted Redis
- When operational burden becomes too high
- When team wants fully managed solution

**Migration Steps**:

### Step 1: Provision GCP Memorystore (15 minutes)

**Terraform**:
```hcl
resource "google_redis_instance" "broadcast_bus" {
  name           = "ws-broadcast-bus"
  tier           = "STANDARD_HA"  # High availability
  memory_size_gb = 1
  region         = "us-central1"

  redis_version = "REDIS_7_0"

  auth_enabled = true
  transit_encryption_mode = "DISABLED" # VPC-only, TLS optional

  redis_configs = {
    maxmemory-policy = "allkeys-lru"
  }
}

output "redis_host" {
  value = google_redis_instance.broadcast_bus.host
}

output "redis_port" {
  value = google_redis_instance.broadcast_bus.port
}
```

Deploy:
```bash
terraform apply
# Expected: redis_host = "10.0.0.3", redis_port = 6379
```

### Step 2: Update Environment Variables (5 minutes)

**BEFORE** (self-hosted):
```bash
REDIS_SENTINEL_ADDRS=10.128.0.10:26379,10.128.0.11:26379,10.128.0.12:26379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=your-secure-password
```

**AFTER** (GCP Memorystore):
```bash
REDIS_SENTINEL_ADDRS=10.0.0.3:6379
REDIS_MASTER_NAME=mymaster
REDIS_PASSWORD=<gcp-generated-password>
```

**Note**: GCP Memorystore uses standard Redis protocol, NOT Sentinel protocol for Standard HA tier. Update code to use `redis.NewClient()` instead of `redis.NewFailoverClient()` if needed, OR keep using Failover client with single address (it will work).

**Actually, simpler approach**: Update code to support BOTH patterns:

```go
func NewBroadcastBus(cfg BroadcastBusConfig) (*BroadcastBus, error) {
	var client *redis.Client

	// Detect mode based on number of Sentinel addresses
	if len(cfg.SentinelAddrs) > 1 {
		// Self-hosted Sentinel mode
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.MasterName,
			SentinelAddrs: cfg.SentinelAddrs,
			Password:      cfg.Password,
			DB:            cfg.DB,
			PoolSize:      50,
			MinIdleConns:  10,
		})
	} else {
		// Direct connection mode (GCP Memorystore or single Redis)
		client = redis.NewClient(&redis.Options{
			Addr:         cfg.SentinelAddrs[0], // e.g., "10.0.0.3:6379"
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     50,
			MinIdleConns: 10,
		})
	}

	// ... rest of initialization ...
}
```

**This way, migration is TRULY zero code changes** - just update env var.

### Step 3: Rolling Restart (10 minutes)

```bash
# Restart instances one at a time
gcloud compute instances restart ws-prod-1 --zone=us-central1-a
# Wait 2 minutes, verify health
curl http://ws-prod-1:3005/health | jq '.broadcast_bus'

gcloud compute instances restart ws-prod-2 --zone=us-central1-a
# Verify health
```

### Step 4: Decommission Self-Hosted Redis (After 1 Week)

```bash
# After 1 week of stable GCP Memorystore
terraform destroy -target=google_compute_instance.redis_nodes
```

**Total Migration Time**: 30 minutes
**Code Changes**: ZERO (if using detection logic above)
**Risk**: Very Low (GCP Memorystore has 99.9% SLA)

---

## Cost Comparison

### Self-Hosted Redis Sentinel

**Infrastructure**:
- 3× e2-standard-2 (2 vCPU, 8GB): $49.35/month each
- **Total**: $148/month

**Labor** (hidden cost):
- Setup: 2-3 days @ $800/day = $1,600-2,400 one-time
- Maintenance: 4 hours/month @ $100/hr = $400/month
- Incidents: 2 hours/year @ $100/hr = $200/year

**Annual Cost**: $148×12 + $1,600 + $400×12 = $8,176 - $8,976

---

### GCP Memorystore (Managed)

**Infrastructure**:
- Standard HA (1GB): $41.21/month
- **Total**: $41/month

**Labor**:
- Setup: 30 minutes @ $800/day = $50 one-time
- Maintenance: 30 minutes/month @ $100/hr = $50/month
- Incidents: 0 (GCP handles)

**Annual Cost**: $41×12 + $50 + $50×12 = $1,142

**Savings**: $7,034 - $7,834/year (87% cheaper with managed) 💰

---

## Testing Checklist

Before going to production:

### Unit Tests
- ✅ Publish 1000 messages, verify all received
- ✅ Simulate Redis disconnect, verify reconnection
- ✅ Test health check reports unhealthy during outage
- ✅ Test graceful shutdown drains messages

### Integration Tests
- ✅ 2 instances, publish to Instance 1, receive on Instance 2
- ✅ Kill Redis master, verify <10s failover
- ✅ Load test: 1K connections, 10K msg/sec for 10 minutes

### Production Readiness
- ✅ Monitoring dashboards (Redis CPU, latency, error rate)
- ✅ Alerting rules (Redis down, high latency >15ms, error rate >1%)
- ✅ Runbook for Redis failures
- ✅ Backup/restore procedure documented

---

## Common Pitfalls

### Pitfall 1: Not Handling Sentinel Failover Correctly

**Problem**: Client doesn't reconnect after failover.

**Solution**: Use `redis.NewFailoverClient()` (it auto-discovers new master).

---

### Pitfall 2: Blocking Publish Calls

**Problem**: Redis is slow, blocks Kafka consumer.

**Solution**: Use 100ms timeout on Publish, drop message if timeout.

---

### Pitfall 3: Not Monitoring Redis Health

**Problem**: Redis is down for 10 minutes before anyone notices.

**Solution**: Health check endpoint exposes `broadcast_bus.healthy`, alert if false.

---

### Pitfall 4: Forgetting Password in Sentinel Config

**Problem**: Sentinel can monitor master, but can't perform failover (auth failure).

**Solution**: Always set `sentinel auth-pass mymaster <password>`.

---

## Summary

**Week 1**: Implement RedisBroadcastBus (5 days)
**Week 2**: Deploy self-hosted Redis Sentinel (5 days)
**Week 3**: Testing and validation (5 days)
**Week 4**: Production rollout (4 days)

**Total**: 4 weeks from start to production
**Code Changes for Managed Migration**: ZERO (env var only)
**Cost**: $148/month (self-hosted) → $41/month (managed)

**Key Guarantees**:
1. ✅ High availability (Redis Sentinel auto-failover <10s)
2. ✅ Zero-code-change migration to GCP Memorystore
3. ✅ Simple implementation (no interface abstraction)
4. ✅ Production-ready (health checks, reconnection, graceful shutdown)
