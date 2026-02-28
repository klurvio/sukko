# Feature Specification: TypeScript SDK for Odin WS

**Branch**: `feat/typescript-sdk`
**Created**: 2026-02-26
**Status**: Draft

## Context

Odin WS is a multi-tenant WebSocket infrastructure platform for real-time data distribution. Today, consumers must implement the WebSocket protocol manually — constructing message envelopes, handling authentication, tracking subscriptions, detecting sequence gaps, managing reconnection with offset replay, and interpreting error codes. This is error-prone and creates duplicated effort across every client application.

A TypeScript SDK provides a typed, ergonomic client library that encapsulates the full Odin WS protocol. TypeScript is the primary language of client applications consuming Odin WS data (trading dashboards, admin tools, monitoring UIs). The SDK eliminates protocol-level boilerplate and gives consumers a reliable, well-tested interface to the platform.

## User Scenarios

### Scenario 1 - Subscribe to Real-Time Data (Priority: P1)

A developer building a trading dashboard connects to Odin WS, authenticates with a JWT, subscribes to multiple channels, and receives broadcast messages with typed payloads.

**Acceptance Criteria**:
1. **Given** a valid JWT and server URL, **When** the developer creates a client and calls `connect()`, **Then** a WebSocket connection is established with the JWT passed as authentication.
2. **Given** a connected client, **When** the developer calls `subscribe(["tenant.BTC.trade", "tenant.ETH.orderbook"])`, **Then** a subscribe message is sent and the returned promise resolves with the `subscription_ack` (subscribed channels and count).
3. **Given** active subscriptions, **When** the server broadcasts a message on a subscribed channel, **Then** the SDK emits a typed event with the channel name, payload data, sequence number, and timestamp.
4. **Given** active subscriptions, **When** the developer calls `unsubscribe(["tenant.BTC.trade"])`, **Then** an unsubscribe message is sent and the returned promise resolves with the `unsubscription_ack`.

### Scenario 2 - Publish Messages (Priority: P1)

A developer publishes data to a channel through the WebSocket connection.

**Acceptance Criteria**:
1. **Given** a connected client, **When** the developer calls `publish("tenant.BTC.trade", { price: 50000 })`, **Then** a publish message is sent and the returned promise resolves with the `publish_ack`.
2. **Given** a connected client, **When** a publish fails (rate limited, forbidden, etc.), **Then** the returned promise rejects with a typed error containing the machine-readable `code` and human-readable `message`.

### Scenario 3 - Automatic Reconnection with Message Replay (Priority: P1)

A developer's application loses connectivity. When the connection is restored, the SDK reconnects and replays missed messages without developer intervention.

**Acceptance Criteria**:
1. **Given** a connected client with active subscriptions, **When** the WebSocket connection drops unexpectedly, **Then** the SDK automatically attempts to reconnect using exponential backoff.
2. **Given** the SDK is reconnecting, **When** a new connection is established, **Then** the SDK re-authenticates, re-subscribes to all previously subscribed channels, and sends a `reconnect` request with the last known offsets to replay missed messages.
3. **Given** a reconnect replay completes, **When** replayed messages arrive, **Then** the SDK emits them through the same message event handler as live messages, preserving ordering.
4. **Given** the SDK is reconnecting, **When** the maximum number of reconnection attempts is reached, **Then** the SDK emits a connection failure event and stops retrying.

### Scenario 4 - Sequence Gap Detection (Priority: P2)

A developer wants to detect when messages were dropped (slow client disconnection) so their application can take corrective action.

**Acceptance Criteria**:
1. **Given** a connected client receiving messages, **When** a gap in sequence numbers is detected (e.g., seq 5 → seq 8), **Then** the SDK emits a gap event with the expected and received sequence numbers.
2. **Given** a gap event, **When** the developer has configured automatic reconnect-on-gap, **Then** the SDK triggers a reconnect with replay to fill the gap.

### Scenario 5 - Heartbeat and Connection Health (Priority: P2)

The SDK maintains connection liveness through heartbeats and exposes connection health to the developer.

**Acceptance Criteria**:
1. **Given** a connected client, **When** a configurable heartbeat interval elapses, **Then** the SDK sends a heartbeat message and expects a `pong` response.
2. **Given** a connected client, **When** no pong response is received within a configurable timeout, **Then** the SDK considers the connection dead and initiates reconnection.
3. **Given** a connected client, **When** a pong response is received, **Then** the SDK exposes the server timestamp and computed latency.

### Scenario 6 - Token Refresh (Priority: P2)

The developer provides a token refresh callback so the SDK can maintain long-lived connections without disruption when JWTs expire.

**Acceptance Criteria**:
1. **Given** a client configured with a `getToken` callback, **When** the current JWT is ~30 seconds from expiry, **Then** the SDK proactively calls `getToken` and sends the new token over the existing connection without disconnecting.
2. **Given** a client reconnecting after a drop, **When** a new connection is being established, **Then** the SDK calls `getToken` to obtain a fresh token for the new connection (lazy fallback).
3. **Given** a client whose `getToken` callback throws an `UnauthorizedError`, **When** the SDK receives the error, **Then** it emits an auth failure event and disconnects permanently (no retry).
4. **Given** a client whose `getToken` callback fails transiently (network error), **When** the SDK receives the error, **Then** it retries with exponential backoff before giving up.

### Scenario 7 - Error Handling (Priority: P2)

The developer receives structured, typed errors for all failure modes.

**Acceptance Criteria**:
1. **Given** any operation (subscribe, unsubscribe, publish, reconnect), **When** the server responds with an error, **Then** the SDK surfaces a typed error object with `type`, `code`, and `message` fields.
2. **Given** a connected client, **When** the server sends a generic `error` response (e.g., `invalid_json`), **Then** the SDK emits a protocol error event.
3. **Given** a connected client, **When** the server closes the connection with code 1008 (slow client), **Then** the SDK identifies this as a policy disconnection and emits a specific event before initiating reconnection.

### Edge Cases

- What happens when `subscribe()` is called before `connect()` completes? The SDK MUST queue the operation and execute it after connection is established.
- What happens when the JWT expires during an active session? The SDK proactively calls the developer-provided `getToken` async callback ~30 seconds before JWT `exp`. The new token is sent over the existing connection with no disconnection. On reconnect, `getToken` is also called as a fallback to ensure a fresh token. If `getToken` throws an `UnauthorizedError`, the SDK disconnects permanently and emits an auth failure event.
- What happens when the developer subscribes to channels they're not authorized for? The server silently filters unauthorized channels from the `subscription_ack` — the SDK MUST surface the difference between requested and confirmed channels.
- What happens when `publish()` is called while disconnected? The SDK MUST reject the promise immediately with a "not connected" error (no queuing — publish ordering semantics require live connections).

## Requirements

### Functional Requirements

- **FR-001**: SDK MUST establish WebSocket connections with JWT authentication via query parameter or Authorization header.
- **FR-002**: SDK MUST accept an optional async `getToken` callback that returns a fresh JWT string. The SDK MUST call it proactively before token expiry (~30s before `exp`) and on every reconnect attempt.
- **FR-003**: SDK MUST support `subscribe`, `unsubscribe`, `publish`, `reconnect`, and `heartbeat` message types as defined in the AsyncAPI spec.
- **FR-004**: SDK MUST parse all server response types (`message`, `subscription_ack`, `unsubscription_ack`, `publish_ack`, `publish_error`, `reconnect_ack`, `reconnect_error`, `pong`, `error`, `subscribe_error`, `unsubscribe_error`) into typed objects.
- **FR-005**: SDK MUST track per-connection sequence numbers and detect gaps in broadcast messages.
- **FR-006**: SDK MUST automatically reconnect on unexpected disconnection with configurable exponential backoff (initial delay, max delay, max attempts).
- **FR-007**: SDK MUST re-subscribe to all active channels and request message replay on reconnection.
- **FR-008**: SDK MUST send periodic heartbeats and detect connection staleness.
- **FR-009**: SDK MUST expose an event-driven API (EventEmitter pattern or similar) for all asynchronous server messages.
- **FR-010**: SDK MUST provide promise-based APIs for request/response operations (subscribe, unsubscribe, publish).
- **FR-011**: SDK MUST track subscription state locally (set of subscribed channels) and expose it to the developer.
- **FR-012**: SDK MUST work in both browser and Node.js environments.
- **FR-013**: SDK MUST export full TypeScript type definitions for all protocol messages, error codes, and configuration options.

### Non-Functional Requirements

- **NFR-001**: SDK bundle size MUST be under 15KB gzipped (no heavy dependencies).
- **NFR-002**: SDK MUST have zero runtime dependencies beyond the WebSocket API (browser-native `WebSocket` or a Node.js polyfill provided by the consumer).
- **NFR-003**: SDK MUST support tree-shaking — unused features (e.g., publish, reconnect) MUST be eliminable by bundlers.
- **NFR-004**: SDK MUST target ES2020+ and provide both ESM and CJS module formats.
- **NFR-005**: SDK MUST achieve >90% test coverage for protocol handling, reconnection logic, and error paths.
- **NFR-006**: SDK API MUST be stable — breaking changes require major version bumps following semver.
- **NFR-007**: SDK MUST not leak memory on long-lived connections (event listener cleanup, subscription tracking cleanup on disconnect).

### Key Entities

- **SukkoClient**: The main SDK entry point. Manages connection lifecycle, authentication, and exposes subscribe/unsubscribe/publish/heartbeat methods.
- **Channel**: A string in `{tenant}.{identifier}.{category}` format representing a data stream.
- **Message**: A broadcast message received on a subscribed channel, containing `seq`, `ts`, `channel`, and `data`.
- **ErrorCode**: A machine-readable string enum (`invalid_json`, `invalid_request`, `invalid_channel`, `message_too_large`, `rate_limited`, `forbidden`, `topic_not_provisioned`, `service_unavailable`, `not_available`, `replay_failed`, `publish_failed`).
- **ConnectionState**: The client's lifecycle state (`disconnected`, `connecting`, `connected`, `reconnecting`).
- **ClientOptions**: Configuration for the client (URL, token, heartbeat interval, reconnect policy, etc.).

## Success Criteria

- **SC-001**: A developer can go from `npm install` to receiving live messages in under 10 lines of code.
- **SC-002**: SDK correctly handles all 11 server response types as defined in the AsyncAPI spec.
- **SC-003**: Automatic reconnection with replay recovers from network disruptions without data loss (within Kafka retention window and replay limits).
- **SC-004**: SDK passes integration tests against a live Odin WS instance (subscribe, receive, publish, reconnect flows).
- **SC-005**: SDK is published to npm with full TypeScript type definitions, README with usage examples, and API reference documentation.
- **SC-006**: SDK is published to the public npm registry under `@sukko/ws-sdk` with full TypeScript type definitions, README with usage examples, and API reference documentation.

## Out of Scope

- **Server-side SDK**: This spec covers client-side TypeScript only. A Node.js backend SDK for server-to-server communication is a separate effort.
- **React/Vue/Angular bindings**: Framework-specific hooks or components (e.g., `useSukkoChannel()`) are follow-up work that builds on this core SDK.
- **Admin/provisioning API client**: The SDK connects to the WebSocket endpoint only, not the provisioning REST API.
- **Offline message queuing**: Messages published while disconnected are rejected, not queued. Offline queuing introduces ordering complexity that is out of scope.
- **Custom serialization**: The SDK uses JSON. Binary protocols (MessagePack, Protobuf) are not supported.
- **WebSocket polyfill bundling**: The SDK uses the native `WebSocket` API. Consumers in environments without native support (e.g., older Node.js) must provide their own polyfill.
- **Codegen pipeline**: Auto-generation of TypeScript protocol types from the AsyncAPI spec in odin-ws is a separate infrastructure task, not part of this SDK spec.

## Clarifications

- Q: Should the SDK accept a token refresh callback or emit an event for manual re-auth? → A: **Both — proactive `getToken` callback with lazy fallback on reconnect.** SDK reads JWT `exp` and calls the developer-provided async `getToken` ~30s before expiry to send a new token over the existing connection (no disconnection). On reconnect, `getToken` is also called as a safety net. Throwing `UnauthorizedError` from the callback signals permanent auth failure. This follows the industry standard pattern used by Ably (`authCallback`) and Centrifugo (`getToken`).
- Q: Should the SDK be published to a public or private npm registry? → A: **Public npm under `@sukko/ws-sdk`.**
- Q: Should the SDK live in this monorepo or a separate repository? → A: **Separate repository as a sibling of odin-ws, with TypeScript protocol types auto-generated from the AsyncAPI spec in this monorepo.** Independent versioning/CI while keeping protocol types in sync via codegen.
- Q: What is the public product name for the WebSocket SaaS? → A: **Sukko.** All public SDK types use `Sukko` prefix: `SukkoClient`, `SukkoEvents`, `SukkoError`. Internal platform remains "Odin WS".
