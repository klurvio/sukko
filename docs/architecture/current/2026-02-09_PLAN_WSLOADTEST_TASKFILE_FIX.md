# Plan: Fix wsloadtest Taskfile (Same Pattern as wspublisher)

**Date:** 2026-02-09
**Status:** Done

**Scope:** Update wsloadtest taskfile to use the same working pattern as wspublisher to fix variable resolution issues.

---

## Problem

The wsloadtest taskfile has the same issues that wspublisher had:
1. Uses `{{.ROOT_DIR}}` which doesn't resolve in nested includes
2. Uses `dir:` directive which doesn't work correctly
3. Uses conflicting variable names (`NAMESPACE`)
4. Missing `local:stop` task
5. Missing shortcuts in root Taskfile

---

## Changes Required

### 1. Update `taskfiles/wsloadtest/Taskfile.yml`

**Before:**
```yaml
vars:
  DEPLOY_DIR: "{{.ROOT_DIR}}/deployments/wsloadtest"
  WSLOADTEST_DIR: "{{.ROOT_DIR}}/wsloadtest"
  PROJECT: "trim-array-480700-j7"
  NAMESPACE: "sukko-local"  # Conflicts with k8s taskfiles!
  LOCAL_PORT: "3006"
```

**After:**
```yaml
vars:
  # Use LOADTEST_ prefix to avoid conflicts
  LOADTEST_DIR: "{{.USER_WORKING_DIR}}/wsloadtest"
  LOADTEST_DEPLOY_DIR: "{{.USER_WORKING_DIR}}/deployments/wsloadtest"
  LOADTEST_PROJECT: "trim-array-480700-j7"
  LOADTEST_NAMESPACE: "sukko-local"
  LOADTEST_PORT: "3006"
```

**Build tasks:** Remove `dir:` directive, use explicit paths

**Local tasks:**
- Update to use Docker container (like wspublisher)
- Add `local:stop` task

---

### 2. Update root `Taskfile.yml`

Add shortcuts:
```yaml
loadtest:local:
  desc: Run wsloadtest locally
  cmds:
    - task: wsloadtest:local:run

loadtest:stop:
  desc: Stop wsloadtest container
  cmds:
    - task: wsloadtest:local:stop
```

---

## Files to Modify

| File | Changes |
|------|---------|
| `taskfiles/wsloadtest/Taskfile.yml` | Fix variable names, remove `dir:`, add `local:stop` |
| `Taskfile.yml` | Add `loadtest:*` shortcuts |

---

## Implementation Details

### wsloadtest/Taskfile.yml

```yaml
version: "3"

vars:
  LOADTEST_DIR: "{{.USER_WORKING_DIR}}/wsloadtest"
  LOADTEST_DEPLOY_DIR: "{{.USER_WORKING_DIR}}/deployments/wsloadtest"
  LOADTEST_PROJECT: "trim-array-480700-j7"
  LOADTEST_NAMESPACE: "sukko-local"
  LOADTEST_PORT: "3006"

tasks:
  build:
    desc: Build wsloadtest binary
    cmds:
      - cd {{.LOADTEST_DIR}} && go build -o wsloadtest .
      - 'echo "Built wsloadtest binary"'

  docker:
    desc: Build wsloadtest Docker image
    cmds:
      - docker build -t wsloadtest:latest {{.LOADTEST_DIR}}
      - 'echo "Built wsloadtest:latest"'

  push:
    desc: Build and push to Artifact Registry
    cmds:
      - docker buildx build --platform linux/amd64
          -t us-central1-docker.pkg.dev/{{.LOADTEST_PROJECT}}/sukko/wsloadtest:latest
          -f {{.LOADTEST_DIR}}/Dockerfile {{.LOADTEST_DIR}} --push
      - 'echo "Pushed to Artifact Registry"'

  local:run:
    desc: Run wsloadtest locally (CONNECTIONS=100, DURATION=1m)
    vars:
      CONNECTIONS: '{{.CONNECTIONS | default "100"}}'
      DURATION: '{{.DURATION | default "1m"}}'
      RAMP: '{{.RAMP | default "10"}}'
    cmds:
      - task: docker
      - |
        docker run --rm --name wsloadtest-local \
          --add-host=host.docker.internal:host-gateway \
          -e WS_URL=ws://host.docker.internal:{{.LOADTEST_PORT}}/ws \
          -e HEALTH_URL=http://host.docker.internal:{{.LOADTEST_PORT}}/health \
          -e CONNECTIONS={{.CONNECTIONS}} \
          -e RAMP_RATE={{.RAMP}} \
          -e DURATION={{.DURATION}} \
          -e TENANT=sukko \
          -e CHANNELS=sukko.all.trade \
          wsloadtest:latest

  local:quick:
    desc: Quick smoke test (10 connections, 30s)
    cmds:
      - task: local:run
        vars:
          CONNECTIONS: "10"
          DURATION: "30s"
          RAMP: "5"

  local:stop:
    desc: Stop wsloadtest container
    cmds:
      - docker stop wsloadtest-local 2>/dev/null || true
      - 'echo "Stopped wsloadtest"'

  remote:deploy:
    desc: Deploy wsloadtest GCE VM
    cmds:
      - cd {{.LOADTEST_DEPLOY_DIR}} && chmod +x *.sh && ./deploy.sh

  remote:destroy:
    desc: Destroy wsloadtest GCE VM
    cmds:
      - cd {{.LOADTEST_DEPLOY_DIR}} && ./destroy.sh

  remote:logs:
    desc: View wsloadtest VM logs
    cmds:
      - cd {{.LOADTEST_DEPLOY_DIR}} && ./logs.sh

  remote:ssh:
    desc: SSH into wsloadtest VM
    cmds:
      - cd {{.LOADTEST_DEPLOY_DIR}} && ./ssh.sh
```

---

## Root Taskfile Shortcuts

```yaml
loadtest:local:
  desc: Run wsloadtest locally
  cmds:
    - task: wsloadtest:local:run

loadtest:quick:
  desc: Quick smoke test (10 connections)
  cmds:
    - task: wsloadtest:local:quick

loadtest:stop:
  desc: Stop wsloadtest container
  cmds:
    - task: wsloadtest:local:stop
```

---

## Verification

1. `task wsloadtest:build` compiles binary
2. `task wsloadtest:docker` builds Docker image
3. `task loadtest:local CONNECTIONS=10 DURATION=30s` runs load test
4. `task loadtest:stop` stops the container
5. `task loadtest:quick` runs smoke test
