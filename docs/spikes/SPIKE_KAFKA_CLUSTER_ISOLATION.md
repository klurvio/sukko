# Spike: Kafka/Redpanda Cluster Isolation and Aggregate Channels

**Date:** 2026-01-14
**Status:** Complete
**Author:** Engineering Team

---

## Context

This spike investigates potential data contamination issues when using aggregate channels (e.g., `all.trade`) across multiple environments (DEV, PROD) and evaluates isolation strategies.

### Key Questions

1. Can DEV and PROD trades contaminate each other when using `all.trade` as a Kafka key?
2. Does topic namespace separation (`sukko.dev.*` vs `sukko.main.*`) provide sufficient isolation?
3. Does using separate Redpanda clusters per environment eliminate contamination risk?
4. Are there hot partition concerns with aggregate channel keys?
5. If we implement sharded aggregates, how does it affect gateway filtering and client subscriptions?

---

## Background

### Current Architecture

Publishers send messages to Redpanda with:
- **Topic**: `sukko.{namespace}.{base}` (e.g., `sukko.main.trade`)
- **Key**: Broadcast subject (e.g., `BTC.trade`, `all.trade`)
- **Value**: JSON message payload

The Kafka key determines:
1. **Partition assignment** (via hash) - messages with same key go to same partition
2. **Broadcast channel** - ws-server uses key as WebSocket subscription subject

### Aggregate Channel Pattern

To support "subscribe to all" use cases, publishers emit to both specific and aggregate channels:

```
Publisher sends TWO messages for each trade:
1. Topic: sukko.main.trade, Key: BTC.trade    → specific subscribers
2. Topic: sukko.main.trade, Key: all.trade    → aggregate subscribers
```

### Current Configuration

DEV environment is configured with `KAFKA_TOPIC_NAMESPACE=main` to use production-style topic names, with isolation provided at the cluster level (separate Redpanda instances).

---

## Analysis

### Scenario 1: Same Cluster, Different Topic Namespaces

```
DEV Publisher  → sukko.dev.trade   [Key: all.trade] → Partition X in dev topic
PROD Publisher → sukko.main.trade  [Key: all.trade] → Partition Y in main topic
```

| Aspect | Assessment |
|--------|------------|
| Isolation | Topics are separate, partitions are independent |
| Contamination Risk | None - different topics have separate partition spaces |
| Concern | Relies on correct `KAFKA_TOPIC_NAMESPACE` configuration |

**Verdict:** Safe, but depends on correct application configuration.

### Scenario 2: Same Cluster, Same Topic Namespace (Misconfiguration)

```
DEV Publisher  → sukko.main.trade  [Key: all.trade] → Partition Z
PROD Publisher → sukko.main.trade  [Key: all.trade] → Partition Z (SAME!)
```

| Aspect | Assessment |
|--------|------------|
| Isolation | None - both write to same topic and partition |
| Contamination Risk | **HIGH** - messages interleave, consumers can't distinguish |
| Concern | Human error in configuration causes data pollution |

**Verdict:** Dangerous. DEV test data would appear in PROD streams.

### Scenario 3: Separate Clusters (Current Setup)

```
DEV Environment:
  └── Redpanda Cluster A (redpanda-dev:9092)
        └── Topic: sukko.main.trade
              └── Partitions: [DEV messages only]

PROD Environment:
  └── Redpanda Cluster B (redpanda-prod:9092)
        └── Topic: sukko.main.trade
              └── Partitions: [PROD messages only]
```

| Aspect | Assessment |
|--------|------------|
| Isolation | Physical - completely separate systems |
| Contamination Risk | **None** - clusters are independent |
| Concern | None for data isolation |

**Verdict:** Safe. Topic namespace is purely a naming convention when clusters are separate.

---

## Cluster Isolation Deep Dive

With separate Redpanda clusters per environment, the following are completely independent:

| Component | DEV Cluster | PROD Cluster | Shared? |
|-----------|-------------|--------------|---------|
| Broker addresses | `redpanda-dev:9092` | `redpanda-prod:9092` | No |
| Topic `sukko.main.trade` | Exists in DEV | Exists in PROD | No |
| Partition space | DEV partitions | PROD partitions | No |
| Key `all.trade` hash | DEV partition N | PROD partition M | No |
| Consumer group offsets | DEV offset storage | PROD offset storage | No |
| Message retention | DEV retention policy | PROD retention policy | No |

The topic name `sukko.main.trade` in Cluster A has **zero relationship** to `sukko.main.trade` in Cluster B. They are as separate as two PostgreSQL databases that happen to have tables with the same name.

### Why Use Same Namespace Across Environments?

With cluster-level isolation, using `KAFKA_TOPIC_NAMESPACE=main` in all environments provides:

1. **Identical topic names** - Simplifies configuration, dashboards, and alerts
2. **Same code paths** - No environment-specific topic name logic
3. **Realistic testing** - DEV mirrors PROD topic structure exactly
4. **Reduced config complexity** - Fewer environment variables to manage

---

## Hot Partition Analysis

Even with proper isolation, the `all.trade` aggregate key creates a hot partition concern within each environment.

### Partition Distribution

```
Topic: sukko.main.trade (8 partitions)

Key: BTC.trade   → hash → Partition 3
Key: ETH.trade   → hash → Partition 7
Key: SOL.trade   → hash → Partition 1
Key: DOGE.trade  → hash → Partition 4
Key: all.trade   → hash → Partition 5  ← ALL aggregate traffic
```

### Traffic Analysis

| Channel Type | Messages/Trade | Partition Distribution |
|--------------|----------------|------------------------|
| Specific (`BTC.trade`) | 1 | Distributed across partitions |
| Aggregate (`all.trade`) | 1 | Single partition (hot) |

For N tokens with T trades/second each:
- Specific channels: T trades/sec distributed across partitions
- Aggregate channel: N × T trades/sec to ONE partition

### Hot Partition Impact

| Metric | Specific Channels | Aggregate Channel |
|--------|-------------------|-------------------|
| Throughput ceiling | ~10-50 MB/s per partition | Same, but receives all traffic |
| Consumer load | Distributed | Concentrated |
| Latency under load | Low | Higher at scale |

### Mitigation Options

#### Option A: Accept Hot Partition (Recommended for Current Scale)

For moderate traffic (<10K aggregate messages/sec), a single partition handles load adequately.

**Pros:** Simple, no code changes
**Cons:** Scaling ceiling

#### Option B: Sharded Aggregates

```
Key: all.trade.0   # shard 0
Key: all.trade.1   # shard 1
Key: all.trade.2   # shard 2
```

Publisher: `all.trade.{hash(tokenId) % num_shards}`
Client subscribes to all shards or uses wildcard matching.

**Pros:** Distributed load
**Cons:** Client complexity, ordering only within shard

#### Option C: Separate Aggregate Topic

```
Specific: Topic=sukko.main.trade, Key=BTC.trade
Aggregate: Topic=sukko.main.trade.aggregate, Key=all.trade
```

Configure aggregate topic with more partitions and different retention.

**Pros:** Independent scaling
**Cons:** More topics to manage

---

## Sharded Aggregates: Gateway and Client Impact

Before implementing sharded aggregates, we must understand the impact on gateway filtering and client subscriptions.

### The Core Problem

The ws-server uses **exact match** subscriptions. This creates a fundamental issue with sharded keys:

#### Current Flow (Working)

```
Publisher:  Key = "all.trade"
                    ↓
ws-server:  broadcast("all.trade", message)
                    ↓
Client:     subscribed to ["all.trade"] → ✓ RECEIVES MESSAGE
```

#### Naive Sharded Flow (Broken)

```
Publisher:  Key = "all.trade.0"
                    ↓
ws-server:  broadcast("all.trade.0", message)
                    ↓
Client:     subscribed to ["all.trade"] → ✗ NO MATCH (exact matching only)
```

### Gateway Filtering Impact

**Current gateway patterns:**

```go
PublicPatterns: "*.trade,*.liquidity,...,all.trade,all.liquidity,..."
```

**With sharded aggregates:**

| Approach | Pattern Change | Works? |
|----------|----------------|--------|
| Add each shard explicitly | `all.trade.0,all.trade.1,all.trade.2,...` | Yes, but brittle |
| Use wildcard suffix | `all.trade.*` | Yes, gateway supports wildcards |

**Verdict:** Gateway filtering is **low impact** - simple config change to add `all.trade.*` pattern.

### Client Subscription Impact

This is the critical concern. Four options exist:

#### Option 1: Clients Subscribe to All Shards Explicitly

```javascript
// Client must know shard count and subscribe to each
subscribe(["all.trade.0", "all.trade.1", "all.trade.2", "all.trade.3"])
```

| Aspect | Assessment |
|--------|------------|
| Client complexity | High - must know shard count |
| Shard changes | Breaking - all clients must update |
| Backward compatible | No |

#### Option 2: Server-Side Wildcard Expansion

Client subscribes to `all.trade.*`, server expands to all shards internally.

```go
if strings.HasSuffix(channel, ".*") {
    base := strings.TrimSuffix(channel, ".*")
    for i := 0; i < numShards; i++ {
        subscribeClient(client, fmt.Sprintf("%s.%d", base, i))
    }
}
```

| Aspect | Assessment |
|--------|------------|
| Client complexity | Low - subscribe to `all.trade.*` |
| Server complexity | Medium - expansion logic needed |
| Security risk | Medium - wildcards could match too broadly |
| Backward compatible | Partial - new subscription format |

#### Option 3: Virtual Channel Mapping

Client subscribes to `all.trade`, server internally maps to sharded channels.

```go
var virtualChannels = map[string][]string{
    "all.trade": {"all.trade.0", "all.trade.1", "all.trade.2", "all.trade.3"},
}

func subscribe(client, channel) {
    if shards, ok := virtualChannels[channel]; ok {
        for _, shard := range shards {
            subscribeInternal(client, shard)
        }
    } else {
        subscribeInternal(client, channel)
    }
}
```

| Aspect | Assessment |
|--------|------------|
| Client complexity | None - unchanged |
| Server complexity | Medium - mapping table maintenance |
| Shard changes | Server config change only |
| Backward compatible | Yes |

#### Option 4: Header-Based Subject Normalization (Recommended)

**Use sharded keys for Kafka partitioning, but normalize to single subject for broadcast.**

The key insight: separate two concerns:
- **Partitioning key**: How Kafka distributes messages across partitions
- **Broadcast subject**: What channel clients subscribe to

```go
// Publisher - shard the Kafka key for partition distribution
shard := hash(tokenID) % NUM_SHARDS

record := &kgo.Record{
    Topic: "sukko.main.trade",
    Key:   []byte(fmt.Sprintf("all.trade.%d", shard)),  // Partitioning key
    Headers: []kgo.RecordHeader{
        {Key: "broadcast-subject", Value: []byte("all.trade")},  // Broadcast subject
    },
    Value: message,
}
```

```go
// Consumer - use header for broadcast if present
func (c *Consumer) processRecord(record *kgo.Record) {
    subject := string(record.Key)  // Default: "all.trade.0"

    // Check for broadcast subject override
    for _, h := range record.Headers {
        if h.Key == "broadcast-subject" {
            subject = string(h.Value)  // Override: "all.trade"
            break
        }
    }

    c.broadcast(subject, record.Value)
}
```

**Result:**

```
Kafka:      Key="all.trade.0" → Partition 3 (distributed!)
            Key="all.trade.1" → Partition 7 (distributed!)
            Key="all.trade.2" → Partition 1 (distributed!)
                    ↓
ws-server:  Reads header, broadcasts to "all.trade"
                    ↓
Client:     subscribed to ["all.trade"] → ✓ RECEIVES MESSAGE
```

| Aspect | Assessment |
|--------|------------|
| Client complexity | None - unchanged |
| Gateway change | None - still validates `all.trade` |
| Kafka distribution | Achieved - multiple partitions |
| Backward compatible | Yes |
| Publisher change | Add header to aggregate messages |
| Consumer change | Read header (small change) |

### Sharding Options Comparison

| Option | Client Change | Server Change | Gateway Change | Backward Compatible | Recommended |
|--------|---------------|---------------|----------------|---------------------|-------------|
| 1. Explicit shards | **High** | None | Config | No | No |
| 2. Wildcard expansion | Medium | Medium | Config | Partial | No |
| 3. Virtual channels | None | Medium | None | Yes | Maybe |
| 4. Header normalization | None | Low | None | **Yes** | **Yes** |

### Recommended Sharding Implementation

When aggregate traffic exceeds 10K messages/sec, implement **Option 4 (Header-Based Normalization)**:

```go
// Publisher implementation
func publishAggregateEvent(tokenID string, eventType string, message []byte, numShards int) {
    // Shard based on token ID for even distribution
    shard := hashString(tokenID) % numShards

    record := &kgo.Record{
        Topic: getTopicForEvent(eventType),
        Key:   []byte(fmt.Sprintf("all.%s.%d", eventType, shard)),
        Headers: []kgo.RecordHeader{
            {Key: "broadcast-subject", Value: []byte(fmt.Sprintf("all.%s", eventType))},
        },
        Value: message,
    }

    producer.Produce(ctx, record)
}

// Consumer implementation
func (c *Consumer) processRecord(record *kgo.Record) {
    subject := string(record.Key)

    // Check for broadcast subject override (for sharded aggregates)
    for _, h := range record.Headers {
        if h.Key == "broadcast-subject" {
            subject = string(h.Value)
            break
        }
    }

    c.broadcast(subject, record.Value)
}
```

**Benefits:**
1. Kafka partitioning is distributed (no hot partition)
2. Clients subscribe to `all.trade` as before (no changes)
3. Gateway validates `all.trade` as before (no changes)
4. Fully backward compatible
5. Can be rolled out incrementally

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Cross-env contamination (shared cluster) | N/A | High | Using separate clusters |
| Cross-env contamination (separate clusters) | None | N/A | Physical isolation |
| Hot partition at current scale | Low | Low | Monitor partition lag |
| Hot partition at 10x scale | Medium | Medium | Implement sharding |
| Wrong broker address in config | Low | Medium | Config validation, separate networks |

---

## Recommendations

### 1. Continue Using Separate Clusters (Current Approach)

Separate Redpanda clusters per environment provides the strongest isolation guarantee. The `KAFKA_TOPIC_NAMESPACE` configuration becomes a naming convention, not a security boundary.

### 2. Keep `KAFKA_TOPIC_NAMESPACE=main` in All Environments

With cluster-level isolation, using consistent topic names across environments:
- Simplifies configuration
- Enables identical monitoring/alerting
- Reduces environment-specific code paths

### 3. Monitor Aggregate Partition Metrics

Add monitoring for the partition receiving `all.trade` messages:

```yaml
# Prometheus alert example
- alert: AggregatePartitionLag
  expr: kafka_consumer_group_lag{topic="sukko.main.trade", partition="5"} > 10000
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: High lag on aggregate partition
```

### 4. Document Cluster Addresses Clearly

Ensure broker addresses are clearly documented and environment-specific:

```bash
# DEV
KAFKA_BROKERS=redpanda-dev.internal:9092

# STAGING
KAFKA_BROKERS=redpanda-staging.internal:9092

# PROD
KAFKA_BROKERS=redpanda-prod.internal:9092
```

### 5. Plan for Sharded Aggregates at Scale

If aggregate traffic exceeds 10K messages/sec, implement sharded aggregates:

```go
func getAggregateKey(tokenID string, numShards int) string {
    shard := hash(tokenID) % numShards
    return fmt.Sprintf("all.trade.%d", shard)
}
```

---

## Conclusion

### Cluster Isolation

**Using separate Redpanda clusters per environment eliminates cross-environment contamination risk entirely.** The current setup with `KAFKA_TOPIC_NAMESPACE=main` in DEV is safe because isolation is enforced at the infrastructure level, not the application level.

### Hot Partition

The `all.trade` aggregate key creates a hot partition within each environment, but this is a scaling concern rather than a correctness concern. At current traffic levels, this is acceptable. Monitor partition lag and implement sharding if aggregate traffic grows significantly.

### Sharded Aggregates

If sharding becomes necessary, **use header-based subject normalization** (Option 4). This approach:
- Distributes Kafka partition load via sharded keys (`all.trade.0`, `all.trade.1`, etc.)
- Preserves client experience by normalizing broadcast subject via Kafka headers
- Requires no gateway or client changes
- Is fully backward compatible

**Do not** implement naive sharding (changing the broadcast subject to sharded keys) as this breaks exact-match client subscriptions and requires coordinated client updates.

---

## Related Documentation

- [SUBJECT_PATTERNS.md](../architecture/SUBJECT_PATTERNS.md) - Subject/channel naming conventions
- [KAFKA_PARTITIONS_EXPLAINED.md](../architecture/KAFKA_PARTITIONS_EXPLAINED.md) - Kafka partitioning deep dive
- [Redpanda Documentation](https://docs.redpanda.com/) - Cluster configuration and monitoring
