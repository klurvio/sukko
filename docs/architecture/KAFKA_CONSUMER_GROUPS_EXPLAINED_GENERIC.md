# Kafka Consumer Groups Explained

## The Golden Rule

**One partition can only be consumed by one consumer within the same consumer group at any given time.**

This is the fundamental rule that governs Kafka consumer behavior.

---

## Key Concept: Partitions Are Assigned, Not Topics

A common misconception is that topics are assigned to consumers. In reality:

- **Topics** are logical groupings
- **Partitions** are the actual unit of assignment and parallelism

```
Consumer Group: chat-ws-consumer
│
├── Server 1 (10.1.0.30)
│   ├── messages         [Partition 0]
│   ├── presence         [Partition 0]
│   ├── typing-events    [Partition 0]
│   └── reactions        [Partition 0]
│
└── Server 2 (10.1.0.31)
    ├── notifications    [Partition 0]
    ├── read-receipts    [Partition 0]
    ├── user-events      [Partition 0]
    └── channel-events   [Partition 0]
```

Each topic above has 1 partition, so 8 partitions total are distributed across 2 servers.

---

## Scaling Consumption Within a Topic

If a topic has multiple partitions, multiple consumers in the same group can consume in parallel:

### 1 Partition = 1 Consumer Max

```
Topic: chat.messages
└── Partition 0 → Server 1 only (Server 2 is idle for this topic)
```

### 2 Partitions = 2 Consumers Can Work in Parallel

```
Topic: chat.messages
├── Partition 0 → Server 1
└── Partition 1 → Server 2
```

Messages are distributed across partitions (usually by key, e.g., room_id):
- Messages for rooms A-M → Partition 0 → Server 1
- Messages for rooms N-Z → Partition 1 → Server 2

Both servers consume, but **different messages** - they don't consume the same message twice.

---

## Multiple Consumer Groups

To have the same partition consumed by multiple consumers, they must be in **different consumer groups**.

```
Partition 0 (chat.messages)
│
├── Consumer Group "ws-consumer"     → Server 1 reads message
├── Consumer Group "search-indexer"  → Search service reads same message
└── Consumer Group "audit-logger"    → Audit service reads same message
```

Each group:
- Maintains its own offset
- Reads all messages independently
- Doesn't affect other groups

---

## Can Multiple Consumers Read the Same Message Simultaneously?

**Yes** - if they're in different consumer groups.

Kafka is a **log**, not a queue:

```
Partition 0 (immutable log)
┌─────┬─────┬─────┬─────┬─────┐
│ m0  │ m1  │ m2  │ m3  │ m4  │
└─────┴─────┴─────┴─────┴─────┘
        ▲
        │
   ┌────┼────┐
   │    │    │
 Group Group Group
   A    B    C
   │    │    │
   ▼    ▼    ▼
  Srv1 Srv2 Srv3

(All reading m1 simultaneously)
```

Key differences from traditional queues:
- Messages are **not removed** when read
- No locking at the message level
- Each group tracks its own offset independently
- Messages stay until retention period expires

---

## Avoiding Duplicate Broadcasts

If multiple consumer groups all broadcast to the same channel, clients receive duplicates:

```
Partition 0 (message M1)
│
├── Consumer Group A → WebSocket "room.general" → Client receives M1
└── Consumer Group B → WebSocket "room.general" → Client receives M1 again (duplicate!)
```

**Solution**: Different groups should have different purposes:

| Consumer Group | Purpose |
|----------------|---------|
| `chat-ws-consumer` | Broadcast to clients via WebSocket |
| `search-indexer` | Index messages for search |
| `audit-consumer` | Write to compliance/audit log |

Only one group broadcasts to clients. No duplicates.

---

## Example Setup (Chat Application)

```bash
# Check consumer group (using Redpanda's rpk CLI)
rpk group describe chat-ws-consumer
```

Output:
```
GROUP        chat-ws-consumer
STATE        Stable
MEMBERS      2
BALANCER     cooperative-sticky

TOPIC                PARTITION  MEMBER-ID    HOST
chat.messages        0          kgo-aa53...  10.1.0.30
chat.notifications   0          kgo-cf62...  10.1.0.31
chat.presence        0          kgo-aa53...  10.1.0.30
chat.typing-events   0          kgo-cf62...  10.1.0.31
...
```

- 2 servers in the consumer group
- Each topic has 1 partition
- Partitions are distributed across servers
- Messages only go to one server (10.1.0.30)
- Internal message bus broadcasts to all servers, so all connected clients receive messages

---

## Summary

| Scenario | Result |
|----------|--------|
| 1 partition, 2 consumers (same group) | Only 1 consumer receives messages |
| 2 partitions, 2 consumers (same group) | Each consumer gets ~50% of messages |
| 1 partition, 2 consumers (different groups) | Both consumers receive ALL messages |
| Multiple groups broadcasting to same channel | Clients receive duplicates |

---

## Common Use Cases

### Chat/Messaging Platform
- `chat-ws-consumer` - Real-time delivery to WebSocket clients
- `push-notification-consumer` - Send push notifications for offline users
- `search-indexer` - Index messages for full-text search
- `analytics-consumer` - Track message metrics

### E-commerce Platform
- `order-ws-consumer` - Real-time order status to customers
- `inventory-consumer` - Update stock levels
- `notification-consumer` - Send email/SMS notifications
- `reporting-consumer` - Generate business reports

### IoT Platform
- `device-ws-consumer` - Real-time data to dashboards
- `alert-consumer` - Trigger alerts on thresholds
- `timeseries-consumer` - Store in time-series database
- `ml-consumer` - Feed data to ML models

---

## Key Takeaways

1. **Partitions, not topics**, are the unit of parallelism
2. **Consumer groups** provide isolation - same data, different processing
3. **Kafka is a log** - messages aren't deleted when read
4. **One consumer per partition** within a group - scale by adding partitions
5. **Coordinate your groups** - avoid duplicate processing to the same destination
