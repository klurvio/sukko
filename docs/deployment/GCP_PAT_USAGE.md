# GCP Deployment with GitHub PAT

## Quick Start

### 1. Deploy to GCP
The `deployments/v1/gcp/.env.gcp` file is automatically loaded by task - no manual sourcing needed!

**Note**: The deployment uses the `working-refactored-12k` branch by default. To deploy a different branch, set `GIT_BRANCH`:
```bash
GIT_BRANCH=main task gcp:deployment:setup:backend
```

### 2. Run Deployment Tasks
```bash
# Create infrastructure
task gcp:deployment:create:backend
task gcp:deployment:create:ws
task gcp:deployment:firewall:backend
task gcp:deployment:firewall:ws
task gcp:deployment:reserve-ip:ws

# Setup applications (PAT will be used automatically!)
task gcp:deployment:setup:backend
task gcp:deployment:setup:ws
```

### 3. Rebuild with Latest Code
```bash
# Rebuild backend
task gcp:deployment:rebuild:backend

# Rebuild WS server
task gcp:deployment:rebuild:ws
```

---

## How It Works

**Without PAT (will fail for private repos):**
```bash
# If deployments/v1/gcp/.env.gcp doesn't exist or GIT_PAT is empty
task gcp:deployment:setup:backend
# Shows: ⚠️  Using public clone (no PAT) - will fail for private repos
#        git clone https://github.com/Toniq-Labs/odin-ws.git
```

**With PAT (automatic from deployments/v1/gcp/.env.gcp):**
```bash
task gcp:deployment:setup:backend
# Shows: ✅ Using authenticated clone (PAT provided)
#        git clone https://ghp_xxxxx@github.com/Toniq-Labs/odin-ws.git
```

---

## PAT Details

**Location**: `.env.gcp` (not committed - see .gitignore)  
**Token**: `your_github_pat_here` (replace with actual read-only PAT)  
**Repository**: `https://github.com/Toniq-Labs/odin-ws`  
**Scope**: Read-only access (cannot push)

**Security:**
- ✅ `.env.gcp` is gitignored (won't be committed)
- ✅ Read-only PAT (safe if exposed)
- ✅ Passed as environment variable (not hardcoded)

---

## Common Workflows

### Complete First-Time Deployment
```bash
# Deploy everything (PAT loaded automatically)
task gcp:deploy
```

### Daily Operations  
```bash
# Start/stop services
task gcp:up
task gcp:down
task gcp:status
```

### Update Code After Changes
```bash
# Rebuild with latest from GitHub
task gcp:deployment:rebuild:all
```

---

## Troubleshooting

### "Using public clone (no PAT)" Warning
**Problem**: `deployments/v1/gcp/.env.gcp` file missing or `GIT_PAT` is empty  
**Solution**: Ensure `deployments/v1/gcp/.env.gcp` exists with `GIT_PAT` set

### "Permission denied" During Clone
**Problem**: PAT expired or invalid  
**Solution**: Generate new PAT at https://github.com/settings/tokens/new

### "Repository not found"
**Problem**: Wrong repository URL  
**Solution**: Check `.env.gcp` has correct repo URL

---

## For Production Later

Current setup is perfect for testing. For production, consider:

1. **GCP Secret Manager** - Store PAT centrally
2. **Deploy Keys** - Per-repository SSH keys
3. **Workload Identity** - GCP-native authentication

For now, the `.env.gcp` approach is simple and works great! 🎉

---

**Quick Reference:**
```bash
# Essential commands (PAT loaded automatically from deployments/v1/gcp/.env.gcp)
task gcp:deploy                              # Complete deployment
task gcp:deployment:rebuild:all              # Update code
task gcp:status                              # Check status
GIT_BRANCH=main task gcp:deploy              # Deploy specific branch
```
