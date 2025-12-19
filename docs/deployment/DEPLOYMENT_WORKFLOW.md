# Deployment Workflow Guide

This guide explains when to rebuild images, restart containers, or recreate VMs based on the type of change you've made.

## Quick Reference

| Change Type | Action Required | Command |
|-------------|-----------------|---------|
| Go code changes | Rebuild + restart | `task gcp:deployment:rebuild:ws` |
| Env var value changes | Restart container | See [Restart Only](#restart-container-only) |
| New env vars with defaults | Rebuild + restart | `task gcp:deployment:rebuild:ws` |
| VM infrastructure | Recreate VM | `task gcp:delete && task gcp:deploy` |

## Detailed Scenarios

### 1. Go Code Changes (Most Common)

**When:** You modified Go source files (`.go` files)

**Examples:**
- Added new feature (e.g., hysteresis band)
- Fixed a bug
- Added new config fields with defaults
- Changed business logic

**Action:** Rebuild Docker image and restart container

```bash
task gcp:deployment:rebuild:ws
```

This will:
1. Pull latest code on the VM
2. Rebuild the Docker image with new binary
3. Stop and restart the container

---

### 2. Environment Variable Value Changes

**When:** You want to change a config value without code changes

**Examples:**
- Changing `WS_CPU_REJECT_THRESHOLD` from 75% to 80%
- Adjusting `WS_MAX_CONNECTIONS` limit
- Updating Redis password

**Action:** Update `.env` file and restart container

#### Option A: Restart Container Only

```bash
# SSH to the VM
gcloud compute ssh odin-ws-go --zone=us-central1-a

# Switch to deploy user and edit .env
sudo -i -u deploy
cd ~/odin-ws/deployments/v1/gcp/distributed/ws
nano .env.production  # or vim

# Restart the container
docker restart odin-ws-multi
```

#### Option B: Via gcloud command

```bash
gcloud compute ssh odin-ws-go --zone=us-central1-a \
  --command="sudo -u deploy docker restart odin-ws-multi"
```

---

### 3. New Environment Variables

**When:** Code adds new env vars

**With Defaults Provided:**
- Just rebuild - defaults will be used automatically
- `task gcp:deployment:rebuild:ws`

**Without Defaults (Required):**
1. Update `.env.production` on VM first
2. Then rebuild: `task gcp:deployment:rebuild:ws`

---

### 4. VM Infrastructure Changes

**When:** You need to change the underlying VM

**Examples:**
- Machine type (e2-standard-2 → e2-standard-4)
- Disk size
- Network configuration
- VM region/zone

**Action:** Recreate VM (nuclear option)

```bash
# Delete and recreate everything
task gcp:delete
task gcp:deploy
```

> **Warning:** This will cause downtime and lose any local state on VMs.

---

## Task Reference

### Rebuild Tasks (Code Changes)

Use these when Go code has changed and you need to rebuild the Docker image.

| Service | Command | Description |
|---------|---------|-------------|
| WebSocket | `task gcp:deployment:rebuild:ws` | Rebuild WS server with latest code |
| Backend | `task gcp:deployment:rebuild:backend` | Rebuild backend services |
| All | `task gcp:deployment:rebuild:all` | Rebuild all services |

### Restart Tasks (Config/Env Changes Only)

Use these when only environment variables changed (no code changes).

| Service | Command | Description |
|---------|---------|-------------|
| WebSocket | `task gcp:services:restart:ws` | Restart WS container only |
| Backend | `task gcp:services:restart:backend` | Restart backend containers |
| Redis | `task gcp:redis:restart` | Restart Redis container |
| All | `task gcp:services:restart:all` | Restart all services |
| All (alt) | `task gcp:restart` | Restart all GCP services |

### Other Useful Tasks

| Command | Description |
|---------|-------------|
| `task gcp:deployment:reset` | Stop + rebuild + start (full reset) |
| `task gcp:status` | Check status of all services |
| `task gcp:logs:ws` | View WebSocket server logs |
| `task gcp:logs:backend` | View backend logs |
| `task gcp:redis:health` | Check Redis health |

---

## Service-Specific Commands

### WebSocket Server (odin-ws-go)

```bash
# Rebuild and restart (code changes)
task gcp:deployment:rebuild:ws

# Restart only (env var changes)
task gcp:services:restart:ws

# View logs
task gcp:logs:ws
```

### Redis (ws-redis)

```bash
# Restart
task gcp:redis:restart

# View logs
task gcp:redis:logs

# Health check
task gcp:redis:health
```

### Backend (odin-backend)

```bash
# Rebuild and restart (code changes)
task gcp:deployment:rebuild:backend

# Restart only (env var changes)
task gcp:services:restart:backend

# View logs
task gcp:logs:backend
```

---

## Configuration Files Location

| VM | Config Path |
|----|-------------|
| odin-ws-go | `/home/deploy/odin-ws/deployments/v1/gcp/distributed/ws/.env.production` |
| ws-redis | `/home/deploy/odin-ws/deployments/v1/gcp/distributed/redis/.env` |
| odin-backend | `/home/deploy/odin-ws/deployments/v1/gcp/distributed/backend/.env` |

---

## Verifying Changes

### Check if new code is running

```bash
# View container start time
gcloud compute ssh odin-ws-go --zone=us-central1-a \
  --command="sudo -u deploy docker ps --format 'table {{.Names}}\t{{.Status}}'"

# Check logs for startup message
task gcp:logs:ws
```

### Verify configuration loaded

```bash
# Check health endpoint
curl http://<WS_IP>:3004/health | jq

# Look for config in startup logs
gcloud compute ssh odin-ws-go --zone=us-central1-a \
  --command="sudo -u deploy docker logs odin-ws-multi 2>&1 | head -50"
```

---

## Troubleshooting

### Container won't start after rebuild

```bash
# Check container logs
gcloud compute ssh odin-ws-go --zone=us-central1-a \
  --command="sudo -u deploy docker logs odin-ws-multi --tail=100"

# Check if image built correctly
gcloud compute ssh odin-ws-go --zone=us-central1-a \
  --command="sudo -u deploy docker images | grep ws"
```

### Config not taking effect

1. Verify `.env` file has correct values
2. Ensure container was actually restarted (check start time)
3. Check logs for config validation errors

### Need to rollback

```bash
# SSH to VM
gcloud compute ssh odin-ws-go --zone=us-central1-a

# Switch to deploy user
sudo -i -u deploy
cd ~/odin-ws

# Checkout previous commit
git log --oneline -5  # find the commit to rollback to
git checkout <commit-hash>

# Rebuild
cd deployments/v1/gcp/distributed/ws
docker-compose build
docker-compose up -d
```
