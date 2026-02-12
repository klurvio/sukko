#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.env}"

# =============================================================================
# Load Configuration
# =============================================================================
if [ ! -f "$CONFIG_FILE" ]; then
  echo "ERROR: Config file not found: $CONFIG_FILE"
  echo ""
  echo "Create it from the example:"
  echo "  cp $SCRIPT_DIR/config.env.example $SCRIPT_DIR/config.env"
  echo "  # Edit config.env with your values"
  exit 1
fi

# shellcheck source=/dev/null
source "$CONFIG_FILE"

# =============================================================================
# Validate Required Config
# =============================================================================
ERRORS=()

[ -z "${PROJECT:-}" ] && ERRORS+=("PROJECT is required")
[ -z "${ZONE:-}" ] && ERRORS+=("ZONE is required")
[ -z "${NETWORK:-}" ] && ERRORS+=("NETWORK is required")
[ -z "${SUBNET:-}" ] && ERRORS+=("SUBNET is required")
[ -z "${KAFKA_BROKERS:-}" ] && ERRORS+=("KAFKA_BROKERS is required")

if [ ${#ERRORS[@]} -gt 0 ]; then
  echo "ERROR: Missing required configuration:"
  for err in "${ERRORS[@]}"; do
    echo "  - $err"
  done
  exit 1
fi

# Defaults
VM_NAME="${VM_NAME:-wspublisher-vm}"
MACHINE_TYPE="${MACHINE_TYPE:-e2-standard-2}"
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/$PROJECT/odin-ws}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
KAFKA_NAMESPACE="${KAFKA_NAMESPACE:-stag}"
RATE="${RATE:-100}"
DURATION="${DURATION:-0}"
TENANT_ID="${TENANT_ID:-odin}"
IDENTIFIERS="${IDENTIFIERS:-BTC,ETH,SOL,all}"
CATEGORIES="${CATEGORIES:-trade,liquidity,orderbook}"

# =============================================================================
# Safety Checks
# =============================================================================

# Ensure VM name has wspublisher prefix (safety measure)
if [[ ! "$VM_NAME" =~ ^wspublisher ]]; then
  echo "ERROR: VM_NAME must start with 'wspublisher' prefix for safety"
  echo "  Got: $VM_NAME"
  exit 1
fi

# Check if VM already exists
if gcloud compute instances describe "$VM_NAME" --project="$PROJECT" --zone="$ZONE" &>/dev/null; then
  echo "ERROR: VM '$VM_NAME' already exists in zone $ZONE"
  echo "  Destroy it first: task wspublisher:remote:destroy"
  exit 1
fi

# Verify image exists in registry
echo "Validating image exists: $REGISTRY/wspublisher:$IMAGE_TAG"
if ! gcloud artifacts docker images describe "$REGISTRY/wspublisher:$IMAGE_TAG" \
    --project="$PROJECT" &>/dev/null; then
  echo "ERROR: Image not found in registry."
  echo "  Push it first: task wspublisher:push"
  exit 1
fi

# =============================================================================
# Create VM (this is the ONLY resource we create)
# =============================================================================
echo ""
echo "=== Creating wspublisher VM ==="
echo "  VM Name:     $VM_NAME"
echo "  Project:     $PROJECT"
echo "  Zone:        $ZONE"
echo "  Network:     $NETWORK"
echo "  Brokers:     $KAFKA_BROKERS"
echo "  Namespace:   $KAFKA_NAMESPACE"
echo "  Rate:        $RATE msg/sec"
echo "  Duration:    $DURATION (0=infinite)"
echo ""

gcloud compute instances create "$VM_NAME" \
  --project="$PROJECT" \
  --zone="$ZONE" \
  --machine-type="$MACHINE_TYPE" \
  --network="$NETWORK" \
  --subnet="$SUBNET" \
  --no-address \
  --preemptible \
  --scopes=cloud-platform \
  --labels="purpose=loadtest,tool=wspublisher" \
  --metadata-from-file=startup-script="$SCRIPT_DIR/startup-script.sh" \
  --metadata=KAFKA_BROKERS="$KAFKA_BROKERS",KAFKA_NAMESPACE="$KAFKA_NAMESPACE",RATE="$RATE",DURATION="$DURATION",TENANT_ID="$TENANT_ID",IDENTIFIERS="$IDENTIFIERS",CATEGORIES="$CATEGORIES",REGISTRY="$REGISTRY",IMAGE_TAG="$IMAGE_TAG"

echo ""
echo "=== VM Created Successfully ==="
echo "View logs:  task wspublisher:remote:logs"
echo "SSH:        task wspublisher:remote:ssh"
echo "Destroy:    task wspublisher:remote:destroy"
