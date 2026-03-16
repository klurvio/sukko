#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.env}"

# Load config if exists, otherwise use defaults
if [ -f "$CONFIG_FILE" ]; then
  # shellcheck source=/dev/null
  source "$CONFIG_FILE"
fi

PROJECT="${PROJECT:-sukko-9e902}"
ZONE="${ZONE:-us-central1-a}"
VM_NAME="${VM_NAME:-wsloadtest-vm}"

# =============================================================================
# Safety Checks
# =============================================================================

# CRITICAL: Only delete VMs with wsloadtest prefix
if [[ ! "$VM_NAME" =~ ^wsloadtest ]]; then
  echo "ERROR: Refusing to delete VM without 'wsloadtest' prefix"
  echo "  VM Name: $VM_NAME"
  echo "  This is a safety measure to prevent accidental deletion of production VMs"
  exit 1
fi

# Check if VM exists
if ! gcloud compute instances describe "$VM_NAME" --project="$PROJECT" --zone="$ZONE" &>/dev/null; then
  echo "VM '$VM_NAME' does not exist in zone $ZONE. Nothing to delete."
  exit 0
fi

# =============================================================================
# Delete VM (this is the ONLY resource we delete)
# =============================================================================
echo "Deleting wsloadtest VM: $VM_NAME"
echo "  Project: $PROJECT"
echo "  Zone:    $ZONE"

gcloud compute instances delete "$VM_NAME" \
  --project="$PROJECT" \
  --zone="$ZONE" \
  --quiet

echo ""
echo "=== VM Deleted Successfully ==="
