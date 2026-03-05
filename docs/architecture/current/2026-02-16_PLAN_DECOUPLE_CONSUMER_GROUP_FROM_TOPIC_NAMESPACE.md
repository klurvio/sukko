# Plan: Robust Topic Namespace Override with Cross-Env Safety

## Context

The Sukko API (external) always publishes to `prod.sukko.{category}` — it conforms to the topic naming pattern `{env}.{tenant}.{category}` but its env is always `prod`. Non-prod environments need to consume this data via a namespace override. Four problems exist:

1. **Naming**: `kafkaTopicNamespace` doesn't signal it's an override — easy to misconfigure
2. **Provisioning mismatch**: ws-server and provisioning have separate, independently configured namespace values — they can silently diverge
3. **No prod guard**: Nothing prevents setting the override in prod
4. **Consumer group naming**: Consumer group uses `sukko-shared-{namespace}` / `sukko-{tenant}-{namespace}` — the format is inconsistent and doesn't start with environment

### Architecture Context

- **Each environment has its own Redpanda cluster** — cross-env data contamination is impossible at the infrastructure level
- **Consumer groups are cluster-scoped** — `dev-shared-consumer` on dev Redpanda has zero relation to `prod-shared-consumer` on prod Redpanda
- **Environment prefix in consumer groups is for self-documentation**, not a safety mechanism
- **Tenant isolation for shared consumers** happens at the subscription/routing layer (client subscriptions filter by both tenant AND category), not at the Kafka consumer group level
- **Shared vs dedicated** is determined by a `type` field in the provisioning database per tenant

### Message Routing (Namespace Override Safety)

The namespace override does NOT affect message routing. The namespace only appears in the **Kafka topic name**, never in the routing key:

| Context | Format | Example |
|---|---|---|
| Kafka topic | `{namespace}.{tenant}.{category}` | `prod.sukko.trade` |
| Kafka key (= routing key) | `{tenant}.{identifier}.{category}` | `sukko.BTC.trade` |
| Client subscription | `{tenant}.{identifier}.{category}` | `sukko.BTC.trade` |

**Flow**: Consumer reads from `prod.sukko.trade` → extracts Kafka key `sukko.BTC.trade` → broadcasts to clients subscribed to `sukko.BTC.trade`. The topic name (and its namespace prefix) is discarded for routing. Whether the source topic is `prod.sukko.trade` or `dev.sukko.trade`, a message with key `sukko.BTC.trade` reaches the same subscribers.

The tenant prefix in the Kafka key (`sukko.`) serves as both self-documentation and a security boundary. The ws-gateway already enforces tenant prefix validation on subscribe and publish (always active) — clients can only access channels matching their tenant. The ws-server treats the key as an opaque string for exact-match routing.

**Goal**: Rename with "override", align provisioning, block override in prod, adopt `{env}-{identifier}-consumer` group naming, and remove dead code.

## Consumer Group Naming Convention

### Old Pattern
- Shared: `sukko-shared-{namespace}` (e.g., `sukko-shared-prod`)
- Dedicated: `sukko-{tenant}-{namespace}` (e.g., `sukko-acme-prod`)

### New Pattern: `{env}-{identifier}-consumer`
- Shared: `{env}-shared-consumer` (e.g., `dev-shared-consumer`)
- Dedicated: `{env}-{tenant}-consumer` (e.g., `dev-sukko-consumer`)

**Rationale**:
- Environment first → easy to filter: `rpk group list | grep ^dev-`
- `shared` vs tenant name distinguishes the type
- `-consumer` suffix identifies the service that owns the group
- No tenant name baked into shared group (shared pool reads ALL shared tenants' topics)
- Tenant isolation for shared consumers is enforced downstream at the subscription/routing layer

**Edge case**: Tenant ID `"shared"` would collide → validate in provisioning that tenant IDs can't be `"shared"`.

| Type | Pattern | Dev Example | Prod Example |
|---|---|---|---|
| Shared | `{env}-shared-consumer` | `dev-shared-consumer` | `prod-shared-consumer` |
| Dedicated | `{env}-{tenant}-consumer` | `dev-sukko-consumer` | `prod-sukko-consumer` |

## Changes

### Part 1: Rename `kafkaTopicNamespace` → `kafkaTopicNamespaceOverride`

**`ws/internal/shared/platform/server_config.go`** (line 174):
```go
// Before:
KafkaTopicNamespace string `env:"KAFKA_TOPIC_NAMESPACE" envDefault:""`

// After:
KafkaTopicNamespaceOverride string `env:"KAFKA_TOPIC_NAMESPACE_OVERRIDE" envDefault:""`
```

Update docstring to emphasize this is an OVERRIDE (only for dev/stg).

Update `Print()` (lines 514-515):
```go
// Before:
if c.KafkaTopicNamespace != "" {
    fmt.Printf("Topic Namespace: %s (override)\n", c.KafkaTopicNamespace)
}
// After:
if c.KafkaTopicNamespaceOverride != "" {
    fmt.Printf("Topic Namespace: %s (override)\n", c.KafkaTopicNamespaceOverride)
}
```

Update `LogConfig()` (line 604):
```go
// Before:
Str("kafka_topic_namespace", c.KafkaTopicNamespace).
// After:
Str("kafka_topic_namespace_override", c.KafkaTopicNamespaceOverride).
```

**`ws/cmd/server/main.go`** (lines 75-83):
```go
// Before:
topicNamespace := cfg.KafkaTopicNamespace
if topicNamespace == "" {
    topicNamespace = kafka.NormalizeEnv(cfg.Environment)
} else {
    topicNamespace = kafka.NormalizeEnv(topicNamespace)
}

// After (shared function eliminates duplication with provisioning):
topicNamespace := kafka.ResolveNamespace(cfg.KafkaTopicNamespaceOverride, cfg.Environment)
```

**`deployments/helm/sukko/charts/ws-server/values.yaml`** (line 41):
```yaml
# Before:
kafkaTopicNamespace: ""

# After:
kafkaTopicNamespaceOverride: ""
```

**`deployments/helm/sukko/charts/ws-server/templates/deployment.yaml`** (lines 75-78):
```yaml
# Before:
- name: KAFKA_TOPIC_NAMESPACE
  value: "{{ .Values.config.kafkaTopicNamespace | default "" }}"

# After:
- name: KAFKA_TOPIC_NAMESPACE_OVERRIDE
  value: "{{ .Values.config.kafkaTopicNamespaceOverride | default "" }}"
```

### Part 2: Align provisioning with ws-server

Provisioning currently uses `topicNamespace: "prod"` as chart default (overridden to `"dev"` in dev.yaml). Change it to use the same override pattern as ws-server.

**`ws/internal/shared/platform/provisioning_config.go`**:

Rename field (line 48):
```go
// Before:
TopicNamespace string `env:"KAFKA_TOPIC_NAMESPACE" envDefault:"prod"`
// After:
TopicNamespaceOverride string `env:"KAFKA_TOPIC_NAMESPACE_OVERRIDE" envDefault:""`
```

Update `Validate()` — namespace validation (lines 188-195). Override is optional, so only validate when set:
```go
// Before:
validNS := parseNamespaces(c.ValidNamespaces)
if len(validNS) == 0 {
    return errors.New("VALID_NAMESPACES must contain at least one namespace")
}
if !validNS[c.TopicNamespace] {
    return fmt.Errorf("KAFKA_TOPIC_NAMESPACE must be one of: %s (got: %s)",
        c.ValidNamespaces, c.TopicNamespace)
}

// After:
validNS := parseNamespaces(c.ValidNamespaces)
if len(validNS) == 0 {
    return errors.New("VALID_NAMESPACES must contain at least one namespace")
}
if c.TopicNamespaceOverride != "" && !validNS[c.TopicNamespaceOverride] {
    return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE must be one of: %s (got: %s)",
        c.ValidNamespaces, c.TopicNamespaceOverride)
}
```

Update `Print()` (line 240):
```go
// Before:
fmt.Printf("Namespace:          %s\n", c.TopicNamespace)
// After:
if c.TopicNamespaceOverride != "" {
    fmt.Printf("Namespace Override: %s\n", c.TopicNamespaceOverride)
}
```

Update `LogConfig()` (line 271):
```go
// Before:
Str("topic_namespace", c.TopicNamespace).
// After:
Str("topic_namespace_override", c.TopicNamespaceOverride).
```

**`ws/cmd/provisioning/main.go`** (after config load):
Add namespace resolution using the same shared function as ws-server:
```go
topicNamespace := kafka.ResolveNamespace(cfg.TopicNamespaceOverride, cfg.Environment)
logger.Printf("Topic namespace: %s (environment: %s)", topicNamespace, cfg.Environment)
```
Pass resolved `topicNamespace` to `provisioning.NewService()`.

**`deployments/helm/sukko/charts/provisioning/values.yaml`** (line 53):
```yaml
# Before:
topicNamespace: "prod"

# After:
environment: "local"
kafkaTopicNamespaceOverride: ""
```

**`deployments/helm/sukko/charts/provisioning/templates/deployment.yaml`**:
```yaml
# Before (lines 106-107):
- name: KAFKA_TOPIC_NAMESPACE
  value: "{{ .Values.config.topicNamespace }}"

# After:
- name: KAFKA_TOPIC_NAMESPACE_OVERRIDE
  value: "{{ .Values.config.kafkaTopicNamespaceOverride | default "" }}"

# Before (lines 132-133):
- name: ENVIRONMENT
  value: "{{ .Release.Namespace }}"

# After:
- name: ENVIRONMENT
  value: "{{ .Values.config.environment | default "local" }}"
```

### Part 3: Block override in prod

Add validation in BOTH services' `Validate()` methods. If `ENVIRONMENT=prod` and override is set, fail fast during config load — consistent with the coding guidelines' Configuration Validation pattern (L937).

**`ws/internal/shared/platform/server_config.go`** (in `Validate()`, after ping/pong validation):
```go
// Prod guard: namespace override is only for dev/stg
env := strings.ToLower(strings.TrimSpace(c.Environment))
if env == "prod" && c.KafkaTopicNamespaceOverride != "" {
    return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE is not allowed in production (environment: %s)", c.Environment)
}
```

Add `"strings"` to `server_config.go` imports (not currently imported; `platform` cannot import `kafka` due to circular dependency, so inline normalization is used).

**`ws/internal/shared/platform/provisioning_config.go`** (in `Validate()`, after CORS validation):
```go
// Prod guard: namespace override is only for dev/stg
env := strings.ToLower(strings.TrimSpace(c.Environment))
if env == "prod" && c.TopicNamespaceOverride != "" {
    return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE is not allowed in production (environment: %s)", c.Environment)
}
```
Note: `strings` is already imported in provisioning_config.go.

#### Prod Guard Tests

**`ws/internal/shared/platform/server_config_test.go`** (append):
```go
func TestServerConfig_Validate_ProdOverrideBlocked(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name        string
        environment string
        override    string
        shouldError bool
    }{
        {"prod_with_override", "prod", "dev", true},
        {"prod_no_override", "prod", "", false},
        {"dev_with_override", "dev", "prod", false},
        {"dev_no_override", "dev", "", false},
        {"PROD_uppercase", "PROD", "dev", true},
        {"prod_whitespace", " prod ", "dev", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            cfg := newValidServerConfig()
            cfg.Environment = tt.environment
            cfg.KafkaTopicNamespaceOverride = tt.override
            err := cfg.Validate()
            if tt.shouldError {
                if err == nil {
                    t.Error("Should error on prod override")
                } else if !strings.Contains(err.Error(), "KAFKA_TOPIC_NAMESPACE_OVERRIDE") {
                    t.Errorf("Error should mention KAFKA_TOPIC_NAMESPACE_OVERRIDE: %v", err)
                }
            } else {
                if err != nil {
                    t.Errorf("Should not error: %v", err)
                }
            }
        })
    }
}
```

**`ws/internal/shared/platform/provisioning_config_test.go`** (new file):
```go
package platform

import (
    "strings"
    "testing"
)

func newValidProvisioningConfig() *ProvisioningConfig {
    return &ProvisioningConfig{
        Addr:                    ":8080",
        LogLevel:                "info",
        LogFormat:               "json",
        DatabaseURL:             "postgres://user:pass@localhost:5432/provisioning?sslmode=disable",
        DBMaxOpenConns:          25,
        DBMaxIdleConns:          5,
        DefaultPartitions:       3,
        DefaultRetentionMs:      604800000,
        MaxTopicsPerTenant:      50,
        MaxPartitionsPerTenant:  200,
        DeprovisionGraceDays:    30,
        APIRateLimitPerMinute:   60,
        ValidNamespaces:         "local,dev,stag,prod",
        TopicNamespaceOverride:  "",
        Environment:             "development",
        CORSAllowedOrigins:      []string{"http://localhost:3000"},
        CORSMaxAge:              3600,
    }
}

func TestProvisioningConfig_Validate_ProdOverrideBlocked(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name        string
        environment string
        override    string
        shouldError bool
    }{
        {"prod_with_override", "prod", "dev", true},
        {"prod_no_override", "prod", "", false},
        {"dev_with_override", "dev", "prod", false},
        {"dev_no_override", "dev", "", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            cfg := newValidProvisioningConfig()
            cfg.Environment = tt.environment
            cfg.TopicNamespaceOverride = tt.override
            err := cfg.Validate()
            if tt.shouldError {
                if err == nil {
                    t.Error("Should error on prod override")
                } else if !strings.Contains(err.Error(), "KAFKA_TOPIC_NAMESPACE_OVERRIDE") {
                    t.Errorf("Error should mention KAFKA_TOPIC_NAMESPACE_OVERRIDE: %v", err)
                }
            } else {
                if err != nil {
                    t.Errorf("Should not error: %v", err)
                }
            }
        })
    }
}
```

### Part 4: Consumer group naming — `{env}-{identifier}-consumer`

**`ws/internal/server/orchestration/multitenant_pool.go`**:

Add `Environment string` to `MultiTenantPoolConfig` (after `Namespace` field, line 84):
```go
// Environment is the deployment environment (e.g., "dev", "stg", "prod").
// Used for consumer group naming: {env}-shared-consumer, {env}-{tenant}-consumer.
// If empty, falls back to Namespace for backward compatibility.
Environment string
```

Add fallback in constructor (~line 128):
```go
if config.Environment == "" {
    config.Environment = config.Namespace
}
```

Change consumer group construction (2 lines):
```go
// Before (line 308):
ConsumerGroup: "sukko-shared-" + p.config.Namespace,
// After:
ConsumerGroup: fmt.Sprintf("%s-shared-consumer", p.config.Environment),

// Before (line 401):
ConsumerGroup: fmt.Sprintf("sukko-%s-%s", tenant.TenantID, p.config.Namespace),
// After:
ConsumerGroup: fmt.Sprintf("%s-%s-consumer", p.config.Environment, tenant.TenantID),
```

Update docstring (lines 19-44):
```
// Architecture:
//
//	MultiTenantConsumerPool
//	├── SharedConsumer (explicit topics, 60-second refresh)
//	│   └── Consumer Group: {env}-shared-consumer
//	│   └── Topics: queried from TenantRegistry
//	│
//	└── DedicatedConsumers (per-tenant isolation)
//	    └── Consumer Group: {env}-{tenant_id}-consumer
//	    └── Topics: from provisioning DB for that tenant
```

**`ws/cmd/server/main.go`** (line 219):
```go
Environment: kafka.NormalizeEnv(cfg.Environment),
```

**`ws/internal/server/orchestration/multitenant_pool_test.go`** (line 467):
- Update `TestMultiTenantPool_ConsumerGroupNaming` with new pattern
- Test cases:
  - `{env:"dev", ns:"prod"}` → shared: `dev-shared-consumer`, dedicated: `dev-sukko-consumer`
  - `{env:"prod", ns:"prod"}` → shared: `prod-shared-consumer`, dedicated: `prod-sukko-consumer`
  - `{env:"", ns:"dev"}` → fallback: shared: `dev-shared-consumer` (Environment falls back to Namespace)

### Part 5: Helm environment values

**`deployments/helm/sukko/values/standard/dev.yaml`**:
```yaml
ws-server:
  config:
    environment: dev
    kafkaTopicNamespaceOverride: prod  # Consume from prod.sukko.* (Sukko API data)

provisioning:
  config:
    environment: dev
    kafkaTopicNamespaceOverride: prod  # Create topics in prod namespace
```

**`deployments/helm/sukko/values/standard/stg.yaml`**:
```yaml
ws-server:
  config:
    environment: stg
    kafkaTopicNamespaceOverride: prod  # Consume from prod.sukko.* (Sukko API data)

provisioning:
  config:
    environment: stg
    kafkaTopicNamespaceOverride: prod  # Create topics in prod namespace
```

**`deployments/helm/sukko/values/standard/prod.yaml`**: Verify NO override is set (and it would fail fast if someone added one).

**`deployments/helm/sukko/values/local.yaml`**: Update provisioning's `topicNamespace: local` → `environment: local`.

### Part 6: Dead code cleanup

Remove unused `KAFKA_CONSUMER_GROUP` from all references:

**Go source:**
- `ws/internal/shared/platform/server_config.go`: Remove `ConsumerGroup` field (line 48)
- `ws/internal/shared/platform/server_config.go`: Remove from `Print()` (line 519: `fmt.Printf("Consumer Group:  %s\n", c.ConsumerGroup)`)
- `ws/internal/shared/platform/server_config.go`: Remove from `LogConfig()` (line 607: `Str("consumer_group", c.ConsumerGroup).`)
- `ws/internal/shared/types/types.go`: Remove `ConsumerGroup` field (line 37)
- `ws/cmd/server/main.go`: Remove `ConsumerGroup: cfg.ConsumerGroup` (line 278)

**Tests:**
- `ws/internal/shared/platform/server_config_test.go`: Remove `ConsumerGroup: "test-group"` from `newValidServerConfig()` (line 14)
- `ws/internal/shared/types/types_test.go`: Remove `ConsumerGroup` references from `TestServerConfig_DefaultFields` (lines 67, 77-78)

**Helm:**
- `deployments/helm/sukko/charts/ws-server/values.yaml`: Remove `consumerGroup` (line 147)
- `deployments/helm/sukko/charts/ws-server/templates/deployment.yaml`: Remove `KAFKA_CONSUMER_GROUP` env var (lines 70-71)

### Part 7: Provisioning — only create "trade" category

The provisioning taskfiles currently create 8 categories for the "sukko" tenant:
```
trade, liquidity, metadata, social, community, creation, analytics, balances
```

Only `trade` is actually used by the Sukko API. The other 7 create empty topics in Redpanda.

**`taskfiles/k8s.yml`** (line 249):
```bash
# Before:
-d '{"categories": ["trade", "liquidity", "metadata", "social", "community", "creation", "analytics", "balances"]}'

# After:
-d '{"categories": ["trade"]}'
```

**`taskfiles/local.yml`** (line 169):
```bash
# Before:
-d '{"categories": ["trade", "liquidity", "metadata", "social", "community", "creation", "analytics", "balances"]}'

# After:
-d '{"categories": ["trade"]}'
```

**Post-deploy**: Delete the 7 orphaned topics from Redpanda (covered in Post-Deploy section).

### Part 8: Shared `ResolveNamespace()` + documentation update

Extract namespace resolution logic to a shared function (Coding Guidelines: Shared Code Consolidation, L349).

**`ws/internal/shared/kafka/config.go`**:

Add `ResolveNamespace()` function (after `NormalizeEnv()`):
```go
// ResolveNamespace resolves the effective topic namespace from an override and environment.
// If override is non-empty, it takes precedence (normalized). Otherwise, falls back to
// the normalized environment value.
//
// This is the single source of truth for namespace resolution — used by both ws-server
// and provisioning to eliminate duplicated logic.
//
// Examples:
//   - ResolveNamespace("prod", "dev") → "prod" (override wins)
//   - ResolveNamespace("", "dev") → "dev" (fallback to environment)
//   - ResolveNamespace("", "") → "local" (NormalizeEnv default)
func ResolveNamespace(override, environment string) string {
    if override != "" {
        return NormalizeEnv(override)
    }
    return NormalizeEnv(environment)
}
```

Update header documentation to reference `KAFKA_TOPIC_NAMESPACE_OVERRIDE` and explain override-only-in-dev/stg semantics.

**`ws/internal/shared/kafka/config_test.go`**:

Add `TestResolveNamespace` (after `TestNormalizeEnv`):
```go
func TestResolveNamespace(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name        string
        override    string
        environment string
        expected    string
    }{
        {"override_wins", "prod", "dev", "prod"},
        {"fallback_to_env", "", "dev", "dev"},
        {"both_empty_defaults_to_local", "", "", "local"},
        {"override_normalized", " PROD ", "dev", "prod"},
        {"env_normalized", "", " DEV ", "dev"},
        {"override_empty_string", "", "stg", "stg"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            result := ResolveNamespace(tt.override, tt.environment)
            if result != tt.expected {
                t.Errorf("ResolveNamespace(%q, %q) = %q, want %q",
                    tt.override, tt.environment, result, tt.expected)
            }
        })
    }
}
```

**`ws/internal/shared/platform/server_config.go`**: Update Multi-Tenant Consumer comments (lines 258-264) to reference `{env}-shared-consumer` / `{env}-{tenant}-consumer` for consumer groups.

### Part 9: "shared" tenant ID collision guard (follow-up)

**Not implemented in this PR** — noted for follow-up. Tenant ID `"shared"` would collide with `{env}-shared-consumer` consumer group. The provisioning API's tenant creation endpoint should validate that tenant IDs cannot be `"shared"`. This is a provisioning-layer concern, not a ws-server concern.

**Tracking**: Add validation in `ws/internal/provisioning/service.go` `CreateTenant()` method:
```go
if tenantID == "shared" {
    return fmt.Errorf("tenant ID %q is reserved", tenantID)
}
```

## Post-Deploy: Cleanup old consumer groups and topics

After deploying and verifying `prod.sukko.*` consumption works:
```bash
# Delete old-format consumer groups
rpk group delete sukko-shared-dev sukko-shared-prod

# Delete orphaned dev-prefixed topics (if any exist)
rpk topic delete dev.sukko.trade dev.sukko.liquidity dev.sukko.balances \
    dev.sukko.community dev.sukko.creation dev.sukko.metadata \
    dev.sukko.social dev.sukko.analytics

# Investigate sukko.main.trade (84.9 B/s — unknown origin)
rpk topic describe sukko.main.trade
```

## Key Behavior After Deploy

| Environment | Override | Topics | Consumer Group (Shared) | Consumer Group (Dedicated) |
|---|---|---|---|---|
| dev | `prod` | `prod.sukko.trade` | `dev-shared-consumer` | `dev-sukko-consumer` |
| stg | `prod` | `prod.sukko.trade` | `stg-shared-consumer` | `stg-sukko-consumer` |
| prod | (blocked) | `prod.sukko.trade` | `prod-shared-consumer` | `prod-sukko-consumer` |

## Verification

1. `cd ws && go test ./...` — all tests pass
2. `go vet ./...` — clean
3. `task k8s:deploy ENV=dev`
4. Check logs: `kubectl logs -n sukko-dev -l app.kubernetes.io/name=ws-server --tail=50 | grep "Topic namespace"`
   → `Topic namespace: prod (environment: dev)`
5. Check consumer groups: `rpk group list | grep ^dev-`
   → `dev-shared-consumer`
6. Check Prometheus: `ws_kafka_messages_received_total` shows `topic="prod.sukko.trade"` with `consumer_group="dev-shared-consumer"`
7. Grafana: Redpanda Throughput shows non-zero `Fetched: prod.sukko.trade`

## Coding Guidelines Compliance

| Guideline | Status | Fix |
|---|---|---|
| No Hardcoded Values (L29) | ✅ | All values configurable via env vars |
| Defense in Depth (L44) | ✅ | Prod guard in `Validate()` for both services |
| Observable by Default (L61) | ✅ | Startup log: `Topic namespace: %s (environment: %s)` |
| No Shortcuts (L64) | ✅ | No TODOs, hacks, or magic numbers |
| Configuration Validation (L937) | ✅ | Prod guard in `Validate()`, not scattered in main.go |
| Testing (L995/L1731) | ✅ | Table-driven tests for prod guard + ResolveNamespace |
| Shared Code Consolidation (L349) | ✅ | `kafka.ResolveNamespace()` eliminates duplicate logic |
| Error Handling (L473) | ✅ | `fmt.Errorf` with context in all validation |

## Files Modified (22 files)

| # | File | Change |
|---|---|---|
| 1 | `ws/internal/shared/platform/server_config.go` | Rename field + `Print()`/`LogConfig()`, add prod guard in `Validate()`, update docs, remove dead ConsumerGroup + `Print()`/`LogConfig()` refs |
| 2 | `ws/internal/shared/platform/server_config_test.go` | Add `TestServerConfig_Validate_ProdOverrideBlocked`, remove `ConsumerGroup` from `newValidServerConfig()` |
| 3 | `ws/cmd/server/main.go` | Use `kafka.ResolveNamespace()`, pass Environment to pool, remove dead ConsumerGroup |
| 4 | `ws/internal/server/orchestration/multitenant_pool.go` | Add Environment field, new consumer group pattern `{env}-{id}-consumer` |
| 5 | `ws/internal/server/orchestration/multitenant_pool_test.go` | Cross-env, fallback, and new naming pattern tests |
| 6 | `ws/internal/shared/platform/provisioning_config.go` | Replace TopicNamespace with override pattern + `Validate()`/`Print()`/`LogConfig()`, add prod guard |
| 7 | `ws/internal/shared/platform/provisioning_config_test.go` | New file: `TestProvisioningConfig_Validate_ProdOverrideBlocked` |
| 8 | `ws/cmd/provisioning/main.go` | Use `kafka.ResolveNamespace()` |
| 9 | `ws/internal/shared/kafka/config.go` | Add `ResolveNamespace()`, update documentation |
| 10 | `ws/internal/shared/kafka/config_test.go` | Add `TestResolveNamespace` |
| 11 | `ws/internal/shared/types/types.go` | Remove dead ConsumerGroup field |
| 12 | `ws/internal/shared/types/types_test.go` | Remove ConsumerGroup from `TestServerConfig_DefaultFields` |
| 13 | `ws/internal/shared/kafka/consumer.go` | Fix Kafka key format in comments (`sukko.BTC.trade`) |
| 14 | `deployments/helm/sukko/charts/ws-server/values.yaml` | Rename config, remove dead consumerGroup |
| 15 | `deployments/helm/sukko/charts/ws-server/templates/deployment.yaml` | Update env vars |
| 16 | `deployments/helm/sukko/charts/provisioning/values.yaml` | Add environment, replace topicNamespace |
| 17 | `deployments/helm/sukko/charts/provisioning/templates/deployment.yaml` | Update env vars |
| 18 | `deployments/helm/sukko/values/standard/dev.yaml` | Set override for both services |
| 19 | `deployments/helm/sukko/values/standard/stg.yaml` | Update provisioning config |
| 20 | `deployments/helm/sukko/values/local.yaml` | Update provisioning config |
| 21 | `taskfiles/k8s.yml` | Reduce provisioning categories to `["trade"]` only |
| 22 | `taskfiles/local.yml` | Reduce provisioning categories to `["trade"]` only |
