# Plan: Fix GCE loadtest deployment for dev

## Context

The GCE loadtest tasks (`taskfiles/gce.yml`) and shell scripts (`deployments/wsloadtest/`) have several issues that prevent deploying the loadtest VM against the dev GKE cluster:
- Wrong project ID (`trim-array-480700-j7` → should be `odin-9e902`)
- Wrong registry repo (`odin` → should be `odin-ws`)
- `loadtest:run` doesn't pass WS_URL or other required env vars to the container
- `deploy.sh` requires a `config.env` file but `gce.yml` bypasses it with inline commands
- No build/push task for wsloadtest image
- `config.env.example` has wrong project/network values

## Files to modify

1. `taskfiles/gce.yml` — fix project, registry, and loadtest:run/deploy tasks
2. `taskfiles/shared/build.yml` — add wsloadtest build/push tasks
3. `deployments/wsloadtest/config.env.example` — fix project, add dev defaults
4. `deployments/wsloadtest/deploy.sh` — fix default project
5. `deployments/wsloadtest/destroy.sh` — fix default project
6. `deployments/wsloadtest/logs.sh` — fix default project
7. `deployments/wsloadtest/startup-script.sh` — fix default registry

---

## Change 1: Fix `taskfiles/gce.yml`

### 1a. Fix vars section

**Change:**
```yaml
  GCE_PROJECT: '{{.PROJECT | default "trim-array-480700-j7"}}'
```
**To:**
```yaml
  GCE_PROJECT: '{{.PROJECT | default "odin-9e902"}}'
```

**Change:**
```yaml
  GCE_REGISTRY: 'us-central1-docker.pkg.dev/{{.GCE_PROJECT}}/odin'
```
**To:**
```yaml
  GCE_REGISTRY: 'us-central1-docker.pkg.dev/{{.GCE_PROJECT}}/odin-ws'
```

### 1b. Add foundation TF dir and dynamic vars

Add to vars section:
```yaml
  GCE_FOUNDATION_TF_DIR: "{{.ROOT_DIR}}/deployments/terraform/environments/standard/{{.GCE_ENV}}-foundation"
```

### 1c. Rewrite `loadtest:deploy`

The current task just shells out to `deploy.sh` which requires `config.env`. Instead, make the taskfile self-contained by dynamically reading the VPC/subnet from terraform. The VM only does kernel tuning + docker install on boot — use `loadtest:run` separately to start the actual test.

**Replace with:**
```yaml
  loadtest:deploy:
    desc: Deploy load test VM (kernel tuning + docker only, use loadtest:run to start test)
    vars:
      VPC_NAME:
        sh: terraform -chdir={{.GCE_FOUNDATION_TF_DIR}} output -raw vpc_name 2>/dev/null || echo ""
      SUBNET_NAME:
        sh: terraform -chdir={{.GCE_FOUNDATION_TF_DIR}} output -raw ws_subnet_name 2>/dev/null || echo ""
    cmds:
      - |
        VM_NAME="wsloadtest-{{.GCE_ENV}}"

        # Check if VM already exists
        if gcloud compute instances describe $VM_NAME --project={{.GCE_PROJECT}} --zone={{.GCE_ZONE}} &>/dev/null; then
          echo "ERROR: VM '$VM_NAME' already exists. Destroy it first:"
          echo "  task gce:loadtest:destroy ENV={{.GCE_ENV}}"
          exit 1
        fi

        echo "=== Deploying wsloadtest VM ==="
        echo "  VM:       $VM_NAME"
        echo "  VPC:      {{.VPC_NAME}}"
        echo "  Registry: {{.GCE_REGISTRY}}"
        echo ""

        gcloud compute instances create $VM_NAME \
          --project={{.GCE_PROJECT}} \
          --zone={{.GCE_ZONE}} \
          --machine-type=e2-standard-8 \
          --network={{.VPC_NAME}} \
          --subnet={{.SUBNET_NAME}} \
          --no-address \
          --preemptible \
          --scopes=cloud-platform \
          --labels="purpose=loadtest,env={{.GCE_ENV}}" \
          --metadata-from-file=startup-script={{.GCE_LOADTEST_DIR}}/startup-script.sh \
          --metadata=REGISTRY="{{.GCE_REGISTRY}}"

        echo ""
        echo "=== VM Created (startup: kernel tuning + docker install) ==="
        echo "  Run test: task gce:loadtest:run ENV={{.GCE_ENV}}"
        echo "  Logs:     task gce:loadtest:logs ENV={{.GCE_ENV}}"
        echo "  SSH:      task gce:loadtest:ssh ENV={{.GCE_ENV}}"
        echo "  Destroy:  task gce:loadtest:destroy ENV={{.GCE_ENV}}"
```

### 1d. Fix `loadtest:run` to pass WS_URL and all env vars

**Replace with:**
```yaml
  loadtest:run:
    desc: Run/restart loadtest (CONNECTIONS=5000 DURATION=10m)
    vars:
      GATEWAY_IP:
        sh: terraform -chdir={{.GCE_FOUNDATION_TF_DIR}} output -raw gateway_external_ip 2>/dev/null || echo ""
      CONNECTIONS: '{{.CONNECTIONS | default "5000"}}'
      DURATION: '{{.DURATION | default "10m"}}'
      RAMP: '{{.RAMP | default "100"}}'
    cmds:
      - |
        VM_NAME="wsloadtest-{{.GCE_ENV}}"
        WS_URL="ws://{{.GATEWAY_IP}}:443/ws"
        echo "Running wsloadtest on $VM_NAME (target: $WS_URL)..."
        gcloud compute ssh $VM_NAME --zone={{.GCE_ZONE}} --project={{.GCE_PROJECT}} --command="
          docker stop wsloadtest 2>/dev/null || true
          docker run -d --rm --name wsloadtest \
            --ulimit nofile=1000000:1000000 \
            -e WS_URL=$WS_URL \
            -e TARGET_CONNECTIONS={{.CONNECTIONS}} \
            -e RAMP_RATE={{.RAMP}} \
            -e DURATION={{.DURATION}} \
            -e TENANT_ID=odin \
            -e CHANNELS=odin.BTC.trade,odin.ETH.trade,odin.SOL.trade \
            -e CHANNELS_PER_CLIENT=1 \
            -e LOG_LEVEL=info \
            {{.GCE_REGISTRY}}/wsloadtest:latest
        "
```

### 1e. Fix `loadtest:destroy` and `loadtest:logs`

Make them inline and consistent instead of shelling out to scripts:

**Replace `loadtest:destroy` with:**
```yaml
  loadtest:destroy:
    desc: Destroy load test VM
    cmds:
      - |
        VM_NAME="wsloadtest-{{.GCE_ENV}}"
        if ! gcloud compute instances describe $VM_NAME --project={{.GCE_PROJECT}} --zone={{.GCE_ZONE}} &>/dev/null; then
          echo "VM '$VM_NAME' does not exist. Nothing to delete."
          exit 0
        fi
        echo "Deleting wsloadtest VM: $VM_NAME"
        gcloud compute instances delete $VM_NAME \
          --project={{.GCE_PROJECT}} \
          --zone={{.GCE_ZONE}} \
          --quiet
        echo "VM deleted."
```

**Replace `loadtest:logs` with:**
```yaml
  loadtest:logs:
    desc: View load test logs
    cmds:
      - |
        VM_NAME="wsloadtest-{{.GCE_ENV}}"
        gcloud compute instances get-serial-port-output $VM_NAME \
          --project={{.GCE_PROJECT}} \
          --zone={{.GCE_ZONE}}
```

---

## Change 2: Add wsloadtest build/push to `taskfiles/shared/build.yml`

Add build and push tasks for wsloadtest, matching the existing pattern for ws-server/gateway/provisioning.

Add to the `tasks:` section:
```yaml
  build:loadtest:
    desc: Build wsloadtest image
    cmds:
      - docker build -t wsloadtest:latest {{.ROOT_DIR}}/wsloadtest

  push:loadtest:
    desc: Push wsloadtest to registry
    requires:
      vars: [REGISTRY]
    cmds:
      - docker tag wsloadtest:latest {{.REGISTRY}}/wsloadtest:latest
      - docker push {{.REGISTRY}}/wsloadtest:latest
```

Add `push:loadtest` to the `push:all` task:
```yaml
      - task: push:loadtest
        vars:
          REGISTRY: "{{.REGISTRY}}"
```

---

## Change 3: Fix `deployments/wsloadtest/config.env.example`

Update to reflect correct project and dev network:
```
PROJECT=odin-9e902
ZONE=us-central1-a
NETWORK=odin-ws-dev-vpc
SUBNET=odin-ws-dev-vpc-subnet
WS_URL=
MACHINE_TYPE=e2-standard-8
VM_NAME=wsloadtest-dev
REGISTRY=us-central1-docker.pkg.dev/${PROJECT}/odin-ws
IMAGE_TAG=latest
TARGET_CONNECTIONS=5000
RAMP_RATE=100
DURATION=10m
```

---

## Change 4: Fix default project in shell scripts

These scripts are now secondary to the taskfile (which is self-contained), but fix them for consistency:

**`deployments/wsloadtest/deploy.sh`** line 44:
- `REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/$PROJECT/odin}"` → `REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/$PROJECT/odin-ws}"`

**`deployments/wsloadtest/destroy.sh`** line 13:
- `PROJECT="${PROJECT:-trim-array-480700-j7}"` → `PROJECT="${PROJECT:-odin-9e902}"`

**`deployments/wsloadtest/logs.sh`** line 10:
- `PROJECT="${PROJECT:-trim-array-480700-j7}"` → `PROJECT="${PROJECT:-odin-9e902}"`

**`deployments/wsloadtest/startup-script.sh`** — rewrite to only do kernel tuning, docker install, and image pre-pull (no auto-run):
```bash
#!/bin/bash
set -e

# Helper to get metadata
get_metadata() {
  curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" \
    -H "Metadata-Flavor: Google" 2>/dev/null || echo ""
}

REGISTRY=$(get_metadata REGISTRY)
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/odin-9e902/odin-ws}"

echo "=== wsloadtest VM startup ==="
echo "Registry: $REGISTRY"

# Tune kernel for high connections
echo "Tuning kernel parameters..."
cat >> /etc/sysctl.conf << 'EOF'
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
EOF
sysctl -p

# Set ulimits
ulimit -n 1000000

# Install Docker
echo "Installing Docker..."
apt-get update -qq && apt-get install -y -qq docker.io

# Authenticate to Artifact Registry
echo "Authenticating to Artifact Registry..."
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# Pre-pull image so loadtest:run starts faster
echo "Pre-pulling wsloadtest image..."
docker pull "$REGISTRY/wsloadtest:latest"

echo "=== VM ready. Use 'task gce:loadtest:run' to start the test ==="
```

---

## Verification

```bash
# 1. Build and push wsloadtest image
task k8s:build ENV=dev  # push:all now includes wsloadtest

# 2. Deploy loadtest VM (kernel tuning + docker install + image pre-pull only)
task gce:loadtest:deploy ENV=dev
# Expected: VM created in odin-ws-dev-vpc, no loadtest running yet

# 3. Check startup logs (kernel tuning, docker install, image pull)
task gce:loadtest:logs ENV=dev

# 4. Start the loadtest
task gce:loadtest:run ENV=dev CONNECTIONS=5000 DURATION=10m
# Expected: connects to ws://34.44.7.48:443/ws

# 5. Re-run with different params
task gce:loadtest:run ENV=dev CONNECTIONS=1000 DURATION=5m

# 6. Cleanup
task gce:loadtest:destroy ENV=dev
```
