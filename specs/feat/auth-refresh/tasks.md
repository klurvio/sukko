# Tasks: Mid-Connection Auth Refresh

**Branch**: `feat/auth-refresh` | **Generated**: 2026-02-26

---

## Phase 1 — Protocol Separation (Prerequisite Refactor)

- [x] T001 Create `ws/internal/server/protocol.go` with server-only message type constants (`MsgTypeReconnect`, `MsgTypeHeartbeat`, `MsgTypeMessage`, `MsgTypePong`, `MsgTypeError`), response type constants (`RespTypeSubscriptionAck`, `RespTypeUnsubscriptionAck`, `RespTypeReconnectAck`, `RespTypeReconnectError`, `RespTypeSubscribeError`, `RespTypeUnsubscribeError`), `UnsubscribeData` struct, and error codes (`ErrCodeInvalidJSON`, `ErrCodePublishFailed`, `ErrCodeReplayFailed`). Do NOT include `ErrProducerClosed` here — it is defined locally in the kafka package (see T006).

- [x] T002 Remove server-only symbols from `ws/internal/shared/protocol/types.go`: delete `MsgTypeReconnect`, `MsgTypeHeartbeat`, `MsgTypeMessage`, `MsgTypePong`, `MsgTypeError` constants, delete `RespTypeSubscriptionAck`, `RespTypeUnsubscriptionAck`, `RespTypeReconnectAck`, `RespTypeReconnectError`, `RespTypeSubscribeError`, `RespTypeUnsubscribeError` constants, and delete `UnsubscribeData` struct. Retain: `MsgTypeSubscribe`, `MsgTypeUnsubscribe`, `MsgTypePublish`, `RespTypePublishAck`, `RespTypePublishError`, `ClientMessage`, `SubscribeData`, `PublishData`. (`RespTypePublishAck` and `RespTypePublishError` are used by gateway proxy.go — they stay in shared.)

- [x] T003 Remove server-only symbols from `ws/internal/shared/protocol/errors.go`: delete `ErrCodeInvalidJSON`, `ErrCodePublishFailed`, `ErrCodeReplayFailed` constants and `ErrProducerClosed` sentinel error. Retain all shared error codes (`ErrCodeInvalidRequest`, `ErrCodeNotAvailable`, `ErrCodeInvalidChannel`, `ErrCodeMessageTooLarge`, `ErrCodeRateLimited`, `ErrCodeForbidden`, `ErrCodeTopicNotProvisioned`, `ErrCodeServiceUnavailable`), `PublishErrorMessages`, and shared sentinel errors.

- [x] T004 Update `ws/internal/server/handlers_message.go` to use package-local protocol constants from `ws/internal/server/protocol.go` for server-only types. Keep importing `protocol.ClientMessage`, `protocol.SubscribeData`, `protocol.MsgTypeSubscribe`, `protocol.MsgTypePublish`, `protocol.ErrorCode`, `protocol.ErrCodeInvalidRequest`, `protocol.ErrCodeNotAvailable` from shared. Replace all `protocol.MsgTypeReconnect` → `MsgTypeReconnect`, `protocol.MsgTypePong` → `MsgTypePong`, `protocol.MsgTypeHeartbeat` → `MsgTypeHeartbeat`, `protocol.MsgTypeMessage` → `MsgTypeMessage`, `protocol.MsgTypeError` → `MsgTypeError`, `protocol.RespType*` → local `RespType*`, `protocol.UnsubscribeData` → `UnsubscribeData`, `protocol.ErrCodeInvalidJSON` → `ErrCodeInvalidJSON`, `protocol.ErrCodeReplayFailed` → `ErrCodeReplayFailed`.

- [x] T005 Update `ws/internal/server/handlers_publish.go` to use package-local `ErrCodePublishFailed` from `ws/internal/server/protocol.go`. Keep importing shared types (`protocol.PublishData`, `protocol.ErrorCode`, `protocol.RespTypePublishAck`, `protocol.RespTypePublishError`, `protocol.PublishErrorMessages`, and all shared `ErrCode*` constants and sentinel errors). Replace `protocol.ErrCodePublishFailed` → `ErrCodePublishFailed`.

- [x] T006 Update `ws/internal/shared/kafka/producer.go`: if it imports `protocol.ErrProducerClosed`, replace with a locally defined `ErrProducerClosed = errors.New("producer is closed")` sentinel error in the kafka package. This avoids a circular dependency (server→kafka→server). If the producer already defines its own, remove the protocol import reference.

- [x] T007 Update server test files (`ws/internal/server/*_test.go`) to use package-local protocol constants instead of `protocol.*` for any moved symbols. Verify all tests compile and pass.

- [x] T008 Verify Phase 1: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All tests must pass with zero behavioral changes. Confirm no import cycles.

---

## Phase 2 — Auth Refresh Protocol, Config & Metrics

- [x] T009 [P] Create `ws/internal/gateway/auth_protocol.go` with auth message type constants (`MsgTypeAuth = "auth"`, `RespTypeAuthAck = "auth_ack"`, `RespTypeAuthError = "auth_error"`), auth-specific error code constants (`AuthErrInvalidToken = "invalid_token"`, `AuthErrTokenExpired = "token_expired"`, `AuthErrTenantMismatch = "tenant_mismatch"`, `AuthErrRateLimited = "rate_limited"`, `AuthErrNotAvailable = "not_available"`), `AuthData` struct with `Token string` field, `AuthErrorMessages` map for human-readable messages, and response structs: `AuthAckResponse` (`Type string` + `Data AuthAckData{Exp int64}`), `AuthErrorResponse` (`Type string` + `Data AuthErrorData{Code string, Message string}`).

- [x] T010 [P] Add `AuthRefreshRateInterval time.Duration` field with env tag `GATEWAY_AUTH_REFRESH_RATE_INTERVAL` and `envDefault:"30s"` to `GatewayConfig` in `ws/internal/shared/platform/gateway_config.go`. Add validation in `Validate()`: must be >= 1s. Add to `LogConfig()` output following existing format.

- [x] T011 [P] Add auth refresh Prometheus metrics to `ws/internal/gateway/metrics.go`: `gateway_auth_refresh_total` (CounterVec, label: `result` — values: success, invalid_token, token_expired, tenant_mismatch, rate_limited, not_available), `gateway_auth_refresh_latency_seconds` (Histogram with standard latency buckets), `gateway_forced_unsubscriptions_total` (Counter). Add helper functions `RecordAuthRefresh(result string)`, `RecordAuthRefreshLatency(seconds float64)`, `RecordForcedUnsubscription()` following existing `Record*` patterns.

---

## Phase 3 — Auth Refresh Proxy Implementation

- [x] T012 Define `TokenValidator` interface in `ws/internal/gateway/interfaces.go` (which already exists with `TenantRegistry` and `MultiIssuerValidator` interfaces — add to the same file): `ValidateToken(ctx context.Context, tokenString string) (*auth.Claims, error)`. The existing `auth.MultiTenantValidator` already satisfies this interface.

- [x] T013 Extend `Proxy` struct and `ProxyConfig` in `ws/internal/gateway/proxy.go`. Add to Proxy: `validator TokenValidator`, `claimsMu sync.RWMutex`, `authLimiter *rate.Limiter`, `subscribedChannels map[string]struct{}`. Add to ProxyConfig: `Validator TokenValidator`, `AuthRefreshRateInterval time.Duration`. Update `NewProxy()` to initialize `authLimiter` with `rate.Every(cfg.AuthRefreshRateInterval)` burst 1, initialize `subscribedChannels` map, and store validator.

- [x] T014 Add backend→client subscription tracking in `ws/internal/gateway/proxy.go`. Add `trackSubscriptionResponse(payload []byte)` method: fast byte check for `"_ack"` in first 80 bytes (skip broadcasts), partial JSON parse to extract `type`, if `subscription_ack` parse channels and add to `p.subscribedChannels`, if `unsubscription_ack` parse channels and remove. Protect map access with `p.claimsMu.Lock()` (brief lock — the backend→client goroutine takes `claimsMu.Lock()` only for map writes; the client→backend goroutine takes `claimsMu.Lock()` during auth refresh for map reads + claims swap; both are brief, no I/O under lock). Call from `proxyBackendToClient()` for TEXT frames when `p.authEnabled` is true — always forward to client regardless (tracking is observational).

- [x] T015 Implement `interceptAuthRefresh(clientMsg protocol.ClientMessage) ([]byte, error)` method on Proxy in `ws/internal/gateway/proxy.go`. Full flow: (1) record start time, (2) if `!p.authEnabled` send `auth_error` not_available + return nil, (3) rate limit check with `p.authLimiter.Allow()` — if denied send `auth_error` rate_limited, (4) parse `AuthData` from clientMsg.Data, (5) call `p.validator.ValidateToken()` — map `auth.ErrTokenExpired` to token_expired and other errors to invalid_token, (6) verify `newClaims.TenantID == p.tenantID` — if mismatch send tenant_mismatch, (7) call `p.forceUnsubscribeRevokedChannels(newClaims)` — collects revoked channels under lock, updates tracking, releases lock, then performs I/O, (8) lock `p.claimsMu`, swap `p.claims = newClaims`, unlock, (9) send `auth_ack` (`AuthAckResponse` with exp timestamp) to client, (10) log at Info, record success metric. Add `sendAuthErrorToClient(code, message string) ([]byte, error)` helper mirroring `sendPublishErrorToClient` — sends `AuthErrorResponse`.

- [x] T016 Implement `forceUnsubscribeRevokedChannels(newClaims *auth.Claims) []string` method on Proxy in `ws/internal/gateway/proxy.go`. Two phases (Constitution VII — no mutex across I/O): **Phase A (under lock):** lock `p.claimsMu`, for each channel in `p.subscribedChannels` strip tenant prefix and check `p.permissions.CanSubscribe(newClaims, stripped)`, collect revoked channels, remove revoked from `p.subscribedChannels`, unlock `p.claimsMu`. **Phase B (lock released):** if any revoked: build synthetic unsubscribe `protocol.ClientMessage` with revoked channels, forward to backend via `p.forwardFrame()` (fire-and-forget, no waiting for backend ack), send `unsubscription_ack` to client listing revoked channels, record `gateway_forced_unsubscriptions_total` metric. Return revoked list for logging.

- [x] T017 Add `MsgTypeAuth` case to `interceptClientMessage()` switch in `ws/internal/gateway/proxy.go`: route to `p.interceptAuthRefresh(clientMsg)`. Add `p.claimsMu.RLock()`/`p.claimsMu.RUnlock()` around `p.claims` reads in `interceptSubscribe()` for defensive concurrency safety.

- [x] T018 Wire validator into Proxy in `ws/internal/gateway/gateway.go` — `HandleWebSocket()`: pass `Validator: gw.validator` and `AuthRefreshRateInterval: gw.config.AuthRefreshRateInterval` in `ProxyConfig` when creating new Proxy.

---

## Phase 4 — Tests

- [x] T019 [P] Create `ws/internal/gateway/auth_protocol_test.go` with table-driven tests for JSON marshaling/unmarshaling of `AuthData`, auth_ack response structure, and auth_error response structure. Verify all error code constants are valid strings.

- [x] T020 [P] Create `ws/internal/gateway/subscription_tracking_test.go` with table-driven tests: (1) track channels from `subscription_ack` payload, (2) remove channels from `unsubscription_ack` payload, (3) ignore broadcast messages (`{"type":"message",...}`) without JSON parse, (4) handle empty subscription set, (5) handle duplicate add/remove. Use a mock backend connection.

- [x] T021 Create `ws/internal/gateway/auth_refresh_test.go` with table-driven tests using a mock `TokenValidator`. Test cases: valid token refresh (same tenant, expect auth_ack with exp), expired token (expect auth_error token_expired), invalid signature (expect auth_error invalid_token), malformed token (expect auth_error invalid_token), tenant mismatch (expect auth_error tenant_mismatch), rate limited (two rapid requests, second gets rate_limited), auth disabled (expect auth_error not_available), empty token (expect auth_error invalid_token), missing data field (expect auth_error invalid_token). Include a `BenchmarkInterceptAuthRefresh` benchmark to validate NFR-001 (auth refresh < 50ms excluding external lookups) — benchmark the full `interceptAuthRefresh` flow with a pre-warmed mock validator.

- [x] T022 Add forced unsubscription tests to `ws/internal/gateway/auth_refresh_test.go`: (1) permission expanded — no forced unsub, auth_ack only, (2) permission reduced — synthetic unsubscribe sent to backend, unsubscription_ack sent to client with revoked channels, (3) permissions unchanged — no forced unsub, (4) all channels revoked — all unsubscribed, (5) no active subscriptions — auth_ack only.

- [x] T023 Add end-to-end proxy integration test to `ws/internal/gateway/proxy_test.go`: connect → subscribe → receive subscription_ack (verify tracking) → send auth refresh with reduced permissions → verify forced unsubscribe sent to backend → verify unsubscription_ack sent to client → verify subscription tracking updated.

- [x] T024 Verify Phase 4: run `cd ws && go vet ./...` and `cd ws && go test ./...`. All tests must pass including new and existing.

---

## Phase 5 — Helm & Deploy Verification

- [x] T025 [P] Add `GATEWAY_AUTH_REFRESH_RATE_INTERVAL` env var to `deployments/helm/sukko/charts/ws-gateway/templates/deployment.yaml`, sourcing from values. Add `authRefreshRateInterval: "30s"` to `deployments/helm/sukko/charts/ws-gateway/values.yaml` under the gateway config section.

- [x] T026 [P] Add auth refresh config to `deployments/helm/sukko/values/standard/dev.yaml` if gateway values are overridden there. Check if the file has gateway-specific overrides; if so, add `authRefreshRateInterval` to match.

- [x] T027 Verify Helm: run `helm lint deployments/helm/sukko/charts/ws-gateway`. Confirm no lint errors.

---

## Dependency Graph

```
Phase 1: T001 ─┬─ T004 ─┬─ T002 ─┬─ T007 ─── T008
               │  T005 ─┤  T003 ─┘
               │  T006 ─┘
               │
Phase 2:       T008 ─┬─ T009 [P] ─┐
               │     ├─ T010 [P]  ├─ T012
               │     └─ T011 [P] ─┘
               │
Phase 3:       T012 ── T013 ── T014 ── T015 ── T016 ── T017 ── T018
               │
Phase 4:       T018 ─┬─ T019 [P] ─┐
               │     ├─ T020 [P]  ├─ T021 ── T022 ── T023 ── T024
               │     └────────────┘
               │
Phase 5:       T024 ─┬─ T025 [P] ─┬─ T027
                     └─ T026 [P] ─┘
```

### Key Dependencies
- T001 must complete before T004-T006 (server protocol.go must exist before server imports are updated)
- T004+T005+T006 must complete before T002+T003 (server imports updated to use local symbols BEFORE removing from shared — otherwise compilation breaks)
- T002+T003 must complete before T007 (shared package cleaned before test verification)
- T008 (Phase 1 verification) gates Phase 2
- T009-T011 are parallelizable (separate files, no deps)
- T012 must complete before T013 (interface defined before struct uses it)
- T013-T018 are sequential (each builds on prior proxy changes)
- T019+T020 are parallelizable with each other
- T021 depends on T019+T020 patterns
- T025+T026 are parallelizable (separate Helm files)
