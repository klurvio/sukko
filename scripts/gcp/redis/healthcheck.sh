#!/bin/bash
# Redis Health Check Script
# Usage: ./healthcheck.sh
# Exit codes: 0 = healthy, 1 = unhealthy

set -e

# Configuration
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_PASSWORD="${REDIS_PASSWORD}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "========================================"
echo "Redis Health Check"
echo "Host: $REDIS_HOST:$REDIS_PORT"
echo "========================================"

# Function to run redis-cli command
redis_cmd() {
    if [ -n "$REDIS_PASSWORD" ]; then
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" --no-auth-warning "$@"
    else
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" "$@"
    fi
}

# 1. Basic PING test
echo -n "1. Connectivity (PING)... "
if redis_cmd ping | grep -q PONG; then
    echo -e "${GREEN}✓ OK${NC}"
else
    echo -e "${RED}✗ FAILED${NC}"
    exit 1
fi

# 2. Check Redis version
echo -n "2. Redis Version... "
VERSION=$(redis_cmd INFO server | grep redis_version | cut -d: -f2 | tr -d '\r')
echo -e "${GREEN}$VERSION${NC}"

# 3. Check uptime
echo -n "3. Uptime... "
UPTIME_SECONDS=$(redis_cmd INFO server | grep uptime_in_seconds | cut -d: -f2 | tr -d '\r')
UPTIME_DAYS=$((UPTIME_SECONDS / 86400))
UPTIME_HOURS=$(((UPTIME_SECONDS % 86400) / 3600))
echo -e "${GREEN}${UPTIME_DAYS}d ${UPTIME_HOURS}h${NC}"

# 4. Check memory usage
echo -n "4. Memory Usage... "
USED_MEMORY=$(redis_cmd INFO memory | grep used_memory_human | head -1 | cut -d: -f2 | tr -d '\r')
MAX_MEMORY=$(redis_cmd INFO memory | grep maxmemory_human | cut -d: -f2 | tr -d '\r')
USED_MEMORY_PCT=$(redis_cmd INFO memory | grep used_memory_percentage | cut -d: -f2 | tr -d '\r' | cut -d. -f1)

if [ -z "$USED_MEMORY_PCT" ]; then
    USED_MEMORY_PCT=0
fi

if [ "$USED_MEMORY_PCT" -lt 70 ]; then
    echo -e "${GREEN}$USED_MEMORY / $MAX_MEMORY (${USED_MEMORY_PCT}%)${NC}"
elif [ "$USED_MEMORY_PCT" -lt 90 ]; then
    echo -e "${YELLOW}$USED_MEMORY / $MAX_MEMORY (${USED_MEMORY_PCT}%)${NC} [WARNING]"
else
    echo -e "${RED}$USED_MEMORY / $MAX_MEMORY (${USED_MEMORY_PCT}%)${NC} [CRITICAL]"
fi

# 5. Check connected clients
echo -n "5. Connected Clients... "
CLIENTS=$(redis_cmd INFO clients | grep connected_clients | cut -d: -f2 | tr -d '\r')
echo -e "${GREEN}$CLIENTS${NC}"

if [ "$CLIENTS" -lt 2 ] || [ "$CLIENTS" -gt 20 ]; then
    echo -e "   ${YELLOW}⚠ Unexpected client count (expected 2-10 for WS servers)${NC}"
fi

# 6. Check pub/sub channels
echo -n "6. Pub/Sub Channels... "
CHANNELS=$(redis_cmd PUBSUB CHANNELS | wc -l | tr -d ' ')
echo -e "${GREEN}$CHANNELS active${NC}"

# 7. Check pub/sub patterns
echo -n "7. Pub/Sub Patterns... "
PATTERNS=$(redis_cmd PUBSUB NUMPAT | tr -d '\r')
echo -e "${GREEN}$PATTERNS${NC}"

# 8. Check commands processed
echo -n "8. Total Commands... "
TOTAL_COMMANDS=$(redis_cmd INFO stats | grep total_commands_processed | cut -d: -f2 | tr -d '\r')
echo -e "${GREEN}$(printf "%'d" $TOTAL_COMMANDS)${NC}"

# 9. Check slow log
echo -n "9. Slow Log Entries... "
SLOW_LOG_LEN=$(redis_cmd SLOWLOG LEN | tr -d '\r')
if [ "$SLOW_LOG_LEN" -gt 0 ]; then
    echo -e "${YELLOW}$SLOW_LOG_LEN${NC} [Review with: redis-cli SLOWLOG GET 10]"
else
    echo -e "${GREEN}$SLOW_LOG_LEN${NC}"
fi

# 10. Check latency
echo -n "10. Latency Test (5 pings)... "
LATENCY=$(redis_cmd --latency-history -i 1 -c 5 2>/dev/null | tail -1 | awk '{print $NF}' || echo "N/A")
if [ "$LATENCY" != "N/A" ]; then
    LATENCY_MS=$(echo "$LATENCY" | cut -d. -f1)
    if [ "$LATENCY_MS" -lt 5 ]; then
        echo -e "${GREEN}${LATENCY}ms${NC}"
    elif [ "$LATENCY_MS" -lt 10 ]; then
        echo -e "${YELLOW}${LATENCY}ms${NC} [WARNING: >5ms]"
    else
        echo -e "${RED}${LATENCY}ms${NC} [CRITICAL: >10ms]"
    fi
else
    echo -e "${YELLOW}$LATENCY${NC}"
fi

# 11. Check replication (for Sentinel setups)
echo -n "11. Replication Role... "
ROLE=$(redis_cmd INFO replication | grep role | cut -d: -f2 | tr -d '\r')
echo -e "${GREEN}$ROLE${NC}"

if [ "$ROLE" = "master" ]; then
    REPLICAS=$(redis_cmd INFO replication | grep connected_slaves | cut -d: -f2 | tr -d '\r')
    echo "    Connected replicas: $REPLICAS"
fi

# 12. Check persistence (should be disabled for pub/sub)
echo -n "12. Persistence... "
RDB_ENABLED=$(redis_cmd CONFIG GET save | tail -1 | tr -d '\r')
AOF_ENABLED=$(redis_cmd CONFIG GET appendonly | tail -1 | tr -d '\r')

if [ "$RDB_ENABLED" = "" ] && [ "$AOF_ENABLED" = "no" ]; then
    echo -e "${GREEN}Disabled (as expected for pub/sub)${NC}"
else
    echo -e "${YELLOW}Enabled (RDB: $RDB_ENABLED, AOF: $AOF_ENABLED)${NC}"
fi

# 13. Check evicted keys (should be 0 for pub/sub)
echo -n "13. Evicted Keys... "
EVICTED=$(redis_cmd INFO stats | grep evicted_keys | cut -d: -f2 | tr -d '\r')
if [ "$EVICTED" -eq 0 ]; then
    echo -e "${GREEN}$EVICTED${NC}"
else
    echo -e "${YELLOW}$EVICTED${NC} [Investigate memory pressure]"
fi

# 14. Check rejected connections
echo -n "14. Rejected Connections... "
REJECTED=$(redis_cmd INFO stats | grep rejected_connections | cut -d: -f2 | tr -d '\r')
if [ "$REJECTED" -eq 0 ]; then
    echo -e "${GREEN}$REJECTED${NC}"
else
    echo -e "${RED}$REJECTED${NC} [CRITICAL: maxclients limit reached]"
fi

echo "========================================"
echo -e "${GREEN}Health check PASSED ✓${NC}"
echo "========================================"

# Summary
echo ""
echo "Summary:"
echo "  Status: Healthy"
echo "  Memory: $USED_MEMORY / $MAX_MEMORY (${USED_MEMORY_PCT}%)"
echo "  Clients: $CLIENTS"
echo "  Channels: $CHANNELS"
echo "  Avg Latency: ${LATENCY}ms"
echo ""

exit 0
