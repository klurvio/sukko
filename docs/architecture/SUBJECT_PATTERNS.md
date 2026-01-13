# Subject Pattern Convention

## Overview

This document defines the contract between publishers (CDC, etc.) and the WebSocket broadcast system.

**Key Principle:** The Kafka message Key IS the broadcast subject IS the client channel.

## Message Flow

```
Publisher → Kafka (Key = subject) → ws-server → BroadcastBus → Clients
```

The ws-server acts as a "dumb pipe" - it does not interpret or transform the subject. Whatever Key the publisher sets becomes the channel that clients subscribe to.

## Subject Format

| Category | Pattern | Example | Gateway Validation |
|----------|---------|---------|-------------------|
| Public | `{identifier}.{channel}` | `BTC.trade` | Valid JWT (any user) |
| User-scoped | `{identifier}.{channel}.{principal}` | `BTC.balances.user123` | JWT.sub == principal |
| Group-scoped | `{identifier}.{channel}.{group_id}` | `23sz.community.crypto-traders` | User is member of group |

### Format Rules

- **Dot-separated**: Use `.` as the separator between parts
- **Minimum 2 parts**: Subject must have at least 2 dot-separated segments
- **No empty parts**: Each segment must be non-empty

## Gateway Patterns

The gateway validates client subscriptions against allowed patterns. Patterns use wildcards:

| Category | Patterns |
|----------|----------|
| Public | `*.trade`, `*.liquidity`, `*.metadata`, `*.analytics`, `*.creation`, `*.social` |
| User-scoped | `*.balances.{principal}`, `*.trade.{principal}`, `balances.{principal}`, `notifications.{principal}` |
| Group-scoped | `*.community.{group_id}`, `community.{group_id}`, `social.{group_id}` |

**Pattern Matching:**
- `*` matches any single segment
- `{principal}` is replaced with the JWT subject claim
- `{group_id}` is replaced with group IDs the user belongs to

## Publisher Examples

### Public Channel

```
Topic: odin.main.trade.refined
Key:   "BTC.trade"
Value: {"price": "50000.00", "volume": "1.5", "timestamp": 1705123456789}
```

Result: All clients subscribed to `BTC.trade` receive the message.

### User-scoped Channel

```
Topic: odin.main.balances.refined
Key:   "BTC.balances.user123"
Value: {"available": "10.5", "locked": "2.0", "total": "12.5"}
```

Result: Only the client with `JWT.sub = "user123"` can subscribe to and receive messages on this channel.

### Group-scoped Channel

```
Topic: odin.main.community.refined
Key:   "23sz.community.crypto-traders"
Value: {"message": "Hello!", "author": "user456", "timestamp": 1705123456789}
```

Result: Only clients who are members of the "crypto-traders" group can subscribe to and receive messages.

## Rules for Publishers

1. **Key = Full Subject**: Set the Kafka message Key to the complete broadcast subject
2. **Value is Opaque**: ws-server passes the Value through as-is without parsing
3. **Match Gateway Patterns**: The subject must match at least one gateway pattern, otherwise clients cannot subscribe
4. **Consistent Format**: Use the same subject format for related messages

## Adding New Channel Types

To add a new channel type:

1. **Define the subject format** (e.g., `{token}.newchannel` or `{token}.newchannel.{scope}`)
2. **Add gateway pattern** to the appropriate category via environment variable:
   - `GATEWAY_PUBLIC_PATTERNS` for public channels
   - `GATEWAY_USER_SCOPED_PATTERNS` for user-scoped channels
   - `GATEWAY_GROUP_SCOPED_PATTERNS` for group-scoped channels
3. **Update publisher** to set the Kafka Key in the new format
4. **Clients can subscribe** using the new channel format

## Examples by Use Case

| Use Case | Subject | Gateway Pattern |
|----------|---------|-----------------|
| Token trades | `BTC.trade` | `*.trade` |
| Token liquidity | `BTC-USDC.liquidity` | `*.liquidity` |
| User balances | `BTC.balances.user123` | `*.balances.{principal}` |
| User notifications | `notifications.user123` | `notifications.{principal}` |
| Community chat | `23sz.community.crypto-traders` | `*.community.{group_id}` |
| Social feed | `social.group456` | `social.{group_id}` |

## Aggregate Channels

ws-server uses **exact match** subscriptions only - no wildcard matching at runtime. To support "subscribe to all" use cases, publishers should emit to **aggregate channels** in addition to specific channels.

### Pattern

```
all.{channel}
```

### How It Works

Publishers emit the same message to both:
1. **Specific channel**: `BTC.trade` - for clients wanting just BTC
2. **Aggregate channel**: `all.trade` - for clients wanting ALL trades

```
CDC publishes trade event:
  → Key: "BTC.trade"     (specific)
  → Key: "all.trade"     (aggregate, same Value)
```

### Client Subscription Examples

| Use Case | Subscribe To |
|----------|--------------|
| Only BTC trades | `["BTC.trade"]` |
| BTC and ETH trades | `["BTC.trade", "ETH.trade"]` |
| ALL trades | `["all.trade"]` |
| ALL trades + specific liquidity | `["all.trade", "BTC.liquidity"]` |

### Publisher Implementation

When publishing a trade event, CDC sends TWO messages:

```
# Message 1: Specific channel
Topic: odin.main.trade.refined
Key:   "BTC.trade"
Value: {"token": "BTC", "price": "50000.00", ...}

# Message 2: Aggregate channel (same Value)
Topic: odin.main.trade.refined
Key:   "all.trade"
Value: {"token": "BTC", "price": "50000.00", ...}
```

### Aggregate Channel Conventions

| Channel Type | Specific | Aggregate |
|--------------|----------|-----------|
| Trades | `BTC.trade` | `all.trade` |
| Liquidity | `BTC.liquidity` | `all.liquidity` |
| Metadata | `BTC.metadata` | `all.metadata` |
| Analytics | `BTC.analytics` | `all.analytics` |
| Creation | `BTC.creation` | `all.creation` |
| Social | `BTC.social` | `all.social` |

### Gateway Configuration

Add aggregate patterns to `GATEWAY_PUBLIC_PATTERNS`:

```
GATEWAY_PUBLIC_PATTERNS=*.trade,*.liquidity,*.metadata,all.trade,all.liquidity,all.metadata
```

### Trade-offs

| Aspect | Impact |
|--------|--------|
| Kafka messages | 2x for events with aggregate channels |
| ws-server complexity | None (still exact match) |
| Client flexibility | High (choose specific or all) |
| Bandwidth | Clients get only what they subscribe to |

## Related Documentation

- [WS_GATEWAY.md](./WS_GATEWAY.md) - Gateway authentication and permission model
- [KAFKA_PARTITIONS_EXPLAINED.md](./KAFKA_PARTITIONS_EXPLAINED.md) - How Kafka partitioning works with subjects
