#!/bin/bash
# Setup Redpanda topics with appropriate configs
# Usage: ./setup-redpanda-topics.sh [container_name]
# Example: ./setup-redpanda-topics.sh redpanda-local

set -e

CONTAINER=${1:-redpanda-local}

echo "🚀 Setting up Redpanda topics..."
echo "   Container: $CONTAINER"
echo ""

# Helper function to run rpk commands
rpk_cmd() {
  docker exec "$CONTAINER" rpk "$@"
}

# Helper function to create a topic
create_topic() {
  local topic=$1
  local retention=$2

  echo "📝 Creating topic: $topic (retention: ${retention}ms)"

  if rpk_cmd topic create "$topic" \
    --partitions 12 \
    --replicas 1 \
    --topic-config "retention.ms=${retention}" \
    --topic-config "segment.ms=10000" \
    --topic-config "cleanup.policy=delete" \
    --topic-config "compression.type=snappy" \
    --topic-config "max.message.bytes=1048576" \
    --topic-config "min.insync.replicas=1" 2>/dev/null; then
    echo "   ✅ Created: $topic"
    return 0
  else
    # Topic might already exist
    if rpk_cmd topic describe "$topic" &>/dev/null; then
      echo "   ⚠️  Already exists: $topic"
      return 0
    else
      echo "   ❌ Failed: $topic"
      return 1
    fi
  fi
}

# Create all topics
SUCCESS_COUNT=0
FAIL_COUNT=0

# Trading events - 30s retention
if create_topic "odin.trades" "30000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Liquidity events - 1min retention
if create_topic "odin.liquidity" "60000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Metadata events - 1hr retention
if create_topic "odin.metadata" "3600000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Social events - 1hr retention
if create_topic "odin.social" "3600000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Community events - 5min retention
if create_topic "odin.community" "300000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Creation events - 1hr retention
if create_topic "odin.creation" "3600000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Analytics events - 5min retention
if create_topic "odin.analytics" "300000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

# Balance events - 30s retention
if create_topic "odin.balances" "30000"; then ((SUCCESS_COUNT++)); else ((FAIL_COUNT++)); fi
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📊 Summary:"
echo "   Success: $SUCCESS_COUNT topics"
echo "   Failed:  $FAIL_COUNT topics"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# List all topics
echo "📋 Topic List:"
rpk_cmd topic list
echo ""

echo "✅ Setup complete!"
echo ""
echo "🎯 Next steps:"
echo "   1. Verify in Redpanda Console: http://localhost:8080"
echo "   2. Start publisher: cd publisher && npm run dev"
echo "   3. Check messages: docker exec $CONTAINER rpk topic consume odin.trades --num 5"
