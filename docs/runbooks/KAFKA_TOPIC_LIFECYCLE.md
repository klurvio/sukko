# Kafka Topic Lifecycle Management

This document describes how to add or remove Kafka topics in the Sukko WebSocket infrastructure.

## Overview

The ws-server uses a **static topic list** defined at compile time. Topics are subscribed to once at startup via `kgo.ConsumeTopics()`. This means:

- New topics are **not detected dynamically** at runtime
- Adding/removing topics requires code changes and redeployment
- Deployment order is critical to prevent message loss

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Topic Flow                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   Publisher ──► Kafka/Redpanda ──► ws-server ──► WebSocket Clients  │
│                      │                                              │
│                      │                                              │
│              ┌───────┴───────┐                                      │
│              │    Topics     │                                      │
│              ├───────────────┤                                      │
│              │ sukko.trades   │                                      │
│              │ sukko.liquidity│                                      │
│              │ sukko.balances │                                      │
│              │ sukko.metadata │                                      │
│              │ sukko.social   │                                      │
│              │ sukko.community│                                      │
│              │ sukko.creation │                                      │
│              │ sukko.analytics│                                      │
│              └───────────────┘                                      │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Adding a New Topic

### Step 1: Code Changes

#### A. Update topic constants (`ws/internal/kafka/config.go`)

```go
// Add new topic constant
const (
    TopicTrades     = "sukko.trades"
    TopicLiquidity  = "sukko.liquidity"
    // ... existing topics ...
    TopicNewFeature = "sukko.newfeature"  // ← ADD
)

// Add to AllTopics()
func AllTopics() []string {
    return []string{
        TopicTrades,
        TopicLiquidity,
        // ... existing topics ...
        TopicNewFeature,  // ← ADD
    }
}

// Add case to TopicToEventType()
func TopicToEventType(topic string) string {
    switch topic {
    case TopicTrades:
        return "trade"
    // ... existing cases ...
    case TopicNewFeature:  // ← ADD
        return "newfeature"
    default:
        return "unknown"
    }
}
```

#### B. Update Helm values (`deployments/k8s/helm/sukko/values.yaml`)

```yaml
ws-server:
  kafka:
    topics:
      - sukko.trades
      - sukko.liquidity
      # ... existing topics ...
      - sukko.newfeature  # ← ADD

redpanda:
  topics:
    # ... existing topics ...
    - name: sukko.newfeature  # ← ADD (for auto-creation)
      partitions: 12
      replicationFactor: 1
```

### Step 2: Deployment Sequence

**Order is critical to prevent message loss!**

```
┌─────────────────────────────────────────────────────────────────────┐
│                 CORRECT ORDER FOR ADDING TOPIC                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. CREATE TOPIC IN KAFKA                                           │
│     └── helm upgrade creates the topic                              │
│     └── Topic exists but is empty                                   │
│                         │                                           │
│                         ▼                                           │
│  2. DEPLOY WS-SERVER                                                │
│     └── Rolling update with new topic in subscription               │
│     └── Consumer group rebalances                                   │
│     └── Now listening to new topic (from LATEST offset)             │
│                         │                                           │
│                         ▼                                           │
│  3. DEPLOY PUBLISHER                                                │
│     └── Starts publishing to new topic                              │
│     └── ws-server receives messages immediately                     │
│                                                                     │
│  ✓ Zero message loss                                                │
└─────────────────────────────────────────────────────────────────────┘
```

**Wrong order consequences:**

| Wrong Order | Consequence |
|-------------|-------------|
| Publisher → ws-server | Messages published before ws-server subscribes are **LOST** (starts from latest offset) |
| ws-server → Topic creation | franz-go retries until topic exists (error logs but no data loss) |

### Step 3: Deployment Commands

```bash
# 1. Update Helm charts (creates topic + deploys ws-server)
helm upgrade sukko ./deployments/k8s/helm/sukko \
  -f values-production.yaml \
  -n sukko-prod

# 2. Verify topic exists
kubectl exec -n sukko-prod sukko-redpanda-0 -- \
  rpk topic list | grep newfeature

# 3. Verify ws-server is consuming (check for partition assignment)
kubectl logs -n sukko-prod -l app.kubernetes.io/name=ws-server | \
  grep "Partitions assigned"

# 4. Deploy publisher (separate deployment)
# ... your publisher deployment process ...
```

---

## Removing a Topic

### Step 1: Deployment Sequence

**Reverse order from adding!**

```
┌─────────────────────────────────────────────────────────────────────┐
│                CORRECT ORDER FOR REMOVING TOPIC                     │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. STOP PUBLISHER                                                  │
│     └── No more messages to the deprecated topic                    │
│                         │                                           │
│                         ▼                                           │
│  2. DRAIN REMAINING MESSAGES                                        │
│     └── Wait for ws-server to consume all pending messages          │
│     └── Verify lag is 0                                             │
│                         │                                           │
│                         ▼                                           │
│  3. DEPLOY WS-SERVER (without topic)                                │
│     └── Remove from AllTopics() and TopicToEventType()              │
│     └── Consumer group stops consuming from deprecated topic        │
│                         │                                           │
│                         ▼                                           │
│  4. DELETE TOPIC (optional)                                         │
│     └── rpk topic delete sukko.deprecated                            │
│     └── Or keep for audit trail                                     │
│                                                                     │
│  ✓ Zero message loss                                                │
└─────────────────────────────────────────────────────────────────────┘
```

### Step 2: Code Changes

Remove from `ws/internal/kafka/config.go`:
- Delete the topic constant
- Remove from `AllTopics()` slice
- Remove case from `TopicToEventType()` switch

Remove from Helm values:
- Remove from `ws-server.kafka.topics`
- Optionally remove from `redpanda.topics`

### Step 3: Drain Verification

```bash
# Check consumer lag before removing
kubectl exec -n sukko-prod sukko-redpanda-0 -- \
  rpk group describe sukko-consumer

# Output should show LAG=0 for the topic being removed:
# TOPIC              PARTITION  CURRENT-OFFSET  LOG-END-OFFSET  LAG
# sukko.deprecated    0          12345           12345           0  ← Ready
# sukko.deprecated    1          6789            6789            0  ← Ready
```

---

## Rolling Update Behavior

During a rolling update when adding/removing topics, the consumer group will rebalance:

```
Before Update:
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│ ws-server-0 │  │ ws-server-1 │  │ ws-server-2 │
│ [old code]  │  │ [old code]  │  │ [old code]  │
│ topics: 8   │  │ topics: 8   │  │ topics: 8   │
└─────────────┘  └─────────────┘  └─────────────┘

During Update (pod-0 restarting):
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│ ws-server-0 │  │ ws-server-1 │  │ ws-server-2 │
│ [UPDATING]  │  │ [old code]  │  │ [old code]  │
│ topics: 9   │  │ topics: 8   │  │ topics: 8   │
└─────────────┘  └─────────────┘  └─────────────┘
       │
       └── REBALANCE TRIGGERED
           └── Partitions reassigned across remaining pods
           └── Brief pause (~10-30 seconds)
           └── No message loss (Kafka retains messages)
```

**Key points:**
- Rebalance happens when consumer group membership changes
- Brief pause (~10-30 seconds) during partition reassignment
- **No message loss** - Kafka retains messages during rebalance
- Current `RebalanceTimeout` is 60 seconds (configured in consumer.go)

---

## Emergency Procedures

### Emergency Topic Addition

If you need to add a topic urgently and cannot wait for full deployment:

```bash
# 1. Create topic manually
kubectl exec -n sukko-prod sukko-redpanda-0 -- \
  rpk topic create sukko.emergency -p 12 -r 1

# 2. Messages will buffer in Kafka until ws-server is updated
#    Default retention: 7 days

# 3. Deploy ws-server with updated code as soon as possible
helm upgrade sukko ./deployments/k8s/helm/sukko \
  -f values-production.yaml \
  -n sukko-prod
```

### Recovering Missed Messages

If messages were published before ws-server was subscribed (wrong deployment order):

```bash
# 1. Check earliest available offset
kubectl exec -n sukko-prod sukko-redpanda-0 -- \
  rpk topic consume sukko.newfeature --offset start -n 1

# 2. Options:
#    a) Accept the loss (if messages are not critical)
#    b) Manually replay from specific offset using ReplayFromOffsets()
#    c) Reset consumer group offset (dangerous - may cause duplicates)

# Reset to earliest (use with caution):
kubectl exec -n sukko-prod sukko-redpanda-0 -- \
  rpk group seek sukko-consumer --to start --topics sukko.newfeature
```

---

## Checklists

### Adding Topic Checklist

```markdown
- [ ] Add topic constant to `ws/internal/kafka/config.go`
- [ ] Add to `AllTopics()` function
- [ ] Add case to `TopicToEventType()`
- [ ] Add topic to `values.yaml` → `ws-server.kafka.topics`
- [ ] Add topic to `values.yaml` → `redpanda.topics` (for auto-creation)
- [ ] Update AsyncAPI spec if applicable (`ws/asyncapi/`)
- [ ] Commit and push changes
- [ ] Deploy Helm chart (creates topic + updates ws-server)
- [ ] Verify topic exists: `rpk topic list`
- [ ] Verify ws-server logs show partition assignment
- [ ] Deploy publisher with new topic support
- [ ] Monitor consumer lag: `rpk group describe sukko-consumer`
```

### Removing Topic Checklist

```markdown
- [ ] Announce deprecation to team
- [ ] Stop publisher from writing to topic
- [ ] Verify lag is 0: `rpk group describe sukko-consumer`
- [ ] Remove from `ws/internal/kafka/config.go`:
    - [ ] Delete constant
    - [ ] Remove from `AllTopics()`
    - [ ] Remove from `TopicToEventType()`
- [ ] Remove from `values.yaml` → `ws-server.kafka.topics`
- [ ] Update AsyncAPI spec if applicable
- [ ] Commit and push changes
- [ ] Deploy Helm chart
- [ ] Optionally delete topic: `rpk topic delete sukko.deprecated`
- [ ] Update documentation
```

---

## Configuration Reference

### Consumer Settings (consumer.go)

| Setting | Value | Purpose |
|---------|-------|---------|
| `ConsumeResetOffset` | `AtEnd()` | Start from latest offset (new consumers skip history) |
| `SessionTimeout` | 30s | Time before consumer considered dead |
| `RebalanceTimeout` | 60s | Max time for rebalance to complete |
| `FetchMaxWait` | 500ms | Max time to wait for messages |

### Topic Settings (values.yaml)

| Setting | Default | Purpose |
|---------|---------|---------|
| `partitions` | 12 | Number of partitions per topic |
| `replicationFactor` | 1 (local), 3 (prod) | Copies of data for durability |

---

## Troubleshooting

### ws-server not receiving messages from new topic

1. **Check topic exists:**
   ```bash
   rpk topic list | grep topicname
   ```

2. **Check consumer group subscription:**
   ```bash
   rpk group describe sukko-consumer
   ```

3. **Check ws-server logs:**
   ```bash
   kubectl logs -l app.kubernetes.io/name=ws-server | grep -i partition
   ```

4. **Verify topic is in code:**
   - Check `AllTopics()` includes the topic
   - Check `TopicToEventType()` has a case for it

### High consumer lag after adding topic

1. **Check message rate:**
   ```bash
   rpk topic consume sukko.newtopic --offset end -f '%t %p %o\n'
   ```

2. **Check ws-server resource usage:**
   ```bash
   kubectl top pods -l app.kubernetes.io/name=ws-server
   ```

3. **Check for CPU throttling (triggers backpressure):**
   ```bash
   kubectl logs -l app.kubernetes.io/name=ws-server | grep "CPU emergency brake"
   ```
