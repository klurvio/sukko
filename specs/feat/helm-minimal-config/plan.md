# Implementation Plan: Minimal Helm Configuration

**Branch**: feat/helm-minimal-config | **Date**: 2026-03-03 | **Spec**: specs/feat/helm-minimal-config/spec.md

## Summary

Eliminate developer friction by making Go `envDefault` values the single source of truth for all configuration. Helm values.yaml files are stripped to Kubernetes-required fields only (image, resources, probes, service, security). Developers override by adding fields to their values file. Docker Compose provides the zero-friction "try it out" path. Pre-install validation hooks catch misconfig before pods exist. Config and version introspection endpoints provide runtime visibility on each service's admin mux.

## Technical Context

**Language**: Go 1.22+
**Services**: ws-server, ws-gateway, provisioning
**Infrastructure**: Kubernetes (GKE Standard), Helm, Terraform, Docker Compose
**Messaging**: Redpanda/Kafka (franz-go), NATS (broadcast + JetStream)
**Storage**: PostgreSQL, SQLite (provisioning)
**Monitoring**: Prometheus, Grafana, Loki
**Build/Deploy**: Docker, Taskfile, Artifact Registry

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Configuration | COMPLIANT | Strengthened — Go envDefault becomes single source of truth |
| II. Defense in Depth | COMPLIANT | Config validation at startup + Helm pre-install hook |
| III. Error Handling | COMPLIANT | Validation errors name exact env var and expected value |
| IV. Graceful Degradation | COMPLIANT | Direct mode + SQLite = zero external dependencies |
| V. Structured Logging | N/A | No logging changes |
| VI. Observability | COMPLIANT | `/config` endpoint exposes runtime config for operational visibility |
| VII. Concurrency Safety | N/A | No concurrency changes |
| VIII. Testing | COMPLIANT | Config validation tests updated, config mode tests removed, introspection tests added |
| IX. Security | COMPLIANT | AUTH_ENABLED defaults **false** in Go (temporary per FR-018a — TODO in code for future flip to true); `/config` redacts sensitive fields |
| X. Shared Code | COMPLIANT | Config handler + redaction in shared/platform |
| XI. Prior Art | COMPLIANT | Decision guide based on industry patterns; `/config` redaction follows 12-factor admin process pattern |
| XII. API Design | COMPLIANT | `/health`, `/config`, `/version` on admin mux (internal); each service queried individually |

**Justified violation**: None.

---

## Phase 1: Go Default Alignment & Config Cleanup

**Goal**: Make Go `envDefault` the single source of truth. Remove config mode from provisioning. Fix AUTH_ENABLED default. Add `--validate-config` flag.

### 1.1 — Align Go Defaults with Helm Production Values (FR-005, FR-005b)

**File**: `ws/internal/shared/platform/server_config.go`

| Field | Change | Env Var |
|-------|--------|---------|
| `BroadcastType` | envDefault `"valkey"` → `"nats"` | `BROADCAST_TYPE` |
| `MaxBroadcastRate` | envDefault `"20"` → `"25"` | `WS_MAX_BROADCAST_RATE` |
| `ConnRateLimitIPBurst` | envDefault `"10"` → `"100"` | `CONN_RATE_LIMIT_IP_BURST` |
| `ConnRateLimitIPRate` | envDefault `"1.0"` → `"100.0"` | `CONN_RATE_LIMIT_IP_RATE` |
| `Environment` | envDefault `"development"` → `"local"` | `ENVIRONMENT` |

**Also**: Update `Validate()` to reject empty `BROADCAST_TYPE` (remove from valid set). Remove `case ""` fallback from `ws/internal/server/broadcast/factory.go` — the factory only receives validated values. If BROADCAST_TYPE is unset, envDefault provides `"nats"`. If explicitly set to empty, startup fails with: `[CONFIG ERROR] BROADCAST_TYPE="" is invalid (valid: nats, valkey)`.

### 1.2 — Auto-compute Hysteresis Lower Thresholds (FR-009)

**File**: `ws/internal/shared/platform/server_config.go`

Change `CPURejectThresholdLower` and `CPUPauseThresholdLower` envDefault to `"0"` (sentinel for auto-compute). In `Validate()`, add logic:

```go
// Auto-compute lower thresholds when not explicitly set (0 = auto)
if c.CPURejectThresholdLower == 0 {
    c.CPURejectThresholdLower = c.CPURejectThreshold - 10.0
}
if c.CPUPauseThresholdLower == 0 {
    c.CPUPauseThresholdLower = c.CPUPauseThreshold - 10.0
}
```

Existing validation (lower < upper) still applies after auto-compute.

**File**: `ws/internal/shared/platform/server_config_test.go`
- Update hysteresis tests for auto-compute behavior
- Add test: when lower=0, computed as upper-10

### 1.3 — Fix AUTH_ENABLED Default (FR-018a)

**File**: `ws/internal/shared/platform/gateway_config.go` (line 29)

Change `AuthEnabled` envDefault from `"true"` to `"false"`:

```go
// TODO: Change envDefault to "true" when sukko-api auth integration is production-ready
AuthEnabled bool `env:"AUTH_ENABLED" envDefault:"false"`
```

This aligns Go with the current Helm subchart/parent default (`false`). Docker Compose needs no override since auth is off by default.

**Also**: Change `Environment` envDefault from `"development"` to `"local"` (FR-005b — same change as 1.1 for server, must be consistent across all three config structs).

### 1.4 — Remove Provisioning Config Mode (FR-007a)

**DELETE** entire package:
- `ws/internal/provisioning/configstore/` (loader.go, parser.go, validator.go, types.go, stores.go + all *_test.go files)

**MODIFY** `ws/internal/shared/platform/provisioning_config.go`:
- Remove fields: `ProvisioningMode`, `ConfigFilePath`
- Remove validation: mode enum check, ConfigFilePath requirement for config mode
- Remove conditional: database validation skip for config mode
- Update `Print()` and `LogConfig()` to remove config mode branches
- Change `Environment` envDefault from `"development"` to `"local"` (FR-005b — same change as 1.1 for server, must be consistent across all three config structs)

**MODIFY** `ws/internal/shared/platform/provisioning_config_test.go`:
- Remove `TestProvisioningConfig_Validate_ProvisioningMode()`
- Remove `TestProvisioningConfig_Validate_ConfigModeRequiresPath()`

**MODIFY** `ws/cmd/provisioning/main.go`:
- Remove config mode switch branch (~lines 95-124)
- Remove SIGHUP handler (~lines 309-345)
- Remove config reload metrics (~lines 35-50)
- Remove `configLoader` variable and `configstore` import
- Simplify: always use database path (db is never nil)
- Remove `db != nil` guard on admin auth validation

**MODIFY** `ws/cmd/cli/commands/config.go`:
- Remove `configInitCmd`, `configValidateCmd`, `configExportCmd`
- Remove `configCmd` command group (or file entirely if nothing remains)

### 1.5 — Add --validate-config Flag (FR-014, FR-016)

**MODIFY** `ws/cmd/server/main.go`:
```go
var validateConfig = flag.Bool("validate-config", false, "Validate configuration and exit")
flag.Parse()

cfg, err := platform.LoadServerConfig(nil)
if err != nil {
    fmt.Fprintf(os.Stderr, "[CONFIG ERROR] %v\n", err)
    os.Exit(1)
}
if *validateConfig {
    fmt.Println("Configuration valid")
    os.Exit(0)
}
```

**MODIFY** `ws/cmd/gateway/main.go`:
- Add `flag` import and `--validate-config` flag
- Parse flags before config loading
- Same pattern as server

**MODIFY** `ws/cmd/provisioning/main.go`:
- Add `--validate-config` flag (same pattern)

### 1.6 — Enhance Validate() Error Messages (FR-016)

**Files**: `ws/internal/shared/platform/server_config.go`, `gateway_config.go`, `provisioning_config.go`

Enhance validation error messages to be developer-friendly:
```go
// Current: "broadcast type must be valkey, redis, or nats"
// New: "[CONFIG ERROR] BROADCAST_TYPE=\"%s\" is invalid (valid: nats, valkey)"
```

Pattern: `fmt.Errorf("[CONFIG ERROR] %s=%q is invalid (valid: %s)", envName, value, validValues)`

Apply to all validation errors in all three config structs:
- Mode selection validation (BROADCAST_TYPE, MESSAGE_BACKEND, DATABASE_DRIVER)
- Required-field-when-mode checks (KAFKA_BROKERS when MESSAGE_BACKEND=kafka)
- Range/threshold validation

### Phase 1 Verification
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

**Goal**: Expose runtime configuration and version info through admin endpoints. Each service exposes its own `/config` endpoint for operational visibility.

### 2.1 — Create Shared Config Handler (FR-022)

**CREATE** `ws/internal/shared/platform/config_handler.go`:

Follows the same pattern as `version.Handler()` in `shared/version/handler.go`:

```go
package platform

import (
    "net/http"
    "reflect"

    "sukko/internal/shared/httputil"
)

// ConfigHandler returns an http.HandlerFunc that serves the effective runtime
// configuration as JSON with sensitive fields redacted.
// Reflection runs ONCE at handler creation — cached in closure for zero-cost serving.
func ConfigHandler(cfg any) http.HandlerFunc {
    // Cache redacted config at creation time — config is immutable after startup.
    // This avoids per-request reflection on the shared mux (same mux as /ws hot path).
    redacted := RedactConfig(cfg)
    return func(w http.ResponseWriter, _ *http.Request) {
        httputil.WriteJSON(w, http.StatusOK, redacted)
    }
}

// RedactConfig walks struct fields via reflection and replaces fields tagged
// with `redact:"true"` with "[REDACTED]". Returns a map suitable for JSON.
func RedactConfig(cfg any) map[string]any {
    result := make(map[string]any)
    v := reflect.ValueOf(cfg)
    if v.Kind() == reflect.Ptr {
        v = v.Elem()
    }
    t := v.Type()

    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        if !field.IsExported() {
            continue
        }

        // Use env tag name as key, falling back to field name
        key := field.Tag.Get("env")
        if key == "" {
            key = field.Name
        }

        if field.Tag.Get("redact") == "true" {
            val := v.Field(i)
            if val.Kind() == reflect.String && val.String() != "" {
                result[key] = "[REDACTED]"
            } else {
                result[key] = "" // Empty stays empty
            }
        } else {
            result[key] = v.Field(i).Interface()
        }
    }
    return result
}
```

### 2.2 — Add `redact:"true"` Tags to Sensitive Fields (FR-022)

**MODIFY** `ws/internal/shared/platform/server_config.go`:

Add `redact:"true"` to:
- `KafkaSASLPassword` (~line 79)
- `ValkeyPassword` (~line 208)
- `NATSToken` (~line 271)
- `NATSPassword` (~line 273)
- `NATSJetStreamToken` (~line 291)
- `NATSJetStreamPassword` (~line 293)

```go
// Example:
KafkaSASLPassword string `env:"KAFKA_SASL_PASSWORD" redact:"true"`
```

Note: TLS CA paths and usernames are NOT redacted — only passwords/tokens/secrets.

**MODIFY** `ws/internal/shared/platform/provisioning_config.go`:

Add `redact:"true"` to:
- `AdminToken`
- `DatabaseURL` (may contain credentials in connection string)

**MODIFY** `ws/internal/shared/platform/gateway_config.go`:

No sensitive fields to redact (gateway has no passwords/tokens in its own config).

### 2.3 — Wire `/config` Endpoint into All Three Binaries (FR-022)

**MODIFY** `ws/internal/server/orchestration/loadbalancer.go` (~line 115):
```go
mux.HandleFunc("/ws", lb.handleWebSocket)
mux.HandleFunc("/health", lb.handleHealth)
mux.HandleFunc("/version", version.Handler("ws-server"))
mux.HandleFunc("/config", platform.ConfigHandler(lb.cfg))  // NEW
mux.HandleFunc("/metrics", metrics.HandleMetrics)
```

Requires passing the config to LoadBalancer (or accessing it via existing field).

**MODIFY** `ws/internal/gateway/gateway.go` (~line 438):
```go
mux.HandleFunc("/ws", gw.HandleWebSocket)
mux.HandleFunc("/health", gw.HandleHealth)
mux.HandleFunc("/version", version.Handler("gateway"))
mux.HandleFunc("/config", platform.ConfigHandler(gw.config))  // NEW
mux.HandleFunc("/metrics", HandleMetrics)
```

**MODIFY** `ws/internal/provisioning/api/router.go` (~line 68):
```go
r.Get("/health", h.Health)
r.Get("/ready", h.Ready)
r.Get("/version", version.Handler("provisioning"))
r.Get("/config", platform.ConfigHandler(cfg.Config))  // NEW — pass provisioning config
r.Get("/metrics", h.Metrics)
```

### 2.4 — Docker Compose Build Args for Version Injection (FR-023)

**MODIFY** `docker-compose.yml` (created in Phase 4):

Add build args to inject commit hash:
```yaml
services:
  ws-server:
    build:
      context: ws
      dockerfile: build/server/Dockerfile
      args:
        COMMIT_HASH: "${COMMIT_HASH:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
```

Same for ws-gateway and provisioning. Requires Dockerfile ARG → ldflags wiring (already exists in current Dockerfiles).

### 2.5 — Tests (FR-022)

**CREATE** `ws/internal/shared/platform/config_handler_test.go`:

```go
func TestRedactConfig_RedactsSensitiveFields(t *testing.T) {
    // Struct with redact:"true" on password field → "[REDACTED]"
}

func TestRedactConfig_PreservesNonSensitiveFields(t *testing.T) {
    // Struct without redact tag → value shown
}

func TestRedactConfig_EmptySecretStaysEmpty(t *testing.T) {
    // Empty password with redact:"true" → "" (not "[REDACTED]")
}

func TestConfigHandler_ReturnsJSON(t *testing.T) {
    // HTTP test: handler returns 200 with JSON content-type
}
```

### Phase 2 Verification
```bash
cd ws && go vet ./...
cd ws && go test ./internal/shared/platform/...
curl -s http://localhost:3005/config  # ws-server (redacted)
curl -s http://localhost:8080/config  # provisioning (redacted)
curl -s http://localhost:3000/config  # gateway (redacted)
```

---

## Phase 3: Helm Values & Templates Cleanup

**Goal**: Reduce subchart values.yaml to Kubernetes-required fields. Rewrite templates to only inject non-default env vars.

### 3.1 — Rewrite ws-server/values.yaml (FR-001, FR-002, FR-005a)

**File**: `deployments/helm/sukko/charts/ws-server/values.yaml`

Strip from 292 → ~85 lines. Keep ONLY:

```yaml
# K8s required
enabled, replicaCount, image, resources, probes, service
podSecurityContext, securityContext, autoscaling, networkPolicy
podAnnotations, nodeSelector, tolerations, affinity

# Deployment identity (always injected — drives Kafka topic namespace, consumer groups)
# environment: local  # any string; Sukko uses: local | dev | stg | prod

# Mode selection (commented — Go defaults apply when unset)
# messageBackend: direct  # direct | kafka | nats
# broadcast:
#   type: nats  # nats | valkey

# K8s-specific computed field
connectionLimits:
  clusterMaxConnections: 100000

# Infrastructure addresses (commented — auto-wired from release name)
# kafka: { brokers: "", sasl: { enabled: false }, tls: { enabled: false } }
# nats: { urls: "", tls: { enabled: false } }
# jetstream: { urls: "", tls: { enabled: false } }
# valkey: { addrs: "", tls: { enabled: false } }

# Escape hatch: override any Go envDefault
extraEnv: {}
```

Provide minimal defaults for nested structures templates reference:
```yaml
kafka:
  sasl: { enabled: false }
  tls: { enabled: false }
nats:
  tls: { enabled: false }
jetstream:
  tls: { enabled: false }
valkey:
  tls: { enabled: false }
broadcast: {}
config: {}
```

### 3.2 — Rewrite ws-server ConfigMap Template

**File**: `deployments/helm/sukko/charts/ws-server/templates/configmap.yaml`

Strip from 69 → ~20 lines. Only emit:

```yaml
data:
  # Computed values (K8s-specific — no Go default possible)
  WS_MAX_CONNECTIONS: "{{ div ... }}"
  WS_MAX_GOROUTINES: "{{ add (mul ...) 5000 }}"
  # Developer overrides via extraEnv
  {{- range $key, $val := .Values.extraEnv }}
  {{ $key }}: {{ $val | quote }}
  {{- end }}
```

Remove ALL lines that set env vars matching Go defaults.

### 3.3 — Rewrite ws-server Deployment Template

**File**: `deployments/helm/sukko/charts/ws-server/templates/deployment.yaml`

Reduce env section from ~200 to ~80 lines. Keep only:

1. **Mode selection** (only injected when explicitly set in values — templates use `{{ .Values.messageBackend | default "direct" }}` for conditional logic without injecting the env var when unset):
   - `MESSAGE_BACKEND`, `BROADCAST_TYPE`

1b. **Deployment identity** (always injected — ENVIRONMENT drives Kafka topic namespace, consumer group naming, and safety guards; Go default "local" is only correct for local dev, not K8s):
   - `ENVIRONMENT`: `value: "{{ .Values.environment | default "local" }}"` — override files set per environment (e.g., `environment: dev`)

2. **Auto-wired addresses** (from `{{ .Release.Name }}`):
   - `NATS_URLS` (always — broadcast default is nats)
   - `PROVISIONING_GRPC_ADDR`
   - `KAFKA_BROKERS` (conditional: messageBackend=kafka)
   - `NATS_JETSTREAM_URLS` (conditional: messageBackend=nats)
   - `VALKEY_ADDRS` (conditional: broadcast.type=valkey)

3. **Conditional security** (only when enabled):
   - Kafka SASL/TLS blocks
   - NATS auth/TLS blocks
   - JetStream auth/TLS blocks

4. **Remove entirely** (Go defaults handle these):
   - All alerting env vars
   - All audit env vars
   - `DEFAULT_TENANT_ID`, `KAFKA_TOPIC_NAMESPACE_OVERRIDE`
   - All rate limit, timeout, ping/pong, CPU threshold vars
   - `TOPIC_REFRESH_INTERVAL`, `KAFKA_CONSUMER_ENABLED`, `KAFKA_DEFAULT_*`

These can still be overridden by developers via the `extraEnv` map in ConfigMap.

### 3.4 — Rewrite ws-gateway/values.yaml

**File**: `deployments/helm/sukko/charts/ws-gateway/values.yaml`

Strip from 154 → ~55 lines. Keep ONLY:

```yaml
# K8s required
enabled, replicaCount, image, resources, probes, service
podSecurityContext, securityContext, autoscaling
podAnnotations, nodeSelector, tolerations, affinity

# Deployment identity
# environment: local  # any string; Sukko uses: local | dev | stg | prod

# Feature flags (commented — Go defaults apply)
# config:
#   authEnabled: false       # default: false (Go, temporary — FR-018a)
#   multiIssuerOIDCEnabled: false
#   perTenantChannelRulesEnabled: false

# Channel permission patterns (K8s-specific — no Go default)
permissions:
  public: ["*.trade", "*.liquidity", "*.metadata"]
  userScoped: ["balances.{principal}", "notifications.{principal}"]
  groupScoped: ["community.{group_id}", "social.{group_id}"]

# Rate limiting (commented — Go defaults apply)
# rateLimit: { enabled: true, burst: 100, rate: 10.0 }

# Escape hatch
extraEnv: {}
```

### 3.5 — Update ws-gateway Templates

**File**: `deployments/helm/sukko/charts/ws-gateway/templates/deployment.yaml`

Same pattern as ws-server:
- Only set auto-wired addresses (`GATEWAY_BACKEND_URL`, `PROVISIONING_GRPC_ADDR`)
- Only set mode flags if explicitly provided (`AUTH_ENABLED`, feature flags)
- `extraEnv` for developer overrides
- Remove all fields that match Go defaults

### 3.6 — Rewrite provisioning/values.yaml

**File**: `deployments/helm/sukko/charts/provisioning/values.yaml`

Strip from 187 → ~65 lines. Keep ONLY:

```yaml
# K8s required
enabled, replicaCount, image, resources, probes, service
podSecurityContext, securityContext, persistence, ingress
podAnnotations, nodeSelector, tolerations, affinity

# Deployment identity
# environment: local  # any string; Sukko uses: local | dev | stg | prod

# PostgreSQL (commented — SQLite by default)
# externalDatabase:
#   url: ""
#   existingSecret: ""

# Escape hatch
extraEnv: {}
```

Remove: provisioningMode, configFilePath, configFileConfigMap (FR-007a), all config.* fields that match Go defaults.

### 3.7 — Update provisioning Templates

**File**: `deployments/helm/sukko/charts/provisioning/templates/deployment.yaml`

- Remove `PROVISIONING_MODE` and `PROVISIONING_CONFIG_PATH` injection
- Remove config file volume mount logic
- Only set: `DATABASE_DRIVER` (auto-derived from externalDatabase presence), `DATABASE_URL` (from secret), `ENVIRONMENT` (always injected: `{{ .Values.environment | default "local" }}`)
- `extraEnv` for developer overrides (inline in deployment template — provisioning has no configmap template)

### 3.8 — Rewrite Parent values.yaml with Decision Guide (FR-003, FR-017)

**File**: `deployments/helm/sukko/values.yaml`

Strip from 568 → ~180 lines. Structure:

```yaml
# =============================================================================
# DEPLOYMENT MODE GUIDE
# =============================================================================
# Zero-config (default): helm install sukko ./deployments/helm/sukko
#   → direct messages, NATS broadcast, SQLite provisioning
#
# Local dev (Docker Compose): docker compose up
#   → same defaults, no Helm/K8s needed
#
# Add persistence (Kafka):
#   ws-server:
#     messageBackend: kafka
#   redpanda:
#     enabled: true
#
# Add persistence (NATS JetStream):
#   ws-server:
#     messageBackend: nats
#   nats:
#     jetstream:
#       enabled: true
#
# Production database:
#   provisioning:
#     externalDatabase:
#       existingSecret: "my-pg-secret"
#   postgresql:
#     enabled: true  # OR use external
#
# Broadcast bus override (Valkey):
#   ws-server:
#     broadcast:
#       type: valkey
#   valkey:
#     enabled: true
#
# See examples/helm/ for complete, copy-paste-ready values files.
# =============================================================================

global:
  namespace: sukko
  imageRegistry: ""
  labels:
    app.kubernetes.io/part-of: sukko
  postgresql:
    enabled: false

# Services (minimal — Go defaults for all config)
ws-gateway:
  enabled: true
ws-server:
  enabled: true
provisioning:
  enabled: true    # Changed from false → true for zero-config

# Infrastructure subcharts
nats:
  enabled: true     # Required for broadcast (default mode)
  jetstream:
    enabled: false  # Enable when messageBackend=nats
redpanda:
  enabled: false    # Enable when messageBackend=kafka
valkey:
  enabled: false    # Enable when broadcast.type=valkey
postgresql:
  enabled: false    # Enable when DATABASE_DRIVER=postgres
monitoring:
  enabled: true
```

Remove ALL subchart config overrides from parent (FR-003).

### 3.9 — Add Pre-Install Validation Hook Templates (FR-015)

**CREATE** `deployments/helm/sukko/charts/ws-server/templates/validate-config-job.yaml`:

```yaml
{{- if .Values.enabled }}
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "ws-server.fullname" . }}-validate-config
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-5"
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: validate
          image: "{{ image }}"
          command: ["./sukko", "--validate-config"]
          envFrom:
            - configMapRef:
                name: {{ include "ws-server.fullname" . }}-config
          env:
            # Same env vars as deployment (auto-wired addresses, mode selection)
            {{ ... }}
  backoffLimit: 0
{{- end }}
```

Same pattern for ws-gateway and provisioning.

### Phase 3 Verification
```bash
helm lint deployments/helm/sukko/charts/ws-server
helm lint deployments/helm/sukko/charts/ws-gateway
helm lint deployments/helm/sukko/charts/provisioning
helm lint deployments/helm/sukko
helm template sukko deployments/helm/sukko  # Verify rendered manifests
```

---

## Phase 4: Docker Compose & Examples

**Goal**: `docker compose up` → working WebSocket in 30 seconds. Example values files for each deployment mode.

### 4.1 — Create docker-compose.yml (FR-018, FR-019, FR-020)

**CREATE** `docker-compose.yml` at repo root:

After Phase 1 Go default alignment, Docker Compose only needs inter-service wiring env vars. Go defaults handle everything else:
- `BROADCAST_TYPE=nats` → now Go default, **not needed**
- `ENVIRONMENT=local` → now Go default, **not needed**
- `AUTH_ENABLED=false` → now Go default (FR-018a), **not needed**
- `DATABASE_DRIVER=sqlite` → already Go default, **not needed**

```yaml
services:
  nats:
    image: nats:2.10-alpine
    ports: ["4222:4222", "8222:8222"]
    healthcheck:
      test: ["CMD", "nats-server", "--signal", "ldm=localhost:8222"]

  provisioning:
    build:
      context: ws
      dockerfile: build/provisioning/Dockerfile
      args:
        COMMIT_HASH: "${COMMIT_HASH:-unknown}"
    ports: ["8080:8080", "9090:9090"]
    environment:
      # Inter-service wiring only — all other config uses Go envDefaults
      # (DATABASE_DRIVER=sqlite, AUTO_MIGRATE=true, etc. are Go defaults)
    volumes: ["provisioning-data:/data"]
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]

  ws-server:
    build:
      context: ws
      dockerfile: build/server/Dockerfile
      args:
        COMMIT_HASH: "${COMMIT_HASH:-unknown}"
    ports: ["3001:3001"]
    environment:
      # Inter-service wiring — Go can't default to Docker Compose service names
      NATS_URLS: "nats://nats:4222"
      PROVISIONING_GRPC_ADDR: "provisioning:9090"
    depends_on:
      nats: { condition: service_healthy }
      provisioning: { condition: service_healthy }

  ws-gateway:
    build:
      context: ws
      dockerfile: build/gateway/Dockerfile
      args:
        COMMIT_HASH: "${COMMIT_HASH:-unknown}"
    ports: ["3000:3000"]
    environment:
      # Inter-service wiring only
      GATEWAY_BACKEND_URL: "ws://ws-server:3001/ws"
      PROVISIONING_GRPC_ADDR: "provisioning:9090"
    depends_on:
      ws-server: { condition: service_started }

volumes:
  provisioning-data:

# =============================================================================
# MODE OVERRIDES — Uncomment to switch modes
# =============================================================================
# Kafka/Redpanda mode:
#   1. Uncomment the redpanda service below
#   2. Add to ws-server environment: MESSAGE_BACKEND=kafka
#
# redpanda:
#   image: redpandadata/redpanda:v23.3.5
#   command: ["redpanda", "start", "--smp=1", "--memory=512M", ...]
#   ports: ["9092:9092"]
#
# PostgreSQL mode:
#   1. Uncomment the postgres service below
#   2. Add to provisioning environment: DATABASE_DRIVER=postgres, DATABASE_URL=...
#
# postgres:
#   image: postgres:16-alpine
#   environment:
#     POSTGRES_DB: odin_provisioning
#     POSTGRES_USER: sukko
#     POSTGRES_PASSWORD: sukko
#   ports: ["5432:5432"]
#
# NATS JetStream mode:
#   1. Add "-js" flag to nats command
#   2. Add to ws-server environment:
#      MESSAGE_BACKEND=nats
#      NATS_JETSTREAM_URLS=nats://nats:4222
#
# Valkey broadcast mode:
#   1. Uncomment the valkey service below
#   2. Add to ws-server environment:
#      BROADCAST_TYPE=valkey
#      VALKEY_ADDRS=valkey:6379
#
# valkey:
#   image: valkey/valkey:7.2-alpine
#   ports: ["6379:6379"]
#
# Auth mode:
#   Add to ws-gateway environment: AUTH_ENABLED=true
#   Then provision tenants via provisioning API
```

### 4.2 — Create Example Helm Values (FR-011, FR-012)

**CREATE** `examples/helm/values-minimal.yaml`:
```yaml
# Zero-config: direct messages + NATS broadcast + SQLite
# This is what you get with: helm install sukko ./deployments/helm/sukko
# (No overrides needed — shown here for documentation)
```

**CREATE** `examples/helm/values-kafka.yaml`:
```yaml
# Kafka/Redpanda mode: persistent messages with offset-based replay
ws-server:
  messageBackend: kafka
redpanda:
  enabled: true
```
With comments explaining each field, when to set broker addresses for external Kafka, SASL/TLS options.

**CREATE** `examples/helm/values-nats-jetstream.yaml`:
```yaml
# NATS JetStream mode: lightweight persistent streams
ws-server:
  messageBackend: nats
nats:
  jetstream:
    enabled: true
```
With comments explaining JetStream vs broadcast NATS, external vs in-cluster.

**CREATE** `examples/helm/values-production.yaml`:
```yaml
# Production: PostgreSQL + Kafka + external NATS + TLS
ws-server:
  replicaCount: 3
  messageBackend: kafka
  kafka: { brokers, sasl, tls }
  resources, autoscaling
ws-gateway:
  replicaCount: 3
  resources, autoscaling
  extraEnv:
    AUTH_ENABLED: "true"
provisioning:
  replicaCount: 2
  externalDatabase: { existingSecret }
nats:
  enabled: false  # External managed NATS
redpanda:
  enabled: false  # External managed Kafka
postgresql:
  enabled: false  # External managed PostgreSQL
```

### 4.3 — Add CI Validation for Example Files (FR-013)

Spec Out of Scope carves out an exception: "CI/CD pipeline changes (beyond adding `helm lint` for example files)" — meaning `helm lint` for examples IS in scope. Add a Taskfile target that validates all example values files:

**MODIFY** `taskfiles/k8s.yml` (or appropriate Taskfile):

Add a `helm:lint:examples` target:
```yaml
helm:lint:examples:
  desc: Validate example Helm values files
  cmds:
    - for: [minimal, kafka, nats-jetstream, production]
      cmd: helm lint deployments/helm/sukko -f examples/helm/values-{{.ITEM}}.yaml
    - for: [minimal, kafka, nats-jetstream, production]
      cmd: helm template sukko deployments/helm/sukko -f examples/helm/values-{{.ITEM}}.yaml > /dev/null
```

This target can be added to CI and run locally during development.

### Phase 4 Verification
```bash
docker compose config  # Validate Docker Compose syntax
docker compose build   # Verify Dockerfiles work
for f in examples/helm/values-*.yaml; do
  helm lint deployments/helm/sukko -f "$f"
done
helm template sukko deployments/helm/sukko -f examples/helm/values-kafka.yaml
```

---

## Phase 5: Sukko Internal Deployment Update

**Goal**: Update Sukko's environment overrides to work with new Helm structure. Create migration checklist.

### 5.1 — Rewrite Environment Overrides (FR-004, FR-021)

**MODIFY** `deployments/helm/sukko/values/standard/dev.yaml`:
Strip from 326 → ~60 lines. Keep ONLY:
- global (namespace, imageRegistry)
- Service replicas, image tags, pullPolicy
- Deployment identity: `environment: dev` on all three services (drives Kafka topic namespace, consumer groups)
- Mode selections (messageBackend: kafka, broadcast.type: nats)
- Infrastructure enables (redpanda, nats, postgresql)
- Resources, tolerations (Spot VMs)
- Service types
- kafkaTopicNamespaceOverride: prod (via extraEnv)

Remove ALL config.* fields that now match Go defaults.

**MODIFY** `deployments/helm/sukko/values/standard/stg.yaml` (same pattern)
**MODIFY** `deployments/helm/sukko/values/standard/prod.yaml` (same pattern, if exists)

### 5.2 — Update Taskfile Targets (FR-021)

**MODIFY** `taskfiles/k8s.yml` and `taskfiles/local.yml`:
- Remove references to `provisioningMode`
- Ensure `helm upgrade` commands work with new values structure
- Update any `--set` flags that reference removed fields

### 5.3 — Create Migration Checklist (NFR-004)

**CREATE** `specs/feat/helm-minimal-config/migration-checklist.md`:

Document every default that changed:
| Change | Old Default | New Default | Migration Action |
|--------|------------|------------|-----------------|
| BROADCAST_TYPE | valkey (Go) | nats (Go) | Add `broadcast.type: valkey` to override if using Valkey |
| WS_MAX_BROADCAST_RATE | 20 (Go) | 25 (Go) | No action — Helm already set 25 |
| CONN_RATE_LIMIT_IP_BURST | 10 (Go) | 100 (Go) | No action — Helm already set 100 |
| CONN_RATE_LIMIT_IP_RATE | 1.0 (Go) | 100.0 (Go) | No action — Helm already set 100 |
| ENVIRONMENT | development (Go) | local (Go) | No action — Helm already overrides per env |
| AUTH_ENABLED | true (Go) | false (Go) | Temporary — add `AUTH_ENABLED: "true"` if auth needed |
| PROVISIONING_MODE | Exists | Removed | Remove from values if set; config mode no longer available |
| provisioning.enabled | false (parent) | true (parent) | No action for deployments that already set it |
| CPU lower thresholds | Set in Helm | Auto-computed in Go | Remove from values; Go computes upper-10 |
| Alerting/Audit fields | In values.yaml | Removed | Use extraEnv if custom values needed |
| All config.* fields | In values.yaml | Removed | Use extraEnv if custom values needed |

### Phase 5 Verification
```bash
# Verify existing dev deployment renders correctly
helm template sukko deployments/helm/sukko \
  -f deployments/helm/sukko/values/standard/dev.yaml \
  --debug 2>&1 | head -50

helm lint deployments/helm/sukko -f deployments/helm/sukko/values/standard/dev.yaml
```

---

## Phase 6: Final Verification

```bash
# Go
cd ws && go vet ./...
cd ws && go test ./...

# Helm
helm lint deployments/helm/sukko
helm lint deployments/helm/sukko/charts/ws-server
helm lint deployments/helm/sukko/charts/ws-gateway
helm lint deployments/helm/sukko/charts/provisioning

# Examples
for f in examples/helm/values-*.yaml; do
  helm lint deployments/helm/sukko -f "$f"
done

# Docker Compose
docker compose config

# Validate-config
cd ws && go build -o /tmp/ws-server ./cmd/server && /tmp/ws-server --validate-config
cd ws && go build -o /tmp/ws-gateway ./cmd/gateway && /tmp/ws-gateway --validate-config
cd ws && go build -o /tmp/provisioning ./cmd/provisioning && /tmp/provisioning --validate-config

# Introspection (each service queried individually)
curl -s http://localhost:3005/config | jq .  # ws-server (redacted)
curl -s http://localhost:8080/config | jq .  # provisioning (redacted)
curl -s http://localhost:3000/config | jq .  # gateway (redacted)
```

---

## File Change Summary

### CREATE (11 files)
| File | FR |
|------|-----|
| `ws/internal/shared/platform/config_handler.go` | FR-022 |
| `ws/internal/shared/platform/config_handler_test.go` | FR-022 |
| `docker-compose.yml` | FR-018 |
| `examples/helm/values-minimal.yaml` | FR-011 |
| `examples/helm/values-kafka.yaml` | FR-011 |
| `examples/helm/values-nats-jetstream.yaml` | FR-011 |
| `examples/helm/values-production.yaml` | FR-011 |
| `deployments/helm/sukko/charts/ws-server/templates/validate-config-job.yaml` | FR-015 |
| `deployments/helm/sukko/charts/ws-gateway/templates/validate-config-job.yaml` | FR-015 |
| `deployments/helm/sukko/charts/provisioning/templates/validate-config-job.yaml` | FR-015 |
| `specs/feat/helm-minimal-config/migration-checklist.md` | NFR-004 |

### MODIFY (27 files)
| File | FR |
|------|-----|
| `ws/internal/shared/platform/server_config.go` | FR-005, FR-005b, FR-009, FR-022 (redact tags) |
| `ws/internal/server/broadcast/factory.go` | FR-005 (remove `case ""` fallback) |
| `ws/internal/shared/platform/server_config_test.go` | FR-005b, FR-009 |
| `ws/internal/shared/platform/provisioning_config.go` | FR-007a, FR-022 (redact tags) |
| `ws/internal/shared/platform/provisioning_config_test.go` | FR-007a |
| `ws/internal/shared/platform/gateway_config.go` | FR-018a (AUTH_ENABLED) |
| `ws/internal/server/orchestration/loadbalancer.go` | FR-022 (wire /config) |
| `ws/internal/gateway/gateway.go` | FR-022 (wire /config) |
| `ws/internal/provisioning/api/router.go` | FR-022 (wire /config) |
| `ws/cmd/server/main.go` | FR-014 |
| `ws/cmd/gateway/main.go` | FR-014 |
| `ws/cmd/provisioning/main.go` | FR-007a, FR-014 |
| `ws/cmd/cli/commands/config.go` | FR-007a |
| `deployments/helm/sukko/values.yaml` | FR-003, FR-017 |
| `deployments/helm/sukko/charts/ws-server/values.yaml` | FR-001, FR-002 |
| `deployments/helm/sukko/charts/ws-server/templates/configmap.yaml` | FR-001 |
| `deployments/helm/sukko/charts/ws-server/templates/deployment.yaml` | FR-001, FR-010 |
| `deployments/helm/sukko/charts/ws-gateway/values.yaml` | FR-001, FR-002 |
| `deployments/helm/sukko/charts/ws-gateway/templates/configmap.yaml` | FR-001 |
| `deployments/helm/sukko/charts/ws-gateway/templates/deployment.yaml` | FR-001 |
| `deployments/helm/sukko/charts/provisioning/values.yaml` | FR-001, FR-007a |
| `deployments/helm/sukko/charts/provisioning/templates/deployment.yaml` | FR-007a |
| `deployments/helm/sukko/values/standard/dev.yaml` | FR-004, FR-021 |
| `deployments/helm/sukko/values/standard/stg.yaml` | FR-004, FR-021 |
| `deployments/helm/sukko/values/standard/prod.yaml` | FR-004, FR-021 |
| `taskfiles/k8s.yml` | FR-021 |
| `taskfiles/local.yml` | FR-021 |

### DELETE (9+ files)
| File | FR |
|------|-----|
| `ws/internal/provisioning/configstore/loader.go` | FR-007a |
| `ws/internal/provisioning/configstore/parser.go` | FR-007a |
| `ws/internal/provisioning/configstore/validator.go` | FR-007a |
| `ws/internal/provisioning/configstore/types.go` | FR-007a |
| `ws/internal/provisioning/configstore/stores.go` | FR-007a |
| `ws/internal/provisioning/configstore/loader_test.go` | FR-007a |
| `ws/internal/provisioning/configstore/parser_test.go` | FR-007a |
| `ws/internal/provisioning/configstore/validator_test.go` | FR-007a |
| `ws/internal/provisioning/configstore/stores_test.go` | FR-007a |

**Totals**: 11 create + 27 modify + 9 delete = **47 file changes**

---

## FR Coverage Matrix

| FR | Phase | Description |
|----|-------|-------------|
| FR-001, FR-002 | 3 | Subchart values.yaml cleanup |
| FR-003 | 3 | Parent values.yaml cleanup |
| FR-003a | 3 | Inter-service auto-wiring |
| FR-004 | 5 | Environment override cleanup |
| FR-005, FR-005a, FR-005b | 1 | Go default alignment |
| FR-005c | 3 | Infrastructure subchart defaults |
| FR-005d | 3, 4 | Topology flexibility |
| FR-005e | 3 | NATS JetStream toggle |
| FR-006, FR-006a | 1, 3 | MESSAGE_BACKEND defaults |
| FR-007, FR-007a | 1 | DATABASE_DRIVER defaults, config mode removal |
| FR-008 | 3 | Env var coherence |
| FR-009 | 1 | Hysteresis auto-compute |
| FR-010 | 3 | Conditional mode templates |
| FR-011 | 4 | Example values files |
| FR-011a | — | **DEFERRED** (GKE examples) |
| FR-012 | 4 | Example inline comments |
| FR-013 | 4 | CI validation commands |
| FR-014, FR-015, FR-016 | 1, 3 | Config validation |
| FR-017 | 3 | Decision guide |
| FR-018, FR-018a, FR-018b | 1, 4 | Docker Compose + AUTH_ENABLED fix |
| FR-019, FR-020 | 4 | Docker Compose ports + mode overrides |
| FR-021 | 5 | Internal deployment update |
| FR-022 | 2 | /config endpoint + redaction |
| FR-023 | 2, 4 | /version preservation + build args |
| FR-024 | — | **REMOVED** (no gateway aggregation) |
