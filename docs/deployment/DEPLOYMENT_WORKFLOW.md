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
cd ~/ws_poc/deployments/v1/gcp/distributed/ws
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

## Service-Specific Commands

### WebSocket Server (odin-ws-go)

```bash
# Rebuild and restart
task gcp:deployment:rebuild:ws

# Restart only (no code changes)
gcloud compute ssh odin-ws-go --zone=us-central1-a \
  --command="sudo -u deploy docker restart odin-ws-multi"

# View logs
task gcp:logs:ws
```

### Redis (ws-redis)

```bash
# Rebuild and restart
task gcp:redis:restart

# View logs
task gcp:redis:logs

# Health check
task gcp:redis:health
```

### Backend (odin-backend)

```bash
# Restart services
gcloud compute ssh odin-backend --zone=us-central1-a \
  --command="sudo -u deploy bash -c 'cd ~/ws_poc/deployments/v1/gcp/distributed/backend && docker-compose restart'"
```

---

## Configuration Files Location

| VM | Config Path |
|----|-------------|
| odin-ws-go | `/home/deploy/ws_poc/deployments/v1/gcp/distributed/ws/.env.production` |
| ws-redis | `/home/deploy/ws_poc/deployments/v1/gcp/distributed/redis/.env` |
| odin-backend | `/home/deploy/ws_poc/deployments/v1/gcp/distributed/backend/.env` |

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
cd ~/ws_poc

# Checkout previous commit
git log --oneline -5  # find the commit to rollback to
git checkout <commit-hash>

# Rebuild
cd deployments/v1/gcp/distributed/ws
docker-compose build
docker-compose up -d
```
