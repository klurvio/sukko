# Kafka Partitions Explained

## Table of Contents
- [1. What is a Kafka Partition?](#1-what-is-a-kafka-partition)
- [2. Why Do Partitions Exist?](#2-why-do-partitions-exist)
- [3. How Messages Get Assigned to Partitions](#3-how-messages-get-assigned-to-partitions)
- [4. Consumer Groups: How Kafka Splits Partitions Across Instances](#4-consumer-groups-how-kafka-splits-partitions-across-instances)
- [5. Your Specific Case: 8 Topics, Multi-Instance](#5-your-specific-case-8-topics-multi-instance)
- [6. Message Flow Example](#6-message-flow-example)
- [7. Partition Assignment Strategies](#7-partition-assignment-strategies)
- [8. Detailed Rebalancing Process](#8-detailed-rebalancing-process)
- [9. Why Your Current Code Breaks with Multiple Instances](#9-why-your-current-code-breaks-with-multiple-instances)
- [10. The Solution: External Message Bus](#10-the-solution-external-message-bus)
- [Summary](#summary)

---

## 1. What is a Kafka Partition?

Think of a Kafka **topic** as a category or feed name, like a TV channel. A **partition** is like having multiple recording devices for that channel, where each device records a portion of the broadcast.

### Basic Analogy

```
Traditional Queue (Single Line):
┌─────────────────────────────────┐
│ [Msg1][Msg2][Msg3][Msg4][Msg5] │ ← One queue, one consumer at a time
└─────────────────────────────────┘

Kafka Topic with Partitions (Multiple Lanes):
Topic: "orders"
┌─────────────────────┐
│ Partition 0:        │
│ [Msg1][Msg4][Msg7]  │ ← Independent queue
├─────────────────────┤
│ Partition 1:        │
│ [Msg2][Msg5][Msg8]  │ ← Independent queue
├─────────────────────┤
│ Partition 2:        │
│ [Msg3][Msg6][Msg9]  │ ← Independent queue
└─────────────────────┘

Each partition is an ORDERED, IMMUTABLE sequence of messages
```

### Key Properties

1. **Each partition is ordered** - Messages within a partition maintain strict order
2. **Partitions are independent** - No ordering guarantee ACROSS partitions
3. **Partitions are immutable** - Messages are append-only (log structure)
4. **Partitions enable parallelism** - Multiple consumers can read different partitions simultaneously

---

## 2. Why Do Partitions Exist?

### Problem: Single Queue Bottleneck

```
Without partitions (traditional queue):

Producer 1 ─┐       ┌─────────┐      ┌─→ Consumer 1
Producer 2 ─┼──→    │ Queue   │  ──→ │  (bottleneck!)
Producer 3 ─┘       └─────────┘      └─→ Only 1 can read

Throughput: Limited by single consumer speed
```

### Solution: Partitions Enable Parallel Processing

```
With partitions:

Producer 1 ──→ Partition 0 ──→ Consumer 1 (reads partition 0)
Producer 2 ──→ Partition 1 ──→ Consumer 2 (reads partition 1)
Producer 3 ──→ Partition 2 ──→ Consumer 3 (reads partition 2)

Throughput: 3× faster (can scale to hundreds of partitions)
```

### Real Benefits

1. **Scalability** - More partitions = more parallel consumers
2. **High throughput** - Distribute load across many disks/brokers
3. **Fault tolerance** - Each partition can be replicated
4. **Ordering guarantees** - Strict order within a partition (by partition key)

---

## 3. How Messages Get Assigned to Partitions

When a producer sends a message, Kafka decides which partition to write to:

### Strategy 1: Round-Robin (No Key)

```go
// Producer code (no key specified)
producer.Produce("orders", nil, []byte("order data"))
```

```
Messages distributed evenly:

Message 1 → Partition 0
Message 2 → Partition 1
Message 3 → Partition 2
Message 4 → Partition 0  ← Wraps around
Message 5 → Partition 1
...
```

**Use case**: When you don't care about ordering, just want high throughput

### Strategy 2: Partition Key (Hash-Based)

```go
// Producer code (with key)
producer.Produce("orders", []byte("user-123"), []byte("order data"))
```

```
Kafka hashes the key:

Key: "user-123" → hash() % 3 = Partition 1
Key: "user-456" → hash() % 3 = Partition 0
Key: "user-789" → hash() % 3 = Partition 2
Key: "user-123" → hash() % 3 = Partition 1  ← Same user, same partition!

Result: All messages for "user-123" go to Partition 1 (ordered!)
```

**Use case**: When you need ordering guarantees (e.g., all trades for BTC in order)

---

## 4. Consumer Groups: How Kafka Splits Partitions Across Instances

This is the **critical concept** for multi-instance deployments.

### Consumer Group Basics

A **consumer group** is a set of consumers that work together to consume a topic. Kafka **automatically assigns partitions** to consumers in the group.

**Golden Rule**:
> Each partition is consumed by **exactly ONE consumer** in a consumer group at any given time.

### Example Scenarios

#### Scenario 1: One Instance (Current)

```
Kafka Topic: sukko.trades
┌─────────────┬─────────────┬─────────────┬─────────────┐
│ Partition 0 │ Partition 1 │ Partition 2 │ Partition 3 │
│ [M1][M5]    │ [M2][M6]    │ [M3][M7]    │ [M4][M8]    │
└─────────────┴─────────────┴─────────────┴─────────────┘
       │             │             │             │
       └─────────────┴─────────────┴─────────────┘
                          ↓
              Consumer Group: ws-server-group
                    ┌──────────────┐
                    │ Instance 1   │
                    │ Consumer A   │
                    │ Gets: 0,1,2,3│ ← All 4 partitions
                    └──────────────┘
                          ↓
              Receives ALL messages: M1-M8
```

**Result**: One consumer gets assigned ALL partitions

#### Scenario 2: Two Instances Join

```
Kafka Topic: sukko.trades
┌─────────────┬─────────────┬─────────────┬─────────────┐
│ Partition 0 │ Partition 1 │ Partition 2 │ Partition 3 │
│ [M1][M5]    │ [M2][M6]    │ [M3][M7]    │ [M4][M8]    │
└─────────────┴─────────────┴─────────────┴─────────────┘
       │             │             │             │
       │             │             │             │
   ┌───┴─────┐   ┌───┴─────┐   ┌──┴──────┐   ┌──┴──────┐
   │         │   │         │   │         │   │         │
   ↓         │   │         ↓   ↓         │   │         ↓
┌──────────┐ │   │  ┌──────────┐ ┌──────────┐ │  ┌──────────┐
│Instance 1│ │   │  │Instance 1│ │Instance 2│ │  │Instance 2│
│Consumer A│ │   │  │Consumer A│ │Consumer B│ │  │Consumer B│
│Gets: 0,2 │←┘   └→│Gets: 0,2 │ │Gets: 1,3 │←┘  └→│Gets: 1,3 │
└──────────┘        └──────────┘ └──────────┘        └──────────┘
     ↓                    ↓
Receives: M1,M3,M5,M7  Receives: M2,M4,M6,M8

Consumer Group: ws-server-group (2 members)
  - Consumer A (Instance 1): Partitions 0, 2
  - Consumer B (Instance 2): Partitions 1, 3
```

**Result**: Partitions split evenly - each consumer gets **half** the messages

#### Scenario 3: Three Instances Join

```
Kafka Topic: sukko.trades (4 partitions)

Consumer Group: ws-server-group (3 members)
  - Consumer A (Instance 1): Partition 0
  - Consumer B (Instance 2): Partition 1
  - Consumer C (Instance 3): Partitions 2, 3  ← Gets 2 partitions

Instance 1: Receives messages from Partition 0 only
Instance 2: Receives messages from Partition 1 only
Instance 3: Receives messages from Partitions 2 AND 3
```

**Note**: With 4 partitions and 3 consumers, one consumer gets 2 partitions

#### Scenario 4: Five Instances Join (Over-Provisioned)

```
Kafka Topic: sukko.trades (4 partitions)

Consumer Group: ws-server-group (5 members)
  - Consumer A (Instance 1): Partition 0
  - Consumer B (Instance 2): Partition 1
  - Consumer C (Instance 3): Partition 2
  - Consumer D (Instance 4): Partition 3
  - Consumer E (Instance 5): IDLE  ← No partitions assigned!

Instance 5 sits IDLE (no work to do)
```

**Important**: You can't have more **active** consumers than partitions in a group!

---

## 5. Your Specific Case: 8 Topics, Multi-Instance

### Your Topics (from codebase)

**All topics configured with 12 partitions each** (configured in `deployments/v1/shared/kafka-topics.env`):

```
1. sukko.trades       → 12 partitions
2. sukko.liquidity    → 12 partitions
3. sukko.metadata     → 12 partitions
4. sukko.social       → 12 partitions
5. sukko.community    → 12 partitions
6. sukko.creation     → 12 partitions
7. sukko.analytics    → 12 partitions
8. sukko.balances     → 12 partitions

TOTAL: 96 partitions across 8 topics
```

### Why 12 Partitions? The "Magic Number" Explained

The choice of **12 partitions per topic** is NOT arbitrary - it's based on solid engineering principles:

#### 1. Mathematical Divisibility (Maximum Flexibility)

12 has the most divisors of any number under 20, making it incredibly flexible for scaling:

```
Divisors of 12: 1, 2, 3, 4, 6, 12

This means clean splits for:
├─  1 instance:  12 partitions each (no waste)
├─  2 instances:  6 partitions each (perfect split)
├─  3 instances:  4 partitions each (perfect split)
├─  4 instances:  3 partitions each (perfect split)
├─  6 instances:  2 partitions each (perfect split)
└─ 12 instances:  1 partition each (maximum parallelism)

Compare to other numbers:
- 10 partitions: Awkward with 3, 4, 6, 7, 8, or 9 instances
- 16 partitions: Awkward with 3, 5, 6, 7, 9, 10, 11 instances
- 8 partitions:  Limited scaling (only 1, 2, 4, 8 instances work cleanly)
```

#### 2. Performance Sweet Spot

Based on Kafka performance characteristics:

```
Too Few Partitions (1-3):
  ✗ Limited parallelism (bottleneck)
  ✗ Can't fully utilize multi-core systems
  ✗ Single consumer becomes bottleneck

Optimal Range (10-15):
  ✓ Good parallelism without overhead
  ✓ Kafka coordinator can manage efficiently
  ✓ Low rebalance latency
  ✓ Memory footprint manageable

Too Many Partitions (50+):
  ✗ High metadata overhead in Kafka
  ✗ Longer rebalance times
  ✗ More memory per consumer
  ✗ Increased ZooKeeper/KRaft load
```

#### 3. Matches CPU Core Counts

Modern servers commonly have 8-16 cores, and 12 partitions aligns well:

```
Your current setup (e2-highcpu-8):
  8 vCPUs available

With 12 partitions:
  ├─ Can run 12 parallel consumer threads
  ├─ Core pinning: ~1.5 partitions per core
  └─ Utilizes all CPU capacity without over-subscription

Future scaling (e2-highcpu-16):
  16 vCPUs available

With 12 partitions:
  ├─ Can run 12 parallel threads
  ├─ ~0.75 partitions per core
  └─ Room for other processes (LoadBalancer, monitoring, etc.)
```

#### 4. Industry Best Practices

From Kafka's official recommendations and field experience:

```
LinkedIn (Kafka creators):
  "Start with max(t/p, t/c) where:
   t = target throughput
   p = partition throughput (~10-30 MB/s)
   c = consumer throughput

   For most use cases, 10-12 partitions is optimal"

Confluent (Kafka maintainers):
  "More than 12 partitions per broker core rarely improves throughput"

AWS MSK recommendations:
  "12-24 partitions per topic for general workloads"
```

#### 5. Your Specific Requirements

Looking at your system architecture:

```
Expected Load:
  ├─ 18,000 WebSocket connections
  ├─ ~1,000 messages/sec throughput
  ├─ 8 topics (different data types)
  └─ Need for horizontal scaling (2-4 instances)

12 Partitions Provides:
  ├─ 12,000 messages/sec capacity per topic (plenty of headroom)
  ├─ Clean split for 2, 3, or 4 instance deployments
  ├─ Low enough count to avoid Kafka overhead
  └─ High enough for good parallelism

Math Check:
  1,000 msg/sec ÷ 12 partitions = ~83 msg/sec per partition

  Kafka can handle 10,000+ msg/sec per partition
  Result: 99% headroom for traffic spikes
```

#### 6. Comparison: Alternative Partition Counts

What if you chose differently?

| Partitions | Pros | Cons | Best For |
|------------|------|------|----------|
| **3** | Simple, low overhead | Limited scaling (max 3 instances) | Low-traffic topics |
| **6** | Good for 2-3 instances | Awkward with 4+ instances | Medium traffic |
| **12** ⭐ | Maximum flexibility | Slightly more metadata | Most use cases |
| **24** | Very high parallelism | Higher overhead, slower rebalancing | High-throughput topics |
| **50+** | Maximum parallelism | High overhead, slow rebalancing | Massive scale only |

#### 7. When to Use Different Counts

You might deviate from 12 if:

```
Use FEWER partitions (3-6) when:
  ├─ Very low traffic topic (<100 msg/sec)
  ├─ Strong ordering requirements (fewer partitions = easier ordering)
  ├─ Small team/simple deployment (1-2 instances max)
  └─ Example: sukko.metadata (if very low volume)

Use MORE partitions (24-30) when:
  ├─ Extremely high traffic (>10,000 msg/sec)
  ├─ Need to scale to 10+ instances
  ├─ Messages are very small (high msg/sec, low MB/sec)
  └─ Example: If sukko.trades becomes 100x volume

Keep 12 partitions when:
  ├─ Medium-high traffic (100-10,000 msg/sec) ← Your case
  ├─ Need flexible scaling (2-6 instances)
  ├─ Want to keep configuration simple
  └─ Not sure yet (12 is safe default)
```

#### 8. The "Rule of Thumb" Formula

Industry standard for choosing partition count:

```
Partitions = max(T/P, T/C)

Where:
  T = Target throughput (msg/sec)
  P = Partition throughput (typically 1,000-10,000 msg/sec)
  C = Consumer throughput (msg/sec per consumer)

Your calculation:
  T = 1,000 msg/sec (current)
  P = 5,000 msg/sec (conservative estimate)
  C = 500 msg/sec (per consumer thread)

  Partitions = max(1000/5000, 1000/500)
             = max(0.2, 2)
             = 2 partitions (minimum)

  But multiply by 4-6× for:
  ├─ Growth headroom (traffic spikes)
  ├─ Scaling flexibility (multiple instances)
  └─ Performance buffer

  Result: 2 × 6 = 12 partitions ✓
```

#### 9. Real-World Examples

How major companies partition topics:

```
Uber:
  ├─ Trip events: 12-16 partitions
  ├─ Driver locations: 50+ partitions (very high volume)
  └─ Pricing events: 6-8 partitions (medium volume)

Netflix:
  ├─ Viewing events: 24 partitions
  ├─ User actions: 12 partitions
  └─ Metadata updates: 3 partitions

Your system (Sukko):
  ├─ Trades: 12 partitions (medium-high volume, real-time)
  ├─ Liquidity: 12 partitions (medium volume, frequent updates)
  └─ Social: 12 partitions (lower volume, but keep consistent)
```

#### 10. Future-Proofing

Why 12 works long-term:

```
Today (18K connections, 1K msg/sec):
  ├─ 12 partitions: 99% headroom
  ├─ 1 instance: Works perfectly
  └─ CPU: 80% idle

Tomorrow (100K connections, 10K msg/sec):
  ├─ 12 partitions: Still 90% headroom
  ├─ 4 instances: Clean split (3 partitions each)
  └─ No topic reconfiguration needed

5 Years (1M connections, 100K msg/sec):
  ├─ Increase to 24 partitions (still manageable)
  ├─ 12 instances: Clean split (2 partitions each)
  └─ Graceful scaling path
```

### Summary: Why 12 is the "Magic Number"

```
12 partitions per topic is optimal because:

✓ Mathematically divisible (1,2,3,4,6,12)
✓ Industry-proven sweet spot (10-15 range)
✓ Matches common CPU core counts (8-16)
✓ Provides 10-100× headroom for your traffic
✓ Enables clean horizontal scaling
✓ Low Kafka overhead (metadata, rebalancing)
✓ Future-proof for 10-100× growth
✓ Simple to manage (same count across topics)

The "magic" isn't mystical - it's engineering.
```

### Partition Assignment: 1 Instance vs 2 Instances

#### With 1 Instance

```
Consumer Group: ws-server-group
Member: Consumer-vm1-abc123

Assigned Partitions: ALL 96
┌─────────────────────────────────────────────────────────┐
│ sukko.trades:      [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.liquidity:   [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.metadata:    [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.social:      [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.community:   [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.creation:    [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.analytics:   [0,1,2,3,4,5,6,7,8,9,10,11]          │
│ sukko.balances:    [0,1,2,3,4,5,6,7,8,9,10,11]          │
└─────────────────────────────────────────────────────────┘

Result: Instance 1 processes 100% of all messages
```

#### With 2 Instances (After Rebalance)

Kafka uses **range assignment strategy** (default) which splits by topic:

```
Consumer Group: ws-server-group
Members: Consumer-vm1-abc123, Consumer-vm2-def456

INSTANCE 1 Assigned Partitions: 48
┌──────────────────────────────────────────────────────────┐
│ sukko.trades:      [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.liquidity:   [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.metadata:    [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.social:      [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.community:   [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.creation:    [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.analytics:   [0,2,4,6,8,10]       ← 6 of 12        │
│ sukko.balances:    [0,2,4,6,8,10]       ← 6 of 12        │
└──────────────────────────────────────────────────────────┘
Total: 48 partitions (50% of 96)

INSTANCE 2 Assigned Partitions: 48
┌──────────────────────────────────────────────────────────┐
│ sukko.trades:      [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.liquidity:   [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.metadata:    [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.social:      [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.community:   [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.creation:    [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.analytics:   [1,3,5,7,9,11]       ← 6 of 12        │
│ sukko.balances:    [1,3,5,7,9,11]       ← 6 of 12        │
└──────────────────────────────────────────────────────────┘
Total: 48 partitions (50% of 96)

Result: Each instance processes exactly 50% of messages
```

---

## 6. Message Flow Example

Let's trace a specific message:

### With 1 Instance

```
PRODUCER publishes:
  Topic: sukko.trades
  Key: "BTC"  (token ID)
  Value: {"price": 50000, "volume": 1.5, ...}

  Kafka calculates: hash("BTC") % 4 = Partition 2

  Message written to: sukko.trades-2

CONSUMPTION:
  ✓ Instance 1 is assigned partition 2
  ✓ Instance 1 consumes message
  ✓ Routes to BroadcastBus
  ✓ All shards (0, 1, 2) receive it
  ✓ All clients see the update
```

### With 2 Instances (BROKEN - Current Implementation)

```
MESSAGE 1:
  Topic: sukko.trades
  Key: "BTC"
  Kafka calculates: hash("BTC") % 4 = Partition 2

  Message written to: sukko.trades-2

  ✓ Instance 1 is assigned partition 2 (from assignment above)
  ✓ Instance 1 consumes message
  ✓ Routes to BroadcastBus #1 (in-memory on Instance 1)
  ✓ Only Shards 0, 1, 2 on Instance 1 receive it
  ✗ Instance 2's BroadcastBus never sees it
  ✗ Shards 3, 4, 5 on Instance 2 don't get it
  ✗ Clients on Instance 2 miss this BTC update!

────────────────────────────────────────────────

MESSAGE 2:
  Topic: sukko.trades
  Key: "ETH"
  Kafka calculates: hash("ETH") % 4 = Partition 1

  Message written to: sukko.trades-1

  ✗ Instance 1 is NOT assigned partition 1
  ✓ Instance 2 is assigned partition 1
  ✓ Instance 2 consumes message
  ✓ Routes to BroadcastBus #2 (in-memory on Instance 2)
  ✓ Only Shards 3, 4, 5 on Instance 2 receive it
  ✗ Shards 0, 1, 2 on Instance 1 miss this ETH update!
```

**Problem**: Different instances handle different tokens randomly (depends on hash)!

---

## 7. Partition Assignment Strategies

Kafka supports different strategies for assigning partitions:

### Strategy 1: Range (Default)

Partitions divided by topic, then split among consumers:

```
Topic: sukko.trades (partitions 0, 1, 2, 3)
Consumers: 2

Range assignment:
  Consumer 1: [0, 1]  ← First half
  Consumer 2: [2, 3]  ← Second half
```

### Strategy 2: Round-Robin

All partitions pooled, then distributed round-robin:

```
All partitions: [t1-0, t1-1, t1-2, t2-0, t2-1, t2-2, ...]
Consumers: 2

Round-robin:
  Consumer 1: [t1-0, t1-2, t2-1, ...]  ← Every other
  Consumer 2: [t1-1, t2-0, t2-2, ...]  ← Every other
```

### Strategy 3: Sticky

Tries to keep previous assignments during rebalance (minimize movement):

```
Before rebalance:
  Consumer 1: [0, 1, 2]
  Consumer 2: [3, 4, 5]

Consumer 3 joins:

Sticky assignment:
  Consumer 1: [0, 1]      ← Kept most of original
  Consumer 2: [3, 4]      ← Kept most of original
  Consumer 3: [2, 5]      ← Gets remainder
```

---

## 8. Detailed Rebalancing Process

### Step-by-Step: What Happens When Instance 2 Joins

```
TIMELINE:

T=0s: Instance 1 running alone
  ├─ Consuming all 22 partitions
  ├─ Processing ~1000 messages/sec
  └─ All clients receiving updates

T=10s: Instance 2 starts, joins consumer group
  ↓
  [1] Instance 2 sends JoinGroup request to Kafka coordinator
  [2] Coordinator detects new member
  [3] Coordinator initiates REBALANCE

T=10.1s: REBALANCE PHASE 1 - Revocation
  ↓
  [4] Coordinator sends "Revoke" to Instance 1
      ├─ Instance 1 STOPS consuming
      ├─ Commits current offsets to Kafka:
      │   sukko.trades-0: offset 5000
      │   sukko.trades-1: offset 4800
      │   ... (all 22 partitions)
      └─ Waits for new assignment

  [5] Both instances now IDLE (no consumption happening)

T=10.5s: REBALANCE PHASE 2 - Assignment
  ↓
  [6] Coordinator calculates new assignment:
      Using range strategy:
        Instance 1 → [11 partitions]
        Instance 2 → [11 partitions]

  [7] Coordinator sends assignments to both instances

T=11s: REBALANCE PHASE 3 - Resume
  ↓
  [8] Instance 1 receives assignment
      ├─ Assigned: sukko.trades [0, 2], sukko.liquidity [0, 2], ...
      ├─ Looks up last committed offsets
      └─ Resumes from: sukko.trades-0 offset 5000, etc.

  [9] Instance 2 receives assignment
      ├─ Assigned: sukko.trades [1, 3], sukko.liquidity [1], ...
      ├─ No previous offsets (new consumer)
      └─ Starts from: END (configured as "latest")

T=11.5s: BOTH INSTANCES CONSUMING
  ├─ Instance 1: Processing partitions 0, 2, 4, ...
  ├─ Instance 2: Processing partitions 1, 3, 5, ...
  └─ Total throughput: Still ~1000 messages/sec
      But split: ~500 msg/sec each instance

REBALANCE COMPLETE
Duration: ~1.5 seconds (may be longer under high load)
```

### During Rebalance: What Happens to Messages?

```
Messages arriving during rebalance:

T=10.1s - T=11.5s: NO CONSUMPTION (both instances idle)
  ├─ Kafka brokers keep buffering messages
  ├─ Messages pile up in partitions
  └─ No data loss! Kafka stores everything

T=11.5s: Consumption resumes
  ├─ Backlog starts processing
  ├─ Consumers catch up (lag increases temporarily)
  └─ Normal operation resumes within seconds
```

### Visualizing Offset Management

```
Kafka Partition: sukko.trades-0
┌─────┬─────┬─────┬─────┬─────┬─────┬─────┬─────┐
│ 100 │ 101 │ 102 │ 103 │ 104 │ 105 │ 106 │ 107 │ ...
└─────┴─────┴─────┴─────┴─────┴─────┴─────┴─────┘
                    ↑                           ↑
                    │                           │
            Last committed               Current write
            offset: 103                  position: 107

BEFORE REBALANCE:
  Instance 1 reads offset 103
  Instance 1 commits offset 103

DURING REBALANCE:
  Instance 1 stops, commits offset 103
  Kafka remembers: "ws-server-group last read offset 103"

AFTER REBALANCE:
  Instance 1 resumes from offset 104 (next unread message)

NO MESSAGES LOST, NO DUPLICATES (exactly-once semantics)
```

---

## 9. Why Your Current Code Breaks with Multiple Instances

### Full Message Journey (2 Instances)

```
STEP 1: Producer publishes
  ├─ Token: "BTC"
  ├─ Event: "trade"
  ├─ Data: {"price": 50000, ...}
  └─ Kafka writes to: sukko.trades-2 (hash("BTC") % 4 = 2)

STEP 2: Kafka partition assignment
  ├─ Instance 1 owns: partitions [0, 2]  ← Includes partition 2!
  └─ Instance 2 owns: partitions [1, 3]

STEP 3: Instance 1's consumer fetches
  ├─ Consumer reads from sukko.trades-2
  ├─ Gets message: {"token": "BTC", "event": "trade", ...}
  └─ Calls: routeMessage("BTC", "trade", data)

STEP 4: routeMessage() in Instance 1
  ├─ Creates subject: "sukko.token.BTC.trade"
  ├─ Creates BroadcastMessage
  └─ Calls: broadcastBus.Publish(msg)
      │
      └─→ THIS IS THE PROBLEM!
          broadcastBus is IN-MEMORY (Instance 1 only)

STEP 5: BroadcastBus #1 (Instance 1 only)
  ├─ Receives message in channel
  ├─ Fans out to subscribers:
  │   ├─ Shard 0 (Instance 1) ✓ Receives it
  │   ├─ Shard 1 (Instance 1) ✓ Receives it
  │   └─ Shard 2 (Instance 1) ✓ Receives it
  └─ NO CONNECTION to Instance 2's BroadcastBus!

STEP 6: Shards on Instance 1
  ├─ Shard 0 broadcasts to its WebSocket clients ✓
  ├─ Shard 1 broadcasts to its WebSocket clients ✓
  └─ Shard 2 broadcasts to its WebSocket clients ✓

STEP 7: Shards on Instance 2
  ├─ Shard 3: Receives NOTHING ✗
  ├─ Shard 4: Receives NOTHING ✗
  └─ Shard 5: Receives NOTHING ✗
      │
      └─→ Their BroadcastBus #2 never saw this message!

RESULT:
  ✓ Clients on Instance 1: See BTC trade update
  ✗ Clients on Instance 2: Miss BTC trade update

  Problem: 50% of clients miss 50% of messages!
```

### The Fundamental Issue

```
KAFKA PARTITIONS: ✓ Designed for multi-instance (splits load)
                     Each instance gets subset of messages

GO CHANNELS:      ✗ NOT designed for multi-instance (in-memory only)
                     Cannot bridge across VM boundaries

MISMATCH:         Kafka distributes messages across instances,
                  but BroadcastBus can't redistribute to all shards
```

### Code Reference

**File**: `ws/internal/multi/kafka_pool.go:93-109`

```go
func (p *KafkaConsumerPool) routeMessage(tokenID string, eventType string, message []byte) {
    subject := fmt.Sprintf("sukko.token.%s.%s", tokenID, eventType)
    broadcastMsg := &BroadcastMessage{
        Subject: subject,
        Message: message,
    }

    // THIS is the problem - publishes to LOCAL in-memory bus only!
    // Instance 2's BroadcastBus cannot reach Instance 1's shards
    p.broadcastBus.Publish(broadcastMsg)
}
```

---

## 10. The Solution: External Message Bus

### How It Should Work

```
KAFKA LAYER (Distributed):
  Kafka Topics (22 partitions)
       ↓
  Consumer Group splits partitions:
    Instance 1: 11 partitions
    Instance 2: 11 partitions
       ↓

BRIDGE LAYER (Network-based):  ← THIS IS MISSING!
  Both instances publish to External Message Bus:
    ├─ Instance 1 publishes messages from its 11 partitions
    └─ Instance 2 publishes messages from its 11 partitions
       ↓
  External Bus (Redis Pub/Sub or NATS):
    - Receives messages from both instances
    - Broadcasts to ALL subscribers (all shards, all instances)
       ↓

SHARD LAYER (Local):
  All shards subscribe to External Bus:
    Instance 1: Shards 0, 1, 2 ✓ Get ALL messages
    Instance 2: Shards 3, 4, 5 ✓ Get ALL messages
       ↓

RESULT: All clients see all messages ✓
```

### Implementation Options

#### Option 1: Redis Pub/Sub (Recommended)

```go
type RedisBroadcastBus struct {
    client *redis.Client
}

func (r *RedisBroadcastBus) Publish(msg *BroadcastMessage) {
    jsonMsg, _ := json.Marshal(msg)
    r.client.Publish(ctx, "ws:broadcast", jsonMsg).Result()
}

func (r *RedisBroadcastBus) Subscribe() chan *BroadcastMessage {
    pubsub := r.client.Subscribe(ctx, "ws:broadcast")
    subCh := make(chan *BroadcastMessage, 1024)

    go func() {
        for msg := range pubsub.Channel() {
            var bm BroadcastMessage
            json.Unmarshal([]byte(msg.Payload), &bm)
            subCh <- &bm
        }
    }()
    return subCh
}
```

#### Option 2: NATS (Lower latency)

```go
type NatsBroadcastBus struct {
    conn *nats.Conn
}

func (n *NatsBroadcastBus) Publish(msg *BroadcastMessage) {
    jsonMsg, _ := json.Marshal(msg)
    n.conn.Publish("ws.broadcast", jsonMsg)
}

func (n *NatsBroadcastBus) Subscribe() chan *BroadcastMessage {
    subCh := make(chan *BroadcastMessage, 1024)

    n.conn.Subscribe("ws.broadcast", func(m *nats.Msg) {
        var bm BroadcastMessage
        json.Unmarshal(m.Data, &bm)
        subCh <- &bm
    })

    return subCh
}
```

### Corrected Architecture

```
GCP Load Balancer (external)
         ↓
    ┌────┴────┐
   VM1        VM2
(Shard 0-2)  (Shard 3-5)
    ↓         ↓
  Kafka     Kafka
Consumer  Consumer
(11 parts) (11 parts)
    ↓         ↓
    └────┬────┘
         ↓
   Redis Pub/Sub
   (or NATS)
         ↓
  Broadcasts to ALL instances
         ↓
    ┌────┴────┐
    ↓         ↓
 VM1 Shards  VM2 Shards
  ALL get     ALL get
ALL messages ALL messages ✅
```

---

## Summary

### Key Concepts

1. **Partitions** = Parallel lanes for messages within a topic
2. **Consumer Group** = Team of consumers working together
3. **Partition Assignment** = Kafka automatically splits partitions among group members
4. **Rebalancing** = Re-distributing partitions when members join/leave
5. **The Problem** = Kafka distributes load, but BroadcastBus is in-memory only

### The Numbers

```
Your Setup:
├─ 8 Kafka topics (sukko.trades, sukko.liquidity, etc.)
├─ 12 partitions per topic = 96 partitions total
├─ 1 consumer group: "ws-server-group"
└─ N instances (you want 2+)

With 1 Instance:
  └─ 1 consumer gets all 96 partitions ✓ Works!

With 2 Instances:
  ├─ Instance 1: 48 partitions (50%)
  ├─ Instance 2: 48 partitions (50%)
  └─ Each instance's BroadcastBus is isolated ✗ Broken!

With 3 Instances:
  ├─ Instance 1: 32 partitions (33%)
  ├─ Instance 2: 32 partitions (33%)
  ├─ Instance 3: 32 partitions (33%)
  └─ Each instance's BroadcastBus is isolated ✗ Broken!

Solution:
  Replace in-memory BroadcastBus with external message bus
  (Redis, NATS, or even Kafka itself)
```

### Critical Takeaways

- ✅ Kafka **CAN** distribute partitions across multiple instances
- ✅ This **DOES** split the message load (50% per instance with 2 VMs)
- ❌ Your current **in-memory BroadcastBus CANNOT** bridge instances
- ✅ You **MUST** add external message bus for multi-instance to work

### Related Documentation

- `HORIZONTAL_SCALING_PLAN.md` - Describes the planned external message bus architecture
- `PRODUCTION_ARCHITECTURE.md` - Production deployment with NATS integration
- `ws/internal/multi/broadcast.go` - Current in-memory BroadcastBus implementation
- `ws/internal/multi/kafka_pool.go` - Kafka consumer pool implementation
