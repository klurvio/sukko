#!/bin/bash
set -e

# Helper to get metadata
get_metadata() {
  curl -sf "http://metadata.google.internal/computeMetadata/v1/instance/attributes/$1" \
    -H "Metadata-Flavor: Google" 2>/dev/null || echo ""
}

REGISTRY=$(get_metadata REGISTRY)
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/odin-9e902/odin-ws}"

echo "=== wsloadtest VM startup ==="
echo "Registry: $REGISTRY"

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

# Set ulimits
ulimit -n 1000000

# Install Docker
echo "Installing Docker..."
apt-get update -qq && apt-get install -y -qq docker.io

# Authenticate to Artifact Registry
echo "Authenticating to Artifact Registry..."
gcloud auth configure-docker us-central1-docker.pkg.dev --quiet

# Pre-pull image so loadtest:run starts faster
echo "Pre-pulling wsloadtest image..."
docker pull "$REGISTRY/wsloadtest:latest"

echo "=== VM ready. Use 'task gce:loadtest:run' to start the test ==="
