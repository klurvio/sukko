# Implementation Plan: TypeScript SDK for Odin WS

**Branch**: `feat/typescript-sdk` | **Date**: 2026-02-26 | **Spec**: `specs/feat/typescript-sdk/spec.md`

## Summary

Build `@sukko/ws-sdk`, a zero-dependency TypeScript client library for the Odin WS real-time data platform. The SDK encapsulates the full WebSocket protocol (subscribe, unsubscribe, publish, reconnect, heartbeat), provides automatic reconnection with message replay, proactive token refresh via graceful reconnect, sequence gap detection, and typed error handling. Published to public npm as dual ESM/CJS with full type definitions.

## Technical Context

**Language**: TypeScript (ES2022 target)
**Runtime**: Browser (native WebSocket) + Node.js 18+ (optional `ws` polyfill)
**Build**: tsdown (Rolldown/Rust-based, successor to tsup)
**Test**: Vitest + vitest-websocket-mock
**Lint**: Biome
**Package**: `@sukko/ws-sdk` on public npm with provenance
**Module formats**: ESM (primary) + CJS (fallback)
**Repository**: Separate repo, sibling of `odin-ws`

## Constitution Compliance

| Principle | Applicability | Status |
|---|---|---|
| I. No Hardcoded Values | N/A — SDK, not a Go service | SKIP |
| II. Defense in Depth | Applies — validate inputs at SDK boundary | PASS — SDK validates channel format, connection state, options |
| III. Error Handling | Applies — structured errors with context | PASS — typed `SukkoError` with code + message, promise rejections |
| IV. Graceful Degradation | Applies — reconnection, heartbeat | PASS — auto-reconnect with backoff, heartbeat timeout detection |
| V. Structured Logging | N/A — SDK, not a server | SKIP (SDK uses optional debug callback) |
| VI. Observability | N/A — SDK, not a server | SKIP |
| VII. Concurrency Safety | N/A — single-threaded JavaScript | SKIP |
| VIII. Configuration Validation | Applies — validate ClientOptions | PASS — validate at construction time, fail fast |
| IX. Testing | Applies — table-driven, mocks, edge cases | PASS — Vitest, mock WebSocket, >90% coverage target |
| X. Security | Applies — no secrets in logs, input validation | PASS — JWT never logged, channel format validated |
| XI. Shared Code | N/A — separate repository | SKIP |

No violations.

## Architecture

### Module Dependency Graph

```
index.ts (barrel export)
    ├── client.ts (SukkoClient — public API)
    │   ├── connection.ts (WebSocket lifecycle, heartbeat)
    │   ├── subscription.ts (channel tracking, gap detection)
    │   ├── reconnect.ts (backoff, re-subscribe, replay)
    │   ├── token.ts (JWT decode, proactive refresh scheduling)
    │   └── protocol.ts (encode/decode wire messages)
    ├── errors.ts (SukkoError, UnauthorizedError, error codes)
    └── types.ts (all public type definitions)
```

### Data Flow

```
Developer API                    SDK Internals                    Wire
─────────────────────────────────────────────────────────────────────────
client.subscribe(channels) → subscription.ts → protocol.encode() → ws.send()
                                                                      ↓
                           subscription.ts ← protocol.decode() ← ws.onmessage
                                  ↓
              promise.resolve(SubscriptionAck)

              on('message', cb) ← subscription.dispatch() ← protocol.decode()
```

### State Machine

```
                    connect()
  DISCONNECTED ──────────────→ CONNECTING
       ↑                           │
       │                    success│
       │                           ↓
       │              ┌──── CONNECTED ←──────┐
       │              │        │              │
       │   token near │  unexpected    reconnect
       │      expiry  │     close       success
       │              │        │              │
       │              ↓        ↓              │
       │         TOKEN_    RECONNECTING ──────┘
       │        REFRESH        │
       │           │      max retries
       │           │      exceeded
       │           │        │
       │           ↓        ↓
       │      CONNECTING  DISCONNECTED
       │      (new conn)  (emit 'error')
       │           │
       └───────────┘ (if getToken throws UnauthorizedError)
```

Note: `TOKEN_REFRESH` is an internal sub-state of `CONNECTED`. Externally it appears as a brief transition through `RECONNECTING` → `CONNECTED`.

## Phase 1 — Project Scaffolding

### 1.1 Repository Setup

Create new repository `sukko-ws-sdk` as a sibling of `odin-ws`:

```
../sukko-ws-sdk/
├── src/
│   ├── index.ts
│   ├── client.ts
│   ├── connection.ts
│   ├── subscription.ts
│   ├── reconnect.ts
│   ├── token.ts
│   ├── protocol.ts
│   ├── errors.ts
│   └── types.ts
├── tests/
│   ├── client.test.ts
│   ├── connection.test.ts
│   ├── subscription.test.ts
│   ├── reconnect.test.ts
│   ├── token.test.ts
│   ├── protocol.test.ts
│   └── helpers/
│       └── mock-server.ts
├── tsdown.config.ts
├── tsconfig.json
├── tsconfig.build.json
├── biome.json
├── vitest.config.ts
├── package.json
├── .gitignore
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
└── README.md
```

### 1.2 package.json

```json
{
  "name": "@sukko/ws-sdk",
  "version": "0.1.0",
  "description": "TypeScript SDK for Odin WS real-time data platform",
  "type": "module",
  "main": "./dist/index.cjs",
  "module": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "exports": {
    ".": {
      "import": {
        "types": "./dist/index.d.ts",
        "default": "./dist/index.js"
      },
      "require": {
        "types": "./dist/index.d.cts",
        "default": "./dist/index.cjs"
      }
    }
  },
  "files": ["dist"],
  "sideEffects": false,
  "engines": { "node": ">=18" },
  "publishConfig": {
    "access": "public",
    "provenance": true
  },
  "scripts": {
    "build": "tsdown",
    "test": "vitest run",
    "test:watch": "vitest",
    "test:coverage": "vitest run --coverage",
    "lint": "biome check .",
    "lint:fix": "biome check --write .",
    "check:exports": "publint && attw --pack .",
    "prepublishOnly": "npm run build && npm run check:exports"
  },
  "devDependencies": {
    "tsdown": "^0.x",
    "typescript": "^5.7",
    "vitest": "^3.x",
    "vitest-websocket-mock": "^0.x",
    "@biomejs/biome": "^2.x",
    "publint": "^0.x",
    "@arethetypeswrong/cli": "^0.x"
  }
}
```

### 1.3 tsdown.config.ts

```typescript
import { defineConfig } from 'tsdown';

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  dts: true,
  sourcemap: true,
  clean: true,
  target: 'es2022',
});
```

### 1.4 tsconfig.json

```json
{
  "compilerOptions": {
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "isolatedModules": true,
    "verbatimModuleSyntax": true,
    "moduleDetection": "force",
    "noUncheckedIndexedAccess": true,
    "noImplicitOverride": true,
    "module": "nodenext",
    "moduleResolution": "nodenext",
    "target": "es2022",
    "lib": ["es2022"],
    "outDir": "dist",
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "isolatedDeclarations": true
  },
  "include": ["src", "tests"],
  "exclude": ["node_modules", "dist"]
}
```

### 1.5 biome.json

```json
{
  "$schema": "https://biomejs.dev/schemas/2.0.0/schema.json",
  "formatter": {
    "indentStyle": "space",
    "indentWidth": 2,
    "lineWidth": 100
  },
  "linter": {
    "rules": {
      "recommended": true,
      "correctness": {
        "noUnusedImports": "error",
        "noUnusedVariables": "error"
      },
      "style": {
        "useConst": "error",
        "useImportType": "error"
      },
      "suspicious": {
        "noExplicitAny": "warn"
      }
    }
  },
  "javascript": {
    "formatter": {
      "quoteStyle": "single",
      "trailingCommas": "all",
      "semicolons": "always"
    }
  }
}
```

### 1.6 vitest.config.ts

```typescript
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
    include: ['tests/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.ts'],
      exclude: ['src/index.ts', 'src/types.ts'],
      thresholds: {
        branches: 90,
        functions: 90,
        lines: 90,
        statements: 90,
      },
    },
    testTimeout: 10_000,
  },
});
```

## Phase 2 — Protocol Layer

### 2.1 `src/types.ts` — Public Type Definitions

All types exported from the SDK. Derived directly from the AsyncAPI spec and Go protocol types.

```typescript
// --- Connection ---

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

export interface ClientOptions {
  /** WebSocket server URL (wss://...) */
  url: string;
  /** Initial JWT token. Mutually exclusive with getToken for initial auth. */
  token?: string;
  /** Async callback to obtain a fresh JWT. Called on connect, reconnect, and before token expiry. */
  getToken?: () => Promise<string>;
  /** Custom WebSocket constructor (for Node.js < 22 environments). Defaults to globalThis.WebSocket. */
  WebSocket?: WebSocketConstructor;
  /** Heartbeat interval in ms. Default: 30000. */
  heartbeatInterval?: number;
  /** Heartbeat timeout in ms (time to wait for pong). Default: 10000. */
  heartbeatTimeout?: number;
  /** Reconnection configuration. */
  reconnect?: ReconnectOptions | false;
  /** Whether to automatically reconnect on sequence gap detection. Default: false. */
  reconnectOnGap?: boolean;
  /** Optional debug logger. */
  debug?: (msg: string, data?: Record<string, unknown>) => void;
}

export interface ReconnectOptions {
  /** Initial backoff delay in ms. Default: 1000. */
  initialDelay?: number;
  /** Maximum backoff delay in ms. Default: 30000. */
  maxDelay?: number;
  /** Maximum reconnection attempts. Default: Infinity. */
  maxAttempts?: number;
  /** Backoff multiplier. Default: 2. */
  multiplier?: number;
  /** Jitter factor (0-1). Default: 0.1. */
  jitter?: number;
}

export type WebSocketConstructor = new (url: string, protocols?: string | string[]) => WebSocket;

// --- Messages (Client → Server) ---

export interface SubscribeRequest {
  channels: string[];
}

export interface UnsubscribeRequest {
  channels: string[];
}

export interface PublishRequest {
  channel: string;
  data: unknown;
}

export interface ReconnectRequest {
  client_id: string;
  last_offset: Record<string, number>;
}

// --- Messages (Server → Client) ---

export interface Message<T = unknown> {
  type: 'message';
  seq: number;
  ts: number;
  channel: string;
  data: T;
}

export interface SubscriptionAck {
  type: 'subscription_ack';
  subscribed: string[];
  count: number;
}

export interface UnsubscriptionAck {
  type: 'unsubscription_ack';
  unsubscribed: string[];
  count: number;
}

export interface PublishAck {
  type: 'publish_ack';
  channel: string;
  status: 'accepted';
}

export interface ReconnectAck {
  type: 'reconnect_ack';
  status: 'completed';
  messages_replayed: number;
  message: string;
}

export interface Pong {
  type: 'pong';
  ts: number;
}

// --- Error Responses ---

export type ErrorCode =
  | 'invalid_json'
  | 'invalid_request'
  | 'not_available'
  | 'invalid_channel'
  | 'message_too_large'
  | 'rate_limited'
  | 'publish_failed'
  | 'forbidden'
  | 'topic_not_provisioned'
  | 'service_unavailable'
  | 'replay_failed';

export interface ErrorResponse {
  type: 'error' | 'subscribe_error' | 'unsubscribe_error' | 'publish_error' | 'reconnect_error';
  code: ErrorCode;
  message: string;
}

// --- Events ---

export interface SukkoEvents {
  /** Broadcast message received on a subscribed channel. */
  message: (msg: Message) => void;
  /** Connection state changed. */
  stateChange: (state: ConnectionState, previousState: ConnectionState) => void;
  /** Sequence gap detected (missed messages). */
  gap: (expected: number, received: number) => void;
  /** Protocol error from server (type: "error"). */
  error: (error: ErrorResponse) => void;
  /** Connection permanently failed (max retries exceeded or auth failure). */
  connectionError: (error: Error) => void;
  /** Pong received with server timestamp and computed latency. */
  pong: (serverTs: number, latencyMs: number) => void;
  /** Channels were filtered during subscribe (requested vs confirmed mismatch). */
  channelsFiltered: (requested: string[], confirmed: string[]) => void;
  /** Token refresh initiated. */
  tokenRefresh: (reason: 'expiry' | 'reconnect') => void;
}
```

### 2.2 `src/errors.ts` — Error Types

```typescript
export class SukkoError extends Error {
  constructor(
    public readonly code: ErrorCode,
    message: string,
    public readonly type: string,
  ) {
    super(message);
    this.name = 'SukkoError';
  }
}

export class UnauthorizedError extends Error {
  constructor(message = 'Unauthorized') {
    super(message);
    this.name = 'UnauthorizedError';
  }
}

export class NotConnectedError extends Error {
  constructor() {
    super('Not connected to server');
    this.name = 'NotConnectedError';
  }
}
```

### 2.3 `src/protocol.ts` — Wire Protocol Codec

Encodes client messages into JSON strings and decodes server messages into typed objects. Pure functions, no side effects, highly testable.

**Responsibilities**:
- `encodeSubscribe(channels: string[]): string`
- `encodeUnsubscribe(channels: string[]): string`
- `encodePublish(channel: string, data: unknown): string`
- `encodeReconnect(clientId: string, offsets: Record<string, number>): string`
- `encodeHeartbeat(): string`
- `decodeServerMessage(raw: string): ServerMessage` — discriminated union by `type` field

**Validation**:
- Channel format: minimum 3 dot-separated non-empty parts
- Publish data: must be JSON-serializable

### 2.4 `src/token.ts` — JWT Decode and Refresh Scheduling

**Responsibilities**:
- `decodeJwtExp(token: string): number | null` — base64-decode JWT payload, extract `exp` claim. No signature verification (SDK doesn't need the secret).
- `scheduleRefresh(exp: number, callback: () => void): NodeJS.Timeout` — schedule callback ~30s before `exp` (or immediately if <30s remaining)
- Clear refresh timer on disconnect

## Phase 3 — Connection Layer

### 3.1 `src/connection.ts` — WebSocket Lifecycle

**Responsibilities**:
- Open/close WebSocket connection with JWT auth (via query param `?token=`)
- Send raw messages (string)
- Receive raw messages (string) and dispatch to a callback
- Heartbeat: send `heartbeat` at configured interval, expect `pong` within timeout
- Detect dead connection (no pong) and notify
- Handle close codes (1000=normal, 1001=going away, 1008=slow client, etc.)
- Expose connection state

**Key design**:
- The connection layer does NOT know about protocol semantics — it sends/receives raw strings
- Heartbeat timer is managed internally (start on open, stop on close)
- Close code 1008 (policy violation / slow client) is surfaced as a specific event

### 3.2 `src/subscription.ts` — Channel State and Gap Detection

**Responsibilities**:
- Track set of subscribed channels (add on `subscription_ack`, remove on `unsubscription_ack`)
- Track expected sequence number (starts at 1, increments per message)
- Detect gaps (received seq > expected seq) and emit gap event
- Reset sequence tracking on reconnect
- Compare requested vs confirmed channels and emit `channelsFiltered` if mismatch
- Expose current subscriptions as `ReadonlySet<string>`

### 3.3 `src/reconnect.ts` — Reconnection with Backoff

**Responsibilities**:
- Exponential backoff with jitter: `delay = min(initialDelay * multiplier^attempt, maxDelay) * (1 + random * jitter)`
- Track attempt count, reset on successful connect
- On reconnect: call `getToken` (if provided) → connect → re-subscribe to all tracked channels → send `reconnect` message
- Stop retrying after `maxAttempts` and emit `connectionError`
- Cancel pending reconnect on explicit `disconnect()`

## Phase 4 — Client API

### 4.1 `src/client.ts` — SukkoClient (Public API)

The main entry point. Composes connection, subscription, reconnect, and token modules.

```typescript
export class SukkoClient {
  // --- Lifecycle ---
  constructor(options: ClientOptions);
  connect(): Promise<void>;
  disconnect(): void;

  // --- Messaging ---
  subscribe(channels: string[]): Promise<SubscriptionAck>;
  unsubscribe(channels: string[]): Promise<UnsubscriptionAck>;
  publish(channel: string, data: unknown): Promise<PublishAck>;

  // --- Events ---
  on<E extends keyof SukkoEvents>(event: E, listener: SukkoEvents[E]): this;
  off<E extends keyof SukkoEvents>(event: E, listener: SukkoEvents[E]): this;

  // --- State ---
  readonly state: ConnectionState;
  readonly subscriptions: ReadonlySet<string>;
  readonly latency: number | null;
}
```

**Operation serialization**:
- Subscribe, unsubscribe, and reconnect are serialized (one at a time) via an internal promise queue
- Publish resolves on `publish_ack` or rejects on `publish_error` — multiple in-flight allowed if channel differs, but serialized per-channel for simplicity
- Operations called before `connect()` completes are queued and executed after connection

**Token refresh flow**:
1. On `connect()`: if `getToken` provided, call it; else use `token`
2. Decode JWT `exp`, schedule refresh timer
3. On timer fire: emit `tokenRefresh('expiry')` → call `getToken()` → graceful reconnect
4. On reconnect: emit `tokenRefresh('reconnect')` → call `getToken()` → use new token
5. If `getToken` throws `UnauthorizedError`: emit `connectionError`, disconnect permanently
6. If `getToken` fails transiently: retry with backoff (3 attempts), then emit `connectionError`

**Message dispatch**:
- `connection.onMessage` receives raw string
- `protocol.decodeServerMessage` parses into typed object
- Switch on `type`:
  - `message` → update sequence tracker → emit `message` event
  - `subscription_ack` → update subscription set → resolve pending subscribe promise
  - `unsubscription_ack` → update subscription set → resolve pending unsubscribe promise
  - `publish_ack` → resolve pending publish promise
  - `publish_error` → reject pending publish promise with `SukkoError`
  - `subscribe_error` → reject pending subscribe promise with `SukkoError`
  - `unsubscribe_error` → reject pending unsubscribe promise with `SukkoError`
  - `reconnect_ack` → resolve internal reconnect promise
  - `reconnect_error` → reject internal reconnect promise with `SukkoError`
  - `pong` → compute latency, emit `pong` event
  - `error` → emit `error` event

### 4.2 `src/index.ts` — Barrel Export

```typescript
export { SukkoClient } from './client.js';
export { SukkoError, UnauthorizedError, NotConnectedError } from './errors.js';
export type {
  ClientOptions,
  ReconnectOptions,
  ConnectionState,
  Message,
  SubscriptionAck,
  UnsubscriptionAck,
  PublishAck,
  ReconnectAck,
  Pong,
  ErrorCode,
  ErrorResponse,
  SukkoEvents,
} from './types.js';
```

## Phase 5 — Testing

### 5.1 Test Strategy

| Layer | Test Type | Mock | Coverage Target |
|---|---|---|---|
| `protocol.ts` | Unit (pure functions) | None | 100% |
| `token.ts` | Unit (pure functions) | None | 100% |
| `errors.ts` | Unit | None | 100% |
| `connection.ts` | Unit | vitest-websocket-mock | 95% |
| `subscription.ts` | Unit | None | 95% |
| `reconnect.ts` | Unit | vitest-websocket-mock, fake timers | 90% |
| `client.ts` | Integration | vitest-websocket-mock | 90% |

### 5.2 Key Test Scenarios

**Protocol tests** (table-driven):
- Encode/decode every message type
- Invalid JSON decode → error
- Channel validation (valid, too few parts, empty parts)

**Connection tests**:
- Connect with token via query param
- Heartbeat send and pong receive
- Heartbeat timeout → dead connection callback
- Close code 1008 → slow client event
- Close code 1000 → normal close

**Subscription tests**:
- Track subscribed channels from acks
- Detect gap (seq 5 → 8)
- Detect filtered channels (requested 3, confirmed 2)
- Reset sequence on reconnect

**Reconnect tests** (fake timers):
- Exponential backoff timing
- Re-subscribe after reconnect
- Max attempts → give up
- Cancel reconnect on explicit disconnect

**Token tests**:
- Decode valid JWT exp
- Decode JWT without exp → null
- Decode invalid base64 → null
- Schedule refresh 30s before exp
- Schedule immediate refresh when <30s remaining

**Client integration tests**:
- Full flow: connect → subscribe → receive message → unsubscribe → disconnect
- Publish → ack and publish → error paths
- Reconnect after drop → re-subscribe → replay
- Queue operations before connect resolves
- Publish while disconnected → NotConnectedError
- getToken callback → proactive reconnect before expiry
- getToken throws UnauthorizedError → permanent disconnect

### 5.3 `tests/helpers/mock-server.ts`

Utility wrapping `vitest-websocket-mock` with Odin WS protocol awareness:
- `mockServer.expectSubscribe(channels)` → auto-respond with `subscription_ack`
- `mockServer.sendMessage(channel, data, seq)` → send typed broadcast
- `mockServer.sendError(code, message)` → send error response

## Phase 6 — Documentation

### 6.1 README.md

- Quick start (10 lines to receive messages — SC-001)
- Installation
- Authentication (static token and getToken callback)
- Subscribe/unsubscribe
- Publish
- Events
- Reconnection
- Token refresh
- Node.js usage (WebSocket polyfill)
- API reference (brief, link to full docs)
- Error codes table

### 6.2 API Reference

JSDoc on all public types and methods. Generated via `typedoc` or inline in README for v1.

## Phase 7 — CI/CD

### 7.1 `.github/workflows/ci.yml`

- Trigger: push/PR to main
- Matrix: Node.js 18, 20, 22
- Steps: install → lint → build → test → check:exports

### 7.2 `.github/workflows/release.yml`

- Trigger: push tag `v*`
- Steps: install → lint → build → test → publint → attw → npm publish (trusted publishing with provenance)

## Files Created (Summary)

| # | File | Purpose |
|---|---|---|
| 1 | `src/index.ts` | Barrel export |
| 2 | `src/types.ts` | All public type definitions |
| 3 | `src/errors.ts` | SukkoError, UnauthorizedError, NotConnectedError |
| 4 | `src/protocol.ts` | Wire protocol encode/decode |
| 5 | `src/token.ts` | JWT decode, refresh scheduling |
| 6 | `src/connection.ts` | WebSocket lifecycle, heartbeat |
| 7 | `src/subscription.ts` | Channel tracking, gap detection |
| 8 | `src/reconnect.ts` | Backoff, re-subscribe, replay |
| 9 | `src/client.ts` | SukkoClient public API |
| 10 | `tests/protocol.test.ts` | Protocol encode/decode tests |
| 11 | `tests/token.test.ts` | JWT decode and scheduling tests |
| 12 | `tests/connection.test.ts` | WebSocket lifecycle tests |
| 13 | `tests/subscription.test.ts` | Channel tracking and gap tests |
| 14 | `tests/reconnect.test.ts` | Backoff and reconnect flow tests |
| 15 | `tests/client.test.ts` | End-to-end integration tests |
| 16 | `tests/helpers/mock-server.ts` | Protocol-aware mock server |
| 17 | `package.json` | Package config |
| 18 | `tsdown.config.ts` | Build config |
| 19 | `tsconfig.json` | TypeScript config (type-checking) |
| 20 | `tsconfig.build.json` | TypeScript config (build, excludes tests) |
| 21 | `biome.json` | Lint/format config |
| 22 | `vitest.config.ts` | Test config |
| 23 | `README.md` | Usage documentation |
| 24 | `.github/workflows/ci.yml` | CI pipeline |
| 25 | `.github/workflows/release.yml` | Release pipeline |

## Implementation Order

```
Phase 1: Scaffolding (package.json, configs, CI)
    ↓
Phase 2: Protocol layer (types → errors → protocol → token)
    ↓
Phase 3: Connection layer (connection → subscription → reconnect)
    ↓
Phase 4: Client API (client → index barrel)
    ↓
Phase 5: Tests (unit → integration)
    ↓
Phase 6: Documentation (README, JSDoc)
    ↓
Phase 7: CI/CD (ci.yml, release.yml)
```

Each phase is independently testable. Protocol layer has zero dependencies on connection layer. Connection layer is testable with mock WebSocket. Client composes all layers.

## Verification

1. `npm run build` — produces `dist/` with `.js`, `.cjs`, `.d.ts`, `.d.cts`
2. `npm test` — all tests pass, >90% coverage
3. `npm run lint` — no Biome violations
4. `npm run check:exports` — publint + attw pass
5. `npm pack --dry-run` — only `dist/` files included
6. Bundle size check — `dist/index.js` < 15KB gzipped
7. Integration test against live Odin WS instance (manual, pre-release)

## Resource Impact

- **No server-side changes required** — SDK uses existing protocol
- **Token refresh uses graceful reconnect** — no new `auth` message type needed (see research.md R1)
- **npm scope `@sukko`** must be registered on npmjs.com before first publish
- **GitHub Actions** needs npm trusted publishing configured (OIDC, no NPM_TOKEN)
