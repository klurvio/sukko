# Runbook: Redis Connection Issues

**Alert**: `BroadcastBusDisconnected`
**Severity**: Critical
**SLA**: Resolve within 15 minutes

---

## Symptoms

- Alert firing: `broadcast_bus_connected{type="redis"} == 0`
- Health endpoint returns 503
- Logs showing: "Failed to connect to Redis" or "Redis connection closed"
- Messages not crossing between ws-server instances

---

## Immediate Actions (< 5 minutes)

### Step 1: Verify Redis Server Status

```bash
# SSH to Redis instance
gcloud compute ssh redis-server --zone=us-central1-a

# Check Redis processes
sudo systemctl status redis-master redis-replica-{1,2} redis-sentinel-{1,2,3}

# Expected: All services "active (running)"
# If ANY service is "failed" → proceed to Step 2
```

### Step 2: Check Redis Connectivity

```bash
# From ws-server instance, test connection
redis-cli -h <REDIS_INTERNAL_IP> -p 6379 -a <REDIS_PASSWORD> PING
# Expected: PONG

# If "Could not connect" → network issue (Step 3)
# If "NOAUTH" → password issue (Step 4)
# If timeout → Redis overloaded (Step 5)
```

### Step 3: Network Troubleshooting

```bash
# Test network connectivity
ping -c 5 <REDIS_INTERNAL_IP>
# Expected: 0% packet loss, <1ms latency

# Check firewall rules
gcloud compute firewall-rules list | grep redis
# Expected: allow-redis-internal rule exists

# Verify internal IP hasn't changed
gcloud compute instances describe redis-server --zone=us-central1-a | grep networkIP
# Expected: Matches REDIS_SENTINEL_ADDRS in config
```

### Step 4: Authentication Issues

```bash
# Verify password in config matches Redis
cat /etc/redis/password.txt
# Compare to ws-server env var: REDIS_PASSWORD

# If mismatch, update ws-server config:
export REDIS_PASSWORD=<CORRECT_PASSWORD>
task gcp:deploy:rolling-restart

# Monitor health endpoint for recovery
```

### Step 5: Redis Overloaded

```bash
# Check Redis CPU/memory
redis-cli -h <REDIS_IP> -a <PASSWORD> INFO stats
# Look for: used_cpu_sys, used_memory

# If CPU >80% or memory >450MB → resource issue
# Temporary fix: Scale up instance
gcloud compute instances stop redis-server --zone=us-central1-a
gcloud compute instances set-machine-type redis-server \
  --machine-type=e2-standard-4 --zone=us-central1-a
gcloud compute instances start redis-server --zone=us-central1-a
```

---

## Escalation Path

**If issue persists after 10 minutes**:

1. **Emergency Rollback to In-Memory**
   ```bash
   # Rollback to single-instance mode
   export BROADCAST_BUS_TYPE=inmemory
   task gcp:deploy:rolling-restart

   # Verify health
   curl http://ws-server-1:3005/health | jq '.broadcast_bus'
   # Expected: {"type":"inmemory","healthy":true}

   # Note: This reverts to single-instance architecture
   # Must remove 2nd instance from load balancer
   ```

2. **Page On-Call Engineer**
   - Subject: "Redis broadcast bus down >10 minutes"
   - Include: Alert output, Redis logs, network diagnostics

3. **Communicate Impact**
   - Post to #incidents channel
   - Notify stakeholders if customer-facing impact

---

## Root Cause Analysis (Post-Incident)

### Collect Evidence

```bash
# Redis logs (last 30 minutes)
sudo journalctl -u redis-master --since "30 minutes ago" > /tmp/redis-master.log
sudo journalctl -u redis-sentinel-1 --since "30 minutes ago" > /tmp/sentinel-1.log

# ws-server logs
gcloud logging read "resource.type=gce_instance \
  AND jsonPayload.component=redis_broadcast_bus \
  AND timestamp>=\"$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%SZ)\"" \
  --limit=1000 > /tmp/ws-server-redis.log

# Metrics snapshot
curl http://redis-server:9121/metrics > /tmp/redis-metrics.txt
```

### Common Root Causes

| Symptom | Root Cause | Prevention |
|---------|-----------|------------|
| All Redis processes crashed | Out of memory | Increase memory limit, tune eviction policy |
| Network timeout | Firewall rule changed | Infrastructure as Code, change review |
| Authentication failure | Password rotated without updating ws-server | Automated secret sync |
| Sentinel failover stuck | Split-brain, insufficient quorum | 3 Sentinels, proper network topology |

### Post-Incident Actions

1. **Update Runbook**: Document new learnings
2. **Improve Monitoring**: Add missing alerts
3. **Automate Recovery**: Script common fixes
4. **Test Failover**: Schedule chaos engineering test

---

## Prevention

### Proactive Monitoring

```yaml
# Alerts to prevent this incident
- RedisServerDown (detect before ws-server disconnect)
- RedisMemoryHigh (prevent OOM)
- RedisSentinelFailover (detect failover issues)
- NetworkLatencyHigh (detect network degradation)
```

### Regular Testing

```bash
# Monthly failover test (off-hours)
# Simulates Redis master crash
sudo systemctl stop redis-master

# Verify:
# - Sentinel promotes replica (<10s)
# - ws-servers auto-reconnect (<3s)
# - No message loss
# - Alerts fire correctly

# Restore
sudo systemctl start redis-master
```

### Configuration Review

```bash
# Quarterly review (checklist)
- [ ] Firewall rules unchanged
- [ ] Redis password in sync
- [ ] Memory limits appropriate
- [ ] Sentinel quorum correct (2/3)
- [ ] Backup/disaster recovery tested
```

---

## Related Runbooks

- `docs/runbooks/redis-server-down.md` - Redis server failure
- `docs/runbooks/sentinel-failover.md` - Manual failover procedure
- `docs/runbooks/rollback-to-inmemory.md` - Emergency rollback
- `docs/runbooks/high-latency.md` - Performance troubleshooting

---

**Version**: 1.0
**Last Updated**: 2025-01-20
**Owner**: SRE Team
**Tested**: ☐ (Schedule first drill)
