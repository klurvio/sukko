#!/bin/bash
set -e

# Helper to get metadata
get_metadata() {
  curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" \
    -H "Metadata-Flavor: Google" 2>/dev/null || echo ""
}

REGISTRY=$(get_metadata REGISTRY)
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/sukko-9e902/sukko}"

echo "=== wspublisher VM startup ==="
echo "Registry: $REGISTRY"

# Install Docker
echo "Installing Docker..."
apt-get update -qq && apt-get install -y -qq docker.io

# Authenticate to Artifact Registry
echo "Authenticating to Artifact Registry..."
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# Pre-pull image so publisher:run starts faster
echo "Pre-pulling wspublisher image..."
docker pull "$REGISTRY/wspublisher:latest"

echo "=== VM ready. Use 'task gce:publisher:run' to start publishing ==="
