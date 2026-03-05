# Research: Mid-Connection Auth Refresh

**Branch**: `feat/auth-refresh` | **Date**: 2026-02-26

## R1. Proxy Claims Mutability

**Question**: The Proxy struct's `claims` and `permissions` fields are set once at creation and never updated. How should auth refresh update them safely?

**Finding**: The Proxy has two goroutines: `proxyClientToBackend()` (reads from client, writes to backend) and `proxyBackendToClient()` (reads from backend, writes to client). Both read `p.claims` and `p.permissions`. The `permissions` field is a shared `*PermissionChecker` (stateless — takes claims as parameter). The `claims` field is per-connection.

**Decision**: Protect `claims` with `sync.RWMutex`.
- `proxyClientToBackend()` already serializes client→backend operations (single goroutine reading sequentially). Auth refresh runs in this goroutine, so writes to `claims` don't race with subscribe filtering — they're in the same goroutine.
- `proxyBackendToClient()` doesn't read `claims` today, but subscription tracking (R2) adds reads there. A `sync.RWMutex` ensures safe concurrent access.
- `permissions` doesn't need swapping — it's a shared stateless checker. Only claims change.

**Alternative considered**: `atomic.Pointer[auth.Claims]` — simpler for reads, but the auth refresh operation (validate token + swap claims + re-evaluate subscriptions) is a multi-step transaction that benefits from a mutex to prevent interleaved subscribe operations.

## R2. Backend→Client Subscription Tracking

**Question**: The Proxy needs to track subscribed channels for forced unsubscription. Currently `proxyBackendToClient()` is pure pass-through. How to add interception without impacting broadcast performance?

**Finding**: 99%+ of backend→client messages are broadcasts (`{"type":"message",...}`). Full JSON parsing every message is wasteful. The `subscription_ack` and `unsubscription_ack` responses are infrequent (only on subscribe/unsubscribe requests).

**Decision**: Use a fast byte prefix check before JSON parsing:
- Broadcasts always start with `{"type":"message"` — skip immediately.
- Only parse responses that contain `"_ack"` or `"_error"` in the first 80 bytes.
- Track confirmed channels from `subscription_ack` (add) and `unsubscription_ack` (remove).
- The subscription set uses a `map[string]struct{}` protected by the same `claimsMu` RWMutex used for claims (auth refresh and subscription tracking are correlated operations).

**Alternative considered**: Track subscriptions only in `proxyClientToBackend()` by recording what the proxy forwards. Rejected because the server could reject or modify the subscription (subscribe_error), so we must use the server's ack as source of truth.

## R3. Serialization of Auth with Subscribe/Unsubscribe

**Question**: How to serialize auth refresh with subscribe/unsubscribe operations on the client→backend path?

**Finding**: `proxyClientToBackend()` is a single goroutine that reads frames sequentially. `interceptClientMessage()` dispatches by type. Subscribe, unsubscribe, and auth refresh all run in this same goroutine. They are **already serialized** by the single-goroutine read loop.

**Decision**: No additional serialization mechanism needed. The existing single-goroutine design on the client→backend path provides natural serialization. Auth refresh (intercepted before forwarding) completes before the next message is read. This is the simplest correct approach.

## R4. Validator Access in Proxy

**Question**: The Proxy doesn't have access to the JWT validator (it's on the Gateway struct). Auth refresh needs to validate the new token.

**Finding**: The `MultiTenantValidator` is created once at gateway startup and stored as `gw.validator`. It's thread-safe (uses sync.RWMutex internally for key cache). The Proxy's `ProxyConfig` already passes gateway-owned resources (e.g., `Permissions`).

**Decision**: Add `Validator` field to `ProxyConfig` and store on the Proxy struct. Pass `gw.validator` when creating the proxy. The validator is shared and thread-safe — no ownership issues.

## R5. Forced Unsubscription Mechanism

**Question**: When auth refresh reduces permissions, how does the gateway force-unsubscribe the client from revoked channels?

**Finding**: The gateway proxy intercepts client→backend messages. For forced unsubscription, the gateway needs to:
1. Send an `unsubscribe` message to the backend (server-side subscription removal)
2. Send an `unsubscription_ack` to the client (client-side notification)

The proxy already has `sendToClient()` (mutex-protected write to client) and `forwardFrame()` (write to backend). Both are available for constructing synthetic messages.

**Decision**: Before swapping claims, diff the tracked subscription set against the new permissions (two phases — collect under lock, I/O after unlock):
1. Lock `claimsMu`. For each tracked channel, call `p.permissions.CanSubscribe(newClaims, strippedChannel)`.
2. Collect channels that are no longer permitted.
3. Remove revoked channels from local tracking. Unlock `claimsMu`.
4. Send a synthetic `unsubscribe` message to the backend for revoked channels (fire-and-forget — no waiting for backend ack).
5. Send `unsubscription_ack` to the client listing revoked channels.

Fire-and-forget rationale: waiting for backend ack would block the client→backend path and hold the mutex across I/O (Constitution VII violation). The backend will send its own `unsubscription_ack` which the subscription tracker ignores (channels already removed).

**Edge case**: If the backend unsubscribe fails, the client still receives the revoked channels' broadcasts until the next message cycle. This is acceptable — the permission change is best-effort and the window is brief.

## R6. Protocol Type Location — Server-Only Types

**Question**: Which protocol types are truly server-only and safe to move?

**Finding**: Comprehensive grep across gateway/ and server/ packages confirms 15 server-only symbols. See spec clarifications for the full list. No file in `ws/internal/server/` currently defines protocol types — `protocol.go` will be a new file.

**Decision**: Create `ws/internal/server/protocol.go` with all server-only message types, response types, error codes, and sentinel errors. The shared package retains only types referenced by both gateway and server.

**Import impact**: Only `handlers_message.go` and `handlers_publish.go` in the server package import `shared/protocol`. After migration, they import from their own package for server-only types and continue importing `shared/protocol` for truly shared types.

## R7. Prior Art Research — Mid-Connection Auth Refresh

**Question**: How do established real-time/WebSocket services handle token refresh on live connections? (Constitution XI compliance)

**Finding**: Six reference services were researched. They split into two camps:

**Camp 1 — True mid-connection refresh (2 of 6):**

- **Ably**: Supports an `AUTH` protocol message. The client sends a new token over the existing connection. On success, capabilities are updated in-place. On permission downgrade, channels transition to `FAILED` state and the client receives detach events. Ably also supports a server-side "auth callback" where the server proactively requests a new token from the client before expiry.
- **Centrifugo**: Supports `refresh` (connection-level) and `sub_refresh` (per-subscription) commands. Three failure modes: `UnauthorizedError` (disconnect), transient error (retry with backoff), empty token (keep using current). Server-side refresh proxy available — the server calls an application backend to validate the refresh. Separate token lifetimes for connection vs. individual subscriptions.

**Camp 2 — Reconnect to refresh (4 of 6):**

- **Pusher Channels**: No mid-connection refresh. Token refresh requires disconnect/reconnect. GitHub issue #530 documents user requests for this feature.
- **Socket.IO**: Auth only during handshake. Refresh via `socket.disconnect().connect()`. The `auth` option is evaluated only on initial connection.
- **Phoenix Channels**: Auth via `connect/3` callback only. Token updated via params on reconnect. No in-flight token swap.
- **NATS WebSocket**: Auth via CONNECT handshake only. `UserJWT` callback invoked on reconnect. No mid-connection re-auth.

**Edge cases identified from mature implementations:**

1. **Proactive refresh before expiry** — Ably and Centrifugo support server-initiated refresh requests before token expiry, avoiding the race between expiry and client-initiated refresh.
2. **Connection vs. subscription tokens** — Centrifugo separates connection tokens from per-subscription tokens with independent lifetimes.
3. **Three failure modes** — Centrifugo distinguishes: permanent auth failure (disconnect), transient failure (retry), and empty token (keep current).
4. **Server-initiated re-auth** — Ably's auth callback lets the server request a fresh token from the client proactively.
5. **Permission downgrade handling** — Ably transitions affected channels to FAILED state rather than silently unsubscribing.
6. **Concurrent refresh coalescing** — Mature clients coalesce multiple refresh attempts into one to avoid duplicate work.
7. **Token during reconnection** — When a connection drops during refresh, the client must decide which token to use for reconnect (always use the latest attempted).
8. **Backoff with jitter** — Failed refresh retries use exponential backoff with jitter to avoid thundering herd.
9. **Server-side liveness proxy** — Centrifugo's refresh proxy delegates token validation to the application backend, keeping the WebSocket server stateless.
10. **Atomic permission application** — Permission changes from refresh should be applied atomically — no window where old permissions are partially active.

**Decision**: Sukko's design aligns with Camp 1 (Ably/Centrifugo pattern). Key design choices informed by prior art:
- Gateway-side interception (like Centrifugo's refresh proxy — WebSocket server stays auth-unaware).
- Forced unsubscription on permission downgrade (similar to Ably's channel FAILED transition, but using `unsubscription_ack` for consistency with existing protocol).
- Fire-and-forget backend unsub (Sukko-specific — Ably/Centrifugo own their subscription state; Sukko's gateway proxies to a separate backend).
- No server-initiated refresh in v1 (documented in spec Out of Scope — can be added later following Ably's auth callback pattern).
- No per-subscription tokens (Sukko uses connection-level claims with channel-level permission checks, simpler than Centrifugo's model).
