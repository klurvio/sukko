# Feature Specification: Mid-Connection Auth Refresh

**Branch**: `feat/auth-refresh`
**Created**: 2026-02-26
**Status**: Draft

## Context

Sukko authenticates clients via JWT at WebSocket upgrade time only. Once a connection is established, the tenant identity and permissions are fixed for the session lifetime. There is no protocol-level mechanism for a client to send a refreshed token on an existing connection.

This creates a hard limitation: when a JWT approaches expiry, the only option is to disconnect and reconnect with a fresh token. This causes:

1. **Brief service interruptions** — even with a graceful reconnect strategy, there is a window where the client is disconnected, must re-subscribe, and replay missed messages.
2. **Unnecessary load** — every token refresh triggers a full reconnect cycle: new WebSocket upgrade, new backend dial, re-subscription to all channels, and potential Kafka replay.
3. **SDK complexity** — the TypeScript SDK (`@sukko/ws-sdk`) must implement a proactive graceful-reconnect workaround for token refresh because the server has no native support for it.
4. **Permission staleness** — if a user's roles, groups, or scopes change mid-session (e.g., upgraded to a premium tier), those changes cannot take effect until the next reconnect.

Adding an `auth` message type to the WebSocket protocol allows clients to refresh their token on an existing connection. The gateway validates the new JWT, updates the connection's claims and permissions in-place, and optionally re-evaluates active subscriptions against the new permission set — all without disconnecting.

Additionally, the shared protocol package (`ws/internal/shared/protocol/`) currently contains 15 server-only symbols that violate separation of concern. As a prerequisite to adding gateway-specific auth types, these server-only types MUST be migrated to the server package to establish a clean protocol boundary.

## User Scenarios

### Scenario 1 — Proactive Token Refresh (Priority: P1)

A client's JWT is approaching expiry. The client sends a new token over the existing connection and continues receiving data without interruption.

**Acceptance Criteria**:
1. **Given** a connected client with an active session, **When** the client sends an `auth` message containing a valid JWT, **Then** the gateway validates the new token, updates the connection's claims and permissions, and responds with an `auth_ack` containing the new token's expiry timestamp.
2. **Given** a connected client that sends an `auth` message, **When** the new JWT is valid and for the **same tenant**, **Then** the connection continues uninterrupted with the updated claims — no re-subscription or replay is required.
3. **Given** a connected client that sends an `auth` message, **When** the new JWT is valid but for a **different tenant**, **Then** the gateway responds with an `auth_error` (code: `tenant_mismatch`) and the connection continues with the original token unchanged.

### Scenario 2 — Permission Update via Token Refresh (Priority: P1)

A user's permissions change mid-session (e.g., new roles granted, scopes expanded). The client refreshes its token to pick up the new permissions without reconnecting.

**Acceptance Criteria**:
1. **Given** a connected client with subscriptions to channels A, B, and C, **When** the client sends an `auth` message with a new JWT that has **expanded** permissions (now also authorized for channel D), **Then** the gateway updates permissions and the client can subscribe to channel D on the next `subscribe` call.
2. **Given** a connected client with subscriptions to channels A, B, and C, **When** the client sends an `auth` message with a new JWT that has **reduced** permissions (no longer authorized for channel C), **Then** the gateway updates permissions, forcibly unsubscribes the client from channel C, and sends an `unsubscription_ack` listing the revoked channels.

### Scenario 3 — Invalid Token Refresh (Priority: P1)

A client sends an invalid or expired token as a refresh attempt. The connection is preserved with the original credentials.

**Acceptance Criteria**:
1. **Given** a connected client, **When** the client sends an `auth` message with an **expired** JWT, **Then** the gateway responds with an `auth_error` (code: `token_expired`) and the connection continues with the original token unchanged.
2. **Given** a connected client, **When** the client sends an `auth` message with a **malformed** or **invalid-signature** JWT, **Then** the gateway responds with an `auth_error` (code: `invalid_token`) and the connection continues with the original token unchanged.
3. **Given** a connected client, **When** the client sends an `auth` message with a JWT that fails validation for any reason, **Then** the original session claims, permissions, and subscriptions remain completely unaffected.

### Scenario 4 — Auth Refresh on Unauthenticated Connections (Priority: P2)

When authentication is disabled at the gateway level, auth refresh messages are gracefully rejected.

**Acceptance Criteria**:
1. **Given** a gateway running with `AuthEnabled=false`, **When** a connected client sends an `auth` message, **Then** the gateway responds with an `auth_error` (code: `not_available`) indicating auth refresh is not supported in this mode.

### Scenario 5 — Rate-Limited Auth Refresh (Priority: P2)

Auth refresh is rate-limited to prevent abuse (e.g., a buggy client sending auth messages in a tight loop).

**Acceptance Criteria**:
1. **Given** a connected client, **When** the client sends auth refresh messages faster than the configured rate limit, **Then** the gateway responds with an `auth_error` (code: `rate_limited`) for excess requests and the connection continues with the most recently accepted token.

### Edge Cases

- What happens if the client sends an `auth` message while a subscribe/unsubscribe is in flight? The gateway MUST serialize auth refresh with subscribe/unsubscribe operations to avoid race conditions where permissions change mid-evaluation.
- What happens if the new token has a shorter expiry than the original? The gateway accepts it — token lifetime management is the client's responsibility.
- What happens if the new token has the same `exp` as the current one but different claims? The gateway accepts it and updates permissions accordingly — the token is not deduplicated.
- What happens if the auth refresh triggers forced unsubscription of channels? The gateway sends an `unsubscription_ack` with the revoked channels so the client knows which subscriptions were removed.

## Requirements

### Functional Requirements

**Phase 1 — Protocol Separation (Prerequisite)**

- **FR-P01**: Server-only protocol types MUST be moved from `ws/internal/shared/protocol/` to `ws/internal/server/`. The following symbols are server-only:
  - Message types: `MsgTypeReconnect`, `MsgTypeHeartbeat`, `MsgTypeMessage`, `MsgTypePong`, `MsgTypeError`
  - Response types: `RespTypeSubscriptionAck`, `RespTypeUnsubscriptionAck`, `RespTypeReconnectAck`, `RespTypeReconnectError`, `RespTypeSubscribeError`, `RespTypeUnsubscribeError`
  - Data type: `UnsubscribeData`
  - Error codes: `ErrCodeInvalidJSON`, `ErrCodePublishFailed`, `ErrCodeReplayFailed`
  - Note: `ErrProducerClosed` is removed from the shared protocol package but relocated to `shared/kafka/producer.go` (producer concern), not to the server package.
- **FR-P02**: The shared protocol package MUST retain only types used by both gateway and server: `MsgTypeSubscribe`, `MsgTypeUnsubscribe`, `MsgTypePublish`, `RespTypePublishAck`, `RespTypePublishError`, `ClientMessage`, `SubscribeData`, `PublishData`, `ErrorCode`, shared `ErrCode*` constants, `PublishErrorMessages`, and shared sentinel errors.
- **FR-P03**: All existing tests MUST pass after the migration with no behavioral changes.

**Phase 2 — Auth Refresh**

- **FR-001**: The WebSocket protocol MUST support a new `auth` client→server message type containing a JWT token. The auth message type and related types (`auth_ack`, `auth_error`, error codes) MUST be defined in the gateway package, not in the shared protocol package.
- **FR-002**: The gateway MUST validate the new JWT using the same `MultiTenantValidator` used at connection time.
- **FR-003**: On successful validation, the gateway MUST update the connection's claims and permission checker in-place.
- **FR-004**: On successful validation, the gateway MUST respond with an `auth_ack` containing the new token's expiry timestamp (`exp`).
- **FR-005**: The gateway MUST reject auth refresh attempts that change the `tenant_id` claim, responding with `auth_error` (code: `tenant_mismatch`).
- **FR-006**: On validation failure, the gateway MUST respond with an `auth_error` containing a machine-readable error code and the connection MUST continue with the original token unchanged.
- **FR-007**: When a token refresh results in reduced permissions, the gateway MUST forcibly unsubscribe the client from channels they are no longer authorized for and notify the client via `unsubscription_ack`.
- **FR-008**: Auth refresh MUST be serialized with subscribe and unsubscribe operations on the client→backend path to prevent permission race conditions.
- **FR-009**: Auth refresh MUST be rate-limited per connection (default: 1 per 30 seconds, configurable via environment variable).
- **FR-010**: When `AuthEnabled=false`, auth refresh messages MUST be rejected with `auth_error` (code: `not_available`).
- **FR-011**: The `auth` message type MUST be intercepted and handled entirely by the gateway — it MUST NOT be forwarded to the backend ws-server.

### Non-Functional Requirements

- **NFR-001**: Auth refresh MUST complete within 50ms (excluding external key registry lookups) to avoid perceptible interruption to message flow.
- **NFR-002**: Auth refresh MUST NOT block the backend→client broadcast forwarding pipeline. Broadcast messages continue flowing during auth refresh.
- **NFR-003**: Auth refresh MUST be observable via Prometheus metrics: attempts, successes, failures (by error code), and latency histogram.
- **NFR-004**: Auth refresh MUST be logged at Info level on success and Warn level on failure, with structured fields (tenant_id, error_code, token_exp).

### Key Entities

- **AuthMessage**: Client→server message with `type: "auth"` and `token` field containing the new JWT string.
- **AuthAck**: Server→client response with `type: "auth_ack"` and `exp` field (Unix timestamp of new token expiry).
- **AuthError**: Server→client error response with `type: "auth_error"`, `code` (machine-readable), and `message` (human-readable).
- **AuthErrorCode**: New error codes — `invalid_token`, `token_expired`, `tenant_mismatch`, `rate_limited`, `not_available`.

## Success Criteria

- **SC-001**: A connected client can refresh its JWT without disconnecting, and message delivery continues uninterrupted during the refresh.
- **SC-002**: Permission changes in refreshed tokens take effect immediately — expanded permissions allow new subscriptions, reduced permissions trigger forced unsubscription.
- **SC-003**: Invalid token refresh attempts never corrupt the existing session state.
- **SC-004**: The TypeScript SDK can implement seamless token refresh using the `auth` message instead of the graceful-reconnect workaround, eliminating the brief interruption window.
- **SC-005**: Auth refresh operations are fully observable via metrics and structured logs.
- **SC-006**: The shared protocol package contains only types used by both gateway and server. Service-specific types live in their respective service packages.

## Clarifications

- Q: How should the gateway track subscriptions for forced unsubscription on permission downgrade? → A: **Gateway tracks subscriptions locally.** The gateway proxy intercepts `subscription_ack` and `unsubscription_ack` responses from the backend to maintain a local set of subscribed channels per connection. On auth refresh with reduced permissions, it diffs the local set against the new permission checker, sends `unsubscribe` to the backend for revoked channels, and sends `unsubscription_ack` to the client. This is self-contained — no backend changes needed.

- Q: What should the default auth refresh rate limit be? → A: **1 per 30 seconds per connection**, configurable via environment variable. Generous for legitimate use (tokens have 15-60 min lifetimes) while catching tight-loop bugs.
- Q: What should the auth message payload structure look like? → A: **Simple token field in the standard `data` envelope.** `{"type":"auth","data":{"token":"eyJ..."}}` — consistent with all other message types that use `type` + `data`.
- Q: Where should the auth message types be defined? → A: **Gateway-internal package.** Auth protocol types (`auth`, `auth_ack`, `auth_error` and their error codes) are gateway-only and MUST be defined in `ws/internal/gateway/`, not in the shared protocol package. Separation of concern: service-specific protocol types belong in that service's package.
- Q: Should migrating server-only protocol types out of the shared package be part of this spec? → A: **Yes, as Phase 1 (prerequisite).** 15 server-only symbols in `ws/internal/shared/protocol/` are moved to `ws/internal/server/` before auth types are added to the gateway. One coherent change that establishes clean protocol boundaries.
- Q: How should auth refresh interact with the message forwarding pipeline? → A: **Serialize auth with subscribe/unsubscribe only.** Auth refresh, subscribe, and unsubscribe operations share a serialization queue on the client→backend path. Broadcast messages (backend→client) continue flowing unblocked during auth refresh. NFR-002 applies to broadcast forwarding, not to client→backend request handling.

## Out of Scope

- **TypeScript SDK changes**: Updating `@sukko/ws-sdk` to use the new `auth` message is a separate task that builds on this server-side capability.
- **Token revocation**: This feature refreshes tokens — it does not add server-side token revocation or blacklisting.
- **Backend (ws-server) auth awareness**: The ws-server remains auth-unaware. Auth refresh is handled entirely in the gateway proxy layer, consistent with the current architecture.
- **Tenant migration**: Switching a connection from one tenant to another mid-session is explicitly forbidden (tenant_mismatch error).
- **Automatic server-initiated refresh**: The server does not proactively request token refresh from clients. Token lifecycle management remains client-driven.
