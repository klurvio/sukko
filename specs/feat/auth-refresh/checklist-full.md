# Requirements Checklist: Mid-Connection Auth Refresh (Full Sweep)

**Branch**: `feat/auth-refresh` | **Generated**: 2026-02-28 | **Constitution**: v1.3.2

---

## Concurrency

- [ ] CHK001 Are lock acquisition and release points specified for every shared data structure (`claims`, `subscribedChannels`)? [Completeness]
- [ ] CHK002 Is the mutex type (`sync.Mutex` vs `sync.RWMutex`) justified by the read/write ratio for each protected field? [Clarity]
- [ ] CHK003 Are the goroutine ownership boundaries documented — which goroutine reads and which writes each shared field? [Completeness]
- [ ] CHK004 Is the two-phase pattern (collect under lock, I/O after unlock) specified with explicit lock/unlock boundaries for `forceUnsubscribeRevokedChannels`? [Clarity]
- [ ] CHK005 Are requirements consistent between the mutex scope in `interceptAuthRefresh` (plan 3.3 step 8) and `forceUnsubscribeRevokedChannels` (plan 3.5)? [Consistency]
- [ ] CHK006 Is the window between lock release (Phase A) and claims swap (step 8) documented with its implications for concurrent subscription acks? [Coverage]
- [ ] CHK007 Are requirements for serialization of auth refresh with subscribe/unsubscribe defined without relying on implicit single-goroutine assumption? [Measurability]
- [ ] CHK008 Is the lock type for subscription tracking map writes (`Lock()` vs `RLock()`) explicitly specified for both the backend→client and client→backend goroutines? [Clarity]
- [ ] CHK009 Are requirements defined for what happens if `sendToClient()` or `forwardFrame()` blocks during Phase B of forced unsubscription? [Coverage]
- [ ] CHK010 Is the fire-and-forget rationale for backend unsubscribe documented with the acceptable failure window? [Completeness]
- [ ] CHK011 Are requirements specified for the `rate.Limiter` initialization — confirming it is per-connection (not shared across connections)? [Clarity]
- [ ] CHK012 Is the defensive `RLock` in `interceptSubscribe` justified with a rationale that doesn't contradict the single-goroutine serialization design? [Consistency]

## Security

- [ ] CHK013 Are requirements defined for validating the new JWT with the same validator and configuration used at connection time? [Completeness]
- [ ] CHK014 Is the tenant isolation requirement (`tenant_mismatch` rejection) specified as a hard block — not a warning? [Clarity]
- [ ] CHK015 Are all five auth error codes (`invalid_token`, `token_expired`, `tenant_mismatch`, `rate_limited`, `not_available`) defined with unambiguous trigger conditions? [Completeness]
- [ ] CHK016 Is the requirement that failed auth refresh MUST NOT modify session state (claims, permissions, subscriptions) explicitly stated? [Measurability]
- [ ] CHK017 Are requirements defined for what information is exposed in `auth_error` responses — no token content, no internal error details? [Coverage]
- [ ] CHK018 Is the rate limit default (1 per 30s) justified with reference to typical JWT lifetimes and abuse scenarios? [Clarity]
- [ ] CHK019 Are requirements specified for handling JWTs with missing or malformed `exp` claims during refresh? [Coverage]
- [ ] CHK020 Is the requirement that auth messages MUST NOT be forwarded to the backend specified as a security invariant, not just a routing rule? [Clarity]
- [ ] CHK021 Are requirements defined for logging auth refresh failures at Warn level without including the token string? [Coverage]
- [ ] CHK022 Is the `AuthEnabled=false` behavior specified — does the gateway return `not_available` or silently drop the message? [Clarity]

## Deployment

- [ ] CHK023 Is the env var name `GATEWAY_AUTH_REFRESH_RATE_INTERVAL` consistent between Go struct tag, Helm deployment template, and Helm values? [Config Coherence]
- [ ] CHK024 Is the default value `30s` consistent between Go `envDefault` tag and Helm `values.yaml`? [Config Coherence]
- [ ] CHK025 Are validation bounds for `AuthRefreshRateInterval` (>= 1s) specified in both Go code and documented for operators? [Completeness]
- [ ] CHK026 Is the startup failure behavior on invalid config specified — does the gateway refuse to start or fall back to default? [Clarity]
- [ ] CHK027 Are requirements defined for backward compatibility — what happens to existing gateways that don't have the new env var set? [Coverage]
- [ ] CHK028 Is the deployment scope documented — gateway-only redeploy, no server changes at runtime? [Completeness]
- [ ] CHK029 Are requirements specified for the Helm values path (`values/standard/dev.yaml`) being conditional vs. required? [Clarity]
- [ ] CHK030 Is the `LogConfig()` output format for the new config field specified to match existing patterns? [Consistency]

## Monitoring

- [ ] CHK031 Are metric names specified with the correct prefix (`gateway_`) and units (`_total`, `_seconds`)? [Config Coherence]
- [ ] CHK032 Is the `result` label on `gateway_auth_refresh_total` defined with an exhaustive, closed set of values? [Completeness]
- [ ] CHK033 Is label cardinality bounded — are the 6 `result` values the maximum, with no risk of unbounded growth? [Measurability]
- [ ] CHK034 Are histogram buckets for `gateway_auth_refresh_latency_seconds` specified or deferred to "standard latency buckets"? [Clarity]
- [ ] CHK035 Is the `gateway_forced_unsubscriptions_total` metric defined as a Counter (monotonic) vs. a Gauge? [Clarity]
- [ ] CHK036 Are requirements defined for when each metric is recorded — on attempt, on completion, on error? [Completeness]
- [ ] CHK037 Is the latency metric defined to measure end-to-end auth refresh duration or just validation time? [Clarity]
- [ ] CHK038 Are requirements specified for structured log fields on auth refresh — which fields at Info vs. Warn level? [Completeness]
- [ ] CHK039 Can the 50ms NFR-001 target be objectively measured given the "excluding external lookups" caveat? [Measurability]
- [ ] CHK040 Are requirements defined for a Grafana dashboard or alert rules for the new metrics, or is that explicitly out of scope? [Coverage]

## Cross-Domain

- [ ] CHK041 Are the spec's acceptance criteria (Scenarios 1–5) each traceable to at least one test case in the task list? [Coverage]
- [ ] CHK042 Is the forced unsubscription flow specified end-to-end — from permission diff to backend unsub to client notification to metric recording? [Completeness]
- [ ] CHK043 Are requirements consistent between the spec's edge cases section and the plan's forced unsubscription design? [Consistency]
- [ ] CHK044 Is the prior art research (R7) traceable to specific design decisions in the plan — not just informational? [Measurability]
- [ ] CHK045 Are requirements for the `TokenValidator` interface specified with enough detail to write a mock without reading implementation code? [Clarity]

---

**Summary**: 45 items | Concurrency: 12 | Security: 10 | Deployment: 8 | Monitoring: 10 | Cross-Domain: 5
