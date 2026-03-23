#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.env}"

# Load config if exists
if [ -f "$CONFIG_FILE" ]; then
  # shellcheck source=/dev/null
  source "$CONFIG_FILE"
fi

ENVIRONMENT="${ENVIRONMENT:-demo}"
DROPLET_NAME="sukko-tester-${ENVIRONMENT}"

# Get Droplet IP
DROPLET_IP=$(doctl compute droplet get "$DROPLET_NAME" --format PublicIPv4 --no-header 2>/dev/null || echo "")
if [ -z "$DROPLET_IP" ]; then
  echo "ERROR: Droplet '${DROPLET_NAME}' not found or has no IP"
  echo "  Deploy first: task tester:do:deploy"
  exit 1
fi

echo "Connecting to ${DROPLET_NAME} (${DROPLET_IP})..."
ssh -o StrictHostKeyChecking=no "root@${DROPLET_IP}"
