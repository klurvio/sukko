# NATS → Redpanda Migration Plan

**Status:** Planning  
**Timeline:** 12-16 days (~2-3 weeks)  
**Approach:** Direct replacement (no dual-mode)  
**Deployment:** Self-hosted Redpanda on GCP  
**Date:** January 2025

---

## Executive Summary

Replace NATS JetStream with Redpanda to implement coarse-grained event channels for the Sukko WebSocket system. The migration includes:

- **8 event types** mapped to 8 Redpanda topics
- **Subscription bundles** (TRADING, FULL_MARKET, COMMUNITY, etc.)
- **Self-hosted Redpanda** on existing GCP infrastructure
- **Latest libraries:** franz-go v1.18 (Go), kafkajs v2.2 (Node.js)
- **Zero message loss** during migration
- **<5ms latency** increase vs current NATS setup

---

## Current Architecture

```
API Backend (Elixir)
    ↓ Events
Publisher (Node.js + NATS)
    ↓ Subject: sukko.token.{SYMBOL}.{EVENT}
NATS JetStream
    ↓ Subscribe: sukko.token.>
WS Server (Go)
    ↓ Broadcast: {SYMBOL}.trade only
12,000 WebSocket Clients
```

**Current Limitations:**
- ❌ Only 1 of 8 event types implemented (trade)
- ❌ No event type granularity
- ❌ Clients can't subscribe to specific event types
- ❌ 87.5% of planned events unused

---

## Target Architecture

```
API Backend (Elixir)
    ↓ Events
Publisher (Node.js + kafkajs)
    ↓ Topics: 8 topics × event types
Redpanda (Self-hosted)
    ↓ Consume with Consumer Groups
WS Server (Go + franz-go)
    ↓ Broadcast: {SYMBOL}.{EVENT_TYPE}
12,000 WebSocket Clients
    ↓ Subscribe to bundles or channels
```

**Improvements:**
- ✅ All 8 event types implemented
- ✅ Client-side filtering via subscription bundles
- ✅ Scalable topic architecture
- ✅ Better monitoring (Redpanda Console)

---

## Event Type Mapping

Based on `/docs/events/TOKEN_UPDATE_EVENTS.md`:

| Event Category | Topic | NATS Subject (old) | Events Included |
|----------------|-------|-------------------|-----------------|
| **1. Trading** | `sukko.trades` | `sukko.token.{TOKEN}.trade` | Buy, sell, external trades |
| **2. Liquidity** | `sukko.liquidity` | `sukko.token.{TOKEN}.liquidity` | Add/remove liquidity, rebalances |
| **3. Metadata** | `sukko.metadata` | `sukko.token.{TOKEN}.metadata` | Name, ticker, description, flags |
| **4. Social** | `sukko.social` | `sukko.token.{TOKEN}.social` | Twitter verification, social links |
| **5. Community** | `sukko.community` | `sukko.token.{TOKEN}.community` | Comments, pins, upvotes, favorites |
| **6. Creation** | `sukko.creation` | `sukko.token.{TOKEN}.creation` | Token creation, initial listing |
| **7. Analytics** | `sukko.analytics` | `sukko.token.{TOKEN}.analytics` | Price deltas, holder counts, trending |
| **8. Balances** | `sukko.balances` | `sukko.token.{TOKEN}.balances` | User balance updates |

---

## Topic Architecture

### Design: One Topic Per Event Type

```
Topics (8 total):
├── sukko.trades        (high volume, 30s retention)
├── sukko.liquidity     (medium volume, 1min retention)
├── sukko.metadata      (low volume, 1hr retention)
├── sukko.social        (low volume, 1hr retention)
├── sukko.community     (medium volume, 5min retention)
├── sukko.creation      (very low volume, 1hr retention)
├── sukko.analytics     (high volume, 5min retention)
└── sukko.balances      (high volume, 30s retention)

Message Structure:
  Key: {TOKEN_ID}        (e.g., "BTC", "ETH")
  Value: JSON event data
  Timestamp: Unix epoch milliseconds
  
Partitions: 12 per topic
Replication: 1 (single node dev, 3 for production)
Compression: Snappy
```

**Why This Design:**

✅ **Simple:** Only 8 topics (vs 8×N for per-token topics)  
✅ **Scalable:** Add new tokens without creating topics  
✅ **Flexible:** Different retention per event type  
✅ **Load-balanced:** Key-based partitioning distributes tokens  

---

## Subscription Bundles

Clients subscribe to **bundles** that group related channels:

```typescript
export const SubscriptionBundles = {
  // Trading-focused clients (most common)
  TRADING: ['trade', 'liquidity', 'analytics'],
  
  // Full market data (institutional/analytics)
  FULL_MARKET: ['trade', 'liquidity', 'analytics', 'metadata'],
  
  // Community/social clients
  COMMUNITY: ['community', 'social', 'metadata'],
  
  // Portfolio tracking
  PORTFOLIO: ['trade', 'analytics', 'balances'],
  
  // Minimal (price ticker only)
  PRICE_ONLY: ['trade', 'analytics'],
  
  // Everything (admin/monitoring)
  ALL: ['trade', 'liquidity', 'metadata', 'social', 'community', 
        'creation', 'analytics', 'balances'],
};
```

**Client Usage Examples:**

```javascript
// Subscribe to trading bundle for BTC, ETH, SOL
ws.send({
  type: 'subscribe',
  bundle: 'TRADING',  // → [trade, liquidity, analytics]
  tokens: ['BTC', 'ETH', 'SOL']
  // Expands to: BTC.trade, BTC.liquidity, BTC.analytics, ...
});

// Or fine-grained control
ws.send({
  type: 'subscribe',
  channels: ['BTC.trade', 'BTC.analytics', 'ETH.trade']
});
```

---

## Technology Stack

### Redpanda

- **Version:** v24.2.11 (latest stable, January 2025)
- **Image:** `docker.redpanda.com/redpandadata/redpanda:v24.2.11`
- **Console:** `docker.redpanda.com/redpandadata/console:v2.7.2`
- **Deployment:** Self-hosted on GCP (backend instance)

### Client Libraries

**Go (WS Server):**
- **Library:** `github.com/twmb/franz-go` v1.18.0
- **Admin:** `github.com/twmb/franz-go/pkg/kadm` v1.14.0
- **Why:** 2-3x faster than Sarama, modern API, used by Redpanda team

**Node.js (Publisher):**
- **Library:** `kafkajs` v2.2.4
- **Why:** Most popular, active maintenance, good TypeScript support

---

## Implementation Plan

### Phase 1: Infrastructure Setup (2-3 days)

#### 1.1 Self-Hosted Redpanda Deployment

**Docker Compose Configuration:**

File: `deployments/gcp-distributed/backend/docker-compose.yml`

```yaml
version: "3.8"

services:
  redpanda:
    image: docker.redpanda.com/redpandadata/redpanda:v24.2.11
    container_name: redpanda
    command:
      - redpanda
      - start
      - --kafka-addr internal://0.0.0.0:9092,external://0.0.0.0:19092
      - --advertise-kafka-addr internal://redpanda:9092,external://0.0.0.0:19092
      - --pandaproxy-addr internal://0.0.0.0:8082,external://0.0.0.0:18082
      - --advertise-pandaproxy-addr internal://redpanda:8082,external://0.0.0.0:18082
      - --schema-registry-addr internal://0.0.0.0:8081,external://0.0.0.0:18081
      - --rpc-addr redpanda:33145
      - --advertise-rpc-addr redpanda:33145
      - --mode dev-container
      - --smp 2
      - --memory 4G
      - --reserve-memory 1G
      - --overprovisioned
    ports:
      - "19092:19092"  # Kafka API (external)
      - "18081:18081"  # Schema Registry
      - "18082:18082"  # HTTP Proxy
      - "9644:9644"    # Admin API
    volumes:
      - redpanda-data:/var/lib/redpanda/data
    healthcheck:
      test: ["CMD-SHELL", "rpk cluster health | grep 'Healthy:.*true' || exit 1"]
      interval: 15s
      timeout: 3s
      retries: 5
    deploy:
      resources:
        limits:
          memory: 4G
          cpus: "2.0"
    networks:
      - backend

  console:
    image: docker.redpanda.com/redpandadata/console:v2.7.2
    container_name: redpanda-console
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      KAFKA_BROKERS: redpanda:9092
      KAFKA_SCHEMAREGISTRY_ENABLED: "true"
      KAFKA_SCHEMAREGISTRY_URLS: http://redpanda:8081
    depends_on:
      - redpanda
    networks:
      - backend

  publisher:
    build:
      context: ../../publisher
      dockerfile: Dockerfile
    container_name: sukko-publisher
    restart: unless-stopped
    ports:
      - "3003:3003"
    environment:
      KAFKA_BROKERS: redpanda:9092
      NODE_ENV: production
    depends_on:
      - redpanda
    networks:
      - backend

  prometheus:
    # ... existing Prometheus config

  grafana:
    # ... existing Grafana config

volumes:
  redpanda-data:
  prometheus-data:
  grafana-data:

networks:
  backend:
    driver: bridge
```

**Resource Requirements:**
- CPU: 2 cores
- Memory: 4GB
- Storage: 100GB SSD
- Fits on existing e2-small backend instance

#### 1.2 Topic Creation Script

File: `scripts/setup-redpanda-topics.sh`

```bash
#!/bin/bash
# Setup Redpanda topics with appropriate configs

BROKERS=${KAFKA_BROKERS:-localhost:19092}

# Topic configurations: name -> retention.ms
declare -A TOPICS=(
  ["sukko.trades"]="30000"         # 30s
  ["sukko.liquidity"]="60000"      # 1min
  ["sukko.metadata"]="3600000"     # 1hr
  ["sukko.social"]="3600000"       # 1hr
  ["sukko.community"]="300000"     # 5min
  ["sukko.creation"]="3600000"     # 1hr
  ["sukko.analytics"]="300000"     # 5min
  ["sukko.balances"]="30000"       # 30s
)

for topic in "${!TOPICS[@]}"; do
  echo "Creating topic: $topic (retention: ${TOPICS[$topic]}ms)"
  
  rpk topic create "$topic" \
    --brokers "$BROKERS" \
    --partitions 12 \
    --replicas 1 \
    --config "retention.ms=${TOPICS[$topic]}" \
    --config segment.ms=10000 \
    --config cleanup.policy=delete \
    --config compression.type=snappy \
    --config max.message.bytes=1048576 \
    --config min.insync.replicas=1
    
  if [ $? -eq 0 ]; then
    echo "✅ Created: $topic"
  else
    echo "❌ Failed: $topic"
  fi
done

echo ""
echo "📋 Topic List:"
rpk topic list --brokers "$BROKERS"

echo ""
echo "📊 Topic Details:"
for topic in "${!TOPICS[@]}"; do
  rpk topic describe "$topic" --brokers "$BROKERS"
done
```

**Make executable:**
```bash
chmod +x scripts/setup-redpanda-topics.sh
```

#### 1.3 Deploy Infrastructure

```bash
# Deploy backend with Redpanda
cd deployments/gcp-distributed/backend
docker-compose up -d

# Wait for healthy
docker-compose ps
docker logs redpanda

# Create topics
../../scripts/setup-redpanda-topics.sh

# Verify in console
open http://backend-ip:8080
```

**Verification Checklist:**
- [ ] Redpanda container running
- [ ] Console accessible at :8080
- [ ] All 8 topics created
- [ ] Topics have correct retention
- [ ] Health check passing

---

### Phase 2: Publisher Implementation (3-4 days)

#### 2.1 Dependencies

**Update `publisher/package.json`:**

```json
{
  "name": "sukko-publisher",
  "version": "2.0.0",
  "type": "module",
  "scripts": {
    "dev": "tsx watch publisher.ts",
    "build": "tsc",
    "start": "node dist/publisher.js"
  },
  "dependencies": {
    "kafkajs": "^2.2.4",
    "express": "^4.21.2",
    "cors": "^2.8.5",
    "dotenv": "^16.4.7"
  },
  "devDependencies": {
    "@types/node": "^22.10.5",
    "@types/express": "^5.0.0",
    "@types/cors": "^2.8.17",
    "typescript": "^5.7.3",
    "tsx": "^4.19.2"
  }
}
```

**Remove NATS, add Kafka:**
```bash
cd publisher
npm uninstall nats
npm install kafkajs@^2.2.4
npm install --save-dev @types/node@latest
```

#### 2.2 Event Type Mapping

File: `publisher/types/event-types.ts`

```typescript
export const EVENT_TYPE_TOPICS = {
  // Trading events
  TRADE_EXECUTED: 'sukko.trades',
  BUY_COMPLETED: 'sukko.trades',
  SELL_COMPLETED: 'sukko.trades',
  
  // Liquidity events
  LIQUIDITY_ADDED: 'sukko.liquidity',
  LIQUIDITY_REMOVED: 'sukko.liquidity',
  LIQUIDITY_REBALANCED: 'sukko.liquidity',
  
  // Metadata events
  METADATA_UPDATED: 'sukko.metadata',
  TOKEN_NAME_CHANGED: 'sukko.metadata',
  TOKEN_FLAGS_CHANGED: 'sukko.metadata',
  
  // Social events
  TWITTER_VERIFIED: 'sukko.social',
  SOCIAL_LINKS_UPDATED: 'sukko.social',
  
  // Community events
  COMMENT_POSTED: 'sukko.community',
  COMMENT_PINNED: 'sukko.community',
  COMMENT_UPVOTED: 'sukko.community',
  FAVORITE_TOGGLED: 'sukko.community',
  
  // Creation events
  TOKEN_CREATED: 'sukko.creation',
  TOKEN_LISTED: 'sukko.creation',
  
  // Analytics events
  PRICE_DELTA_UPDATED: 'sukko.analytics',
  HOLDER_COUNT_UPDATED: 'sukko.analytics',
  ANALYTICS_RECALCULATED: 'sukko.analytics',
  TRENDING_UPDATED: 'sukko.analytics',
  
  // Balance events
  BALANCE_UPDATED: 'sukko.balances',
  TRANSFER_COMPLETED: 'sukko.balances',
} as const;

export type EventType = keyof typeof EVENT_TYPE_TOPICS;
export type TopicName = typeof EVENT_TYPE_TOPICS[EventType];
```

#### 2.3 Publisher Implementation

File: `publisher/redpanda-publisher.ts`

```typescript
import { Kafka, Producer, CompressionTypes, logLevel, Partitioners } from 'kafkajs';
import { EVENT_TYPE_TOPICS, EventType } from './types/event-types.js';

interface PublisherConfig {
  brokers: string[];
  clientId: string;
  compression: CompressionTypes;
}

interface PublisherStats {
  messagesPublished: number;
  messagesFailed: number;
  errors: number;
  startTime: number;
  byTopic: Record<string, number>;
}

export class RedpandaPublisher {
  private kafka: Kafka;
  private producer: Producer;
  private stats: PublisherStats = {
    messagesPublished: 0,
    messagesFailed: 0,
    errors: 0,
    startTime: Date.now(),
    byTopic: {},
  };

  constructor(config: PublisherConfig) {
    this.kafka = new Kafka({
      clientId: config.clientId,
      brokers: config.brokers,
      logLevel: logLevel.INFO,
      retry: {
        retries: 5,
        initialRetryTime: 300,
        maxRetryTime: 30000,
      },
    });

    this.producer = this.kafka.producer({
      createPartitioner: Partitioners.DefaultPartitioner,
      compression: config.compression,
      allowAutoTopicCreation: false,
      transactionTimeout: 30000,
      idempotent: true,          // Prevents duplicates
      maxInFlightRequests: 5,    // Pipelining
      acks: 1,                   // Wait for leader ack
    });
  }

  async connect(): Promise<void> {
    await this.producer.connect();
    console.log('✅ Connected to Redpanda');
  }

  /**
   * Publish single event
   */
  async publishEvent(
    tokenId: string,
    eventType: EventType,
    data: any
  ): Promise<void> {
    const topic = EVENT_TYPE_TOPICS[eventType];
    
    try {
      await this.producer.send({
        topic,
        compression: CompressionTypes.Snappy,
        messages: [{
          key: tokenId,              // Partition by token
          value: JSON.stringify({
            type: eventType,
            tokenId,
            timestamp: Date.now(),
            ...data,
          }),
          timestamp: Date.now().toString(),
        }],
      });
      
      this.stats.messagesPublished++;
      this.stats.byTopic[topic] = (this.stats.byTopic[topic] || 0) + 1;
      
    } catch (error) {
      this.stats.messagesFailed++;
      this.stats.errors++;
      console.error(`Failed to publish ${eventType} for ${tokenId}:`, error);
      throw error;
    }
  }

  /**
   * Publish batch of events (more efficient)
   */
  async publishBatch(events: Array<{
    tokenId: string;
    eventType: EventType;
    data: any;
  }>): Promise<void> {
    // Group by topic for efficient batching
    const byTopic = new Map<string, any[]>();
    
    for (const event of events) {
      const topic = EVENT_TYPE_TOPICS[event.eventType];
      if (!byTopic.has(topic)) {
        byTopic.set(topic, []);
      }
      byTopic.get(topic)!.push({
        key: event.tokenId,
        value: JSON.stringify({
          type: event.eventType,
          tokenId: event.tokenId,
          timestamp: Date.now(),
          ...event.data,
        }),
        timestamp: Date.now().toString(),
      });
    }

    // Send all batches in parallel
    const promises = Array.from(byTopic.entries()).map(([topic, messages]) =>
      this.producer.send({
        topic,
        compression: CompressionTypes.Snappy,
        messages,
      }).then(() => {
        this.stats.messagesPublished += messages.length;
        this.stats.byTopic[topic] = (this.stats.byTopic[topic] || 0) + messages.length;
      }).catch(error => {
        this.stats.messagesFailed += messages.length;
        this.stats.errors++;
        console.error(`Batch send failed for topic ${topic}:`, error);
        throw error;
      })
    );

    await Promise.all(promises);
  }

  getStats(): PublisherStats & { uptime: number; messagesPerSecond: number } {
    const uptime = Date.now() - this.stats.startTime;
    return {
      ...this.stats,
      uptime,
      messagesPerSecond: this.stats.messagesPublished / (uptime / 1000),
    };
  }

  async disconnect(): Promise<void> {
    await this.producer.disconnect();
    console.log('Disconnected from Redpanda');
  }
}
```

#### 2.4 Event Simulator

File: `publisher/event-simulator.ts`

```typescript
import { RedpandaPublisher } from './redpanda-publisher.js';
import { EventType } from './types/event-types.js';

export class EventSimulator {
  private publisher: RedpandaPublisher;
  private tokens = ['BTC', 'ETH', 'SOL', 'SUKKO', 'DOGE'];
  private intervalId: NodeJS.Timeout | null = null;
  private isRunning = false;

  constructor(publisher: RedpandaPublisher) {
    this.publisher = publisher;
  }

  /**
   * Start simulating events at specified rate
   */
  start(messagesPerSecond: number = 25): void {
    if (this.isRunning) {
      console.log('⚠️  Simulator already running');
      return;
    }

    const intervalMs = 1000 / messagesPerSecond;
    
    this.intervalId = setInterval(() => {
      const token = this.randomToken();
      const eventType = this.randomEventType();
      const data = this.generateEventData(token, eventType);
      
      this.publisher.publishEvent(token, eventType, data).catch(console.error);
    }, intervalMs);

    this.isRunning = true;
    console.log(`🚀 Simulator started: ${messagesPerSecond} msg/sec`);
  }

  /**
   * Stop simulator
   */
  stop(): void {
    if (!this.isRunning) {
      console.log('⚠️  Simulator not running');
      return;
    }

    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }

    this.isRunning = false;
    console.log('⏹️  Simulator stopped');
  }

  private randomToken(): string {
    return this.tokens[Math.floor(Math.random() * this.tokens.length)];
  }

  private randomEventType(): EventType {
    // Weight distribution: more trades, fewer metadata updates
    const weighted: EventType[] = [
      'TRADE_EXECUTED', 'TRADE_EXECUTED', 'TRADE_EXECUTED',  // 3x weight
      'ANALYTICS_RECALCULATED', 'ANALYTICS_RECALCULATED',     // 2x weight
      'LIQUIDITY_ADDED',
      'METADATA_UPDATED',
      'COMMENT_POSTED',
    ];
    return weighted[Math.floor(Math.random() * weighted.length)];
  }

  private generateEventData(token: string, eventType: EventType): any {
    switch (eventType) {
      case 'TRADE_EXECUTED':
      case 'BUY_COMPLETED':
      case 'SELL_COMPLETED':
        return {
          price: Math.random() * 100 + 10,
          volume: Math.random() * 1000,
          side: Math.random() > 0.5 ? 'buy' : 'sell',
          amount: Math.random() * 10,
        };

      case 'LIQUIDITY_ADDED':
      case 'LIQUIDITY_REMOVED':
        return {
          btcAmount: Math.random() * 10,
          tokenAmount: Math.random() * 1000,
        };

      case 'METADATA_UPDATED':
        return {
          name: `${token} Token`,
          description: `Updated at ${Date.now()}`,
        };

      case 'ANALYTICS_RECALCULATED':
      case 'PRICE_DELTA_UPDATED':
        return {
          priceChange24h: (Math.random() - 0.5) * 20,
          volume24h: Math.random() * 1000000,
          holders: Math.floor(Math.random() * 10000),
        };

      case 'COMMENT_POSTED':
        return {
          text: 'Great project!',
          author: 'user123',
        };

      default:
        return {};
    }
  }
}
```

#### 2.5 HTTP Control API

File: `publisher/api-server.ts`

```typescript
import express, { Request, Response } from 'express';
import cors from 'cors';
import { RedpandaPublisher } from './redpanda-publisher.js';
import { EventSimulator } from './event-simulator.js';

export function createAPIServer(
  publisher: RedpandaPublisher,
  simulator: EventSimulator
) {
  const app = express();
  app.use(cors());
  app.use(express.json());

  // Control endpoint
  app.post('/control', (req: Request, res: Response) => {
    const { action, messagesPerSecond } = req.body;

    switch (action) {
      case 'start':
        simulator.start(messagesPerSecond || 25);
        res.json({ success: true, message: 'Simulator started' });
        break;

      case 'stop':
        simulator.stop();
        res.json({ success: true, message: 'Simulator stopped' });
        break;

      default:
        res.status(400).json({ 
          success: false, 
          message: `Invalid action: ${action}` 
        });
    }
  });

  // Stats endpoint
  app.get('/stats', (req: Request, res: Response) => {
    res.json(publisher.getStats());
  });

  // Health check
  app.get('/health', (req: Request, res: Response) => {
    res.json({
      status: 'healthy',
      uptime: process.uptime(),
      publisher: 'connected',
    });
  });

  return app;
}
```

#### 2.6 Main Entry Point

File: `publisher/index.ts`

```typescript
import { CompressionTypes } from 'kafkajs';
import { RedpandaPublisher } from './redpanda-publisher.js';
import { EventSimulator } from './event-simulator.js';
import { createAPIServer } from './api-server.js';
import dotenv from 'dotenv';

dotenv.config();

async function main() {
  // Initialize publisher
  const publisher = new RedpandaPublisher({
    brokers: (process.env.KAFKA_BROKERS || 'localhost:19092').split(','),
    clientId: 'sukko-publisher',
    compression: CompressionTypes.Snappy,
  });

  await publisher.connect();

  // Initialize simulator
  const simulator = new EventSimulator(publisher);

  // Start HTTP API
  const app = createAPIServer(publisher, simulator);
  const port = parseInt(process.env.PORT || '3003');
  
  app.listen(port, () => {
    console.log(`📊 Publisher API running on port ${port}`);
    console.log(`   Stats:   http://localhost:${port}/stats`);
    console.log(`   Control: POST http://localhost:${port}/control`);
  });

  // Graceful shutdown
  process.on('SIGTERM', async () => {
    console.log('Shutting down...');
    simulator.stop();
    await publisher.disconnect();
    process.exit(0);
  });

  process.on('SIGINT', async () => {
    console.log('Shutting down...');
    simulator.stop();
    await publisher.disconnect();
    process.exit(0);
  });
}

main().catch(console.error);
```

#### 2.7 Testing Publisher

```bash
# Start Redpanda locally
cd deployments/local
docker-compose up -d redpanda

# Create topics
./scripts/setup-redpanda-topics.sh

# Run publisher
cd publisher
npm install
npm run dev

# Start simulator
curl -X POST http://localhost:3003/control \
  -H "Content-Type: application/json" \
  -d '{"action":"start","messagesPerSecond":25}'

# Check stats
curl http://localhost:3003/stats

# Stop simulator
curl -X POST http://localhost:3003/control \
  -H "Content-Type: application/json" \
  -d '{"action":"stop"}'
```

---

### Phase 3: WS Server Implementation (4-5 days)

#### 3.1 Dependencies

**Update `ws/go.mod`:**

```go
module sukko

go 1.25.1

require (
    github.com/twmb/franz-go v1.18.0
    github.com/twmb/franz-go/pkg/kadm v1.14.0
    github.com/gobwas/ws v1.4.0
    github.com/rs/zerolog v1.33.0
    github.com/caarlos0/env/v11 v11.3.1
    github.com/joho/godotenv v1.5.1
    github.com/prometheus/client_golang v1.20.5
    go.uber.org/automaxprocs v1.6.0
)
```

**Install:**
```bash
cd ws
go get github.com/twmb/franz-go@v1.18.0
go get github.com/twmb/franz-go/pkg/kadm@v1.14.0
go mod tidy
```

#### 3.2 Configuration

**Update `ws/config.go`:**

```go
package main

import (
    "fmt"
    "time"

    "github.com/caarlos0/env/v11"
    "github.com/joho/godotenv"
    "github.com/rs/zerolog"
)

type Config struct {
    // Server
    Addr string `env:"WS_ADDR" envDefault:":3002"`
    
    // Redpanda/Kafka
    KafkaBrokers string `env:"KAFKA_BROKERS" envDefault:"localhost:19092"`
    KafkaGroupID string `env:"KAFKA_GROUP_ID" envDefault:"ws-server"`
    
    // Topics (all 8)
    TopicTrades     string `env:"KAFKA_TOPIC_TRADES" envDefault:"sukko.trades"`
    TopicLiquidity  string `env:"KAFKA_TOPIC_LIQUIDITY" envDefault:"sukko.liquidity"`
    TopicMetadata   string `env:"KAFKA_TOPIC_METADATA" envDefault:"sukko.metadata"`
    TopicSocial     string `env:"KAFKA_TOPIC_SOCIAL" envDefault:"sukko.social"`
    TopicCommunity  string `env:"KAFKA_TOPIC_COMMUNITY" envDefault:"sukko.community"`
    TopicCreation   string `env:"KAFKA_TOPIC_CREATION" envDefault:"sukko.creation"`
    TopicAnalytics  string `env:"KAFKA_TOPIC_ANALYTICS" envDefault:"sukko.analytics"`
    TopicBalances   string `env:"KAFKA_TOPIC_BALANCES" envDefault:"sukko.balances"`
    
    // Resource limits
    CPULimit    float64 `env:"WS_CPU_LIMIT" envDefault:"1.0"`
    MemoryLimit int64   `env:"WS_MEMORY_LIMIT" envDefault:"15569256448"` // 14.5GB
    
    // Capacity
    MaxConnections int `env:"WS_MAX_CONNECTIONS" envDefault:"12000"`
    
    // Worker pool
    WorkerPoolSize  int `env:"WS_WORKER_POOL_SIZE" envDefault:"0"`
    WorkerQueueSize int `env:"WS_WORKER_QUEUE_SIZE" envDefault:"0"`
    
    // Rate limiting
    MaxKafkaRate     int `env:"WS_MAX_KAFKA_RATE" envDefault:"25"`
    MaxBroadcastRate int `env:"WS_MAX_BROADCAST_RATE" envDefault:"25"`
    MaxGoroutines    int `env:"WS_MAX_GOROUTINES" envDefault:"30000"`
    
    // Safety thresholds
    CPURejectThreshold float64 `env:"WS_CPU_REJECT_THRESHOLD" envDefault:"75.0"`
    CPUPauseThreshold  float64 `env:"WS_CPU_PAUSE_THRESHOLD" envDefault:"80.0"`
    
    // Monitoring
    MetricsInterval time.Duration `env:"METRICS_INTERVAL" envDefault:"15s"`
    
    // Logging
    LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
    LogFormat string `env:"LOG_FORMAT" envDefault:"json"`
    
    Environment string `env:"ENVIRONMENT" envDefault:"development"`
}

// AllTopics returns all 8 Kafka topics
func (c *Config) AllTopics() []string {
    return []string{
        c.TopicTrades,
        c.TopicLiquidity,
        c.TopicMetadata,
        c.TopicSocial,
        c.TopicCommunity,
        c.TopicCreation,
        c.TopicAnalytics,
        c.TopicBalances,
    }
}

// TopicToEventType maps Kafka topic to event type
func (c *Config) TopicToEventType(topic string) string {
    switch topic {
    case c.TopicTrades:
        return "trade"
    case c.TopicLiquidity:
        return "liquidity"
    case c.TopicMetadata:
        return "metadata"
    case c.TopicSocial:
        return "social"
    case c.TopicCommunity:
        return "community"
    case c.TopicCreation:
        return "creation"
    case c.TopicAnalytics:
        return "analytics"
    case c.TopicBalances:
        return "balances"
    default:
        return "unknown"
    }
}

// LoadConfig reads configuration from environment
func LoadConfig(logger *zerolog.Logger) (*Config, error) {
    if err := godotenv.Load(); err != nil {
        if logger != nil {
            logger.Info().Msg("No .env file found (using environment variables only)")
        } else {
            fmt.Println("Info: No .env file found")
        }
    }

    cfg := &Config{}
    if err := env.Parse(cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    // Auto-calculate worker pool sizes if not set
    if cfg.WorkerPoolSize == 0 {
        cfg.WorkerPoolSize = int(cfg.CPULimit * 192)
    }
    if cfg.WorkerQueueSize == 0 {
        cfg.WorkerQueueSize = cfg.WorkerPoolSize * 100
    }

    return cfg, nil
}
```

#### 3.3 Kafka Consumer Implementation

File: `ws/kafka_consumer.go`

```go
package main

import (
    "context"
    "fmt"
    "strings"
    "sync/atomic"
    "time"

    "github.com/rs/zerolog"
    "github.com/twmb/franz-go/pkg/kgo"
)

type KafkaConsumer struct {
    client       *kgo.Client
    config       *Config
    server       *Server
    logger       zerolog.Logger
    ctx          context.Context
    cancel       context.CancelFunc
    
    // Stats
    messagesConsumed int64
    messagesDropped  int64
    commitErrors     int64
}

func NewKafkaConsumer(config *Config, server *Server, logger zerolog.Logger) (*KafkaConsumer, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    opts := []kgo.Opt{
        // Broker configuration
        kgo.SeedBrokers(strings.Split(config.KafkaBrokers, ",")...),
        kgo.ConsumerGroup(config.KafkaGroupID),
        kgo.ConsumeTopics(config.AllTopics()...),
        
        // Performance tuning
        kgo.FetchMaxWait(100 * time.Millisecond),     // Low latency
        kgo.FetchMinBytes(1),                          // Don't wait for batches
        kgo.FetchMaxBytes(10 * 1024 * 1024),          // 10MB max
        kgo.FetchMaxRecords(1000),                     // Batch size
        kgo.FetchMaxPartitionBytes(5 * 1024 * 1024),  // 5MB per partition
        
        // Manual commit for rate limiting control
        kgo.DisableAutoCommit(),
        kgo.AutoCommitInterval(5 * time.Second),
        
        // Consumer group rebalancing
        kgo.SessionTimeout(10 * time.Second),
        kgo.RebalanceTimeout(30 * time.Second),
        kgo.HeartbeatInterval(3 * time.Second),
        
        // Read consistency
        kgo.RequireStableFetchOffsets(),
        
        // Logging
        kgo.WithLogger(kgo.BasicLogger(logger, kgo.LogLevelInfo, nil)),
    }
    
    client, err := kgo.NewClient(opts...)
    if err != nil {
        return nil, fmt.Errorf("failed to create Kafka client: %w", err)
    }
    
    // Test connection
    if err := client.Ping(ctx); err != nil {
        return nil, fmt.Errorf("failed to ping Kafka: %w", err)
    }
    
    logger.Info().
        Strs("brokers", strings.Split(config.KafkaBrokers, ",")).
        Msg("✅ Connected to Redpanda")
    
    return &KafkaConsumer{
        client: client,
        config: config,
        server: server,
        logger: logger,
        ctx:    ctx,
        cancel: cancel,
    }, nil
}

func (kc *KafkaConsumer) Start() error {
    go kc.consumeLoop()
    
    kc.logger.Info().
        Strs("topics", kc.config.AllTopics()).
        Str("group", kc.config.KafkaGroupID).
        Msg("Started Kafka consumer")
    
    return nil
}

func (kc *KafkaConsumer) consumeLoop() {
    for {
        select {
        case <-kc.ctx.Done():
            return
        default:
            // Poll for records
            fetches := kc.client.PollFetches(kc.ctx)
            
            // Handle errors
            if errs := fetches.Errors(); len(errs) > 0 {
                for _, err := range errs {
                    kc.logger.Error().
                        Err(err.Err).
                        Str("topic", err.Topic).
                        Int32("partition", err.Partition).
                        Msg("Kafka fetch error")
                    RecordKafkaError(ErrorSeverityWarning)
                }
                continue
            }
            
            // Process records
            var recordsToCommit []*kgo.Record
            
            fetches.EachPartition(func(p kgo.FetchTopicPartition) {
                for _, record := range p.Records {
                    if kc.processRecord(record) {
                        recordsToCommit = append(recordsToCommit, record)
                    }
                }
            })
            
            // Commit successfully processed records
            if len(recordsToCommit) > 0 {
                if err := kc.client.CommitRecords(kc.ctx, recordsToCommit...); err != nil {
                    atomic.AddInt64(&kc.commitErrors, 1)
                    kc.logger.Error().
                        Err(err).
                        Int("records", len(recordsToCommit)).
                        Msg("Failed to commit records")
                    RecordKafkaError(ErrorSeverityWarning)
                }
            }
        }
    }
}

func (kc *KafkaConsumer) processRecord(record *kgo.Record) bool {
    // STEP 1: Rate limiting (same as NATS implementation)
    allow, _ := kc.server.resourceGuard.AllowNATSMessage(kc.ctx)
    if !allow {
        atomic.AddInt64(&kc.messagesDropped, 1)
        // Don't commit - will be redelivered
        return false
    }
    
    // STEP 2: CPU emergency brake
    if kc.server.resourceGuard.ShouldPauseNATS() {
        atomic.AddInt64(&kc.messagesDropped, 1)
        return false
    }
    
    // STEP 3: Build channel name: {TOKEN}.{EVENT_TYPE}
    // Record key = token ID (e.g., "BTC")
    // Record topic maps to event type (e.g., "sukko.trades" → "trade")
    tokenID := string(record.Key)
    eventType := kc.config.TopicToEventType(record.Topic)
    channel := fmt.Sprintf("%s.%s", tokenID, eventType)
    
    // STEP 4: Broadcast to subscribed clients
    kc.server.broadcast(channel, record.Value)
    
    atomic.AddInt64(&kc.messagesConsumed, 1)
    
    // Log progress every 100 messages
    count := atomic.LoadInt64(&kc.messagesConsumed)
    if count%100 == 0 {
        kc.logger.Info().
            Int64("count", count).
            Str("topic", record.Topic).
            Str("key", tokenID).
            Msg("📬 Consumed messages from Redpanda")
    }
    
    return true // Successfully processed, commit this record
}

func (kc *KafkaConsumer) Stop() error {
    kc.logger.Info().Msg("Stopping Kafka consumer")
    kc.cancel()
    
    // Graceful shutdown
    kc.client.Close()
    
    return nil
}

func (kc *KafkaConsumer) GetStats() map[string]int64 {
    return map[string]int64{
        "messages_consumed": atomic.LoadInt64(&kc.messagesConsumed),
        "messages_dropped":  atomic.LoadInt64(&kc.messagesDropped),
        "commit_errors":     atomic.LoadInt64(&kc.commitErrors),
    }
}
```

#### 3.4 Subscription Bundle Support

File: `ws/subscription_bundles.go`

```go
package main

// SubscriptionBundle defines groups of related event types
type SubscriptionBundle string

const (
    BundleTrading    SubscriptionBundle = "TRADING"
    BundleFullMarket SubscriptionBundle = "FULL_MARKET"
    BundleCommunity  SubscriptionBundle = "COMMUNITY"
    BundlePortfolio  SubscriptionBundle = "PORTFOLIO"
    BundlePriceOnly  SubscriptionBundle = "PRICE_ONLY"
    BundleAll        SubscriptionBundle = "ALL"
)

// GetBundleEventTypes returns event types for a bundle
func GetBundleEventTypes(bundle SubscriptionBundle) []string {
    switch bundle {
    case BundleTrading:
        return []string{"trade", "liquidity", "analytics"}
    case BundleFullMarket:
        return []string{"trade", "liquidity", "analytics", "metadata"}
    case BundleCommunity:
        return []string{"community", "social", "metadata"}
    case BundlePortfolio:
        return []string{"trade", "analytics", "balances"}
    case BundlePriceOnly:
        return []string{"trade", "analytics"}
    case BundleAll:
        return []string{
            "trade", "liquidity", "metadata", "social",
            "community", "creation", "analytics", "balances",
        }
    default:
        return []string{}
    }
}

// SubscriptionRequest is sent by clients
type SubscriptionRequest struct {
    Type     string   `json:"type"`     // "subscribe"
    Bundle   string   `json:"bundle"`   // Optional: bundle name
    Tokens   []string `json:"tokens"`   // Optional: token list
    Channels []string `json:"channels"` // Optional: explicit channels
}

// ExpandSubscription converts bundle+tokens into explicit channels
func ExpandSubscription(req SubscriptionRequest) []string {
    var channels []string
    
    if req.Bundle != "" {
        // Expand bundle to event types
        eventTypes := GetBundleEventTypes(SubscriptionBundle(req.Bundle))
        
        // Cross product: tokens × event types
        for _, token := range req.Tokens {
            for _, eventType := range eventTypes {
                channel := token + "." + eventType
                channels = append(channels, channel)
            }
        }
    } else if len(req.Channels) > 0 {
        // Use explicit channels
        channels = req.Channels
    }
    
    return channels
}
```

#### 3.5 Update Server Struct

**Update `ws/server.go`:**

```go
type Server struct {
    config          ServerConfig
    logger          *log.Logger
    structLogger    zerolog.Logger
    listener        net.Listener
    
    // Replace NATS with Kafka
    kafkaConsumer   *KafkaConsumer
    
    // ... rest unchanged
    connections       *ConnectionPool
    subscriptionIndex *SubscriptionIndex
    workerPool        *WorkerPool
    resourceGuard     *ResourceGuard
    // ... etc
}

func NewServer(config ServerConfig, logger *log.Logger, structLogger zerolog.Logger) (*Server, error) {
    s := &Server{
        config:       config,
        logger:       logger,
        structLogger: structLogger,
        // ... initialize other fields
    }
    
    // Initialize Kafka consumer
    if config.KafkaBrokers != "" {
        kafkaConsumer, err := NewKafkaConsumer(&config, s, structLogger)
        if err != nil {
            return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
        }
        s.kafkaConsumer = kafkaConsumer
    }
    
    return s, nil
}

func (s *Server) Start(ctx context.Context) error {
    // ... start HTTP server, listener, etc.
    
    // Start Kafka consumer
    if s.kafkaConsumer != nil {
        if err := s.kafkaConsumer.Start(); err != nil {
            return fmt.Errorf("failed to start Kafka consumer: %w", err)
        }
    }
    
    // ... rest of start logic
    return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
    s.logger.Println("Shutting down server...")
    
    // Stop Kafka consumer first
    if s.kafkaConsumer != nil {
        if err := s.kafkaConsumer.Stop(); err != nil {
            s.logger.Printf("Error stopping Kafka consumer: %v", err)
        }
    }
    
    // ... rest of shutdown
    return nil
}
```

#### 3.6 Update Metrics

**Update `ws/metrics.go`:**

```go
// Replace all "nats_" metrics with "kafka_"

var (
    // Kafka metrics (renamed from NATS)
    kafkaMessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
        Name: "kafka_messages_received_total",
        Help: "Total messages received from Kafka",
    })
    
    kafkaMessagesDropped = promauto.NewCounter(prometheus.CounterOpts{
        Name: "kafka_messages_dropped_total",
        Help: "Total Kafka messages dropped due to rate limiting",
    })
    
    kafkaCommitErrors = promauto.NewCounter(prometheus.CounterOpts{
        Name: "kafka_commit_errors_total",
        Help: "Total Kafka commit errors",
    })
    
    // ... rest of metrics unchanged
)

// Helper functions
func RecordKafkaMessage() {
    kafkaMessagesReceived.Inc()
}

func RecordKafkaDropped() {
    kafkaMessagesDropped.Inc()
}

func RecordKafkaError(severity ErrorSeverity) {
    kafkaCommitErrors.Inc()
}
```

---

### Phase 4: Deployment & Testing (3-4 days)

#### 4.1 Environment Configuration

**Backend `.env.production`:**
```bash
# Redpanda runs in same docker-compose
KAFKA_BROKERS=redpanda:9092

# Publisher
NODE_ENV=production
PORT=3003
```

**WS Server `.env.production`:**
```bash
# Connect to backend instance internal IP
KAFKA_BROKERS=10.128.0.9:9092  # Backend internal IP
KAFKA_GROUP_ID=ws-server

# Topics (use defaults from config.go)
# KAFKA_TOPIC_TRADES=sukko.trades
# KAFKA_TOPIC_LIQUIDITY=sukko.liquidity
# ... etc

# Capacity
WS_MAX_CONNECTIONS=12000
WS_WORKER_POOL_SIZE=192
WS_WORKER_QUEUE_SIZE=19200

# Resources
WS_CPU_LIMIT=1
WS_MEMORY_LIMIT=15569256448  # 14.5 GB
WS_MAX_GOROUTINES=30000

# Rate limiting
WS_MAX_KAFKA_RATE=25
WS_MAX_BROADCAST_RATE=25

# Safety
WS_CPU_REJECT_THRESHOLD=75.0
WS_CPU_PAUSE_THRESHOLD=80.0

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
ENVIRONMENT=production
```

#### 4.2 Update Taskfile

**Add Redpanda tasks to `taskfiles/isolated-setup.yml`:**

```yaml
tasks:
  # ... existing tasks

  gcp2:redpanda:console:
    desc: Open Redpanda Console in browser
    cmds:
      - open "http://{{.BACKEND_EXTERNAL_IP}}:8080"

  gcp2:redpanda:topics:
    desc: List Redpanda topics
    cmds:
      - gcloud compute ssh {{.BACKEND_INSTANCE}} --zone={{.GCP_ZONE}} --command="docker exec redpanda rpk topic list"

  gcp2:redpanda:stats:
    desc: Show Redpanda cluster stats
    cmds:
      - gcloud compute ssh {{.BACKEND_INSTANCE}} --zone={{.GCP_ZONE}} --command="docker exec redpanda rpk cluster info"
```

#### 4.3 Deployment Steps

**Deploy everything:**

```bash
# 1. Update code on all VMs
task gcp2:deploy:all

# 2. Verify Redpanda is running
task gcp2:redpanda:stats
task gcp2:redpanda:topics

# 3. Start publisher
task gcp2:publisher:start

# 4. Verify messages flowing
task gcp2:redpanda:console  # Open console in browser

# 5. Run capacity test
task gcp2:test:remote:capacity
```

#### 4.4 Testing Plan

**Test 1: Local Smoke Test**
```bash
# Terminal 1: Redpanda
cd deployments/local
docker-compose up redpanda console

# Terminal 2: Topics
./scripts/setup-redpanda-topics.sh

# Terminal 3: Publisher
cd publisher
npm run dev
curl -X POST http://localhost:3003/control -d '{"action":"start","messagesPerSecond":10}'

# Terminal 4: WS Server
cd ws
go run .

# Terminal 5: Test client
wscat -c ws://localhost:3002
> {"type":"subscribe","bundle":"TRADING","tokens":["BTC","ETH"]}

# Verify: See messages for BTC.trade, BTC.liquidity, BTC.analytics, etc.
```

**Test 2: Load Test (Local)**
```bash
cd loadtest
go run . \
  --target 1000 \
  --duration 300 \
  --ramp-rate 10 \
  --server ws://localhost:3002 \
  --subscription-mode bundle \
  --bundle TRADING \
  --tokens BTC,ETH,SOL
```

**Test 3: GCP Capacity Test**
```bash
# Deploy
task gcp2:create:all
task gcp2:deploy:all

# Setup topics (one-time)
task gcp2:ssh:backend
docker exec redpanda rpk topic list

# Start publisher
task gcp2:publisher:start

# Run full capacity test
task gcp2:test:remote:capacity

# Monitor
task gcp2:redpanda:console
# Open Grafana: http://backend-ip:3001
```

**Test 4: Subscription Bundle Validation**
```bash
# Test each bundle type
bundles=("TRADING" "FULL_MARKET" "COMMUNITY" "PORTFOLIO" "PRICE_ONLY" "ALL")

for bundle in "${bundles[@]}"; do
  echo "Testing bundle: $bundle"
  wscat -c ws://ws-server-ip:3002 -x '{"type":"subscribe","bundle":"'$bundle'","tokens":["BTC"]}'
  sleep 5
done
```

#### 4.5 Monitoring Setup

**Verify in Redpanda Console** (http://backend-ip:8080):
- ✅ All 8 topics visible
- ✅ Messages flowing to all topics
- ✅ Consumer group "ws-server" active
- ✅ No lag in consumer group

**Verify in Grafana** (http://backend-ip:3001):
- ✅ kafka_messages_received_total increasing
- ✅ ws_active_connections at target
- ✅ ws_broadcasts_total increasing
- ✅ No errors

**Check WS Server Metrics:**
```bash
curl http://ws-server-ip:3003/metrics | grep kafka
```

---

## Success Criteria

### Functional Requirements
- [ ] All 8 topics created and healthy
- [ ] Publisher sending to all 8 topics
- [ ] WS server consuming from all 8 topics
- [ ] Subscription bundles working correctly
- [ ] Clients receiving messages only for subscribed channels

### Performance Requirements
- [ ] 12,000 connections sustained for 1 hour
- [ ] 29,000+ msg/sec throughput
- [ ] <5ms added latency vs NATS baseline
- [ ] 0% message loss
- [ ] <30% CPU usage on WS server
- [ ] <50% memory usage on WS server

### Operational Requirements
- [ ] Redpanda Console accessible
- [ ] All metrics visible in Grafana
- [ ] Consumer group lag < 100 messages
- [ ] No error spikes in logs
- [ ] Graceful shutdown/restart working

---

## Rollback Plan

**If critical issues arise:**

1. **Keep old NATS branch:**
   ```bash
   git tag before-redpanda-migration
   git checkout -b nats-backup
   ```

2. **Quick rollback:**
   ```bash
   # Revert docker-compose to NATS
   git checkout before-redpanda-migration deployments/
   
   # Rebuild and deploy
   task gcp2:deploy:all
   
   # Total time: ~30 minutes
   ```

3. **Emergency fallback:**
   - Keep NATS container config in separate file
   - Can swap docker-compose.yml and restart
   - No code changes needed (keep both implementations)

---

## Timeline Summary

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| **Phase 1: Infrastructure** | 2-3 days | Redpanda deployed, topics created, health checks passing |
| **Phase 2: Publisher** | 3-4 days | kafkajs integrated, all 8 event types publishing, HTTP API working |
| **Phase 3: WS Server** | 4-5 days | franz-go integrated, subscription bundles implemented, metrics updated |
| **Phase 4: Testing** | 3-4 days | Load tests passing, monitoring setup, documentation complete |
| **Total** | **12-16 days** | **Production-ready Redpanda migration** |

---

## Post-Migration Tasks

1. **Documentation Updates**
   - Update architecture diagrams
   - Document subscription bundle usage
   - Update deployment guides

2. **Monitoring Enhancements**
   - Add Redpanda-specific dashboards to Grafana
   - Set up alerts for consumer lag
   - Monitor topic retention/cleanup

3. **Performance Tuning**
   - Adjust partition counts based on load
   - Tune batch sizes
   - Optimize retention policies

4. **Production Hardening**
   - Enable authentication (SASL)
   - Add TLS encryption
   - Set up replication (3 nodes)
   - Configure backups

---

## Open Questions

1. **Authentication:** Do we need SASL/TLS for Redpanda?
2. **Replication:** Start with 1 replica (dev) or 3 (production)?
3. **Retention tuning:** Are the default retention times appropriate?
4. **Monitoring:** Any specific Redpanda metrics to track?
5. **Scaling:** When do we add more Redpanda nodes?

---

## References

- **Redpanda Docs:** https://docs.redpanda.com
- **franz-go:** https://github.com/twmb/franz-go
- **kafkajs:** https://kafka.js.org
- **TOKEN_UPDATE_EVENTS.md:** `/docs/events/TOKEN_UPDATE_EVENTS.md`
- **Current Architecture:** `/docs/architecture/ARCHITECTURE_NATS_FLOW.md`

---

**Status:** Ready for implementation  
**Next Step:** Begin Phase 1 - Infrastructure Setup  
**Approval Required:** Technical lead sign-off
