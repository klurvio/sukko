#!/bin/bash
# Redis Backup Script
# Usage: ./backup.sh [backup-dir]
# Note: For pub/sub workload, backups are not critical (data is ephemeral)
#       But useful for debugging and disaster recovery scenarios

set -e

# Configuration
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_PASSWORD="${REDIS_PASSWORD}"
BACKUP_DIR="${1:-./backups}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="redis_backup_${TIMESTAMP}.rdb"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "========================================"
echo "Redis Backup Script"
echo "========================================"

# Function to run redis-cli command
redis_cmd() {
    if [ -n "$REDIS_PASSWORD" ]; then
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" --no-auth-warning "$@"
    else
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" "$@"
    fi
}

# Create backup directory if it doesn't exist
mkdir -p "$BACKUP_DIR"

echo -n "1. Checking Redis connectivity... "
if ! redis_cmd ping | grep -q PONG; then
    echo -e "${RED}✗ FAILED${NC}"
    echo "Error: Cannot connect to Redis"
    exit 1
fi
echo -e "${GREEN}✓ OK${NC}"

# Check if persistence is enabled
echo -n "2. Checking persistence configuration... "
RDB_ENABLED=$(redis_cmd CONFIG GET save | tail -1 | tr -d '\r')
if [ "$RDB_ENABLED" = "" ]; then
    echo -e "${YELLOW}⚠ WARNING${NC}"
    echo "   RDB persistence is DISABLED (save \"\")"
    echo "   This is expected for pub/sub workload"
    echo "   Backup will create a snapshot anyway for debugging purposes"
else
    echo -e "${GREEN}✓ Enabled${NC}"
fi

# Trigger background save
echo -n "3. Triggering background save (BGSAVE)... "
SAVE_RESULT=$(redis_cmd BGSAVE)
if echo "$SAVE_RESULT" | grep -q "Background saving started"; then
    echo -e "${GREEN}✓ Started${NC}"
elif echo "$SAVE_RESULT" | grep -q "Background save already in progress"; then
    echo -e "${YELLOW}⚠ Already in progress${NC}"
    echo "   Waiting for existing save to complete..."
else
    echo -e "${RED}✗ FAILED${NC}"
    echo "   Result: $SAVE_RESULT"
    exit 1
fi

# Wait for save to complete
echo -n "4. Waiting for save to complete... "
MAX_WAIT=60  # seconds
WAITED=0
while [ $WAITED -lt $MAX_WAIT ]; do
    SAVE_TIME=$(redis_cmd LASTSAVE | tr -d '\r')
    CURRENT_TIME=$(date +%s)
    TIME_DIFF=$((CURRENT_TIME - SAVE_TIME))

    # If last save was within last 5 seconds, it's likely just completed
    if [ $TIME_DIFF -lt 5 ]; then
        echo -e "${GREEN}✓ Complete${NC}"
        break
    fi

    sleep 1
    WAITED=$((WAITED + 1))
    echo -n "."
done

if [ $WAITED -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ TIMEOUT${NC}"
    echo "   Backup did not complete within ${MAX_WAIT}s"
    exit 1
fi

# Get Redis data directory
echo -n "5. Locating RDB file... "
RDB_FILENAME=$(redis_cmd CONFIG GET dbfilename | tail -1 | tr -d '\r')
RDB_DIR=$(redis_cmd CONFIG GET dir | tail -1 | tr -d '\r')
RDB_PATH="${RDB_DIR}/${RDB_FILENAME}"
echo -e "${GREEN}${RDB_PATH}${NC}"

# Copy RDB file to backup directory
echo -n "6. Copying RDB file to backup directory... "
if docker exec redis-single test -f "$RDB_PATH" 2>/dev/null; then
    docker cp redis-single:"$RDB_PATH" "${BACKUP_DIR}/${BACKUP_FILE}" 2>/dev/null
    echo -e "${GREEN}✓ OK${NC}"
else
    # If running outside Docker, try direct copy
    if [ -f "$RDB_PATH" ]; then
        cp "$RDB_PATH" "${BACKUP_DIR}/${BACKUP_FILE}"
        echo -e "${GREEN}✓ OK${NC}"
    else
        echo -e "${RED}✗ FAILED${NC}"
        echo "   Error: RDB file not found at $RDB_PATH"
        exit 1
    fi
fi

# Get backup file size
BACKUP_SIZE=$(ls -lh "${BACKUP_DIR}/${BACKUP_FILE}" | awk '{print $5}')

# Compress backup
echo -n "7. Compressing backup... "
gzip "${BACKUP_DIR}/${BACKUP_FILE}"
COMPRESSED_FILE="${BACKUP_FILE}.gz"
COMPRESSED_SIZE=$(ls -lh "${BACKUP_DIR}/${COMPRESSED_FILE}" | awk '{print $5}')
echo -e "${GREEN}✓ OK${NC}"

# Get Redis info for backup metadata
REDIS_VERSION=$(redis_cmd INFO server | grep redis_version | cut -d: -f2 | tr -d '\r')
USED_MEMORY=$(redis_cmd INFO memory | grep used_memory_human | head -1 | cut -d: -f2 | tr -d '\r')
TOTAL_KEYS=$(redis_cmd DBSIZE | cut -d: -f2 | tr -d '\r')

# Create metadata file
METADATA_FILE="${BACKUP_DIR}/backup_${TIMESTAMP}.meta"
cat > "$METADATA_FILE" <<EOF
Backup Metadata
===============
Timestamp: $(date)
Redis Host: ${REDIS_HOST}:${REDIS_PORT}
Redis Version: ${REDIS_VERSION}
Used Memory: ${USED_MEMORY}
Total Keys: ${TOTAL_KEYS}
Backup File: ${COMPRESSED_FILE}
Original Size: ${BACKUP_SIZE}
Compressed Size: ${COMPRESSED_SIZE}
Last Save Time: $(date -r $SAVE_TIME 2>/dev/null || echo "N/A")

Notes:
- This is a pub/sub workload, data is ephemeral
- Backup is for debugging/disaster recovery only
- To restore: gunzip ${COMPRESSED_FILE} && redis-cli --rdb ${BACKUP_FILE}
EOF

echo ""
echo "========================================"
echo -e "${GREEN}Backup Complete ✓${NC}"
echo "========================================"
echo "Backup Location: ${BACKUP_DIR}/${COMPRESSED_FILE}"
echo "Metadata: ${METADATA_FILE}"
echo "Original Size: ${BACKUP_SIZE}"
echo "Compressed Size: ${COMPRESSED_SIZE}"
echo ""

# List recent backups
echo "Recent Backups:"
ls -lht "${BACKUP_DIR}"/*.rdb.gz 2>/dev/null | head -5 || echo "No previous backups found"

echo ""
echo "To restore this backup:"
echo "  1. Stop Redis: docker-compose stop redis"
echo "  2. Extract: gunzip ${BACKUP_DIR}/${COMPRESSED_FILE}"
echo "  3. Copy: docker cp ${BACKUP_DIR}/${BACKUP_FILE} redis-single:/data/dump.rdb"
echo "  4. Start Redis: docker-compose start redis"

exit 0
