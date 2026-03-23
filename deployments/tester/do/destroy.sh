#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.env}"

# Load config if exists, otherwise use defaults
if [ -f "$CONFIG_FILE" ]; then
  # shellcheck source=/dev/null
  source "$CONFIG_FILE"
fi

ENVIRONMENT="${ENVIRONMENT:-demo}"
DROPLET_NAME="sukko-tester-${ENVIRONMENT}"

# =============================================================================
# Safety Checks
# =============================================================================

# CRITICAL: Only delete Droplets with sukko-tester prefix
if [[ ! "$DROPLET_NAME" =~ ^sukko-tester- ]]; then
  echo "ERROR: Refusing to delete Droplet without 'sukko-tester-' prefix"
  echo "  Droplet Name: $DROPLET_NAME"
  echo "  This is a safety measure to prevent accidental deletion"
  exit 1
fi

# Check if Droplet exists
if ! doctl compute droplet list --format Name --no-header | grep -q "^${DROPLET_NAME}$"; then
  echo "Droplet '${DROPLET_NAME}' does not exist. Nothing to delete."
  exit 0
fi

# =============================================================================
# Delete Droplet
# =============================================================================
echo "Deleting Droplet: $DROPLET_NAME"

doctl compute droplet delete "$DROPLET_NAME" --force

echo ""
echo "=== Droplet Deleted Successfully ==="
