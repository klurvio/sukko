# Plan: Explicit Channels Refactor + AsyncAPI Documentation

**Status:** Implemented
**Date:** 2026-02-05
**Scope:** Switch to explicit tenant-prefixed channels, remove mapping layer, then create AsyncAPI 3.0 specs

---

## Summary

### Design Decision: Explicit Channels

Channels now always include the tenant prefix. Clients send `odin.BTC.trade`, not `BTC.trade`.

**Why:** The previous implicit approach (client sends `BTC.trade`, gateway prepends `odin.`, server strips `odin.` on output) created unnecessary complexity — a ChannelMapper component, prepend/strip on every message, nil guards throughout, and an ack consistency bug where acks didn't strip but broadcasts did. Industry platforms that use implicit channels (Ably, Pusher) do so because they silo infrastructure per tenant. Odin-ws shares a single server process across tenants, making explicit channels the simpler path — similar to Slack/Discord where the tenant (workspace/guild) is part of the channel identifier.

**What changes:**
- Clients include tenant in all channel names: `odin.BTC.trade`
- Gateway **validates** tenant prefix (instead of transforming)
- Server uses channels as-is (no stripping in broadcast or acks)
- `ChannelMapper` usage removed from server and gateway
- `crossTenantRoles` removed (admin-only feature with no current use case)

### AsyncAPI Documentation

Create AsyncAPI 3.0 specs documenting two interfaces:

1. **Client WebSocket API** — what external clients connect to (via ws-gateway)
2. **Redpanda/Kafka API** — how external services publish events that reach WebSocket clients

The specs will live in `ws/docs/asyncapi/` as YAML files.

---

## Output Files

```
ws/docs/asyncapi/
  client-ws.asyncapi.yaml      # Client-facing WebSocket API (+ internal appendix)
  redpanda.asyncapi.yaml       # External service -> Redpanda -> broadcast
```

Old AsyncAPI files in `ws/docs/asyncapi/` (committed to HEAD) will be removed and replaced by these two files.

---

## Prerequisite: Explicit Channels Refactor

Before writing the AsyncAPI specs, refactor the codebase to use explicit tenant-prefixed channels.

### Principle

Channels are explicit: the client always includes the tenant prefix. The system never prepends or strips tenant from channel names. The gateway validates that the tenant in the channel matches the authenticated tenant (or default tenant when auth is disabled).

### Server-side removals

| File | What to remove |
|------|---------------|
| `server/server.go:35-40` | `ChannelMapper` interface definition |
| `server/server.go:90` | `channelMapper` field on Server struct |
| `server/server.go:182-187` | channelMapper initialization block |
| `broadcast.go:119-122` | Tenant stripping block (`s.channelMapper.MapToClient()`) |
| `types/types.go:86-90` | `ChannelMapper any` on ServerConfig |
| `cmd/server/main.go:264` | `channelMapper` creation |
| `cmd/server/main.go:310` | Passing `ChannelMapper` to config |

After removal, broadcast uses the channel as-is. Acks already use the channel as-is (no code change needed — the "bug" becomes correct behavior).

### Gateway-side changes

**Remove (mapping):**

| File | What to remove |
|------|---------------|
| `proxy.go:41` | `channelMapper` field on Proxy |
| `proxy.go:63` | `ChannelMapper` on ProxyConfig |
| `proxy.go:100` | `channelMapper` assignment in NewProxy |
| `proxy.go:373-394` | `mapChannelToInternal()` and `mapChannelsToInternal()` methods |
| `proxy.go:356` | Mapping call in `interceptSubscribe` |
| `proxy.go:442` | Mapping call in `interceptPublish` |
| `proxy.go:377-379` | "Already prefixed" detection |
| `gateway.go:34` | `channelMapper` field on Gateway |
| `gateway.go:49` | `NewChannelMapper()` creation |
| `gateway.go:428` | Passing `ChannelMapper` to ProxyConfig |

**Add (validation + permission adaptation):**

Gateway validates that the tenant prefix in each channel matches the connection's tenant:

```go
// validateChannelTenant checks that the channel's tenant prefix matches the connection's tenant.
// Returns true if channel starts with "{tenantID}." — simple string prefix check.
func (p *Proxy) validateChannelTenant(channel string) bool {
    return strings.HasPrefix(channel, p.tenantID+".")
}

// stripTenantPrefix removes the tenant prefix from a channel for permission matching.
// "odin.BTC.trade" → "BTC.trade". Only used locally in the gateway for permission checks —
// the full tenant-prefixed channel is always forwarded to the server.
func (p *Proxy) stripTenantPrefix(channel string) string {
    return strings.TrimPrefix(channel, p.tenantID+".")
}
```

**`interceptSubscribe` flow (updated):**

1. Parse subscribe data
2. Validate tenant prefix on each channel — **filter out** channels with wrong prefix (silent drop, consistent with permission filtering behavior)
3. If auth enabled: strip tenant prefix locally, run `FilterChannels` on stripped names, then map allowed channels back to full tenant-prefixed form
4. Forward tenant-prefixed channels to server as-is

Permission patterns (`*.trade`, `balances.{principal}`) remain unchanged — they operate on the non-tenant portion. The gateway strips the tenant prefix **locally** just for the permission check, then forwards the original tenant-prefixed channel.

**`interceptPublish` flow (updated):**

1. Validate tenant prefix — return `forbidden` error if mismatch
2. Validate minimum 3 dot-separated parts (was 2, now uses `MinInternalChannelParts`)
3. Rate limit, size check (unchanged)
4. Forward to server as-is

**`unsubscribe` — no interception needed:**

Unsubscribe is not intercepted by the proxy (pass-through to server). Channels were already validated during subscribe, and unsubscribing from non-subscribed channels is harmless.

- When auth disabled: `p.tenantID` comes from `config.DefaultTenantID` (existing behavior)

### Remove crossTenantRoles

| File | What to remove |
|------|---------------|
| `proxy.go:43` | `crossTenantRoles` field on Proxy |
| `proxy.go:64` | `CrossTenantRoles` on ProxyConfig |
| `proxy.go:101` | `crossTenantRoles` assignment |
| `proxy.go:446` | `ValidateChannelAccess` call with crossTenantRoles |
| `gateway.go:429` | Passing `CrossTenantRoles` to ProxyConfig |
| `platform/gateway_config.go:96` | `CrossTenantRoles` config field |
| `auth/channel.go:156-175` | `ValidateChannelAccess` method (or remove crossTenantRoles param) |
| `auth/tenant.go:24-26` | `CrossTenantRoles` in TenantIsolationConfig |
| `auth/tenant.go:45` | Default cross-tenant roles |
| `auth/tenant.go:213-223` | Cross-tenant role check in TenantIsolator |
| `auth/topic.go:44-46` | `CrossTenantRoles` in TopicIsolationConfig |
| `auth/topic.go:66` | Default cross-tenant roles |
| `auth/topic.go:150-158` | Cross-tenant role check in TopicIsolator |

Access check simplifies to:

```
1. Channel tenant prefix == client tenant (JWT or DefaultTenantID)? → allow
2. Deny
```

### Channel format changes

| Before | After |
|--------|-------|
| Client sends `BTC.trade` | Client sends `odin.BTC.trade` |
| Gateway prepends → `odin.BTC.trade` | Gateway validates prefix, forwards as-is |
| Server strips → `BTC.trade` for client | Server sends `odin.BTC.trade` as-is |
| Min client channel parts: 2 | Min channel parts: 3 (everywhere) |
| Min internal channel parts: 3 | Same constant, one format |

### Constant update: `protocol/limits.go`

Remove `MinClientChannelParts` (was 2). Use `MinInternalChannelParts` (3) everywhere. Update `proxy.go:433` which currently checks `len(parts) < protocol.MinClientChannelParts` — change to `protocol.MinInternalChannelParts`.

### auth/channel.go — methods that become unused

| Method | Status |
|--------|--------|
| `MapToClient` | No callers — remove or keep for future use |
| `MapToInternalWithTenant` | No callers — remove or keep for future use |
| `MapToInternal` | No callers — remove or keep for future use |
| `ExtractTenant` | No callers — remove or keep for future use |
| `ValidateChannelAccess` | Simplified (remove crossTenantRoles param) or replaced by `validateChannelTenant` |
| `ParseChannel` | Still useful |
| `BuildInternalChannel` | May still be useful |

### Tests to update

- `proxy_test.go` — update channel values in test cases to include tenant prefix
- `broadcast_test.go` — remove tests for tenant stripping
- `channel_test.go` — update/remove mapping tests, update access validation tests
- `tenant_test.go` — remove cross-tenant role tests
- `topic_test.go` — remove cross-tenant role tests
- Any test referencing `mapChannelToInternal`, `mapChannelsToInternal`

---

## Phase 1: Client WebSocket API (`client-ws.asyncapi.yaml`)

The primary spec. Documents what an external client needs to connect, subscribe, publish, and receive messages.

### Server

```yaml
servers:
  gateway:
    host: "{host}:{port}"
    protocol: wss
    description: Odin WebSocket Gateway
    security:
      - $ref: '#/components/securitySchemes/bearerToken'
      - $ref: '#/components/securitySchemes/queryToken'
```

### Connection

- **URL**: `wss://{host}/ws?token={jwt}` or `wss://{host}/ws` with `Authorization: Bearer {jwt}`
- **Auth disabled mode**: no token required, all connections routed to default tenant
- Document health endpoint (`GET /health → {"status":"ok","service":"ws-gateway"}`) in `info.description` as a note (it's REST, not AsyncAPI)

### Connection Lifecycle

Document in `info.description` or a dedicated `x-connection-lifecycle` extension:

- **Admission control**: server may reject WebSocket upgrades under high CPU (>60%), memory pressure, goroutine limits, or per-tenant connection limits
- **Slow client disconnection**: if the server fails to deliver 3 consecutive messages to a client (send buffer full), it disconnects with close code 1008 (Policy Violation)
- **Graceful shutdown**: server sends close code 1001 (Going Away) to all clients before stopping
- **Heartbeat**: client sends `heartbeat`, server responds with `pong` containing timestamp

### Channels (client sends)

Document these as `send` operations from the client's perspective.

Channel format: `{tenant}.{identifier}.{category}` (minimum 3 dot-separated parts). The tenant prefix must match the authenticated tenant (or the default tenant when auth is disabled).

#### `subscribe`
```json
{
  "type": "subscribe",
  "data": { "channels": ["odin.BTC.trade", "odin.ETH.orderbook"] }
}
```
- When auth enabled: channels filtered by permission rules (see Channel Permissions below)
- Gateway validates tenant prefix, then forwards as-is

#### `unsubscribe`
```json
{
  "type": "unsubscribe",
  "data": { "channels": ["odin.BTC.trade"] }
}
```

#### `publish`
```json
{
  "type": "publish",
  "data": { "channel": "odin.BTC.trade", "data": { "price": 50000 } }
}
```
- Max payload: 64KB (`data` field)
- Rate limit: 10 msg/sec, burst 100 (per connection)
- Gateway validates tenant prefix matches authenticated tenant (JWT or DefaultTenantID)
- Routed to Redpanda topic `{namespace}.{tenant}.{category}`

#### `reconnect`
```json
{
  "type": "reconnect",
  "data": { "client_id": "abc123", "last_offset": {"local.odin.trade": 12345} }
}
```
- `client_id`: persistent client identifier for reconnection
- `last_offset`: map of Kafka topic to last received offset
- Replays missed messages from Kafka offsets
- Max 100 messages, 5s timeout

#### `heartbeat`
```json
{ "type": "heartbeat", "data": {} }
```

### Channels (client receives)

#### `message` (broadcast)
```json
{
  "type": "message",
  "seq": 1234,
  "ts": 1706000000000,
  "channel": "odin.BTC.trade",
  "data": { "price": 50000, "volume": 1.5 }
}
```
- `seq`: monotonic sequence number
- `ts`: Unix milliseconds
- `channel`: explicit tenant-prefixed format

#### `subscription_ack`
```json
{ "type": "subscription_ack", "subscribed": ["odin.BTC.trade"], "count": 5 }
```
- `subscribed`: channels as provided (with tenant prefix)
- `count`: total active subscription count for this connection

#### `unsubscription_ack`
```json
{ "type": "unsubscription_ack", "unsubscribed": ["odin.BTC.trade"], "count": 4 }
```
- `unsubscribed`: channels as provided (with tenant prefix)
- `count`: total active subscription count for this connection

#### `publish_ack`
```json
{ "type": "publish_ack", "channel": "odin.BTC.trade", "status": "accepted" }
```
- `channel`: as provided (with tenant prefix)
- `status`: always `"accepted"`

#### `publish_error`
```json
{
  "type": "publish_error",
  "code": "rate_limited",
  "message": "Publish rate limit exceeded"
}
```

Error codes to document:

| Code | Message | Cause |
|------|---------|-------|
| `not_available` | Publishing is not enabled on this server | Producer not configured |
| `invalid_request` | Invalid publish request format | Malformed JSON |
| `invalid_channel` | Channel must have format: tenant.identifier.category | < 3 dot-separated parts |
| `message_too_large` | Message exceeds maximum size limit | Payload > 64KB |
| `rate_limited` | Publish rate limit exceeded | Token bucket exhausted |
| `publish_failed` | Failed to publish message | Kafka write failure |
| `forbidden` | Not authorized to publish to this channel | Tenant prefix mismatch |
| `topic_not_provisioned` | Category is not provisioned for your tenant | Category not in tenant_categories |
| `service_unavailable` | Service temporarily unavailable, please retry | Circuit breaker open |

#### `reconnect_ack`
```json
{
  "type": "reconnect_ack",
  "status": "completed",
  "messages_replayed": 5,
  "message": "Replayed 5 missed messages"
}
```
- `status`: always `"completed"`
- `messages_replayed`: number of messages replayed from Kafka
- `message`: human-readable summary

#### `reconnect_error`
```json
{ "type": "reconnect_error", "message": "Message replay not available (no Kafka consumer)" }
```

#### `pong`
```json
{ "type": "pong", "ts": 1706000000000 }
```

#### `error`
```json
{ "type": "error", "message": "..." }
```

### Channel Permissions

The gateway performs validation in two stages:

**Stage 1 — Tenant validation (always, auth enabled or disabled):**
Channel must start with `{tenant_id}.` matching the JWT's `tenant_id` (auth enabled) or `DefaultTenantID` (auth disabled). Channels with wrong tenant prefix are silently filtered out.

**Stage 2 — Pattern matching (auth enabled only):**
The gateway strips the tenant prefix locally and matches the remaining portion against permission patterns. Patterns operate on the non-tenant portion of the channel (e.g., `odin.BTC.trade` → match `BTC.trade` against `*.trade`).

| Pattern Type | Config Key | Format | Example | Behavior |
|--------------|-----------|--------|---------|----------|
| Public | `publicPatterns` | Wildcard glob | `*.trade` | Anyone can subscribe (matches `BTC.trade` from `odin.BTC.trade`) |
| User-scoped | `userScopedPatterns` | `{principal}` placeholder | `balances.{principal}` | Only the JWT subject can subscribe (e.g., `odin.balances.user123` requires `sub=user123`) |
| Group-scoped | `groupScopedPatterns` | `{group_id}` placeholder | `community.{group_id}` | Only members of the group can subscribe (matched against JWT `groups` claim) |

Channels not matching any pattern are denied. Permission patterns remain unchanged from previous config — they never included tenant prefixes.

### Schemas

Source of truth for all schemas: `ws/internal/shared/protocol/types.go`

Define reusable schemas in `components/schemas/`:

| Schema | Fields | Source |
|--------|--------|--------|
| `ClientMessage` | `type: string`, `data: object` | `protocol.ClientMessage` |
| `SubscribeData` | `channels: string[]` | `protocol.SubscribeData` |
| `UnsubscribeData` | `channels: string[]` | `protocol.UnsubscribeData` |
| `PublishData` | `channel: string`, `data: object` | `protocol.PublishData` |
| `ReconnectData` | `client_id: string`, `last_offset: map[string, integer]` | `handlers_message.go` inline struct |
| `MessageEnvelope` | `type`, `seq`, `ts`, `channel`, `data` | `messaging.MessageEnvelope` |
| `PublishError` | `type`, `code`, `message` | `protocol.PublishErrorMessages` |
| `SubscriptionAck` | `type`, `subscribed: string[]`, `count: integer` | `handlers_message.go:110` |
| `UnsubscriptionAck` | `type`, `unsubscribed: string[]`, `count: integer` | `handlers_message.go:151` |
| `PublishAck` | `type`, `channel: string`, `status: string` | `handlers_publish.go:127` |
| `ReconnectAck` | `type`, `status: string`, `messages_replayed: integer`, `message: string` | `handlers_message.go:305` |
| `ReconnectError` | `type`, `message: string` | `handlers_message.go:205` |
| `Pong` | `type`, `ts: integer` | `handlers_message.go:49` |
| `Error` | `type`, `message: string` | generic error response |

### Security Schemes

```yaml
components:
  securitySchemes:
    bearerToken:
      type: http
      scheme: bearer
      bearerFormat: JWT
      description: "Authorization: Bearer {token}"
    queryToken:
      type: httpApiKey
      in: query
      name: token
      description: "?token={jwt} — takes precedence over header"
```

### JWT Claims (document in description)

| Field | Type | Description |
|-------|------|-------------|
| `sub` | string | Principal (user identifier) |
| `tenant_id` | string | Tenant for routing and access control |
| `roles` | string[] | Roles for authorization |
| `groups` | string[] | Group memberships (for group-scoped channels) |
| `scopes` | string[] | Permission scopes |
| `attrs` | map[string]string | Custom attributes |

### Internal Protocol Appendix

Include as an `x-internal-protocol` section or a description block. With explicit channels, the gateway-to-server protocol uses **the same channel format** as the client API (`{tenant}.{identifier}.{category}`). The gateway's role is validation and permission filtering, not channel transformation.

| Aspect | Client → Gateway | Gateway → Server |
|--------|-----------------|------------------|
| Channel format | `{tenant}.{identifier}.{category}` | Same (passed through) |
| Auth | JWT validated at gateway | Already validated |
| Subscribe filtering | Gateway filters unauthorized channels | All channels pre-authorized |
| Publish validation | Gateway validates tenant + permissions | Server validates Kafka routing |

Message flow:
```
Client → Gateway → [tenant validation + permission filtering] → Server → Kafka
                                                                  ↓
Client ← Gateway ← [pass-through] ←————————————————————— Server ← Kafka Consumer
```

---

## Phase 2: Redpanda API (`redpanda.asyncapi.yaml`)

Documents how external services (CDC pipelines, backend services) publish events to Redpanda that get broadcast to WebSocket clients.

### Topic Naming

```
{namespace}.{tenant}.{category}
```

- `namespace`: environment — `local`, `dev`, `stag`, `prod`
- `tenant`: tenant identifier — e.g., `odin`, `acme`
- `category`: event category — e.g., `trade`, `balances`, `orderbook`

Built by: `kafka.BuildTopicName(namespace, tenantID, category)`

### Message Format

External services publish Kafka records with:

| Field | Format | Example |
|-------|--------|---------|
| **Topic** | `{namespace}.{tenant}.{category}` | `prod.odin.trade` |
| **Key** | `{tenant}.{identifier}.{category}` | `odin.BTC.trade` |
| **Value** | JSON payload (arbitrary, schema owned by publisher) | `{"price": 50000, "volume": 1.5}` |

The Key determines:
1. **Kafka partition** — messages with the same Key go to the same partition (ordering guarantee)
2. **Broadcast channel** — ws-server uses the Key as the subscription index lookup key and the channel in the message envelope sent to clients

The Value is opaque to the WebSocket platform. It is forwarded as-is to subscribed clients in the `data` field of `MessageEnvelope`. Payload schemas are owned by the publishing service, not defined in this spec.

### Headers (optional, added by ws-server producer)

| Header | Description |
|--------|-------------|
| `client_id` | WebSocket client that published (only for client-published messages) |
| `source` | Origin service (e.g., `ws-server`) |
| `timestamp` | ISO 8601 timestamp |

### How It Reaches Clients

```
External Service
  → Redpanda topic: prod.odin.trade (Key: odin.BTC.trade, Value: {...})
  → ws-server consumer reads message
  → Broadcast to clients subscribed to "odin.BTC.trade"
  → Client receives: { "type": "message", "channel": "odin.BTC.trade", "data": {...} }
```

No channel transformation occurs. The Kafka key is used as-is for subscription matching and as the channel in the client envelope.

### Channels to Document

Use a parameterized channel pattern rather than per-category channels, since categories are tenant-defined and provisioned dynamically:

```yaml
channels:
  tenantEvents:
    address: "{namespace}.{tenant}.{category}"
    description: >
      Parameterized Redpanda topic. Categories are provisioned per-tenant
      via the provisioning API (stored in tenant_categories table).
      Common categories include: trade, balances, orderbook — but tenants
      can provision any category.
    parameters:
      namespace:
        description: "Environment: local, dev, stag, prod"
      tenant:
        description: "Tenant identifier (e.g., odin, acme)"
      category:
        description: "Event category (e.g., trade, balances)"
    messages:
      event:
        payload:
          type: object
          description: "Arbitrary JSON. Schema owned by publishing service."
```

### Provisioning Requirement

Topics must be provisioned before use via the provisioning API. Each tenant has a set of categories stored in `tenant_categories`. Publishing to an unprovisioned category returns `topic_not_provisioned`.

---

## Implementation Notes

### AsyncAPI 3.0 Structure

Each spec file follows this structure:

```yaml
asyncapi: 3.0.0
info:
  title: ...
  version: ...
  description: ...

servers:
  ...

channels:
  ...

operations:
  ...

components:
  schemas:
    ...
  securitySchemes:
    ...
  messages:
    ...
```

### Channel Naming Conventions to Document

With explicit channels, there is one channel format used everywhere:

| Context | Format | Example |
|---------|--------|---------|
| Client channel | `{tenant}.{identifier}.{category}` | `odin.BTC.trade` |
| Subscription index key | `{tenant}.{identifier}.{category}` | `odin.BTC.trade` |
| Kafka topic | `{namespace}.{tenant}.{category}` | `local.odin.trade` |
| Kafka key | `{tenant}.{identifier}.{category}` | `odin.BTC.trade` |

### Rate Limits and Constraints to Document

| Constraint | Default | Config Env Var |
|------------|---------|----------------|
| Publish rate | 10 msg/sec | `GATEWAY_PUBLISH_RATE_LIMIT` |
| Publish burst | 100 | `GATEWAY_PUBLISH_BURST` |
| Max publish payload | 64KB | `GATEWAY_MAX_PUBLISH_SIZE` |
| Send buffer | 256 messages | `WS_SEND_BUFFER_SIZE` |
| Slow client max attempts | 3 | `WS_SLOW_CLIENT_MAX_ATTEMPTS` |
| Reconnect replay limit | 100 messages | - |
| Reconnect timeout | 5s | - |
| Min channel parts | 3 | - |

### WebSocket Close Codes

| Code | Meaning | Trigger |
|------|---------|---------|
| 1000 | Normal closure | Client or server initiated clean close |
| 1001 | Going away | Server shutting down (graceful) |
| 1008 | Policy violation | Slow client disconnected after max attempts |
| 1011 | Internal error | Unexpected server error |

### Source Files (schema source of truth)

| File | What to Extract |
|------|-----------------|
| `shared/protocol/types.go` | All message types and data structures |
| `shared/protocol/errors.go` | All publish error codes and messages |
| `shared/protocol/limits.go` | Rate limits and size constants |
| `shared/auth/jwt.go` | JWT claims structure |
| `shared/kafka/topic.go` | Topic naming function |
| `shared/kafka/config.go` | Namespace normalization |
| `server/messaging/message.go` | MessageEnvelope structure |

---

## Implementation Order

1. **Explicit channels refactor** (prerequisite)
   - Remove ChannelMapper from server (broadcast stripping)
   - Remove ChannelMapper from gateway/proxy (channel prepending)
   - Remove crossTenantRoles from gateway, proxy, auth
   - Add tenant prefix validation in gateway proxy
   - Update min channel parts from 2 to 3
   - Update tests
   - Remove old AsyncAPI files from `ws/docs/asyncapi/`

2. **Create `ws/docs/asyncapi/client-ws.asyncapi.yaml`** (~450 lines)

3. **Create `ws/docs/asyncapi/redpanda.asyncapi.yaml`** (~150 lines)

**Total estimated:** ~600 lines of AsyncAPI specs + net negative lines of Go code
