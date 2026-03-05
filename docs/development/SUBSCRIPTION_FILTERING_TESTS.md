# Hierarchical Subscription Filtering Load Tests

## Overview

The WebSocket server implements **hierarchical subscription filtering** to reduce CPU overhead from broadcast operations. Instead of sending every message to every client, the server only sends messages to clients subscribed to specific channels with event-type granularity.

**Channel Format**: `{SYMBOL}.{EVENT_TYPE}`
- Examples: `BTC.trade`, `ETH.liquidity`, `SOL.analytics`
- Event types: trade, liquidity, metadata, social, favorites, creation, analytics, balances

**Performance Impact:**
- **Without filtering**: 10K clients × 12 msg/sec × 8 event types = 960K writes/sec → CPU overload
- **With symbol-only filtering**: 10K clients × 12 msg/sec = 120K writes/sec → 99%+ CPU
- **With hierarchical filtering**: 500 avg subscribers × 12 msg/sec = 6K writes/sec → <30% CPU
- **Result**: 160x reduction vs no filtering, 20x reduction vs symbol-only filtering

This document explains how to test the hierarchical subscription filtering functionality using the updated test scripts.

---

## Test Scripts

### 1. `test-subscription-filtering.cjs` (Functional Tests)

**Purpose**: Validate that hierarchical subscription filtering works correctly

**Test Scenarios:**
1. **Basic Filtering** - Two clients subscribe to different hierarchical channels, verify selective delivery
2. **Unsubscribe** - Client unsubscribes from specific event type mid-test, stops receiving those messages
3. **Multiple Subscriptions** - Client subscribes to 5+ hierarchical channels, receives all
4. **No Subscription** - Client without subscriptions receives nothing

**Channel Format**: All tests use hierarchical format `{SYMBOL}.{EVENT_TYPE}` (e.g., `BTC.trade`, `ETH.trade`)

**Usage:**
```bash
# Run against local server
node scripts/test-subscription-filtering.cjs

# Run against deployed server
WS_URL=ws://34.61.200.145:3004/ws NATS_URL=nats://34.61.200.145:4222 \
  node scripts/test-subscription-filtering.cjs
```

**Expected Output:**
```
╔═══════════════════════════════════════════════════════════╗
║     WebSocket Subscription Filtering Test Suite          ║
╚═══════════════════════════════════════════════════════════╝

━━━ Test 1: Basic Subscription Filtering ━━━
✅ Test 1 PASSED: Clients received only subscribed messages

━━━ Test 2: Unsubscribe Functionality ━━━
✅ Test 2 PASSED: Unsubscribe stopped messages from BTC

━━━ Test 3: Multiple Subscriptions ━━━
✅ Test 3 PASSED: Received all messages from subscribed channels

━━━ Test 4: No Subscription = No Messages ━━━
✅ Test 4 PASSED: No subscriptions = no messages received

╔═══════════════════════════════════════════════════════════╗
║                     Test Summary                          ║
╚═══════════════════════════════════════════════════════════╝
  ✅ PASS - Basic Filtering
  ✅ PASS - Unsubscribe
  ✅ PASS - Multiple Subscriptions
  ✅ PASS - No Subscription

Total: 4/4 tests passed

🎉 All tests passed! Subscription filtering is working correctly.
```

---

### 2. `sustained-load-test.cjs` (Load/Capacity Tests)

**Purpose**: Test server stability and CPU usage under load with subscription filtering

**New Features Added:**
- ✅ Automatic subscription on connection
- ✅ Multiple subscription distribution modes (all, single, random)
- ✅ Configurable channels via environment variables
- ✅ Subscription metrics tracking
- ✅ Subscription acknowledgment handling

---

## Subscription Modes

### Mode 1: All Clients Subscribe to All Channels (default)

**Use Case**: Testing worst-case subscription overhead (minimal fanout reduction)

```bash
# Default behavior - all clients subscribe to BTC.trade,ETH.trade,SOL.trade,SUKKO.trade,DOGE.trade
TARGET_CONNECTIONS=5000 DURATION=300 node scripts/sustained-load-test.cjs

# Or explicitly set with multiple event types
SUBSCRIPTION_MODE=all CHANNELS=BTC.trade,BTC.liquidity,ETH.trade,ETH.analytics \
  TARGET_CONNECTIONS=5000 DURATION=300 node scripts/sustained-load-test.cjs
```

**Behavior:**
- All 5000 clients subscribe to all specified hierarchical channels
- Expected message rate depends on event type publishing rates
- Trade events: ~12 msg/sec per symbol
- Analytics events: ~1 msg/min per symbol

**Metrics to Watch:**
- CPU should be moderate (filtering still reduces overhead vs broadcasting all event types)
- All subscriptions should confirm (5000 × number of channels)
- Message rate should match total clients × message rate per subscribed event type

---

### Mode 2: Single Channel per Client (best fanout reduction)

**Use Case**: Realistic production scenario - most users watch specific event types for specific tokens

```bash
# Each client subscribes to ONE hierarchical channel (round-robin distribution)
SUBSCRIPTION_MODE=single CHANNELS=BTC.trade,ETH.trade,SOL.trade,SUKKO.trade,DOGE.trade \
  TARGET_CONNECTIONS=10000 DURATION=300 node scripts/sustained-load-test.cjs
```

**Behavior:**
- Client 0: subscribes to BTC.trade
- Client 1: subscribes to ETH.trade
- Client 2: subscribes to SOL.trade
- Client 3: subscribes to SUKKO.trade
- Client 4: subscribes to DOGE.trade
- Client 5: subscribes to BTC.trade (wraps around)
- ...

**Expected Results:**
- 10,000 clients / 5 channels = 2,000 subscribers per channel
- Message fanout: 2,000 clients × 12 msg/sec = 24K msg/sec (vs 960K without hierarchical filtering!)
- **CPU usage should be <30%** (vs 99%+ without hierarchical filtering)

**Hierarchical Filtering Advantage:**
- Users watching BTC.trade don't receive BTC.liquidity, BTC.analytics, etc.
- 8x reduction compared to symbol-only filtering
- 160x reduction compared to no filtering

**Metrics to Watch:**
```
🔔 Subscriptions:
   Mode:         single
   Channels:     BTC.trade,ETH.trade,SOL.trade,SUKKO.trade,DOGE.trade (5 total)
   Sent:         10,000
   Confirmed:    10,000
   Success Rate: 100.0%

📨 Messages:
   Received:     1,200,000  (after 100 seconds: 2K subscribers × 12 msg/sec × 5 channels × 10s)
   Rate:         12.00 msg/sec (per client average)

💻 Server Health:
   CPU:          28.3%  ← KEY METRIC (should be <30%)
   Memory:       45.2%
```

---

### Mode 3: Random Channels per Client (realistic variety)

**Use Case**: Users watching multiple event types across tokens (portfolio tracking, analytics dashboards)

```bash
# Each client subscribes to 2 random hierarchical channels
SUBSCRIPTION_MODE=random CHANNELS_PER_CLIENT=2 \
  CHANNELS=BTC.trade,ETH.trade,SOL.trade,BTC.liquidity,ETH.analytics,SOL.social,SUKKO.trade,DOGE.analytics \
  TARGET_CONNECTIONS=10000 DURATION=300 node scripts/sustained-load-test.cjs
```

**Behavior:**
- Client 0: subscribes to [BTC.trade, ETH.analytics] (random selection)
- Client 1: subscribes to [SOL.social, DOGE.analytics] (random selection)
- Client 2: subscribes to [BTC.liquidity, ETH.trade] (random selection)
- ...

**Expected Results:**
- 10,000 clients × 2 channels = 20,000 subscriptions
- Avg subscribers per channel: 20,000 / 8 channels = 2,500
- Message fanout varies by event type:
  - Trade channels: 2,500 avg × 12 msg/sec = 30K msg/sec
  - Analytics channels: 2,500 avg × 1 msg/min = 42 msg/sec
- **CPU usage should be ~35%** (slightly higher than 'single' mode, varies with event mix)

**Realistic Production Patterns:**
```bash
# Dashboard users watching prices + analytics for 3 tokens
SUBSCRIPTION_MODE=random CHANNELS_PER_CLIENT=6 \
  CHANNELS=BTC.trade,BTC.analytics,ETH.trade,ETH.analytics,SOL.trade,SOL.analytics \
  TARGET_CONNECTIONS=10000 DURATION=300 node scripts/sustained-load-test.cjs
```

**Metrics to Watch:**
- Subscription success rate should be 100%
- Each client receives messages only from subscribed event types
- CPU scales with both CHANNELS_PER_CLIENT and event type publishing rates
- Trade-heavy subscriptions increase CPU more than analytics-heavy subscriptions

---

### Mode 4: No Subscriptions (baseline comparison)

**Use Case**: Testing original behavior (no filtering) for baseline comparison

```bash
# Disable subscription filtering
CHANNELS= TARGET_CONNECTIONS=2500 DURATION=300 \
  node scripts/sustained-load-test.cjs
```

**Behavior:**
- Clients connect but don't subscribe to any channels
- Server broadcasts to ALL clients (no filtering applied)
- Same as original implementation (before subscription filtering)

**Expected Results:**
- No subscriptions sent/confirmed
- All clients receive all messages
- High CPU usage (baseline for comparison)

**Metrics to Watch:**
```
⚠️  Subscription Filtering: DISABLED (all clients receive all messages)

📨 Messages:
   Received:     30,000  (2,500 clients × 12 msg/sec = 30K msg/sec total)

💻 Server Health:
   CPU:          99.2%  ← Baseline (without filtering)
```

---

## Test Scenarios and Expected Results

### Scenario 1: Verify Subscription Filtering Reduces CPU

**Goal**: Prove that subscription filtering reduces CPU from 99% to <30%

**Test Steps:**
1. Run baseline test WITHOUT subscriptions (2500 connections)
2. Run test WITH single-channel subscriptions (10,000 connections)
3. Compare CPU usage

```bash
# Step 1: Baseline (no filtering) - expect 99% CPU at 2500 connections
CHANNELS= TARGET_CONNECTIONS=2500 DURATION=120 \
  node scripts/sustained-load-test.cjs

# Record: CPU usage, message rate
# Expected: CPU ~99%, 2500 clients × 12 msg/sec = 30K msg/sec

# Step 2: With filtering - expect <30% CPU at 10,000 connections
SUBSCRIPTION_MODE=single TARGET_CONNECTIONS=10000 DURATION=120 \
  node scripts/sustained-load-test.cjs

# Record: CPU usage, message rate
# Expected: CPU <30%, effective msg rate = 2000 × 12 × 5 channels = 120K/sec
```

**Success Criteria:**
- ✅ Baseline: 99% CPU at 2,500 connections (30K msg/sec fanout)
- ✅ Filtered: <30% CPU at 10,000 connections (24K msg/sec fanout)
- ✅ Result: 4x more clients with 70% less CPU usage

---

### Scenario 2: Capacity Test - Find Maximum Connections

**Goal**: Determine max connections with subscription filtering

**Test Steps:**
```bash
# Test 1: 5,000 connections (should succeed easily)
SUBSCRIPTION_MODE=single TARGET_CONNECTIONS=5000 DURATION=300 \
  node scripts/sustained-load-test.cjs

# Test 2: 10,000 connections (target goal)
SUBSCRIPTION_MODE=single TARGET_CONNECTIONS=10000 DURATION=300 \
  node scripts/sustained-load-test.cjs

# Test 3: 15,000 connections (stretch goal)
SUBSCRIPTION_MODE=single TARGET_CONNECTIONS=15000 DURATION=300 \
  node scripts/sustained-load-test.cjs
```

**Success Criteria:**
- ✅ 5K: 100% success rate, CPU <20%
- ✅ 10K: 100% success rate, CPU <30%
- ❓ 15K: Expect rejections if exceeding WS_MAX_CONNECTIONS (18K)

---

### Scenario 3: Realistic Production Simulation

**Goal**: Simulate real-world usage patterns

**Production Pattern Assumptions:**
- 70% of users watch 1 token (whales, focused traders)
- 20% of users watch 2-3 tokens (portfolio managers)
- 10% of users watch 5+ tokens (dashboards, bots)

**Test Configuration:**
```bash
# Simulate 10K production users with realistic distribution
SUBSCRIPTION_MODE=random CHANNELS_PER_CLIENT=2 \
  CHANNELS=BTC,ETH,SOL,SUKKO,DOGE,ADA,DOT,AVAX,MATIC,LINK \
  TARGET_CONNECTIONS=10000 DURATION=600 \
  node scripts/sustained-load-test.cjs
```

**Expected Results:**
- 10K clients × 2 avg channels = 20K subscriptions
- Avg 2,000 subscribers per channel (10 channels)
- Message fanout: 2,000 × 12 msg/sec × 10 channels = 240K msg/sec
- **CPU: 30-40%** (2x more than single-channel mode)
- **Should sustain for 10+ minutes without degradation**

---

## Monitoring and Metrics

### Key Metrics to Track

**From Test Script Output:**
```
📊 SUSTAINED LOAD TEST - Elapsed: 120s - Phase: SUSTAINING

🔌 Connections:
   Active:       10000 / 10000 target       ← Should be 100%
   Success Rate: 100.0%                     ← Should be 100%

🔔 Subscriptions:
   Mode:         single
   Channels:     BTC,ETH,SOL,SUKKO,DOGE
   Sent:         10000                      ← One per client
   Confirmed:    10000                      ← Should match sent
   Success Rate: 100.0%                     ← Should be 100%

📨 Messages:
   Received:     14,400,000                 ← Total across all clients
   Rate:         12.00 msg/sec              ← Per-client average
   ⚠️  Pre-sub msgs: 0                      ← Should be 0 (filtering works!)

💻 Server Health:
   CPU:          28.3%                      ← KEY: Should be <30% (vs 99% baseline)
   Memory:       45.2%                      ← Should be <70%
```

**From Grafana Dashboard:**
- `ws_active_connections` - Should match test target
- `ws_cpu_usage_percent` - Should be <30% with filtering
- `ws_broadcasts_total` - Rate should match publisher (12 msg/sec)
- `ws_messages_sent_total` - Rate should be broadcasts × avg_subscribers_per_channel

---

## Troubleshooting

### Issue: Subscriptions not confirming

**Symptoms:**
```
🔔 Subscriptions:
   Sent:         10000
   Confirmed:    0          ← Problem!
   Failed:       0
```

**Possible Causes:**
1. Server doesn't have subscription handlers implemented
2. WebSocket connection closing before ack received
3. Server rejecting subscription messages

**Debug Steps:**
```bash
# Check server logs for subscription messages
docker logs sukko-go | grep -i subscribe

# Test with single client manually
wscat -c ws://localhost:3004/ws
> {"type":"subscribe","data":{"channels":["BTC"]}}
# Should receive: {"type":"subscription_ack","subscribed":["BTC"],"count":1}
```

---

### Issue: High CPU despite filtering

**Symptoms:**
```
💻 Server Health:
   CPU:          95.3%     ← Expected <30%, getting 95%!
```

**Possible Causes:**
1. Subscription filtering not working (all clients still receiving all messages)
2. Too many clients subscribed to same channel
3. SUBSCRIPTION_MODE=all (no fanout reduction)

**Debug Steps:**
```bash
# 1. Check messages_filtered_out metric
# If > 0, filtering might not be working

# 2. Verify subscription mode
echo $SUBSCRIPTION_MODE  # Should be 'single' for best reduction

# 3. Check server broadcast logs (enable debug logging)
docker exec sukko-go grep "Subscription filtering" /logs

# 4. Count subscribers per channel (should be ~2000 for 10K clients / 5 channels)
```

---

### Issue: Clients receiving wrong messages

**Symptoms:**
```
📨 Messages:
   ⚠️  Pre-sub msgs: 5000   ← Should be 0!
```

**Possible Causes:**
1. Messages sent before subscription ack received
2. Server broadcasting before filtering applied
3. Race condition in subscription registration

**Fix:**
- Ensure server applies filtering immediately after subscription
- Client should wait for subscription_ack before counting messages

---

## Next Steps

After validating subscription filtering locally:

1. ✅ **Functional Tests** - Run `test-subscription-filtering.cjs` (all 4 tests pass)
2. ✅ **Local Load Test** - Run 1K connections with subscriptions
3. ⏳ **Deploy to GCP** - Update docker-compose on e2-standard-2 instance
4. ⏳ **Capacity Test** - Progressive testing (1K → 5K → 10K → 15K)
5. ⏳ **Production Simulation** - 10K clients with realistic subscription patterns
6. ⏳ **Document Results** - Update capacity reports with new limits

---

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WS_URL` | `ws://localhost:3004/ws` | WebSocket server URL |
| `HEALTH_URL` | `http://localhost:3004/health` | Health check endpoint |
| `TARGET_CONNECTIONS` | `18000` | Number of connections to create |
| `RAMP_RATE` | `100` | Connections per second during ramp-up |
| `DURATION` | `1800` | Sustain duration in seconds (30 min) |
| `CHANNELS` | `BTC.trade,ETH.trade,SOL.trade,SUKKO.trade,DOGE.trade` | Comma-separated hierarchical channel list (SYMBOL.EVENT_TYPE) |
| `SUBSCRIPTION_MODE` | `all` | Distribution: `all`, `single`, `random` |
| `CHANNELS_PER_CLIENT` | `3` | Channels per client (random mode only) |

### Hierarchical Channel Format

**Pattern**: `{SYMBOL}.{EVENT_TYPE}`

**Event Types**:
- `trade` - High-frequency trading data (~12 msg/sec)
- `liquidity` - Liquidity pool updates (~1 msg/min)
- `metadata` - Token metadata changes (~1 msg/day)
- `social` - Comments, reactions (~variable)
- `favorites` - User bookmarks (~variable)
- `creation` - Token creation events (~rare)
- `analytics` - Statistical updates (~1 msg/min)
- `balances` - Wallet balance changes (~variable)

### Quick Reference

```bash
# Test hierarchical subscription filtering works (functional)
node scripts/test-subscription-filtering.cjs

# Baseline test (no filtering) - 2500 connections
CHANNELS= TARGET_CONNECTIONS=2500 DURATION=120 node scripts/sustained-load-test.cjs

# Optimized test (single trade channel per client) - 10K connections
SUBSCRIPTION_MODE=single TARGET_CONNECTIONS=10000 DURATION=300 \
  node scripts/sustained-load-test.cjs

# Realistic test (random 2 event types per client) - 10K connections
SUBSCRIPTION_MODE=random CHANNELS_PER_CLIENT=2 \
  CHANNELS=BTC.trade,ETH.trade,SOL.analytics,SUKKO.social \
  TARGET_CONNECTIONS=10000 DURATION=300 node scripts/sustained-load-test.cjs

# Dashboard simulation (price + analytics for 3 tokens) - 10K connections
SUBSCRIPTION_MODE=all \
  CHANNELS=BTC.trade,BTC.analytics,ETH.trade,ETH.analytics,SOL.trade,SOL.analytics \
  TARGET_CONNECTIONS=10000 DURATION=300 node scripts/sustained-load-test.cjs

# Stress test - find breaking point
SUBSCRIPTION_MODE=single TARGET_CONNECTIONS=20000 RAMP_RATE=200 DURATION=300 \
  node scripts/sustained-load-test.cjs
```

---

## Success Criteria Summary

| Test | Without Filtering | Symbol-Only Filtering | Hierarchical Filtering (single) | Improvement |
|------|------------------|----------------------|--------------------------------|-------------|
| **Max Connections** | 500 @ 99% CPU | 2,500 @ 99% CPU | 10,000 @ <30% CPU | **20x vs none, 4x vs symbol** |
| **Message Fanout** | 960K writes/sec | 120K writes/sec | 6K writes/sec | **160x vs none, 20x vs symbol** |
| **CPU Efficiency** | 99% @ 500 | 99% @ 2.5K | 30% @ 10K | **70% reduction** |
| **Connections per vCPU** | 250 | 1,250 | 5,000 | **20x improvement** |

**Hierarchical Filtering Advantages:**
- **8x reduction** over symbol-only filtering (specific event type targeting)
- **160x reduction** over no filtering (massive broadcast savings)
- Users watching trade data don't receive social/analytics updates
- Critical for production bandwidth optimization

**Target Goals:**
- ✅ Support 10,000 concurrent connections on e2-standard-2 (2 vCPU, 8 GB)
- ✅ CPU usage <30% under normal load (trade events at 12 msg/sec)
- ✅ 100% subscription confirmation rate for hierarchical channels
- ✅ Zero messages received before subscription (filtering works)
- ✅ Sustained stability for 30+ minutes without degradation
- ✅ Support 8 distinct event types per symbol
