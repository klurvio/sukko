# Tasks: Minimal Helm Configuration

**Branch**: feat/helm-minimal-config | **Generated**: 2026-03-03

---

## Phase 1: Go Config Alignment & Cleanup

**Goal**: Make Go `envDefault` the single source of truth. Remove config mode. Fix AUTH_ENABLED. Add `--validate-config`.

**Checkpoint**: `cd ws && go vet ./... && go test ./internal/shared/platform/... && go test ./internal/provisioning/...`

- [x] T001 [P] Align Go envDefaults with Helm production values in `ws/internal/shared/platform/server_config.go` — change `BROADCAST_TYPE` envDefault from `"valkey"` to `"nats"`, `WS_MAX_BROADCAST_RATE` from `"20"` to `"25"`, `CONN_RATE_LIMIT_IP_BURST` from `"10"` to `"100"`, `CONN_RATE_LIMIT_IP_RATE` from `"1.0"` to `"100.0"`, `ENVIRONMENT` from `"development"` to `"local"`. In `Validate()`: reject empty BROADCAST_TYPE (remove `""` from valid set). In `ws/internal/server/broadcast/factory.go`: remove `case ""` fallback — factory only receives validated values.

- [x] T002 [P] Auto-compute hysteresis lower thresholds in `ws/internal/shared/platform/server_config.go` — change `CPURejectThresholdLower` and `CPUPauseThresholdLower` envDefault to `"0"` (sentinel). In `Validate()`, add auto-compute: `if lower == 0 { lower = upper - 10.0 }` before the existing `lower < upper` validation. Update `ws/internal/shared/platform/server_config_test.go` — add test cases for auto-compute behavior (lower=0 → computed as upper-10) and explicit lower override (lower!=0 → unchanged).

- [x] T003 Delete provisioning configstore package — remove entire directory `ws/internal/provisioning/configstore/` (loader.go, parser.go, validator.go, types.go, stores.go, loader_test.go, parser_test.go, validator_test.go, stores_test.go)

- [x] T004 [P] Remove config mode and align ENVIRONMENT in `ws/internal/shared/platform/provisioning_config.go` — (1) remove `ProvisioningMode` and `ConfigFilePath` struct fields, remove mode enum validation, remove ConfigFilePath-required-for-config-mode check, remove database-validation-skip for config mode. Update `Print()`/`LogConfig()` to remove config mode branches. (2) Change `Environment` envDefault from `"development"` to `"local"` (FR-005b — same change as T001 for server, must be consistent across all three configs). Update `ws/internal/shared/platform/provisioning_config_test.go` — remove config mode tests.

- [x] T005 Remove config mode from provisioning main in `ws/cmd/provisioning/main.go` — remove config mode switch branch (~lines 95-124), remove SIGHUP handler (~lines 309-345), remove config reload metrics (~lines 35-50), remove `configLoader` variable and `configstore` import, remove `db != nil` guard on admin auth validation (db is always non-nil). Depends on: T003, T004.

- [x] T006 [P] Remove CLI config commands in `ws/cmd/cli/commands/config.go` — remove `configInitCmd`, `configValidateCmd`, `configExportCmd`, and `configCmd` parent. Delete the file if nothing remains after removal.

- [x] T007 [P] Fix AUTH_ENABLED default and align ENVIRONMENT in `ws/internal/shared/platform/gateway_config.go` — (1) change `AuthEnabled` envDefault from `"true"` to `"false"`, add TODO comment: `// TODO: Change envDefault to "true" when odin-api auth integration is production-ready` (FR-018a). (2) Change `Environment` envDefault from `"development"` to `"local"` (FR-005b — same change as T001 for server, must be consistent across all three configs). Update `ws/internal/shared/platform/gateway_config_test.go` if existing tests depend on AUTH_ENABLED=true default or ENVIRONMENT=development.

- [x] T008 Add `--validate-config` flag to all three binaries. Modify `ws/cmd/server/main.go`: add `flag.Bool("validate-config", ...)`, parse flags, after config load+validate: if flag set, print "Configuration valid" and `os.Exit(0)`. Same pattern for `ws/cmd/gateway/main.go` (add flag import and parsing before env.Parse) and `ws/cmd/provisioning/main.go`. Depends on: T005.

- [x] T009 Enhance `Validate()` error messages in `ws/internal/shared/platform/server_config.go`, `gateway_config.go`, and `provisioning_config.go` — format: `[CONFIG ERROR] ENV_VAR="value" is invalid (valid: opt1, opt2)` and `[CONFIG ERROR] ENV_VAR is required when CONDITION`. Apply to mode selection validation, required-field-when-mode checks, and range validation. Depends on: T001, T002, T004.

**Phase 1 verification**:
```bash
cd ws && go vet ./...
cd ws && go test ./internal/shared/platform/...
cd ws && go test ./internal/provisioning/...
cd ws && go build -o /tmp/ws-server ./cmd/server && /tmp/ws-server --validate-config
cd ws && go build -o /tmp/ws-gateway ./cmd/gateway && /tmp/ws-gateway --validate-config
cd ws && go build -o /tmp/provisioning ./cmd/provisioning && /tmp/provisioning --validate-config
```

---

## Phase 2: Config & Version Introspection

**Goal**: Expose `/config` endpoint on all binaries with redaction. Each service exposes its own config individually.

**Checkpoint**: `cd ws && go test ./internal/shared/platform/...`

- [x] T010 [P] Create shared config handler `ws/internal/shared/platform/config_handler.go` — implement `ConfigHandler(cfg any) http.HandlerFunc` following `version.Handler()` pattern. Call `RedactConfig()` ONCE at handler creation time, cache result in closure (zero reflection at request time — config is immutable after startup, avoids per-request reflection on shared hot-path mux). Implement `RedactConfig(cfg any) map[string]any` using reflection: walk struct fields, use `env` tag as JSON key (fallback to field name), replace fields tagged `redact:"true"` with `"[REDACTED]"` (only when non-empty; empty stays empty). Use `httputil.WriteJSON` for response writing.

- [x] T011 [P] Create config handler tests `ws/internal/shared/platform/config_handler_test.go` — table-driven tests covering: (1) redacts sensitive fields with `redact:"true"` tag, (2) preserves non-sensitive fields, (3) empty secrets stay empty (not "[REDACTED]"), (4) handler returns 200 with JSON content-type, (5) pointer vs value struct handling, (6) unexported fields are skipped.

- [x] T012 [P] Add `redact:"true"` struct tags to sensitive fields in `ws/internal/shared/platform/server_config.go` — fields: `KafkaSASLPassword` (~line 79), `ValkeyPassword` (~line 208), `NATSToken` (~line 271), `NATSPassword` (~line 273), `NATSJetStreamToken` (~line 291), `NATSJetStreamPassword` (~line 293). Only passwords/tokens — NOT usernames, CA paths, or non-secret fields.

- [x] T013 [P] Add `redact:"true"` struct tags to sensitive fields in `ws/internal/shared/platform/provisioning_config.go` — fields: `AdminToken`, `DatabaseURL` (may contain credentials in connection string).

- [x] T014 Wire `/config` endpoint into ws-server in `ws/internal/server/orchestration/loadbalancer.go` (~line 115) — add `mux.HandleFunc("/config", platform.ConfigHandler(lb.cfg))` after `/version` route. Ensure config is accessible from LoadBalancer (add `cfg *platform.ServerConfig` to `LoadBalancerConfig` struct and store in LoadBalancer). Depends on: T010.

- [x] T015 Wire `/config` endpoint into provisioning in `ws/internal/provisioning/api/router.go` (~line 68) — add `r.Get("/config", platform.ConfigHandler(cfg.Config))` after `/version` route. Add `Config *platform.ProvisioningConfig` field to `RouterConfig` struct. Depends on: T010.

- [x] T016 Wire `/config` endpoint into gateway in `ws/internal/gateway/gateway.go` (~line 438) — add `mux.HandleFunc("/config", platform.ConfigHandler(gw.config))` after `/version` route. No aggregation — gateway serves its own config only, same as ws-server and provisioning. Depends on: T010.

**Phase 2 verification**:
```bash
cd ws && go vet ./...
cd ws && go test ./internal/shared/platform/...
```

---

## Phase 3: Helm Values & Templates Cleanup

**Goal**: Reduce subchart values.yaml to K8s-required fields. Rewrite templates to only inject non-default env vars.

**Checkpoint**: `helm lint deployments/helm/odin/charts/ws-server && helm lint deployments/helm/odin/charts/ws-gateway && helm lint deployments/helm/odin/charts/provisioning && helm lint deployments/helm/odin`

- [x] T020 [P] Rewrite `deployments/helm/odin/charts/ws-server/values.yaml` — strip from 292 to ~85 lines. Keep: enabled, replicaCount, image, resources, probes, service, pod/security contexts, autoscaling, networkPolicy, podAnnotations, nodeSelector, tolerations, affinity, connectionLimits.clusterMaxConnections. Add commented `# environment: local` field (deployment identity — drives Kafka topic namespace, consumer groups; any string, Odin uses local/dev/stg/prod). Add `extraEnv: {}` escape hatch. Remove all config.* fields that match Go defaults (shards, basePort, lbAddr, maxKafkaRate, slowClientMaxAttempts, defaultTenantID, CPU thresholds, timeouts, log settings, metrics interval, ping/pong, provisioning reconnect, topic refresh). Remove all alerting.*, audit.* sections. Keep minimal nested defaults for template nil-safety: `kafka: { sasl: { enabled: false }, tls: { enabled: false } }`, `nats: { tls: { enabled: false } }`, etc. Add comments documenting valid values for mode selections.

- [x] T021 [P] Rewrite `deployments/helm/odin/charts/ws-gateway/values.yaml` — strip from 154 to ~55 lines. Keep: enabled, replicaCount, image, resources, probes, service, pod/security contexts, autoscaling, podAnnotations, nodeSelector, tolerations, affinity, permissions (public/userScoped/groupScoped patterns). Add commented `# environment: local` field. Add `extraEnv: {}`. Remove all config.* fields that match Go defaults (timeouts, log settings, auth refresh, OIDC cache, JWKS settings, provisioning reconnect). Add comments for feature flags (authEnabled, multiIssuerOIDCEnabled, perTenantChannelRulesEnabled). Note: authEnabled defaults false per FR-018a.

- [x] T022 [P] Rewrite `deployments/helm/odin/charts/provisioning/values.yaml` — strip from 187 to ~65 lines. Keep: enabled, replicaCount, image, resources, probes, service, pod/security contexts, persistence, ingress, podAnnotations, nodeSelector, tolerations, affinity. Add commented `# environment: local` field. Add `extraEnv: {}`. Remove: provisioningMode, configFilePath, configFileConfigMap (FR-007a), all config.* fields matching Go defaults (log settings, autoMigrate, authEnabled, timeouts, quota defaults, lifecycle settings, admin auth, key registry, shutdown timeout), database pool settings. Add commented `externalDatabase:` section for PostgreSQL.

- [x] T023 Rewrite `deployments/helm/odin/charts/ws-server/templates/configmap.yaml` — strip from 69 to ~20 lines. Keep only: WS_MAX_CONNECTIONS (computed from connectionLimits.clusterMaxConnections / replicaCount), WS_MAX_GOROUTINES (computed). Add `extraEnv` range loop: `{{- range $key, $val := .Values.extraEnv }}`. Remove all lines that set env vars matching Go defaults. Depends on: T020.

- [x] T024 Rewrite `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` env section — reduce env vars from ~200 to ~80 lines. Keep: deployment identity (ENVIRONMENT — always injected: `{{ .Values.environment | default "local" }}`), mode selection (MESSAGE_BACKEND, BROADCAST_TYPE — only if set in values), auto-wired addresses (NATS_URLS, PROVISIONING_GRPC_ADDR, KAFKA_BROKERS conditional, NATS_JETSTREAM_URLS conditional, VALKEY_ADDRS conditional), conditional SASL/TLS blocks. Remove: all alerting/audit env vars, DEFAULT_TENANT_ID, all rate limit/timeout/ping-pong/CPU threshold vars, TOPIC_REFRESH_INTERVAL, KAFKA_CONSUMER_ENABLED, KAFKA_DEFAULT_*. Remove hysteresis `{{ sub }}` computation (Go auto-computes). Remove PROVISIONING_MODE injection. Depends on: T020, T023.

- [x] T025 Rewrite `deployments/helm/odin/charts/ws-gateway/templates/deployment.yaml` env section — same pattern as ws-server: ENVIRONMENT always injected (`{{ .Values.environment | default "local" }}`), auto-wired addresses (GATEWAY_BACKEND_URL, PROVISIONING_GRPC_ADDR), mode flags (AUTH_ENABLED, feature flags — only if set), conditional TLS/auth blocks. Remove all config.* env vars matching Go defaults. Depends on: T021.

- [x] T026 [P] Update `deployments/helm/odin/charts/ws-gateway/templates/configmap.yaml` — align with new pattern (extraEnv range loop only, or delete if no computed values needed for gateway).

- [x] T027 Rewrite `deployments/helm/odin/charts/provisioning/templates/deployment.yaml` — remove PROVISIONING_MODE and PROVISIONING_CONFIG_PATH env var injection, remove config file volume mount logic. Only set: DATABASE_DRIVER (auto-derived from externalDatabase presence), DATABASE_URL (from secret), ENVIRONMENT, auto-wired addresses. Add extraEnv inline in deployment env section (provisioning has no configmap template): `{{- range $key, $val := .Values.extraEnv }}` loop in the `env:` block. Depends on: T022.

- [x] T028 Rewrite parent `deployments/helm/odin/values.yaml` with decision guide — strip from 568 to ~180 lines. Add commented deployment mode guide at top (FR-017): zero-config defaults, Docker Compose local dev, Kafka mode, JetStream mode, PostgreSQL mode, Valkey mode. Reference examples/helm/ files. Keep: global settings, service enables (ws-gateway, ws-server, provisioning.enabled=true), infrastructure subchart enables (nats.enabled=true, jetstream.enabled=false, redpanda.enabled=false, valkey.enabled=false, postgresql.enabled=false, monitoring.enabled=true). Remove ALL subchart config overrides that duplicate subchart defaults (FR-003). Depends on: T020, T021, T022.

- [x] T029 Create validation hook `deployments/helm/odin/charts/ws-server/templates/validate-config-job.yaml` — Helm pre-install/pre-upgrade Job that runs `./odin-ws --validate-config` with same env vars as deployment. `helm.sh/hook-weight: "-5"`, `hook-delete-policy: before-hook-creation,hook-succeeded`, `backoffLimit: 0`. Depends on: T024.

- [x] T030 [P] Create validation hook `deployments/helm/odin/charts/ws-gateway/templates/validate-config-job.yaml` — same pattern as T029 for gateway binary. Depends on: T025.

- [x] T031 [P] Create validation hook `deployments/helm/odin/charts/provisioning/templates/validate-config-job.yaml` — same pattern as T029 for provisioning binary. Depends on: T027.

**Phase 3 verification**:
```bash
helm lint deployments/helm/odin/charts/ws-server
helm lint deployments/helm/odin/charts/ws-gateway
helm lint deployments/helm/odin/charts/provisioning
helm lint deployments/helm/odin
helm template odin deployments/helm/odin
```

---

## Phase 4: Docker Compose & Examples

**Goal**: `docker compose up` → working WebSocket in 30 seconds. Example values for each mode.

**Checkpoint**: `docker compose config && for f in examples/helm/values-*.yaml; do helm lint deployments/helm/odin -f "$f"; done`

- [x] T032 Create `docker-compose.yml` at repo root — four services: (1) nats (nats:2.10-alpine, ports 4222+8222, healthcheck), (2) provisioning (build from ws/build/provisioning/Dockerfile, ports 8080+9090, healthcheck on /health, build args for COMMIT_HASH), (3) ws-server (build from ws/build/server/Dockerfile, ports 3001, depends_on nats+provisioning healthy, build args for COMMIT_HASH), (4) ws-gateway (build from ws/build/gateway/Dockerfile, port 3000, depends_on ws-server). **Minimal env vars**: only inter-service wiring (NATS_URLS, PROVISIONING_GRPC_ADDR, GATEWAY_BACKEND_URL) — all other config uses Go envDefaults after Phase 1 alignment. Add provisioning-data volume. Add commented-out mode override blocks: Redpanda+MESSAGE_BACKEND=kafka, PostgreSQL+DATABASE_DRIVER=postgres, NATS JetStream+MESSAGE_BACKEND=nats, Valkey+BROADCAST_TYPE=valkey, Auth (AUTH_ENABLED=true).

- [x] T033 [P] Create `examples/helm/values-minimal.yaml` — commented documentation showing zero-config is the default (direct + NATS + SQLite). No fields needed — exists as documentation with inline explanation of every Go default.

- [x] T034 [P] Create `examples/helm/values-kafka.yaml` — Kafka/Redpanda mode: `ws-server.messageBackend: kafka`, `redpanda.enabled: true`. Comments explaining each field, when to set broker addresses for external Kafka, SASL/TLS options (commented), consumer group settings, topic namespace.

- [x] T035 [P] Create `examples/helm/values-nats-jetstream.yaml` — NATS JetStream mode: `ws-server.messageBackend: nats`, `nats.jetstream.enabled: true`. Comments explaining JetStream vs broadcast NATS, external vs in-cluster, stream replicas and max age.

- [x] T036 [P] Create `examples/helm/values-production.yaml` — production-grade: ws-server (replicaCount=3, messageBackend=kafka, kafka brokers+SASL+TLS, resources, autoscaling), ws-gateway (replicaCount=3, resources, autoscaling, AUTH_ENABLED=true via extraEnv), provisioning (replicaCount=2, externalDatabase secret), external NATS (nats.enabled=false), external Kafka (redpanda.enabled=false), external PostgreSQL (postgresql.enabled=false). Comments on every field.

- [x] T036b Add CI validation target for example Helm values files in `taskfiles/k8s.yml` — add a `helm:lint:examples` task that runs `helm lint` and `helm template` against all `examples/helm/values-*.yaml` files (FR-013). Spec explicitly includes this: Out of Scope carves an exception for "adding `helm lint` for example files". Depends on: T033, T034, T035, T036.

**Phase 4 verification**:
```bash
docker compose config
docker compose build
for f in examples/helm/values-*.yaml; do helm lint deployments/helm/odin -f "$f"; done
helm template odin deployments/helm/odin -f examples/helm/values-kafka.yaml
```

---

## Phase 5: Odin Internal Deployment Update

**Goal**: Update Odin's environment overrides and Taskfiles for new Helm structure.

**Checkpoint**: `helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/dev.yaml`

- [x] T037 Rewrite `deployments/helm/odin/values/standard/dev.yaml` — strip from 326 to ~60 lines. Keep only: global (namespace, imageRegistry), service replicas and image tags, deployment identity (`environment: dev` on all three services — drives Kafka topic namespace, consumer groups), mode selections (messageBackend: kafka, broadcast.type: nats), infrastructure enables (redpanda, nats, postgresql), resources, tolerations (Spot VMs), service types. Move any non-default tuning to extraEnv. Remove all config.* fields matching Go defaults. Depends on: T028.

- [x] T038 [P] Rewrite `deployments/helm/odin/values/standard/stg.yaml` — same pattern as dev.yaml. Strip to deployment-level settings, deployment identity (`environment: stg` on all three services), mode selections, infrastructure topology only. Depends on: T028.

- [x] T039 [P] Rewrite `deployments/helm/odin/values/standard/prod.yaml` — same pattern. Deployment identity (`environment: prod` on all three services — triggers Kafka namespace override guard), production resources, replicas, external service URLs, TLS/SASL config. Depends on: T028.

- [x] T040 Update Taskfile targets in `taskfiles/k8s.yml` and `taskfiles/local.yml` — remove references to `provisioningMode`, update any `--set` flags that reference removed values.yaml fields. Ensure `helm upgrade` commands work with new values structure. Also check `taskfiles/gce.yml` for Terraform references to changed fields. Depends on: T037.

- [x] T041 Create migration checklist `specs/feat/helm-minimal-config/migration-checklist.md` — document every default that changed: BROADCAST_TYPE (valkey→nats), WS_MAX_BROADCAST_RATE (20→25), CONN_RATE_LIMIT_IP_BURST (10→100), CONN_RATE_LIMIT_IP_RATE (1.0→100.0), ENVIRONMENT (development→local), AUTH_ENABLED (true→false, temporary), PROVISIONING_MODE (removed), provisioning.enabled (false→true), CPU lower thresholds (Helm→Go auto-compute), all removed config.* fields (use extraEnv if custom needed). Include migration action for each.

---

## Phase 6: Final Verification

- [x] T042 Run full verification suite:
  - Go: `cd ws && go vet ./... && go test ./...`
  - Helm: `helm lint` on all charts and all example values files
  - Docker Compose: `docker compose config`
  - Validate-config: build all three binaries and run `--validate-config`
  - Introspection: verify `/config` returns redacted JSON on all three binaries (queried individually)
  - Verify line counts: ws-server/values.yaml <100, ws-gateway/values.yaml <60, provisioning/values.yaml <80, parent values.yaml <200, dev.yaml <80

---

## Summary

| Phase | Tasks | Parallel |
|-------|-------|----------|
| 1: Go Config | T001–T009 | T001, T002, T004, T006, T007 parallel; T003→T005→T008 sequential; T009 after T001+T002+T004 |
| 2: Config Introspection | T010–T016 | T010, T011, T012, T013 parallel; T014, T015, T016 after T010 |
| 3: Helm Cleanup | T020–T031 | T020, T021, T022, T026 parallel; T023→T024, T025, T027, T028 depend on values; T029–T031 after templates |
| 4: Docker Compose & Examples | T032–T036b | T033, T034, T035, T036 parallel; T032 independent; T036b after examples |
| 5: Odin Internal | T037–T041 | T038, T039 parallel; T037→T040 sequential; T041 independent |
| 6: Verification | T042 | — |

**Total**: 40 tasks (6 phases)
**Parallel opportunities**: 22 tasks marked [P]
**Start with**: T001, T002, T003, T004, T006, T007 (all independent)
