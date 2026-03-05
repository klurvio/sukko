# Plan: Fix GCE publisher deployment for dev

## Context

The GCE publisher tasks (`taskfiles/gce.yml` publisher section) and shell scripts (`deployments/wspublisher/`) have the same issues as wsloadtest:
- Wrong project ID (`trim-array-480700-j7` → should be `sukko-9e902`)
- Wrong registry repo (`sukko` → should be `sukko`)
- `publisher:run` doesn't pass `KAFKA_BROKERS` or other required env vars to the container
- `publisher:deploy` shells out to `deploy.sh` which requires a `config.env` file
- No build/push task for wspublisher image
- `config.env.example` has wrong project/network values and defaults to `stag` namespace
- Startup script auto-runs the publisher (should be separated like wsloadtest)

## Files to modify

1. `taskfiles/gce.yml` — fix publisher:deploy, publisher:run, publisher:destroy, publisher:logs
2. `taskfiles/shared/build.yml` — add wspublisher build/push tasks
3. `deployments/wspublisher/config.env.example` — fix project, add dev defaults
4. `deployments/wspublisher/deploy.sh` — fix default registry
5. `deployments/wspublisher/destroy.sh` — fix default project
6. `deployments/wspublisher/logs.sh` — fix default project
7. `deployments/wspublisher/ssh.sh` — fix default project
8. `deployments/wspublisher/startup-script.sh` — fix registry, remove auto-run

---

## Change 1: Fix publisher tasks in `taskfiles/gce.yml`

The vars section (`GCE_PROJECT`, `GCE_REGISTRY`, `GCE_FOUNDATION_TF_DIR`) was already fixed in the wsloadtest plan. These changes only affect the publisher tasks.

### 1a. Rewrite `publisher:deploy`

Make self-contained — read VPC/subnet from foundation terraform, no `config.env` dependency. VM only does docker install + image pre-pull (no auto-run).

**Replace:**
```yaml
  publisher:deploy:
    desc: Deploy publisher VM (create + start)
    cmds:
      - |
        echo "Deploying wspublisher VM for {{.GCE_ENV}}..."
        cd {{.GCE_PUBLISHER_DIR}} && chmod +x *.sh && ENV={{.GCE_ENV}} ./deploy.sh
```

**With:**
```yaml
  publisher:deploy:
    desc: Deploy publisher VM (docker only, use publisher:run to start publishing)
    vars:
      VPC_NAME:
        sh: terraform -chdir={{.GCE_FOUNDATION_TF_DIR}} output -raw vpc_name 2>/dev/null || echo ""
      SUBNET_NAME:
        sh: terraform -chdir={{.GCE_FOUNDATION_TF_DIR}} output -raw ws_subnet_name 2>/dev/null || echo ""
    cmds:
      - |
        VM_NAME="wspublisher-{{.GCE_ENV}}"

        # Check if VM already exists
        if gcloud compute instances describe $VM_NAME --project={{.GCE_PROJECT}} --zone={{.GCE_ZONE}} &>/dev/null; then
          echo "ERROR: VM '$VM_NAME' already exists. Destroy it first:"
          echo "  task gce:publisher:destroy ENV={{.GCE_ENV}}"
          exit 1
        fi

        echo "=== Deploying wspublisher VM ==="
        echo "  VM:       $VM_NAME"
        echo "  VPC:      {{.VPC_NAME}}"
        echo "  Registry: {{.GCE_REGISTRY}}"
        echo ""

        gcloud compute instances create $VM_NAME \
          --project={{.GCE_PROJECT}} \
          --zone={{.GCE_ZONE}} \
          --machine-type=e2-standard-2 \
          --network={{.VPC_NAME}} \
          --subnet={{.SUBNET_NAME}} \
          --no-address \
          --preemptible \
          --scopes=cloud-platform \
          --labels="purpose=loadtest,env={{.GCE_ENV}}" \
          --metadata-from-file=startup-script={{.GCE_PUBLISHER_DIR}}/startup-script.sh \
          --metadata=REGISTRY="{{.GCE_REGISTRY}}"

        echo ""
        echo "=== VM Created (startup: docker install + image pre-pull) ==="
        echo "  Run:     task gce:publisher:run ENV={{.GCE_ENV}}"
        echo "  Logs:    task gce:publisher:logs ENV={{.GCE_ENV}}"
        echo "  SSH:     task gce:publisher:ssh ENV={{.GCE_ENV}}"
        echo "  Destroy: task gce:publisher:destroy ENV={{.GCE_ENV}}"
```

### 1b. Fix `publisher:run` to pass KAFKA_BROKERS and all env vars

The current task only passes `POISSON_LAMBDA` — it's missing `KAFKA_BROKERS`, `KAFKA_NAMESPACE`, and all other required env vars.

**Replace:**
```yaml
  publisher:run:
    desc: Run/restart publisher (RATE=10 msg/sec)
    vars:
      RATE: '{{.RATE | default "10"}}'
    cmds:
      - |
        echo "Running wspublisher on VM for {{.GCE_ENV}}..."
        VM_NAME="wspublisher-{{.GCE_ENV}}"
        gcloud compute ssh $VM_NAME --zone={{.GCE_ZONE}} --project={{.GCE_PROJECT}} --command="
          docker stop wspublisher 2>/dev/null || true
          docker run -d --rm --name wspublisher \
            -e POISSON_LAMBDA={{.RATE}} \
            {{.GCE_REGISTRY}}/wspublisher:latest
        "
```

**With:**
```yaml
  publisher:run:
    desc: Run/restart publisher (RATE=100 msg/sec)
    vars:
      REDPANDA_IP:
        sh: terraform -chdir={{.GCE_FOUNDATION_TF_DIR}} output -raw redpanda_internal_ip 2>/dev/null || echo ""
      RATE: '{{.RATE | default "100"}}'
      DURATION: '{{.DURATION | default "0"}}'
    cmds:
      - |
        VM_NAME="wspublisher-{{.GCE_ENV}}"
        KAFKA_BROKERS="{{.REDPANDA_IP}}:9092"
        echo "Running wspublisher on $VM_NAME (brokers: $KAFKA_BROKERS, rate: {{.RATE}} msg/s)..."
        gcloud compute ssh $VM_NAME --zone={{.GCE_ZONE}} --project={{.GCE_PROJECT}} --command="
          docker stop wspublisher 2>/dev/null || true
          docker run -d --rm --name wspublisher \
            -e KAFKA_BROKERS=$KAFKA_BROKERS \
            -e KAFKA_NAMESPACE={{.GCE_ENV}} \
            -e TIMING_MODE=poisson \
            -e POISSON_LAMBDA={{.RATE}} \
            -e DURATION={{.DURATION}} \
            -e TENANT_ID=sukko \
            -e IDENTIFIERS=BTC,ETH,SOL,all \
            -e CATEGORIES=trade,liquidity,orderbook \
            -e LOG_LEVEL=info \
            {{.GCE_REGISTRY}}/wspublisher:latest
        "
```

### 1c. Fix `publisher:destroy` and `publisher:logs`

Make inline and consistent (same pattern as loadtest).

**Replace `publisher:destroy` with:**
```yaml
  publisher:destroy:
    desc: Destroy publisher VM
    cmds:
      - |
        VM_NAME="wspublisher-{{.GCE_ENV}}"
        if ! gcloud compute instances describe $VM_NAME --project={{.GCE_PROJECT}} --zone={{.GCE_ZONE}} &>/dev/null; then
          echo "VM '$VM_NAME' does not exist. Nothing to delete."
          exit 0
        fi
        echo "Deleting wspublisher VM: $VM_NAME"
        gcloud compute instances delete $VM_NAME \
          --project={{.GCE_PROJECT}} \
          --zone={{.GCE_ZONE}} \
          --quiet
        echo "VM deleted."
```

**Replace `publisher:logs` with:**
```yaml
  publisher:logs:
    desc: View publisher logs
    cmds:
      - |
        VM_NAME="wspublisher-{{.GCE_ENV}}"
        gcloud compute instances get-serial-port-output $VM_NAME \
          --project={{.GCE_PROJECT}} \
          --zone={{.GCE_ZONE}}
```

---

## Change 2: Add wspublisher build/push to `taskfiles/shared/build.yml`

Add build and push tasks for wspublisher, matching the existing pattern.

Add to the `tasks:` section:
```yaml
  publisher:
    desc: Build wspublisher image
    cmds:
      - docker build -t wspublisher:latest {{.ROOT_DIR}}/wspublisher

  push:publisher:
    desc: Push wspublisher to registry
    requires:
      vars: [REGISTRY]
    cmds:
      - docker tag wspublisher:latest {{.REGISTRY}}/wspublisher:latest
      - docker push {{.REGISTRY}}/wspublisher:latest
```

Add `push:publisher` to the `push:all` task:
```yaml
      - task: push:publisher
        vars:
          REGISTRY: "{{.REGISTRY}}"
```

---

## Change 3: Fix `deployments/wspublisher/config.env.example`

Update to reflect correct project and dev defaults:
```
# =============================================================================
# wspublisher Configuration
# =============================================================================
# Copy to config.env and fill in values:
#   cp config.env.example config.env
# =============================================================================

# GCP Project
PROJECT=sukko-9e902

# Zone for VM
ZONE=us-central1-a

# Network (must match Redpanda cluster's VPC)
NETWORK=sukko-dev-vpc
SUBNET=sukko-dev-vpc-subnet

# VM Settings (smaller than wsloadtest - not connection-heavy)
MACHINE_TYPE=e2-standard-2
VM_NAME=wspublisher-dev

# Container image
REGISTRY=us-central1-docker.pkg.dev/${PROJECT}/sukko
IMAGE_TAG=latest

# Kafka/Redpanda Settings (REQUIRED)
# Get IP: terraform -chdir=deployments/terraform/environments/standard/dev-foundation output redpanda_internal_ip
KAFKA_BROKERS=
KAFKA_NAMESPACE=dev

# Publishing Settings
RATE=100              # Messages per second
DURATION=0            # Run duration (0=infinite)

# Channel Settings (optional - uses defaults if not set)
# TENANT_ID=sukko
# IDENTIFIERS=BTC,ETH,SOL,all
# CATEGORIES=trade,liquidity,orderbook
```

---

## Change 4: Fix default values in shell scripts

These scripts are now secondary to the taskfile (which is self-contained), but fix for consistency:

**`deployments/wspublisher/deploy.sh`** line 44:
- `REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/$PROJECT/sukko}"` → `REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/$PROJECT/sukko}"`

**`deployments/wspublisher/destroy.sh`** line 13:
- `PROJECT="${PROJECT:-trim-array-480700-j7}"` → `PROJECT="${PROJECT:-sukko-9e902}"`

**`deployments/wspublisher/logs.sh`** line 10:
- `PROJECT="${PROJECT:-trim-array-480700-j7}"` → `PROJECT="${PROJECT:-sukko-9e902}"`

**`deployments/wspublisher/ssh.sh`** line 10:
- `PROJECT="${PROJECT:-trim-array-480700-j7}"` → `PROJECT="${PROJECT:-sukko-9e902}"`

**`deployments/wspublisher/startup-script.sh`** — rewrite to only do docker install + image pre-pull (no auto-run):
```bash
#!/bin/bash
set -e

# Helper to get metadata
get_metadata() {
  curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" \
    -H "Metadata-Flavor: Google" 2>/dev/null || echo ""
}

REGISTRY=$(get_metadata REGISTRY)
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/sukko-9e902/sukko}"

echo "=== wspublisher VM startup ==="
echo "Registry: $REGISTRY"

# Install Docker
echo "Installing Docker..."
apt-get update -qq && apt-get install -y -qq docker.io

# Authenticate to Artifact Registry
echo "Authenticating to Artifact Registry..."
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# Pre-pull image so publisher:run starts faster
echo "Pre-pulling wspublisher image..."
docker pull "$REGISTRY/wspublisher:latest"

echo "=== VM ready. Use 'task gce:publisher:run' to start publishing ==="
```

---

## Safety review

- VM names are env-scoped: `wspublisher-dev`, `wspublisher-stg`, etc.
- `publisher:deploy` checks if VM already exists before creating
- `publisher:destroy` only deletes VMs with `wspublisher` prefix (enforced by naming)
- All commands are scoped to `GCE_PROJECT` and `GCE_ZONE`
- No VPC/cluster/firewall modifications — only creates/deletes compute instances
- The VM is created with `--no-address` (no public IP) inside the cluster VPC
- `e2-standard-2` (smaller than loadtest — publisher is not connection-heavy)
- All terraform calls are read-only (`output -raw`)
- `publisher:run` reads `redpanda_internal_ip` from same foundation terraform the cluster uses

---

## Verification

```bash
# 1. Build and push wspublisher image
task k8s:build ENV=dev  # push:all now includes wspublisher

# 2. Deploy publisher VM (docker install + image pre-pull only)
task gce:publisher:deploy ENV=dev
# Expected: VM created in sukko-dev-vpc, no publisher running yet

# 3. Check startup logs (docker install, image pull)
task gce:publisher:logs ENV=dev

# 4. Start the publisher
task gce:publisher:run ENV=dev RATE=100
# Expected: publishes to Redpanda at 10.x.x.x:9092, namespace=dev

# 5. Re-run with different rate
task gce:publisher:run ENV=dev RATE=50 DURATION=5m

# 6. Stop without destroying VM
task gce:publisher:stop ENV=dev

# 7. Cleanup
task gce:publisher:destroy ENV=dev
```
