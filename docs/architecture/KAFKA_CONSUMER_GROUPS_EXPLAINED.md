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
Consumer Group: odin-ws-consumer
│
├── Pod 1 (10.1.0.30)
│   ├── trade.refined      [Partition 0]
│   ├── balances.refined   [Partition 0]
│   ├── community.refined  [Partition 0]
│   └── liquidity.refined  [Partition 0]
│
└── Pod 2 (10.1.0.31)
    ├── analytics.refined  [Partition 0]
    ├── creation.refined   [Partition 0]
    ├── metadata.refined   [Partition 0]
    └── social.refined     [Partition 0]
```

Each topic above has 1 partition, so 8 partitions total are distributed across 2 pods.

---

## Scaling Consumption Within a Topic

If a topic has multiple partitions, multiple consumers in the same group can consume in parallel:

### 1 Partition = 1 Consumer Max

```
Topic: odin.main.trade.refined
└── Partition 0 → Pod 1 only (Pod 2 is idle for this topic)
```

### 2 Partitions = 2 Consumers Can Work in Parallel

```
Topic: odin.main.trade.refined
├── Partition 0 → Pod 1
└── Partition 1 → Pod 2
```

Messages are distributed across partitions (usually by key):
- ~50% of trade messages → Partition 0 → Pod 1
- ~50% of trade messages → Partition 1 → Pod 2

Both pods consume, but **different messages** - they don't consume the same message twice.

---

## Multiple Consumer Groups

To have the same partition consumed by multiple consumers, they must be in **different consumer groups**.

```
Partition 0 (trade.refined)
│
├── Consumer Group "ws-consumer"    → Pod 1 reads message
├── Consumer Group "analytics"      → Analytics service reads same message
└── Consumer Group "audit-logger"   → Audit service reads same message
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
  Pod1 Pod2 Pod3

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
├── Consumer Group A → NATS "all.trade" → Client receives M1
└── Consumer Group B → NATS "all.trade" → Client receives M1 again (duplicate!)
```

**Solution**: Different groups should have different purposes:

| Consumer Group | Purpose |
|----------------|---------|
| `odin-ws-consumer` | Broadcast to clients via NATS |
| `analytics-consumer` | Write to analytics DB |
| `audit-consumer` | Write to audit log |

Only one group broadcasts to clients. No duplicates.

---

## Current Setup (odin-ws)

```bash
# Check consumer group
kubectl exec -n odin-std-develop odin-redpanda-0 -- rpk group describe odin-ws-consumer
```

Output:
```
GROUP        odin-ws-consumer
STATE        Stable
MEMBERS      2
BALANCER     cooperative-sticky

TOPIC                        PARTITION  MEMBER-ID    HOST
odin.main.trade.refined      0          kgo-aa53...  10.1.0.30
odin.main.analytics.refined  0          kgo-cf62...  10.1.0.31
...
```

- 2 pods in the consumer group
- Each topic has 1 partition
- Partitions are distributed across pods
- Trade messages only go to one pod (10.1.0.30)
- NATS broadcasts to all pods, so all clients receive messages

---

## Summary

| Scenario | Result |
|----------|--------|
| 1 partition, 2 consumers (same group) | Only 1 consumer receives messages |
| 2 partitions, 2 consumers (same group) | Each consumer gets ~50% of messages |
| 1 partition, 2 consumers (different groups) | Both consumers receive ALL messages |
| Multiple groups broadcasting to same channel | Clients receive duplicates |

---

## Related Documentation

- [KAFKA_PARTITIONS_EXPLAINED.md](./KAFKA_PARTITIONS_EXPLAINED.md) - Deep dive on partitions and scaling
- [ARCHITECTURE_NATS_FLOW.md](./ARCHITECTURE_NATS_FLOW.md) - How NATS bridges pods
