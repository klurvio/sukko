#!/bin/bash
set -e

# Helper to get metadata
get_metadata() {
  curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" \
    -H "Metadata-Flavor: Google" 2>/dev/null || echo ""
}

# Get all metadata
WS_URL=$(get_metadata WS_URL)
TARGET_CONNECTIONS=$(get_metadata TARGET_CONNECTIONS)
RAMP_RATE=$(get_metadata RAMP_RATE)
DURATION=$(get_metadata DURATION)
REGISTRY=$(get_metadata REGISTRY)
IMAGE_TAG=$(get_metadata IMAGE_TAG)

# Defaults
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/trim-array-480700-j7/odin}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

echo "=== wsloadtest VM startup ==="
echo "Target: $WS_URL"
echo "Connections: $TARGET_CONNECTIONS, Ramp: $RAMP_RATE/s, Duration: $DURATION"
echo "Image: $REGISTRY/wsloadtest:$IMAGE_TAG"

# Tune kernel for high connections
echo "Tuning kernel parameters..."
cat >> /etc/sysctl.conf << 'EOF'
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
EOF
sysctl -p

# Set ulimits for current shell
ulimit -n 1000000

# Install Docker
echo "Installing Docker..."
apt-get update -qq && apt-get install -y -qq docker.io

# Authenticate to Artifact Registry
echo "Authenticating to Artifact Registry..."
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# Pull and run wsloadtest
echo "Starting wsloadtest..."
docker run --rm \
  --ulimit nofile=1000000:1000000 \
  -e WS_URL="$WS_URL" \
  -e TARGET_CONNECTIONS="$TARGET_CONNECTIONS" \
  -e RAMP_RATE="$RAMP_RATE" \
  -e DURATION="$DURATION" \
  -e LOG_LEVEL=info \
  "$REGISTRY/wsloadtest:$IMAGE_TAG"

echo "=== wsloadtest completed ==="
