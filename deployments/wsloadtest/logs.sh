#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.env}"

# Load config if exists
[ -f "$CONFIG_FILE" ] && source "$CONFIG_FILE"

PROJECT="${PROJECT:-odin-9e902}"
ZONE="${ZONE:-us-central1-a}"
VM_NAME="${VM_NAME:-wsloadtest-vm}"

echo "Fetching serial console logs for: $VM_NAME"
echo ""

gcloud compute instances get-serial-port-output "$VM_NAME" \
  --project="$PROJECT" \
  --zone="$ZONE"
