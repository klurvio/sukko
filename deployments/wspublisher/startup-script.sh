#!/bin/bash
set -e

# Helper to get metadata
get_metadata() {
  curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" \
    -H "Metadata-Flavor: Google" 2>/dev/null || echo ""
}

# Get all metadata
KAFKA_BROKERS=$(get_metadata KAFKA_BROKERS)
KAFKA_NAMESPACE=$(get_metadata KAFKA_NAMESPACE)
RATE=$(get_metadata RATE)
DURATION=$(get_metadata DURATION)
TENANT_ID=$(get_metadata TENANT_ID)
IDENTIFIERS=$(get_metadata IDENTIFIERS)
CATEGORIES=$(get_metadata CATEGORIES)
REGISTRY=$(get_metadata REGISTRY)
IMAGE_TAG=$(get_metadata IMAGE_TAG)

# Defaults
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/trim-array-480700-j7/odin}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
KAFKA_NAMESPACE="${KAFKA_NAMESPACE:-stag}"
RATE="${RATE:-100}"
DURATION="${DURATION:-0}"
TENANT_ID="${TENANT_ID:-odin}"
IDENTIFIERS="${IDENTIFIERS:-BTC,ETH,SOL,all}"
CATEGORIES="${CATEGORIES:-trade,liquidity,orderbook}"

echo "=== wspublisher VM startup ==="
echo "Brokers: $KAFKA_BROKERS"
echo "Namespace: $KAFKA_NAMESPACE"
echo "Rate: $RATE msg/sec"
echo "Duration: $DURATION (0=infinite)"
echo "Image: $REGISTRY/wspublisher:$IMAGE_TAG"

# Install Docker
echo "Installing Docker..."
apt-get update -qq && apt-get install -y -qq docker.io

# Authenticate to Artifact Registry
echo "Authenticating to Artifact Registry..."
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# Pull and run wspublisher
echo "Starting wspublisher..."
docker run --rm \
  -e KAFKA_BROKERS="$KAFKA_BROKERS" \
  -e KAFKA_NAMESPACE="$KAFKA_NAMESPACE" \
  -e TIMING_MODE="poisson" \
  -e POISSON_LAMBDA="$RATE" \
  -e DURATION="$DURATION" \
  -e TENANT_ID="$TENANT_ID" \
  -e IDENTIFIERS="$IDENTIFIERS" \
  -e CATEGORIES="$CATEGORIES" \
  -e LOG_LEVEL=info \
  "$REGISTRY/wspublisher:$IMAGE_TAG"

echo "=== wspublisher completed ==="
