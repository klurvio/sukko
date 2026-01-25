# WebSocket Client Guide

This guide explains how to connect to the Odin WebSocket server, subscribe to channels, and handle messages.

## Table of Contents

- [Quick Start](#quick-start)
- [Connection](#connection)
- [Authentication](#authentication)
- [Multi-Tenant Architecture](#multi-tenant-architecture)
- [Subscribing to Channels](#subscribing-to-channels)
- [Message Format](#message-format)
- [Available Channels](#available-channels)
- [Heartbeat](#heartbeat)
- [Reconnection & Replay](#reconnection--replay)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)
- [Complete Example](#complete-example)

---

## Quick Start

```javascript
// 1. Connect with JWT token
const ws = new WebSocket('wss://ws.example.com/ws?token=YOUR_JWT_TOKEN');

// 2. Subscribe to channels
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: 'subscribe',
    data: { channels: ['BTC.trade', 'ETH.trade'] }
  }));
};

// 3. Receive messages
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'message') {
    console.log(`[${msg.channel}] seq=${msg.seq}`, msg.data);
  }
};
```

---

## Connection

### WebSocket URL

```
wss://ws.example.com/ws?token=YOUR_JWT_TOKEN
```

Or pass the token via header:

```
Authorization: Bearer YOUR_JWT_TOKEN
```

### Connection Flow

```
Client                    Gateway                   Server
  │                          │                         │
  │──── WSS + JWT ──────────►│                         │
  │                          │── Validate JWT          │
  │                          │── Check permissions     │
  │                          │──── WS (internal) ─────►│
  │◄─── Connection Open ─────│◄────────────────────────│
  │                          │                         │
```

---

## Authentication

**All connections require a valid JWT token.** No anonymous access.

### JWT Token Structure

```json
{
  "sub": "user-12345",
  "exp": 1699901234,
  "iat": 1699814834,
  "tenant_id": "my-company",
  "groups": ["traders", "premium"]
}
```

| Field | Description |
|-------|-------------|
| `sub` | User/application identifier (principal) |
| `exp` | Expiration timestamp |
| `iat` | Issued at timestamp |
| `tenant_id` | Organization identifier |
| `groups` | Group memberships (for group-scoped channels) |

### Authentication Errors

| HTTP Status | Meaning |
|-------------|---------|
| 401 | No token provided |
| 401 | Invalid or expired token |
| 403 | Token valid but lacks permission |

---

## Multi-Tenant Architecture

Odin WebSocket uses a multi-tenant architecture where each organization (tenant) has isolated access.

### How It Works

1. **Your organization** generates JWT tokens signed with your private key
2. **Odin Gateway** validates tokens using your registered public key
3. **Tenant isolation** ensures you only access your organization's data

```
Your Application                       Odin Gateway
     │                                      │
     │  1. Sign JWT with                    │
     │     your private key                 │
     │                                      │
     │  2. Connect with JWT                 │
     │ ─────────────────────────────────►   │
     │                                      │  3. Validate JWT with
     │                                      │     your public key
     │                                      │
     │                                      │  4. Extract tenant_id
     │                                      │
     │  5. Access granted to                │
     │ ◄─────────────────────────────────   │     your tenant's data only
     │                                      │
```

### JWT Token Requirements

Your JWT tokens must include:

| Claim | Required | Description |
|-------|----------|-------------|
| `tenant_id` | Yes | Your organization identifier |
| `sub` | Yes | User/application identifier |
| `exp` | Yes | Token expiration timestamp |
| `kid` | Yes (header) | Key ID of your signing key |

**Example JWT Payload:**
```json
{
  "sub": "user-12345",
  "tenant_id": "acme-corp",
  "exp": 1699901234,
  "iat": 1699814834,
  "groups": ["traders", "premium"],
  "roles": ["user"]
}
```

**JWT Header with Key ID:**
```json
{
  "alg": "ES256",
  "typ": "JWT",
  "kid": "acme-prod-2026"
}
```

### Supported Signing Algorithms

| Algorithm | Type | Recommended |
|-----------|------|-------------|
| ES256 | ECDSA P-256 | Yes (smaller, faster) |
| RS256 | RSA 2048-bit | Yes |
| EdDSA | Ed25519 | Yes (fastest) |

### Tenant Isolation

Your tenant can only access:
- Channels prefixed with your tenant ID (automatic)
- Topics in your tenant's namespace
- Your own users' scoped channels

**Example - Channel Mapping:**

| You Subscribe To | Server Maps To |
|-----------------|----------------|
| `BTC.trade` | `acme-corp.BTC.trade` |
| `ETH.liquidity` | `acme-corp.ETH.liquidity` |
| `balances.user123` | `acme-corp.user123.balances` |

You never see the tenant prefix - it's added automatically based on your JWT.

### Key Registration

To use Odin WebSocket:

1. **Generate a key pair** (ES256 recommended):
   ```bash
   openssl ecparam -name prime256v1 -genkey -noout -out private.pem
   openssl ec -in private.pem -pubout -out public.pem
   ```

2. **Register your public key** with Odin (via provisioning API or admin portal)

3. **Sign tokens** with your private key (keep this secret!)

4. **Connect** with signed JWTs

### Multi-Tenant Errors

| Error Code | Meaning |
|------------|---------|
| `KEY_NOT_FOUND` | Your key ID is not registered |
| `KEY_REVOKED` | Your key has been revoked |
| `TENANT_MISMATCH` | Attempting to access another tenant's resources |
| `MISSING_TENANT` | JWT missing required `tenant_id` claim |

---

## Subscribing to Channels

### Subscribe

```javascript
ws.send(JSON.stringify({
  type: 'subscribe',
  data: {
    channels: ['BTC.trade', 'ETH.trade', 'all.trade']
  }
}));
```

**Server Response:**
```json
{
  "type": "subscription_ack",
  "subscribed": ["BTC.trade", "ETH.trade", "all.trade"],
  "count": 3
}
```

### Unsubscribe

```javascript
ws.send(JSON.stringify({
  type: 'unsubscribe',
  data: {
    channels: ['BTC.trade']
  }
}));
```

**Server Response:**
```json
{
  "type": "unsubscription_ack",
  "unsubscribed": ["BTC.trade"],
  "count": 1
}
```

---

## Message Format

All messages include a `type` field for consistent routing. This follows industry standards (Coinbase, Binance, Pusher).

### Data Messages (from subscribed channels)

Data messages have `type: "message"` and a `channel` field:

```json
{
  "type": "message",
  "seq": 1234,
  "ts": 1699901234567,
  "channel": "BTC.trade",
  "data": {
    "token": "BTC",
    "price": "45000.50",
    "volume": "1234567"
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"message"` for data messages |
| `seq` | int64 | Sequence number (monotonically increasing per connection) |
| `ts` | int64 | Server timestamp in Unix milliseconds |
| `channel` | string | Channel this message is from (e.g., "BTC.trade", "all.trade") |
| `data` | object | Actual payload |

### Control Messages

Control messages use other `type` values:

| Type | Description |
|------|-------------|
| `message` | **Data from subscribed channel** (has `channel`, `seq`, `ts`, `data`) |
| `subscription_ack` | Subscription confirmed |
| `unsubscription_ack` | Unsubscription confirmed |
| `pong` | Heartbeat response |
| `reconnect_ack` | Reconnection completed |
| `reconnect_error` | Reconnection failed |
| `publish_ack` | Publish to Kafka confirmed |
| `publish_error` | Publish to Kafka failed |
| `error` | General error response |

---

## Available Channels

### Channel Format

```
{identifier}.{channel}
{identifier}.{channel}.{scope}
```

### Public Channels

Anyone with a valid token can subscribe.

| Pattern | Example | Description |
|---------|---------|-------------|
| `{token}.trade` | `BTC.trade` | Trade events for a token |
| `{token}.liquidity` | `ETH.liquidity` | Liquidity pool changes |
| `{token}.metadata` | `SOL.metadata` | Token information updates |
| `{token}.analytics` | `BTC.analytics` | Statistical data |
| `{token}.creation` | `*.creation` | New token creation events |
| `{token}.social` | `DOGE.social` | Social/community updates |
| `all.trade` | `all.trade` | ALL trade events (aggregate) |
| `all.liquidity` | `all.liquidity` | ALL liquidity events |

### User-Scoped Channels

Only the user matching `JWT.sub` can subscribe.

| Pattern | Example | Description |
|---------|---------|-------------|
| `{token}.balances.{principal}` | `BTC.balances.user123` | User's token balance |
| `{token}.trade.{principal}` | `ETH.trade.user123` | User's trades |
| `notifications.{principal}` | `notifications.user123` | User notifications |

### Group-Scoped Channels

Only users with `group_id` in `JWT.groups` can subscribe.

| Pattern | Example | Description |
|---------|---------|-------------|
| `{token}.community.{group_id}` | `BTC.community.traders` | Group discussions |
| `community.{group_id}` | `community.premium` | General group channel |
| `social.{group_id}` | `social.vip` | Group social feed |

### Aggregate Channels

Subscribe to ALL events of a type:

```javascript
// Instead of subscribing to each token individually:
ws.send(JSON.stringify({
  type: 'subscribe',
  data: { channels: ['BTC.trade', 'ETH.trade', 'SOL.trade', ...] }
}));

// Subscribe to the aggregate channel:
ws.send(JSON.stringify({
  type: 'subscribe',
  data: { channels: ['all.trade'] }
}));
```

---

## Heartbeat

Send periodic heartbeats to keep the connection alive:

```javascript
// Send heartbeat every 30 seconds
setInterval(() => {
  ws.send(JSON.stringify({ type: 'heartbeat' }));
}, 30000);
```

**Server Response:**
```json
{
  "type": "pong",
  "ts": 1699901234567
}
```

---

## Reconnection & Replay

When reconnecting after a disconnect, you can recover missed messages.

### Track Last Received Offset

```javascript
let lastOffsets = {};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  // Track offset from message metadata (if provided)
  if (msg.offset && msg.topic) {
    lastOffsets[msg.topic] = msg.offset;
  }

  // Process message...
};
```

### Reconnect with Replay

```javascript
function reconnect() {
  ws = new WebSocket('wss://ws.example.com/ws?token=YOUR_JWT_TOKEN');

  ws.onopen = () => {
    // Request replay of missed messages
    ws.send(JSON.stringify({
      type: 'reconnect',
      data: {
        client_id: 'my-client-12345',
        last_offset: lastOffsets
      }
    }));

    // Re-subscribe to channels
    ws.send(JSON.stringify({
      type: 'subscribe',
      data: { channels: ['BTC.trade', 'ETH.trade'] }
    }));
  };
}
```

### Replay Response

**Success:**
```json
{
  "type": "reconnect_ack",
  "status": "completed",
  "messages_replayed": 42,
  "message": "Replayed 42 missed messages"
}
```

**Error:**
```json
{
  "type": "reconnect_error",
  "message": "Failed to replay messages: [error details]"
}
```

### Replayed Messages

Replayed messages have the same format as regular data messages, with `type: "message"`:

```json
{
  "type": "message",
  "seq": 1,
  "ts": 1699901200000,
  "channel": "BTC.trade",
  "data": { "token": "BTC", "price": "44500.00" }
}
```

---

## Error Handling

### WebSocket Close Codes

| Code | Name | Meaning | Action |
|------|------|---------|--------|
| 1000 | Normal | Clean disconnect | Don't retry |
| 1001 | Going Away | Server shutdown | Retry after delay |
| 1008 | Policy Violation | Client too slow | Check network, retry |
| 1011 | Internal Error | Backend unavailable | Retry with backoff |
| 1012 | Service Restart | Server overloaded | Retry quickly |

### Error Messages

```json
{
  "type": "error",
  "code": "RATE_LIMIT_EXCEEDED",
  "message": "Too many messages, please slow down (limit: 10/sec)"
}
```

### Rate Limits

| Limit | Value |
|-------|-------|
| Messages per second | 10 (sustained) |
| Burst allowance | 100 messages |

---

## Best Practices

### 1. Exponential Backoff for Reconnection

```javascript
function reconnectWithBackoff(attempt = 0) {
  const maxAttempts = 10;
  const baseDelay = 1000;
  const maxDelay = 60000;

  if (attempt >= maxAttempts) {
    console.error('Max reconnection attempts reached');
    return;
  }

  const delay = Math.min(baseDelay * Math.pow(2, attempt), maxDelay);
  const jitter = Math.random() * 1000;

  setTimeout(() => {
    connect().catch(() => reconnectWithBackoff(attempt + 1));
  }, delay + jitter);
}
```

### 2. Subscribe Only to What You Need

```javascript
// Bad: Subscribe to everything
ws.send(JSON.stringify({
  type: 'subscribe',
  data: { channels: ['all.trade', 'all.liquidity', 'all.metadata', ...] }
}));

// Good: Subscribe only to what you display
ws.send(JSON.stringify({
  type: 'subscribe',
  data: { channels: ['BTC.trade', 'ETH.trade'] }
}));
```

### 3. Handle Messages Efficiently

```javascript
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  // All messages have 'type' - route by type first
  switch (msg.type) {
    case 'message':
      // Data message - route by channel
      switch (msg.channel) {
        case 'BTC.trade':
          handleBTCTrade(msg.data);
          break;
        case 'all.trade':
          handleAllTrades(msg.data);
          break;
        default:
          handleGenericData(msg.channel, msg.data);
      }
      break;

    case 'subscription_ack':
      console.log('Subscribed to', msg.subscribed);
      break;

    case 'publish_ack':
      console.log('Message published to', msg.channel);
      break;

    case 'publish_error':
      console.error('Publish failed:', msg.code, msg.message);
      break;

    case 'error':
      handleError(msg);
      break;

    case 'pong':
      // Heartbeat acknowledged
      break;
  }
};
```

### 4. Use Sequence Numbers for Ordering

```javascript
let lastSeq = 0;

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);

  if (msg.seq <= lastSeq) {
    console.warn('Out of order or duplicate message', msg.seq);
    return;
  }

  lastSeq = msg.seq;
  processMessage(msg);
};
```

### 5. Graceful Shutdown

```javascript
window.addEventListener('beforeunload', () => {
  ws.close(1000, 'Client closing');
});
```

---

## Complete Example

```javascript
class OdinWebSocketClient {
  constructor(url, token) {
    this.url = url;
    this.token = token;
    this.ws = null;
    this.subscriptions = new Set();
    this.lastOffsets = {};
    this.lastSeq = 0;
    this.retryCount = 0;
    this.maxRetries = 10;
    this.heartbeatInterval = null;
  }

  connect() {
    return new Promise((resolve, reject) => {
      const wsUrl = `${this.url}?token=${this.token}`;
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        console.log('Connected');
        this.retryCount = 0;
        this.startHeartbeat();

        // Re-subscribe if reconnecting
        if (this.subscriptions.size > 0) {
          this.subscribe([...this.subscriptions]);
        }

        resolve();
      };

      this.ws.onclose = (event) => {
        this.stopHeartbeat();
        this.handleClose(event);
      };

      this.ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        reject(error);
      };

      this.ws.onmessage = (event) => {
        this.handleMessage(JSON.parse(event.data));
      };
    });
  }

  handleMessage(msg) {
    // Check sequence
    if (msg.seq && msg.seq <= this.lastSeq) {
      console.warn('Out of order message:', msg.seq);
      return;
    }
    if (msg.seq) this.lastSeq = msg.seq;

    // Track offsets for replay
    if (msg.offset && msg.topic) {
      this.lastOffsets[msg.topic] = msg.offset;
    }

    // Emit to listeners
    this.onMessage(msg);
  }

  onMessage(msg) {
    // Override this method to handle messages
    // All messages have 'type' - data messages have type: "message"
    if (msg.type === 'message') {
      console.log(`[${msg.channel}]`, msg.data);
    } else {
      console.log(`[${msg.type}]`, msg);
    }
  }

  subscribe(channels) {
    channels.forEach(ch => this.subscriptions.add(ch));
    this.send({
      type: 'subscribe',
      data: { channels }
    });
  }

  unsubscribe(channels) {
    channels.forEach(ch => this.subscriptions.delete(ch));
    this.send({
      type: 'unsubscribe',
      data: { channels }
    });
  }

  send(data) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  startHeartbeat() {
    this.heartbeatInterval = setInterval(() => {
      this.send({ type: 'heartbeat' });
    }, 30000);
  }

  stopHeartbeat() {
    if (this.heartbeatInterval) {
      clearInterval(this.heartbeatInterval);
      this.heartbeatInterval = null;
    }
  }

  handleClose(event) {
    console.log(`Connection closed: ${event.code} ${event.reason}`);

    if (event.code === 1000) {
      // Normal closure, don't retry
      return;
    }

    this.reconnectWithBackoff();
  }

  reconnectWithBackoff() {
    if (this.retryCount >= this.maxRetries) {
      console.error('Max reconnection attempts reached');
      return;
    }

    const baseDelay = this.getBaseDelay();
    const delay = Math.min(baseDelay * Math.pow(2, this.retryCount), 60000);
    const jitter = Math.random() * 1000;

    console.log(`Reconnecting in ${Math.round(delay + jitter)}ms...`);
    this.retryCount++;

    setTimeout(() => {
      this.connect().catch(() => {});
    }, delay + jitter);
  }

  getBaseDelay() {
    // Adjust base delay based on close reason
    return 1000;
  }

  close() {
    this.stopHeartbeat();
    if (this.ws) {
      this.ws.close(1000, 'Client closing');
    }
  }
}

// Usage
const client = new OdinWebSocketClient(
  'wss://ws.example.com/ws',
  'YOUR_JWT_TOKEN'
);

client.onMessage = (msg) => {
  // All messages have 'type' - route by type
  switch (msg.type) {
    case 'message':
      // Data message - route by channel
      console.log(`Data from ${msg.channel}:`, msg.data);
      break;
    case 'subscription_ack':
      console.log('Subscribed:', msg.subscribed);
      break;
    case 'publish_ack':
      console.log('Published to:', msg.channel);
      break;
    case 'publish_error':
      console.error('Publish failed:', msg.code, msg.message);
      break;
    case 'error':
      console.error('Error:', msg.message);
      break;
  }
};

client.connect().then(() => {
  client.subscribe(['BTC.trade', 'ETH.trade', 'all.liquidity']);
});
```

---

## Related Documentation

- [Multi-Tenant Authentication](./architecture/MULTI_TENANT_AUTH.md) - Full authentication architecture details
- [Subject Patterns](./architecture/SUBJECT_PATTERNS.md) - Detailed channel naming conventions
- [Kafka Replay Protocol](./architecture/KAFKA_REPLAY_PROTOCOL.md) - How message replay works
- [API Rejection Responses](./development/API_REJECTION_RESPONSES.md) - Error codes and retry strategies
