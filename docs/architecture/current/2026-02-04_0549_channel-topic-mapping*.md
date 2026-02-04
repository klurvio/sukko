# Channel-to-Topic Mapping Architecture

**Status:** ✅ Implemented

## Overview

This document explains the relationship between **channels** (WebSocket routing) and **topics** (Kafka messaging) in the Odin WebSocket infrastructure.

---

## Core Concepts

### Topics: Coarse-Grained Message Streams

**Kafka topics** are the physical message streams that store and transport events:

```
{namespace}.{tenant_id}.{category}

Examples:
- prod.acme.trade        → All trade events for tenant "acme"
- prod.acme.liquidity    → All liquidity events for tenant "acme"
- dev.globex.balances    → All balance events for tenant "globex"
```

**Key characteristics:**
- **Provisioned** via the provisioning service (not hardcoded)
- **Tenant-isolated** - each tenant has their own topics
- **Category-based** - one topic per event category per tenant
- **Persistent** - stored in Kafka/Redpanda with configured retention

### Channels: Fine-Grained Routing Keys

**Channels** provide granular routing **within** a topic:

```
{tenant}.{identifier}.{category}

Examples:
- acme.BTC.trade         → BTC trade events (within acme's trade topic)
- acme.ETH.trade         → ETH trade events (within acme's trade topic)
- acme.user123.balances  → User-specific balance updates
```

**Key characteristics:**
- **Dynamic** - no provisioning required
- **Kafka message key** - determines partition assignment
- **WebSocket routing** - used for subscription matching
- **Ordering guarantee** - same channel = same partition = ordered

---

## Why This Design?

### Problem: Topic Explosion

If we created a topic for every token/asset:

```
prod.acme.trade.BTC    ← Topic for BTC trades
prod.acme.trade.ETH    ← Topic for ETH trades
prod.acme.trade.SOL    ← Topic for SOL trades
... (thousands more)
```

**Issues:**
- Kafka has limits on number of topics/partitions
- Operational overhead (monitoring, retention policies)
- Topic provisioning latency for new assets
- Resource waste for low-volume assets

### Solution: Channels as Dynamic Subtopics

Instead, we use a single topic per category with channels for routing:

```
Topic: prod.acme.trade
├── Channel: acme.BTC.trade  (Kafka Key)
├── Channel: acme.ETH.trade  (Kafka Key)
├── Channel: acme.SOL.trade  (Kafka Key)
└── ... (unlimited channels, no provisioning)
```

**Benefits:**
- **Scalable** - unlimited channels without topic overhead
- **Dynamic** - new assets work immediately
- **Ordered** - messages with same channel are ordered (same partition)
- **Efficient** - Kafka optimized for fewer topics with more keys

---

## Channel-to-Topic Mapping Flow

### Client Publish Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│ 1. CLIENT SENDS                                                      │
│    {"type": "publish", "data": {"channel": "BTC.trade", ...}}       │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 2. WS-GATEWAY (has JWT with tenant_id: "acme")                      │
│                                                                      │
│    ChannelMapper.MapToInternal("BTC.trade", claims)                 │
│    → "acme.BTC.trade"                                               │
│                                                                      │
│    Forward: {"type": "publish", "data": {"channel": "acme.BTC.trade"}}
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 3. WS-SERVER                                                         │
│                                                                      │
│    parseChannel("acme.BTC.trade")                                   │
│    → tenant: "acme", category: "trade"                              │
│                                                                      │
│    buildTopic(namespace: "prod", tenant: "acme", category: "trade") │
│    → "prod.acme.trade"                                              │
│                                                                      │
│    Publish to Kafka:                                                 │
│    - Topic: "prod.acme.trade"                                       │
│    - Key: "acme.BTC.trade" (channel)                                │
│    - Value: message data                                            │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 4. KAFKA                                                             │
│                                                                      │
│    Topic: prod.acme.trade                                           │
│    ├── Partition 0: [..., acme.BTC.trade, acme.BTC.trade, ...]     │
│    ├── Partition 1: [..., acme.ETH.trade, acme.ETH.trade, ...]     │
│    └── Partition 2: [..., acme.SOL.trade, ...]                      │
│                                                                      │
│    (Partitioning by key ensures channel ordering)                   │
└─────────────────────────────────────────────────────────────────────┘
```

### Consumer Broadcast Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│ 5. WS-SERVER CONSUMER                                                │
│                                                                      │
│    Reads from: prod.acme.trade                                      │
│    Extracts:                                                        │
│    - Key (channel): "acme.BTC.trade"                                │
│    - Value: message data                                            │
│                                                                      │
│    Broadcasts to subscribers of "acme.BTC.trade"                    │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 6. WEBSOCKET CLIENTS                                                 │
│                                                                      │
│    Client A (subscribed to "acme.BTC.trade") → Receives message     │
│    Client B (subscribed to "acme.ETH.trade") → Does not receive     │
│    Client C (subscribed to "acme.*.trade")   → Receives (wildcard)  │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Mapping Rules

### Channel Format

```
{tenant}.{identifier}.{category}

Parts:
- tenant:     Tenant ID (from JWT, added by gateway)
- identifier: Asset/entity identifier (e.g., BTC, user123, group456)
- category:   Event category (determines Kafka topic)

Minimum: 3 parts (tenant.identifier.category)
Maximum: Unlimited (tenant.a.b.c.category)
```

### Topic Format

```
{namespace}.{tenant}.{category}

Parts:
- namespace: Environment (prod, staging, dev, local)
- tenant:    Tenant ID (extracted from channel)
- category:  Event category (extracted from channel)
```

### Extraction Rules

| Channel | Tenant | Category | Topic (prod) |
|---------|--------|----------|--------------|
| `acme.BTC.trade` | `acme` | `trade` | `prod.acme.trade` |
| `acme.ETH.liquidity` | `acme` | `liquidity` | `prod.acme.liquidity` |
| `acme.user123.balances` | `acme` | `balances` | `prod.acme.balances` |
| `acme.group.chat.community` | `acme` | `community` | `prod.acme.community` |

**Rule:**
- Tenant = first segment
- Category = last segment
- Everything in between = identifier (part of the Kafka key)

---

## Valid Categories

Categories correspond to provisioned topics. Standard categories:

| Category | Description | Example Channels |
|----------|-------------|-----------------|
| `trade` | Trading events | `acme.BTC.trade`, `acme.ETH.trade` |
| `liquidity` | Liquidity pool events | `acme.POOL1.liquidity` |
| `balances` | Balance updates | `acme.user123.balances` |
| `metadata` | Token metadata | `acme.BTC.metadata` |
| `social` | Social events | `acme.user123.social` |
| `community` | Community events | `acme.group456.community` |
| `creation` | Token creation | `acme.newtoken.creation` |
| `analytics` | Analytics events | `acme.BTC.analytics` |

**Note:** Categories are not hardcoded. Each tenant has their own provisioned topics with chosen categories. The above are common patterns.

---

## Topic Provisioning

### Topics Must Be Provisioned

Topics are **not created dynamically**. They must be provisioned via the provisioning service:

```bash
# Create topics for a new tenant
POST /api/v1/tenants
{
  "id": "acme",
  "name": "Acme Corp",
  "categories": ["trade", "liquidity", "balances"]
}

# Creates:
# - prod.acme.trade
# - prod.acme.liquidity
# - prod.acme.balances
```

### Channels Are Dynamic

Channels require **no provisioning**. Any valid channel format works:

```javascript
// All of these work immediately (if "trade" topic is provisioned)
client.publish("BTC.trade", data);    // → acme.BTC.trade
client.publish("ETH.trade", data);    // → acme.ETH.trade
client.publish("NEWTOKEN.trade", data); // → acme.NEWTOKEN.trade
```

### Why Not Hardcode Topics?

The `allTopicBases` in `config.go` was used for legacy "odin" infrastructure topics. For multi-tenant deployments:

1. **Each tenant chooses their categories** - not all tenants need all categories
2. **Topics are stored in database** - queried via `TenantRegistry`
3. **Provisioning service manages lifecycle** - creation, deletion, quotas
4. **ACLs are tenant-scoped** - `User:acme` can only access `*.acme.*`

---

## Security Model

### Tenant Isolation

1. **Gateway adds tenant from JWT** - clients cannot spoof tenant
2. **Channel validation** - server verifies tenant in channel matches JWT
3. **Kafka ACLs** - `User:acme` can only publish/consume `*.acme.*`
4. **No cross-tenant access** - unless explicitly granted via roles

### Channel Access Control

```go
// Gateway validates before forwarding
if !channelMapper.ValidateChannelAccess(claims, internalChannel, crossTenantRoles) {
    return error("forbidden")
}
```

---

## Performance Characteristics

### Partition Assignment

Same channel → same partition → ordering guaranteed

```
Channel: acme.BTC.trade
  │
  ├── Message 1 (offset 100) ─┐
  ├── Message 2 (offset 101)  │── Same partition, ordered
  └── Message 3 (offset 102) ─┘
```

### Throughput

- **Topics** limit: Kafka recommendation ~10K topics per cluster
- **Channels** limit: Unlimited (just Kafka keys)
- **Partitions per topic**: Configurable (default: 12)

---

## Examples

### Trading Platform

```
Client subscribes: ["BTC.trade", "ETH.trade", "*.liquidity"]

Internal subscriptions (after gateway mapping):
- acme.BTC.trade      → Topic: prod.acme.trade
- acme.ETH.trade      → Topic: prod.acme.trade
- acme.*.liquidity    → Topic: prod.acme.liquidity (wildcard)
```

### Chat Application

```
Client publishes to: "room123.community"
Internal channel: acme.room123.community
Topic: prod.acme.community
Key: acme.room123.community

All users subscribed to "room123.community" receive the message.
```

### User Notifications

```
Channel: user456.balances
Internal: acme.user456.balances
Topic: prod.acme.balances

Only user456 (subscribed to their own channel) receives balance updates.
```

---

## Summary

| Concept | Granularity | Provisioned | Used For |
|---------|-------------|-------------|----------|
| **Topic** | Coarse (category) | Yes | Kafka storage, ACLs |
| **Channel** | Fine (entity) | No | Routing, ordering, subscriptions |

**Key insight:** Topics are infrastructure (managed), channels are application logic (dynamic).
