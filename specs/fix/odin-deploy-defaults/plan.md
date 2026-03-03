# Implementation Plan: Consistent Single-Knob Infrastructure Configuration

**Branch**: `fix/odin-deploy-defaults` | **Date**: 2026-03-03 | **Spec**: `specs/fix/odin-deploy-defaults/spec.md`

## Summary

Rename inconsistent Helm values (`broadcast.type` → `broadcastType`, add explicit `databaseDriver`), create template guards in the parent chart to catch all 14 mode/infrastructure mismatches at `helm template` time, clean up Odin's environment files, and update example documentation. No Go code changes — Helm-only.

## Technical Context

**Language**: Go 1.22+ (no changes — env vars unchanged)
**Infrastructure**: Helm 3 charts (`deployments/helm/odin/`)
**Affected Charts**: parent (`odin`), `ws-server`, `provisioning`
**Not Affected**: `ws-gateway`, `nats`, `redpanda`, `valkey`, `monitoring`, Go source, Docker Compose

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | COMPLIANT | Go `envDefault` stays source of truth. Helm values renamed to match existing Go env vars (`BROADCAST_TYPE`, `DATABASE_DRIVER`). No new env vars. |
| II. Defense in Depth | COMPLIANT | Template guards (primary, `helm template` time) + `--validate-config` hook (secondary, runtime) = layered defense. |
| III. Error Handling | COMPLIANT | Every `fail()` guard produces an actionable error message telling the developer exactly what to set. |
| IV. Graceful Degradation | COMPLIANT | Zero-config deploy (no values file) works: SQLite + NATS + direct. No half-initialized state. |
| V–XII | N/A | Helm-only changes — no Go code, no logging, no metrics, no concurrency. |

## Research Notes

### Helm `index` for Subchart Values

The parent chart accesses subchart values via `index .Values "subchart-name" "field"`. For our subcharts:

```go
// ws-server values (hyphenated name requires index)
{{ index .Values "ws-server" "messageBackend" }}
{{ index .Values "ws-server" "broadcastType" }}
{{ index .Values "ws-server" "kafka" "brokers" }}
{{ index .Values "ws-server" "valkey" "addrs" }}

// provisioning values
{{ index .Values "provisioning" "databaseDriver" }}
{{ index .Values "provisioning" "externalDatabase" "url" }}
{{ index .Values "provisioning" "externalDatabase" "existingSecret" }}

// parent-level values (direct access)
{{ .Values.redpanda.enabled }}
{{ .Values.valkey.enabled }}
{{ .Values.postgresql.enabled }}
```

### Init Container Fix

The ws-server `wait-for-redpanda` init container always waits for `{{ .Release.Name }}-redpanda:9092`, even with external Kafka (`kafka.brokers` set). This causes hangs when using external Kafka without the in-cluster Redpanda subchart. Fix: skip init container when `kafka.brokers` is set. Same pattern for `wait-for-nats-jetstream` when `jetstream.urls` is set.

### `global.postgresql.enabled` is Dead After This Change

The parent `values.yaml` has `global.postgresql.enabled: false`. This is currently read by the provisioning template for auto-derivation. After replacing auto-derivation with explicit `databaseDriver`, no template reads `global.postgresql.enabled`. Remove it to avoid confusion.

### dev.yaml Correction

The spec says "dev silently runs against PostgreSQL." More precisely: dev.yaml sets `postgresql.enabled: true` (deploys the PostgreSQL pod) but does NOT set `global.postgresql.enabled: true` (the provisioning template's auto-derivation signal). So provisioning actually uses SQLite — the PostgreSQL pod runs idle. The fix is the same: remove the `postgresql:` block from dev.yaml.

---

## Phase 1: Helm Values Renaming

**Goal**: FR-001, FR-002, FR-003 — consistent top-level camelCase naming

### 1.1 ws-server Subchart Values

**File**: `deployments/helm/odin/charts/ws-server/values.yaml`
**Change**: Rename nested `broadcast.type` to top-level `broadcastType`

```yaml
# BEFORE (lines 32-35):
# Broadcast Bus: "nats" (Go default) or "valkey"
# Inter-pod pub/sub for message fan-out across server instances.
broadcast:
  type: nats

# AFTER:
# Broadcast Bus: "nats" (Go default) or "valkey"
# Inter-pod pub/sub for message fan-out across server instances.
broadcastType: nats
```

### 1.2 Provisioning Subchart Values

**File**: `deployments/helm/odin/charts/provisioning/values.yaml`
**Changes**:
1. Add explicit `databaseDriver: sqlite` knob
2. Remove dead `global.postgresql.enabled`

```yaml
# BEFORE (lines 5-8):
global:
  imageRegistry: ""
  postgresql:
    enabled: false

# AFTER:
global:
  imageRegistry: ""

# BEFORE (lines 29-32):
# Database: SQLite by default. Set externalDatabase to use PostgreSQL.
database:
  # SQLite file path (only when DATABASE_DRIVER=sqlite)
  sqlitePath: "/data/odin.db"

# AFTER:
# Database driver: "sqlite" (Go default) or "postgres"
# Set databaseDriver: postgres with postgresql.enabled or externalDatabase.
databaseDriver: sqlite

database:
  # SQLite file path (only when databaseDriver=sqlite)
  sqlitePath: "/data/odin.db"

# BEFORE (lines 34-38):
# External PostgreSQL (auto-derives DATABASE_DRIVER=postgres when set)
externalDatabase:

# AFTER:
# External PostgreSQL (when databaseDriver=postgres without postgresql.enabled)
externalDatabase:
```

### 1.3 Parent Chart Values

**File**: `deployments/helm/odin/values.yaml`
**Changes**:
1. Remove `global.postgresql.enabled` (dead after explicit `databaseDriver`)
2. Rename `ws-server.broadcast.type` to `ws-server.broadcastType`
3. Add `provisioning.databaseDriver: sqlite`
4. Add recipe documentation comments (FR-006a)

Remove `global.postgresql`:
```yaml
# BEFORE (lines 22-29):
global:
  namespace: odin
  imageRegistry: ""
  labels:
    app.kubernetes.io/part-of: odin
  # PostgreSQL bundled (default: false → SQLite in provisioning)
  postgresql:
    enabled: false

# AFTER:
global:
  namespace: odin
  imageRegistry: ""
  labels:
    app.kubernetes.io/part-of: odin
```

Rename broadcast and add recipe docs to ws-server section:
```yaml
# BEFORE (lines 85-88):
  # Mode selections (Go defaults: direct + nats)
  messageBackend: direct
  broadcast:
    type: nats

# AFTER:
  # Mode selections (Go defaults: direct + nats)
  messageBackend: direct
  broadcastType: nats
```

Add `databaseDriver` to provisioning section:
```yaml
# BEFORE (lines 114-120):
provisioning:
  enabled: true
  replicaCount: 1
  image:
    ...
  # External PostgreSQL (optional — overrides SQLite default)
  externalDatabase:

# AFTER:
provisioning:
  enabled: true
  replicaCount: 1
  image:
    ...
  # Database driver: "sqlite" (default) or "postgres"
  databaseDriver: sqlite
  # External PostgreSQL (when databaseDriver: postgres without postgresql.enabled)
  externalDatabase:
```

Replace the header comment block with recipe documentation:
```yaml
# DEPLOYMENT MODES (zero-config defaults work out of the box):
#
# 1. Minimal (default):    direct + NATS broadcast + SQLite
#    → Just `helm install odin .` — no external dependencies needed
#
# ── Kafka/Redpanda message backend ──────────────────────────────────
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
#
# ── NATS JetStream message backend ──────────────────────────────────
# Copy this block for lightweight persistent streaming:
#
# ws-server:
#   messageBackend: nats
# nats:
#   jetstream:
#     enabled: true
#
# ── Valkey broadcast bus ────────────────────────────────────────────
# Copy this block to replace NATS with Valkey for broadcast:
#
# ws-server:
#   broadcastType: valkey
# valkey:
#   enabled: true
#
# ── PostgreSQL provisioning database ────────────────────────────────
# Copy this block for production-grade storage:
#
# provisioning:
#   databaseDriver: postgres
# postgresql:
#   enabled: true
#
# For external PostgreSQL, use instead:
#
# provisioning:
#   databaseDriver: postgres
#   externalDatabase:
#     existingSecret: "my-db-credentials"
```

---

## Phase 2: Template Updates

**Goal**: Update all templates to use renamed values and fix init containers

### 2.1 ws-server Deployment Template

**File**: `deployments/helm/odin/charts/ws-server/templates/deployment.yaml`
**Changes**:

1. **Lines 27-30** — Simplify `$broadcastType` initialization:
```yaml
# BEFORE:
{{- $broadcastType := "nats" }}
{{- if .Values.broadcast }}
  {{- $broadcastType = .Values.broadcast.type | default "nats" }}
{{- end }}

# AFTER:
{{- $broadcastType := .Values.broadcastType | default "nats" }}
```

2. **Lines 108-111** — Update MESSAGE_BACKEND env var injection (Constitution I — don't duplicate Go default):
```yaml
# BEFORE:
{{- if .Values.messageBackend }}
- name: MESSAGE_BACKEND
  value: {{ .Values.messageBackend | quote }}
{{- end }}

# AFTER:
{{- if and .Values.messageBackend (ne .Values.messageBackend "direct") }}
- name: MESSAGE_BACKEND
  value: {{ .Values.messageBackend | quote }}
{{- end }}
```

3. **Lines 112-115** — Update BROADCAST_TYPE env var injection:
```yaml
# BEFORE:
{{- if .Values.broadcast }}
- name: BROADCAST_TYPE
  value: {{ .Values.broadcast.type | default "nats" | quote }}
{{- end }}

# AFTER:
{{- if and .Values.broadcastType (ne .Values.broadcastType "nats") }}
- name: BROADCAST_TYPE
  value: {{ .Values.broadcastType | quote }}
{{- end }}
```

4. **Lines 48-59** — Fix init container for external Kafka:
```yaml
# BEFORE:
{{- if $needsKafka }}
- name: wait-for-redpanda
  command: ['sh', '-c', 'until nc -z {{ .Release.Name }}-redpanda 9092; ...]

# AFTER:
{{- if and $needsKafka (not .Values.kafka.brokers) }}
- name: wait-for-redpanda
  command: ['sh', '-c', 'until nc -z {{ .Release.Name }}-redpanda 9092; ...]
```

5. **Lines 60-71** — Fix init container for external JetStream:
```yaml
# BEFORE:
{{- if $needsJetStream }}
- name: wait-for-nats-jetstream

# AFTER:
{{- if and $needsJetStream (not .Values.jetstream.urls) }}
- name: wait-for-nats-jetstream
```

6. **Line 204-205** — Update Valkey comment:
```yaml
# BEFORE: # Valkey (only when broadcast.type=valkey)
# AFTER:  # Valkey (only when broadcastType=valkey)
```

### 2.2 ws-server Validate-Config Job

**File**: `deployments/helm/odin/charts/ws-server/templates/validate-config-job.yaml`
**Changes**: Same `broadcastType` rename + MESSAGE_BACKEND injection fix as deployment.yaml (lines 2-5, 52-55, 57-59)

```yaml
# BEFORE (lines 2-5):
{{- $broadcastType := "nats" }}
{{- if .Values.broadcast }}
  {{- $broadcastType = .Values.broadcast.type | default "nats" }}
{{- end }}

# AFTER:
{{- $broadcastType := .Values.broadcastType | default "nats" }}

# BEFORE (lines 52-55):
{{- if .Values.messageBackend }}
- name: MESSAGE_BACKEND
  value: {{ .Values.messageBackend | quote }}
{{- end }}

# AFTER:
{{- if and .Values.messageBackend (ne .Values.messageBackend "direct") }}
- name: MESSAGE_BACKEND
  value: {{ .Values.messageBackend | quote }}
{{- end }}

# BEFORE (line 57-59):
{{- if .Values.broadcast }}
- name: BROADCAST_TYPE
  value: {{ .Values.broadcast.type | default "nats" | quote }}
{{- end }}

# AFTER:
{{- if and .Values.broadcastType (ne .Values.broadcastType "nats") }}
- name: BROADCAST_TYPE
  value: {{ .Values.broadcastType | quote }}
{{- end }}
```

### 2.3 Provisioning Deployment Template

**File**: `deployments/helm/odin/charts/provisioning/templates/deployment.yaml`
**Changes**: Replace auto-derivation with explicit `databaseDriver`

```yaml
# BEFORE (lines 2-13):
{{- /*
  Auto-derive DATABASE_DRIVER from values:
  - If externalDatabase.url or externalDatabase.existingSecret is set → postgres
  - If global.postgresql.enabled → postgres
  - Otherwise → sqlite (default)
*/ -}}
{{- $dbDriver := "sqlite" -}}
{{- if or .Values.externalDatabase.url .Values.externalDatabase.existingSecret -}}
  {{- $dbDriver = "postgres" -}}
{{- else if .Values.global.postgresql.enabled -}}
  {{- $dbDriver = "postgres" -}}
{{- end -}}

# AFTER:
{{- /* Database driver: explicit from databaseDriver Helm value (no auto-derivation) */ -}}
{{- $dbDriver := .Values.databaseDriver | default "sqlite" -}}
```

Update comment on line 75:
```yaml
# BEFORE: # Database driver (auto-derived from externalDatabase presence)
# AFTER:  # Database driver (explicit — set via databaseDriver Helm value)
```

Update DATABASE_DRIVER env var injection (lines 76-77) — only inject when non-default (Constitution I):
```yaml
# BEFORE:
- name: DATABASE_DRIVER
  value: {{ $dbDriver | quote }}

# AFTER:
{{- if ne $dbDriver "sqlite" }}
- name: DATABASE_DRIVER
  value: {{ $dbDriver | quote }}
{{- end }}
```

### 2.4 Provisioning Validate-Config Job

**File**: `deployments/helm/odin/charts/provisioning/templates/validate-config-job.yaml`
**Changes**: Same — replace auto-derivation with explicit `databaseDriver` + DATABASE_DRIVER injection fix

```yaml
# BEFORE (lines 2-7):
{{- $dbDriver := "sqlite" -}}
{{- if or .Values.externalDatabase.url .Values.externalDatabase.existingSecret -}}
  {{- $dbDriver = "postgres" -}}
{{- else if .Values.global.postgresql.enabled -}}
  {{- $dbDriver = "postgres" -}}
{{- end -}}

# AFTER:
{{- $dbDriver := .Values.databaseDriver | default "sqlite" -}}
```

Update DATABASE_DRIVER env var injection (lines 46-47) — only inject when non-default:
```yaml
# BEFORE:
- name: DATABASE_DRIVER
  value: {{ $dbDriver | quote }}

# AFTER:
{{- if ne $dbDriver "sqlite" }}
- name: DATABASE_DRIVER
  value: {{ $dbDriver | quote }}
{{- end }}
```

### 2.5 Provisioning PVC

**File**: `deployments/helm/odin/charts/provisioning/templates/pvc.yaml`
**Changes**: Same — replace auto-derivation

```yaml
# BEFORE (lines 3-8):
{{- $dbDriver := "sqlite" -}}
{{- if or .Values.externalDatabase.url .Values.externalDatabase.existingSecret -}}
  {{- $dbDriver = "postgres" -}}
{{- else if .Values.global.postgresql.enabled -}}
  {{- $dbDriver = "postgres" -}}
{{- end -}}

# AFTER:
{{- $dbDriver := .Values.databaseDriver | default "sqlite" -}}
```

---

## Phase 3: Template Guards

**Goal**: FR-004, FR-005 — catch all 14 mode/infrastructure mismatches

### 3.1 Create `_validate.tpl`

**File**: `deployments/helm/odin/templates/_validate.tpl` (NEW)
**Content**: 14 guard rules using `fail()`. All guards in parent chart because they cross subchart boundaries.

```yaml
{{- /*
  Infrastructure validation guards.
  Catch mode/infrastructure mismatches at helm template time
  with actionable error messages.
*/ -}}

{{- /* === Kafka / Redpanda === */ -}}
{{- $messageBackend := index .Values "ws-server" "messageBackend" | default "direct" -}}
{{- $kafkaBrokers := "" -}}
{{- if index .Values "ws-server" "kafka" -}}
  {{- $kafkaBrokers = index .Values "ws-server" "kafka" "brokers" | default "" -}}
{{- end -}}
{{- $redpandaEnabled := .Values.redpanda.enabled | default false -}}

{{- /* Guard 1: kafka mode without infrastructure */ -}}
{{- if and (eq $messageBackend "kafka") (not $redpandaEnabled) (not $kafkaBrokers) -}}
  {{- fail "\n[CONFIG ERROR] messageBackend is 'kafka' but no Kafka infrastructure is configured.\nEither set redpanda.enabled: true (in-cluster) or set ws-server.kafka.brokers (external)." -}}
{{- end -}}

{{- /* Guard 2: redpanda + external brokers conflict */ -}}
{{- if and $redpandaEnabled $kafkaBrokers -}}
  {{- fail "\n[CONFIG ERROR] Both redpanda.enabled and kafka.brokers are set.\nRemove redpanda.enabled (use external) or remove kafka.brokers (use in-cluster)." -}}
{{- end -}}

{{- /* Guard 3: redpanda enabled but mode doesn't use it */ -}}
{{- if and $redpandaEnabled (ne $messageBackend "kafka") -}}
  {{- fail (printf "\n[CONFIG ERROR] redpanda.enabled is true but messageBackend is '%s'.\nRedpanda will be deployed but unused. Set messageBackend: kafka to use it, or remove redpanda.enabled." $messageBackend) -}}
{{- end -}}

{{- /* === Valkey === */ -}}
{{- $broadcastType := index .Values "ws-server" "broadcastType" | default "nats" -}}
{{- $valkeyAddrs := "" -}}
{{- if index .Values "ws-server" "valkey" -}}
  {{- $valkeyAddrs = index .Values "ws-server" "valkey" "addrs" | default "" -}}
{{- end -}}
{{- $valkeyEnabled := .Values.valkey.enabled | default false -}}

{{- /* Guard 4: valkey mode without infrastructure */ -}}
{{- if and (eq $broadcastType "valkey") (not $valkeyEnabled) (not $valkeyAddrs) -}}
  {{- fail "\n[CONFIG ERROR] broadcastType is 'valkey' but no Valkey infrastructure is configured.\nEither set valkey.enabled: true (in-cluster) or set ws-server.valkey.addrs (external)." -}}
{{- end -}}

{{- /* Guard 5: valkey enabled + external addrs conflict */ -}}
{{- if and $valkeyEnabled $valkeyAddrs -}}
  {{- fail "\n[CONFIG ERROR] Both valkey.enabled and valkey.addrs are set.\nRemove valkey.enabled (use external) or remove valkey.addrs (use in-cluster)." -}}
{{- end -}}

{{- /* Guard 6: valkey enabled but mode doesn't use it */ -}}
{{- if and $valkeyEnabled (ne $broadcastType "valkey") -}}
  {{- fail (printf "\n[CONFIG ERROR] valkey.enabled is true but broadcastType is '%s'.\nValkey will be deployed but unused. Set broadcastType: valkey to use it, or remove valkey.enabled." $broadcastType) -}}
{{- end -}}

{{- /* === PostgreSQL === */ -}}
{{- $databaseDriver := index .Values "provisioning" "databaseDriver" | default "sqlite" -}}
{{- $extDBUrl := "" -}}
{{- $extDBSecret := "" -}}
{{- if index .Values "provisioning" "externalDatabase" -}}
  {{- $extDBUrl = index .Values "provisioning" "externalDatabase" "url" | default "" -}}
  {{- $extDBSecret = index .Values "provisioning" "externalDatabase" "existingSecret" | default "" -}}
{{- end -}}
{{- $pgEnabled := .Values.postgresql.enabled | default false -}}
{{- $hasExternalDB := or $extDBUrl $extDBSecret -}}

{{- /* Guard 7: postgres mode without infrastructure */ -}}
{{- if and (eq $databaseDriver "postgres") (not $pgEnabled) (not $hasExternalDB) -}}
  {{- fail "\n[CONFIG ERROR] databaseDriver is 'postgres' but no PostgreSQL infrastructure is configured.\nEither set postgresql.enabled: true (in-cluster) or set provisioning.externalDatabase.url (external)." -}}
{{- end -}}

{{- /* Guard 8: postgresql enabled + external db conflict */ -}}
{{- if and $pgEnabled $hasExternalDB -}}
  {{- fail "\n[CONFIG ERROR] Both postgresql.enabled and externalDatabase are set.\nRemove postgresql.enabled (use external) or remove externalDatabase (use in-cluster)." -}}
{{- end -}}

{{- /* Guard 9: postgresql enabled but mode doesn't use it */ -}}
{{- if and $pgEnabled (ne $databaseDriver "postgres") -}}
  {{- fail (printf "\n[CONFIG ERROR] postgresql.enabled is true but databaseDriver is '%s'.\nPostgreSQL will be deployed but unused. Set databaseDriver: postgres to use it, or remove postgresql.enabled." $databaseDriver) -}}
{{- end -}}

{{- /* === Reverse External Guards (address set, mode doesn't use it) === */ -}}

{{- /* Guard 10: external kafka address without kafka mode */ -}}
{{- if and $kafkaBrokers (ne $messageBackend "kafka") -}}
  {{- fail (printf "\n[CONFIG ERROR] kafka.brokers is set but messageBackend is '%s'.\nThe external Kafka address will be ignored. Set messageBackend: kafka to use it, or remove kafka.brokers." $messageBackend) -}}
{{- end -}}

{{- /* Guard 11: external valkey address without valkey mode */ -}}
{{- if and $valkeyAddrs (ne $broadcastType "valkey") -}}
  {{- fail (printf "\n[CONFIG ERROR] valkey.addrs is set but broadcastType is '%s'.\nThe external Valkey address will be ignored. Set broadcastType: valkey to use it, or remove valkey.addrs." $broadcastType) -}}
{{- end -}}

{{- /* Guard 12: external database without postgres mode */ -}}
{{- if and $hasExternalDB (ne $databaseDriver "postgres") -}}
  {{- fail (printf "\n[CONFIG ERROR] externalDatabase is configured but databaseDriver is '%s'.\nThe external database will be ignored and provisioning will use SQLite. Set databaseDriver: postgres to use it, or remove externalDatabase." $databaseDriver) -}}
{{- end -}}

{{- /* === Deprecation Guards (catch old renamed/removed keys) === */ -}}

{{- /* Guard 13: old broadcast.type key still present */ -}}
{{- if index .Values "ws-server" "broadcast" -}}
  {{- fail "\n[CONFIG ERROR] ws-server.broadcast.type has been renamed to ws-server.broadcastType.\nUpdate your values file." -}}
{{- end -}}

{{- /* Guard 14: old global.postgresql.enabled key still present */ -}}
{{- if .Values.global.postgresql -}}
  {{- fail "\n[CONFIG ERROR] global.postgresql.enabled is no longer supported.\nSet provisioning.databaseDriver: postgres instead." -}}
{{- end -}}
```

**Note**: The `_validate.tpl` file is a partial template — it's automatically included by Helm when any parent chart template is rendered. The guards execute during `helm template`/`helm install` and block rendering if any mismatch is found.

**Important**: Helm partials (`_*.tpl`) are NOT rendered directly. They must be `include`d or `template`d from a rendered template. We need a thin wrapper to invoke the guards:

### 3.2 Create `_validate-guards.yaml` Wrapper

**File**: `deployments/helm/odin/templates/_validate-guards.yaml`

Actually, a better pattern: make `_validate.tpl` define a named template, and include it from an existing rendered template. Or, simpler — create a small rendered template that only does validation:

**File**: `deployments/helm/odin/templates/validate-guards.yaml` (NOT prefixed with `_`)

```yaml
{{- /* This template only performs validation — no resources rendered */ -}}
{{- include "odin.validate" . -}}
```

And `_validate.tpl` wraps everything in a `define`:
```yaml
{{- define "odin.validate" -}}
  ... all 14 guards ...
{{- end -}}
```

---

## Phase 4: Environment Files

**Goal**: FR-007, FR-008, FR-009, FR-010

### 4.1 dev.yaml

**File**: `deployments/helm/odin/values/standard/dev.yaml`
**Change**: Remove `postgresql:` block (lines 127-140)

```yaml
# REMOVE these 14 lines:
# ── PostgreSQL ──────────────────────────────────────────────────────
postgresql:
  enabled: true
  auth:
    database: odin_provisioning
    username: odin
  persistence:
    enabled: true
    size: 1Gi
  tolerations:
    - key: "cloud.google.com/gke-spot"
      operator: "Equal"
      value: "true"
      effect: "NoSchedule"
```

**Verification**: After removal, dev.yaml has `messageBackend: kafka` + `redpanda.enabled: true` (valid recipe). No `broadcast.type`, no `databaseDriver`, no `postgresql.enabled`.

### 4.2 stg.yaml — No changes needed

Already correct: `messageBackend: kafka` + `redpanda.enabled: true`. No `broadcast.type`, no `postgresql.enabled`.

### 4.3 prod.yaml — No changes needed

Already correct: `messageBackend: kafka` + `redpanda.enabled: true`. No `broadcast.type`, no `postgresql.enabled`.

---

## Phase 5: Example Files & Documentation

**Goal**: FR-006a, FR-006b, FR-012

### 5.1 values-production.yaml

**File**: `examples/helm/values-production.yaml`
**Changes**: Add `databaseDriver: postgres` and update comments

```yaml
# BEFORE (lines 64-70):
provisioning:
  replicaCount: 2
  environment: prod
  # External PostgreSQL (required for multi-replica)
  externalDatabase:

# AFTER:
provisioning:
  replicaCount: 2
  environment: prod
  # PostgreSQL mode (required for multi-replica)
  databaseDriver: postgres
  # External PostgreSQL connection
  externalDatabase:
```

### 5.2 values-kafka.yaml — No changes needed

Already uses correct pattern: `messageBackend: kafka` + `redpanda.enabled: true`. No `broadcast.type`.

### 5.3 values-nats-jetstream.yaml — No changes needed

Already uses correct pattern: `messageBackend: nats` + `nats.jetstream.enabled: true`. No `broadcast.type`.

### 5.4 values-minimal.yaml — No changes needed

Documentation only.

---

## Phase 6: Verification

### 6.1 Helm Lint

```bash
helm lint deployments/helm/odin
helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/dev.yaml
helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/stg.yaml
helm lint deployments/helm/odin -f deployments/helm/odin/values/standard/prod.yaml
```

### 6.2 Zero-Config Deploy (Scenario 7)

```bash
helm template odin deployments/helm/odin | grep -c 'kind: Deployment'
# Expected: 3 (gateway, ws-server, provisioning) — no redpanda, valkey, postgresql
```

### 6.3 Guard Testing

```bash
# Forward guards (mode needs infra)
# Guard 1: kafka without infrastructure → should FAIL
helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka 2>&1 | grep "CONFIG ERROR"
# Guard 4: valkey without infrastructure → should FAIL
helm template odin deployments/helm/odin --set ws-server.broadcastType=valkey 2>&1 | grep "CONFIG ERROR"
# Guard 7: postgres without infrastructure → should FAIL
helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres 2>&1 | grep "CONFIG ERROR"

# Conflict guards (both in-cluster and external)
# Guard 2: redpanda + external brokers → should FAIL
helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka --set redpanda.enabled=true --set ws-server.kafka.brokers=x 2>&1 | grep "CONFIG ERROR"
# Guard 5: valkey + external addrs → should FAIL
helm template odin deployments/helm/odin --set ws-server.broadcastType=valkey --set valkey.enabled=true --set ws-server.valkey.addrs=x 2>&1 | grep "CONFIG ERROR"
# Guard 8: postgresql + external db → should FAIL
helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres --set postgresql.enabled=true --set provisioning.externalDatabase.url=x 2>&1 | grep "CONFIG ERROR"

# Reverse in-cluster guards (infra enabled, mode unused)
# Guard 3: redpanda without kafka mode → should FAIL
helm template odin deployments/helm/odin --set redpanda.enabled=true 2>&1 | grep "CONFIG ERROR"
# Guard 6: valkey without valkey mode → should FAIL
helm template odin deployments/helm/odin --set valkey.enabled=true 2>&1 | grep "CONFIG ERROR"
# Guard 9: postgresql without postgres mode → should FAIL
helm template odin deployments/helm/odin --set postgresql.enabled=true 2>&1 | grep "CONFIG ERROR"

# Reverse external guards (address set, mode unused)
# Guard 10: external kafka without kafka mode → should FAIL
helm template odin deployments/helm/odin --set ws-server.kafka.brokers=x 2>&1 | grep "CONFIG ERROR"
# Guard 11: external valkey without valkey mode → should FAIL
helm template odin deployments/helm/odin --set ws-server.valkey.addrs=x 2>&1 | grep "CONFIG ERROR"
# Guard 12: external database without postgres mode → should FAIL
helm template odin deployments/helm/odin --set provisioning.externalDatabase.url=x 2>&1 | grep "CONFIG ERROR"

# Deprecation guards (old keys)
# Guard 13: old broadcast.type key → should FAIL
helm template odin deployments/helm/odin --set ws-server.broadcast.type=valkey 2>&1 | grep "CONFIG ERROR"
# Guard 14: old global.postgresql.enabled key → should FAIL
helm template odin deployments/helm/odin --set global.postgresql.enabled=true 2>&1 | grep "CONFIG ERROR"

# Valid: kafka + redpanda → should PASS
helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka --set redpanda.enabled=true > /dev/null && echo "PASS"
# Valid: kafka + external brokers → should PASS
helm template odin deployments/helm/odin --set ws-server.messageBackend=kafka --set ws-server.kafka.brokers=kafka:9092 > /dev/null && echo "PASS"
# Valid: valkey + valkey.enabled → should PASS
helm template odin deployments/helm/odin --set ws-server.broadcastType=valkey --set valkey.enabled=true > /dev/null && echo "PASS"
# Valid: postgres + postgresql.enabled → should PASS
helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres --set postgresql.enabled=true > /dev/null && echo "PASS"
# Valid: postgres + external db → should PASS
helm template odin deployments/helm/odin --set provisioning.databaseDriver=postgres --set provisioning.externalDatabase.url=postgres://x > /dev/null && echo "PASS"
# Valid: dev.yaml → should PASS
helm template odin deployments/helm/odin -f deployments/helm/odin/values/standard/dev.yaml > /dev/null && echo "PASS"
```

### 6.4 Success Criteria Verification

```bash
# SC-003: No old nested broadcast key in env files
grep -rn '^\s*broadcast:' deployments/helm/odin/values/standard/
# Expected: no output

# SC-003: No postgresql section in env files
grep -rn '^postgresql:' deployments/helm/odin/values/standard/
# Expected: no output

# SC-005: No old nested broadcast key in examples
grep -rn '^\s*broadcast:' examples/helm/
# Expected: no output
```

---

## File Change Summary

| Action | File | FRs |
|--------|------|-----|
| MODIFY | `deployments/helm/odin/charts/ws-server/values.yaml` | FR-002 |
| MODIFY | `deployments/helm/odin/charts/provisioning/values.yaml` | FR-003 |
| MODIFY | `deployments/helm/odin/values.yaml` | FR-001, FR-002, FR-003, FR-006a |
| MODIFY | `deployments/helm/odin/charts/ws-server/templates/deployment.yaml` | FR-002 |
| MODIFY | `deployments/helm/odin/charts/ws-server/templates/validate-config-job.yaml` | FR-002 |
| MODIFY | `deployments/helm/odin/charts/provisioning/templates/deployment.yaml` | FR-003 |
| MODIFY | `deployments/helm/odin/charts/provisioning/templates/validate-config-job.yaml` | FR-003 |
| MODIFY | `deployments/helm/odin/charts/provisioning/templates/pvc.yaml` | FR-003 |
| CREATE | `deployments/helm/odin/templates/_validate.tpl` | FR-004, FR-005 |
| CREATE | `deployments/helm/odin/templates/validate-guards.yaml` | FR-004 |
| MODIFY | `deployments/helm/odin/values/standard/dev.yaml` | FR-007 |
| MODIFY | `examples/helm/values-production.yaml` | FR-012 |

**Total**: 10 MODIFY + 2 CREATE = 12 file changes
