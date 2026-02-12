#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.env}"

# Load config if exists
[ -f "$CONFIG_FILE" ] && source "$CONFIG_FILE"

PROJECT="${PROJECT:-odin-9e902}"
ZONE="${ZONE:-us-central1-a}"
VM_NAME="${VM_NAME:-wspublisher-vm}"

# VM has no public IP, requires IAP tunnel
# If IAP not set up, see: task wspublisher (shows prerequisites)

echo "Connecting to $VM_NAME via IAP tunnel..."
gcloud compute ssh "$VM_NAME" \
  --project="$PROJECT" \
  --zone="$ZONE" \
  --tunnel-through-iap
