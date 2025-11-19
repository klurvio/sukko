# WS Server Refactoring - Performance Baseline

**Date:** 2025-11-10
**Branch:** kafka-refactor (commit: 5787ddb)
**Test:** Capacity test with 12K target, 100 conn/sec ramp rate

## Baseline Performance Metrics

### Peak Capacity (Before CPU Limit)
```
Peak Connections:     3,941 active
Total Attempted:      6,930 connections
Failed Connections:   2,979 (43% - due to CPU saturation)
Success Rate:         57.0%
Time to Peak:         ~90 seconds
```

### Resource Usage @ Peak
```
CPU:                  99.6% (hits 100%, then drops as connections fail)
Memory:               1.1% (~164MB of 14.8GB limit)
Goroutines:           ~3,963 (3,941 clients + 22 system)
Memory per Client:    ~42KB (164MB / 3,941)
```

### Message Throughput @ Peak
```
Message Rate:         19,043 msg/sec
Messages Received:    1,713,956 total
Errors:               0 (0.00%)
```

### Subscription Performance
```
Mode:                 all (every client subscribes to all 5 channels)
Channels:             [BTC.trade, ETH.trade, SOL.trade, ODIN.trade, DOGE.trade]
Subscriptions Sent:   3,941
Confirmed:            3,931 (99.7%)
Failed:               0
```

### Connection Stability
```
During ramp:          CPU fluctuates 12-100% as connections added
At plateau:           CPU drops to 12.7% after failed connections stop
Behavior:             Server rejects new connections when CPU >75%
```

## Performance Budget for Refactoring

**Zero Tolerance for Regression** - Any metric degradation >2% triggers rollback.

| Metric | Baseline | Min Acceptable | Max Acceptable | Rollback Trigger |
|--------|----------|----------------|----------------|------------------|
| Peak Connections | 3,941 | 3,862 (-2%) | 4,020 (+2%) | <3,850 or >4,050 |
| CPU @ Peak | 99.6% | - | 100.5% | >100.5% |
| Memory @ Peak | 1.1% (164MB) | - | 1.3% (193MB) | >1.3% |
| Message Rate | 19,043/sec | 18,662/sec | 19,424/sec | <18,500/sec |
| Subscription Success | 99.7% | 98.5% | 100% | <98% |

## Hot Path Analysis

### CPU Distribution (Estimated from profiling)
```
Component               CPU %    Notes
───────────────────────────────────────────────────────────────
writePump goroutines    ~97%     WebSocket writes (3,941 goroutines)
broadcast() iteration   ~2%      Non-blocking channel sends
Kafka consumer          ~1%      Franz-go + processRecord
Other                   <0.1%    Metrics, monitoring, etc.
───────────────────────────────────────────────────────────────
TOTAL                   100%
```

### Memory Distribution
```
Component                   Memory      Per-Client
──────────────────────────────────────────────────
send channels (512×bytes)   ~160MB      ~41KB
Client structs              ~800KB      ~200 bytes
Connection pool             ~2MB        ~500 bytes
Subscription index          ~1MB        ~250 bytes
Kafka consumer              ~20MB       ~5KB
──────────────────────────────────────────────────
TOTAL                       ~164MB      ~42KB
```

## Critical Performance Constraints

### 1. Single Serialization (broadcast.go)
**Current:** 1 json.Marshal per message (25/sec)
**Impact:** ~7.5ms CPU/sec = 0.75% CPU
**Constraint:** Must NOT add per-client serialization

### 2. Subscription Index (connection.go)
**Current:** O(subscribers) lookup, not O(all_clients)
**Impact:** 93% reduction in broadcast iterations
**Constraint:** Must preserve subscription index architecture

### 3. Non-blocking Sends (broadcast.go)
**Current:** select{} with default case (never blocks)
**Impact:** Keeps broadcast latency <1ms
**Constraint:** Must NOT introduce blocking operations

### 4. writePump Goroutine Structure (handlers.go)
**Current:** 1 goroutine per client, buffered channel reads
**Impact:** 97% of total CPU
**Constraint:** Must preserve EXACT goroutine structure

## Code Size Analysis

```
File                Lines    After Refactor    Extraction Target
────────────────────────────────────────────────────────────────────
server.go           1,613    ~300             core/server.go
                             ~200             core/broadcast.go
                             ~300             core/handlers.go
                             ~250             messaging/protocol.go
                             ~150             (distributed to other files)

connection.go       454      454              core/connection.go (unchanged)
metrics.go          544      544              monitoring/metrics.go (moved)
resource_guard.go   409      409              limits/resource_guard.go (moved)
kafka/consumer.go   304      304              kafka/consumer.go (moved)
rate_limiter.go     264      264              limits/rate_limiter.go (moved)
```

## Test Plan for Each Phase

### Phase 2: Cold Path Movement
**Risk:** Low (init code, async monitoring)
**Test:** Build + manual smoke test
**Success:** Compiles, starts, serves health check

### Phase 3: broadcast.go Extraction
**Risk:** HIGH (2-3% CPU, critical hot path)
**Test:** 
1. Benchmark: `go test -bench=BenchmarkBroadcast`
2. Integration: Run capacity test
**Success:** 
- Latency <1.2ms per broadcast
- CPU ≤100.5%
- Connections ≥3,850

### Phase 4: writePump Extraction
**Risk:** CRITICAL (97% CPU, highest impact)
**Test:**
1. Benchmark: `go test -bench=BenchmarkWritePump`
2. Integration: Run capacity test
**Success:**
- Throughput ≥18,500 msg/sec
- CPU ≤100.5%
- Connections ≥3,850
- Goroutines ≤4,050

### Phase 5: protocol.go Extraction
**Risk:** Medium (client message handling)
**Test:** Integration test with subscriptions
**Success:** 
- Subscription success ≥98%
- No new errors in logs

### Phase 6: server.go Cleanup
**Risk:** Low (final polish)
**Test:** Full capacity test
**Success:** All baseline metrics within tolerance

## Rollback Plan

**Automatic Rollback Triggers:**
1. Any metric degrades >2% from baseline
2. New errors appear in logs during testing
3. Goroutine leaks detected (>4,050 goroutines)
4. Memory leaks detected (>1.3% usage)

**Rollback Procedure:**
```bash
# Revert commits
git reset --hard <baseline-commit>

# Rebuild and deploy
task gcp:deployment:rebuild:ws

# Verify baseline restored
task gcp:load-test:capacity:short
```

## Next Steps

1. ✅ **Phase 1 Complete** - Baseline documented
2. ⏳ **Phase 2 Next** - Create directory structure, move cold path files
3. ⏳ Phase 3 - Extract broadcast.go (CRITICAL)
4. ⏳ Phase 4 - Extract handlers.go (CRITICAL)
5. ⏳ Phase 5 - Extract protocol.go
6. ⏳ Phase 6 - Final cleanup

---

**Baseline Commit:** 5787ddb (fix: Use docker-compose in GCP deployment tasks)
**Test Date:** 2025-11-10 12:53-12:55 UTC
**Test Duration:** ~90 seconds to peak
