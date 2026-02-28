# Implementation Plan: Mid-Connection Auth Refresh

**Branch**: `feat/auth-refresh` | **Date**: 2026-02-26 | **Spec**: specs/feat/auth-refresh/spec.md

## Summary

Add a mid-connection `auth` message type to the WebSocket protocol so clients can refresh JWT tokens without disconnecting. The feature is handled entirely in the gateway proxy layer — the ws-server remains auth-unaware. As a prerequisite, migrate server-only protocol types out of the shared package to enforce separation of concern.

## Technical Context

**Language**: Go 1.22+
**Services affected**: ws-gateway (primary), ws-server (protocol migration only)
**Infrastructure**: No changes (no new Kubernetes resources, Helm charts, or Terraform)
**Messaging**: No changes to Kafka/NATS
**Storage**: No new storage requirements
**Monitoring**: New Prometheus metrics for auth refresh operations
**Build/Deploy**: No changes

## Constitution Compliance

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | PASS | Auth refresh rate limit configurable via env var with `envDefault`; validated at startup (>= 1s) |
| II. Defense in Depth | PASS | Token validated at refresh time using same validator as connection time |
| III. Error Handling | PASS | All errors wrapped with context, typed error codes in responses |
| IV. Graceful Degradation | PASS | Failed refresh preserves existing session state; auth disabled mode returns `not_available` |
| V. Structured Logging | PASS | zerolog with structured fields (tenant_id, error_code, token_exp) |
| VI. Observability | PASS | New metrics: attempts, successes, failures by code, latency histogram |
| VII. Concurrency Safety | PASS | Claims protected by RWMutex; single-goroutine serialization on client→backend path; mutex released before I/O in forced unsub; pipeline non-blocking |
| VIII. Testing | PASS | Tests for all scenarios: valid refresh, invalid token, tenant mismatch, rate limit, forced unsub; benchmark for NFR-001 |
| IX. Security | PASS | Full JWT validation on refresh; tenant switch forbidden; rate limited |
| X. Shared Code Consolidation | PASS | Protocol types reorganized by ownership; auth error codes in gateway package |
| XI. Prior Art Research | PASS | See research.md R7 — prior art from Pusher, Ably, Socket.IO, Phoenix Channels, Centrifugo |

---

## Phase 1 — Protocol Separation

Move server-only types from `ws/internal/shared/protocol/` to `ws/internal/server/`. Pure refactor — zero behavioral change.

### 1.1 Create server-local protocol file

**New file**: `ws/internal/server/protocol.go`

Move these symbols from `shared/protocol/types.go`:
```
MsgTypeReconnect, MsgTypeHeartbeat     (client→server, server-only handlers)
MsgTypeMessage, MsgTypePong, MsgTypeError  (server→client, server-produced)
RespTypeSubscriptionAck, RespTypeUnsubscriptionAck
RespTypeReconnectAck, RespTypeReconnectError
RespTypeSubscribeError, RespTypeUnsubscribeError
UnsubscribeData
```

Move these symbols from `shared/protocol/errors.go`:
```
ErrCodeInvalidJSON, ErrCodePublishFailed, ErrCodeReplayFailed
```

> Note: `ErrProducerClosed` is NOT moved here — it is defined locally in `shared/kafka/producer.go` (see 1.4).

### 1.2 Update server imports

**Modify**: `ws/internal/server/handlers_message.go`
- Server-only constants become package-local references (no `protocol.` prefix for moved symbols)
- Shared types (`protocol.ClientMessage`, `protocol.SubscribeData`, `protocol.MsgTypeSubscribe`, `protocol.MsgTypePublish`, `protocol.ErrorCode`, `protocol.ErrCodeInvalidRequest`, `protocol.ErrCodeNotAvailable`) remain imported from shared

**Modify**: `ws/internal/server/handlers_publish.go`
- `protocol.RespTypePublishAck` and `protocol.RespTypePublishError` are used by the gateway (proxy.go:494) — these stay in shared
- `protocol.ErrCodePublishFailed` moves to server
- `protocol.PublishErrorMessages` stays in shared (used by both)
- `protocol.ErrProducerClosed` is defined locally in `shared/kafka/producer.go` (producer concern — see 1.4), not moved to server

### 1.3 Clean shared protocol package

**Modify**: `ws/internal/shared/protocol/types.go`
- Remove moved message type constants and `UnsubscribeData`
- Retain: `MsgTypeSubscribe`, `MsgTypeUnsubscribe`, `MsgTypePublish`, `RespTypePublishAck`, `RespTypePublishError`, `ClientMessage`, `SubscribeData`, `PublishData`

> Note: `MsgTypeUnsubscribe` stays in shared — the gateway will need it for sending synthetic unsubscribe messages to the backend during forced unsubscription in Phase 2.

**Modify**: `ws/internal/shared/protocol/errors.go`
- Remove `ErrCodeInvalidJSON`, `ErrCodePublishFailed`, `ErrCodeReplayFailed`, `ErrProducerClosed`
- Retain all other error codes and sentinel errors used by both packages

### 1.4 Update kafka producer

**Modify**: `ws/internal/shared/kafka/producer.go`
- Define `ErrProducerClosed` locally in `shared/kafka/producer.go` since it's a producer concern. Remove from shared protocol package. If the producer currently imports `protocol.ErrProducerClosed`, replace with a local sentinel. Server maps it in error handling.

### 1.5 Verify

- `cd ws && go vet ./...`
- `cd ws && go test ./...`
- All existing tests pass, no import cycles

### Files changed (Phase 1)

| Action | File |
|--------|------|
| **Create** | `ws/internal/server/protocol.go` |
| Modify | `ws/internal/server/handlers_message.go` |
| Modify | `ws/internal/server/handlers_publish.go` |
| Modify | `ws/internal/shared/protocol/types.go` |
| Modify | `ws/internal/shared/protocol/errors.go` |
| Modify | `ws/internal/shared/kafka/producer.go` (if ErrProducerClosed used from protocol) |

---

## Phase 2 — Auth Refresh: Protocol & Config

### 2.1 Gateway auth protocol types

**New file**: `ws/internal/gateway/auth_protocol.go`

```go
// Auth message type constants (gateway-only).
const (
    MsgTypeAuth      = "auth"
    RespTypeAuthAck  = "auth_ack"
    RespTypeAuthError = "auth_error"
)

// Auth-specific error codes.
const (
    AuthErrInvalidToken  = "invalid_token"
    AuthErrTokenExpired  = "token_expired"
    AuthErrTenantMismatch = "tenant_mismatch"
    AuthErrRateLimited   = "rate_limited"
    AuthErrNotAvailable  = "not_available"
)

// AuthData is the payload for auth refresh messages.
type AuthData struct {
    Token string `json:"token"`
}

// AuthAckResponse is the server→client response for successful auth refresh.
// Wire format: {"type":"auth_ack","data":{"exp":1740000000}}
type AuthAckResponse struct {
    Type string      `json:"type"` // RespTypeAuthAck
    Data AuthAckData `json:"data"`
}

type AuthAckData struct {
    Exp int64 `json:"exp"` // Unix timestamp of new token expiry
}

// AuthErrorResponse is the server→client response for failed auth refresh.
// Wire format: {"type":"auth_error","data":{"code":"invalid_token","message":"..."}}
type AuthErrorResponse struct {
    Type string        `json:"type"` // RespTypeAuthError
    Data AuthErrorData `json:"data"`
}

type AuthErrorData struct {
    Code    string `json:"code"`    // Machine-readable error code (AuthErr* constants)
    Message string `json:"message"` // Human-readable description
}
```

### 2.2 Gateway config extension

**Modify**: `ws/internal/shared/platform/gateway_config.go`

Add to `GatewayConfig`:
```go
AuthRefreshRateInterval time.Duration `env:"GATEWAY_AUTH_REFRESH_RATE_INTERVAL" envDefault:"30s"`
```

Add validation in `Validate()`:
```go
if c.AuthRefreshRateInterval < 1*time.Second {
    return fmt.Errorf("GATEWAY_AUTH_REFRESH_RATE_INTERVAL must be >= 1s, got %s", c.AuthRefreshRateInterval)
}
```

Add to `LogConfig()` output.

### 2.3 Auth refresh metrics

**Modify**: `ws/internal/gateway/metrics.go`

Add metrics following existing patterns:
```go
gateway_auth_refresh_total        CounterVec  labels: result (success, invalid_token, token_expired, tenant_mismatch, rate_limited, not_available)
gateway_auth_refresh_latency_seconds  Histogram
gateway_forced_unsubscriptions_total  Counter
```

Add helper functions: `RecordAuthRefresh(result string)`, `RecordAuthRefreshLatency(seconds float64)`, `RecordForcedUnsubscription()`.

### Files changed (Phase 2)

| Action | File |
|--------|------|
| **Create** | `ws/internal/gateway/auth_protocol.go` |
| Modify | `ws/internal/shared/platform/gateway_config.go` |
| Modify | `ws/internal/gateway/metrics.go` |

---

## Phase 3 — Auth Refresh: Proxy Implementation

This is the core implementation phase. All changes are in the gateway proxy.

### 3.1 Proxy struct changes

**Modify**: `ws/internal/gateway/proxy.go`

Add to `Proxy` struct:
```go
// Auth refresh
validator    TokenValidator        // JWT validator for mid-connection refresh (defined in interfaces.go)
claimsMu     sync.RWMutex          // Protects claims reads/writes
authLimiter  *rate.Limiter         // Per-connection auth refresh rate limit (1 per 30s)

// Subscription tracking (for forced unsubscription)
subscribedChannels map[string]struct{} // Channels confirmed by backend acks
```

> Note: `claimsMu` protects `claims` field. The existing `claims` field type stays `*auth.Claims`.

Add to `ProxyConfig`:
```go
Validator              TokenValidator
AuthRefreshRateInterval time.Duration
```

Update `NewProxy()` to initialize:
```go
authLimiter:        rate.NewLimiter(rate.Every(cfg.AuthRefreshRateInterval), 1),
subscribedChannels: make(map[string]struct{}),
validator:          cfg.Validator,
```

### 3.2 Auth refresh interception (client→backend)

**Modify**: `ws/internal/gateway/proxy.go` — `interceptClientMessage()`

Add case to the message type switch:
```go
case MsgTypeAuth:
    return p.interceptAuthRefresh(clientMsg)
```

`interceptAuthRefresh()` returns `(nil, nil)` — auth messages are NEVER forwarded to backend. This follows the same pattern as `sendPublishErrorToClient()`.

### 3.3 Auth refresh handler

**New method on Proxy**: `interceptAuthRefresh(clientMsg protocol.ClientMessage) ([]byte, error)`

Flow:
1. Record start time for latency metric.
2. If `!p.authEnabled`: send `auth_error` (`not_available`), return `(nil, nil)`.
3. Rate limit check (`p.authLimiter.Allow()`): if denied, send `auth_error` (`rate_limited`), return `(nil, nil)`.
4. Parse `AuthData` from `clientMsg.Data`. If invalid: send `auth_error` (`invalid_token`).
5. Call `p.validator.ValidateToken(ctx, authData.Token)`.
   - If `ErrTokenExpired`: send `auth_error` (`token_expired`).
   - If any other error: send `auth_error` (`invalid_token`).
6. Check `newClaims.TenantID == p.tenantID`. If mismatch: send `auth_error` (`tenant_mismatch`).
7. Call `p.forceUnsubscribeRevokedChannels(newClaims)` — collects revoked channels under lock, updates tracking, releases lock, then performs I/O.
8. Lock `p.claimsMu`, swap `p.claims = newClaims`, unlock.
9. Send `auth_ack` with `exp` timestamp to client.
10. Log at Info level. Record success metric.
11. Return `(nil, nil)`.

Error responses use a new `sendAuthErrorToClient(code string, message string)` method (mirrors `sendPublishErrorToClient` pattern).

### 3.4 Backend→client subscription tracking

**Modify**: `ws/internal/gateway/proxy.go` — `proxyBackendToClient()`

Add interception for TEXT frames (before forwarding to client):
```go
if header.OpCode == ws.OpText && p.authEnabled {
    p.trackSubscriptionResponse(payload)
}
// Always forward to client (tracking is observational, not mutating)
```

`trackSubscriptionResponse(payload []byte)`:
1. Quick byte check: if payload doesn't contain `"_ack"` in first 80 bytes, return immediately (broadcasts skip JSON parse).
2. Parse JSON to extract `type` field only (partial parse for efficiency).
3. If `subscription_ack`: parse channels array, add to `p.subscribedChannels`.
4. If `unsubscription_ack`: parse channels array, remove from `p.subscribedChannels`.
5. Subscription map access protected by `p.claimsMu.Lock()`.

### 3.5 Forced unsubscription on permission downgrade

**New method on Proxy**: `forceUnsubscribeRevokedChannels(newClaims *auth.Claims) []string`

Flow (two phases — collect under lock, I/O after unlock):

**Phase A — Collect revoked channels (under lock):**
1. Lock `p.claimsMu` (write lock).
2. For each channel in `p.subscribedChannels`:
   - Strip tenant prefix.
   - Call `p.permissions.CanSubscribe(newClaims, strippedChannel)`.
   - If denied: add to `revoked` list.
3. If no revoked channels: unlock, return empty.
4. Remove revoked channels from `p.subscribedChannels`.
5. Unlock `p.claimsMu`.

**Phase B — Perform I/O (lock released):**
6. Build synthetic `unsubscribe` message with revoked channels.
7. Forward to backend via `p.forwardFrame(p.backendConn, ...)`.
8. Send `unsubscription_ack` to client with revoked channels list.
9. Record `gateway_forced_unsubscriptions_total` metric.
10. Return revoked channel list (for logging).

> Design note: The mutex is released before any I/O (Constitution VII compliance). The local tracking is updated optimistically under the lock. The backend unsubscribe and client notification happen without holding any lock. The backend will send its own `unsubscription_ack` which the subscription tracker ignores (channels already removed). Fire-and-forget — no waiting for backend ack.

### 3.6 Claims-safe reads

**Modify**: `ws/internal/gateway/proxy.go` — `interceptSubscribe()`

Wrap the `p.claims` read in `p.claimsMu.RLock()`/`p.claimsMu.RUnlock()`. Since `interceptSubscribe()` and `interceptAuthRefresh()` both run in the `proxyClientToBackend()` goroutine, they are naturally serialized. The RLock is needed for correctness if future refactoring adds concurrency, and the cost is negligible.

### 3.7 Wire validator into Proxy

**Modify**: `ws/internal/gateway/gateway.go` — `HandleWebSocket()`

Pass validator to ProxyConfig:
```go
proxy := NewProxy(ProxyConfig{
    // ... existing fields ...
    Validator:               gw.validator,
    AuthRefreshRateInterval: gw.config.AuthRefreshRateInterval,
})
```

### 3.8 TokenValidator interface

**Decision**: The Proxy needs to call `ValidateToken(ctx, token)` but shouldn't depend on the concrete `MultiTenantValidator`. Define a minimal interface in `ws/internal/gateway/interfaces.go` (which already contains `TenantRegistry` and `MultiIssuerValidator` interfaces):

```go
// TokenValidator validates JWT tokens and returns claims.
type TokenValidator interface {
    ValidateToken(ctx context.Context, tokenString string) (*auth.Claims, error)
}
```

The existing `MultiTenantValidator` already satisfies this interface. This enables test mocking.

### Files changed (Phase 3)

| Action | File |
|--------|------|
| Modify | `ws/internal/gateway/interfaces.go` (add TokenValidator interface) |
| Modify | `ws/internal/gateway/proxy.go` (struct, interception, handler, tracking, forced unsub) |
| Modify | `ws/internal/gateway/gateway.go` (pass validator to proxy) |

---

## Phase 4 — Tests

### 4.1 Protocol separation tests

**Modify**: `ws/internal/server/*_test.go`
- Update imports for moved protocol types.
- Verify all existing tests pass.

### 4.2 Auth protocol type tests

**New file**: `ws/internal/gateway/auth_protocol_test.go`
- Test JSON marshaling/unmarshaling of auth message, auth_ack, auth_error.

### 4.3 Auth refresh handler tests

**New file**: `ws/internal/gateway/auth_refresh_test.go`

Table-driven tests covering:

| Test case | Input | Expected |
|-----------|-------|----------|
| Valid token refresh | Valid JWT, same tenant | `auth_ack` with new `exp` |
| Expired token | Expired JWT | `auth_error` (`token_expired`) |
| Invalid signature | Tampered JWT | `auth_error` (`invalid_token`) |
| Malformed token | Non-JWT string | `auth_error` (`invalid_token`) |
| Tenant mismatch | Valid JWT, different tenant | `auth_error` (`tenant_mismatch`) |
| Rate limited | Two auth messages within 30s | Second gets `auth_error` (`rate_limited`) |
| Auth disabled | `authEnabled=false` | `auth_error` (`not_available`) |
| Empty token | `{"token":""}` | `auth_error` (`invalid_token`) |
| Missing data field | `{"type":"auth"}` | `auth_error` (`invalid_token`) |

### 4.4 Subscription tracking tests

**New file**: `ws/internal/gateway/subscription_tracking_test.go`

Table-driven tests:
- Track channels from `subscription_ack`
- Remove channels from `unsubscription_ack`
- Ignore non-ack messages (broadcasts)
- Broadcast messages skip JSON parsing (performance)

### 4.5 Forced unsubscription tests

**Add to**: `ws/internal/gateway/auth_refresh_test.go`

| Test case | Input | Expected |
|-----------|-------|----------|
| Permission expanded | New token adds permissions | No forced unsub, `auth_ack` |
| Permission reduced | New token removes channel C access | Unsub sent to backend, `unsubscription_ack` to client with [C] |
| Permission unchanged | Same permissions | No forced unsub, `auth_ack` |
| All channels revoked | New token has no permissions | All channels unsubscribed |
| No active subscriptions | No channels subscribed | `auth_ack` only |

### 4.6 Integration tests

**Add to**: existing proxy test file (`ws/internal/gateway/proxy_test.go`)

End-to-end proxy test: connect → subscribe → auth refresh → verify subscription tracking → verify forced unsub.

### Files changed (Phase 4)

| Action | File |
|--------|------|
| Modify | `ws/internal/server/*_test.go` (import updates) |
| **Create** | `ws/internal/gateway/auth_protocol_test.go` |
| **Create** | `ws/internal/gateway/auth_refresh_test.go` |
| **Create** | `ws/internal/gateway/subscription_tracking_test.go` |
| Modify | `ws/internal/gateway/proxy_test.go` |

---

## Phase 5 — Helm & Documentation

### 5.1 Helm values

**Modify**: `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml`
- Add `GATEWAY_AUTH_REFRESH_RATE_INTERVAL` env var.

**Modify**: `deployments/helm/odin/charts/ws-gateway/values.yaml`
- Add `authRefreshRateInterval: "30s"` under gateway config section.

**Modify**: `deployments/helm/odin/values/standard/dev.yaml` (if gateway values overridden)
- Add auth refresh config if needed.

### Files changed (Phase 5)

| Action | File |
|--------|------|
| Modify | `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml` |
| Modify | `deployments/helm/odin/charts/ws-gateway/values.yaml` |
| Modify | `deployments/helm/odin/values/standard/dev.yaml` (if applicable) |

---

## Verification

### After Phase 1 (Protocol Separation)
```bash
cd ws && go vet ./...
cd ws && go test ./...
```

### After Phase 3 (Auth Refresh Implementation)
```bash
cd ws && go vet ./...
cd ws && go test ./internal/gateway/...
cd ws && go test ./...
```

### After Phase 5 (Helm)
```bash
helm lint deployments/helm/odin/charts/ws-gateway
```

### Manual verification (dev environment)
1. Deploy to dev: `task k8s:build:push:ws-gateway ENV=dev && task k8s:deploy ENV=dev`
2. Connect via WebSocket with JWT
3. Send `{"type":"auth","data":{"token":"<new-jwt>"}}` — expect `auth_ack`
4. Send auth with expired token — expect `auth_error` with `token_expired`
5. Send auth with different tenant — expect `auth_error` with `tenant_mismatch`
6. Check Prometheus metrics: `gateway_auth_refresh_total`, `gateway_auth_refresh_latency_seconds`
7. Check structured logs for auth refresh events

---

## Summary of All File Changes

| Action | File | Phase |
|--------|------|-------|
| **Create** | `ws/internal/server/protocol.go` | 1 |
| Modify | `ws/internal/server/handlers_message.go` | 1 |
| Modify | `ws/internal/server/handlers_publish.go` | 1 |
| Modify | `ws/internal/shared/protocol/types.go` | 1 |
| Modify | `ws/internal/shared/protocol/errors.go` | 1 |
| Modify | `ws/internal/shared/kafka/producer.go` | 1 |
| **Create** | `ws/internal/gateway/auth_protocol.go` | 2 |
| Modify | `ws/internal/shared/platform/gateway_config.go` | 2 |
| Modify | `ws/internal/gateway/metrics.go` | 2 |
| Modify | `ws/internal/gateway/interfaces.go` | 3 |
| Modify | `ws/internal/gateway/proxy.go` | 3 |
| Modify | `ws/internal/gateway/gateway.go` | 3 |
| Modify | `ws/internal/server/*_test.go` | 4 |
| **Create** | `ws/internal/gateway/auth_protocol_test.go` | 4 |
| **Create** | `ws/internal/gateway/auth_refresh_test.go` | 4 |
| **Create** | `ws/internal/gateway/subscription_tracking_test.go` | 4 |
| Modify | `ws/internal/gateway/proxy_test.go` | 4 |
| Modify | `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml` | 5 |
| Modify | `deployments/helm/odin/charts/ws-gateway/values.yaml` | 5 |

**New files**: 5 | **Modified files**: 14 | **Total**: 19

## Resource Impact

- **Memory**: ~200 bytes per connection for subscription tracking map (negligible)
- **CPU**: Fast byte prefix check on backend→client messages; full JSON parse only for ack responses (<0.1% of messages)
- **Network**: No additional network calls (validator uses cached keys)
- **Deployment**: Gateway-only redeploy; server unchanged at runtime (protocol migration is compile-time only)
