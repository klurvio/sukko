# Shared Constants & Types Consolidation Plan

**Status:** Implemented
**Date:** 2026-01-31

**Goal:** Eliminate code duplication between gateway, server, and provisioning by creating shared packages for constants, types, and configuration.

---

## Summary of Duplications Found

| Category | Gateway | Server | Provisioning | Action |
|----------|---------|--------|--------------|--------|
| **PublishErrorCode** | 5 codes (proxy.go) | 9 codes (errors.go) | N/A | Consolidate |
| **Message Types** | Inline strings | Inline strings | N/A | Create constants |
| **ClientMessage struct** | proxy.go | Inline in handlers | N/A | Consolidate |
| **SubscribeData struct** | proxy.go | Inline in handlers | N/A | Consolidate |
| **PublishData struct** | proxy.go | Inline in handlers | N/A | Consolidate |
| **Max Message Size** | 64KB configurable | Not enforced | N/A | Share constant |
| **Rate Limit Defaults** | 10/sec, 100 burst | 10/sec, 100 burst | N/A | Share constants |
| **Log Level Validation** | gateway_config.go | server_config.go | provisioning_config.go | Extract function |
| **Kafka SASL Validation** | N/A | server_config.go | provisioning_config.go | Extract function |
| **OIDC Config + Validation** | gateway_config.go | N/A | provisioning_config.go | Create shared struct |
| **DB Pool Config** | gateway_config.go | server_config.go | provisioning_config.go | Create shared struct |

---

## Implementation Phases

### Phase 1: Create Shared Protocol Types

**New file:** `ws/internal/shared/protocol/types.go`

```go
// Message type constants
const (
    MsgTypeSubscribe   = "subscribe"
    MsgTypeUnsubscribe = "unsubscribe"
    MsgTypePublish     = "publish"
    MsgTypeReconnect   = "reconnect"
    MsgTypeHeartbeat   = "heartbeat"
    MsgTypeMessage     = "message"
    MsgTypePong        = "pong"
    MsgTypeError       = "error"
)

// Response type constants
const (
    RespTypeSubscriptionAck   = "subscription_ack"
    RespTypeUnsubscriptionAck = "unsubscription_ack"
    RespTypePublishAck        = "publish_ack"
    RespTypePublishError      = "publish_error"
    RespTypeReconnectAck      = "reconnect_ack"
    RespTypeReconnectError    = "reconnect_error"
)

// ClientMessage is the standard envelope for client→server messages
type ClientMessage struct {
    Type string          `json:"type"`
    Data json.RawMessage `json:"data,omitempty"`
}

// SubscribeData is the payload for subscribe messages
type SubscribeData struct {
    Channels []string `json:"channels"`
}

// UnsubscribeData is the payload for unsubscribe messages
type UnsubscribeData struct {
    Channels []string `json:"channels"`
}

// PublishData is the payload for publish messages
type PublishData struct {
    Channel string          `json:"channel"`
    Data    json.RawMessage `json:"data"`
}
```

**Files to update:**
- `ws/internal/gateway/proxy.go` - Remove local types, import shared
- `ws/internal/server/handlers_message.go` - Use shared types
- `ws/internal/server/handlers_publish.go` - Use shared types

---

### Phase 2: Create Shared Publish Errors

**New file:** `ws/internal/shared/protocol/errors.go`

```go
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

// Sentinel errors for internal use
var (
    ErrInvalidChannel      = errors.New("invalid channel format")
    ErrTopicNotProvisioned = errors.New("topic not provisioned")
    ErrServiceUnavailable  = errors.New("service unavailable")
    ErrProducerClosed      = errors.New("producer is closed")
)
```

**Files to update:**
- `ws/internal/gateway/proxy.go` - Remove local PublishErrorCode, import shared
- `ws/internal/server/errors.go` - Delete file (moved to shared)
- `ws/internal/server/handlers_publish.go` - Import from shared
- `ws/internal/shared/kafka/producer.go` - Import sentinel errors from shared

---

### Phase 3: Create Shared Limits Constants

**New file:** `ws/internal/shared/protocol/limits.go`

```go
// Size limits
const (
    MaxPublishMessageSize = 64 * 1024 // 64KB
    DefaultSendBufferSize = 256
)

// Rate limit defaults
const (
    DefaultPublishRateLimit = 10.0  // messages per second
    DefaultPublishBurst     = 100   // burst capacity
)

// Channel format constants
const (
    MinClientChannelParts   = 2 // {identifier}.{category}
    MinInternalChannelParts = 3 // {tenant}.{identifier}.{category}
)
```

**Files to update:**
- `ws/internal/gateway/proxy.go` - Use shared constants
- `ws/internal/server/handlers_publish.go` - Use shared constants
- `ws/internal/shared/platform/gateway_config.go` - Reference shared defaults

---

### Phase 4: Create Shared Config Validation

**New file:** `ws/internal/shared/platform/validation.go`

```go
// ValidLogLevels are the supported log levels
var ValidLogLevels = map[string]bool{
    "debug": true, "info": true, "warn": true, "error": true,
}

// ValidLogFormats are the supported log formats
var ValidLogFormats = map[string]bool{
    "json": true, "text": true, "pretty": true,
}

// ValidKafkaSASLMechanisms are the supported SASL mechanisms
var ValidKafkaSASLMechanisms = map[string]bool{
    "scram-sha-256": true, "scram-sha-512": true,
}

func ValidateLogLevel(level string) error
func ValidateLogFormat(format string) error
func ValidateKafkaSASLMechanism(mechanism string) error
```

**Files to update:**
- `ws/internal/shared/platform/gateway_config.go` - Use shared validators
- `ws/internal/shared/platform/server_config.go` - Use shared validators
- `ws/internal/shared/platform/provisioning_config.go` - Use shared validators

---

## Files Summary

### New Files (4)
| File | Purpose |
|------|---------|
| `ws/internal/shared/protocol/types.go` | Message types, request/response structs |
| `ws/internal/shared/protocol/errors.go` | PublishErrorCode, error messages, sentinel errors |
| `ws/internal/shared/protocol/limits.go` | Size limits, rate limit defaults |
| `ws/internal/shared/platform/validation.go` | Config validation helpers |

### Files Modified (8)
| File | Changes |
|------|---------|
| `ws/internal/gateway/proxy.go` | Remove duplicated types, import shared |
| `ws/internal/server/handlers_message.go` | Use shared types and constants |
| `ws/internal/server/handlers_publish.go` | Use shared types, errors, limits |
| `ws/internal/shared/kafka/producer.go` | Import sentinel errors from shared |
| `ws/internal/shared/platform/gateway_config.go` | Use shared validators |
| `ws/internal/shared/platform/server_config.go` | Use shared validators |
| `ws/internal/shared/platform/provisioning_config.go` | Use shared validators |

### Files Deleted (1)
| File | Reason |
|------|--------|
| `ws/internal/server/errors.go` | Moved to shared/protocol/errors.go |

---

## Verification

```bash
# Build all packages
cd /Volumes/Dev/Codev/Toniq/sukko/ws
go build ./...

# Run all tests
go test ./... -short

# Run linter
golangci-lint run

# Verify no duplicate type definitions remain
grep -r "type PublishErrorCode" internal/
grep -r "type ClientMessage" internal/
```

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Import cycles | New packages have no dependencies on gateway/server |
| Breaking API | Types are internal, not exported to external consumers |
| Test failures | Update test files to use shared types |
| Config parsing changes | Embedded structs preserve env var parsing via `envPrefix` |

---

## Estimated Impact

- **Lines removed:** ~150 (duplicated code)
- **Lines added:** ~200 (new shared packages)
- **Net benefit:** Single source of truth, easier maintenance, consistent error codes
