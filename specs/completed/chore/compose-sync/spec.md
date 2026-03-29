# Feature Specification: Docker Compose Cross-Repo Sync

**Branch**: `chore/compose-sync`
**Created**: 2026-03-28
**Status**: Draft

## Context

Sukko has two `docker-compose.yml` files across two repos:

- **`sukko/docker-compose.yml`** — source of truth. Builds from source (`build: { context: ./ws, dockerfile: ... }`). Used by developers working on the sukko platform.
- **`sukko-cli/compose/docker-compose.yml`** — embedded in the CLI binary via `//go:embed`. Uses pre-built images (`image: ghcr.io/klurvio/...`). Written to `.sukko/docker-compose.yml` by `sukko up` for end users.

These files share service definitions, ports, environment variables, healthchecks, profiles, and inline configs — but differ in image source (`build:` vs `image:`). When the sukko repo changes its docker-compose.yml (new services, new env vars, new profiles like observability), the CLI's embedded copy must be updated to match.

Today this sync is manual. Every docker-compose.yml change is a drift risk — if the CLI's copy falls behind, `sukko up` creates a broken or outdated environment for end users.

### Transform Required

The transformation from sukko → sukko-cli is deterministic:

1. Replace each service's `build:` block with an `image:` reference
2. Remove `build`-only fields (`args: COMMIT_HASH`)
3. Everything else (ports, environment, healthchecks, depends_on, profiles, configs, volumes) stays identical

Service → image mapping:
- `provisioning` → `ghcr.io/klurvio/sukko-provisioning:latest`
- `ws-server` → `ghcr.io/klurvio/sukko-server:latest`
- `ws-gateway` → `ghcr.io/klurvio/sukko-gateway:latest`
- `sukko-tester` → `ghcr.io/klurvio/sukko-tester:latest`

## User Scenarios

### Scenario 1 — Automatic Sync on Merge (Priority: P1)

A developer merges a PR that changes `docker-compose.yml` in the sukko repo. The sync should happen automatically.

**Acceptance Criteria**:
1. **Given** a PR is merged to `main` that modifies `docker-compose.yml`, **When** CI runs, **Then** a PR is automatically created in `sukko-cli` with the transformed docker-compose.yml
2. **Given** the auto-PR is created, **When** the maintainer reviews it, **Then** the diff shows only image-source changes (build → image) with no other modifications
3. **Given** no changes to `docker-compose.yml`, **When** any other PR is merged, **Then** no sync PR is created

### Scenario 2 — Transform Correctness (Priority: P1)

The transformation must be correct and verifiable.

**Acceptance Criteria**:
1. **Given** the sukko docker-compose.yml, **When** the transform runs, **Then** every `build:` block is replaced with the correct `image:` reference
2. **Given** the transform output, **When** compared to the existing sukko-cli compose file, **Then** only intentional changes appear (no whitespace drift, no reordering)
3. **Given** a service without a `build:` block (e.g., `nats`, `redis`, observability services), **When** the transform runs, **Then** the service is passed through unchanged

### Scenario 3 — Security (Priority: P1)

Cross-repo access must be minimal and auditable.

**Acceptance Criteria**:
1. **Given** the CI workflow, **When** it accesses sukko-cli, **Then** it uses a GitHub App installation token with the minimum required permissions
2. **Given** the GitHub App, **When** its permissions are reviewed, **Then** it has only `contents: write` and `pull-requests: write` on `sukko-cli` — no other repos
3. **Given** a compromised CI run, **When** the token is examined, **Then** it is short-lived (1-hour GitHub App installation token, not a long-lived PAT)

### Edge Cases

- What if the sukko-cli repo already has an open sync PR? The workflow MUST update the existing PR branch instead of creating a duplicate.
- What if the transform produces an identical file to what's in sukko-cli? No PR should be created (no-op).
- What if the docker-compose.yml in sukko has a syntax error? The transform MUST validate YAML before pushing.

## Requirements

### Functional Requirements

#### Transform Tool

- **FR-001**: A Go tool MUST be created at `scripts/compose-transform/main.go` that reads a docker-compose YAML from stdin, replaces `build:` blocks with `image:` references using a hardcoded service→image mapping, and writes the result to stdout.
- **FR-002**: The transform MUST preserve all non-build fields: `ports`, `environment`, `depends_on`, `healthcheck`, `profiles`, `configs`, `command`, `volumes`, `image` (for services that already use images).
- **FR-003**: The transform MUST preserve YAML comments, ordering, and formatting as closely as possible. Use a YAML library that supports round-trip parsing (e.g., `gopkg.in/yaml.v3` with node-level manipulation).
- **FR-004**: The transform MUST validate the input is valid docker-compose YAML before processing. Invalid input MUST cause a non-zero exit code with a clear error message.
- **FR-005**: The service→image mapping MUST be defined as a Go map, not external config. Changes to the mapping require a code change (intentional — new services are rare).
- **FR-006**: The transform MUST be idempotent — running it twice on the same input produces identical output.

#### GitHub App

- **FR-007**: A GitHub App MUST be created (or an existing one reused) with the minimum permissions required:
  - `contents: write` on `klurvio/sukko-cli` (to push branches)
  - `pull-requests: write` on `klurvio/sukko-cli` (to create/update PRs)
  - No permissions on any other repository
- **FR-008**: The GitHub App's private key MUST be stored as a repository secret in `klurvio/sukko` (e.g., `COMPOSE_SYNC_APP_PRIVATE_KEY`).
- **FR-009**: The GitHub App's ID MUST be stored as a repository variable (not secret) in `klurvio/sukko` (e.g., `COMPOSE_SYNC_APP_ID`).

#### CI Workflow

- **FR-010**: A GitHub Actions workflow MUST be created at `.github/workflows/sync-compose.yml` that triggers on push to `main` when `docker-compose.yml` is modified.
- **FR-011**: The workflow MUST:
  1. Run the transform tool on `docker-compose.yml`
  2. Authenticate as the GitHub App to get an installation token
  3. Clone `sukko-cli` using the installation token
  4. Compare the transformed file to `sukko-cli/compose/docker-compose.yml`
  5. If different: create/update a branch `chore/sync-compose`, commit the transformed file, create/update a PR
  6. If identical: do nothing (no-op)
- **FR-012**: The PR title MUST be `chore: sync docker-compose.yml from sukko` with a body that includes the triggering commit SHA and a summary of what changed.
- **FR-013**: If a sync PR already exists (branch `chore/sync-compose` with an open PR), the workflow MUST force-push to the existing branch and update the PR body — not create a new PR.
- **FR-014**: The workflow MUST create/update PRs via `gh pr create` / `gh pr edit` (GitHub CLI) authenticated with the GitHub App installation token.

#### Validation

- **FR-015**: The transform tool MUST have unit tests that verify:
  - `build:` blocks are replaced with correct `image:` references
  - Non-build services pass through unchanged
  - YAML structure is preserved (ports, env vars, healthchecks)
  - Unknown services (not in the mapping) pass through with `build:` removed and a warning logged
- **FR-016**: The CI workflow MUST run `docker compose config -f <transformed-file>` to validate the output is valid docker-compose before pushing.

### Non-Functional Requirements

- **NFR-001**: The GitHub App installation token MUST be short-lived (GitHub's default: 1 hour). No long-lived PATs.
- **NFR-002**: The workflow MUST complete within 2 minutes (clone, transform, compare, push).
- **NFR-003**: The sync PR MUST be clearly labeled as automated (e.g., `automated`, `sync` labels) so reviewers know it's machine-generated.
- **NFR-004**: The transform tool MUST NOT depend on network access — it's a pure YAML transformation.

### Key Entities

- **Transform Tool**: A Go CLI that reads docker-compose YAML, replaces `build:` → `image:`, writes the result. Pure function, no side effects.
- **Service→Image Mapping**: A Go map linking compose service names to GHCR image references. Source of truth for the transformation.
- **GitHub App**: A GitHub App with minimal cross-repo permissions, used to authenticate CI workflows for cross-repo operations.
- **Sync PR**: An automatically created/updated PR in sukko-cli that carries the transformed docker-compose.yml.

## Success Criteria

- **SC-001**: Merging a PR that changes `docker-compose.yml` in sukko automatically creates a sync PR in sukko-cli within 2 minutes.
- **SC-002**: The sync PR's transformed file passes `docker compose config` validation.
- **SC-003**: Running `sukko up` with the synced compose file produces identical service behavior to `docker compose up` in the sukko repo (same ports, same env vars, same healthchecks).
- **SC-004**: The GitHub App has no permissions beyond `contents: write` + `pull-requests: write` on `klurvio/sukko-cli`.
- **SC-005**: Merging a PR that does NOT change `docker-compose.yml` triggers no sync workflow.

## Clarifications

- Q: Should docker-compose.yml live in sukko (private) or sukko-cli (public) as source of truth? → A: sukko (private). The compose file is coupled to the Go codebase — env var names, ports, healthcheck paths, and service topology must match the Go config contract. Keeping it alongside the code ensures changes to the config contract and compose file happen in the same PR. The sync CI pushes a transformed copy (build → image) to sukko-cli automatically.
- Q: Is the sync CI cost-free? → A: Yes. GitHub App is free. CI triggers only when docker-compose.yml changes on main (~1-2 runs/month, <1 minute each). Negligible against GitHub Actions free tier.

## Out of Scope

- Syncing Helm charts or other deployment files between repos
- Bi-directional sync (sukko-cli → sukko)
- Automatic merging of sync PRs (human review required)
- Version-pinned image tags (uses `:latest` — image tag management is a separate concern)
- Sync for non-docker-compose files (Taskfile, config, etc.)
