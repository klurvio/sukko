# Runbook: Emergency Rollback to In-Memory Broadcast Bus

**Purpose**: Revert from Redis (multi-instance) to In-Memory (single-instance) broadcast bus
**Use Case**: Critical Redis failure, unresolvable performance issues, emergency rollback
**Time to Complete**: < 5 minutes
**Impact**: Temporary loss of horizontal scaling (revert to single instance)

---

## When to Use This Runbook

**Trigger Conditions**:
- ✅ Redis completely unavailable (all replicas down)
- ✅ Redis performance degraded beyond acceptable limits (>20ms latency)
- ✅ Redis security incident (potential breach)
- ✅ Unresolvable Redis configuration issue
- ✅ Emergency directive from incident commander

**Do NOT Use For**:
- ❌ Brief Redis outage (<5 minutes) - wait for auto-reconnect
- ❌ Single replica failure (Sentinel handles failover)
- ❌ Minor performance degradation (<15ms latency)
- ❌ Planned maintenance (use planned migration instead)

---

## Pre-Rollback Checklist

**Verify Trigger Condition** (30 seconds):
```bash
# Check if Redis truly unavailable
redis-cli -h <REDIS_IP> -a <PASSWORD> PING
# If PONG → Redis is UP, do NOT rollback!

# Check if Sentinel failing over
redis-cli -p 26379 SENTINEL masters
# If failover in progress → WAIT 10 seconds

# Check broadcast bus health
curl http://ws-server-1:3005/health | jq '.broadcast_bus'
# If healthy:true → do NOT rollback!
```

**Acknowledge Limitations** (understand impact):
- ⚠️ **Single Instance Only**: Horizontal scaling disabled
- ⚠️ **Must Remove 2nd Instance**: From load balancer (manual step)
- ⚠️ **Capacity Reduced**: 36K → 18K connections max
- ⚠️ **No Redundancy**: Single point of failure (no failover)

**Get Approval** (if time permits):
- Notify #incidents channel
- Get approval from incident commander or senior engineer
- Document decision in incident notes

---

## Rollback Procedure

### Step 1: Update Configuration (30 seconds)

**On ALL ws-server instances**:

```bash
# SSH to first instance
gcloud compute ssh ws-server-1 --zone=us-central1-a

# Update environment variable
sudo tee -a /etc/systemd/system/ws-server.service.d/override.conf <<EOF
[Service]
Environment="BROADCAST_BUS_TYPE=inmemory"
EOF

# Reload systemd
sudo systemctl daemon-reload
```

**Repeat for all instances** (or use parallel execution):
```bash
# Parallel update (faster)
for instance in ws-server-{1,2}; do
  gcloud compute ssh $instance --zone=us-central1-a --command='
    sudo tee -a /etc/systemd/system/ws-server.service.d/override.conf <<EOF
[Service]
Environment="BROADCAST_BUS_TYPE=inmemory"
EOF
    sudo systemctl daemon-reload
  ' &
done
wait
```

### Step 2: Rolling Restart (2 minutes)

**Important**: Restart instances ONE AT A TIME to maintain availability.

```bash
# Restart instance 1
gcloud compute ssh ws-server-1 --zone=us-central1-a \
  --command='sudo systemctl restart ws-server'

# Wait for health check (30 seconds)
sleep 30
curl http://ws-server-1:3005/health
# Expected: {"status":"healthy","broadcast_bus":{"type":"inmemory"}}

# If healthy, restart instance 2
gcloud compute ssh ws-server-2 --zone=us-central1-a \
  --command='sudo systemctl restart ws-server'

# Wait for health check
sleep 30
curl http://ws-server-2:3005/health
```

### Step 3: Remove Instance from Load Balancer (1 minute)

**Critical**: In-memory bus only works on single instance!

```bash
# Get backend service
gcloud compute backend-services describe ws-backend \
  --global --format=yaml

# Remove instance-2 from backend
gcloud compute backend-services remove-backend ws-backend \
  --instance-group=ws-server-group \
  --instance-group-zone=us-central1-a \
  --global

# Or via GCP Console:
# 1. Navigate to: Network Services → Load Balancing
# 2. Click: ws-backend
# 3. Remove: ws-server-2 from backend instances
# 4. Save
```

### Step 4: Verify Rollback Success (1 minute)

```bash
# Check health of remaining instance
curl http://ws-server-1:3005/health | jq '.'
# Expected:
# {
#   "status": "healthy",
#   "broadcast_bus": {
#     "type": "inmemory",
#     "healthy": true,
#     "connected": true
#   },
#   "connections": 12000,
#   "shards": 3
# }

# Check metrics
curl http://ws-server-1:3005/metrics | grep broadcast_bus_healthy
# Expected: broadcast_bus_healthy{type="inmemory"} 1

# Test client connectivity
# (Connect a test client, verify messages received)
```

### Step 5: Stop ws-server-2 (Optional, 30 seconds)

```bash
# Stop the now-unused instance to save costs
gcloud compute instances stop ws-server-2 --zone=us-central1-a

# Or via Console:
# Compute Engine → VM Instances → ws-server-2 → Stop
```

---

## Post-Rollback Actions

### Immediate (< 10 minutes)

**1. Verify System Stability**
```bash
# Monitor for 5 minutes
watch -n 10 'curl -s http://ws-server-1:3005/health | jq ".connections"'

# Check error rates
curl http://ws-server-1:3005/metrics | grep errors

# Verify client connections
# Expected: Existing clients reconnect automatically
```

**2. Update Monitoring**
```bash
# Silence Redis alerts (no longer relevant)
# Alertmanager: Silence broadcast_bus_* alerts for Redis

# Update dashboard
# Grafana: Switch to "Single Instance" view
```

**3. Communicate Status**
```bash
# Post to #incidents channel
"🔄 Rollback Complete: Reverted to in-memory broadcast bus
 - Current: Single instance (ws-server-1)
 - Capacity: 18K connections
 - Status: Stable
 - Next: Root cause analysis of Redis issue"
```

### Within 1 Hour

**4. Root Cause Analysis**
```bash
# Collect evidence (see redis-connection-issues.md)
# Document timeline
# Identify root cause
# Propose permanent fix
```

**5. Incident Report**
```markdown
# Incident: Redis Broadcast Bus Failure

**Date**: YYYY-MM-DD
**Duration**: XX minutes
**Impact**: Rolled back to single instance

## Timeline
- HH:MM - Alert: Redis connection down
- HH:MM - Investigation started
- HH:MM - Decision: Rollback to in-memory
- HH:MM - Rollback completed
- HH:MM - System stable

## Root Cause
[Describe what caused Redis failure]

## Resolution
Rolled back to in-memory broadcast bus (single instance)

## Prevention
[Action items to prevent recurrence]
```

**6. Plan Recovery**
```bash
# Two options:

# Option A: Fix Redis and re-deploy
- Fix root cause
- Test in staging
- Gradual rollout back to Redis

# Option B: Migrate to NATS
- Deploy NATS cluster
- Implement NatsBroadcastBus
- Shadow mode testing
- Migrate to NATS (better than Redis)
```

---

## Recovery Path (Redis → In-Memory → Redis)

**Timeline: 1-7 days**

### Day 1: Stabilize
- ✅ System running on in-memory bus
- ✅ Incident report completed
- ✅ Root cause identified

### Day 2-3: Fix Redis
- Fix identified issue (e.g., memory limit, network, config)
- Deploy fix to staging Redis
- Test for 24 hours

### Day 4: Re-deploy to Production (Staging)
```bash
# Deploy fixed Redis to staging
export BROADCAST_BUS_TYPE=redis
task gcp:deploy:staging

# Test for 24 hours
# Verify: No connection issues, latency <10ms
```

### Day 5-6: Production Migration
```bash
# Shadow mode (if confident)
# Publish to both in-memory AND Redis
# Subscribe from in-memory only

# Gradual rollout
# Day 5: 50% Redis
# Day 6: 100% Redis
```

### Day 7: Scale Back Up
```bash
# Re-add ws-server-2 to load balancer
gcloud compute instances start ws-server-2 --zone=us-central1-a

# Add to backend
gcloud compute backend-services add-backend ws-backend \
  --instance-group=ws-server-group \
  --instance-group-zone=us-central1-a \
  --global

# Verify both instances healthy
```

---

## Testing This Runbook

**Quarterly Rollback Drill**:

```bash
# Schedule during off-hours (low traffic)
# Follow this runbook exactly
# Measure time to complete
# Identify pain points
# Update runbook

# Rollback drill checklist:
- [ ] Notify team (this is a drill)
- [ ] Start timer
- [ ] Execute rollback procedure
- [ ] Stop timer (target: <5 minutes)
- [ ] Verify system stability
- [ ] Revert to original state
- [ ] Document findings
- [ ] Update runbook
```

---

## Common Issues & Solutions

### Issue: Health Check Still Shows Redis

**Symptom**: After restart, health endpoint shows `"type":"redis"`

**Solution**:
```bash
# Verify environment variable was set
sudo systemctl cat ws-server | grep BROADCAST_BUS_TYPE
# Expected: Environment="BROADCAST_BUS_TYPE=inmemory"

# If missing, manually set and restart
export BROADCAST_BUS_TYPE=inmemory
sudo systemctl restart ws-server
```

### Issue: Connections Drop After Rollback

**Symptom**: Client connections drop to 0

**Solution**:
```bash
# Check if load balancer routing correctly
gcloud compute backend-services get-health ws-backend --global
# Expected: ws-server-1 HEALTHY, ws-server-2 removed

# If both instances present, manually remove ws-server-2
# (See Step 3)
```

### Issue: Both Instances Running In-Memory

**Symptom**: Both instances show `"type":"inmemory"`

**Problem**: This breaks message delivery (messages split across instances)

**Solution**:
```bash
# Stop ws-server-2 immediately
gcloud compute instances stop ws-server-2 --zone=us-central1-a

# Remove from load balancer
# (See Step 3)
```

---

## Appendix: Comparison Matrix

| Aspect | In-Memory (After Rollback) | Redis (Before Rollback) |
|--------|----------------------------|-------------------------|
| **Instances** | 1 | 2 |
| **Capacity** | 18K connections | 36K connections |
| **Latency** | <5ms (lowest) | <10ms (acceptable) |
| **Redundancy** | None (SPOF) | Yes (instance failover) |
| **Complexity** | Lowest | Medium |
| **Cost** | $50/month | $150/month |
| **Horizontal Scaling** | No | Yes |

**Key Takeaway**: Rollback is **temporary**. Plan to scale back up within 1 week.

---

**Version**: 1.0
**Last Updated**: 2025-01-20
**Owner**: SRE Team
**Last Drill**: ☐ (Schedule first drill)
**Approval**: ☐ Engineering Lead
