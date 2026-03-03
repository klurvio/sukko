# Migration Checklist: Minimal Helm Configuration

**Branch**: feat/helm-minimal-config | **Date**: 2026-03-03

This documents every default that changed and provides migration actions for existing deployments.

---

## Go Default Changes

| Env Var | Old Default | New Default | Migration Action |
|---------|------------|-------------|-----------------|
| `BROADCAST_TYPE` | `valkey` | `nats` | If using Valkey broadcast, add `BROADCAST_TYPE: "valkey"` to `extraEnv` or set `ws-server.broadcast.type: valkey` |
| `WS_MAX_BROADCAST_RATE` | `20` | `25` | No action needed (higher rate is safe). If you relied on 20, add to `extraEnv` |
| `CONN_RATE_LIMIT_IP_BURST` | `10` | `100` | No action needed (more permissive). If you need strict limiting, add to `extraEnv` |
| `CONN_RATE_LIMIT_IP_RATE` | `1.0` | `100.0` | No action needed (more permissive). If you need strict limiting, add to `extraEnv` |
| `ENVIRONMENT` | `development` | `local` | Check any scripts/monitoring that filter on `environment=development` — change to `local` |
| `AUTH_ENABLED` (gateway) | `true` | `false` | **Breaking**: Auth now disabled by default. Add `AUTH_ENABLED: "true"` to gateway `extraEnv` for production |

## Removed Go Config Fields

| Removed Field | Migration Action |
|--------------|-----------------|
| `PROVISIONING_MODE` | Removed entirely. Config file mode is no longer supported. Use API-only mode. |
| `PROVISIONING_CONFIG_PATH` | Removed entirely. Use API-only provisioning. |

## Helm Values Changes

### Subchart values.yaml

| Change | Old Path | New Path | Migration Action |
|--------|----------|----------|-----------------|
| Config section removed | `config.*` | `extraEnv` | Move any custom values from `config.logLevel` etc. to `extraEnv: { LOG_LEVEL: "warn" }` |
| Shards moved | `config.shards` | `shards` | Update values files: `ws-server.shards: N` |
| Base port moved | `config.basePort` | `basePort` | Update values files: `ws-server.basePort: N` |
| LB addr moved | `config.lbAddr` | `lbAddr` | Update values files: `ws-server.lbAddr: ":N"` |
| Environment moved | `config.environment` | `environment` | Update values files: `ws-server.environment: dev` |
| Admin token moved | `config.adminToken` | `adminToken` | Update provisioning values: `provisioning.adminToken: "..."` |
| gRPC port moved | `config.grpcPort` | `service.grpcPort` | Update provisioning values |
| Alerting removed | `alerting.*` | `extraEnv` | Move any alerting overrides to `extraEnv` |
| Audit removed | `audit.*` | `extraEnv` | Move any audit overrides to `extraEnv` |
| Provisioning mode removed | `config.provisioningMode` | — | Remove from values (no longer supported) |
| Config file path removed | `config.configFilePath` | — | Remove from values |

### Parent values.yaml

| Change | Migration Action |
|--------|-----------------|
| `provisioning.enabled` changed `false` → `true` | Provisioning now enabled by default. No action needed for most deployments. |
| `redpanda.enabled` changed `true` → `false` | If using Kafka, add `redpanda.enabled: true` to your environment values |

## Auto-computed Values

| Change | Description |
|--------|-------------|
| CPU hysteresis thresholds | Now auto-computed in Go: `lower = upper - 10.0` when not explicitly set. Remove `CPURejectThresholdLower` and `CPUPauseThresholdLower` from Helm values unless you need custom values. |
| `WS_MAX_CONNECTIONS` | Still computed in Helm: `clusterMaxConnections / replicaCount` |
| `WS_MAX_GOROUTINES` | Still computed in Helm: `maxConnections * 10 + 5000` |
| `DATABASE_DRIVER` | Auto-derived from `externalDatabase` presence in Helm template |

## New Features

| Feature | How to Use |
|---------|-----------|
| `extraEnv` escape hatch | Add any Go env var as key-value in `extraEnv: {}` on each service |
| `/config` endpoint | `GET /config` on any service admin port returns redacted runtime config |
| `--validate-config` | Run binary with this flag to validate config without starting the service |
| Helm pre-install hook | Validate-config runs automatically before every `helm install/upgrade` |

## Quick Migration Steps

1. **Replace `config.*` with `extraEnv`**: Any values under `config.*` that aren't in the new values.yaml must move to `extraEnv`
2. **Set auth explicitly**: Add `AUTH_ENABLED: "true"` to gateway `extraEnv` if you need authentication
3. **Set Kafka mode explicitly**: Add `redpanda.enabled: true` and `ws-server.messageBackend: kafka` if using Kafka
4. **Set PostgreSQL mode explicitly**: If using PostgreSQL, keep `postgresql.enabled: true` and configure `externalDatabase`
5. **Remove stale fields**: Delete any `provisioningMode`, `configFilePath`, `configFileConfigMap` from values
6. **Test with `--validate-config`**: Run each binary with `--validate-config` to verify config after migration
7. **Check `/config` endpoint**: After deployment, verify runtime config is correct via `GET /config`
