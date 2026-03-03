# Tasks: Consistent Single-Knob Infrastructure Configuration

**Branch**: `fix/odin-deploy-defaults` | **Generated**: 2026-03-03
**Plan**: `specs/fix/odin-deploy-defaults/plan.md`

---

## Phase 1: Helm Values Renaming (Config)

> Rename inconsistent values and add explicit `databaseDriver` knob across all three values files.
> **Checkpoint**: `helm lint deployments/helm/odin` passes with no errors.

- [x] T001 [P] Rename `broadcast.type` → `broadcastType` in `deployments/helm/odin/charts/ws-server/values.yaml` (FR-002). Change nested `broadcast: type: nats` to top-level `broadcastType: nats`. Update surrounding comment.

- [x] T002 [P] Add explicit `databaseDriver: sqlite` and remove dead `global.postgresql.enabled` in `deployments/helm/odin/charts/provisioning/values.yaml` (FR-003). Add `databaseDriver: sqlite` above the `database:` block. Remove `postgresql: enabled: false` from the `global:` section. Update `externalDatabase` comment to reference `databaseDriver: postgres` instead of auto-derivation.

- [x] T003 Update parent chart values in `deployments/helm/odin/values.yaml` (FR-001, FR-002, FR-003, FR-006a). Three changes: (a) Remove `global.postgresql.enabled` block. (b) Rename `ws-server.broadcast.type` to `ws-server.broadcastType`. (c) Add `provisioning.databaseDriver: sqlite`. (d) Add deployment mode recipe documentation as header comments per plan Phase 1.3.
  > Depends on: T001, T002 (must use same naming)

---

## Phase 2: Template Updates

> Update all templates to consume renamed values and fix init container bugs.
> **Checkpoint**: `helm template odin deployments/helm/odin` renders without errors.

- [x] T004 [P] Update ws-server deployment template `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` (FR-002, Constitution I). Six changes per plan Phase 2.1: (a) Simplify `$broadcastType` init from 4 lines to 1 (`{{ .Values.broadcastType | default "nats" }}`). (b) Update MESSAGE_BACKEND env var injection to only inject when non-default (`ne "direct"`). (c) Update BROADCAST_TYPE env var injection to use `.Values.broadcastType`, only inject when non-default (`ne "nats"`). (d) Fix `wait-for-redpanda` init container to skip when `kafka.brokers` is set (`{{- if and $needsKafka (not .Values.kafka.brokers) }}`). (e) Fix `wait-for-nats-jetstream` init container to skip when `jetstream.urls` is set. (f) Update Valkey section comment.

- [x] T005 [P] Update ws-server validate-config job `deployments/helm/odin/charts/ws-server/templates/validate-config-job.yaml` (FR-002, Constitution I). Three changes: (a) Replace 4-line `$broadcastType` init with single-line `{{ .Values.broadcastType | default "nats" }}`. (b) Update MESSAGE_BACKEND env var injection to only inject when non-default (`ne "direct"`). (c) Update BROADCAST_TYPE env var injection to use `.Values.broadcastType`, only inject when non-default (`ne "nats"`).

- [x] T006 [P] Replace auto-derivation with explicit `databaseDriver` in provisioning deployment `deployments/helm/odin/charts/provisioning/templates/deployment.yaml` (FR-003, Constitution I). Replace the 6-line auto-derivation block (checking `externalDatabase` and `global.postgresql.enabled`) with single-line `{{ .Values.databaseDriver | default "sqlite" }}`. Update the database driver comment. Update DATABASE_DRIVER env var injection to only inject when non-default (`ne "sqlite"`).

- [x] T007 [P] Replace auto-derivation with explicit `databaseDriver` in provisioning validate-config job `deployments/helm/odin/charts/provisioning/templates/validate-config-job.yaml` (FR-003, Constitution I). Replace the 6-line auto-derivation block with single-line `{{ .Values.databaseDriver | default "sqlite" }}`. Update DATABASE_DRIVER env var injection to only inject when non-default (`ne "sqlite"`).

- [x] T008 [P] Replace auto-derivation with explicit `databaseDriver` in provisioning PVC `deployments/helm/odin/charts/provisioning/templates/pvc.yaml` (FR-003). Replace the 6-line auto-derivation block with single-line `{{ .Values.databaseDriver | default "sqlite" }}`.

---

## Phase 3: Template Guards

> Create parent-chart guard infrastructure to catch all 14 mode/infrastructure mismatches.
> **Checkpoint**: `helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka` fails with CONFIG ERROR.

- [x] T009 Create `deployments/helm/odin/templates/_validate.tpl` (FR-004, FR-005). Define named template `odin.validate` containing all 14 guard rules per plan Phase 3.1: 3 forward guards (mode needs infra), 3 conflict guards (both in-cluster and external), 3 reverse in-cluster guards (infra enabled but mode doesn't use it), 3 reverse external guards (address set but mode doesn't use it), 2 deprecation guards (old renamed/removed keys). Use `index .Values "ws-server" ...` for subchart values, `.Values.redpanda.enabled` etc. for parent values.
  > Depends on: T001, T002, T003 (guards reference renamed values)

- [x] T010 Create `deployments/helm/odin/templates/validate-guards.yaml` (FR-004). Thin rendered template that invokes the guards via `{{- include "odin.validate" . -}}`. This is needed because Helm partials (`_*.tpl`) are not rendered directly.
  > Depends on: T009

---

## Phase 4: Environment Files & Examples

> Clean up Odin's environment files and update examples.
> **Checkpoint**: `grep -r 'broadcast\.type\|global\.postgresql\.enabled' deployments/helm/odin/values/standard/` returns no matches.

- [x] T011 [P] Remove `postgresql:` block from `deployments/helm/odin/values/standard/dev.yaml` (FR-007). Remove the entire 14-line block (enabled, auth, persistence, tolerations). Odin uses SQLite — this PostgreSQL pod was running idle.

- [x] T012 [P] Add `databaseDriver: postgres` to `examples/helm/values-production.yaml` (FR-012). Add `databaseDriver: postgres` to the provisioning section and update comments per plan Phase 5.1.

---

## Phase 5: Verification

> Validate all changes work together. Every command below must pass.

- [x] T013 Run `helm lint` on all chart + environment combinations (NFR-001):
  ```bash
  helm lint deployments/helm/odin
  helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/dev.yaml
  helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/stg.yaml
  helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/prod.yaml
  ```
  > Depends on: T001–T012

- [x] T014 Verify zero-config deploy renders correctly (NFR-003, SC-004):
  ```bash
  helm template odin deployments/helm/odin | grep -c 'kind: Deployment'
  # Expected: 3 (gateway, ws-server, provisioning) — no redpanda, valkey, postgresql
  ```
  > Depends on: T013

- [x] T015 Test all 14 template guards fire correctly (FR-004):
  ```bash
  # Forward guards (mode needs infra)
  # Guard 1: kafka without infrastructure → FAIL
  helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka
  # Guard 4: valkey without infrastructure → FAIL
  helm template odin deployments/helm/odin --set ws-server.broadcastType=valkey
  # Guard 7: postgres without infrastructure → FAIL
  helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres

  # Conflict guards (both in-cluster and external)
  # Guard 2: redpanda + external brokers → FAIL
  helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka --set redpanda.enabled=true --set ws-server.kafka.brokers=x
  # Guard 5: valkey + external addrs → FAIL
  helm template odin deployments/helm/odin --set ws-server.broadcastType=valkey --set valkey.enabled=true --set ws-server.valkey.addrs=x
  # Guard 8: postgresql + external db → FAIL
  helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres --set postgresql.enabled=true --set provisioning.externalDatabase.url=x

  # Reverse in-cluster guards (infra enabled, mode unused)
  # Guard 3: redpanda without kafka mode → FAIL
  helm template odin deployments/helm/odin --set redpanda.enabled=true
  # Guard 6: valkey without valkey mode → FAIL
  helm template odin deployments/helm/odin --set valkey.enabled=true
  # Guard 9: postgresql without postgres mode → FAIL
  helm template odin deployments/helm/odin --set postgresql.enabled=true

  # Reverse external guards (address set, mode unused)
  # Guard 10: external kafka without kafka mode → FAIL
  helm template odin deployments/helm/odin --set ws-server.kafka.brokers=x
  # Guard 11: external valkey without valkey mode → FAIL
  helm template odin deployments/helm/odin --set ws-server.valkey.addrs=x
  # Guard 12: external db without postgres mode → FAIL
  helm template odin deployments/helm/odin --set provisioning.externalDatabase.url=x

  # Deprecation guards (old keys)
  # Guard 13: old broadcast.type → FAIL
  helm template odin deployments/helm/odin --set ws-server.broadcast.type=valkey
  # Guard 14: old global.postgresql.enabled → FAIL
  helm template odin deployments/helm/odin --set global.postgresql.enabled=true
  ```
  > Depends on: T013

- [x] T016 Test valid configurations render successfully:
  ```bash
  # kafka + redpanda → PASS
  helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka --set redpanda.enabled=true > /dev/null

  # kafka + external brokers → PASS
  helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka --set ws-server.kafka.brokers=kafka:9092 > /dev/null

  # valkey + valkey.enabled → PASS
  helm template odin deployments/helm/odin --set ws-server.broadcastType=valkey --set valkey.enabled=true > /dev/null

  # postgres + postgresql.enabled → PASS
  helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres --set postgresql.enabled=true > /dev/null

  # postgres + external db → PASS
  helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres --set provisioning.externalDatabase.url=postgres://x > /dev/null

  # dev.yaml → PASS
  helm template odin deployments/helm/odin -f deployments/helm/odin/values/standard/dev.yaml > /dev/null
  ```
  > Depends on: T013

- [x] T017 Verify old patterns are fully removed (SC-003, SC-005):
  ```bash
  # No old nested broadcast key in env files
  grep -rn '^\s*broadcast:' deployments/helm/odin/values/standard/
  # Expected: no output

  # No postgresql section in env files
  grep -rn '^postgresql:' deployments/helm/odin/values/standard/
  # Expected: no output

  # No old nested broadcast key in examples
  grep -rn '^\s*broadcast:' examples/helm/
  # Expected: no output
  ```
  > Depends on: T001–T012

---

## Dependency Graph

```
Phase 1 (T001-T003): Values renaming
  T001 ─┐
  T002 ─┤── T003 (parent values references subchart naming)
        │
Phase 2 (T004-T008): Template updates [all parallel within phase]
  T004 ─┤
  T005 ─┤
  T006 ─┤
  T007 ─┤
  T008 ─┤
        │
Phase 3 (T009-T010): Template guards
  T009 ── T010 (wrapper needs named template)
        │
Phase 4 (T011-T012): Env files & examples [parallel within phase]
  T011 ─┤
  T012 ─┤
        │
Phase 5 (T013-T017): Verification [sequential]
  T013 ── T014
       ── T015
       ── T016
       ── T017
```

## Summary

| Phase | Tasks | Parallel |
|-------|-------|----------|
| 1. Helm Values | T001–T003 | T001, T002 parallel; T003 depends on both |
| 2. Template Updates | T004–T008 | All 5 parallel (different files) |
| 3. Template Guards | T009–T010 | Sequential (T010 depends on T009) |
| 4. Env Files & Examples | T011–T012 | Both parallel |
| 5. Verification | T013–T017 | T013 first, then T014–T017 |
| **Total** | **17 tasks** | **9 parallel opportunities** |
