# Feature Specification: Release Readiness Documentation

**Branch**: `docs/release-readiness`
**Created**: 2026-05-26
**Status**: Draft
**Passes**: clarify: 2 | analyze: 0

## Context

Sukko is approaching its first production release. There are two distinct audiences with two distinct documentation gaps, plus a process gap that will cause both to drift after release.

**Audience separation is critical:**
- **Developers** build, test, and release Sukko. They need a technical e2e testing guide (`ws/docs/e2e-testing.md`) that tells them exactly what to run, in what order, and what a passing system looks like. This lives in the sukko repo and is a developer tool.
- **Operators** deploy and manage Sukko in production. They consume `sukko-docs` — a separate documentation site written for people who will never read the codebase. Developers do not use sukko-docs for their own work; they use it only to audit and validate that the operator experience is correct.

**Gap 1 — No developer e2e testing guide exists.** Developers validating a release or a feature branch have no single document telling them what to run, in what order, and what a passing system looks like. The tester service and sukko-cli together cover the full feature surface but there is no document tying them together into a cohesive test battery. This means release validation is informal, inconsistent, and relies on tribal knowledge.

**Gap 2 — Operator documentation (`sukko-docs`) has known accuracy issues.** The quickstart references stale channel formats, some guides have not been updated to reflect routing rules replacing the old topic-suffix model, and CLI command references may lag implementation. An operator following these docs today will hit broken examples before they finish setup. Poor DX at first contact is a hard barrier to adoption. Developers are responsible for auditing sukko-docs before each release to ensure the operator experience is correct — not for using it themselves.

**Gap 3 — Neither document is required to stay in sync with the codebase.** The constitution covers OpenAPI/AsyncAPI contract maintenance (XVII) and cross-repo awareness (XVI) but neither explicitly mandates that the e2e test guide or the operator docs be updated when the codebase changes. Without a constitution mandate, both documents will drift again the moment the next feature lands.

This spec covers all three gaps as a single release-readiness initiative.

---

## User Scenarios

### Scenario 1 — Developer validates a release branch (Priority: P1)

A developer has just merged a feature branch into main. Before cutting a release tag they need to verify the entire system works end-to-end: provisioning, authentication, WebSocket delivery, SSE, REST publish, edition enforcement, token revocation, tenant isolation, and push notifications.

**Acceptance Criteria**:
1. **Given** a fresh local environment (`sukko up`), **When** the developer follows the e2e testing document from top to bottom, **Then** every step produces an unambiguous pass/fail signal with no prior knowledge required.
2. **Given** the e2e document, **When** the developer reaches any tester suite section, **Then** the exact `sukko test` command is shown alongside what the suite validates and what a passing result looks like.
3. **Given** the e2e document, **When** the developer reaches any manual CLI verification section, **Then** the exact `sukko` command (or `curl` where no CLI command exists), expected output, and failure indicators are shown.
4. **Given** any feature (routing rules, edition gates, token revocation, push, etc.), **When** a developer looks it up in the e2e doc, **Then** the relevant edge cases and failure modes are documented alongside the happy path.

### Scenario 2 — Operator follows sukko-docs to deploy and validate (Priority: P1)

An operator follows `sukko-docs` to deploy Sukko, onboard a tenant, and verify their deployment is working correctly. They have no prior knowledge of the codebase and will never look at source code. The developer's role in this scenario is to audit sukko-docs before release and fix any inaccuracies so the operator experience is seamless.

**Acceptance Criteria**:
1. **Given** the quickstart guide, **When** an operator follows it step by step, **Then** they reach a working WebSocket connection with no broken commands or stale examples.
2. **Given** any guide page (tenant onboarding, SSE, push notifications, testing), **When** the operator runs the CLI commands shown, **Then** the commands succeed and produce output matching the documented examples.
3. **Given** the routing rules documentation in sukko-docs, **When** an operator reads it, **Then** it correctly describes the `topics` array (not `topic_suffix`), the fan-out behavior, the `**` pattern semantics, and the priority evaluation order — all without requiring knowledge of the Go implementation.
4. **Given** the CLI reference in sukko-docs, **When** an operator runs `sukko --help` and compares it to the docs, **Then** every command, subcommand, and flag in the docs exists in the binary and vice versa.
5. **Given** the configuration reference in sukko-docs, **When** an operator searches for an environment variable, **Then** the docs show the correct name, type, default, and description.
6. **Given** any edition-gated feature page in sukko-docs, **When** an operator reads it, **Then** the required edition (Community / Pro / Enterprise) is stated prominently before any setup instructions.

### Scenario 3 — Operator validates that the features they use work in their deployment (Priority: P1)

An operator has deployed Sukko (locally via `sukko up` or remotely in their cloud environment) and needs to confirm it works correctly for their use case. They care about the features relevant to their edition and configuration — not about internal system behaviors, message ordering guarantees, or race conditions. That depth of testing is a developer concern.

The operator testing guide in sukko-docs covers what operators need: is my deployment healthy, can I provision a tenant, do connections work, do the features I'm paying for function correctly? It is comprehensive for that scope, not for the entire system.

In this workflow:
- **sukko-cli is the orchestrator** — it manages the environment (`sukko up/down`), provisions tenants, sets up keys and rules, configures contexts, and sequences the validation workflow.
- **the tester is the executor** — it runs validation suites against whatever environment the CLI context points to, returning structured pass/fail results.
- **context switching** (`sukko context use <name>`) is the only difference between testing locally and testing remotely — the commands are identical.

Local environments have real limitations (no TLS, no cloud infrastructure, no push provider) that must be documented so operators set appropriate expectations.

**Acceptance Criteria**:
1. **Given** a machine with Docker and sukko-cli installed, **When** an operator follows the testing guide in sukko-docs, **Then** they can spin up a local environment, complete the admin keypair setup for suites that require it, run the validation suites applicable to their edition (after activating their license key locally if using Pro/Enterprise), and get a clear pass/fail result for the features they care about — no cloud account, no codebase access.
2. **Given** a remote deployment configured as a context, **When** the operator runs `sukko context use <name>` and re-runs the same `sukko test` commands, **Then** the suites execute against the remote deployment with no other changes required.
3. **Given** any suite relevant to the operator's edition, **When** they look it up in the testing guide, **Then** they find: what it validates in plain language, whether it works locally, the required edition, the exact command, and what healthy output looks like.
4. **Given** a failing suite result, **When** the operator consults the testing guide, **Then** they can identify what failed and what CLI commands to run to diagnose and fix it — without needing a developer.
5. **Given** the local vs. remote limitations section, **When** an operator reads it, **Then** they understand which validations require a real deployment (e.g., push notifications need a configured provider, TLS requires a real load balancer) and what to do instead locally.

### Scenario 4 — Developer adds a new feature (Priority: P2)

A developer adds a new endpoint, changes a WebSocket message schema, or modifies system behavior. The constitution must force them to update the e2e test document (developer tool) and trigger a sukko-docs update (operator tool) as part of the same PR. The key distinction: the developer updates `ws/docs/e2e-testing.md` themselves; they either update sukko-docs themselves or open and link a cross-repo PR so the operator docs stay accurate.

**Acceptance Criteria**:
1. **Given** the updated constitution, **When** a code reviewer reviews a PR that adds or changes a testable behavior, **Then** a missing `ws/docs/e2e-testing.md` update is a blocking constitution violation.
2. **Given** the updated constitution, **When** a code reviewer reviews a PR that changes operator-visible behavior (env vars, CLI flags, API schemas, feature gates, user-facing error codes), **Then** a missing sukko-docs update (or linked cross-repo PR) is a blocking constitution violation.

### Edge Cases

- What if a tester suite requires Pro/Enterprise edition to run? Both documents must specify which edition is required per suite (see normative suite→edition table in FR-003). Running an edition-gated suite on an insufficient edition produces a `FAIL` result at the first gated API call (HTTP 403) — not a graceful `SKIP`. Only `token-revocation` returns `SKIP` on Community (explicit check at startup); all others fail hard. Both documents must make this explicit so developers and operators activate the correct license before running edition-gated suites.
- What if `sukko up` fails to start a service? Both the e2e doc (for developers) and the testing guide (for operators) must cover health verification before running suites — and provide actionable steps when a service is not ready.
- What if routing rules cannot be set because the noop Kafka admin rejects custom topics? Both documents must explain which topic suffixes are valid in the current Phase 1 state (`default`, `dead-letter` only) — in technical terms for developers, in plain language for operators.
- What if an operator has a Community license and tries to follow the SSE guide in sukko-docs? The sukko-docs guide must state the required edition at the top — the operator should know before reading setup instructions.
- What if sukko-docs has a page that references a removed CLI command? The developer accuracy audit (a step in `ws/docs/e2e-testing.md` covering sukko-docs validation) must catch this before it reaches operators.
- What if an operator switches context from local to remote and tests fail? The operator testing guide must explain how to distinguish environment connectivity issues from actual feature failures, and what CLI commands to use to diagnose (`sukko health`, `sukko status`, `sukko test smoke`).
- What if an operator's suites fail with "RequireAdminProvider is true"? The provisioning, tenant-isolation, and token-revocation suites require a pre-registered admin keypair (TESTER_ADMIN_KEY_FILE). The testing guide must document this prerequisite and include it in troubleshooting.
- What if edition-limits is interrupted and orphaned `_sukko_test_edition_*` tenants contaminate the next run? The testing guide must document how to identify and clean up these tenants.

---

## Requirements

### Functional Requirements

**E2E Testing Document (`ws/docs/e2e-testing.md`)**

- **FR-001**: The document MUST include a prerequisites section covering environment setup: `sukko init`, `sukko up`, admin auth (`sukko auth keygen`, `sukko auth register`), and health verification. Health verification MUST show the expected `sukko health` and `sukko status` output on a healthy system and MUST state that no suites should be started until health verification passes (all services `ok`, all containers running). The prerequisites section MUST also include running `sukko test smoke` as the final pre-battery health check — smoke is a standalone command (`sukko test smoke`), not part of the validate battery.

- **FR-002**: The document MUST cover the full validate suite battery in execution order: `provisioning` → `auth` → `channels` → `pubsub` → `ordering` → `reconnect` → `ratelimit` → `tenant-isolation` → `token-revocation` → `sse` → `rest-publish` → `push` → `edition-limits` → `license-reload`. Note: `smoke` is run separately as a prerequisite step (FR-001), not as a validate suite. The ordering must be preserved as stated — `license-reload` runs last because it temporarily mutates the deployment's live edition. If `license-reload` is interrupted and its edition-restore step fails, the deployment may be left on an unexpected edition, causing silent skips in subsequent runs; the document MUST include a manual recovery step (`sukko license push <original-key>`) for this case.

- **FR-003**: For each validate suite the document MUST show: the exact `sukko test validate --suite <name>` command, a one-paragraph description of what the suite validates, the prerequisite edition (see normative table below), and what a passing result looks like. Note: `sukko test load`, `sukko test stress`, and `sukko test soak` are standalone subcommands with different invocation patterns — they are documented under FR-009, not FR-003. Note: `sukko test status --id` is referenced in CLI output but is not yet implemented; the document MUST NOT describe it as an available command.

  **Normative suite → minimum required edition table** (single source of truth for both the e2e doc and the operator testing guide):

  | Suite | Minimum Edition | Notes |
  |---|---|---|
  | `provisioning` | Community | Requires registered admin keypair (TESTER_ADMIN_KEY_FILE) |
  | `auth` | Community | |
  | `channels` | Community | |
  | `pubsub` | Community | |
  | `ordering` | Community | |
  | `reconnect` | Community | |
  | `ratelimit` | Community | |
  | `tenant-isolation` | Community | Requires registered admin keypair (TESTER_ADMIN_KEY_FILE) |
  | `token-revocation` | Pro | Gates on `edition != Community`; requires registered admin keypair |
  | `sse` | Pro | |
  | `rest-publish` | Pro | |
  | `push` | Enterprise | |
  | `edition-limits` | Community | |
  | `license-reload` | Community | Requires signing key (TESTER_SIGNING_KEY_FILE) |

  **Important — edition enforcement behavior**: Running an edition-gated suite (`sse`, `rest-publish`, `push`, `token-revocation`) on a deployment with an insufficient edition produces a `FAIL` result at the first gated API call (HTTP 403), not a graceful `SKIP`. The document MUST make this clear so developers and operators know to activate the correct license before running these suites rather than interpreting the failure as a system error.

- **FR-004**: The document MUST include a manual verification section for every provisioning API operation: tenant lifecycle (create, get, list, update, suspend, reactivate, deprovision with grace period, force deprovision), JWT keys, API keys, routing rules, channel rules, quotas, and audit log. For operations where no CLI command currently exists (`sukko tenant rename`, audit log retrieval via `GET /tenants/{slug}/audit`), verification steps MUST use `curl`/`httpie` examples and MUST explicitly note that the CLI does not yet support these operations (known CLI coverage gaps tracked for a future CLI release).

- **FR-005**: Each manual verification step MUST show: the exact `sukko` CLI command (or `curl` where no CLI command exists), the expected output or response with the key fields that indicate correct operation (not just shape — e.g., `"status": "active"` for a created tenant, `"suspended_at"` non-null for a suspended tenant), which fields are dynamic vs. fixed, and at least one edge case or failure mode for that operation.

- **FR-006**: The document MUST have a dedicated section for routing rules covering: pattern syntax (`**` at head/middle/tail, exact literals), priority evaluation order, fan-out to multiple topics, valid topic suffixes in the current Phase 1 state (`default` and `dead-letter` only), and the `TOO_MANY_ROUTING_RULES` / `ROUTING_RULE_VALIDATION_ERROR` / `TOPIC_NOT_PROVISIONED` error codes.

- **FR-007**: The document MUST include edition enforcement verification: which features require Pro/Enterprise, how to verify the gate blocks Community users (expected 403 + error code), and how to verify the gate passes with a valid license.

- **FR-008**: The document MUST include a WebSocket protocol section covering: connect with JWT, connect with API key, connect with both, subscribe/unsubscribe acknowledgment, message delivery, forced unsubscription (`forced: true` in `UnsubscriptionAck`), and connection limit enforcement (429). Note: the forced unsubscription message shape is constructed inline in `internal/gateway/proxy.go` (not in a canonical protocol type) — the writer MUST verify the full `{type, unsubscribed, forced}` payload against that file, not only from `protocol/types.go`.

- **FR-009**: The document MUST include a load/stress/soak section describing `sukko test load`, `sukko test stress`, and `sukko test soak` with their key flags and what metrics to watch. The document MUST note that `sukko test status --id` is referenced in CLI hint text but is not yet implemented. The streaming flag name MUST be verified from `sukko test run --help` (or the equivalent load/stress/soak subcommand help) before documenting — do not assume the flag is named `--follow`; verify the actual flag name against the current binary. The underlying streaming endpoint is `GET /api/v1/tests/{id}/metrics` (SSE stream).

- **FR-010**: The document MUST have a "test by feature area" index so a developer testing only one area can jump directly to the relevant section without reading the whole document.

**Operator Documentation (`../sukko-docs`) — written for operators, audited by developers**

sukko-docs is the operator-facing documentation site. Operators consume it directly; developers validate its accuracy before each release and update it when operator-visible behavior changes. Content must be written for someone who will never read the codebase.

- **FR-011**: The quickstart MUST use the current channel format (`{tenant}.{suffix}`) and routing rules format (`topics` array, not `topic_suffix`), with working CLI commands that an operator can copy-paste without modification.

- **FR-012**: The tenant onboarding guide MUST explain routing rules in operator-friendly terms: what patterns match, what topics array does (fan-out to multiple Kafka topics), how priority works, and — critically — that topic suffixes must exist before routing rules can reference them (Phase 1 constraint stated in plain language, without referencing Go internals).

- **FR-013**: The CLI reference page MUST document every command and subcommand visible in `sukko --help` and `sukko <cmd> --help` at time of writing. The canonical command enumeration MUST be derived from running the binary — the spec's illustrative list (below) is not normative. Written from the operator's perspective — what the command does, not how it's implemented. Illustrative (not normative, verify against binary): `sukko up/down/init/status/health`, `sukko tenant`, `sukko keys`, `sukko api-keys`, `sukko auth`, `sukko rules routing/channels`, `sukko quota`, `sukko token`, `sukko connections`, `sukko publish`, `sukko subscribe`, `sukko license`, `sukko edition`, `sukko test`, `sukko grafana`, `sukko logs`, `sukko context`, `sukko config`, `sukko version`, `sukko completion`.

- **FR-014**: The configuration reference MUST list every environment variable for ws-server, ws-gateway, provisioning, and the tester service (for the testing guide). Requirements:
  - **Discovery method**: Source env vars from `ws/internal/shared/platform/` struct tags. Each service's complete set = its own struct fields plus all embedded structs (`BaseConfig`, `AuthConfig`, `ProvisioningClientConfig`, `HTTPTimeoutConfig`, `KafkaNamespaceConfig`, `MessageBackendConfig`, etc.). Shared vars (e.g., `LOG_LEVEL`, `PROVISIONING_GRPC_ADDR`) MUST appear in a "Shared / Common" section with a cross-reference note per service. Tester env vars come from `ws/cmd/tester/config.go`.
  - **Table format**: Each table MUST have columns: `Variable | Type | Default | Unit / Valid values | Required | Description`. Conditional requirements MUST be expressed as `required when VAR=VALUE` in the Required column (e.g., `KAFKA_BROKERS`: "required when `MESSAGE_BACKEND=kafka`").
  - **Tiering**: Variables MUST be organized into: (1) Required to deploy (no default or production-unsafe default), (2) Commonly tuned (operators change to match hardware/config), (3) Advanced / rarely changed (internal tuning knobs). All three tiers are documented; tiers 1 and 2 are featured prominently.
  - **Secrets**: Vars tagged `redact:"true"` (e.g., `SUKKO_LICENSE_KEY`, `DATABASE_URL`, `NATS_PASSWORD`, `KAFKA_SASL_PASSWORD`, `CREDENTIALS_ENCRYPTION_KEY`) MUST be grouped in a "Secrets & Credentials" subsection with a "Sensitive — auto-redacted in `/config` endpoint, never log" annotation.
  - **Security-critical vars without envDefault**: Some vars have no `envDefault` yet carry a critical security implication when empty. `TESTER_AUTH_TOKEN` is the key example: when empty, the tester accepts all requests unauthenticated. These vars MUST be marked "required in production" in the Required column even though struct-tag discovery alone will show no default. The writer MUST check auth middleware logic, not just struct tags.
  - **DATABASE_URL special case**: The `DATABASE_URL` pool params (`pool_max_conns`, `pool_min_conns`, `pool_max_conn_lifetime`, `pool_max_conn_idle_time`) are not separate env vars — they are pgxpool library conventions embedded in the URL query string. Document them as an extended description note on the `DATABASE_URL` row itself (not as separate rows), derived from the `Validate()` comment in `provisioning_config.go`.

- **FR-015**: The testing guide (`guides/testing.mdx`) MUST be a comprehensive, standalone reference for operators validating their own deployment. Its scope is what operators need to verify — not every internal system behavior. It MUST be self-contained: operators must never need to consult source code, ask a developer, or reference the developer e2e doc to follow it.

- **FR-015a**: The guide MUST open with the CLI-as-orchestrator / tester-as-executor model in plain language: sukko-cli sets up and controls the environment; the tester runs the validation suites; context switching (`sukko context use`) is the only change needed to go from local to remote.

- **FR-015b**: The guide MUST include a local vs. remote limitations section that tells operators plainly what they can and cannot validate in a Docker environment — and why. Examples: push notifications need a real push provider, TLS requires a real load balancer, edition-gated suites require a license key activated via `sukko license set <key>` followed by `sukko down && sukko up` before they will pass.

  The section MUST accurately document the admin keypair requirement: the `provisioning`, `tenant-isolation`, and `token-revocation` suites require a **registered** admin keypair (`TESTER_ADMIN_KEY_FILE`) in **both local and remote mode**. `sukko up` does not register a local admin keypair automatically — the ephemeral keypair the tester generates internally is not registered with the provisioning service, so those suites will fail without an out-of-band `sukko auth register` step. Suites that do NOT require a registered admin keypair (`auth`, `channels`, `pubsub`, `ordering`, `reconnect`, `ratelimit`, `edition-limits`, `license-reload`) run normally in local mode. The guide MUST list which suites work locally without additional keypair setup, and which require the `sukko auth keygen` + `sukko auth register` step even in a local Docker environment.

- **FR-015c**: The guide MUST document the suites operators actually use: `smoke` (standalone `sukko test smoke`), `provisioning`, `pubsub`, `token-revocation` (Pro), `sse` (Pro), `rest-publish` (Pro), `push` (Enterprise), `edition-limits`, and `license-reload`. The selection criterion is: suites that validate deployment health and feature availability for an operator's edition, including all edition-gated capabilities with a user-observable behavioral contract.

  **Inclusion rationale**: `token-revocation` is included (not developer-internal) because it is a named, marketed, Pro-gated capability (`TokenRevocation` feature in `features.go`) that validates a user-observable behavior — that connected clients are force-disconnected after a revocation call. An operator on Pro has a legitimate need to validate this works in their deployment. The developer-internal category covers suites that test internal correctness invariants without a user-visible behavioral contract: `auth` (auth implementation edge cases), `channels` (channel subscription mechanics), `ordering` (FIFO delivery guarantees), `reconnect` (session recovery timing), `ratelimit` (enforcement thresholds), `tenant-isolation` (cross-tenant data leakage prevention). These MUST be mentioned briefly as available advanced/developer-level suites.

  For each operator suite: the exact command, what it validates in operator terms, required edition (from the normative table in FR-003), whether it requires a registered admin keypair, local/remote availability, and sample healthy output.

- **FR-015d**: The guide MUST include a deployment validation checklist — a sequential procedure operators run after every deployment. The checklist MUST be ordered as follows:
  1. Pre-run cleanup: verify no orphaned `_sukko_test_edition_*` tenants exist (`sukko tenant list | grep _sukko_test_edition_`; delete any found).
  2. Health check: `sukko health` and `sukko status` — all services ok, all containers running.
  3. Smoke: `sukko test smoke` — confirm end-to-end message delivery is alive.
  4. Admin keypair setup (required for the provisioning suite): `sukko auth keygen` + `sukko auth register` + set `TESTER_ADMIN_KEY_FILE`. This step is required even locally.
  5. Provisioning: `sukko test validate --suite provisioning` — confirm tenant lifecycle and API work correctly.
  6. WebSocket delivery: `sukko test validate --suite pubsub` — confirm message delivery via WebSocket.
  7. Edition-gated features applicable to their license (e.g., `sukko test validate --suite sse` for Pro, `sukko test validate --suite push` for Enterprise).

  This is the operator's "is my deployment healthy?" ritual. The checklist must be self-contained: an operator must not need to read any other section to complete it.

- **FR-015e**: The guide MUST include a troubleshooting section for operator-facing failure modes:
  1. **Tester unreachable**: Distinguish three failure modes — (a) tester process not running (error: "cannot connect to tester at X. Is it running?") — fix: `sukko up` or check container status; (b) auth token mismatch (HTTP 401 UNAUTHORIZED) — fix: verify `TESTER_AUTH_TOKEN` matches the tester's configured token; (c) wrong URL/port — fix: check context configuration via `sukko context list`. Include the exact CLI output for each and the resolution command.
  2. **Wrong edition for a suite**: Suite returns edition-gated failure — identify required edition from normative table, check current edition via `sukko edition`, activate a license with `sukko license set <key>` if needed.
  3. **Routing rules rejected** (topic not provisioned): `TOPIC_NOT_PROVISIONED` error — topic suffix must be provisioned before routing rules can reference it; Phase 1 only `default` and `dead-letter` work.
  4. **license-reload: signing key required**: Suite fails with "signing key required for license-reload suite" — `TESTER_SIGNING_KEY_FILE` is not set; the signing key is the Ed25519 private key used to sign license tokens (held by the license issuer). Document where to obtain it and how to pass it.
  5. **license-reload: failed edition restore**: After an interrupted license-reload run, the deployment may remain on an unexpected edition — use `sukko license push <original-key>` to restore manually before re-running any suites.
  6. **suite fails: RequireAdminProvider**: Suites requiring admin keypair (`provisioning`, `tenant-isolation`, `token-revocation`) fail with "AdminProvider is nil but RequireAdminProvider is true" — `TESTER_ADMIN_KEY_FILE` is not set or points to an unregistered keypair. Resolution: run `sukko auth keygen` to generate a keypair, then `sukko auth register` to register it with the provisioning service, then configure `TESTER_ADMIN_KEY_FILE` to point to the generated key. This step is required in both local and remote mode — `sukko up` does not perform this registration automatically.
  7. **Orphaned edition-limits tenants**: If edition-limits was interrupted, `_sukko_test_edition_*` tenants may remain and affect subsequent tenant-count assertions — enumerate and delete before re-running.
  8. **Smoke: publish round-trip timeout**: "message not received within timeout" can mean routing rules were not set (routing-rule setup failure is silent in smoke), gateway connectivity issue, or message backend issue. Diagnostic sequence: verify routing rules exist via `sukko tenant routing-rules get <slug>`, then check gateway health, then check message backend health.
  9. **Context switch from local to remote: tests fail**: Distinguish environment connectivity from feature failure — run `sukko health` and `sukko status` against the remote context first; if services are reachable and healthy, then the failure is a feature issue; if not, debug connectivity first.

- **FR-016**: Every sukko-docs page covering an edition-gated feature (SSE, push notifications, REST publish, analytics) MUST state the required edition (e.g., "Requires Pro") as the first visible element — before prerequisites, before setup steps.

- **FR-017**: The editions comparison page MUST accurately reflect which features are available today vs. planned. Accuracy verification MUST start with `features.go` (`featureMetadata` map in `ws/internal/shared/license/features.go`) as the canonical source — any mismatch between `featureMetadata` and the sukko-docs editions page MUST be resolved by updating `features.go` first (updating `// Implemented` vs. `// Future` status on the `Feature` constant), then the sukko-docs page. Editing the sukko-docs page without a corresponding `features.go` update risks silent regression if the extraction pipeline regenerates the page from source.

**Constitution Amendment**

- **FR-018**: Extend constitution section XVI (Cross-Repo Awareness) to add `ws/docs/e2e-testing.md` as a named in-repo documentation artifact. The amendment MUST add an explicit trigger: "Any PR that adds a new tester suite, changes an error code returned by a handler, adds or removes an env var, changes a WebSocket message type, or adds an API endpoint MUST update `ws/docs/e2e-testing.md`. PRs that refactor without changing behavior visible to the tester or to operators are exempt." The amendment MUST add a code review requirement: a missing `ws/docs/e2e-testing.md` update when a trigger applies is a blocking constitution violation.

- **FR-019**: Extend XVI to strengthen the sukko-docs trigger language. Replace "New features or behavioral changes → relevant guide pages MUST be updated" with an explicit enumerated trigger: "Any change to user-facing error messages, WebSocket protocol message types, HTTP error codes or error body schemas, connection close codes, env var names/defaults/types, CLI command/flag behavior, feature gate additions, or user-visible CLI output format triggers a sukko-docs update (or a linked cross-repo PR to `../sukko-docs`). The phrase 'user-visible behavior' without enumeration is forbidden as a trigger description — triggers MUST be explicit."

- **FR-020**: The XVI code review requirement (requirement 3) MUST be expanded to explicitly include `ws/docs/e2e-testing.md` alongside the existing cross-repo obligations.

- **FR-021**: The constitution version bump resulting from FR-018/FR-019/FR-020 is 1.19.0 → 1.20.0 (MINOR — extending/adding principles per governance rules).

### Non-Functional Requirements

- **NFR-001**: The e2e document MUST be runnable by a developer with no prior Sukko knowledge — every command must be complete and copy-pasteable.
- **NFR-002**: The e2e document MUST be structured so CI can eventually automate it — section headings and command formats MUST follow a consistent pattern.
- **NFR-003**: sukko-docs updates MUST be in the `../sukko-docs` repo (operator docs, separate repo) — never in the sukko repo. They MUST be linked from the sukko PR per XVI. The language in sukko-docs MUST be written for operators, not developers — no references to Go packages, internal struct names, or codebase conventions.
- **NFR-004**: The constitution amendment version bump MUST be MINOR (1.19.0 → 1.20.0) per governance rules, as this extends/adds principles.

### Docs Requirements

- `ws/docs/e2e-testing.md` — developer tool, lives in the sukko repo, written for developers.
- `../sukko-docs` updates — operator tool, lives in the sukko-docs repo, written for operators. Delivered as a companion PR to sukko-docs, linked from this sukko PR. Must never contain codebase-internal language.
- `CLAUDE.md` constitution amendment — delivered inline in the sukko repo. Extends section XVI; bumps version to 1.20.0.

### CLI Requirements

No new CLI commands are implemented by this spec. Two CLI coverage gaps are acknowledged and explicitly documented in the e2e guide:
1. `sukko tenant rename` — the provisioning API has a rename endpoint (`POST /api/v1/tenants/{slug}/rename`) but no CLI command exists. The e2e doc documents this step via curl.
2. Audit log retrieval — the provisioning API has `GET /api/v1/tenants/{slug}/audit` but no `sukko audit` command exists. The e2e doc documents this via curl.

Both gaps are tracked as CLI backlog items. The spec does not require implementing them; it requires the e2e doc to be honest about the gaps.

### API Contract Requirements

No API contract changes required. This spec produces documentation, not code or schema changes.

---

## Success Criteria

- **SC-001**: A developer unfamiliar with the codebase can follow `ws/docs/e2e-testing.md` from prerequisites to full suite completion and reach a passing result on a healthy local environment (defined as: `sukko health` returns 200 with all services `ok`, `sukko status` shows all containers running, `sukko test smoke` passes) — without consulting any other source.
- **SC-002**: `smoke` is covered as a prerequisite verification step, and all 14 validate suites (`provisioning`, `auth`, `channels`, `pubsub`, `ordering`, `reconnect`, `ratelimit`, `tenant-isolation`, `token-revocation`, `sse`, `rest-publish`, `push`, `edition-limits`, `license-reload`) are covered in the e2e document.
- **SC-003**: Running `sukko --help` recursively and comparing the output to the CLI reference in sukko-docs produces zero discrepancies. The e2e doc MUST include a "Developer Accuracy Audit" section with an explicit procedure for performing this comparison (enumerate all subcommands, compare section by section, record any discrepancy as a blocker before release).
- **SC-004**: Every environment variable in `ws/internal/shared/platform/` (for ws-server, ws-gateway, provisioning) and in `ws/cmd/tester/config.go` (for the tester) with an `envDefault` tag is present in the sukko-docs configuration reference with the correct default value, organized per FR-014's table format and tiering requirements.
- **SC-005**: The routing rules documentation in both the e2e doc and sukko-docs uses `topics` (array), not `topic_suffix`, and correctly describes fan-out and Phase 1 constraints.
- **SC-006**: The constitution contains an explicit maintenance requirement for `ws/docs/e2e-testing.md` and `/sukko-docs` (as an extension of XVI) that would be surfaced as a violation in code review when docs are not updated alongside code changes. Version is bumped to 1.20.0.
- **SC-007**: An operator following the quickstart in sukko-docs reaches a working WebSocket connection with a real message delivered — no broken commands, no stale examples, no missing steps. The operator never needs to look at source code or ask a developer to interpret the instructions.
- **SC-008**: An operator with no prior Sukko knowledge can follow the testing guide in sukko-docs and successfully: (a) run the suites applicable to their edition on a local Docker environment, (b) interpret every result as pass/fail with a clear reason, and (c) identify remediation steps for any failure — entirely from the guide, no cloud account, no codebase access, no developer assistance.
- **SC-009**: An operator can switch from local to remote testing by running `sukko context use <name>` and re-running the same `sukko test` commands unchanged — the testing guide makes this workflow explicit with a dedicated context switching section.
- **SC-010**: The developer e2e guide (`ws/docs/e2e-testing.md`) covers more procedures than the operator testing guide — that is expected and correct. The operator guide covers what operators need to validate their deployment (8 operator-relevant suites). The developer guide covers all 14 validate suites plus smoke plus manual CLI verification for every provisioning operation. They serve different audiences with different scopes.

---

## Out of Scope

- Automated e2e CI pipeline (running the document in CI is a future initiative).
- Video tutorials or interactive guides.
- SDK documentation (sukko-js, sukko-python, etc.).
- Performance benchmarks or SLA targets.
- Monitoring/alerting runbooks.
- Disaster recovery playbooks.
- Changes to the tester service itself (the document uses the tester as-is).
- New CLI commands or provisioning API endpoints (the two CLI gaps documented in CLI Requirements are tracked backlog items, not in-scope deliverables).
- Auto-generating the configuration reference from Go struct tags (out of scope for this sprint; manual sync with FR-018/FR-019 constitution triggers as the enforcement mechanism).
