# Client Publish Topic Routing Implementation Plan

**Date:** 2026-01-31
**Status:** Implemented
**Scope:** Fix client publish flow to align with CDC-to-client flow

---

## Problem Statement

Client-published messages currently go to a single `sukko.{namespace}.client-events` topic which:
1. **Is not consumed** by ws-server - messages are lost
2. **Doesn't follow multi-tenant topic pattern** - should be `{namespace}.{tenant}.{category}`
3. **Uses hardcoded topic name** - should be provisioned via provisioning service

---

## Solution Overview

Align client publish flow with CDC flow:
- Gateway intercepts publish messages and maps channels (adds tenant prefix implicitly, like Pusher/Ably)
- Server routes messages to correct topic based on channel structure
- Same topic receives both CDC and client messages
- Channel (Kafka key) provides fine-grained routing within topics

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Unified Message Flow                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   CDC Service ─────────────┐                                         │
│   (Sukko-specific,          │  Topic building happens HERE            │
│    direct to Redpanda)     │  CDC builds: prod.acme.trade            │
│                            ▼                                         │
│                    ┌──────────────────┐                              │
│                    │ prod.acme.trade  │ ◄──── Kafka Topic            │
│                    │                  │       (coarse-grained)       │
│                    │  Key: acme.BTC.trade │ ◄── Channel (with tenant)│
│                    └──────────────────┘                              │
│                            ▲                                         │
│                            │  Topic building happens HERE            │
│   WebSocket Client ────────┘  Server builds: prod.acme.trade         │
│   (via gateway channel mapping)                                      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Where Topic Building Happens

| Source | Topic Building Location | Process |
|--------|------------------------|---------|
| **CDC (Sukko)** | CDC service (external) | CDC builds `prod.acme.trade`, publishes directly to Redpanda |
| **Client Publish** | ws-server Producer | Gateway maps channel, Server extracts tenant/category, builds topic |

**Flow comparison:**

```
CDC Flow:
  CDC service builds topic → publishes to prod.acme.trade
                             key = "acme.BTC.trade"

Client Publish Flow:
  Client: "BTC.trade"
       ↓ gateway (implicit tenant from JWT, like Pusher/Ably)
  Mapped: "acme.BTC.trade"
       ↓ server (parseChannel extracts tenant/category)
  Topic: prod.acme.trade
  Key: "acme.BTC.trade"
```

---

## Architecture Decisions

### 1. Implicit Tenant (Industry Standard)

Following Pusher/Ably pattern - tenant is derived from JWT, not embedded in client channel names:
- Client sends: `BTC.trade`
- Gateway maps: `acme.BTC.trade` (tenant from JWT)
- Client never sees/knows tenant ID

### 2. Topics are Coarse-Grained (Provisioned)

Topics represent **categories** of events, provisioned per-tenant:
- `prod.acme.trade` - All trade events for tenant "acme"
- `prod.acme.liquidity` - All liquidity events for tenant "acme"

**All topics must be provisioned via provisioning service** - no hardcoded `allTopicBases`.

### 3. Channels are Fine-Grained (Dynamic)

Channels provide routing granularity **within** a topic:
- `acme.BTC.trade` - BTC trade events for tenant acme
- `acme.ETH.trade` - ETH trade events for tenant acme

Channels are the **Kafka message key** for:
- Partition assignment (ordering guarantee)
- Broadcast routing to WebSocket subscribers

### 4. parseChannel Always Runs

`parseChannel` is the **primary logic** for topic building, not just a fallback:

```go
func (p *Producer) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
    // ALWAYS runs - extracts tenant/category to build topic
    tenant, category, err := parseChannel(channel)
    if err != nil {
        // Secondary purpose: catches unmapped channels (rollout safety)
        return err
    }

    topic := fmt.Sprintf("%s.%s.%s", p.namespace, tenant, category)
    // ...
}
```

### 5. Channel-to-Topic Mapping

```
Client Channel:     "BTC.trade"
                         │
                         ▼ (Gateway adds tenant from JWT)
Internal Channel:   "acme.BTC.trade"
                         │
                         ├── tenant = "acme" (first segment)
                         ├── identifier = "BTC" (middle segments)
                         └── category = "trade" (last segment)
                         │
                         ▼ (Server parseChannel - ALWAYS runs)
Kafka Topic:        "prod.acme.trade"
                         │
                         ├── namespace = "prod" (from config)
                         ├── tenant = "acme" (from channel)
                         └── category = "trade" (from channel)

Kafka Key:          "acme.BTC.trade" (full internal channel)
```

---

## Error Codes (Documented Constants)

Error codes must be documented constants, not hardcoded strings.

### New File: `ws/internal/server/errors.go`

```go
package server

// PublishErrorCode represents error codes for publish operations.
// These codes are part of the public API and documented in AsyncAPI spec.
type PublishErrorCode string

const (
    // ErrCodeNotAvailable indicates publishing is disabled on this server.
    ErrCodeNotAvailable PublishErrorCode = "not_available"

    // ErrCodeInvalidRequest indicates malformed publish request.
    ErrCodeInvalidRequest PublishErrorCode = "invalid_request"

    // ErrCodeInvalidChannel indicates invalid channel format.
    // Channel must have format: {identifier}.{category} (client) or
    // {tenant}.{identifier}.{category} (internal).
    ErrCodeInvalidChannel PublishErrorCode = "invalid_channel"

    // ErrCodeMessageTooLarge indicates payload exceeds size limit.
    ErrCodeMessageTooLarge PublishErrorCode = "message_too_large"

    // ErrCodeRateLimited indicates publish rate limit exceeded.
    ErrCodeRateLimited PublishErrorCode = "rate_limited"

    // ErrCodePublishFailed indicates Kafka publish failed.
    ErrCodePublishFailed PublishErrorCode = "publish_failed"

    // ErrCodeForbidden indicates not authorized to publish to channel.
    ErrCodeForbidden PublishErrorCode = "forbidden"

    // ErrCodeTopicNotProvisioned indicates the category topic doesn't exist.
    ErrCodeTopicNotProvisioned PublishErrorCode = "topic_not_provisioned"

    // ErrCodeServiceUnavailable indicates Kafka is unavailable (circuit open).
    ErrCodeServiceUnavailable PublishErrorCode = "service_unavailable"
)

// PublishErrorMessages provides human-readable messages for error codes.
var PublishErrorMessages = map[PublishErrorCode]string{
    ErrCodeNotAvailable:        "Publishing is not enabled on this server",
    ErrCodeInvalidRequest:      "Invalid publish request format",
    ErrCodeInvalidChannel:      "Channel must have format: identifier.category",
    ErrCodeMessageTooLarge:     "Message exceeds maximum size limit",
    ErrCodeRateLimited:         "Publish rate limit exceeded",
    ErrCodePublishFailed:       "Failed to publish message",
    ErrCodeForbidden:           "Not authorized to publish to this channel",
    ErrCodeTopicNotProvisioned: "Category is not provisioned for your tenant",
    ErrCodeServiceUnavailable:  "Service temporarily unavailable, please retry",
}
```

---

## Circuit Breaker

Circuit breaker for Server → Kafka connection to prevent cascading failures.

### Implementation

```go
import "github.com/sony/gobreaker"

type Producer struct {
    // ... existing fields ...
    circuitBreaker *gobreaker.CircuitBreaker
}

func NewProducer(cfg ProducerConfig) (*Producer, error) {
    // ... existing setup ...

    cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        "kafka-producer",
        MaxRequests: 1,                      // Requests allowed in half-open
        Interval:    0,                      // No periodic reset
        Timeout:     30 * time.Second,       // Time before half-open
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            return counts.ConsecutiveFailures >= 5
        },
        OnStateChange: func(name string, from, to gobreaker.State) {
            logger.Warn().
                Str("name", name).
                Str("from", from.String()).
                Str("to", to.String()).
                Msg("Circuit breaker state changed")
        },
    })

    return &Producer{
        // ...
        circuitBreaker: cb,
    }, nil
}

func (p *Producer) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
    _, err := p.circuitBreaker.Execute(func() (interface{}, error) {
        return nil, p.doPublish(ctx, clientID, channel, data)
    })

    if errors.Is(err, gobreaker.ErrOpenState) {
        return fmt.Errorf("%w: kafka circuit breaker open", ErrServiceUnavailable)
    }
    if errors.Is(err, gobreaker.ErrTooManyRequests) {
        return fmt.Errorf("%w: kafka circuit breaker half-open", ErrServiceUnavailable)
    }

    return err
}
```

### Circuit Breaker Settings

| Setting | Value | Rationale |
|---------|-------|-----------|
| **Consecutive Failures** | 5 | Trip after 5 consecutive failures |
| **Timeout** | 30 seconds | Wait before testing if Kafka recovered |
| **Half-Open Requests** | 1 | Allow 1 test request |

---

## Pitfall Mitigations

### Pitfall 1: Gateway Becomes Bottleneck

**Risk:** Every publish message requires JSON parse → map → validate → re-serialize.

**Mitigations:**

1. **Efficient JSON parsing:**
   ```go
   import jsoniter "github.com/json-iterator/go"
   var json = jsoniter.ConfigCompatibleWithStandardLibrary
   ```

2. **Metrics to detect bottleneck:**
   ```go
   gatewayPublishLatency = prometheus.NewHistogram(...)
   ```

3. **Future: Binary protocol (Phase 7)**
   - MessagePack recommended for simplicity
   - Protobuf for larger scale

### Pitfall 2: Double-Prefixing Bug

**Risk:** Client sends `acme.BTC.trade`, gateway maps to `acme.acme.BTC.trade`.

**Mitigation:**
```go
func (p *Proxy) interceptPublish(msg ClientMessage) ([]byte, error) {
    // Check if already prefixed with tenant
    if strings.HasPrefix(pubData.Channel, p.claims.TenantID + ".") {
        internalChannel = pubData.Channel  // Already prefixed
    } else {
        internalChannel = p.channelMapper.MapToInternal(p.claims, pubData.Channel)
    }
}
```

### Pitfall 3: Category Validation Gap

**Mitigations:**
1. Disable Kafka auto-topic-creation
2. Cache provisioned topics in producer
3. Return `ErrCodeTopicNotProvisioned` for invalid categories

### Pitfall 4: Rollout Order Dependency

**Mitigation:** Use Kubernetes deployment ordering (ArgoCD waves / Helm hooks).

**Safety:** `parseChannel` returns error for unmapped channels (< 3 parts).

### Pitfall 5: Error Response Path

**Solution:** Gateway sends error directly to client WebSocket, does NOT forward to server.

---

## Additional Requirements

### A. Rate Limiting at Gateway

```go
func (p *Proxy) interceptPublish(msg ClientMessage) ([]byte, error) {
    if !p.publishLimiter.Allow() {
        return p.sendErrorToClient(ErrCodeRateLimited)
    }
}
```

### B. Message Size Validation at Gateway

```go
type ProxyConfig struct {
    MaxPublishSize int  // Configurable per deployment (default 64KB)
}

func (p *Proxy) interceptPublish(msg ClientMessage) ([]byte, error) {
    if len(msg.Data) > p.config.MaxPublishSize {
        return p.sendErrorToClient(ErrCodeMessageTooLarge)
    }
}
```

### C. Observability

**Metrics:**
- `gateway_publish_total{tenant, status}`
- `gateway_publish_latency_seconds{tenant}`
- `server_publish_total{tenant, category, status}`
- `kafka_circuit_breaker_state{name}`

**Tracing:** OpenTelemetry spans with tenant/channel attributes.

**Logging:** Debug logs for channel mapping.

---

## Implementation Phases

### Phase 1: Gateway - Intercept Publish Messages

**File:** `ws/internal/gateway/proxy.go`

1. Add `ChannelMapper` to Proxy struct
2. Intercept `publish` message type
3. Map channel (add tenant prefix, detect double-prefix)
4. Validate channel access
5. Add rate limiting
6. Add message size validation (configurable)
7. Send error directly to client on failure
8. Add metrics and logging

### Phase 2: Server - Error Codes

**File:** `ws/internal/server/errors.go` (NEW)

1. Define `PublishErrorCode` type
2. Define all error code constants
3. Define error messages map
4. Document each code

### Phase 3: Server - Route to Correct Topic

**File:** `ws/internal/shared/kafka/producer.go`

1. Add `parseChannel` function (always runs)
2. Build topic: `{namespace}.{tenant}.{category}`
3. Add circuit breaker
4. Validate topic exists (cached lookup)
5. Use full channel as Kafka key
6. Add metrics
7. Remove `TopicClientEvents` constant

### Phase 4: Update Handler

**File:** `ws/internal/server/handlers_publish.go`

1. Use error code constants instead of hardcoded strings
2. Update channel validation for internal format (3+ parts)

### Phase 5: Deprecate Hardcoded Topic Bases

**File:** `ws/internal/shared/kafka/config.go`

1. Add deprecation comment to `allTopicBases`
2. Add `TopicCache` for producer validation

### Phase 6: Update AsyncAPI Documentation

**Files:**
- `ws/asyncapi/asyncapi.yaml` - Update publish docs, document error codes
- `ws/asyncapi/channel/control.yaml` - Update WsPublish channel
- `ws/asyncapi/component/message/control.yaml` - Update schemas

**Delete:**
- `ws/asyncapi/channel/client_events.yaml`

### Phase 7: Binary Protocol Optimization (Future)

Optional if gateway becomes bottleneck:
- MessagePack encoding for gateway↔server
- Benchmark before/after

---

## Planned Code Changes

### 1. `ws/internal/gateway/proxy.go`

```go
// ADD to Proxy struct
type Proxy struct {
    // ... existing ...
    channelMapper    *auth.ChannelMapper
    publishLimiter   *rate.Limiter
    maxPublishSize   int
}

// MODIFY interceptClientMessage
func (p *Proxy) interceptClientMessage(msg []byte) ([]byte, error) {
    // ... existing subscribe logic ...

    // NEW: Handle publish
    if clientMsg.Type == "publish" {
        return p.interceptPublish(clientMsg)
    }

    return msg, nil
}

// NEW function
func (p *Proxy) interceptPublish(msg ClientMessage) ([]byte, error) {
    start := time.Now()
    defer func() {
        gatewayPublishLatency.Observe(time.Since(start).Seconds())
    }()

    // 1. Rate limit
    if !p.publishLimiter.Allow() {
        return p.sendErrorToClient(ErrCodeRateLimited, PublishErrorMessages[ErrCodeRateLimited])
    }

    // 2. Parse
    var pubData struct {
        Channel string          `json:"channel"`
        Data    json.RawMessage `json:"data"`
    }
    if err := json.Unmarshal(msg.Data, &pubData); err != nil {
        return p.sendErrorToClient(ErrCodeInvalidRequest, PublishErrorMessages[ErrCodeInvalidRequest])
    }

    // 3. Size check
    if len(pubData.Data) > p.maxPublishSize {
        return p.sendErrorToClient(ErrCodeMessageTooLarge, PublishErrorMessages[ErrCodeMessageTooLarge])
    }

    // 4. Map channel (detect double-prefix)
    var internalChannel string
    if strings.HasPrefix(pubData.Channel, p.claims.TenantID+".") {
        internalChannel = pubData.Channel
    } else {
        internalChannel = p.channelMapper.MapToInternal(p.claims, pubData.Channel)
    }

    // 5. Validate access
    if !p.channelMapper.ValidateChannelAccess(p.claims, internalChannel, p.crossTenantRoles) {
        return p.sendErrorToClient(ErrCodeForbidden, PublishErrorMessages[ErrCodeForbidden])
    }

    // 6. Rebuild message with mapped channel
    pubData.Channel = internalChannel
    newData, _ := json.Marshal(pubData)

    gatewayPublishTotal.WithLabelValues(p.claims.TenantID, "success").Inc()

    return json.Marshal(ClientMessage{Type: "publish", Data: newData})
}

// NEW function
func (p *Proxy) sendErrorToClient(code PublishErrorCode, message string) ([]byte, error) {
    gatewayPublishTotal.WithLabelValues(p.claims.TenantID, string(code)).Inc()

    errMsg := map[string]string{
        "type":    "publish_error",
        "code":    string(code),
        "message": message,
    }
    errBytes, _ := json.Marshal(errMsg)
    p.clientConn.WriteMessage(websocket.TextMessage, errBytes)

    return nil, nil  // Don't forward to server
}
```

### 2. `ws/internal/server/errors.go` (NEW FILE)

```go
package server

// PublishErrorCode represents error codes for publish operations.
type PublishErrorCode string

const (
    ErrCodeNotAvailable        PublishErrorCode = "not_available"
    ErrCodeInvalidRequest      PublishErrorCode = "invalid_request"
    ErrCodeInvalidChannel      PublishErrorCode = "invalid_channel"
    ErrCodeMessageTooLarge     PublishErrorCode = "message_too_large"
    ErrCodeRateLimited         PublishErrorCode = "rate_limited"
    ErrCodePublishFailed       PublishErrorCode = "publish_failed"
    ErrCodeForbidden           PublishErrorCode = "forbidden"
    ErrCodeTopicNotProvisioned PublishErrorCode = "topic_not_provisioned"
    ErrCodeServiceUnavailable  PublishErrorCode = "service_unavailable"
)

var PublishErrorMessages = map[PublishErrorCode]string{
    ErrCodeNotAvailable:        "Publishing is not enabled on this server",
    ErrCodeInvalidRequest:      "Invalid publish request format",
    ErrCodeInvalidChannel:      "Channel must have format: identifier.category",
    ErrCodeMessageTooLarge:     "Message exceeds maximum size limit",
    ErrCodeRateLimited:         "Publish rate limit exceeded",
    ErrCodePublishFailed:       "Failed to publish message",
    ErrCodeForbidden:           "Not authorized to publish to this channel",
    ErrCodeTopicNotProvisioned: "Category is not provisioned for your tenant",
    ErrCodeServiceUnavailable:  "Service temporarily unavailable, please retry",
}
```

### 3. `ws/internal/shared/kafka/producer.go`

```go
// MODIFY Producer struct
type Producer struct {
    // ... existing ...
    circuitBreaker    *gobreaker.CircuitBreaker
    provisionedTopics map[string]bool
    topicCacheMu      sync.RWMutex
    topicCacheExpiry  time.Time
}

// MODIFY Publish function
func (p *Producer) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
    _, err := p.circuitBreaker.Execute(func() (interface{}, error) {
        return nil, p.doPublish(ctx, clientID, channel, data)
    })

    if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
        return ErrServiceUnavailable
    }
    return err
}

// NEW function - actual publish logic
func (p *Producer) doPublish(ctx context.Context, clientID int64, channel string, data []byte) error {
    // parseChannel ALWAYS runs
    tenant, category, err := parseChannel(channel)
    if err != nil {
        return fmt.Errorf("%w: %v", ErrInvalidChannel, err)
    }

    topic := fmt.Sprintf("%s.%s.%s", p.topicNamespace, tenant, category)

    // Validate topic is provisioned
    if !p.isTopicProvisioned(topic) {
        return ErrTopicNotProvisioned
    }

    record := &kgo.Record{
        Topic: topic,
        Key:   []byte(channel),
        Value: data,
        Headers: []kgo.RecordHeader{
            {Key: "client_id", Value: []byte(strconv.FormatInt(clientID, 10))},
            {Key: "source", Value: []byte("ws-client")},
            {Key: "timestamp", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
        },
    }

    results := p.client.ProduceSync(ctx, record)
    return results.FirstErr()
}

// NEW function
func parseChannel(channel string) (tenant, category string, err error) {
    parts := strings.Split(channel, ".")

    // Must be mapped format: tenant.identifier.category (3+ parts)
    if len(parts) < 3 {
        return "", "", fmt.Errorf("channel must have at least 3 parts (tenant.identifier.category), got %d", len(parts))
    }

    tenant = parts[0]
    category = parts[len(parts)-1]

    if tenant == "" {
        return "", "", fmt.Errorf("tenant (first segment) cannot be empty")
    }
    if category == "" {
        return "", "", fmt.Errorf("category (last segment) cannot be empty")
    }

    return tenant, category, nil
}

// DELETE
// const TopicClientEvents = "client-events"
```

### 4. `ws/internal/server/handlers_publish.go`

```go
// MODIFY to use error constants
func (s *Server) handleClientPublish(c *Client, data json.RawMessage) {
    if s.kafkaProducer == nil {
        s.sendPublishError(c, ErrCodeNotAvailable, PublishErrorMessages[ErrCodeNotAvailable])
        return
    }

    // ... parsing ...

    if !isValidPublishChannel(pubReq.Channel) {
        s.sendPublishError(c, ErrCodeInvalidChannel, PublishErrorMessages[ErrCodeInvalidChannel])
        return
    }

    // ... etc ...
}

// MODIFY - expects mapped channel (3+ parts)
func isValidPublishChannel(channel string) bool {
    if channel == "" {
        return false
    }
    parts := strings.Split(channel, ".")
    if len(parts) < 3 {  // Changed from < 2
        return false
    }
    return !slices.Contains(parts, "")
}
```

---

## Files Summary

### Files to Modify

| File | Changes |
|------|---------|
| `ws/internal/gateway/proxy.go` | Intercept publish, map channel, rate limit, size validation, error handling |
| `ws/internal/shared/kafka/producer.go` | parseChannel, topic routing, circuit breaker, remove TopicClientEvents |
| `ws/internal/server/handlers_publish.go` | Use error constants, update channel validation |
| `ws/internal/shared/kafka/config.go` | Deprecate `allTopicBases` |
| `ws/asyncapi/asyncapi.yaml` | Update docs, add error codes |
| `ws/asyncapi/channel/control.yaml` | Update WsPublish |
| `ws/asyncapi/component/message/control.yaml` | Update schemas |

### Files to Create

| File | Description |
|------|-------------|
| `ws/internal/server/errors.go` | Error codes and messages |

### Files to Delete

| File | Reason |
|------|--------|
| `ws/asyncapi/channel/client_events.yaml` | No longer a separate topic |

---

## Test Cases

### Gateway Tests

| Input Channel | JWT Tenant | Expected Internal | Result |
|--------------|------------|-------------------|--------|
| `BTC.trade` | `acme` | `acme.BTC.trade` | Pass |
| `acme.BTC.trade` | `acme` | `acme.BTC.trade` | Pass (no double-prefix) |
| `other.BTC.trade` | `acme` | - | Forbidden |
| Large message | `acme` | - | message_too_large |
| Rate exceeded | `acme` | - | rate_limited |

### Producer Tests

| Internal Channel | Namespace | Expected Topic | Key |
|-----------------|-----------|----------------|-----|
| `acme.BTC.trade` | `prod` | `prod.acme.trade` | `acme.BTC.trade` |
| `acme.BTC.unknown` | `prod` | - | topic_not_provisioned |

### Circuit Breaker Tests

| Scenario | Expected |
|----------|----------|
| 5 consecutive Kafka failures | Circuit opens |
| After 30s timeout | Circuit half-open, allows 1 request |
| Half-open request succeeds | Circuit closes |

---

## Rollout Plan

1. **Infrastructure:** Disable Kafka auto-topic-creation
2. **Deploy gateway** with publish interception (ArgoCD wave 1)
3. **Monitor** gateway metrics
4. **Deploy server** with topic routing (ArgoCD wave 2)
5. **Verify** end-to-end flow
6. **Monitor** circuit breaker state

---

## Dependencies

- Gateway: JWT claims available (implemented)
- Gateway: ChannelMapper integration (code exists, wire it)
- Server: gobreaker package for circuit breaker
- Provisioning: Topics provisioned for tenants

---

## Open Questions

1. **Topic Cache TTL:** Suggest 60 seconds - acceptable?
2. **Circuit Breaker Timeout:** 30 seconds - acceptable?
