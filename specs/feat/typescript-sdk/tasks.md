# Tasks: TypeScript SDK for Sukko WS (`@sukko/ws-sdk`)

**Branch**: `feat/typescript-sdk` | **Generated**: 2026-02-26
**Plan**: `specs/feat/typescript-sdk/plan.md`
**Repo**: `../sukko-ws-sdk/` (sibling of `sukko`)

---

## Phase 1: Project Scaffolding

> Initialize the repository, install dependencies, verify the build toolchain works.

- [ ] T001 Create repository directory `../sukko-ws-sdk/` with `git init`, create `.gitignore` (node_modules, dist, coverage, *.tgz)
- [ ] T002 [P] Create `../sukko-ws-sdk/package.json` with `@sukko/ws-sdk` name, dual ESM/CJS exports, scripts, and devDependencies as specified in plan section 1.2
- [ ] T003 [P] Create `../sukko-ws-sdk/tsconfig.json` with strict mode, `module: "nodenext"`, `isolatedDeclarations: true`, target ES2022 as specified in plan section 1.4
- [ ] T004 [P] Create `../sukko-ws-sdk/tsconfig.build.json` extending `tsconfig.json`, include only `src/`, exclude tests
- [ ] T005 [P] Create `../sukko-ws-sdk/tsdown.config.ts` with entry `src/index.ts`, dual format ESM+CJS, dts, sourcemap as specified in plan section 1.3
- [ ] T006 [P] Create `../sukko-ws-sdk/biome.json` with recommended rules, single quotes, trailing commas, semicolons as specified in plan section 1.5
- [ ] T007 [P] Create `../sukko-ws-sdk/vitest.config.ts` with v8 coverage provider, 90% thresholds, test timeout 10s as specified in plan section 1.6
- [ ] T008 Run `npm install` in `../sukko-ws-sdk/` to install all devDependencies. Verify `npx tsdown --version` and `npx vitest --version` succeed.
- [ ] T009 Create placeholder `../sukko-ws-sdk/src/index.ts` exporting an empty object. Run `npm run build` to verify tsdown produces `dist/index.js`, `dist/index.cjs`, `dist/index.d.ts`, `dist/index.d.cts`. Run `npm run lint` to verify Biome works.

**Phase 1 verification**: `npm run build` succeeds, `dist/` contains 4 files (`.js`, `.cjs`, `.d.ts`, `.d.cts`).

---

## Phase 2: Protocol Layer (Types, Errors, Codec, Token)

> Pure modules with zero dependencies on WebSocket. Fully testable in isolation.

- [ ] T010 Create `../sukko-ws-sdk/src/types.ts` with all public type definitions: `ConnectionState`, `ClientOptions`, `ReconnectOptions`, `WebSocketConstructor`, all request types (`SubscribeRequest`, `UnsubscribeRequest`, `PublishRequest`, `ReconnectRequest`), all response types (`Message`, `SubscriptionAck`, `UnsubscriptionAck`, `PublishAck`, `ReconnectAck`, `Pong`), `ErrorCode` union type, `ErrorResponse`, and `SukkoEvents` interface. Copy types exactly from plan section 2.1.
- [ ] T011 [P] Create `../sukko-ws-sdk/src/errors.ts` with three error classes: `SukkoError` (code + message + type), `UnauthorizedError`, `NotConnectedError`. Import `ErrorCode` from `types.ts`. As specified in plan section 2.2.
- [ ] T012 [P] Create `../sukko-ws-sdk/src/protocol.ts` implementing: `encodeSubscribe(channels)`, `encodeUnsubscribe(channels)`, `encodePublish(channel, data)`, `encodeReconnect(clientId, offsets)`, `encodeHeartbeat()` — each returns a JSON string with `{type, data}` envelope. Implement `decodeServerMessage(raw)` returning a discriminated union by `type` field covering all 11 server response types. Include `validateChannel(channel)` helper that enforces min 3 dot-separated non-empty parts. As specified in plan section 2.3.
- [ ] T013 [P] Create `../sukko-ws-sdk/src/token.ts` implementing: `decodeJwtExp(token): number | null` — base64url-decode the JWT payload (second segment), parse JSON, return `exp` claim or null. `scheduleRefresh(expUnix, callback): ReturnType<typeof setTimeout>` — schedule callback 30s before exp (or immediately if <30s remaining). `clearRefresh(timer)` — clear the scheduled timer. As specified in plan section 2.4.

**Phase 2 verification**: All four files compile with `npx tsc --noEmit`. No WebSocket dependency.

---

## Phase 3: Connection Layer

> WebSocket lifecycle, subscription state, and reconnection logic. Depends on Phase 2 types.

- [ ] T014 Create `../sukko-ws-sdk/src/connection.ts` implementing a `Connection` class. Constructor accepts `url`, `token`, `WebSocketConstructor`, `heartbeatInterval`, `heartbeatTimeout`, and callbacks: `onMessage(raw: string)`, `onOpen()`, `onClose(code: number, reason: string)`, `onError(err: Event)`. Methods: `open()` — creates WebSocket to `url?token=...`, wires event handlers, starts heartbeat timer on open. `send(data: string)` — sends string to WebSocket. `close()` — stops heartbeat, closes WebSocket with code 1000. Heartbeat: sends `encodeHeartbeat()` every `heartbeatInterval` ms, sets a `heartbeatTimeout` timer expecting pong. If pong not received, calls `onClose` with synthetic timeout. Exposes `readyState`. As specified in plan section 3.1.
- [ ] T015 Create `../sukko-ws-sdk/src/subscription.ts` implementing a `SubscriptionManager` class. Tracks: `channels: Set<string>` (subscribed channels), `expectedSeq: number` (starts at 1). Methods: `addChannels(subscribed: string[])` — add to set. `removeChannels(unsubscribed: string[])` — remove from set. `checkSequence(seq: number): {gap: boolean, expected: number} | null` — compare with expectedSeq, increment, return gap info if seq > expected. `getFilteredChannels(requested: string[], confirmed: string[]): string[]` — return channels in requested but not in confirmed. `reset()` — clear channels and reset expectedSeq to 1. `readonly subscriptions: ReadonlySet<string>`. As specified in plan section 3.2.
- [ ] T016 Create `../sukko-ws-sdk/src/reconnect.ts` implementing a `ReconnectManager` class. Constructor accepts `ReconnectOptions` (with defaults: initialDelay=1000, maxDelay=30000, maxAttempts=Infinity, multiplier=2, jitter=0.1). Methods: `schedule(callback: () => Promise<void>): void` — schedule reconnect attempt with exponential backoff delay `min(initialDelay * multiplier^attempt, maxDelay) * (1 + random * jitter)`. Increment attempt counter. If attempt >= maxAttempts, call `onMaxAttemptsReached` callback instead. `cancel()` — clear pending timeout, do NOT reset attempt counter. `reset()` — reset attempt counter to 0 (call after successful connect). `readonly attempts: number`. As specified in plan section 3.3.

**Phase 3 verification**: All three files compile. `Connection` can be instantiated with a mock WebSocket constructor.

---

## Phase 4: Client API

> Compose all layers into the public `SukkoClient` class. Depends on Phases 2 and 3.

- [ ] T017 Create `../sukko-ws-sdk/src/client.ts` implementing the `SukkoClient` class. Constructor validates `ClientOptions` (url required, token or getToken required, heartbeat/reconnect defaults). Internal components: `Connection`, `SubscriptionManager`, `ReconnectManager`. Implements typed event emitter (`on`/`off`/`emit` for `SukkoEvents`). State machine: `disconnected → connecting → connected → reconnecting → connected|disconnected`. As specified in plan section 4.1.
- [ ] T018 Implement `SukkoClient.connect()` in `../sukko-ws-sdk/src/client.ts`: If `getToken` provided, call it to obtain token; else use static `token`. Create `Connection` with obtained token. On open: set state to `connected`, flush queued operations, decode JWT exp and schedule token refresh timer. On close (unexpected): set state to `reconnecting`, delegate to `ReconnectManager`. On close (1008 slow client): emit specific event, then reconnect. Return promise that resolves when connection opens or rejects on failure.
- [ ] T019 Implement `SukkoClient.subscribe()` and `SukkoClient.unsubscribe()` in `../sukko-ws-sdk/src/client.ts`: If not connected, queue operation. Otherwise, encode message via `protocol.ts`, send via `Connection`, return promise. On `subscription_ack`: update `SubscriptionManager`, compare requested vs confirmed channels (emit `channelsFiltered` if mismatch), resolve promise. On `subscribe_error`: reject with `SukkoError`. Same pattern for unsubscribe with `unsubscription_ack`/`unsubscribe_error`. Serialize operations via internal promise queue (one at a time).
- [ ] T020 Implement `SukkoClient.publish()` in `../sukko-ws-sdk/src/client.ts`: If not connected, reject with `NotConnectedError`. Validate channel format via `protocol.validateChannel()`. Encode and send. Return promise that resolves on `publish_ack` or rejects on `publish_error` with `SukkoError`.
- [ ] T021 Implement message dispatch in `../sukko-ws-sdk/src/client.ts`: Wire `Connection.onMessage` to `protocol.decodeServerMessage`. Switch on `type` field and route to: `message` → check sequence via `SubscriptionManager` (emit `gap` if detected, trigger reconnect if `reconnectOnGap` enabled) → emit `message` event. `pong` → compute latency (`Date.now() - sentAt`), emit `pong` event. `error` → emit `error` event. All ack/error types → resolve/reject corresponding pending promise.
- [ ] T022 Implement reconnect flow in `../sukko-ws-sdk/src/client.ts`: On unexpected close: `ReconnectManager.schedule()` → callback calls `getToken()` (if provided, emit `tokenRefresh('reconnect')`) → create new `Connection` with fresh token → on open: re-subscribe to all `SubscriptionManager.subscriptions` → send `reconnect` message with last known offsets → on `reconnect_ack`: set state to `connected`, emit `stateChange`, `ReconnectManager.reset()`. On max attempts: emit `connectionError`, set state to `disconnected`.
- [ ] T023 Implement token refresh in `../sukko-ws-sdk/src/client.ts`: After connect, decode JWT `exp` via `token.decodeJwtExp()`. Schedule refresh via `token.scheduleRefresh()`. On timer fire: emit `tokenRefresh('expiry')` → call `getToken()` → if success: initiate graceful reconnect (close current, reconnect with new token, re-subscribe, replay). If `getToken` throws `UnauthorizedError`: emit `connectionError`, disconnect permanently, do not retry. If transient failure: retry `getToken` up to 3 times with 1s/2s/4s delays, then emit `connectionError`.
- [ ] T024 Implement `SukkoClient.disconnect()` in `../sukko-ws-sdk/src/client.ts`: Cancel any pending reconnect (`ReconnectManager.cancel()`). Clear token refresh timer. Close connection with code 1000. Set state to `disconnected`. Clean up all event listeners and pending promises (reject with `NotConnectedError`).
- [ ] T025 Create `../sukko-ws-sdk/src/index.ts` barrel export: export `SukkoClient` class, `SukkoError`, `UnauthorizedError`, `NotConnectedError` as value exports. Export all type definitions from `types.ts` as type-only exports. As specified in plan section 4.2.
- [ ] T026 Run `npm run build` in `../sukko-ws-sdk/`. Verify `dist/` contains `.js`, `.cjs`, `.d.ts`, `.d.cts`. Verify `SukkoClient` is importable from both ESM and CJS entry points. Run `npm run lint` to verify no Biome violations.

**Phase 4 verification**: `npm run build` succeeds. `dist/index.d.ts` exports `SukkoClient` class and all public types.

---

## Phase 5: Testing

> Unit tests for each module, integration tests for the composed client.

- [ ] T027 Create `../sukko-ws-sdk/tests/helpers/mock-server.ts` wrapping `vitest-websocket-mock` with Sukko WS protocol helpers: `expectSubscribe(channels)` → waits for subscribe message, auto-responds with `subscription_ack`. `expectUnsubscribe(channels)` → same for unsubscribe. `sendMessage(channel, data, seq, ts)` → sends typed broadcast message. `sendError(type, code, message)` → sends error response. `sendPong(ts)` → sends pong.
- [ ] T028 [P] Create `../sukko-ws-sdk/tests/protocol.test.ts` with table-driven tests: encode every message type and verify JSON output. Decode every server response type and verify typed output. Decode invalid JSON → throws. `validateChannel` — valid (`a.b.c`), too few parts (`a.b`), empty parts (`a..c`), single part (`a`) → appropriate results.
- [ ] T029 [P] Create `../sukko-ws-sdk/tests/token.test.ts` with tests: decode valid JWT with exp → returns exp value. Decode JWT without exp → returns null. Decode malformed base64 → returns null. Decode non-JSON payload → returns null. `scheduleRefresh` with exp 60s in future → schedules at ~30s. `scheduleRefresh` with exp 10s in future → schedules immediately. Use `vi.useFakeTimers()` for scheduling tests.
- [ ] T030 [P] Create `../sukko-ws-sdk/tests/subscription.test.ts` with tests: `addChannels` tracks channels correctly. `removeChannels` removes from set. `checkSequence` with sequential seq → no gap. `checkSequence` with gap (1,2,5) → reports gap with expected=3, received=5. `getFilteredChannels` with mismatch → returns filtered list. `reset` clears channels and resets seq.
- [ ] T031 Create `../sukko-ws-sdk/tests/connection.test.ts` with tests using `vitest-websocket-mock`: connect with token appended as query param. Send message via `send()`. Receive message triggers `onMessage` callback. Heartbeat sent at interval. Pong received resets heartbeat timeout. No pong within timeout → triggers `onClose`. Close with code 1008 → `onClose` called with 1008. Normal close with code 1000. Use `vi.useFakeTimers()` for heartbeat tests.
- [ ] T032 Create `../sukko-ws-sdk/tests/reconnect.test.ts` with tests using `vi.useFakeTimers()`: first attempt delay = initialDelay. Second attempt = initialDelay * multiplier. Delay capped at maxDelay. Jitter applied (verify delay is within expected range). Max attempts reached → callback invoked. Cancel clears pending timeout. Reset sets attempts to 0.
- [ ] T033 Create `../sukko-ws-sdk/tests/client.test.ts` integration tests using mock server: Full flow — connect → subscribe → receive message → unsubscribe → disconnect. Subscribe returns `SubscriptionAck` with subscribed channels. Publish returns `PublishAck`. Publish error → promise rejects with `SukkoError`. Subscribe before connect → queued and executed after connect. Publish while disconnected → rejects with `NotConnectedError`. Reconnect after server close → re-subscribes to same channels. Sequence gap detected → emits `gap` event. Filtered channels → emits `channelsFiltered` event. State transitions: disconnected → connecting → connected → reconnecting → connected.
- [ ] T034 Run `npm test` in `../sukko-ws-sdk/`. All tests pass. Run `npm run test:coverage`. Verify >90% coverage on `src/protocol.ts`, `src/token.ts`, `src/connection.ts`, `src/subscription.ts`, `src/reconnect.ts`, `src/client.ts`.

**Phase 5 verification**: `npm test` passes, coverage report shows >90% on all source modules.

---

## Phase 6: Documentation

> README with quick start, API reference, and error codes.

- [ ] T035 Create `../sukko-ws-sdk/README.md` with sections: package name and description, installation (`npm install @sukko/ws-sdk`), quick start (10 lines — connect, subscribe, receive message, disconnect — satisfies SC-001), authentication (static token and `getToken` callback examples), subscribe/unsubscribe API, publish API, events table (all `SukkoEvents` with descriptions), reconnection configuration, token refresh explanation, Node.js usage (providing `ws` as WebSocket constructor), error codes table (all 11 `ErrorCode` values with descriptions), and license.
- [ ] T036 Add JSDoc comments to all public methods and types in `../sukko-ws-sdk/src/client.ts` and `../sukko-ws-sdk/src/types.ts`. Every public method on `SukkoClient` must have a `@param`, `@returns`, `@throws`, and `@example` tag.

**Phase 6 verification**: README renders correctly. JSDoc visible in IDE when importing `@sukko/ws-sdk`.

---

## Phase 7: CI/CD & Publish Readiness

> GitHub Actions pipelines and package validation.

- [ ] T037 [P] Create `../sukko-ws-sdk/.github/workflows/ci.yml`: trigger on push/PR to main. Matrix test on Node.js 18, 20, 22. Steps: checkout → setup-node → `npm ci` → `npm run lint` → `npm run build` → `npm test` → `npx publint` → `npx @arethetypeswrong/cli --pack .`
- [ ] T038 [P] Create `../sukko-ws-sdk/.github/workflows/release.yml`: trigger on push tag `v*`. Permissions: `contents: read`, `id-token: write`. Steps: checkout → setup-node with registry-url → `npm ci` → `npm run lint` → `npm run build` → `npm test` → `npx publint` → `npx @arethetypeswrong/cli --pack .` → `npm publish --provenance`.
- [ ] T039 Run final validation in `../sukko-ws-sdk/`: `npm run build` → `npm run lint` → `npm test` → `npm run check:exports` (publint + attw). Verify `npm pack --dry-run` includes only `dist/`, `README.md`, `package.json`. Check `dist/index.js` gzipped size < 15KB.

**Phase 7 verification**: All validation commands pass. Package is ready for `npm publish`.

---

## Summary

| Phase | Tasks | Parallel Opportunities |
|---|---|---|
| 1. Scaffolding | T001–T009 (9) | T002–T007 all parallel (config files) |
| 2. Protocol Layer | T010–T013 (4) | T011–T013 parallel (all depend on T010 types) |
| 3. Connection Layer | T014–T016 (3) | None (T015–T016 depend on T014 patterns but can be parallel as separate files) |
| 4. Client API | T017–T026 (10) | None (sequential — each builds on previous) |
| 5. Testing | T027–T034 (8) | T028–T030 parallel (independent unit tests); T031–T033 sequential (need mock server) |
| 6. Documentation | T035–T036 (2) | T035–T036 parallel |
| 7. CI/CD | T037–T039 (3) | T037–T038 parallel |
| **Total** | **39 tasks** | **13 parallelizable** |

## Dependencies

```
Phase 1 (Scaffolding)
    ↓
Phase 2 (Protocol Layer) — types.ts first, then errors/protocol/token in parallel
    ↓
Phase 3 (Connection Layer) — depends on protocol.ts for encode/decode
    ↓
Phase 4 (Client API) — composes all Phase 2 + 3 modules
    ↓
Phase 5 (Testing) — tests all modules
    ↓
Phase 6 (Documentation) ←→ Phase 7 (CI/CD) — independent, can run in parallel
```

## Start Here

**First task**: T001 — Create the repository and initialize git.
**Suggested command**: `/implement`
