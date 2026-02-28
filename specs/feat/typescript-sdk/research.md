# Research: TypeScript SDK for Sukko WS

**Branch**: `feat/typescript-sdk` | **Date**: 2026-02-26

## R1. Token Refresh — Server Protocol Gap

**Question**: The spec says "send the new token over the existing connection without disconnecting." Does the server support this?

**Finding**: **No.** The Sukko WS gateway authenticates JWT only at WebSocket upgrade time (`proxy.go`). There is no `auth` message type in the protocol. Once connected, the tenant identity is fixed for the session lifetime.

**Decision**: Use **proactive graceful reconnect** for token refresh.
- SDK decodes JWT `exp` client-side (base64 decode, no signature verification needed)
- ~30s before expiry, SDK calls `getToken` to obtain fresh JWT
- SDK closes existing connection cleanly and reconnects with the new token
- SDK re-subscribes to all channels and sends `reconnect` for message replay
- From the developer's perspective, this is seamless — the reconnect flow already handles re-subscription and replay

**Alternatives considered**:
1. Add server-side `auth` message type — out of scope for SDK, requires server changes
2. Wait for token to expire and let reconnect handle it — causes avoidable errors and brief outages
3. Only refresh on reconnect (lazy-only) — acceptable for short-lived tokens but problematic for long sessions

**Future**: If the server adds mid-connection auth refresh, the SDK can adopt it without API changes (same `getToken` callback, different internal mechanism).

## R2. WebSocket API Compatibility (Browser vs Node.js)

**Question**: How to support both browser-native `WebSocket` and Node.js without bundling a polyfill?

**Finding**: Node.js 22+ includes a global `WebSocket` class (based on `undici`). For Node.js 18-20, consumers must provide `ws` or similar. Browser environments have `WebSocket` natively.

**Decision**: Accept an optional `WebSocket` constructor in `ClientOptions`. Default to `globalThis.WebSocket`. If not available, throw a clear error telling the consumer to provide one.

```typescript
interface ClientOptions {
  url: string;
  token?: string;
  getToken?: () => Promise<string>;
  WebSocket?: typeof WebSocket; // Optional override for Node.js < 22
  // ...
}
```

**Rationale**: This is what Centrifugo, Ably, and Socket.IO do. Zero dependency, maximum flexibility.

## R3. Build Tooling

**Question**: What build tool for dual ESM/CJS output with TypeScript declarations?

**Finding**: `tsdown` (successor to `tsup`, built on Rolldown/Rust) is the current best choice for TypeScript libraries. Fast DTS generation via Oxc with `isolatedDeclarations`. `tsup` is effectively abandoned.

**Decision**: Use **tsdown** with dual ESM/CJS output.

## R4. Testing Framework

**Question**: How to test a WebSocket client library?

**Finding**: Vitest is the standard for TypeScript libraries. `vitest-websocket-mock` provides a mock WebSocket server for unit tests. MSW v2 supports WebSocket mocking for integration tests.

**Decision**: Use **Vitest** + **vitest-websocket-mock** for unit tests. Integration tests against a live Sukko WS instance as a separate test suite.

## R5. Linting/Formatting

**Question**: Biome vs ESLint+Prettier?

**Finding**: Biome is 15-25x faster, single tool, single config file. Covers ~85% of ESLint rules. Sufficient for a WebSocket client SDK.

**Decision**: Use **Biome** for linting and formatting.

## R6. Promise-Based Request/Response Correlation

**Question**: How to match server responses to client requests when the protocol has no request ID?

**Finding**: The Sukko WS protocol does NOT include request IDs in messages. Subscribe sends channels, and the ack returns the subscribed channels. Publish sends a channel, and the ack returns the same channel. There's no correlation ID.

**Decision**: Use **implicit correlation by operation type**:
- Only one pending subscribe/unsubscribe/reconnect at a time (serialize these operations)
- Publish can have multiple in-flight if we track by channel, but the protocol only returns channel in the ack — so serialize publish per channel or use a single pending publish queue
- Heartbeat/pong is fire-and-forget with a timeout

**Implementation**: Internal operation queue that serializes request/response pairs. When a subscribe is sent, the next `subscription_ack` or `subscribe_error` resolves the pending promise. This matches how the Go loadtest client works (single-threaded read loop dispatching by type).

## R7. Reconnect Offset Tracking

**Question**: How does the SDK track Kafka offsets for reconnect replay?

**Finding**: The server's `reconnect` message expects `client_id` (string) and `last_offset` (map of topic→offset). The server broadcasts messages with `seq` (connection-scoped sequence number) and `ts` (timestamp), but NOT Kafka offsets. The offset information is internal to the server.

**Decision**: The reconnect feature requires the **server to provide offset information** that the client can echo back. Looking at the protocol more carefully:
- The `reconnect` message sends `last_offset` which is a map of Kafka topics to offsets
- But the `message` envelope doesn't include Kafka offsets — only `seq` and `ts`
- The `client_id` in the reconnect request is a persistent identifier the client maintains

**Implementation approach**:
- The SDK generates a persistent `clientId` (UUID) stored in memory (or optionally localStorage in browsers)
- For offset tracking, the SDK cannot track Kafka offsets because they're not exposed in broadcast messages
- The SDK can use `seq` numbers and `ts` timestamps as the client's view of progress
- The reconnect feature may need server-side tracking (server remembers last delivered offset per client_id)
- For v1: expose reconnect as an explicit API but note that offset tracking depends on server-side state

This is an area where the SDK's reconnect replay feature depends on how the server tracks per-client state. The SDK will send the reconnect message with whatever offset data it has, and the server handles the replay.
