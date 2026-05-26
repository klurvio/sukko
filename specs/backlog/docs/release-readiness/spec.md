# Feature Specification: Release Readiness Documentation

**Branch**: `docs/release-readiness`
**Created**: 2026-05-26
**Status**: Draft
**Passes**: clarify: 0 | analyze: 0

## Context

Sukko is approaching its first production release. There are two blocking documentation gaps:

1. **No developer e2e testing guide exists.** Developers validating a release or a feature branch have no single document telling them what to run, in what order, and what a passing system looks like. The tester service and sukko-cli together cover the full feature surface but there is no document tying them together into a cohesive test battery. This means release validation is informal, inconsistent, and relies on tribal knowledge.

2. **Operator documentation (`sukko-docs`) has known accuracy issues.** The quickstart references stale channel formats, some guides have not been updated to reflect routing rules replacing the old topic-suffix model, and CLI command references may lag implementation. An operator following these docs today will hit broken examples before they finish setup. Poor DX at first contact is a hard barrier to adoption.

3. **Neither document is required to stay in sync with the codebase.** The constitution covers OpenAPI/AsyncAPI contract maintenance (XVII) and cross-repo awareness (XVI) but neither explicitly mandates that the e2e test guide or the operator docs be updated when the codebase changes. Without a constitution mandate, both documents will drift again the moment the next feature lands.

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

### Scenario 2 — Operator follows documentation to deploy and validate (Priority: P1)

An operator follows `sukko-docs` to deploy Sukko, onboard a tenant, and verify their deployment is working correctly. They have no prior knowledge of the codebase.

**Acceptance Criteria**:
1. **Given** the quickstart guide, **When** an operator follows it step by step, **Then** they reach a working WebSocket connection with no broken commands or stale examples.
2. **Given** any guide page (tenant onboarding, SSE, push notifications, testing), **When** the operator runs the CLI commands shown, **Then** the commands succeed and produce output matching the documented examples.
3. **Given** the routing rules documentation, **When** an operator reads it, **Then** it correctly describes the `topics` array (not `topic_suffix`), the fan-out behavior, the `**` pattern semantics, and the priority evaluation order.
4. **Given** the CLI reference, **When** an operator runs `sukko --help` and compares it to the docs, **Then** every command, subcommand, and flag in the docs exists in the binary and vice versa.
5. **Given** the configuration reference, **When** an operator searches for an environment variable, **Then** the docs show the correct name, type, default, and description matching the Go `envDefault` value.

### Scenario 3 — Developer adds a new feature (Priority: P2)

A developer adds a new endpoint, changes a WebSocket message schema, or modifies system behavior. The constitution must force them to update both the e2e test document and the operator docs as part of the same PR.

**Acceptance Criteria**:
1. **Given** the updated constitution, **When** a code reviewer reviews a PR that adds or changes a feature, **Then** a missing e2e doc update is a blocking constitution violation, not an optional follow-up.
2. **Given** the updated constitution, **When** a code reviewer reviews a PR that changes operator-visible behavior (env vars, CLI flags, API schemas, feature gates), **Then** a missing sukko-docs update is a blocking constitution violation.

### Edge Cases

- What if a tester suite requires Pro/Enterprise edition to run? The document must specify which edition is required per suite and what the expected behavior is on lower editions.
- What if `sukko up` fails to start a service? The document must cover service health verification before running tests.
- What if routing rules cannot be set because the noop Kafka admin rejects custom topics? The document must explain which topic suffixes are valid in the current Phase 1 state (`default`, `dead-letter` only).
- What if an operator has a Community license and tries to follow the SSE guide? The guide must state the edition requirement upfront.
- What if sukko-docs has a page that references a removed CLI command? The accuracy audit must catch this.

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

**Operator Documentation (`../sukko-docs`)**

- **FR-011**: The quickstart MUST use the current channel format (`{tenant}.{suffix}`) and routing rules format (`topics` array, not `topic_suffix`), with working CLI commands verified against the current binary.
- **FR-012**: The tenant onboarding guide MUST accurately document routing rules: pattern syntax, topics array, fan-out semantics, priority ordering, and the Phase 1 constraint that topics must be pre-provisioned.
- **FR-013**: The CLI reference page MUST document every command and subcommand present in the current binary: `sukko up/down/init/status/health`, `sukko tenant`, `sukko keys`, `sukko api-keys`, `sukko auth`, `sukko rules routing/channels`, `sukko quota`, `sukko token`, `sukko connections`, `sukko publish`, `sukko subscribe`, `sukko license`, `sukko edition`, `sukko test`, `sukko grafana`, `sukko logs`.
- **FR-014**: The configuration reference MUST list every environment variable for ws-server, ws-gateway, and provisioning with the correct name, type, default (from `envDefault`), and description.
- **FR-015**: The testing guide (`guides/testing.mdx`) MUST document all 14 tester suites with their names, descriptions, required edition, and example output.
- **FR-016**: Every guide that documents an edition-gated feature (SSE, push, REST publish, analytics) MUST state the required edition at the top of the page.
- **FR-017**: The editions comparison page MUST accurately reflect the feature matrix from `features.go` — implemented features must be marked as available, future features as planned.

**Constitution Amendment**

- **FR-018**: The constitution MUST include a principle (or extend an existing one) requiring that `ws/docs/e2e-testing.md` is updated in the same PR as any code change that adds, removes, or modifies a testable behavior, feature, or edge case.
- **FR-019**: The constitution MUST explicitly require that `../sukko-docs` operator pages are updated (or a cross-repo PR is opened and linked) whenever env vars, CLI behavior, API schemas, feature gates, or user-visible behavior changes — this strengthens the existing XVI requirement with explicit doc-type coverage.
- **FR-020**: Code reviews (via `/code-review` and `/pr-review` skills) MUST surface missing e2e doc updates as a constitution violation when the diff introduces or modifies a feature, behavior, or error code.

### Non-Functional Requirements

- **NFR-001**: The e2e document MUST be runnable by a developer with no prior Sukko knowledge — every command must be complete and copy-pasteable.
- **NFR-002**: The e2e document MUST be structured so CI can eventually automate it — section headings and command formats MUST follow a consistent pattern.
- **NFR-003**: The sukko-docs updates MUST be in a separate commit to `../sukko-docs` (different repo), but MUST be linked from the sukko PR via the cross-repo reference requirement in XVI.
- **NFR-004**: The constitution amendment version bump MUST follow the governance rule: adding a new principle = MINOR bump.

### Docs Requirements

- The e2e testing document is itself the primary deliverable — it lives at `ws/docs/e2e-testing.md`.
- `../sukko-docs` updates are in scope for this spec and must be delivered as a companion PR to the sukko-docs repo.
- The constitution amendment is in scope and is delivered by editing `CLAUDE.md`.

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
- **SC-007**: An operator following the quickstart in sukko-docs reaches a working WebSocket connection with a real message delivered — no broken commands, no stale examples, no missing steps.

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
