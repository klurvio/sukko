#!/bin/bash
# Redis Sentinel Failover Test Script
# Usage: ./failover-test.sh
# Note: This script is for FUTURE use when Sentinel cluster is deployed
#       Single-node deployment does NOT support automatic failover

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "========================================"
echo "Redis Sentinel Failover Test"
echo "========================================"
echo ""

# Check if we're in Sentinel mode
REDIS_ADDRS="${REDIS_SENTINEL_ADDRS}"

if [ -z "$REDIS_ADDRS" ]; then
    echo -e "${RED}ERROR: REDIS_SENTINEL_ADDRS not set${NC}"
    echo "This script requires Sentinel configuration"
    exit 1
fi

# Count number of addresses
ADDR_COUNT=$(echo "$REDIS_ADDRS" | tr ',' '\n' | wc -l | tr -d ' ')

if [ "$ADDR_COUNT" -eq 1 ]; then
    echo -e "${YELLOW}⚠ WARNING: Single Redis address detected${NC}"
    echo "This appears to be a single-node deployment"
    echo "Failover testing requires Sentinel cluster (3+ nodes)"
    echo ""
    echo "Current configuration:"
    echo "  REDIS_SENTINEL_ADDRS=$REDIS_ADDRS"
    echo ""
    echo "To enable Sentinel failover:"
    echo "  1. Deploy 3 Redis nodes with Sentinel"
    echo "  2. Update REDIS_SENTINEL_ADDRS=node1:26379,node2:26379,node3:26379"
    echo "  3. Re-run this test"
    exit 1
fi

echo -e "${GREEN}✓ Sentinel configuration detected${NC}"
echo "Sentinel addresses: $REDIS_ADDRS"
echo ""

# Parse Sentinel addresses
IFS=',' read -ra SENTINELS <<< "$REDIS_ADDRS"

SENTINEL_1="${SENTINELS[0]}"
SENTINEL_2="${SENTINELS[1]}"
SENTINEL_3="${SENTINELS[2]}"

REDIS_PASSWORD="${REDIS_PASSWORD}"
REDIS_MASTER_NAME="${REDIS_MASTER_NAME:-mymaster}"

echo "Configuration:"
echo "  Master Name: $REDIS_MASTER_NAME"
echo "  Sentinel 1: $SENTINEL_1"
echo "  Sentinel 2: $SENTINEL_2"
echo "  Sentinel 3: $SENTINEL_3"
echo ""

# Function to run redis-cli on Sentinel
sentinel_cmd() {
    local host=$(echo "$1" | cut -d: -f1)
    local port=$(echo "$1" | cut -d: -f2)
    shift

    if [ -n "$REDIS_PASSWORD" ]; then
        redis-cli -h "$host" -p "$port" -a "$REDIS_PASSWORD" --no-auth-warning "$@"
    else
        redis-cli -h "$host" -p "$port" "$@"
    fi
}

# Function to get master address from Sentinel
get_master() {
    sentinel_cmd "$1" SENTINEL get-master-addr-by-name "$REDIS_MASTER_NAME" 2>/dev/null | paste -sd ":" -
}

# Step 1: Check Sentinel connectivity
echo "========================================"
echo "Step 1: Check Sentinel Connectivity"
echo "========================================"

for sentinel in "${SENTINELS[@]}"; do
    echo -n "Checking $sentinel... "
    if sentinel_cmd "$sentinel" ping | grep -q PONG; then
        echo -e "${GREEN}✓ OK${NC}"
    else
        echo -e "${RED}✗ FAILED${NC}"
        exit 1
    fi
done
echo ""

# Step 2: Get current master
echo "========================================"
echo "Step 2: Identify Current Master"
echo "========================================"

MASTER_ADDR=$(get_master "$SENTINEL_1")
if [ -z "$MASTER_ADDR" ]; then
    echo -e "${RED}ERROR: Could not determine master address${NC}"
    exit 1
fi

MASTER_HOST=$(echo "$MASTER_ADDR" | cut -d: -f1)
MASTER_PORT=$(echo "$MASTER_ADDR" | cut -d: -f2)

echo -e "Current Master: ${GREEN}${MASTER_ADDR}${NC}"
echo ""

# Step 3: Check master health
echo "========================================"
echo "Step 3: Check Master Health"
echo "========================================"

echo -n "Master ping... "
if redis-cli -h "$MASTER_HOST" -p "$MASTER_PORT" -a "$REDIS_PASSWORD" --no-auth-warning ping | grep -q PONG; then
    echo -e "${GREEN}✓ OK${NC}"
else
    echo -e "${RED}✗ FAILED${NC}"
    exit 1
fi

REPLICATION_INFO=$(redis-cli -h "$MASTER_HOST" -p "$MASTER_PORT" -a "$REDIS_PASSWORD" --no-auth-warning INFO replication)
ROLE=$(echo "$REPLICATION_INFO" | grep "role:" | cut -d: -f2 | tr -d '\r')
CONNECTED_SLAVES=$(echo "$REPLICATION_INFO" | grep "connected_slaves:" | cut -d: -f2 | tr -d '\r')

echo "Role: $ROLE"
echo "Connected Replicas: $CONNECTED_SLAVES"

if [ "$ROLE" != "master" ]; then
    echo -e "${RED}ERROR: Node is not a master (role=$ROLE)${NC}"
    exit 1
fi

if [ "$CONNECTED_SLAVES" -lt 2 ]; then
    echo -e "${YELLOW}⚠ WARNING: Less than 2 replicas connected${NC}"
    echo "Recommended: 2 replicas for HA"
fi
echo ""

# Step 4: Trigger failover
echo "========================================"
echo "Step 4: Trigger Manual Failover"
echo "========================================"

echo -e "${YELLOW}This will trigger a failover to a replica${NC}"
echo "The master will become a replica, and a replica will be promoted"
echo ""
read -p "Continue with failover test? (yes/no): " CONFIRM

if [ "$CONFIRM" != "yes" ]; then
    echo "Failover test cancelled"
    exit 0
fi

echo ""
echo "Triggering failover..."
FAILOVER_RESULT=$(sentinel_cmd "$SENTINEL_1" SENTINEL failover "$REDIS_MASTER_NAME" 2>&1)

if echo "$FAILOVER_RESULT" | grep -q "OK"; then
    echo -e "${GREEN}✓ Failover initiated${NC}"
else
    echo -e "${RED}✗ Failover failed${NC}"
    echo "Result: $FAILOVER_RESULT"
    exit 1
fi

# Step 5: Monitor failover progress
echo ""
echo "========================================"
echo "Step 5: Monitor Failover Progress"
echo "========================================"

echo "Waiting for failover to complete (max 30s)..."
sleep 2

MAX_WAIT=30
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    NEW_MASTER=$(get_master "$SENTINEL_1")

    if [ "$NEW_MASTER" != "$MASTER_ADDR" ] && [ -n "$NEW_MASTER" ]; then
        echo -e "${GREEN}✓ Failover complete${NC}"
        echo "Old Master: $MASTER_ADDR"
        echo "New Master: $NEW_MASTER"
        break
    fi

    echo -n "."
    sleep 1
    WAITED=$((WAITED + 1))
done

if [ $WAITED -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Failover timeout${NC}"
    echo "Failover did not complete within ${MAX_WAIT}s"
    exit 1
fi

echo ""

# Step 6: Verify new master
echo "========================================"
echo "Step 6: Verify New Master"
echo "========================================"

NEW_MASTER_HOST=$(echo "$NEW_MASTER" | cut -d: -f1)
NEW_MASTER_PORT=$(echo "$NEW_MASTER" | cut -d: -f2)

echo -n "New master ping... "
if redis-cli -h "$NEW_MASTER_HOST" -p "$NEW_MASTER_PORT" -a "$REDIS_PASSWORD" --no-auth-warning ping | grep -q PONG; then
    echo -e "${GREEN}✓ OK${NC}"
else
    echo -e "${RED}✗ FAILED${NC}"
    exit 1
fi

NEW_ROLE=$(redis-cli -h "$NEW_MASTER_HOST" -p "$NEW_MASTER_PORT" -a "$REDIS_PASSWORD" --no-auth-warning INFO replication | grep "role:" | cut -d: -f2 | tr -d '\r')
echo "Role: $NEW_ROLE"

if [ "$NEW_ROLE" != "master" ]; then
    echo -e "${RED}ERROR: New node is not a master${NC}"
    exit 1
fi

# Step 7: Verify old master became replica
echo ""
echo "========================================"
echo "Step 7: Verify Old Master Demoted"
echo "========================================"

sleep 2  # Give old master time to reconfigure

echo -n "Old master ($MASTER_ADDR) ping... "
if redis-cli -h "$MASTER_HOST" -p "$MASTER_PORT" -a "$REDIS_PASSWORD" --no-auth-warning ping | grep -q PONG; then
    echo -e "${GREEN}✓ OK${NC}"
else
    echo -e "${YELLOW}⚠ Old master not responding${NC}"
fi

OLD_ROLE=$(redis-cli -h "$MASTER_HOST" -p "$MASTER_PORT" -a "$REDIS_PASSWORD" --no-auth-warning INFO replication 2>/dev/null | grep "role:" | cut -d: -f2 | tr -d '\r' || echo "unknown")
echo "Role: $OLD_ROLE"

if [ "$OLD_ROLE" = "slave" ]; then
    echo -e "${GREEN}✓ Old master successfully demoted to replica${NC}"
elif [ "$OLD_ROLE" = "master" ]; then
    echo -e "${YELLOW}⚠ Old master still in master role (may take a moment)${NC}"
else
    echo -e "${YELLOW}⚠ Could not determine old master role${NC}"
fi

# Step 8: Check Sentinel consensus
echo ""
echo "========================================"
echo "Step 8: Verify Sentinel Consensus"
echo "========================================"

CONSENSUS=true
for sentinel in "${SENTINELS[@]}"; do
    REPORTED_MASTER=$(get_master "$sentinel")
    echo "$sentinel reports master: $REPORTED_MASTER"

    if [ "$REPORTED_MASTER" != "$NEW_MASTER" ]; then
        CONSENSUS=false
    fi
done

if $CONSENSUS; then
    echo -e "${GREEN}✓ All Sentinels agree on new master${NC}"
else
    echo -e "${YELLOW}⚠ Sentinels not in consensus (may take a moment)${NC}"
fi

# Summary
echo ""
echo "========================================"
echo -e "${GREEN}Failover Test Complete ✓${NC}"
echo "========================================"
echo "Summary:"
echo "  Original Master: $MASTER_ADDR"
echo "  New Master: $NEW_MASTER"
echo "  Failover Time: ${WAITED}s"
echo "  Sentinel Consensus: $(${CONSENSUS} && echo 'YES' || echo 'NO')"
echo ""
echo "Next Steps:"
echo "  1. Monitor ws-server logs for reconnection"
echo "  2. Verify client connections still work"
echo "  3. Check Grafana dashboard for latency spike"
echo "  4. Run load test to verify performance"
echo ""

exit 0
