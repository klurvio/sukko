# Plan: Taskfiles & Deployments Reorganization

**Goal:** Reorganize taskfiles and deployments directories to follow industry standards, improve modularity, enable reuse, and eliminate confusion.

**Status:** Implemented
**Date:** 2026-02-01

---

## Current State Analysis

### Taskfiles Structure (Current)
```
taskfiles/
├── gcp/
│   ├── v1/                    # Legacy GCP VMs (8 files)
│   └── v2/                    # GCP VMs v2 loadtest/publisher (4 files)
├── k8s/
│   ├── Taskfile.yml           # Entry point
│   ├── common.yml             # Shared tasks (build, lint, test)
│   ├── local.yml              # Kind cluster (~330 lines)
│   ├── autopilot.yml          # GKE Autopilot
│   └── standard.yml           # GKE Standard (~530 lines)
├── local/                     # Legacy Docker Compose (5 files)
├── terraform/                 # Terraform tasks
│   └── Taskfile.yml
├── provisioning.yml           # DB migrations + API (~260 lines)
└── version.yml                # Version management
```

### Deployments Structure (Current)
```
deployments/
├── gcp/
│   ├── v1/distributed/        # Legacy GCP VM configs
│   └── v2/                    # GCP v2 configs
├── k8s/
│   ├── environments/local/    # Kind config only
│   └── helm/sukko/
│       └── values/
│           ├── local.yaml
│           ├── autopilot/{develop,staging,production}.yaml
│           └── standard/{develop,staging,production}.yaml
├── local/                     # Legacy local configs (grafana)
├── shared/                    # Empty/unused?
└── terraform/
    ├── gke-autopilot/{dev-staging,production,shared}/
    ├── gke-standard/{develop,staging,production,modules/}/
    └── modules/{artifact-registry,gke-cluster,vpc}/
```

---

## Problems Identified

### Taskfiles Issues

| Issue | Impact |
|-------|--------|
| **Inconsistent hierarchy** | `gcp/`, `k8s/`, `local/` at same level but represent different concepts |
| **v1/v2 versioning is confusing** | Should be deployment types, not versions |
| **Legacy cruft** | `taskfiles/local/` is Docker Compose era, now obsolete |
| **No clear local vs remote split** | Kind cluster is under `k8s/` but not clearly "local" |
| **Duplicated build logic** | Build tasks in common.yml and scattered in other files |
| **Monolithic files** | local.yml (330 lines), standard.yml (530 lines) - hard to maintain |
| **provisioning.yml at root** | Should be in shared/common location |
| **No composition** | Tasks don't compose well, lots of copy-paste |

### Deployments Issues

| Issue | Impact |
|-------|--------|
| **Inconsistent naming** | `gke-autopilot` vs `autopilot/`, `gke-standard` vs `standard/` |
| **Deep nesting** | `deployments/k8s/helm/sukko/` is 4 levels deep |
| **Legacy directories** | `gcp/v1/`, `gcp/v2/`, `local/` are obsolete |
| **Terraform scattered** | Modules in two places: `terraform/modules/` and `gke-standard/modules/` |
| **No clear environment naming** | `dev-staging` vs `develop` inconsistency |

---

## Proposed Structure

### Taskfiles (New)

```
taskfiles/
├── shared/                         # Reusable task modules
│   ├── build.yml                   # Docker build tasks
│   ├── test.yml                    # Go test tasks
│   ├── helm.yml                    # Helm operations (lint, template, deps)
│   └── version.yml                 # Version management
│
├── provisioning.yml                # DB migrations + API (keep at root for easy access)
│
├── k8s/                            # Kubernetes deployment
│   ├── Taskfile.yml                # Entry point - routes to local/remote
│   ├── common.yml                  # Shared K8s tasks (cluster-agnostic)
│   │
│   ├── local/                      # Local development
│   │   └── Taskfile.yml            # Kind cluster tasks (single file)
│   │
│   └── remote/                     # Remote clusters (GKE)
│       ├── Taskfile.yml            # Entry point
│       ├── common.yml              # Shared remote tasks (connect, build, push)
│       └── standard.yml            # GKE Standard-specific
│
└── terraform/                      # Infrastructure as Code
    ├── Taskfile.yml                # Entry point
    └── standard.yml                # Standard TF commands
```

**Note:** GCP VM tasks and Autopilot removed. Structure supports future Autopilot addition.

### Deployments (New)

```
deployments/
├── helm/                           # Helm charts (simplified path)
│   └── sukko/
│       ├── Chart.yaml
│       ├── values.yaml             # Base defaults
│       ├── charts/                 # Subcharts
│       │   ├── monitoring/
│       │   ├── nats/
│       │   ├── provisioning/
│       │   ├── redpanda/
│       │   ├── valkey/
│       │   ├── ws-gateway/
│       │   └── ws-server/
│       ├── templates/              # Parent templates
│       └── values/                 # Environment overrides
│           ├── local.yaml          # Kind cluster
│           └── standard/
│               ├── develop.yaml
│               ├── staging.yaml
│               └── production.yaml
│
├── k8s/                            # Raw K8s configs (non-Helm)
│   └── local/
│       └── kind-config.yaml        # Kind cluster config
│
└── terraform/
    ├── modules/                    # Reusable modules (single location)
    │   ├── artifact-registry/
    │   ├── gke-cluster/
    │   └── vpc/
    │
    └── environments/               # Environment configs
        └── standard/
            ├── develop/
            ├── staging/
            └── production/
```

**Note:** Autopilot environments removed. Structure supports future addition.

---

## Modular Task Design

### Principle: Compose, Don't Duplicate

Each taskfile should be small, focused, and composable.

### shared/build.yml
```yaml
version: "3"

vars:
  VERSION:
    sh: git describe --tags --always --dirty 2>/dev/null || echo "dev"
  COMMIT_HASH:
    sh: git rev-parse --short HEAD 2>/dev/null || echo "unknown"
  BUILD_TIME:
    sh: date -u +%Y-%m-%dT%H:%M:%SZ

tasks:
  ws-server:
    desc: Build ws-server Docker image
    dir: "{{.ROOT_DIR}}/ws"
    cmds:
      - docker build -t {{.IMAGE_NAME | default "sukko"}}:{{.TAG | default "latest"}}
          --build-arg VERSION={{.VERSION}}
          --build-arg COMMIT_HASH={{.COMMIT_HASH}}
          --build-arg BUILD_TIME={{.BUILD_TIME}}
          -f build/server/Dockerfile .

  gateway:
    desc: Build ws-gateway Docker image
    dir: "{{.ROOT_DIR}}/ws"
    cmds:
      - docker build -t {{.IMAGE_NAME | default "sukko-gateway"}}:{{.TAG | default "latest"}}
          --build-arg VERSION={{.VERSION}}
          --build-arg COMMIT_HASH={{.COMMIT_HASH}}
          --build-arg BUILD_TIME={{.BUILD_TIME}}
          -f build/gateway/Dockerfile .

  all:
    desc: Build all Docker images
    cmds:
      - task: ws-server
      - task: gateway
```

### shared/helm.yml
```yaml
version: "3"

vars:
  CHART_PATH: "{{.ROOT_DIR}}/deployments/helm/sukko"

tasks:
  lint:
    desc: Lint Helm charts
    cmds:
      - helm lint {{.CHART_PATH}}

  template:
    desc: Dry-run render templates
    cmds:
      - helm template sukko {{.CHART_PATH}} -f {{.VALUES_FILE}}

  deps:
    desc: Update Helm dependencies
    cmds:
      - helm dependency update {{.CHART_PATH}}

  diff:
    desc: Show diff before upgrade
    cmds:
      - helm diff upgrade sukko {{.CHART_PATH}} -f {{.VALUES_FILE}} -n {{.NAMESPACE}}

  upgrade:
    desc: Upgrade/install Helm release
    cmds:
      - helm upgrade --install sukko {{.CHART_PATH}} -f {{.VALUES_FILE}} -n {{.NAMESPACE}} --create-namespace {{.EXTRA_ARGS}}
```

### k8s/local/kind.yml (Simplified)
```yaml
version: "3"

includes:
  build:
    taskfile: ../../shared/build.yml
  helm:
    taskfile: ../../shared/helm.yml
    vars:
      VALUES_FILE: "{{.ROOT_DIR}}/deployments/helm/sukko/values/local.yaml"
      NAMESPACE: sukko-local

vars:
  CLUSTER_NAME: sukko-local
  KIND_CONFIG: "{{.ROOT_DIR}}/deployments/k8s/local/kind-config.yaml"

tasks:
  # Cluster lifecycle
  cluster:create:
    desc: Create Kind cluster
    cmds:
      - kind create cluster --config {{.KIND_CONFIG}} --name {{.CLUSTER_NAME}} || true
      - kubectl config use-context kind-{{.CLUSTER_NAME}}

  cluster:delete:
    desc: Delete Kind cluster
    cmds:
      - kind delete cluster --name {{.CLUSTER_NAME}}

  # Build (delegates to shared)
  build:
    desc: Build and load images into Kind
    cmds:
      - task: build:all
      - kind load docker-image sukko:latest --name {{.CLUSTER_NAME}}
      - kind load docker-image sukko-gateway:latest --name {{.CLUSTER_NAME}}

  # Deploy (composes helm + migrations)
  deploy:
    desc: Deploy to Kind (runs migrations first)
    cmds:
      - kubectl config use-context kind-{{.CLUSTER_NAME}}
      - task: migrate
      - task: helm:upgrade

  # Convenience compositions
  setup:
    desc: Full setup from scratch
    cmds:
      - task: cluster:create
      - task: build
      - task: deploy
      - task: status

  # ... other tasks
```

### k8s/remote/common.yml (Shared Remote Tasks)
```yaml
version: "3"

includes:
  build:
    taskfile: ../../shared/build.yml
  helm:
    taskfile: ../../shared/helm.yml

tasks:
  # Registry operations
  push:
    desc: Push images to registry
    cmds:
      - docker tag sukko:latest {{.REGISTRY}}/ws-server:latest
      - docker tag sukko-gateway:latest {{.REGISTRY}}/ws-gateway:latest
      - docker push {{.REGISTRY}}/ws-server:latest
      - docker push {{.REGISTRY}}/ws-gateway:latest

  build-and-push:
    desc: Build and push all images
    cmds:
      - task: build:all
        vars:
          IMAGE_NAME: "{{.REGISTRY}}/ws-server"
      - task: build:gateway
        vars:
          IMAGE_NAME: "{{.REGISTRY}}/ws-gateway"
      # Push with buildx for cross-platform
      - docker buildx build --platform linux/amd64 --push ...

  # Connect to cluster (abstract - implemented by specific files)
  connect:
    desc: Connect to remote cluster
    # Override in autopilot.yml / standard.yml

  # Deploy flow
  deploy:
    desc: Deploy to remote cluster
    cmds:
      - task: connect
      - task: migrate
      - task: helm:upgrade
```

### k8s/remote/standard.yml (Specific)
```yaml
version: "3"

includes:
  common:
    taskfile: ./common.yml
    vars:
      REGISTRY: "{{.GKE_STD_REGISTRY}}"
      VALUES_FILE: "{{.ROOT_DIR}}/deployments/helm/sukko/values/standard/{{.ENV}}.yaml"
      NAMESPACE: "sukko-std-{{.ENV}}"

vars:
  ENV: '{{.ENV | default "develop"}}'
  GKE_STD_PROJECT: 'trim-array-480700-j7'
  GKE_STD_ZONE: 'us-central1-a'
  GKE_STD_CLUSTER: 'sukko-{{.ENV}}'
  GKE_STD_REGISTRY: 'us-central1-docker.pkg.dev/{{.GKE_STD_PROJECT}}/sukko'

tasks:
  connect:
    desc: Connect to GKE Standard cluster
    cmds:
      - gcloud container clusters get-credentials {{.GKE_STD_CLUSTER}} --zone {{.GKE_STD_ZONE}} --project {{.GKE_STD_PROJECT}}

  # Terraform (delegates)
  tf:init:
    cmds:
      - task: terraform:standard:init
        vars: { ENV: "{{.ENV}}" }

  tf:plan:
    cmds:
      - task: terraform:standard:plan
        vars: { ENV: "{{.ENV}}" }

  tf:apply:
    cmds:
      - task: terraform:standard:apply
        vars: { ENV: "{{.ENV}}" }

  # Deploy shortcuts
  deploy:develop:
    cmds:
      - task: common:deploy
        vars: { ENV: develop }

  deploy:staging:
    cmds:
      - task: common:deploy
        vars: { ENV: staging }

  deploy:production:
    prompt: "Deploy to PRODUCTION?"
    cmds:
      - task: common:deploy
        vars: { ENV: production }
```

---

## Top-Level Taskfile (New)

```yaml
version: "3"

includes:
  # Shared utilities
  build:
    taskfile: ./taskfiles/shared/build.yml
  test:
    taskfile: ./taskfiles/shared/test.yml
  version:
    taskfile: ./taskfiles/shared/version.yml

  # Provisioning (DB + API)
  provisioning:
    taskfile: ./taskfiles/provisioning.yml

  # Kubernetes
  k8s:
    taskfile: ./taskfiles/k8s/Taskfile.yml

  # Terraform
  terraform:
    taskfile: ./taskfiles/terraform/Taskfile.yml

  # Legacy GCP (VM-based) - mark as deprecated
  gcp:
    taskfile: ./taskfiles/gcp/Taskfile.yml

tasks:
  default:
    desc: Show available commands
    cmds:
      - task --list

  # === Quick Shortcuts ===
  dev:
    desc: Start local development (Kind)
    cmds:
      - task: k8s:local:setup

  dev:deploy:
    desc: Deploy to local Kind cluster
    cmds:
      - task: k8s:local:deploy

  dev:status:
    desc: Show local cluster status
    cmds:
      - task: k8s:local:status

  # === Remote Shortcuts ===
  deploy:develop:
    desc: Deploy to GKE Standard develop
    cmds:
      - task: k8s:remote:standard:deploy:develop

  deploy:staging:
    desc: Deploy to GKE Standard staging
    cmds:
      - task: k8s:remote:standard:deploy:staging

  deploy:production:
    desc: Deploy to GKE Standard production
    cmds:
      - task: k8s:remote:standard:deploy:production

  # === Build Shortcuts ===
  build:
    desc: Build all Docker images
    cmds:
      - task: build:all

  test:
    desc: Run unit tests
    cmds:
      - task: test:unit

  lint:
    desc: Lint Helm charts
    cmds:
      - task: k8s:common:helm:lint
```

---

## Migration Plan

### Phase 1: Create New Structure

1. Create `taskfiles/shared/` with modular tasks
2. Create `taskfiles/k8s/local/` directory
3. Create `taskfiles/k8s/remote/` directory
4. Move `deployments/k8s/helm/` to `deployments/helm/`

### Phase 2: Migrate Taskfiles

1. Extract shared logic into `shared/*.yml`
2. Refactor `k8s/local.yml` → `k8s/local/Taskfile.yml`
3. Refactor `k8s/standard.yml` → `k8s/remote/standard.yml`
4. Update all `includes:` paths with `dir:` option

### Phase 3: Update Root Taskfile

1. Update includes to new paths
2. Add top-level shortcuts
3. Use `ENV` variable consistently

### Phase 4: Cleanup Legacy

1. Delete `taskfiles/local/` (legacy Docker Compose)
2. Delete `taskfiles/gcp/` (legacy VM deployment)
3. Delete `taskfiles/k8s/autopilot.yml` (not in use)
4. Delete `deployments/gcp/` (legacy VM configs)
5. Delete `deployments/local/` (legacy local configs)

### Phase 5: Terraform Reorganization

1. Move `gke-standard/modules/` to `terraform/modules/`
2. Rename `gke-standard/{develop,staging,production}/` → `terraform/environments/standard/`
3. Delete `gke-autopilot/` (not in use)
4. Keep Terraform state files in place (only directories move)

---

## File Mapping

### Taskfiles

| Current | New |
|---------|-----|
| `taskfiles/k8s/common.yml` | `taskfiles/shared/build.yml` + `helm.yml` + `test.yml` |
| `taskfiles/k8s/local.yml` | `taskfiles/k8s/local/kind.yml` |
| `taskfiles/k8s/standard.yml` | `taskfiles/k8s/remote/standard.yml` |
| `taskfiles/k8s/autopilot.yml` | DELETE (not in use, structure ready for future) |
| `taskfiles/provisioning.yml` | `taskfiles/provisioning.yml` (keep) |
| `taskfiles/version.yml` | `taskfiles/shared/version.yml` |
| `taskfiles/terraform/Taskfile.yml` | `taskfiles/terraform/Taskfile.yml` (keep) |
| `taskfiles/local/*` | DELETE (legacy) |
| `taskfiles/gcp/v1/*` | Archive or DELETE |
| `taskfiles/gcp/v2/*` | `taskfiles/gcp/loadtest.yml` (if still needed) |

### Deployments

| Current | New |
|---------|-----|
| `deployments/k8s/helm/sukko/` | `deployments/helm/sukko/` |
| `deployments/k8s/environments/local/` | `deployments/k8s/local/` |
| `deployments/terraform/gke-standard/` | `deployments/terraform/environments/standard/` |
| `deployments/terraform/gke-autopilot/` | `deployments/terraform/environments/autopilot/` |
| `deployments/terraform/modules/` | `deployments/terraform/modules/` (keep) |
| `deployments/terraform/gke-standard/modules/` | MERGE into `modules/` |
| `deployments/gcp/*` | DELETE (legacy) |
| `deployments/local/*` | DELETE (legacy) |
| `deployments/shared/` | DELETE (empty) |

---

## Command Examples (After Migration)

```bash
# Local development
task dev                          # Full setup (cluster + build + deploy)
task dev:deploy                   # Just deploy
task k8s:local:build              # Build images for Kind
task k8s:local:logs               # Tail logs

# Remote (GKE Standard)
task deploy:develop               # Deploy to develop
task deploy:staging               # Deploy to staging
task deploy:production            # Deploy to production (with prompt)
task k8s:remote:standard:connect  # Connect to cluster
task k8s:remote:standard:status   # Show status

# Terraform
task terraform:standard:init ENV=develop
task terraform:standard:plan ENV=develop
task terraform:standard:apply ENV=develop

# Shared operations
task build                        # Build all images
task test                         # Run tests
task lint                         # Lint Helm charts

# Provisioning
task provisioning:db:migrate
task provisioning:api:tenant:list
```

---

## Verification

```bash
# After migration, verify all commands work:
task --list-all | grep -E "^task "

# Test local workflow
task dev
task k8s:local:status
task k8s:local:logs

# Test remote workflow (dry-run)
task k8s:remote:standard:connect
task terraform:standard:plan ENV=develop
```

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Missing edge cases | Test all commands after migration |
| Terraform state issues | Only move directories, keep state files in place |
| Include path errors | Use Task's `dir:` option for cleaner paths |

---

## Timeline Estimate

| Phase | Description |
|-------|-------------|
| 1 | Create new directory structure |
| 2 | Extract and refactor shared tasks |
| 3 | Migrate k8s taskfiles |
| 4 | Update root Taskfile |
| 5 | Cleanup legacy files |
| 6 | Terraform reorganization |
| 7 | Documentation update |

---

## Questions Resolved

1. **Is GCP v1/v2 still needed?** → DELETE - only using K8s now.
2. **Is Autopilot still in use?** → DELETE but keep structure optimized for future implementation.
3. **Should provisioning.yml be under shared?** → Keep at root for easy `task provisioning:*` access.

## Implementation Decisions

- **No backward compatibility** - Not deployed yet, clean migration
- **Include depth** - Use Task's `dir:` option instead of deep relative paths
- **Terraform state** - Only move directories, not state files
- **Naming convention** - No underscore prefix for files/dirs (use `shared/` not `_shared/`)
- **ENV variable** - Use simple `ENV` variable throughout for consistency
