# Plan: Taskfile Reorganization

**Status:** Done

**Goal:** Create a consistent, domain-driven taskfile pattern that is easy to understand and doesn't interfere between environments.

---

## Current Problems

| Issue | Example | Problem |
|-------|---------|---------|
| Inconsistent depth | `k8s:local:deploy` vs `k8s:remote:standard:deploy` | "standard" is redundant |
| Mixed patterns | `wsloadtest:local:run` vs `k8s:local:provision:topics` | Service location varies |
| ENV in path | `k8s:remote:standard:*` implies env in path | Should be a parameter |
| Scattered services | `wsloadtest:*`, `wspublisher:*` separate from k8s | Hard to discover |

---

## Proposed Pattern

```
{domain}:{service}:{action} [ENV=value]
```

### Domains (Infrastructure Layer)

| Domain | Description | Environment |
|--------|-------------|-------------|
| `local` | Kind cluster (local dev) | Always "local" (no ENV needed) |
| `k8s` | Remote Kubernetes (GKE) | ENV=dev\|stg\|prod |
| `gce` | GCE VMs (standalone) | ENV=dev\|stg\|prod |

### Services (What you're operating on)

| Service | Description |
|---------|-------------|
| (none) | Core cluster operations (deploy, status, logs, build, health) |
| `provision` | Redpanda topics + DB tenant provisioning |
| `loadtest` | WebSocket load testing |
| `publisher` | Kafka message publisher |
| `port-forward` | Port forwarding for local access |
| `db` | Database migrations |
| `tf` | Terraform infrastructure (k8s only) |

### Actions (What you're doing)

| Action | Description |
|--------|-------------|
| `setup` | Full setup (create + deploy) |
| `destroy` | Full teardown |
| `deploy` | Deploy/upgrade application (or create VM for GCE) |
| `build` | Build images (local: build + load to Kind, k8s: build + push to registry) |
| `status` | Show current state |
| `logs` | Tail logs (supports `:gateway`, `:all` variants) |
| `run` | Run/restart service (with parameters) |
| `stop` | Stop service |
| `ssh` | SSH into VM (GCE only) |

**Pattern Note:** `logs:gateway` and `logs:all` are convenience variants of the `logs` action, not separate services.

**GCE Lifecycle:**
- `deploy` = Create VM + start service (first time)
- `run` = Start/restart service on existing VM (rerun with new params)
- `stop` = Stop service container (VM stays running)
- `destroy` = Delete VM entirely

---

## Command Structure

### Local Development (Kind)

```bash
# Core cluster
task local:setup              # Create Kind + deploy + port-forward
task local:destroy            # Delete Kind cluster
task local:deploy             # Deploy/upgrade Helm release
task local:status             # Show pods and services
task local:logs               # Tail ws-server logs (default)
task local:logs:gateway       # Tail ws-gateway logs
task local:logs:all           # Tail all pod logs
task local:build              # Build and reload images
task local:health             # Check health endpoint
task local:shell              # Shell into ws-server pod

# Port forwarding
task local:port-forward:start # Start all port-forwards (gateway, redpanda, grafana, prometheus)
task local:port-forward:stop  # Stop all port-forwards

# Provisioning (Kafka topics + DB tenant/categories)
task local:provision:create   # Create Kafka topics + tenant + categories in DB
task local:provision:status   # List Kafka topics + tenants
task local:provision:delete   # Delete topics + tenant

# Load testing (runs in Docker)
task local:loadtest:run       # Run load test (CONNECTIONS=100 DURATION=1m)
task local:loadtest:smoke     # Quick smoke test
task local:loadtest:stop      # Stop load test

# Publisher (runs in Docker)
task local:publisher:run      # Run publisher (RATE=1)
task local:publisher:stop     # Stop publisher

# Database
task local:db:migrate         # Run migrations
task local:db:status          # Show migration status
```

### Remote Kubernetes (GKE)

```bash
# Core cluster (ENV required)
task k8s:setup ENV=dev    # Full setup (TF + deploy)
task k8s:destroy ENV=dev  # Full teardown
task k8s:deploy ENV=dev   # Deploy/upgrade Helm release
task k8s:connect ENV=dev  # Connect kubectl to cluster
task k8s:status ENV=dev   # Show pods and services
task k8s:logs ENV=dev     # Tail ws-server logs
task k8s:logs:gateway ENV=dev  # Tail ws-gateway logs
task k8s:logs:all ENV=dev # Tail all pod logs
task k8s:build ENV=dev    # Build and push images
task k8s:health ENV=dev   # Check health endpoint
task k8s:reload ENV=dev   # Restart all pods (no rebuild)
task k8s:rollback ENV=dev # Rollback to previous release
task k8s:events ENV=dev   # Show recent events (useful for Spot preemption)
task k8s:external-ips ENV=dev  # Get external IPs for services

# Port forwarding (view dashboards locally)
task k8s:port-forward:gateway ENV=dev    # Port-forward ws-gateway
task k8s:port-forward:grafana ENV=dev    # Port-forward Grafana
task k8s:port-forward:prometheus ENV=dev # Port-forward Prometheus

# Provisioning (Kafka topics + DB tenant/categories)
task k8s:provision:create ENV=dev   # Create topic + tenant + categories
task k8s:provision:loadtest ENV=dev # Create 24 load test topics
task k8s:provision:status ENV=dev   # List topics + tenants
task k8s:provision:delete ENV=dev   # Delete topics + tenant

# Terraform
task k8s:tf:init ENV=dev    # Initialize Terraform
task k8s:tf:plan ENV=dev    # Plan changes
task k8s:tf:apply ENV=dev   # Apply changes
task k8s:tf:destroy ENV=dev # Destroy infrastructure

# Database
task k8s:db:migrate ENV=dev  # Run migrations
task k8s:db:status ENV=dev   # Show migration status
```

### GCE VMs (Standalone Services)

```bash
# Load test VM
task gce:loadtest:deploy ENV=dev   # Deploy VM (create + start)
task gce:loadtest:destroy ENV=dev  # Destroy VM
task gce:loadtest:run ENV=dev      # Run/restart loadtest (CONNECTIONS=100 DURATION=1m)
task gce:loadtest:stop ENV=dev     # Stop loadtest container
task gce:loadtest:logs ENV=dev     # View logs
task gce:loadtest:ssh ENV=dev      # SSH into VM

# Publisher VM
task gce:publisher:deploy ENV=dev  # Deploy VM (create + start)
task gce:publisher:destroy ENV=dev # Destroy VM
task gce:publisher:run ENV=dev     # Run/restart publisher (RATE=1)
task gce:publisher:stop ENV=dev    # Stop publisher container
task gce:publisher:logs ENV=dev    # View logs
task gce:publisher:ssh ENV=dev     # SSH into VM
```

---

## Top-Level Shortcuts

Minimal shortcuts to avoid confusion. Use the full `{domain}:{service}:{action}` pattern for clarity.

```bash
task local                    # → local:setup (quick start for new developers)
task test                     # → test:unit (environment-independent)
```

**All other commands use the consistent pattern:**
```bash
task local:status             # Not "task status"
task local:logs               # Not "task logs"
task k8s:deploy ENV=dev       # Not "task deploy ENV=dev"
```

This ensures one clear way to do everything.

---

## File Structure

```
Taskfile.yml                  # Root entry point (includes all, defines shortcuts)
taskfiles/
├── local.yml                 # local:* tasks (Kind cluster)
├── k8s.yml                   # k8s:* tasks (GKE remote)
├── gce.yml                   # gce:* tasks (GCE VMs)
└── shared/
    ├── build.yml             # Docker build tasks (used internally)
    ├── test.yml              # Test tasks (test:*)
    └── version.yml           # Version info (version:*)
```

**Note:** `shared/` tasks are included by domain taskfiles internally. Users access them via domain commands (e.g., `local:build` calls `build:all` + `kind load`).

**Standalone Tasks (environment-independent):**
```bash
task test:unit        # Run unit tests
task test:integration # Run integration tests
task version:info     # Show version info
```

### Simplified from current:

```
BEFORE                           AFTER
------                           -----
taskfiles/k8s/Taskfile.yml       (removed - merged into k8s.yml)
taskfiles/k8s/common.yml         (removed - merged into shared/)
taskfiles/k8s/local/Taskfile.yml taskfiles/local.yml
taskfiles/k8s/remote/Taskfile.yml (removed - merged into k8s.yml)
taskfiles/k8s/remote/standard.yml taskfiles/k8s.yml
taskfiles/wsloadtest/Taskfile.yml (merged into local.yml, k8s.yml, gce.yml)
taskfiles/wspublisher/Taskfile.yml (merged into local.yml, k8s.yml, gce.yml)
taskfiles/provisioning.yml       (merged into local.yml, k8s.yml)
taskfiles/terraform/Taskfile.yml (merged into k8s.yml as k8s:tf:*)
```

---

## Variable Naming Convention

Each domain uses a unique prefix to avoid conflicts:

| Domain | Prefix | Example |
|--------|--------|---------|
| Local | `LOCAL_` | `LOCAL_NAMESPACE`, `LOCAL_CLUSTER_NAME` |
| K8s | `K8S_` | `K8S_NAMESPACE`, `K8S_ENV` |
| GCE | `GCE_` | `GCE_PROJECT`, `GCE_ZONE` |

Environment-derived variables:

```yaml
# k8s.yml
vars:
  K8S_ENV: '{{.ENV | default "dev"}}'
  K8S_NAMESPACE: 'odin-{{.K8S_ENV}}'
  K8S_CLUSTER: 'odin-ws-{{.K8S_ENV}}'
```

**Environment Names:**
| Full | Short |
|------|-------|
| develop | dev |
| staging | stg |
| production | prod |

**Local (Kind) Resource Constraints:** *(already configured)*
- 1 replica per service (ws-server, ws-gateway, provisioning, etc.)
- Minimal resource requests/limits for M1 MacBook
- See `deployments/helm/odin/values/local.yaml`

---

## Migration Plan

### Phase 1: Rename environment files
1. Rename Helm values files:
   - `values/standard/develop.yaml` → `values/standard/dev.yaml`
   - `values/standard/staging.yaml` → `values/standard/stg.yaml`
   - `values/standard/production.yaml` → `values/standard/prod.yaml`
2. Rename Terraform directories:
   - `environments/standard/develop/` → `environments/standard/dev/`
   - `environments/standard/staging/` → `environments/standard/stg/`
   - `environments/standard/production/` → `environments/standard/prod/`

### Phase 2: Create new taskfiles
1. Create `taskfiles/local.yml` (merge local k8s + loadtest + publisher)
2. Create `taskfiles/k8s.yml` (merge remote k8s + terraform)
3. Create `taskfiles/gce.yml` (GCE VM operations)
4. Update root `Taskfile.yml` with new includes and shortcuts

### Phase 3: Update documentation
1. Update `README.md` with new command patterns
2. Add command reference table

### Phase 4: Cleanup (after testing)
1. Remove old taskfile structure
2. Remove deprecated paths

---

## Command Comparison

### Local Commands

| Current | New |
|---------|-----|
| `task k8s:local:setup` | `task local:setup` |
| `task k8s:local:deploy` | `task local:deploy` |
| `task k8s:local:destroy` | `task local:destroy` |
| `task k8s:local:status` | `task local:status` |
| `task k8s:local:logs` | `task local:logs` |
| `task k8s:local:logs:gateway` | `task local:logs:gateway` |
| `task k8s:local:logs:all` | `task local:logs:all` |
| `task k8s:local:images:reload` | `task local:build` |
| `task k8s:local:health` | `task local:health` |
| `task k8s:local:shell` | `task local:shell` |
| `task k8s:local:port-forward:start` | `task local:port-forward:start` |
| `task k8s:local:port-forward:stop` | `task local:port-forward:stop` |
| `task k8s:local:provision:topics` + `provision:quick-setup` | `task local:provision:create` |
| `task k8s:local:provision:status` | `task local:provision:status` |
| `task k8s:local:provision:delete` | `task local:provision:delete` |
| `task k8s:local:migrate` | `task local:db:migrate` |
| `task k8s:local:migrate:status` | `task local:db:status` |
| `task wsloadtest:local:run` | `task local:loadtest:run` |
| `task wsloadtest:local:smoke` | `task local:loadtest:smoke` |
| `task wsloadtest:local:stop` | `task local:loadtest:stop` |
| `task wspublisher:local:run` | `task local:publisher:run` |
| `task wspublisher:local:stop` | `task local:publisher:stop` |

### K8s Remote Commands

| Current | New |
|---------|-----|
| `task k8s:remote:standard:setup` | `task k8s:setup ENV=dev` |
| `task k8s:remote:standard:deploy` | `task k8s:deploy ENV=dev` |
| `task k8s:remote:standard:destroy` | `task k8s:destroy ENV=dev` |
| `task k8s:remote:standard:connect` | `task k8s:connect ENV=dev` |
| `task k8s:remote:standard:status` | `task k8s:status ENV=dev` |
| `task k8s:remote:standard:logs` | `task k8s:logs ENV=dev` |
| `task k8s:remote:standard:health` | `task k8s:health ENV=dev` |
| `task k8s:remote:standard:reload` | `task k8s:reload ENV=dev` |
| `task k8s:remote:standard:rollback` | `task k8s:rollback ENV=dev` |
| `task k8s:remote:standard:events` | `task k8s:events ENV=dev` |
| `task k8s:remote:standard:external-ips` | `task k8s:external-ips ENV=dev` |
| `task k8s:remote:standard:port-forward:grafana` | `task k8s:port-forward:grafana ENV=dev` |
| `task k8s:remote:standard:provision:official` | `task k8s:provision:create ENV=dev` |
| `task k8s:remote:standard:provision:loadtest` | `task k8s:provision:loadtest ENV=dev` |
| `task k8s:remote:standard:provision:status` | `task k8s:provision:status ENV=dev` |
| `task k8s:remote:standard:provision:delete` | `task k8s:provision:delete ENV=dev` |
| `task k8s:remote:standard:tf:init` | `task k8s:tf:init ENV=dev` |
| `task k8s:remote:standard:tf:plan` | `task k8s:tf:plan ENV=dev` |
| `task k8s:remote:standard:tf:apply` | `task k8s:tf:apply ENV=dev` |
| `task k8s:remote:standard:tf:destroy` | `task k8s:tf:destroy ENV=dev` |
| `task k8s:remote:standard:migrate` | `task k8s:db:migrate ENV=dev` |

### GCE Commands

| Current | New |
|---------|-----|
| `task wsloadtest:remote:deploy` | `task gce:loadtest:deploy ENV=dev` |
| `task wsloadtest:remote:destroy` | `task gce:loadtest:destroy ENV=dev` |
| `task wsloadtest:remote:logs` | `task gce:loadtest:logs ENV=dev` |
| `task wsloadtest:remote:ssh` | `task gce:loadtest:ssh ENV=dev` |
| (new) | `task gce:loadtest:run ENV=dev` |
| (new) | `task gce:loadtest:stop ENV=dev` |
| `task wspublisher:remote:deploy` | `task gce:publisher:deploy ENV=dev` |
| `task wspublisher:remote:destroy` | `task gce:publisher:destroy ENV=dev` |
| `task wspublisher:remote:logs` | `task gce:publisher:logs ENV=dev` |
| `task wspublisher:remote:ssh` | `task gce:publisher:ssh ENV=dev` |
| (new) | `task gce:publisher:run ENV=dev` |
| (new) | `task gce:publisher:stop ENV=dev` |

---

## Benefits

1. **Predictable**: `{domain}:{service}:{action}` always
2. **Discoverable**: `task local:<tab>` shows all local commands
3. **No conflicts**: Unique variable prefixes per domain
4. **Flat structure**: 3 main files instead of 10+
5. **ENV as parameter**: Same command, different environments
6. **Minimal shortcuts**: Only `task local` and `task test` to avoid confusion

---

## Files to Create/Modify

### Taskfiles
| File | Action | Description |
|------|--------|-------------|
| `taskfiles/local.yml` | Create | All local:* tasks |
| `taskfiles/k8s.yml` | Create | All k8s:* tasks |
| `taskfiles/gce.yml` | Create | All gce:* tasks |
| `Taskfile.yml` | Modify | New includes + shortcuts |
| `README.md` | Modify | Update command reference |

### Helm Values (rename)
| From | To |
|------|-----|
| `deployments/helm/odin/values/standard/develop.yaml` | `dev.yaml` |
| `deployments/helm/odin/values/standard/staging.yaml` | `stg.yaml` |
| `deployments/helm/odin/values/standard/production.yaml` | `prod.yaml` |

### Terraform (rename directories)
| From | To |
|------|-----|
| `deployments/terraform/environments/standard/develop/` | `dev/` |
| `deployments/terraform/environments/standard/staging/` | `stg/` |
| `deployments/terraform/environments/standard/production/` | `prod/` |

---

## Approval Checklist

- [ ] Pattern makes sense for the project
- [ ] Top-level shortcuts are correct
- [ ] Variable naming is clear
- [ ] No missing functionality from current setup
