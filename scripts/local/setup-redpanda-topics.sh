#!/bin/bash
# Setup Redpanda topics with environment-prefixed naming
# Creates both regular and refined topics for each base topic
#
# Usage: ./setup-redpanda-topics.sh [environment] [container_name]
# Examples:
#   ./setup-redpanda-topics.sh                    # local environment, redpanda-local container
#   ./setup-redpanda-topics.sh dev                # dev environment
#   ./setup-redpanda-topics.sh local my-redpanda  # local env, custom container
#
# Topic naming pattern:
#   Regular:  sukko.{env}.{base}          (e.g., sukko.dev.trade)
#   Refined:  sukko.{env}.{base}.refined  (e.g., sukko.dev.trade.refined)

set -e

# Parse arguments
ENV=${1:-local}
CONTAINER=${2:-redpanda-local}

# Normalize environment name
normalize_env() {
  local env="$1"
  case "$env" in
    development|local|"")
      echo "local"
      ;;
    develop|dev)
      echo "dev"
      ;;
    staging|stage)
      echo "staging"
      ;;
    production|prod)
      echo "prod"
      ;;
    *)
      echo "$env"
      ;;
  esac
}

ENV=$(normalize_env "$ENV")

echo "Setting up Redpanda topics..."
echo "   Environment: $ENV"
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

  echo "Creating topic: $topic (retention: ${retention}ms)"

  if rpk_cmd topic create "$topic" \
    --partitions 12 \
    --replicas 1 \
    --topic-config "retention.ms=${retention}" \
    --topic-config "segment.ms=10000" \
    --topic-config "cleanup.policy=delete" \
    --topic-config "compression.type=snappy" \
    --topic-config "max.message.bytes=1048576" \
    --topic-config "min.insync.replicas=1" 2>/dev/null; then
    echo "   Created: $topic"
    return 0
  else
    # Topic might already exist
    if rpk_cmd topic describe "$topic" &>/dev/null; then
      echo "   Already exists: $topic"
      return 0
    else
      echo "   Failed: $topic"
      return 1
    fi
  fi
}

# Create both regular and refined topics for a base topic
create_topic_pair() {
  local base=$1
  local retention=$2

  local regular="sukko.${ENV}.${base}"
  local refined="sukko.${ENV}.${base}.refined"

  create_topic "$regular" "$retention"
  create_topic "$refined" "$retention"
  echo ""
}

# Create all topics
SUCCESS_COUNT=0
FAIL_COUNT=0

echo "Creating topic pairs (regular + refined)..."
echo "============================================="
echo ""

# Trading events - 30s retention
if create_topic_pair "trade" "30000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Liquidity events - 1min retention
if create_topic_pair "liquidity" "60000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Metadata events - 1hr retention
if create_topic_pair "metadata" "3600000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Social events - 1hr retention
if create_topic_pair "social" "3600000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Community events - 5min retention
if create_topic_pair "community" "300000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Creation events - 1hr retention
if create_topic_pair "creation" "3600000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Analytics events - 5min retention
if create_topic_pair "analytics" "300000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

# Balance events - 30s retention
if create_topic_pair "balances" "30000"; then ((SUCCESS_COUNT+=2)); else ((FAIL_COUNT+=2)); fi

echo "============================================="
echo "Summary:"
echo "   Environment: $ENV"
echo "   Success: $SUCCESS_COUNT topics"
echo "   Failed:  $FAIL_COUNT topics"
echo "============================================="
echo ""

# List all sukko topics
echo "Topic List (sukko.${ENV}.*):"
rpk_cmd topic list | grep "sukko.${ENV}" || echo "   No topics found"
echo ""

echo "Setup complete!"
echo ""
echo "Next steps:"
echo "   1. Verify in Redpanda Console: http://localhost:8080"
echo "   2. Start publisher: ENVIRONMENT=$ENV go run publisher-go/main.go"
echo "   3. Check messages: docker exec $CONTAINER rpk topic consume sukko.${ENV}.trade --num 5"
