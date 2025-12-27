#!/bin/bash
# =============================================================================
# GCP v2 VM Setup Script
# =============================================================================
# Initializes a GCP VM for running loadtest and publisher
# Includes kernel tuning for high WebSocket connection counts

set -e

echo "=================================================="
echo "GCP v2 VM Setup"
echo "=================================================="

# =============================================================================
# 1. Install Docker
# =============================================================================
echo "[1/5] Installing Docker..."
apt-get update
apt-get install -y docker.io docker-compose git curl jq

systemctl enable docker
systemctl start docker

# =============================================================================
# 2. Create deploy user
# =============================================================================
echo "[2/5] Creating deploy user..."
if ! id -u deploy &>/dev/null; then
    useradd -m -s /bin/bash deploy
fi
usermod -aG docker deploy

# =============================================================================
# 3. Kernel tuning for high connection counts
# =============================================================================
echo "[3/5] Configuring kernel parameters..."

# File descriptor limits (18K+ connections)
cat >> /etc/security/limits.conf <<EOF
# Added by odin v2 setup - high connection limits
* soft nofile 1048576
* hard nofile 1048576
EOF

# TCP/Network tuning
cat >> /etc/sysctl.conf <<EOF
# Added by odin v2 setup - high connection tuning
fs.file-max = 2097152
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
EOF

# Apply immediately
sysctl -p

# =============================================================================
# 4. Setup SSH for GitHub
# =============================================================================
echo "[4/5] Setting up SSH..."
SSH_DIR=/home/deploy/.ssh
mkdir -p $SSH_DIR
chown deploy:deploy $SSH_DIR
chmod 700 $SSH_DIR

# Try to get GitHub deploy key from GCP metadata
if curl -s -f -H "Metadata-Flavor: Google" \
    "http://metadata.google.internal/computeMetadata/v1/project/attributes/github-deploy-key" \
    > $SSH_DIR/github_deploy_key 2>/dev/null; then
    chmod 600 $SSH_DIR/github_deploy_key
    chown deploy:deploy $SSH_DIR/github_deploy_key

    cat > $SSH_DIR/config <<EOF
Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/github_deploy_key
    StrictHostKeyChecking no
EOF
    chmod 600 $SSH_DIR/config
    chown deploy:deploy $SSH_DIR/config
    echo "  GitHub SSH key configured"
else
    echo "  No GitHub deploy key in metadata (optional)"
fi

# =============================================================================
# 5. Create directory structure (code synced via SCP, not git clone)
# =============================================================================
echo "[5/5] Creating directory structure..."
REPO_DIR=/home/deploy/odin-ws
mkdir -p $REPO_DIR/{loadtest,publisher,deployments/gcp/v2/environments}
chown -R deploy:deploy $REPO_DIR

echo ""
echo "=================================================="
echo "VM Setup complete!"
echo "=================================================="
echo ""
echo "Code will be synced from local via 'task gcp:v2:sync-code'"
echo ""
