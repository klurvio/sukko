# Research: Minimal Helm Configuration

**Branch**: feat/helm-minimal-config | **Date**: 2026-03-02

## Current State Audit

### Line Counts
| File | Current Lines | Target |
|------|-------------|--------|
| ws-server/values.yaml | 292 | <100 |
| ws-gateway/values.yaml | 154 | <60 |
| provisioning/values.yaml | 187 | <80 |
| parent values.yaml | 568 | <200 |
| dev.yaml | 326 | <80 |

### Go Default vs Helm Divergences (FR-005b)

These Go `envDefault` values must be updated to match Helm production-intended values:

| Env Var | Go envDefault | Helm Value | Action |
|---------|--------------|------------|--------|
| `BROADCAST_TYPE` | `valkey` | `nats` (parent) | Go → `nats` (FR-005) |
| `WS_MAX_BROADCAST_RATE` | `20` | `25` | Go → `25` |
| `CONN_RATE_LIMIT_IP_BURST` | `10` | `100` | Go → `100` |
| `CONN_RATE_LIMIT_IP_RATE` | `1.0` | `100.0` | Go → `100.0` |
| `ENVIRONMENT` | `development` | `local` (subchart) | Go → `local` |

### Fields to Remove from Helm (Match Go Defaults)

**ws-server** (~50 fields removable):
- config.*: shards(2), basePort(3002), lbAddr(:3001), maxKafkaRate(1000), slowClientMaxAttempts(3), defaultTenantID(sukko), cpuRejectThreshold(60.0), cpuPauseThreshold(70.0), tcpListenBacklog(2048), httpRead/Write/IdleTimeout(15s/15s/60s), logLevel(info), logFormat(json), metricsInterval(15s), cpuPollInterval(1s), pongWait(60s), pingPeriod(45s), writeWait(5s), provisioningGRPCReconnectDelay(1s), provisioningGRPCReconnectMaxDelay(30s), topicRefreshInterval(60s)
- All alerting.* fields (Go defaults)
- All audit.* fields (Go defaults)
- valkey.masterName(mymaster), valkey.db(0), valkey.channel(ws.broadcast)
- nats.clusterMode(false), nats.subject(ws.broadcast)
- jetstream.replicas(1), jetstream.maxAge(24h)
- kafka.consumerEnabled(true), kafka.defaultPartitions(1), kafka.defaultReplicationFactor(1)

**ws-gateway** (~20 fields removable):
- config.*: readTimeout(15s), writeTimeout(15s), idleTimeout(60s), dialTimeout(10s), messageTimeout(60s), logLevel(info), logFormat(json), requireTenantId(true), authRefreshRateInterval(30s), oidcKeyfuncCacheTTL(1h), jwksFetchTimeout(10s), jwksRefreshInterval(1h), fallbackPublicChannels(*.metadata), provisioningGRPCReconnectDelay(1s), provisioningGRPCReconnectMaxDelay(30s)

**provisioning** (~30 fields removable):
- config.*: logLevel(info), logFormat(json), autoMigrate(true), authEnabled(false), httpRead/Write/IdleTimeout(15s/15s/60s), defaultPartitions(3), defaultRetentionMs(604800000), maxTopicsPerTenant(50), maxPartitionsPerTenant(200), maxStorageBytes, producerByteRate, consumerByteRate, deprovisionGraceDays(30), adminAuth* fields, validNamespaces, apiRateLimitPerMin(60), keyRegistry* fields, shutdownTimeout(30s), lifecycleManagerEnabled(true), lifecycleCheckInterval(1h)
- database.maxOpenConns(25), maxIdleConns(5), connMaxLifetime(5m)

### Hysteresis Computation (FR-009)

Current Helm configmap computes lower thresholds via `{{ sub .Values.config.cpuRejectThreshold 10.0 }}`. This violates FR-009 (no derived values in Helm). Solution: compute in Go.

Current Go config has separate env vars for upper and lower. Change to:
- Keep `WS_CPU_REJECT_THRESHOLD` and `WS_CPU_PAUSE_THRESHOLD` as primary config
- Remove `WS_CPU_REJECT_THRESHOLD_LOWER` and `WS_CPU_PAUSE_THRESHOLD_LOWER` from Helm
- In Go `Validate()`, auto-compute lower = upper - 10.0 when lower is 0 (sentinel)
- Developer CAN still set lower explicitly via env var for custom bands

### Provisioning Config Mode (FR-007a)

Files to delete:
- `ws/internal/provisioning/configstore/` (entire package: loader.go, parser.go, validator.go, types.go, stores.go + all tests)

Files to modify:
- `ws/internal/shared/platform/provisioning_config.go` — remove `ProvisioningMode`, `ConfigFilePath`, conditional validation
- `ws/cmd/provisioning/main.go` — remove config mode switch branch (lines 95-124), SIGHUP handler (lines 309-345), config reload metrics (lines 35-50)
- `ws/cmd/cli/commands/config.go` — remove config commands (configInit, configValidate, configExport)
- `ws/internal/shared/platform/provisioning_config_test.go` — remove config mode tests
- Helm provisioning deployment.yaml — remove PROVISIONING_MODE, PROVISIONING_CONFIG_PATH, volume mount logic
- Helm provisioning values.yaml — remove provisioningMode, configFilePath, configFileConfigMap

### Template Architecture Decision

**Current**: ConfigMap (69 lines) sets most env vars via `envFrom`, deployment (304 lines) sets auto-wired and conditional vars via `env`.

**New approach**:
1. **ConfigMap**: Only K8s-computed values (WS_MAX_CONNECTIONS, WS_MAX_GOROUTINES) + developer overrides via `extraEnv` map
2. **Deployment env**: Mode selection + auto-wired addresses + conditional secrets
3. **All other config**: Go envDefault handles it — NOT set in Helm

This requires:
- Rewrite configmap.yaml (~15 lines)
- Rewrite deployment.yaml env section (~80 lines instead of ~200)
- New `extraEnv: {}` escape hatch in values.yaml

### Docker Compose Approach

Dockerfiles build: ws/build/{server,gateway,provisioning}/Dockerfile
- Server entry: `./sukko --shards=3 --base-port=3002 --lb-addr=:3005`
- Gateway entry: `./sukko-gateway`
- Provisioning entry: `./sukko-provisioning`

Docker Compose services:
1. `nats` — official nats:2.10-alpine
2. `provisioning` — build from ws/build/provisioning/Dockerfile, SQLite, port 8080+9090
3. `ws-server` — build from ws/build/server/Dockerfile, port 3001-3005
4. `ws-gateway` — build from ws/build/gateway/Dockerfile, port 3000, AUTH_ENABLED=false

Inter-service wiring:
- Gateway → ws-server: `GATEWAY_BACKEND_URL=ws://ws-server:3001/ws` (docker-compose service name)
- ws-server → NATS: `NATS_URLS=nats://nats:4222`
- ws-server → provisioning: `PROVISIONING_GRPC_ADDR=provisioning:9090`
- Gateway → provisioning: `PROVISIONING_GRPC_ADDR=provisioning:9090`

### --validate-config Implementation

Current flag handling:
- server: `flag.Bool("debug", false, ...)` — add `--validate-config`
- gateway: `caarlos0/env.Parse()` directly — add flag parsing first
- provisioning: `flag.Bool("debug", false, ...)` — add `--validate-config`

All three binaries load config via `env.Parse()` then call `cfg.Validate()`. The `--validate-config` flag intercepts after `Validate()` and before any network connections.
