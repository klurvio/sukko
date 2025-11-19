# SPIKE: External Broadcast Bus for Multi-Instance Deployment

**Status**: Proposed
**Date**: 2025-01-19
**Author**: Architecture Review
**Goal**: Enable multi-instance WebSocket server deployment with Redis or NATS as external broadcast bus

---

## Table of Contents

- [Problem Statement](#problem-statement)
- [Current Architecture](#current-architecture)
- [Proposed Solutions](#proposed-solutions)
- [NATS vs Redis Comparison](#nats-vs-redis-comparison)
- [High Availability Strategy](#high-availability-strategy)
- [Implementation Plan](#implementation-plan)
- [Code Examples](#code-examples)
- [Monitoring & Observability](#monitoring--observability)
- [Recommendation](#recommendation)

---

## Problem Statement

### Current Limitation

The current `BroadcastBus` implementation uses **in-memory Go channels**, which only work within a single process:

```
Current (Single Instance):
  Kafka → Consumer → In-Memory BroadcastBus → All Shards ✓

Broken (Multiple Instances):
  Instance 1: Kafka partitions 0-47 → BroadcastBus #1 → Shards 0-2
  Instance 2: Kafka partitions 48-95 → BroadcastBus #2 → Shards 3-5

  Problem: BroadcastBus #1 cannot reach Instance 2's shards
  Result: 50% of clients miss 50% of messages
```

### Requirements

To support multi-instance deployment (2-4+ instances behind GCP Load Balancer):

```
✓ All instances must receive all messages (broadcast pattern)
✓ Low latency (<10ms end-to-end including bus)
✓ High throughput (1K-10K msg/sec, room for 10-100× growth)
✓ High availability (survive single node failure)
✓ Simple to operate (minimize ops burden)
✓ Cost-effective (run on existing infrastructure)
```

---

## Current Architecture

### In-Memory BroadcastBus

**File**: `ws/internal/multi/broadcast.go`

```
┌────────────────────────────────────┐
│ Instance 1 (Single Process)        │
│                                    │
│  KafkaConsumerPool                 │
│         ↓                          │
│  BroadcastBus (in-memory)          │
│         ↓                          │
│  ┌──────┼──────┐                  │
│  ↓      ↓      ↓                  │
│ Shard0 Shard1 Shard2               │
│                                    │
└────────────────────────────────────┘

Works: Single instance ✓
Breaks: Multiple instances ✗
```

### With Multiple Instances (Current Problem)

```
GCP Load Balancer
        ↓
   ┌────┴────┐
   ↓         ↓
Instance 1  Instance 2
Kafka:      Kafka:
 48 parts    48 parts
   ↓         ↓
BroadcastBus BroadcastBus
(isolated)  (isolated)  ← Cannot communicate!
   ↓         ↓
Shards 0-2  Shards 3-5

50% of clients miss 50% of messages
```

---

## Proposed Solutions

### Option A: NATS as External Broadcast Bus

```
GCP Load Balancer
        ↓
   ┌────┴────┐
   ↓         ↓
Instance 1  Instance 2
Kafka:      Kafka:
 48 parts    48 parts
   ↓         ↓
   └────┬────┘
        ↓
   NATS Cluster (3 nodes)
        ↓
   Broadcasts to ALL instances
        ↓
   ┌────┴────┐
   ↓         ↓
Shards 0-2  Shards 3-5
ALL get     ALL get
ALL msgs ✓  ALL msgs ✓
```

### Option B: Redis Pub/Sub as External Broadcast Bus

```
GCP Load Balancer
        ↓
   ┌────┴────┐
   ↓         ↓
Instance 1  Instance 2
Kafka:      Kafka:
 48 parts    48 parts
   ↓         ↓
   └────┬────┘
        ↓
Redis Sentinel (1M + 2R + 3S)
        ↓
   Broadcasts to ALL instances
        ↓
   ┌────┴────┐
   ↓         ↓
Shards 0-2  Shards 3-5
ALL get     ALL get
ALL msgs ✓  ALL msgs ✓
```

---

## NATS vs Redis Comparison

### Quick Summary

| Aspect | NATS | Redis Pub/Sub | Winner |
|--------|------|---------------|--------|
| **Latency (p99)** | <0.5ms | 1-2ms | NATS ⭐ |
| **Throughput** | 10M+ msg/sec | 1M msg/sec | NATS ⭐ |
| **Purpose** | Messaging only | General-purpose | NATS ⭐ |
| **Memory Usage** | Very low (streaming) | Higher (buffers in RAM) | NATS ⭐ |
| **Clustering** | Built-in (simple) | Sentinel/Cluster (complex) | NATS ⭐ |
| **Persistence** | JetStream (optional) | AOF/RDB (always on) | NATS ⭐ |
| **Familiarity** | Less common | Very common | Redis |
| **Monitoring** | Prometheus built-in | RedisInsight, many tools | Redis |
| **Multi-use** | Messaging only | Cache, queue, pub/sub, etc. | Redis |
| **Client Libraries** | Good (Go native) | Excellent (everywhere) | Tie |
| **Ops Complexity** | Low (single binary) | Medium (config tuning) | NATS ⭐ |

### Latency Comparison (Critical for WebSockets)

```
Message Flow: Kafka → Instance 1 → Message Bus → Instance 2 → Client

With NATS:
  Kafka fetch:         1-2ms
  Consumer processing: <1ms
  NATS pub:            0.1ms
  NATS propagation:    0.2ms   ← Very fast!
  NATS sub:            0.1ms
  Shard broadcast:     1-2ms
  WebSocket write:     1-2ms
  ──────────────────────────
  Total:               ~6-8ms ✓

With Redis Pub/Sub:
  Kafka fetch:         1-2ms
  Consumer processing: <1ms
  Redis PUBLISH:       0.5ms
  Redis propagation:   1ms     ← Slower
  Redis SUBSCRIBE:     0.5ms
  Shard broadcast:     1-2ms
  WebSocket write:     1-2ms
  ──────────────────────────
  Total:               ~8-10ms (still acceptable)

Your SLA: <10ms end-to-end
Both work, but NATS has more headroom.
```

### Throughput Comparison

```
Your Current Load:
  - 1,000 messages/sec
  - 8 topics
  - 2 instances
  - ~500 msg/sec per instance published to bus

NATS Capacity:
  Single NATS server: 10M+ msg/sec
  Your usage: 1K msg/sec
  Headroom: 10,000× (massive buffer)

Redis Capacity:
  Single Redis server: 1M msg/sec (pub/sub)
  Your usage: 1K msg/sec
  Headroom: 1,000× (still plenty)

Both have plenty of headroom, but NATS has 10× more.
```

### Performance Characteristics

```
NATS (Core NATS):
  Latency:     <0.5ms p99
  Throughput:  10M+ msg/sec (single server)
  Memory:      ~10MB base + minimal per message
  CPU:         Very efficient (written in Go)
  Ordering:    Per-subject FIFO
  Delivery:    At-most-once (fire-and-forget)

NATS (JetStream - if you need durability):
  Latency:     1-2ms p99 (persistence overhead)
  Throughput:  1M+ msg/sec
  Memory:      Configurable (stream retention)
  Ordering:    Strict per-stream
  Delivery:    At-least-once, exactly-once available

Redis Pub/Sub:
  Latency:     1-2ms p99
  Throughput:  1M msg/sec
  Memory:      Higher (buffers in memory)
  CPU:         Good (written in C)
  Ordering:    Per-channel FIFO
  Delivery:    At-most-once (ephemeral)
```

### Resource Requirements

```
NATS (3-node cluster):
  Per Node:
    CPU:    0.1-0.5 cores
    Memory: 50-100MB
    Disk:   None (unless using JetStream)

  Total:
    CPU:    0.3-1.5 cores
    Memory: 150-300MB
    Cost:   Minimal (can run on existing instances)

Redis Sentinel (1M + 2R + 3S):
  Master:
    CPU:    0.2-0.5 cores
    Memory: 512MB
    Disk:   AOF (~100MB)

  Replica (×2):
    CPU:    0.2-0.5 cores each
    Memory: 512MB each
    Disk:   AOF (~100MB each)

  Sentinel (×3):
    CPU:    0.1 cores each
    Memory: 50MB each

  Total:
    CPU:    0.9-1.8 cores
    Memory: 1.6GB
    Cost:   Higher (may need separate instance)
```

---

## High Availability Strategy

### Option 1: NATS Cluster (Recommended)

#### Architecture

```
3-Node NATS Cluster (Mesh Topology)

┌─────────┐     ┌─────────┐     ┌─────────┐
│ NATS-1  │────▶│ NATS-2  │────▶│ NATS-3  │
│ (4222)  │◀────│ (4222)  │◀────│ (4222)  │
└────┬────┘     └────┬────┘     └────┬────┘
     │               │               │
     └───────────────┼───────────────┘
                     │
              ┌──────┴──────┐
              ↓             ↓
         Instance 1    Instance 2
         (connects   (connects
          to any)     to any)
```

#### Docker Compose Configuration

```yaml
version: '3.8'

services:
  nats-1:
    image: nats:2.10-alpine
    ports:
      - "4222:4222"  # Client connections
      - "8222:8222"  # HTTP monitoring
    command: >
      --cluster_name nats-cluster
      --cluster nats://0.0.0.0:6222
      --routes nats://nats-2:6222,nats://nats-3:6222
      --http_port 8222
      --max_payload 1MB
      --max_pending 100MB
    networks:
      - ws-network
    restart: unless-stopped

  nats-2:
    image: nats:2.10-alpine
    ports:
      - "4223:4222"
      - "8223:8222"
    command: >
      --cluster_name nats-cluster
      --cluster nats://0.0.0.0:6222
      --routes nats://nats-1:6222,nats://nats-3:6222
      --http_port 8222
      --max_payload 1MB
      --max_pending 100MB
    networks:
      - ws-network
    restart: unless-stopped

  nats-3:
    image: nats:2.10-alpine
    ports:
      - "4224:4222"
      - "8224:8222"
    command: >
      --cluster_name nats-cluster
      --cluster nats://0.0.0.0:6222
      --routes nats://nats-1:6222,nats://nats-2:6222
      --http_port 8222
      --max_payload 1MB
      --max_pending 100MB
    networks:
      - ws-network
    restart: unless-stopped

networks:
  ws-network:
    driver: bridge
```

#### Client Connection (Automatic Failover)

```go
// Connect to NATS cluster with auto-failover
natsURL := "nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222"

conn, err := nats.Connect(natsURL,
    nats.MaxReconnects(-1),              // Infinite reconnects
    nats.ReconnectWait(1*time.Second),   // Wait 1s between attempts
    nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
    nats.PingInterval(20*time.Second),
    nats.MaxPingsOutstanding(2),

    // Handlers
    nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
        logger.Warn().Err(err).Msg("NATS disconnected, reconnecting...")
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
```

#### Failover Behavior

```
Normal Operation:
  Instance 1 → NATS-1 (primary)
  Instance 2 → NATS-2
  NATS-1, NATS-2, NATS-3 sync messages via cluster protocol

NATS-1 Fails:
  T+0s:  NATS-1 crashes
  T+1s:  Instance 1 detects disconnect (ping timeout)
  T+2s:  Instance 1 auto-reconnects to NATS-2
  T+3s:  Normal operation resumed

  Downtime: ~1-3 seconds
  Message Loss: Zero (NATS cluster has all messages)

NATS-2 Fails:
  Instance 2 auto-reconnects to NATS-1 or NATS-3
  Same recovery time

2 of 3 NATS Nodes Fail:
  All instances reconnect to remaining healthy node
  System continues operating (degraded, but functional)
  Recovery: Deploy failed nodes, auto-rejoin cluster
```

### Option 2: Redis Sentinel (More Complex)

#### Architecture

```
Redis Sentinel Setup

┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ Sentinel-1   │────▶│ Sentinel-2   │────▶│ Sentinel-3   │
│ (26379)      │◀────│ (26379)      │◀────│ (26379)      │
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │                    │                    │
       └────────────────────┼────────────────────┘
                            │ (Monitor)
                            ↓
       ┌──────────────────────────────────────┐
       │         Redis Master (6379)          │
       └─────────┬────────────────────────────┘
                 │ (Replicates to)
       ┌─────────┴─────────┐
       ↓                   ↓
┌─────────────┐     ┌─────────────┐
│ Replica-1   │     │ Replica-2   │
│ (6380)      │     │ (6381)      │
└─────────────┘     └─────────────┘
```

#### Docker Compose Configuration

```yaml
version: '3.8'

services:
  redis-master:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    command: >
      redis-server
      --appendonly yes
      --maxmemory 512mb
      --maxmemory-policy volatile-lru
    volumes:
      - redis-master-data:/data
    networks:
      - ws-network
    restart: unless-stopped

  redis-replica-1:
    image: redis:7-alpine
    ports:
      - "6380:6379"
    command: >
      redis-server
      --replicaof redis-master 6379
      --appendonly yes
      --maxmemory 512mb
    volumes:
      - redis-replica1-data:/data
    networks:
      - ws-network
    restart: unless-stopped
    depends_on:
      - redis-master

  redis-replica-2:
    image: redis:7-alpine
    ports:
      - "6381:6379"
    command: >
      redis-server
      --replicaof redis-master 6379
      --appendonly yes
      --maxmemory 512mb
    volumes:
      - redis-replica2-data:/data
    networks:
      - ws-network
    restart: unless-stopped
    depends_on:
      - redis-master

  sentinel-1:
    image: redis:7-alpine
    ports:
      - "26379:26379"
    command: >
      redis-sentinel /etc/redis/sentinel.conf
    volumes:
      - ./sentinel.conf:/etc/redis/sentinel.conf
    networks:
      - ws-network
    restart: unless-stopped
    depends_on:
      - redis-master

  sentinel-2:
    image: redis:7-alpine
    ports:
      - "26380:26379"
    command: >
      redis-sentinel /etc/redis/sentinel.conf
    volumes:
      - ./sentinel.conf:/etc/redis/sentinel.conf
    networks:
      - ws-network
    restart: unless-stopped
    depends_on:
      - redis-master

  sentinel-3:
    image: redis:7-alpine
    ports:
      - "26381:26379"
    command: >
      redis-sentinel /etc/redis/sentinel.conf
    volumes:
      - ./sentinel.conf:/etc/redis/sentinel.conf
    networks:
      - ws-network
    restart: unless-stopped
    depends_on:
      - redis-master

volumes:
  redis-master-data:
  redis-replica1-data:
  redis-replica2-data:

networks:
  ws-network:
    driver: bridge
```

#### Sentinel Configuration

**File**: `sentinel.conf`

```conf
port 26379

# Monitor master
sentinel monitor mymaster redis-master 6379 2

# Timeouts
sentinel down-after-milliseconds mymaster 5000
sentinel failover-timeout mymaster 10000
sentinel parallel-syncs mymaster 1

# Authentication (if needed)
# sentinel auth-pass mymaster your-password
```

#### Client Connection

```go
// Connect to Redis Sentinel
client := redis.NewFailoverClient(&redis.FailoverOptions{
    MasterName:    "mymaster",
    SentinelAddrs: []string{
        "sentinel-1:26379",
        "sentinel-2:26379",
        "sentinel-3:26379",
    },

    // Connection settings
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,

    // Pool settings
    PoolSize:     10,
    MinIdleConns: 5,
    MaxRetries:   3,

    // Reconnect
    MaxRetryBackoff: 2 * time.Second,
})
```

#### Failover Behavior

```
Normal Operation:
  Master: Receives PUBLISH commands
  Replicas: Receive replicated data (async)
  Sentinels: Monitor master health (ping every 1s)

Master Fails:
  T+0s:  Master crashes
  T+5s:  Sentinels detect failure (down-after-milliseconds)
  T+6s:  Sentinels vote (need 2 of 3 quorum)
  T+7s:  Sentinel promotes replica-1 to master
  T+10s: Clients reconnect to new master
  T+15s: Normal operation resumed

  Downtime: 5-15 seconds
  Message Loss: Possible (async replication gap)

Replica Fails:
  No impact on serving (master still operational)
  Sentinel notices but no action needed

Split Brain Risk:
  If network partition between sentinels
  Need 2 of 3 quorum to prevent multiple masters
```

### Comparison: NATS Cluster vs Redis Sentinel

| Aspect | NATS Cluster | Redis Sentinel | Winner |
|--------|--------------|----------------|--------|
| **Setup Complexity** | Simple (3 identical nodes) | Complex (master/replica/sentinel) | NATS ⭐ |
| **Failover Time** | <1 second | 5-15 seconds | NATS ⭐ |
| **Message Loss Risk** | Zero (cluster sync) | Possible (async replication) | NATS ⭐ |
| **Resource Usage** | 150-300MB total | 1.6GB total | NATS ⭐ |
| **Ops Complexity** | Low (identical nodes) | Medium-High (roles, config) | NATS ⭐ |
| **Monitoring** | Built-in `/varz` endpoint | RedisInsight, many tools | Redis |
| **Maturity** | Very mature (10+ years) | Very mature (15+ years) | Tie |
| **Community** | Growing | Very large | Redis |

---

## Code Examples

### BroadcastBus Interface

```go
// ws/internal/multi/broadcast_bus.go
package multi

import "context"

// BroadcastBus defines the interface for message broadcasting
// Can be implemented as in-memory (current), NATS, Redis, etc.
type BroadcastBus interface {
    // Publish sends a message to all subscribers
    Publish(msg *BroadcastMessage) error

    // Subscribe returns a channel for receiving messages
    Subscribe() chan *BroadcastMessage

    // Run starts the broadcast bus (if needed)
    Run()

    // Shutdown gracefully closes the broadcast bus
    Shutdown()
}

// BroadcastMessage represents a message to broadcast
type BroadcastMessage struct {
    Subject string `json:"subject"` // e.g., "odin.token.BTC.trade"
    Message []byte `json:"message"` // Raw message data
}
```

### NATS Implementation

```go
// ws/internal/multi/nats_broadcast_bus.go
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
    // Connect to NATS cluster
    conn, err := nats.Connect(natsURL,
        nats.MaxReconnects(-1),              // Infinite reconnects
        nats.ReconnectWait(1*time.Second),
        nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
        nats.PingInterval(20*time.Second),
        nats.MaxPingsOutstanding(2),

        // Handlers
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

func (n *NatsBroadcastBus) Publish(msg *BroadcastMessage) error {
    // Serialize message
    data, err := json.Marshal(msg)
    if err != nil {
        return err
    }

    // Publish to NATS (fire-and-forget, very fast)
    return n.conn.Publish(n.subject, data)
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
```

### Redis Implementation

```go
// ws/internal/multi/redis_broadcast_bus.go
package multi

import (
    "context"
    "encoding/json"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/rs/zerolog"
)

type RedisBroadcastBus struct {
    client  *redis.Client
    channel string
    logger  zerolog.Logger
    ctx     context.Context
    cancel  context.CancelFunc
}

func NewRedisBroadcastBus(redisURL string, logger zerolog.Logger) (*RedisBroadcastBus, error) {
    // Parse Redis URL
    opt, err := redis.ParseURL(redisURL)
    if err != nil {
        return nil, err
    }

    client := redis.NewClient(opt)

    // Test connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        return nil, err
    }

    ctx, cancel = context.WithCancel(context.Background())

    return &RedisBroadcastBus{
        client:  client,
        channel: "ws:broadcast",
        logger:  logger,
        ctx:     ctx,
        cancel:  cancel,
    }, nil
}

func (r *RedisBroadcastBus) Publish(msg *BroadcastMessage) error {
    // Serialize message
    data, err := json.Marshal(msg)
    if err != nil {
        return err
    }

    // Publish to Redis channel
    return r.client.Publish(r.ctx, r.channel, data).Err()
}

func (r *RedisBroadcastBus) Subscribe() chan *BroadcastMessage {
    msgCh := make(chan *BroadcastMessage, 1024)

    // Subscribe to Redis channel
    pubsub := r.client.Subscribe(r.ctx, r.channel)

    go func() {
        defer close(msgCh)
        defer pubsub.Close()

        ch := pubsub.Channel()

        for {
            select {
            case redisMsg := <-ch:
                if redisMsg == nil {
                    r.logger.Warn().Msg("Redis pubsub channel closed")
                    return
                }

                var msg BroadcastMessage
                if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
                    r.logger.Warn().Err(err).Msg("Failed to unmarshal Redis message")
                    continue
                }

                select {
                case msgCh <- &msg:
                    // Message sent to channel
                case <-r.ctx.Done():
                    return
                default:
                    // Channel full, drop message (backpressure)
                    r.logger.Warn().Msg("Redis subscriber channel full, dropping message")
                }

            case <-r.ctx.Done():
                return
            }
        }
    }()

    return msgCh
}

func (r *RedisBroadcastBus) Run() {
    r.logger.Info().
        Str("channel", r.channel).
        Msg("Redis BroadcastBus ready")
}

func (r *RedisBroadcastBus) Shutdown() {
    r.logger.Info().Msg("Shutting down Redis BroadcastBus")
    r.cancel()
    r.client.Close()
}
```

### Factory Pattern for Initialization

```go
// ws/cmd/multi/main.go (excerpt)

func createBroadcastBus(config Config, logger zerolog.Logger) (multi.BroadcastBus, error) {
    switch config.BroadcastBusType {
    case "nats":
        logger.Info().Str("url", config.NatsURL).Msg("Using NATS BroadcastBus")
        return multi.NewNatsBroadcastBus(config.NatsURL, logger)

    case "redis":
        logger.Info().Str("url", config.RedisURL).Msg("Using Redis BroadcastBus")
        return multi.NewRedisBroadcastBus(config.RedisURL, logger)

    case "memory":
        logger.Info().Msg("Using in-memory BroadcastBus (single instance only)")
        return multi.NewBroadcastBus(1024, logger), nil

    default:
        return nil, fmt.Errorf("unknown broadcast bus type: %s", config.BroadcastBusType)
    }
}

func main() {
    // ... config loading ...

    // Create broadcast bus
    broadcastBus, err := createBroadcastBus(cfg, logger)
    if err != nil {
        logger.Fatal().Err(err).Msg("Failed to create broadcast bus")
    }
    defer broadcastBus.Shutdown()

    broadcastBus.Run()

    // ... rest of initialization ...
}
```

---

## Monitoring & Observability

### Health Checks

```go
// ws/internal/multi/broadcast_bus_health.go
package multi

import (
    "sync"
    "time"
)

type BroadcastBusHealth struct {
    bus            BroadcastBus
    lastSuccessful time.Time
    mu             sync.RWMutex
}

func NewBroadcastBusHealth(bus BroadcastBus) *BroadcastBusHealth {
    return &BroadcastBusHealth{
        bus:            bus,
        lastSuccessful: time.Now(),
    }
}

func (h *BroadcastBusHealth) HealthCheck() error {
    // Test publish
    testMsg := &BroadcastMessage{
        Subject: "health.check",
        Message: []byte(`{"type":"ping"}`),
    }

    if err := h.bus.Publish(testMsg); err != nil {
        return fmt.Errorf("broadcast bus unhealthy: %w", err)
    }

    h.mu.Lock()
    h.lastSuccessful = time.Now()
    h.mu.Unlock()

    return nil
}

func (h *BroadcastBusHealth) IsHealthy() bool {
    h.mu.RLock()
    defer h.mu.RUnlock()

    // Consider unhealthy if no successful publish in last 10s
    return time.Since(h.lastSuccessful) < 10*time.Second
}

func (h *BroadcastBusHealth) LastSuccessful() time.Time {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return h.lastSuccessful
}
```

### Prometheus Metrics

```go
// ws/internal/multi/broadcast_bus_metrics.go
package multi

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    broadcastBusPublishTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "broadcast_bus_publish_total",
            Help: "Total messages published to broadcast bus",
        },
        []string{"status"}, // success, error
    )

    broadcastBusPublishDuration = promauto.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "broadcast_bus_publish_duration_seconds",
            Help:    "Latency of broadcast bus publish operations",
            Buckets: []float64{.0001, .0002, .0005, .001, .002, .005, .01, .02, .05},
        },
    )

    broadcastBusSubscriberCount = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "broadcast_bus_subscribers",
            Help: "Number of active broadcast bus subscribers",
        },
    )

    broadcastBusHealth = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "broadcast_bus_healthy",
            Help: "Health status of broadcast bus (1=healthy, 0=unhealthy)",
        },
    )

    broadcastBusMessagesDropped = promauto.NewCounter(
        prometheus.CounterOpts{
            Name: "broadcast_bus_messages_dropped_total",
            Help: "Total messages dropped due to channel full",
        },
    )
)

// Instrumented wrapper
type InstrumentedBroadcastBus struct {
    bus BroadcastBus
}

func NewInstrumentedBroadcastBus(bus BroadcastBus) *InstrumentedBroadcastBus {
    return &InstrumentedBroadcastBus{bus: bus}
}

func (i *InstrumentedBroadcastBus) Publish(msg *BroadcastMessage) error {
    start := time.Now()

    err := i.bus.Publish(msg)

    duration := time.Since(start)
    broadcastBusPublishDuration.Observe(duration.Seconds())

    if err != nil {
        broadcastBusPublishTotal.WithLabelValues("error").Inc()
    } else {
        broadcastBusPublishTotal.WithLabelValues("success").Inc()
    }

    return err
}

func (i *InstrumentedBroadcastBus) Subscribe() chan *BroadcastMessage {
    ch := i.bus.Subscribe()
    broadcastBusSubscriberCount.Inc()
    return ch
}

func (i *InstrumentedBroadcastBus) Run() {
    i.bus.Run()
}

func (i *InstrumentedBroadcastBus) Shutdown() {
    i.bus.Shutdown()
}
```

### Alerting Rules

```yaml
# prometheus-alerts.yml
groups:
  - name: broadcast_bus
    interval: 30s
    rules:
      - alert: BroadcastBusUnhealthy
        expr: broadcast_bus_healthy == 0
        for: 30s
        labels:
          severity: critical
        annotations:
          summary: "Broadcast bus is unhealthy"
          description: "The broadcast bus has been unhealthy for >30s"

      - alert: BroadcastBusHighLatency
        expr: histogram_quantile(0.99, rate(broadcast_bus_publish_duration_seconds_bucket[5m])) > 0.005
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Broadcast bus p99 latency >5ms"
          description: "p99 latency is {{ $value }}s (>5ms threshold)"

      - alert: BroadcastBusHighErrorRate
        expr: rate(broadcast_bus_publish_total{status="error"}[5m]) > 10
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Broadcast bus error rate >10/sec"
          description: "Error rate is {{ $value }}/sec"

      - alert: BroadcastBusMessagesDropping
        expr: rate(broadcast_bus_messages_dropped_total[5m]) > 1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Broadcast bus dropping messages"
          description: "Dropping {{ $value }} messages/sec (backpressure issue)"
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "Broadcast Bus Metrics",
    "panels": [
      {
        "title": "Publish Rate",
        "targets": [
          {
            "expr": "rate(broadcast_bus_publish_total[5m])",
            "legendFormat": "{{status}}"
          }
        ]
      },
      {
        "title": "Publish Latency (p50, p95, p99)",
        "targets": [
          {
            "expr": "histogram_quantile(0.50, rate(broadcast_bus_publish_duration_seconds_bucket[5m]))",
            "legendFormat": "p50"
          },
          {
            "expr": "histogram_quantile(0.95, rate(broadcast_bus_publish_duration_seconds_bucket[5m]))",
            "legendFormat": "p95"
          },
          {
            "expr": "histogram_quantile(0.99, rate(broadcast_bus_publish_duration_seconds_bucket[5m]))",
            "legendFormat": "p99"
          }
        ]
      },
      {
        "title": "Health Status",
        "targets": [
          {
            "expr": "broadcast_bus_healthy",
            "legendFormat": "Healthy (1=yes, 0=no)"
          }
        ]
      },
      {
        "title": "Active Subscribers",
        "targets": [
          {
            "expr": "broadcast_bus_subscribers",
            "legendFormat": "Subscribers"
          }
        ]
      }
    ]
  }
}
```

---

## Implementation Plan (Revised)

### Phase 1A: Deploy Self-Hosted NATS on e2-standard-small (Week 1-2)

**Strategy**: Start with self-hosted NATS on a single e2-standard-small GCP instance for initial validation and testing. This provides a low-cost, low-risk way to validate the architecture before committing to managed services.

**Why e2-standard-small?**
```
GCP e2-standard-small Specs:
  vCPUs:    2 shared cores
  Memory:   2 GB
  Cost:     ~$14/month (us-central1)
  Network:  Up to 10 Gbps

NATS Resource Requirements (single node):
  CPU:      0.1-0.5 cores (under load)
  Memory:   50-200 MB (your workload)
  Headroom: 4× CPU, 10× memory available

Perfect fit for:
  ✓ Dev/staging environments
  ✓ Initial production validation
  ✓ Up to 50K connections
  ✓ Up to 100K msg/sec
```

**Goals**:
- Deploy single NATS server on e2-standard-small
- Validate NATS performance with your workload
- Set up monitoring and alerting
- Prepare for production multi-node or managed migration

**Tasks**:

1. **Provision GCP Instance**
   ```bash
   # Create e2-standard-small instance
   gcloud compute instances create nats-server \
     --machine-type=e2-standard-small \
     --zone=us-central1-a \
     --image-family=ubuntu-2204-lts \
     --image-project=ubuntu-os-cloud \
     --boot-disk-size=20GB \
     --boot-disk-type=pd-standard \
     --tags=nats-server

   # Create firewall rule for NATS
   gcloud compute firewall-rules create allow-nats \
     --allow=tcp:4222 \
     --target-tags=nats-server \
     --source-ranges=10.0.0.0/8  # Internal network only
   ```

2. **Install NATS Server**
   ```bash
   # SSH to instance
   gcloud compute ssh nats-server --zone=us-central1-a

   # Install NATS
   wget https://github.com/nats-io/nats-server/releases/download/v2.10.7/nats-server-v2.10.7-linux-amd64.tar.gz
   tar xzf nats-server-v2.10.7-linux-amd64.tar.gz
   sudo mv nats-server-v2.10.7-linux-amd64/nats-server /usr/local/bin/

   # Create systemd service
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

   # Start NATS
   sudo useradd -r -s /bin/false nats
   sudo systemctl daemon-reload
   sudo systemctl enable nats
   sudo systemctl start nats

   # Verify
   curl http://localhost:8222/varz
   ```

3. **Configure Monitoring**
   ```bash
   # Install Prometheus Node Exporter (optional)
   wget https://github.com/prometheus/node_exporter/releases/download/v1.7.0/node_exporter-1.7.0.linux-amd64.tar.gz
   tar xzf node_exporter-1.7.0.linux-amd64.tar.gz
   sudo mv node_exporter-1.7.0.linux-amd64/node_exporter /usr/local/bin/

   # Create node_exporter service
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

   sudo useradd -r -s /bin/false prometheus
   sudo systemctl enable node_exporter
   sudo systemctl start node_exporter
   ```

4. **Update ws-server Configuration**
   ```bash
   # Update environment variable
   export NATS_URL="nats://10.x.x.x:4222"  # Internal IP of NATS instance

   # In your deployment config
   BROADCAST_BUS_TYPE=nats
   NATS_URL=nats://nats-server.c.your-project.internal:4222
   ```

5. **Set up Prometheus Scraping**
   ```yaml
   # Add to prometheus.yml
   scrape_configs:
     - job_name: 'nats'
       static_configs:
         - targets: ['nats-server:8222']
       metrics_path: '/varz'
       scheme: http
   ```

6. **Create Grafana Dashboard**
   - Import NATS dashboard template
   - Monitor: CPU, memory, connections, msg/sec, latency
   - Set alerts for high latency or connection failures

**Success Criteria**:
- [ ] NATS server running on e2-standard-small
- [ ] ws-server instances can connect to NATS
- [ ] Latency <0.5ms p99 for publish operations
- [ ] Metrics visible in Grafana
- [ ] Handles 2× current load (2K msg/sec) without issues
- [ ] Memory usage <500MB under load

**Cost Analysis (Phase 1A)**:
```
e2-standard-small:          $14/month
Network egress (internal):  $0 (free within region)
Monitoring overhead:        $0 (existing Prometheus/Grafana)
──────────────────────────────────
Total:                      $14/month

vs. Managed NATS (preview):
Synadia Cloud Starter:      $99/month (minimum)
──────────────────────────────────
Savings:                    $85/month during testing phase
```

### Phase 1B: Migrate to Managed NATS (Week 6-7, Post-Validation)

**Strategy**: Once self-hosted NATS is validated in production for 4+ weeks, migrate to managed NATS for improved reliability, auto-scaling, and reduced operational overhead.

**Managed NATS Options**:

#### Option 1: Synadia Cloud (Recommended)

**Official managed NATS service** from the creators of NATS.

```
Synadia Cloud Pricing:
  Starter:     $99/month   (1M msgs/day, 100 clients)
  Growth:      $299/month  (100M msgs/day, 1K clients)
  Business:    $999/month  (1B msgs/day, 10K clients)
  Enterprise:  Custom      (unlimited)

Your Needs:
  Messages:    ~2.5B/month (1K msg/sec × 30 days)
  Clients:     2-4 ws-server instances
  Recommended: Growth plan ($299/month)

Features:
  ✓ Multi-region deployment
  ✓ Auto-scaling
  ✓ 99.95% SLA
  ✓ Built-in monitoring
  ✓ Automatic backups (JetStream)
  ✓ Expert support from NATS team
```

**Setup**:
```bash
# 1. Sign up at https://www.synadia.com/cloud
# 2. Create account and select region (us-central1)
# 3. Create "ws-broadcast" account
# 4. Get connection credentials

# 5. Update ws-server config
export NATS_URL="nats://connect.ngs.global:4222"
export NATS_CREDS="/path/to/synadia-credentials.creds"

# 6. Update NatsBroadcastBus code to use credentials
conn, err := nats.Connect(natsURL,
    nats.UserCredentials(credsFile),  // Add this
    nats.MaxReconnects(-1),
    // ... rest of config
)
```

#### Option 2: Self-Hosted 3-Node Cluster (Alternative)

**If you prefer to maintain control** and avoid vendor lock-in:

```
3× e2-standard-small instances:
  Cost:         3 × $14 = $42/month
  Redundancy:   High (survives 1 node failure)
  Ops burden:   Medium (you manage updates, monitoring)

vs. Synadia Cloud Growth:
  Cost:         $299/month
  Redundancy:   Very High (multi-region, auto-failover)
  Ops burden:   Very Low (fully managed)

Trade-off:
  Save $257/month but increase ops complexity
  Good if: Team has strong ops experience
  Bad if: Want to focus on application development
```

**3-Node Cluster Setup** (if choosing self-hosted):
```yaml
# Use Docker Compose or Terraform to deploy
# See earlier section for 3-node cluster config

# Each node on separate e2-standard-small instance
nats-1.c.project.internal:4222
nats-2.c.project.internal:4222
nats-3.c.project.internal:4222

# Update client config
export NATS_URL="nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222"
```

#### Option 3: GKE with Helm Chart (Over-Engineered)

**Only if you're already running GKE** for other services:

```
GKE Cluster Cost:
  3× e2-small nodes:    ~$60/month
  GKE management:       $74/month (mandatory)
  Total:                $134/month

Pros:
  ✓ Container orchestration
  ✓ Auto-healing, auto-scaling
  ✓ Fits existing k8s infrastructure

Cons:
  ✗ Higher cost than e2-standard-small
  ✗ More complex (k8s overhead)
  ✗ Overkill for simple NATS deployment

Recommendation: Skip unless already on GKE
```

**Migration Steps (Self-Hosted → Managed)**:

1. **Parallel Deployment (Week 6, Day 1-3)**
   ```
   Keep:  e2-standard-small NATS (existing)
   Add:   Synadia Cloud account (new)

   Deploy dual-connection mode:
     - ws-server connects to BOTH
     - Publishes to BOTH
     - Subscribes from PRIMARY (self-hosted)
     - Monitor message consistency
   ```

2. **Traffic Split (Week 6, Day 4-5)**
   ```
   10% traffic → Synadia Cloud
   90% traffic → Self-hosted

   Monitor for 24 hours:
     - Latency comparison
     - Error rates
     - Message delivery
   ```

3. **Gradual Migration (Week 6, Day 6-7)**
   ```
   Day 6: 25% → Synadia, 75% → Self-hosted
   Day 7: 50% → Synadia, 50% → Self-hosted
   ```

4. **Full Cutover (Week 7, Day 1)**
   ```
   100% traffic → Synadia Cloud

   Keep self-hosted running for 48 hours as fallback
   Monitor:
     - All metrics green
     - No increase in errors
     - Latency within SLA
   ```

5. **Decommission Self-Hosted (Week 7, Day 3)**
   ```
   Shutdown e2-standard-small instance
   Remove firewall rules
   Update runbooks

   Savings: $14/month
   New cost: $299/month (Synadia Growth)
   Net increase: $285/month for managed service
   ```

**Decision Matrix**:

| Approach | Cost/Month | Ops Burden | Reliability | Best For |
|----------|-----------|------------|-------------|----------|
| **e2-standard-small (single)** | $14 | Low | Good | Dev/Staging/Initial Prod |
| **3× e2-standard-small** | $42 | Medium | High | Cost-conscious prod |
| **Synadia Cloud Growth** | $299 | Very Low | Very High | Production (recommended) |
| **GKE Deployment** | $134+ | High | High | Existing k8s infrastructure |

**Recommendation for Phase 1B**:
```
Start: e2-standard-small ($14/month)
  ↓ (validate for 4-6 weeks)
Migrate to: Synadia Cloud Growth ($299/month)

Rationale:
  ✓ Low initial investment for validation
  ✓ Managed service for production reliability
  ✓ Focus ops time on application, not infrastructure
  ✓ Expert support from NATS team
  ✓ 99.95% SLA (vs. best-effort self-hosted)
```

**Success Criteria (Phase 1B)**:
- [ ] Managed NATS account created and configured
- [ ] Migration completed with zero downtime
- [ ] Latency remains <0.5ms p99
- [ ] All error rates unchanged or improved
- [ ] Self-hosted instance decommissioned
- [ ] Runbooks updated with managed NATS procedures
- [ ] Team trained on Synadia Cloud dashboard

### Phase 2: Implement NatsBroadcastBus (Week 2)

**Goals**:
- Create NATS implementation of BroadcastBus interface
- Add health checks and metrics
- Unit test with mock NATS

**Tasks**:
1. ✓ Implement `NatsBroadcastBus` in `ws/internal/multi/nats_broadcast_bus.go`
2. ✓ Add health check wrapper
3. ✓ Add metrics instrumentation
4. ✓ Write unit tests (use `gnatsd` test server)
5. ✓ Add integration test (real NATS cluster)

**Success Criteria**:
- [ ] `NatsBroadcastBus` implements `BroadcastBus` interface
- [ ] Unit tests passing (>80% coverage)
- [ ] Health checks working
- [ ] Metrics exposed

### Phase 3: Shadow Mode Testing (Week 3)

**Goals**:
- Run NATS in parallel with in-memory bus
- Verify message consistency
- Monitor latency and errors

**Tasks**:
1. ✓ Deploy dual-bus mode:
   - Publish to both in-memory AND NATS
   - Subscribe from both
   - Compare messages received
2. ✓ Run for 48 hours in dev environment
3. ✓ Monitor:
   - Latency comparison (in-memory vs NATS)
   - Message delivery consistency
   - Error rates
4. ✓ Load test: 2× current traffic

**Success Criteria**:
- [ ] 100% message consistency (in-memory == NATS)
- [ ] NATS latency <0.5ms p99
- [ ] Zero errors over 48 hours
- [ ] Handles 2× traffic without issues

### Phase 4: Gradual Rollout (Week 4)

**Goals**:
- Gradually shift from in-memory to NATS
- Monitor error rates and rollback if needed

**Tasks**:
1. ✓ 10% of messages via NATS (random selection)
   - Monitor for 24 hours
   - Rollback if error rate >1%
2. ✓ 25% of messages via NATS
   - Monitor for 24 hours
3. ✓ 50% of messages via NATS
   - Monitor for 48 hours
4. ✓ 75% of messages via NATS
   - Monitor for 24 hours
5. ✓ 100% via NATS (disable in-memory)
   - Monitor for 1 week

**Success Criteria**:
- [ ] No increase in error rates at any stage
- [ ] Latency remains <10ms end-to-end
- [ ] No client disconnections due to missing messages

### Phase 5: Multi-Instance Deployment (Week 5)

**Goals**:
- Deploy 2 instances behind GCP Load Balancer
- Verify all clients get all messages
- Load test at scale

**Tasks**:
1. ✓ Deploy GCP TCP Load Balancer
2. ✓ Deploy 2 ws-server instances
3. ✓ Verify Kafka partition assignment (48 + 48)
4. ✓ Verify NATS connections (both instances connected)
5. ✓ End-to-end test:
   - Client A on Instance 1 subscribes to BTC
   - Message for BTC arrives on Instance 2's Kafka partitions
   - Instance 2 publishes to NATS
   - Instance 1 receives from NATS
   - Client A gets the message ✓
6. ✓ Load test: 18K connections, 1K msg/sec

**Success Criteria**:
- [ ] All clients on both instances receive all messages
- [ ] No message loss
- [ ] Latency <10ms end-to-end
- [ ] System stable under load

### Phase 6: Production Validation (Week 6)

**Goals**:
- Run in production with monitoring
- Validate metrics and alerts
- Document runbooks

**Tasks**:
1. ✓ Deploy to production
2. ✓ Monitor for 1 week:
   - Latency (p50, p95, p99)
   - Error rates
   - Message throughput
   - NATS cluster health
3. ✓ Test failover scenarios:
   - Kill one NATS node (verify <1s recovery)
   - Kill one ws-server instance (verify GCP LB routes to other)
   - Network partition (verify reconnect)
4. ✓ Document operational runbooks

**Success Criteria**:
- [ ] Zero production incidents
- [ ] Latency within SLA (<10ms)
- [ ] All alerts working correctly
- [ ] Runbooks documented

---

## Recommendation

### Use NATS 3-Node Cluster

**Reasons**:

1. **Lower Latency**: <0.5ms vs 1-2ms (critical for real-time WebSockets)
2. **Simpler Setup**: 3 identical nodes vs complex master/replica/sentinel
3. **Faster Failover**: <1s vs 5-15s
4. **Lower Resources**: 150-300MB vs 1.6GB
5. **Purpose-Built**: Designed for messaging, not general-purpose
6. **Better for Your Scale**: 10,000× headroom vs 1,000×

### Deployment Configuration

```yaml
Environment Variables:
  BROADCAST_BUS_TYPE=nats
  NATS_URL=nats://nats-1:4222,nats://nats-2:4222,nats://nats-3:4222

Infrastructure:
  - 3 NATS nodes (Docker containers or VMs)
  - Can run on existing instances (minimal overhead)
  - Prometheus for metrics
  - Grafana for dashboards
  - Alertmanager for alerts

Cost:
  - Minimal (150-300MB RAM total)
  - No additional VMs needed
```

### Timeline

```
Week 1: Deploy NATS cluster
Week 2: Implement NatsBroadcastBus
Week 3: Shadow mode testing
Week 4: Gradual rollout
Week 5: Multi-instance deployment
Week 6: Production validation

Total: 6 weeks to production
```

### Success Metrics

```
Latency:          <0.5ms p99 (NATS publish)
                  <10ms p99 (end-to-end)
Throughput:       1,000 msg/sec (current)
                  10,000 msg/sec (capacity)
Availability:     99.9% (survive 1 node failure)
Failover:         <1 second
Error Rate:       <0.01%
Message Loss:     Zero
```

---

## Risks & Mitigation

### Risk 1: NATS Cluster Failure (All 3 Nodes)

**Probability**: Low
**Impact**: High (all messaging stops)

**Mitigation**:
- Deploy across multiple availability zones
- Monitor cluster health (alert if <2 nodes healthy)
- Keep in-memory fallback code (disabled but available)
- Runbook for emergency failover to in-memory mode

### Risk 2: Performance Regression

**Probability**: Medium
**Impact**: Medium (increased latency)

**Mitigation**:
- Shadow mode testing (Week 3)
- Gradual rollout with rollback plan
- Monitor latency at each stage
- Automatic rollback if p99 >5ms

### Risk 3: Message Loss During Failover

**Probability**: Very Low (NATS cluster syncs)
**Impact**: High (client state inconsistency)

**Mitigation**:
- NATS cluster ensures no message loss
- Monitor dropped message counter
- Alert if any messages dropped
- Client reconnect logic handles transient failures

### Risk 4: Operational Complexity

**Probability**: Medium
**Impact**: Low (ops burden increases)

**Mitigation**:
- Comprehensive runbooks
- Automated monitoring and alerts
- Health checks and circuit breakers
- On-call training

---

## Open Questions

1. **Should we use NATS JetStream for durability?**
   - Pro: Persistence, replay capability
   - Con: Higher latency (1-2ms vs <0.5ms)
   - **Recommendation**: Start with Core NATS, add JetStream if needed later

2. **Should we add message deduplication?**
   - Pro: Prevents duplicate delivery
   - Con: Adds complexity and latency
   - **Recommendation**: Not needed (at-most-once delivery is acceptable for real-time data)

3. **Should we shard NATS subjects by topic?**
   - Current: Single subject "ws.broadcast"
   - Alternative: Per-topic subjects ("ws.trades", "ws.liquidity", etc.)
   - **Recommendation**: Start with single subject (simpler), shard later if needed

4. **Should we run NATS on same VMs as ws-server?**
   - Pro: Lower latency (localhost), lower cost
   - Con: Resource contention, failure correlation
   - **Recommendation**: Separate VMs for production (better isolation)

---

## References

- [NATS Documentation](https://docs.nats.io/)
- [NATS Clustering](https://docs.nats.io/running-a-nats-service/configuration/clustering)
- [Redis Pub/Sub](https://redis.io/docs/manual/pubsub/)
- [Redis Sentinel](https://redis.io/docs/management/sentinel/)
- [Kafka Partitions Explained](./KAFKA_PARTITIONS_EXPLAINED.md)
- [Horizontal Scaling Plan](./HORIZONTAL_SCALING_PLAN.md)

---

## Conclusion

**Recommendation**: Implement **NATS 3-node cluster** as the external broadcast bus.

NATS provides the best balance of:
- ✅ Low latency (<0.5ms)
- ✅ High throughput (10M+ msg/sec)
- ✅ Simple operation (3 identical nodes)
- ✅ Fast failover (<1 second)
- ✅ Low resource usage (150-300MB)
- ✅ Purpose-built for messaging

This enables true multi-instance deployment with minimal operational overhead and excellent performance characteristics for real-time WebSocket broadcasting.

**Next Steps**: Proceed with Phase 1 (Deploy NATS Cluster) and validate in dev environment.
