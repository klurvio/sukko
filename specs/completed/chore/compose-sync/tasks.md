# Tasks: Docker Compose Cross-Repo Sync

**Branch**: chore/compose-sync | **Date**: 2026-03-28

## Phase 1: Transform Tool

- [x] T001 Create `scripts/compose-transform/main.go` with standalone `go.mod` (module `github.com/klurvio/sukko/scripts/compose-transform`, depends only on `gopkg.in/yaml.v3`). Standalone Go CLI tool (`//nolint:forbidigo`). Reads docker-compose YAML from stdin, parses with `gopkg.in/yaml.v3` using `yaml.Node` (preserves comments, ordering, formatting). Walks the `services` map node. For each service with a `build` key: removes the `build` key-value node pair, inserts an `image` key-value node using the hardcoded service→image mapping (`provisioning` → `ghcr.io/klurvio/sukko-provisioning:latest`, `ws-server` → `ghcr.io/klurvio/sukko-server:latest`, `ws-gateway` → `ghcr.io/klurvio/sukko-gateway:latest`, `sukko-tester` → `ghcr.io/klurvio/sukko-tester:latest`). Services without `build` pass through unchanged. Unknown services with `build` get it removed with a warning to stderr. Validates input is valid YAML. Writes result to stdout.

- [x] T002 Create `scripts/compose-transform/main_test.go` — table-driven tests: service with `build:` replaced by correct `image:`, service without `build:` unchanged, unknown service with `build:` gets it removed + warning, YAML comments preserved, service ordering preserved, `configs:` block unchanged, `profiles:` unchanged, idempotency (transform already-transformed output → identical result), empty input → error, invalid YAML → error.

## Phase 2: CI Workflow

- [x] T003 Create `.github/workflows/sync-compose.yml` — triggers on push to `main` when `docker-compose.yml` modified. Steps: (1) checkout sukko, (2) setup Go from `scripts/compose-transform/go.mod`, (3) run `cd scripts/compose-transform && go run . < ../../docker-compose.yml > /tmp/docker-compose.yml`, (4) validate with `docker compose -f /tmp/docker-compose.yml config --quiet`, (5) get GitHub App installation token via `actions/create-github-app-token` using `vars.COMPOSE_SYNC_APP_ID` + `secrets.COMPOSE_SYNC_APP_PRIVATE_KEY` scoped to `sukko-cli`, (6) checkout `klurvio/sukko-cli` with app token, (7) diff compare — if identical exit 0, (8) create/update branch `chore/sync-compose`, commit, force-push, (9) create new PR or update existing PR body with source commit SHA. Git author: `sukko-compose-sync[bot]`. Add `--label "automated,sync"` on create. Labels persist on existing PRs across force-push + body edit, so no label action needed on the update path. Pin all actions to SHA hashes matching existing workflow patterns.

## Phase 3: Verify

- [x] T004 Run `cd scripts/compose-transform && go test -v ./...` — transform tests pass.

- [x] T005 Run `go run ./scripts/compose-transform < docker-compose.yml | docker compose -f - config --quiet` — transformed output is valid docker-compose.

- [x] T006 Verify transform output: `go run ./scripts/compose-transform < docker-compose.yml | grep -c 'build:'` returns 0, `go run ./scripts/compose-transform < docker-compose.yml | grep 'ghcr.io'` shows all 4 image references.

- [ ] T007 (manual) Create GitHub App `sukko-compose-sync`, install on `klurvio/sukko-cli` with Contents (R/W) + Pull Requests (R/W). Generate private key. Store as `COMPOSE_SYNC_APP_PRIVATE_KEY` secret and `COMPOSE_SYNC_APP_ID` variable in sukko repo.

- [ ] T008 (manual) Push a trivial docker-compose.yml change to main. Verify sync PR appears in sukko-cli within 2 minutes.

## Dependencies

```
T001 → T002 (tests need the tool)
T001 → T003 (workflow calls the tool)
T001, T002 → T004, T005, T006
T003 + T007 → T008 (end-to-end test needs App + workflow)
```
