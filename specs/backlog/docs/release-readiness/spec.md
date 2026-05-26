# Feature Specification: Release Readiness Documentation

**Branch**: `docs/release-readiness`
**Created**: 2026-05-26
**Status**: Draft
**Passes**: clarify: 0 | analyze: 0

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
3. **Given** the e2e document, **When** the developer reaches any manual CLI verification section, **Then** the exact `sukko` command, expected output, and failure indicators are shown.
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

### Scenario 3 — Operator validates their deployment comprehensively (Priority: P1)

An operator needs to validate that their Sukko deployment — whether local (Docker Compose via `sukko up`) or remote (cloud/Kubernetes) — is working correctly across every feature area. They are not a developer and will not read source code, but they need the same confidence in system correctness that a developer gets from the full e2e test battery.

The operator testing guide in sukko-docs is their complete reference. Every testing procedure that exists for developers must be represented here in operator-appropriate language and tooling. "Only available to developers" is not an acceptable state for any procedure that validates production deployment correctness.

In this workflow:
- **sukko-cli is the orchestrator** — it manages the environment (`sukko up/down`), provisions tenants, sets up keys and rules, configures contexts, and sequences the test workflow.
- **the tester is the executor** — it runs validation suites against whatever environment the CLI context points to, returning structured pass/fail results.
- **context switching** (`sukko context use <name>`) is the only difference between testing locally and testing remotely — the commands are identical.

Local environments have real limitations (single-node Kafka, no TLS, no cloud load balancer) that must be documented so operators know what they can and cannot validate locally vs. remotely.

**Acceptance Criteria**:
1. **Given** a machine with Docker and sukko-cli installed, **When** an operator follows the testing guide in sukko-docs, **Then** they can spin up a full local environment, run the complete locally-viable test battery, and get a clear pass/fail verdict for each area — with no cloud account, no codebase access.
2. **Given** a remote deployment configured as a context, **When** the operator follows the same guide's remote section, **Then** they can run the full test battery including suites only meaningful in production (TLS, multi-node, rate limits, push notifications) — using the same `sukko test` commands.
3. **Given** any of the 14 tester suites, **When** an operator looks it up in the testing guide, **Then** they find: what the suite validates in plain language, whether it runs locally or requires a remote deployment, the required edition, the exact `sukko test validate --suite <name>` command, and how to interpret the output.
4. **Given** a failing suite result, **When** the operator reads the output and consults the testing guide, **Then** they can identify the failure category and find the relevant `sukko` CLI commands to diagnose and remediate — without developer assistance for common failure modes.
5. **Given** the local vs. remote limitations section of the testing guide, **When** an operator reads it, **Then** they understand exactly which suites and features can be validated locally and which require a live deployment, with the reason stated plainly (e.g., "Push notifications require a configured push provider — not available in local Docker environments").

### Scenario 4 — Developer adds a new feature (Priority: P2)

A developer adds a new endpoint, changes a WebSocket message schema, or modifies system behavior. The constitution must force them to update the e2e test document (developer tool) and trigger a sukko-docs update (operator tool) as part of the same PR. The key distinction: the developer updates `ws/docs/e2e-testing.md` themselves; they either update sukko-docs themselves or open and link a cross-repo PR so the operator docs stay accurate.

**Acceptance Criteria**:
1. **Given** the updated constitution, **When** a code reviewer reviews a PR that adds or changes a testable behavior, **Then** a missing `ws/docs/e2e-testing.md` update is a blocking constitution violation.
2. **Given** the updated constitution, **When** a code reviewer reviews a PR that changes operator-visible behavior (env vars, CLI flags, API schemas, feature gates, user-facing error codes), **Then** a missing sukko-docs update (or linked cross-repo PR) is a blocking constitution violation.

### Edge Cases

- What if a tester suite requires Pro/Enterprise edition to run? Both documents must specify which edition is required per suite and what the expected behavior is on lower editions (e.g., suite is skipped or returns edition-gated failures).
- What if `sukko up` fails to start a service? Both the e2e doc (for developers) and the testing guide (for operators) must cover health verification before running suites — and provide actionable steps when a service is not ready.
- What if routing rules cannot be set because the noop Kafka admin rejects custom topics? Both documents must explain which topic suffixes are valid in the current Phase 1 state (`default`, `dead-letter` only) — in technical terms for developers, in plain language for operators.
- What if an operator has a Community license and tries to follow the SSE guide in sukko-docs? The sukko-docs guide must state the required edition at the top — the operator should know before reading setup instructions.
- What if sukko-docs has a page that references a removed CLI command? The developer accuracy audit (a step in `ws/docs/e2e-testing.md` covering sukko-docs validation) must catch this before it reaches operators.
- What if an operator switches context from local to remote and tests fail? The operator testing guide must explain how to distinguish environment connectivity issues from actual feature failures, and what CLI commands to use to diagnose (`sukko health`, `sukko status`, `sukko test smoke`).

---

## Requirements

### Functional Requirements

**E2E Testing Document (`ws/docs/e2e-testing.md`)**

- **FR-001**: The document MUST include a prerequisites section covering environment setup: `sukko init`, `sukko up`, admin auth (`sukko auth keygen`, `sukko auth register`), and health verification (`sukko health`, `sukko status`).
- **FR-002**: The document MUST cover the full tester suite battery in execution order: smoke → provisioning → auth → channels → pubsub → ordering → reconnect → ratelimit → tenant-isolation → token-revocation → sse → rest-publish → push → edition-limits → license-reload.
- **FR-003**: For each tester suite the document MUST show: the exact `sukko test validate --suite <name>` command, a one-paragraph description of what the suite validates, the prerequisite edition, and what a passing result looks like.
- **FR-004**: The document MUST include a manual verification section for every provisioning API operation: tenant lifecycle (create, get, list, update, suspend, reactivate, deprovision with grace period, force deprovision, rename), JWT keys, API keys, routing rules, channel rules, quotas, and audit log.
- **FR-005**: Each manual verification step MUST show the exact `sukko` CLI command, the expected output or response, and at least one edge case or failure mode for that operation.
- **FR-006**: The document MUST have a dedicated section for routing rules covering: pattern syntax (`**` at head/middle/tail, exact literals), priority evaluation order, fan-out to multiple topics, valid topic suffixes in the current Phase 1 state (`default` and `dead-letter` only), and the `TOO_MANY_ROUTING_RULES` / `ROUTING_RULE_VALIDATION_ERROR` / `TOPIC_NOT_PROVISIONED` error codes.
- **FR-007**: The document MUST include edition enforcement verification: which features require Pro/Enterprise, how to verify the gate blocks Community users (expected 403 + error code), and how to verify the gate passes with a valid license.
- **FR-008**: The document MUST include a WebSocket protocol section covering: connect with JWT, connect with API key, connect with both, subscribe/unsubscribe acknowledgment, message delivery, forced unsubscription (`forced: true` in `UnsubscriptionAck`), and connection limit enforcement (429).
- **FR-009**: The document MUST include a load/stress/soak section describing `sukko test load`, `sukko test stress`, and `sukko test soak` with the key flags and what metrics to watch.
- **FR-010**: The document MUST have a "test by feature area" index so a developer testing only one area can jump directly to the relevant section without reading the whole document.

**Operator Documentation (`../sukko-docs`) — written for operators, audited by developers**

sukko-docs is the operator-facing documentation site. Operators consume it directly; developers validate its accuracy before each release and update it when operator-visible behavior changes. Content must be written for someone who will never read the codebase.

- **FR-011**: The quickstart MUST use the current channel format (`{tenant}.{suffix}`) and routing rules format (`topics` array, not `topic_suffix`), with working CLI commands that an operator can copy-paste without modification.
- **FR-012**: The tenant onboarding guide MUST explain routing rules in operator-friendly terms: what patterns match, what topics array does (fan-out to multiple Kafka topics), how priority works, and — critically — that topic suffixes must exist before routing rules can reference them (Phase 1 constraint stated in plain language, without referencing Go internals).
- **FR-013**: The CLI reference page MUST document every command and subcommand in the current binary: `sukko up/down/init/status/health`, `sukko tenant`, `sukko keys`, `sukko api-keys`, `sukko auth`, `sukko rules routing/channels`, `sukko quota`, `sukko token`, `sukko connections`, `sukko publish`, `sukko subscribe`, `sukko license`, `sukko edition`, `sukko test`, `sukko grafana`, `sukko logs`. Written from the operator's perspective — what the command does, not how it's implemented.
- **FR-014**: The configuration reference MUST list every environment variable for ws-server, ws-gateway, and provisioning with the correct name, type, default value, and a plain-English description an operator can act on.
- **FR-015**: The testing guide (`guides/testing.mdx`) MUST be a comprehensive, standalone deployment validation reference for operators. It MUST NOT require the operator to consult any other source, read source code, or ask a developer. Every testing procedure available to developers MUST have an operator-accessible equivalent in this guide — the developer e2e doc and the operator testing guide cover the same system surface, in their respective audiences' language.

- **FR-015a**: The guide MUST open with the CLI-as-orchestrator / tester-as-executor model explained in plain language — what each tool does, why you need both, and how context switching makes local and remote testing identical from the command perspective.

- **FR-015b**: The guide MUST have an explicit local vs. remote limitations section. For each feature area, it MUST state clearly: (a) whether it can be validated locally, (b) what is not testable locally and why (e.g., push notifications need a real push provider, TLS termination requires a real load balancer, multi-node behaviors cannot be verified on a single Docker host), and (c) what the operator should validate manually in their remote deployment for those areas.

- **FR-015c**: The guide MUST document all 14 tester suites with: suite name, the exact `sukko test validate --suite <name>` command, what it validates in operator terms (not implementation terms), minimum required edition, whether it runs locally or remote-only, and a representative healthy output excerpt so the operator knows what pass looks like.

- **FR-015d**: The guide MUST include a sequential deployment validation checklist — a step-by-step procedure an operator follows after every deployment to confirm the system is healthy. It covers: environment health (`sukko health`, `sukko status`), smoke test (`sukko test smoke`), provisioning round-trip (`sukko test validate --suite provisioning`), WebSocket delivery (`sukko test validate --suite pubsub`), edition gate verification, and any edition-specific suites applicable to their license.

- **FR-015e**: The guide MUST include a troubleshooting section for common failure modes: suite fails to connect to tester, suite fails due to wrong edition, provisioning suite fails on routing rules (topic not provisioned), auth suite fails (key not registered), and how to reset state (`sukko tenant deprovision`, `sukko rules routing delete`, re-run setup).

- **FR-015f**: Context switching MUST be documented as a first-class workflow — not a footnote. The guide MUST show: how to create a context for a remote deployment, how to switch between local and remote, and how to verify which context is active before running tests.
- **FR-016**: Every sukko-docs page covering an edition-gated feature (SSE, push notifications, REST publish, analytics) MUST state the required edition (e.g., "Requires Pro") as the first visible element — before prerequisites, before setup steps.
- **FR-017**: The editions comparison page MUST accurately reflect which features are available today vs. planned — operators making purchase decisions depend on this being correct.

**Constitution Amendment**

- **FR-018**: The constitution MUST include a principle (or extend an existing one) requiring that `ws/docs/e2e-testing.md` is updated in the same PR as any code change that adds, removes, or modifies a testable behavior, feature, or edge case.
- **FR-019**: The constitution MUST explicitly require that `../sukko-docs` operator pages are updated (or a cross-repo PR is opened and linked) whenever env vars, CLI behavior, API schemas, feature gates, or user-visible behavior changes — this strengthens the existing XVI requirement with explicit doc-type coverage.
- **FR-020**: Code reviews (via `/code-review` and `/pr-review` skills) MUST surface missing e2e doc updates as a constitution violation when the diff introduces or modifies a feature, behavior, or error code.

### Non-Functional Requirements

- **NFR-001**: The e2e document MUST be runnable by a developer with no prior Sukko knowledge — every command must be complete and copy-pasteable.
- **NFR-002**: The e2e document MUST be structured so CI can eventually automate it — section headings and command formats MUST follow a consistent pattern.
- **NFR-003**: sukko-docs updates MUST be in the `../sukko-docs` repo (operator docs, separate repo) — never in the sukko repo. They MUST be linked from the sukko PR per XVI. The language in sukko-docs MUST be written for operators, not developers — no references to Go packages, internal struct names, or codebase conventions.
- **NFR-004**: The constitution amendment version bump MUST follow the governance rule: adding a new principle = MINOR bump.

### Docs Requirements

- `ws/docs/e2e-testing.md` — developer tool, lives in the sukko repo, written for developers.
- `../sukko-docs` updates — operator tool, lives in the sukko-docs repo, written for operators. Delivered as a companion PR to sukko-docs, linked from this sukko PR. Must never contain codebase-internal language.
- `CLAUDE.md` constitution amendment — delivered inline in the sukko repo.

### CLI Requirements

- No CLI changes are required by this spec. The CLI commands documented are existing commands.

### API Contract Requirements

- No API contract changes required. This spec produces documentation, not code or schema changes.

---

## Success Criteria

- **SC-001**: A developer unfamiliar with the codebase can follow `ws/docs/e2e-testing.md` from prerequisites to full suite completion and reach a passing result on a healthy local environment without consulting any other source.
- **SC-002**: Every one of the 14 tester suites (`smoke`, `provisioning`, `auth`, `channels`, `pubsub`, `ordering`, `reconnect`, `ratelimit`, `tenant-isolation`, `token-revocation`, `sse`, `rest-publish`, `push`, `edition-limits`, `license-reload`) is covered in the e2e document.
- **SC-003**: Running `sukko --help` against the current binary and comparing to the CLI reference in sukko-docs produces zero discrepancies.
- **SC-004**: Every environment variable in `ws/internal/shared/platform/` with an `envDefault` tag is present in the sukko-docs configuration reference with the correct default value.
- **SC-005**: The routing rules documentation in both the e2e doc and sukko-docs uses `topics` (array), not `topic_suffix`, and correctly describes fan-out and Phase 1 constraints.
- **SC-006**: The constitution contains an explicit maintenance requirement for `ws/docs/e2e-testing.md` and `/sukko-docs` that would be surfaced as a violation in code review when docs are not updated alongside code changes.
- **SC-007**: An operator following the quickstart in sukko-docs reaches a working WebSocket connection with a real message delivered — no broken commands, no stale examples, no missing steps. The operator never needs to look at source code or ask a developer to interpret the instructions.
- **SC-008**: An operator with no prior Sukko knowledge can follow the testing guide in sukko-docs and successfully: (a) run the full locally-viable test battery on a Docker environment, (b) interpret every result as pass/fail with a clear reason, and (c) identify remediation steps for any failure — entirely from the guide, no cloud account, no codebase access, no developer assistance.
- **SC-009**: An operator can switch from local to remote testing by running `sukko context use <name>` and re-running the identical `sukko test` commands — the testing guide makes this workflow explicit, and the operator does not need to change any flags or consult any other documentation.
- **SC-010**: The testing guide covers all 14 validation suites. For each suite, an operator can determine in under 30 seconds: what it tests, whether it applies to their edition, whether it runs locally, and what command to run.
- **SC-011**: No testing procedure documented in `ws/docs/e2e-testing.md` is absent from `guides/testing.mdx` without a documented reason (e.g., "developer-internal only" with justification). The two documents cover the same system surface for their respective audiences.

---

## Out of Scope

- Automated e2e CI pipeline (running the document in CI is a future initiative).
- Video tutorials or interactive guides.
- SDK documentation (sukko-js, sukko-python, etc.).
- Performance benchmarks or SLA targets.
- Monitoring/alerting runbooks.
- Disaster recovery playbooks.
- Changes to the tester service itself (the document uses the tester as-is).
- New CLI commands or provisioning API endpoints.
