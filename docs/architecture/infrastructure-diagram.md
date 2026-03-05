# WebSocket POC - Infrastructure Architecture

## High-Level System Diagram

```mermaid
graph TB
    subgraph "Client Layer"
        C1[WebSocket Clients<br/>User Connections]
    end

    subgraph "GCP Infrastructure"
        subgraph "Load Balancing"
            LB[GCP Load Balancer<br/>Static IP<br/>Port 8080]
        end

        subgraph "WS-Server Cluster"
            WS1[WS Instance 1<br/>sukko-go-1<br/>e2-medium]
            WS2[WS Instance 2<br/>sukko-go-2<br/>e2-medium]
            WSN[WS Instance N<br/>sukko-go-N<br/>e2-medium]

            subgraph "Inside Each WS Instance"
                S1[Shard 1<br/>Hash: 0-1999]
                S2[Shard 2<br/>Hash: 2000-3999]
                SN[Shard N<br/>Hash: 8000-9999]
                KCP[KafkaConsumerPool<br/>Consumes from Kafka]
                BB[BroadcastBus<br/>Redis Pub/Sub Client]
            end
        end

        subgraph "Message Bus Infrastructure"
            REDIS[Redis Single Node<br/>ws-redis<br/>e2-standard-2<br/>Port 6379<br/><br/>Pub/Sub Channel:<br/>'ws.broadcast']
            KAFKA[Redpanda/Kafka<br/>Topic: 'events'<br/>Partitions: 3]
        end

        subgraph "Backend Services - sukko-backend (e2-standard-4)"
            PUB[Publisher Service<br/>Port 3003<br/>Sends events to Kafka]

            subgraph "Monitoring Stack"
                PROM[Prometheus<br/>Port 9090<br/>Scrapes metrics]
                GRAF[Grafana<br/>Port 3005<br/>Dashboards]
                REXP[Redis Exporter<br/>Port 9121<br/>On Redis VM]
            end
        end

        subgraph "Load Testing"
            LT[Load Test Runner<br/>sukko-loadtest<br/>e2-standard-2]
        end
    end

    subgraph "External Services"
        EXT[External Backend<br/>Your existing system]
    end

    %% Client connections
    C1 -->|WebSocket<br/>wss://| LB
    LB -->|Round Robin| WS1
    LB -->|Round Robin| WS2
    LB -->|Round Robin| WSN

    %% Inside WS Instance (showing WS1 as example)
    WS1 -.->|Contains| S1
    WS1 -.->|Contains| S2
    WS1 -.->|Contains| SN
    WS1 -.->|Contains| KCP
    WS1 -.->|Contains| BB

    %% Message flow from Kafka to Redis
    KAFKA -->|Pull messages<br/>Partition 0,1,2| KCP
    KCP -->|Publish message| BB
    BB -->|PUBLISH 'ws.broadcast'| REDIS

    %% Redis broadcasts to ALL instances
    REDIS ==>|SUBSCRIBE 'ws.broadcast'<br/>Broadcast to ALL| WS1
    REDIS ==>|SUBSCRIBE 'ws.broadcast'<br/>Broadcast to ALL| WS2
    REDIS ==>|SUBSCRIBE 'ws.broadcast'<br/>Broadcast to ALL| WSN

    %% Backend to Kafka
    EXT -->|HTTP/API<br/>Send events| PUB
    PUB -->|Produce| KAFKA

    %% Monitoring connections
    PROM -.->|Scrape :3004| WS1
    PROM -.->|Scrape :3004| WS2
    PROM -.->|Scrape :3004| WSN
    PROM -.->|Scrape :3003| PUB
    PROM -.->|Scrape :9644| KAFKA
    PROM -.->|Scrape :9121| REXP
    REXP -.->|Monitors| REDIS
    GRAF -.->|Query| PROM

    %% Load testing
    LT -.->|Generate load| LB

    %% Styling
    classDef client fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    classDef ws fill:#fff3e0,stroke:#e65100,stroke-width:2px
    classDef infra fill:#f3e5f5,stroke:#4a148c,stroke-width:3px
    classDef monitor fill:#e8f5e9,stroke:#1b5e20,stroke-width:2px
    classDef external fill:#fce4ec,stroke:#880e4f,stroke-width:2px

    class C1 client
    class WS1,WS2,WSN,S1,S2,SN,KCP,BB ws
    class REDIS,KAFKA,LB infra
    class PROM,GRAF,REXP monitor
    class EXT,PUB external
```

## Message Flow Diagram

```mermaid
sequenceDiagram
    participant EXT as External Backend
    participant PUB as Publisher Service
    participant KAFKA as Kafka/Redpanda
    participant KCP1 as KafkaPool (Instance 1)
    participant BB1 as BroadcastBus (Instance 1)
    participant REDIS as Redis Pub/Sub
    participant BB2 as BroadcastBus (Instance 2)
    participant BBN as BroadcastBus (Instance N)
    participant S1 as Shards (Instance 1)
    participant S2 as Shards (Instance 2)
    participant SN as Shards (Instance N)
    participant C1 as Clients (Instance 1)
    participant C2 as Clients (Instance 2)

    Note over EXT,C2: Event Publishing Flow

    EXT->>PUB: POST /publish<br/>{user_id: 12345, type: "notification"}
    PUB->>KAFKA: Produce to 'events' topic<br/>Partition: hash(user_id) % 3

    Note over KAFKA,BBN: Each Instance Pulls from Kafka

    KAFKA->>KCP1: Pull message (Partition 0,1,2)
    KCP1->>BB1: Publish(message)

    Note over BB1,REDIS: Redis Broadcasts to ALL

    BB1->>REDIS: PUBLISH 'ws.broadcast'<br/>message payload

    Note over REDIS,BBN: ALL instances receive (Broadcast-ALL)

    REDIS-->>BB1: Message received
    REDIS-->>BB2: Message received
    REDIS-->>BBN: Message received

    Note over BB1,SN: Local Filtering (O(1) hash lookup)

    BB1->>S1: Filter: Does any shard<br/>have user_id 12345?
    BB2->>S2: Filter: Does any shard<br/>have user_id 12345?
    BBN->>SN: Filter: Does any shard<br/>have user_id 12345?

    Note over S2,C2: Only Instance 2, Shard X has user_id 12345

    S2->>C2: Send message via WebSocket

    Note over S1,SN: Other instances discard<br/>(no matching connections)
```

## Data Flow Architecture

```mermaid
flowchart LR
    subgraph "Event Source"
        A[Backend System]
    end

    subgraph "Message Queue"
        B[Kafka Topic: 'events'<br/>Partitions: 0, 1, 2]
    end

    subgraph "WS Instance 1"
        C1[KafkaConsumerPool]
        D1[BroadcastBus]
        E1[Shard 1: Hash 0-1999]
        E2[Shard 2: Hash 2000-3999]
        E3[Shard 3: Hash 4000-5999]
    end

    subgraph "WS Instance 2"
        C2[KafkaConsumerPool]
        D2[BroadcastBus]
        F1[Shard 1: Hash 0-1999]
        F2[Shard 2: Hash 2000-3999]
        F3[Shard 3: Hash 4000-5999]
    end

    subgraph "Broadcast Infrastructure"
        G[Redis Pub/Sub<br/>Channel: 'ws.broadcast'<br/><br/>⚡ Broadcasts to ALL]
    end

    subgraph "Connected Clients"
        H1[Clients on Instance 1]
        H2[Clients on Instance 2]
    end

    A -->|HTTP POST| B
    B -->|Pull all partitions| C1
    B -->|Pull all partitions| C2

    C1 -->|Publish| D1
    C2 -->|Publish| D2

    D1 -->|PUBLISH| G
    D2 -->|PUBLISH| G

    G ==>|SUBSCRIBE<br/>Broadcast| D1
    G ==>|SUBSCRIBE<br/>Broadcast| D2

    D1 -.->|Filter locally| E1
    D1 -.->|Filter locally| E2
    D1 -.->|Filter locally| E3

    D2 -.->|Filter locally| F1
    D2 -.->|Filter locally| F2
    D2 -.->|Filter locally| F3

    E1 & E2 & E3 -->|Send to<br/>matching clients| H1
    F1 & F2 & F3 -->|Send to<br/>matching clients| H2

    style G fill:#f3e5f5,stroke:#4a148c,stroke-width:4px
    style A fill:#fce4ec,stroke:#880e4f,stroke-width:2px
    style B fill:#fff3e0,stroke:#e65100,stroke-width:2px
```

## GCP VM Layout

```mermaid
graph TB
    subgraph "GCP Project: sukko-server"
        subgraph "us-central1-a Zone"

            subgraph "sukko-backend (e2-standard-4)"
                BE1[Docker Compose:<br/>- Redpanda :9092, :9644<br/>- Publisher :3003<br/>- Prometheus :9090<br/>- Grafana :3005]
            end

            subgraph "ws-redis (e2-standard-2)"
                RED1[Docker Compose:<br/>- Redis :6379<br/>- Redis Exporter :9121]
            end

            subgraph "sukko-go-1 (e2-medium)"
                WS1[Go Binary:<br/>- WS Server :8080<br/>- Metrics :3004<br/>- 5 Shards<br/>- Connects to Redis]
            end

            subgraph "sukko-go-2 (e2-medium)"
                WS2[Go Binary:<br/>- WS Server :8080<br/>- Metrics :3004<br/>- 5 Shards<br/>- Connects to Redis]
            end

            subgraph "sukko-loadtest (e2-standard-2)"
                LT1[Load Test Runner]
            end

        end

        subgraph "Networking"
            FW1[Firewall Rules:<br/>- backend: 3003, 9090, 3005<br/>- redis: 6379 from ws-*, 9121 from backend<br/>- ws-server: 8080 public]
            IP1[Static IP:<br/>Assigned to ws-go-1<br/>Public endpoint]
        end

    end

    WS1 -->|Internal IP<br/>10.128.0.X:6379| RED1
    WS2 -->|Internal IP<br/>10.128.0.X:6379| RED1
    WS1 -->|Internal IP<br/>10.128.0.X:9092| BE1
    WS2 -->|Internal IP<br/>10.128.0.X:9092| BE1
    BE1 -->|Scrape<br/>10.128.0.X:3004| WS1
    BE1 -->|Scrape<br/>10.128.0.X:3004| WS2
    BE1 -->|Scrape<br/>10.128.0.X:9121| RED1
    LT1 -.->|Load test| IP1

    style RED1 fill:#f3e5f5,stroke:#4a148c,stroke-width:3px
    style BE1 fill:#e8f5e9,stroke:#1b5e20,stroke-width:2px
    style WS1 fill:#fff3e0,stroke:#e65100,stroke-width:2px
    style WS2 fill:#fff3e0,stroke:#e65100,stroke-width:2px
```

---

## Key Architectural Points

### 1. **Broadcast-ALL Pattern**
- Redis does **NOT** do targeted dispatch or lookups
- Redis broadcasts to **ALL** WS instances via Pub/Sub
- Each instance filters locally using O(1) hash lookup
- Trade-off: Higher network bandwidth for simplicity and statelessness

### 2. **Horizontal Scaling**
- Multiple WS instances can be added without coordination
- Each instance has identical shard configuration (Hash 0-9999)
- Load balancer distributes incoming connections
- All instances share the same Redis and Kafka

### 3. **Shard-Based Connection Management**
- Each WS instance has N shards (default: 5)
- Shard assignment: `hash(user_id) % TOTAL_SHARDS`
- Each shard maintains a map of `user_id → WebSocket connections`
- Local filtering is O(1) hash lookup

### 4. **Message Delivery Guarantee**
- Kafka ensures at-least-once delivery
- Redis Pub/Sub provides best-effort broadcast
- Each instance independently processes Kafka messages
- No message loss during scaling (all instances receive all messages)

### 5. **Zero-Code-Change Deployment**
- Single node: `REDIS_SENTINEL_ADDRS=10.128.0.10:6379` (1 address)
- Sentinel cluster: `REDIS_SENTINEL_ADDRS=node1:26379,node2:26379,node3:26379` (3+ addresses)
- Code auto-detects mode and connects appropriately

---

## Monitoring & Observability

### Prometheus Metrics
- **WS Servers**: Active connections, messages sent, latency
- **Redis**: Connected clients, commands/sec, latency percentiles, memory usage
- **Kafka**: Lag, throughput, partition distribution
- **Publisher**: Event publishing rate, errors

### Grafana Dashboards
1. **WebSocket Overview**: Connections, message rates, shard distribution
2. **Redis BroadcastBus**: Health, performance, pub/sub channels
3. **Kafka Pipeline**: Consumer lag, partition balance, throughput

---

## Cost Analysis (Monthly)

| Component | Instance Type | Count | Cost/Month |
|-----------|--------------|-------|------------|
| Backend (Monitoring + Kafka) | e2-standard-4 | 1 | $105 |
| Redis (BroadcastBus) | e2-standard-2 | 1 | $53 |
| WS Server | e2-medium | 2-10 | $26-130 |
| Load Test | e2-standard-2 | 1 | $53 (dev only) |
| **Total (Production)** | | | **$184-288** |

*Costs assume us-central1 region, 730 hours/month*

---

## Deployment Commands

```bash
# Full deployment (creates all VMs, configures networking, deploys services)
task gcp:deploy

# Individual components
task gcp:redis:create-vm
task gcp:redis:deploy-single
task gcp:redis:health

# Monitoring
task gcp:status
task gcp:health:all
task gcp:stats:urls
```

---

## Next Steps

1. **Local Testing** (5-10 min): `docker-compose -f docker-compose.redis-local.yml up -d`
2. **GCP Deployment** (15-30 min): `task gcp:deploy`
3. **Multi-Instance Testing** (Week 2): Deploy 2+ WS instances, validate cross-instance messaging
4. **Production Rollout** (Week 3-4): Monitor for 48 hours, validate <15ms latency

---

Generated: 2025-11-21
Branch: `feature/redis-broadcast-bus`
Status: Ready for deployment
