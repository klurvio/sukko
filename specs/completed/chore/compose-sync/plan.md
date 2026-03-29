# Implementation Plan: Docker Compose Cross-Repo Sync

**Branch**: `chore/compose-sync` | **Date**: 2026-03-28 | **Spec**: `specs/backlog/chore/compose-sync/spec.md`

## Summary

Automate the sync of `docker-compose.yml` from sukko (private, source of truth) to sukko-cli (public, embedded in CLI). A Go transform tool replaces `build:` blocks with `image:` references. A GitHub Actions workflow triggers on merge to main, authenticates via GitHub App, and creates/updates a PR in sukko-cli.

## Technical Context

**Repos**: `klurvio/sukko` (private), `klurvio/sukko-cli` (public)
**CI**: GitHub Actions (existing: ci.yml, deploy-demo.yml — action pins use SHA hashes)
**Target file**: `sukko-cli/compose/docker-compose.yml` (embedded via `//go:embed`)
**Image registry**: ghcr.io/klurvio/ (sukko-server, sukko-gateway, sukko-provisioning, sukko-tester)

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I-XII | N/A | This is CI/tooling — no Go service code, no runtime behavior. Constitution governs application code, not build scripts. |

No violations.

## Design

### Component 1: Transform Tool

**File**: `scripts/compose-transform/main.go`
**Module**: Standalone `go.mod` in `scripts/compose-transform/` — only depends on `gopkg.in/yaml.v3`. No sukko dependencies. Separate from the `ws/` module.

A standalone Go CLI that:
1. Reads docker-compose YAML from stdin
2. Parses with `gopkg.in/yaml.v3` using `yaml.Node` (preserves comments, ordering, formatting)
3. Walks the `services` map
4. For each service with a `build` key: removes `build` node, inserts `image` node from mapping
5. Services without `build` (nats, redpanda, postgres, observability) pass through unchanged
6. Writes result to stdout

**Service → Image mapping** (hardcoded in Go):
```go
var serviceImageMap = map[string]string{
    "provisioning": "ghcr.io/klurvio/sukko-provisioning:latest",
    "ws-server":    "ghcr.io/klurvio/sukko-server:latest",
    "ws-gateway":   "ghcr.io/klurvio/sukko-gateway:latest",
    "sukko-tester": "ghcr.io/klurvio/sukko-tester:latest",
}
```

**Why `yaml.Node`?** Standard `yaml.Unmarshal` → `yaml.Marshal` destroys comments and reorders keys. `yaml.Node` is a tree representation that preserves the original structure. We manipulate nodes directly.

**Transform algorithm:**
```
for each service node in services:
    find "build" key node
    if found:
        look up service name in serviceImageMap
        if found:
            remove the build key-value pair (key node + value node)
            insert "image: <ghcr-ref>" at position 0 (after service name)
        else:
            remove build key-value, log warning (unknown service)
```

**`//nolint:forbidigo`** — standalone CLI tool, uses `fmt` for output (same pattern as genkeys/gentoken).

### Component 2: GitHub App

**Setup** (one-time, manual):
1. Create a GitHub App at https://github.com/settings/apps/new
   - Name: `sukko-compose-sync`
   - Homepage: `https://github.com/klurvio/sukko`
   - Permissions:
     - Repository permissions → Contents: Read & Write
     - Repository permissions → Pull requests: Read & Write
   - Install on: `klurvio/sukko-cli` only
2. Generate a private key (PEM file)
3. Store in sukko repo:
   - Secret: `COMPOSE_SYNC_APP_PRIVATE_KEY` (PEM contents)
   - Variable: `COMPOSE_SYNC_APP_ID` (numeric App ID)

### Component 3: CI Workflow

**File**: `.github/workflows/sync-compose.yml`

```yaml
name: Sync Compose

on:
  push:
    branches: [main]
    paths: [docker-compose.yml]

permissions:
  contents: read

jobs:
  sync:
    name: Sync to sukko-cli
    runs-on: ubuntu-latest
    steps:
      # 1. Checkout sukko
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6

      # 2. Setup Go (for transform tool)
      - uses: actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5 # v5
        with:
          go-version-file: scripts/compose-transform/go.mod

      # 3. Run transform
      - name: Transform docker-compose.yml
        run: |
          cd scripts/compose-transform && go run . < ../../docker-compose.yml > /tmp/docker-compose.yml

      # 4. Validate output
      - name: Validate transformed compose
        run: |
          docker compose -f /tmp/docker-compose.yml config --quiet

      # 5. Get GitHub App token
      - name: Get installation token
        id: app-token
        uses: actions/create-github-app-token@21cfef2c9c2a44d64dae3e281a7e3fca8c0c75cf # v2
        with:
          app-id: ${{ vars.COMPOSE_SYNC_APP_ID }}
          private-key: ${{ secrets.COMPOSE_SYNC_APP_PRIVATE_KEY }}
          repositories: sukko-cli

      # 6. Checkout sukko-cli
      - name: Checkout sukko-cli
        uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6
        with:
          repository: klurvio/sukko-cli
          token: ${{ steps.app-token.outputs.token }}
          path: sukko-cli

      # 7. Compare and create PR if different
      - name: Sync compose file
        env:
          GH_TOKEN: ${{ steps.app-token.outputs.token }}
        run: |
          cd sukko-cli

          # Compare
          if diff -q compose/docker-compose.yml /tmp/docker-compose.yml > /dev/null 2>&1; then
            echo "No changes — compose files are identical"
            exit 0
          fi

          # Configure git
          git config user.name "sukko-compose-sync[bot]"
          git config user.email "compose-sync@sukko.dev"

          # Create or update branch
          BRANCH="chore/sync-compose"
          git checkout -B "$BRANCH"

          # Copy transformed file
          cp /tmp/docker-compose.yml compose/docker-compose.yml

          # Commit
          git add compose/docker-compose.yml
          git commit -m "chore: sync docker-compose.yml from sukko

          Source commit: ${{ github.sha }}
          Triggered by: ${{ github.event.head_commit.message }}"

          # Push (force to update existing branch)
          git push --force origin "$BRANCH"

          # Create or update PR
          EXISTING_PR=$(gh pr list --repo klurvio/sukko-cli --head "$BRANCH" --json number --jq '.[0].number' 2>/dev/null || echo "")

          if [ -n "$EXISTING_PR" ]; then
            gh pr edit "$EXISTING_PR" --repo klurvio/sukko-cli \
              --body "Automated sync of docker-compose.yml from sukko.

          Source commit: [${{ github.sha }}](https://github.com/klurvio/sukko/commit/${{ github.sha }})
          Trigger: \`${{ github.event.head_commit.message }}\`

          This PR was automatically created by the [compose-sync workflow](https://github.com/klurvio/sukko/actions/workflows/sync-compose.yml)."
            echo "Updated existing PR #$EXISTING_PR"
          else
            gh pr create --repo klurvio/sukko-cli \
              --base main \
              --head "$BRANCH" \
              --title "chore: sync docker-compose.yml from sukko" \
              --body "Automated sync of docker-compose.yml from sukko.

          Source commit: [${{ github.sha }}](https://github.com/klurvio/sukko/commit/${{ github.sha }})
          Trigger: \`${{ github.event.head_commit.message }}\`

          This PR was automatically created by the [compose-sync workflow](https://github.com/klurvio/sukko/actions/workflows/sync-compose.yml)." \
              --label "automated,sync"
            echo "Created new sync PR"
          fi
```

**Key design decisions:**
- `actions/create-github-app-token` handles the JWT → installation token exchange (no manual crypto)
- `--force` push to the sync branch ensures only one branch exists (updates, not duplicates)
- `diff -q` comparison prevents no-op PRs
- `docker compose config --quiet` validates the transform output before pushing
- Action SHAs pinned (matches existing workflow patterns in this repo)

### Component 4: Transform Tests

**File**: `scripts/compose-transform/main_test.go`

Test cases:
- Service with `build:` block → replaced with correct `image:` reference
- Service without `build:` (e.g., `nats`) → passes through unchanged
- Service with `build:` but not in mapping → `build:` removed, warning logged
- YAML comments preserved
- Service ordering preserved
- `configs:` block passes through unchanged
- `profiles:` passes through unchanged
- Empty input → error
- Invalid YAML → error

## Files to Create

| File | Purpose |
|------|---------|
| `scripts/compose-transform/main.go` | Transform tool (YAML node manipulation) |
| `scripts/compose-transform/main_test.go` | Transform tests |
| `.github/workflows/sync-compose.yml` | CI workflow |

## Files to Modify

| File | Change |
|------|--------|
| `docker-compose.yml` | Header comment already added (explains source-of-truth rationale) |

## Manual Setup (one-time)

1. Create GitHub App `sukko-compose-sync` at https://github.com/settings/apps/new
2. Set permissions: Contents (R/W) + Pull Requests (R/W)
3. Install on `klurvio/sukko-cli`
4. Generate private key
5. Add to sukko repo: secret `COMPOSE_SYNC_APP_PRIVATE_KEY`, variable `COMPOSE_SYNC_APP_ID`

## Verification

```bash
# Transform tool
cd scripts/compose-transform
go test -v ./...

# Manual transform test
go run . < ../../docker-compose.yml > /tmp/out.yml
docker compose -f /tmp/out.yml config --quiet
grep -c 'build:' /tmp/out.yml  # should be 0 (all replaced)
grep 'ghcr.io' /tmp/out.yml    # should have 4 image refs

# Idempotency test
go run . < /tmp/out.yml > /tmp/out2.yml
diff /tmp/out.yml /tmp/out2.yml  # should be identical

# Workflow test (after GitHub App setup)
# Push a change to docker-compose.yml on main
# Verify PR appears in sukko-cli within 2 minutes
```
