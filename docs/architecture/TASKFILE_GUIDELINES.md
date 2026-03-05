# Taskfile Guidelines

Reference for adding and maintaining tasks in the Sukko WebSocket project.

## Command Pattern

```
{domain}:{service}:{action} [ENV=value]
```

| Component | Description | Examples |
|-----------|-------------|----------|
| **domain** | Infrastructure layer | `local`, `k8s`, `gce` |
| **service** | What you're operating on | `provision`, `loadtest`, `publisher`, `db`, `tf` |
| **action** | What you're doing | `run`, `stop`, `deploy`, `status`, `logs` |
| **ENV** | Environment (remote only) | `dev`, `stg`, `prod` |

## File Structure

```
Taskfile.yml              # Root entry point
taskfiles/
├── local.yml             # local:* tasks (Kind cluster)
├── k8s.yml               # k8s:* tasks (GKE remote)
├── gce.yml               # gce:* tasks (GCE VMs)
└── shared/
    ├── build.yml         # Docker build (internal use)
    ├── test.yml          # test:* tasks
    └── version.yml       # version:* tasks
```

## Domains

| Domain | File | ENV Required | Description |
|--------|------|--------------|-------------|
| `local` | `taskfiles/local.yml` | No | Kind cluster for local development |
| `k8s` | `taskfiles/k8s.yml` | Yes | Remote GKE Kubernetes |
| `gce` | `taskfiles/gce.yml` | Yes | GCE VMs for standalone services |

## Variable Naming

Each domain uses a unique prefix to avoid conflicts:

```yaml
# local.yml
vars:
  LOCAL_NAMESPACE: sukko-local
  LOCAL_CLUSTER_NAME: sukko-local
  LOCAL_GATEWAY_PORT: 3006

# k8s.yml
vars:
  K8S_ENV: '{{.ENV | default "dev"}}'
  K8S_NAMESPACE: 'sukko-{{.K8S_ENV}}'
  K8S_CLUSTER: 'sukko-{{.K8S_ENV}}'

# gce.yml
vars:
  GCE_ENV: '{{.ENV | default "dev"}}'
  GCE_PROJECT: '{{.PROJECT | default "trim-array-480700-j7"}}'
```

## Adding a New Task

### Step 1: Identify the Domain

| If the task... | Use domain |
|----------------|------------|
| Operates on Kind cluster | `local` |
| Operates on remote GKE | `k8s` |
| Operates on GCE VMs | `gce` |
| Is environment-independent | `shared/` |

### Step 2: Identify the Service

| If the task... | Use service |
|----------------|-------------|
| Is core cluster operation | (none) - just `{domain}:{action}` |
| Manages Kafka topics or DB tenants | `provision` |
| Runs load tests | `loadtest` |
| Runs message publisher | `publisher` |
| Manages database migrations | `db` |
| Manages Terraform | `tf` |
| Manages port forwarding | `port-forward` |

### Step 3: Choose the Action

| Action | When to use |
|--------|-------------|
| `setup` | Full setup from scratch |
| `destroy` | Full teardown |
| `deploy` | Deploy/upgrade application |
| `run` | Run/start a service |
| `stop` | Stop a service |
| `status` | Show current state |
| `logs` | Tail logs |
| `ssh` | SSH into VM (GCE only) |

### Step 4: Write the Task

```yaml
# Template for a new task
new-service:action:
  desc: Short description (shown in task --list)
  vars:
    # Task-specific variables (optional)
    SOME_VAR: '{{.SOME_VAR | default "default_value"}}'
  cmds:
    - echo "Running action for {{.LOCAL_NAMESPACE}}"
    - kubectl get pods -n {{.LOCAL_NAMESPACE}}
```

## Examples

### Example 1: Add a Core Action (local)

Adding a `restart` action to local domain:

```yaml
# In taskfiles/local.yml, under tasks:

restart:
  desc: Restart all pods
  cmds:
    - kubectl rollout restart deployment -n {{.LOCAL_NAMESPACE}}
```

Usage: `task local:restart`

### Example 2: Add a Service Action (local)

Adding a `metrics:scrape` action:

```yaml
# In taskfiles/local.yml

metrics:scrape:
  desc: Scrape current metrics
  cmds:
    - |
      POD=$(kubectl get pods -n {{.LOCAL_NAMESPACE}} -l app.kubernetes.io/name=ws-server -o jsonpath='{.items[0].metadata.name}')
      kubectl exec -n {{.LOCAL_NAMESPACE}} $POD -- curl -s localhost:3001/metrics
```

Usage: `task local:metrics:scrape`

### Example 3: Add a Remote Action with ENV (k8s)

Adding a `scale` action:

```yaml
# In taskfiles/k8s.yml

scale:
  desc: Scale ws-server replicas
  vars:
    REPLICAS: '{{.REPLICAS | default "2"}}'
  cmds:
    - kubectl scale deployment sukko-server -n {{.K8S_NAMESPACE}} --replicas={{.REPLICAS}}
```

Usage: `task k8s:scale ENV=dev REPLICAS=3`

### Example 4: Add a GCE VM Action

Adding a `restart` action for loadtest:

```yaml
# In taskfiles/gce.yml

loadtest:restart:
  desc: Restart loadtest container
  cmds:
    - |
      VM_NAME="wsloadtest-{{.GCE_ENV}}"
      gcloud compute ssh $VM_NAME --zone={{.GCE_ZONE}} --project={{.GCE_PROJECT}} \
        --command="docker restart wsloadtest"
```

Usage: `task gce:loadtest:restart ENV=dev`

### Example 5: Add Log Variants

Adding `:provisioning` log variant:

```yaml
# In taskfiles/local.yml

logs:provisioning:
  desc: Tail provisioning-api logs
  cmds:
    - kubectl logs -f -l app.kubernetes.io/name=provisioning-api -n {{.LOCAL_NAMESPACE}} --tail=100
```

Usage: `task local:logs:provisioning`

## Best Practices

### DO

```yaml
# Use domain-prefixed variables
LOCAL_NAMESPACE: sukko-local

# Use default values for optional params
REPLICAS: '{{.REPLICAS | default "2"}}'

# Include desc for every task
desc: Short, clear description

# Use task references for composition
cmds:
  - task: connect
  - task: deploy
```

### DON'T

```yaml
# Don't use generic variable names
NAMESPACE: sukko-local  # BAD - use LOCAL_NAMESPACE

# Don't hardcode values
cmds:
  - kubectl get pods -n sukko-local  # BAD - use {{.LOCAL_NAMESPACE}}

# Don't create top-level shortcuts (except task local and task test)
deploy:  # BAD - use local:deploy or k8s:deploy
```

## Task Dependencies

Use `task:` to call other tasks:

```yaml
setup:
  desc: Full setup
  cmds:
    - task: cluster:create
    - task: images:build
    - task: deploy
    - task: port-forward:start
    - task: status
```

## User Parameters

Accept parameters via command line:

```yaml
loadtest:run:
  desc: Run load test (CONNECTIONS=100 DURATION=1m)
  vars:
    CONNECTIONS: '{{.CONNECTIONS | default "100"}}'
    DURATION: '{{.DURATION | default "1m"}}'
  cmds:
    - docker run ... -e CONNECTIONS={{.CONNECTIONS}} -e DURATION={{.DURATION}} ...
```

Usage: `task local:loadtest:run CONNECTIONS=500 DURATION=5m`

## Prompts for Destructive Actions

Add confirmation prompts for destructive operations:

```yaml
destroy:
  desc: Full teardown
  prompt: 'This will DESTROY everything for {{.K8S_ENV}}. Are you sure?'
  cmds:
    - helm uninstall {{.K8S_RELEASE_NAME}} -n {{.K8S_NAMESPACE}} || true
    - task: tf:destroy
```

## Shared Tasks

For tasks that need to be reused across domains, put them in `taskfiles/shared/`:

```yaml
# taskfiles/shared/build.yml
version: "3"

tasks:
  ws-server:
    desc: Build ws-server image
    cmds:
      - docker build -t {{.IMAGE_NAME}}:{{.IMAGE_TAG}} -f ws/Dockerfile ws/
```

Then include in domain taskfile:

```yaml
# taskfiles/local.yml
includes:
  build:
    taskfile: ./shared/build.yml
    dir: "{{.ROOT_DIR}}"

tasks:
  images:build:
    cmds:
      - task: build:ws-server
        vars:
          IMAGE_NAME: "{{.LOCAL_WS_IMAGE}}"
          IMAGE_TAG: "{{.LOCAL_IMAGE_TAG}}"
```

## Checklist for New Tasks

- [ ] Task is in the correct domain file
- [ ] Task follows `{service}:{action}` naming (or just `{action}` for core ops)
- [ ] Variables use domain prefix (`LOCAL_`, `K8S_`, `GCE_`)
- [ ] Task has a clear `desc`
- [ ] Parameters have defaults where appropriate
- [ ] Destructive actions have `prompt`
- [ ] No hardcoded values (use variables)
- [ ] Tested locally before committing

## Quick Reference

```bash
# Local (Kind) - no ENV needed
task local:setup
task local:status
task local:logs
task local:loadtest:run CONNECTIONS=100

# Remote K8s - ENV required
task k8s:deploy ENV=dev
task k8s:status ENV=stg
task k8s:logs ENV=prod

# GCE VMs - ENV required
task gce:loadtest:deploy ENV=dev
task gce:loadtest:run ENV=dev CONNECTIONS=500

# Environment-independent
task test:unit
task version:info
```
