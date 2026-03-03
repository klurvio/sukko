# Feature Specification: Consistent Single-Knob Infrastructure Configuration

**Branch**: `fix/odin-deploy-defaults`
**Created**: 2026-03-03
**Status**: Ready for Planning

## Context

After `feat/helm-minimal-config` established Go `envDefault` as the single source of truth, the Helm configuration has a developer experience problem: **configuring infrastructure requires coordinating two independent knobs** (mode selection + subchart enablement) with no validation between them. The naming is also inconsistent across the three infrastructure components.

### Current Two-Knob Problem

To use Redpanda for message backend, a developer must set two unrelated values:

```yaml
ws-server:
  messageBackend: kafka        # 1. Tell the binary what protocol to use
redpanda:
  enabled: true                # 2. Tell Helm to deploy the infrastructure
```

Getting one right but not the other fails silently or hangs:

| Mistake | Result |
|---------|--------|
| `messageBackend: kafka` without `redpanda.enabled: true` | Init container waits forever for non-existent Redpanda pod |
| `redpanda.enabled: true` without `messageBackend: kafka` | Redpanda pod runs idle, ws-server ignores it |
| External Kafka without setting `kafka.brokers` | Auto-wires to non-existent `RELEASE-redpanda:9092`, hangs |

### Why Two Knobs Exist (And Why We Can't Fully Eliminate Them)

Helm evaluates subchart conditions (`redpanda.enabled`) from static values BEFORE templates render. There is no way to dynamically compute `redpanda.enabled: true` from `messageBackend: kafka` in a Helm template — the subchart is already included or excluded by the time templates run.

This means infrastructure mode selection and subchart enablement are inherently two fields in Helm. The solution is not to eliminate the second field but to make it **impossible to get wrong**:
1. **Template guards** — `fail()` with actionable error messages when mode and infrastructure don't match
2. **Copy-paste recipes** — examples present mode + infrastructure as a single block to copy
3. **Decision guide** — parent `values.yaml` documents each mode as one recipe with both fields

### Current Naming Inconsistency

| Component | Current Helm knob | Format |
|-----------|------------------|--------|
| Message backend | `ws-server.messageBackend` | Top-level camelCase |
| Broadcast bus | `ws-server.broadcast.type` | Nested object |
| Provisioning DB | (none — auto-derived from `global.postgresql.enabled`) | No explicit knob |

### Odin's Actual Stack

| Component | Odin Uses | Go Default | Override Needed? |
|-----------|-----------|-----------|-----------------|
| Provisioning DB | SQLite | `sqlite` | No — matches default |
| Broadcast bus | NATS Core (in-cluster) | `nats` | No — matches default |
| Message backend | Redpanda (in-cluster) | `direct` | **Yes** — `messageBackend: kafka` |

### Additional Problem: dev.yaml enables PostgreSQL

`dev.yaml` has `postgresql.enabled: true` (14 lines of config), but Odin uses SQLite. The provisioning template auto-derives `DATABASE_DRIVER=postgres` from `global.postgresql.enabled`, so dev silently runs against PostgreSQL instead of SQLite.

## User Scenarios

### Scenario 1 - Kafka Mode: In-Cluster Redpanda (Priority: P0)

A developer wants persistent message ingestion via Redpanda.

**Acceptance Criteria**:
1. **Given** a values file with the Kafka recipe block (mode + subchart enablement), **When** deployed, **Then** Redpanda pod is created and ws-server connects to `{{ .Release.Name }}-redpanda:9092`
2. **Given** `messageBackend: kafka` WITHOUT `redpanda.enabled: true` AND without `kafka.brokers`, **When** `helm install` runs, **Then** the template MUST fail with: `messageBackend is 'kafka' but no Kafka infrastructure is configured. Either set redpanda.enabled: true (in-cluster) or set kafka.brokers (external).`
3. **Given** `messageBackend: direct` (or unset) AND `redpanda.enabled: true`, **When** `helm install` runs, **Then** the template MUST fail with: `redpanda.enabled is true but messageBackend is not 'kafka'. Redpanda will be deployed but unused. Set messageBackend: kafka to use it, or remove redpanda.enabled.`

### Scenario 2 - Kafka Mode: External/Managed Broker (Priority: P1)

A developer wants to connect to an external Kafka cluster.

**Acceptance Criteria**:
1. **Given** `ws-server.messageBackend: kafka` and `ws-server.kafka.brokers: "managed-kafka.example.com:9092"` without `redpanda.enabled`, **When** deployed, **Then** no Redpanda pod is created and ws-server connects to the external broker
2. **Given** `ws-server.messageBackend: kafka` and `ws-server.kafka.brokers` set AND `redpanda.enabled: true`, **When** `helm install` runs, **Then** the template MUST fail: `Both redpanda.enabled and kafka.brokers are set. Remove redpanda.enabled (use external) or remove kafka.brokers (use in-cluster).`

### Scenario 3 - Valkey Broadcast Mode (Priority: P1)

A developer wants Valkey instead of NATS for broadcast.

**Acceptance Criteria**:
1. **Given** a values file with the Valkey recipe block (mode + subchart enablement), **When** deployed, **Then** Valkey pod is created and ws-server connects to `{{ .Release.Name }}-valkey:6379`
2. **Given** `broadcastType: valkey` WITHOUT `valkey.enabled: true` AND without `valkey.addrs`, **When** `helm install` runs, **Then** template MUST fail with actionable error
3. **Given** `broadcastType: valkey` and `valkey.addrs: "external:6379"` without `valkey.enabled`, **When** deployed, **Then** no Valkey pod, connects to external
4. **Given** `broadcastType: nats` (default), **When** deployed, **Then** no Valkey pod is created

### Scenario 4 - PostgreSQL Mode (Priority: P1)

A developer wants production-grade storage.

**Acceptance Criteria**:
1. **Given** a values file with the PostgreSQL recipe block (mode + subchart enablement), **When** deployed, **Then** PostgreSQL pod is created and provisioning connects to it
2. **Given** `databaseDriver: postgres` WITHOUT `postgresql.enabled: true` AND without `externalDatabase`, **When** `helm install` runs, **Then** template MUST fail with actionable error
3. **Given** `databaseDriver: postgres` and `externalDatabase.url` set without `postgresql.enabled`, **When** deployed, **Then** no PostgreSQL pod, connects to external
4. **Given** `databaseDriver: sqlite` (default), **When** deployed, **Then** no PostgreSQL pod is created

### Scenario 5 - NATS JetStream Message Backend (Priority: P1)

A developer wants lightweight persistent streams using the existing NATS instance.

**Acceptance Criteria**:
1. **Given** a values file with `ws-server.messageBackend: nats`, **When** deployed, **Then** JetStream is enabled on the existing in-cluster NATS and `NATS_JETSTREAM_URLS` auto-wires to `{{ .Release.Name }}-nats:4222`
2. **Given** `ws-server.messageBackend: nats` and `ws-server.jetstream.urls: "nats://external:4222"`, **When** deployed, **Then** ws-server connects to external JetStream

### Scenario 6 - Odin's Internal Deployments (Priority: P0)

Odin's dev/stg/prod environment files use only the overrides that differ from Go defaults.

**Acceptance Criteria**:
1. **Given** Odin's environment files, **When** deployed, **Then** provisioning uses SQLite (no PostgreSQL pod), broadcast uses NATS (default), message backend uses Redpanda (in-cluster via `messageBackend: kafka`)
2. **Given** `dev.yaml`, **When** deployed, **Then** no PostgreSQL-related resources exist in the namespace

### Scenario 7 - Zero-Config Deploy (Priority: P0)

A developer deploys with no overrides at all.

**Acceptance Criteria**:
1. **Given** `helm install odin ./deployments/helm/odin` with no values file, **When** deployed, **Then** provisioning uses SQLite, broadcast uses in-cluster NATS, message backend uses direct mode, no Redpanda/Valkey/PostgreSQL pods are created

### Edge Cases

- What happens if `messageBackend: kafka` is set but both `redpanda` subchart is unavailable AND `kafka.brokers` is empty? The `--validate-config` pre-install hook MUST fail with: `[CONFIG ERROR] KAFKA_BROKERS is required when MESSAGE_BACKEND=kafka`
- What happens to existing dev data in PostgreSQL when switching to SQLite? SQLite starts fresh. Dev data is ephemeral and can be re-provisioned
- What if a developer sets both `broadcastType: valkey` and `broadcastType: nats`? Helm values are a single map — last write wins. Not a real conflict

## Requirements

### Functional Requirements

#### Consistent Single-Knob Pattern

- **FR-001**: Every infrastructure component MUST have exactly one mode-selection Helm value as the developer-facing knob. The naming pattern MUST be consistent — top-level camelCase on the service:

  | Component | Helm Value | Go Env Var | Valid Values |
  |-----------|-----------|-----------|-------------|
  | Message backend | `ws-server.messageBackend` | `MESSAGE_BACKEND` | `direct` (default), `kafka`, `nats` |
  | Broadcast bus | `ws-server.broadcastType` | `BROADCAST_TYPE` | `nats` (default), `valkey` |
  | Provisioning DB | `provisioning.databaseDriver` | `DATABASE_DRIVER` | `sqlite` (default), `postgres` |

- **FR-002**: `ws-server.broadcast.type` (nested) MUST be renamed to `ws-server.broadcastType` (top-level) for consistency with `messageBackend`

- **FR-003**: `provisioning.databaseDriver` MUST be added as an explicit Helm value, replacing the implicit auto-derivation from `global.postgresql.enabled`. The developer explicitly picks `sqlite` or `postgres` — no magic inference

#### Template Guards (Mode ↔ Infrastructure Validation)

- **FR-004**: Helm templates MUST include `fail()` guards that catch every mode/infrastructure mismatch at `helm install`/`helm template` time — before any pods are created. Each guard MUST produce an actionable error message telling the developer exactly what to set:

  | Mismatch | Error Message |
  |----------|---------------|
  | `messageBackend: kafka` without `redpanda.enabled` or `kafka.brokers` | `messageBackend is 'kafka' but no Kafka infrastructure is configured. Either set redpanda.enabled: true (in-cluster) or set kafka.brokers (external).` |
  | `broadcastType: valkey` without `valkey.enabled` or `valkey.addrs` | `broadcastType is 'valkey' but no Valkey infrastructure is configured. Either set valkey.enabled: true (in-cluster) or set valkey.addrs (external).` |
  | `databaseDriver: postgres` without `postgresql.enabled` or `externalDatabase` | `databaseDriver is 'postgres' but no PostgreSQL infrastructure is configured. Either set postgresql.enabled: true (in-cluster) or set externalDatabase.url (external).` |
  | `redpanda.enabled: true` with `ws-server.kafka.brokers` set | `Both redpanda.enabled and kafka.brokers are set. Remove redpanda.enabled (use external) or remove kafka.brokers (use in-cluster).` |
  | `valkey.enabled: true` with `ws-server.valkey.addrs` set | `Both valkey.enabled and valkey.addrs are set. Remove valkey.enabled (use external) or remove valkey.addrs (use in-cluster).` |
  | `postgresql.enabled: true` with `provisioning.externalDatabase` set | `Both postgresql.enabled and externalDatabase are set. Remove postgresql.enabled (use external) or remove externalDatabase (use in-cluster).` |
  | `redpanda.enabled: true` without `messageBackend: kafka` | `redpanda.enabled is true but messageBackend is not 'kafka'. Redpanda will be deployed but unused. Set messageBackend: kafka to use it, or remove redpanda.enabled.` |
  | `valkey.enabled: true` without `broadcastType: valkey` | `valkey.enabled is true but broadcastType is not 'valkey'. Valkey will be deployed but unused. Set broadcastType: valkey to use it, or remove valkey.enabled.` |
  | `postgresql.enabled: true` without `databaseDriver: postgres` | `postgresql.enabled is true but databaseDriver is not 'postgres'. PostgreSQL will be deployed but unused. Set databaseDriver: postgres to use it, or remove postgresql.enabled.` |
  | `ws-server.kafka.brokers` set without `messageBackend: kafka` | `kafka.brokers is set but messageBackend is not 'kafka'. The external Kafka address will be ignored. Set messageBackend: kafka to use it, or remove kafka.brokers.` |
  | `ws-server.valkey.addrs` set without `broadcastType: valkey` | `valkey.addrs is set but broadcastType is not 'valkey'. The external Valkey address will be ignored. Set broadcastType: valkey to use it, or remove valkey.addrs.` |
  | `provisioning.externalDatabase` set without `databaseDriver: postgres` | `externalDatabase is configured but databaseDriver is not 'postgres'. The external database will be ignored and provisioning will use SQLite. Set databaseDriver: postgres to use it, or remove externalDatabase.` |
  | `ws-server.broadcast.type` used (old key) | `ws-server.broadcast.type has been renamed to ws-server.broadcastType. Update your values file.` |
  | `global.postgresql.enabled` used (old key) | `global.postgresql.enabled is no longer supported. Set provisioning.databaseDriver: postgres instead.` |

- **FR-005**: Each infrastructure component has exactly two configuration paths. The template guards enforce that exactly one is active. All guards MUST live in the **parent chart** (`deployments/helm/odin/templates/_validate.tpl`) because they cross subchart boundaries (e.g., the Kafka guard checks both `ws-server.messageBackend` and `redpanda.enabled` — a subchart cannot access sibling chart values):

  | Component | In-Cluster Path | External Path |
  |-----------|----------------|--------------|
  | Kafka/Redpanda | `redpanda.enabled: true` (auto-wires address) | `ws-server.kafka.brokers: "host:port"` |
  | Valkey | `valkey.enabled: true` (auto-wires address) | `ws-server.valkey.addrs: "host:port"` |
  | PostgreSQL | `postgresql.enabled: true` (auto-wires URL) | `provisioning.externalDatabase.url` or `.existingSecret` |
  | NATS JetStream | Uses existing NATS subchart (always enabled) | `ws-server.jetstream.urls: "nats://host:port"` |

  Note: In the parent chart, subchart values are accessed via `index .Values "ws-server" "messageBackend"`, `index .Values "provisioning" "databaseDriver"`, etc. Parent-level values (`redpanda.enabled`, `valkey.enabled`, `postgresql.enabled`) are accessed directly via `.Values`.

- **FR-006**: When a mode does NOT require infrastructure, the corresponding subchart MUST NOT be deployed:
  - `messageBackend: direct` → no Redpanda
  - `broadcastType: nats` → no Valkey (NATS subchart is always deployed for broadcast)
  - `databaseDriver: sqlite` → no PostgreSQL

#### Copy-Paste Recipe Documentation

- **FR-006a**: The parent `values.yaml` decision guide MUST present each mode as a single copy-paste recipe block containing both the mode selection AND the infrastructure enablement. The developer copies one block — not two separate fields:

  ```yaml
  # ── Kafka/Redpanda message backend ──────────────────────────
  # Copy this block to enable Kafka ingestion via in-cluster Redpanda:
  #
  # ws-server:
  #   messageBackend: kafka
  # redpanda:
  #   enabled: true
  #
  # For external/managed Kafka, use instead:
  #
  # ws-server:
  #   messageBackend: kafka
  #   kafka:
  #     brokers: "your-kafka-host:9092"
  ```

- **FR-006b**: Example Helm values files (`examples/helm/values-*.yaml`) MUST show complete recipes with both mode and infrastructure as a unit. Each example MUST include inline comments explaining the in-cluster vs external choice

#### Odin Internal Deployment Cleanup

- **FR-007**: `dev.yaml` MUST remove the `postgresql:` block (enabled: true, auth, persistence, tolerations) — Odin uses SQLite
- **FR-008**: All three environment files (dev/stg/prod) MUST set `messageBackend: kafka` — the only non-default mode override for Odin
- **FR-009**: No environment file MUST set `broadcastType` or `databaseDriver` — the Go defaults (`nats`, `sqlite`) match Odin's actual usage
- **FR-010**: All `broadcast.type` references in environment files MUST be updated to `broadcastType` if any exist

#### Validation

- **FR-011**: Defense in depth: Template `fail()` guards (FR-004) are the primary defense — they catch mismatches at `helm template` time before any resources are created. The `--validate-config` pre-install hook is the secondary defense — it validates the *resolved* environment variables at runtime. If `MESSAGE_BACKEND=kafka` but `KAFKA_BROKERS` is empty or unset, the hook MUST fail with: `[CONFIG ERROR] KAFKA_BROKERS is required when MESSAGE_BACKEND=kafka`. The template guards prevent most mismatches; the hook catches any that slip through (e.g., `helm install --set` overrides that bypass values files)

#### Example Files

- **FR-012**: All example Helm values files (`examples/helm/values-*.yaml`) MUST be updated to use the new single-knob pattern (`broadcastType` instead of `broadcast.type`, `databaseDriver` instead of `postgresql.enabled`)

### Non-Functional Requirements

- **NFR-001**: All environment files and example values files MUST pass `helm lint` and `helm template`
- **NFR-002**: Switching between modes MUST require copying exactly one recipe block (mode knob + infrastructure enablement, documented as a single copy-paste unit). For external infrastructure, the recipe block contains the mode knob + address override instead of subchart enablement
- **NFR-003**: The zero-config deploy (no values file) MUST produce: SQLite + NATS broadcast + direct message backend + no unnecessary infrastructure pods

## Success Criteria

- **SC-001**: A developer copies the Kafka recipe block (mode + `redpanda.enabled`) and gets a working deployment. Omitting either field produces a `fail()` error telling them exactly what to set
- **SC-002**: A developer copies the PostgreSQL recipe block (mode + `postgresql.enabled`) and gets a working deployment. Omitting either field produces a `fail()` error telling them exactly what to set
- **SC-003**: `grep -rn '^\s*broadcast:' deployments/helm/odin/values/standard/` and `grep -rn '^postgresql:' deployments/helm/odin/values/standard/` both return no matches (old nested `broadcast:` key and top-level `postgresql:` section removed from env files)
- **SC-004**: `helm install odin ./deployments/helm/odin` with no values file produces zero Redpanda/Valkey/PostgreSQL pods
- **SC-005**: All example files use the new consistent naming

## Out of Scope

- Changing Go defaults — they are already correct (`sqlite`, `nats`, `direct`)
- Changing Go env var names — `MESSAGE_BACKEND`, `BROADCAST_TYPE`, `DATABASE_DRIVER` stay the same
- Adding new infrastructure options beyond what exists today
- Docker Compose changes — only Helm values and templates are affected
- `local.yaml` (Kind values) — local dev uses Docker Compose; Kind config is unused and can be updated separately if needed

## Clarifications

- Q: How is in-cluster vs managed/remote distinguished? → A: By the address override field. Empty address = in-cluster (subchart deployed, address auto-wired from `{{ .Release.Name }}`). Address set = external/managed (subchart not deployed, provided address used). No explicit toggle needed — the mode knob + address presence drives everything.
- Q: Why rename `broadcast.type` to `broadcastType`? → A: Consistency. `messageBackend` is top-level camelCase. `broadcast.type` is an inconsistent nested pattern. All mode selectors MUST follow the same `camelCase` naming at the same level.
- Q: Why add explicit `databaseDriver` instead of auto-deriving from `postgresql.enabled`? → A: The auto-derivation is a hidden side-effect that surprises developers. An explicit knob (`databaseDriver: postgres`) makes the intent clear and follows the same single-knob pattern as the other two components.
- Q: Can subchart enablement be auto-derived from the mode knob to make it truly single-knob? → A: No. Helm evaluates subchart conditions (`redpanda.enabled`) from static values BEFORE templates render — there is no way to dynamically compute them from template logic. The solution is template guards (`fail()`) that catch every mismatch at install time with actionable error messages, combined with copy-paste recipe documentation that presents mode + infrastructure as a single block to copy.
- Q: Where do `kafka.brokers`, `valkey.addrs`, `jetstream.urls` live in the values hierarchy? → A: All are nested under their owning subchart: `ws-server.kafka.brokers`, `ws-server.valkey.addrs`, `ws-server.jetstream.urls`. In ws-server templates they're accessed as `.Values.kafka.brokers` etc. Similarly, `provisioning.externalDatabase.url` is accessed as `.Values.externalDatabase.url` in provisioning templates.
- Q: Where do template guards live — parent chart or subcharts? → A: All guards MUST live in the parent chart (`deployments/helm/odin/templates/_validate.tpl`). Guards cross subchart boundaries (e.g., Kafka guard checks both `ws-server.messageBackend` and `redpanda.enabled`). Subcharts cannot access sibling chart values — only the parent chart has access to all values via `index .Values "subchart-name" "field"`.
- Q: How does the validate-config hook relate to template guards? → A: Template `fail()` guards are the primary defense (run at `helm template` time, before any resources). The `--validate-config` hook is defense-in-depth (runs the Go binary to validate resolved env vars). Template guards catch values-file mismatches; the hook catches `--set` overrides and edge cases that bypass values files.
- Q: Should `local.yaml` (Kind) be updated? → A: No. Local dev uses Docker Compose; Kind config is unused. Out of scope — can be updated separately if Kind is revived.
- Q: Should template guards catch reverse mismatches (infrastructure enabled but mode doesn't use it)? → A: Yes. Guards catch both directions: forward (mode needs infra, infra not enabled) AND reverse (infra enabled, mode doesn't use it). This prevents idle infrastructure and aligns with the "impossible to get wrong" goal.
