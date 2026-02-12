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
[ -z "${WS_URL:-}" ] && ERRORS+=("WS_URL is required (get via: kubectl get svc odin-ws-gateway -n <ns> -o jsonpath='{.status.loadBalancer.ingress[0].ip}')")

if [ ${#ERRORS[@]} -gt 0 ]; then
  echo "ERROR: Missing required configuration:"
  for err in "${ERRORS[@]}"; do
    echo "  - $err"
  done
  exit 1
fi

# Defaults
VM_NAME="${VM_NAME:-wsloadtest-vm}"
MACHINE_TYPE="${MACHINE_TYPE:-e2-standard-8}"
REGISTRY="${REGISTRY:-us-central1-docker.pkg.dev/$PROJECT/odin-ws}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
TARGET_CONNECTIONS="${TARGET_CONNECTIONS:-5000}"
RAMP_RATE="${RAMP_RATE:-100}"
DURATION="${DURATION:-10m}"

# =============================================================================
# Safety Checks
# =============================================================================

# Ensure VM name has wsloadtest prefix (safety measure)
if [[ ! "$VM_NAME" =~ ^wsloadtest ]]; then
  echo "ERROR: VM_NAME must start with 'wsloadtest' prefix for safety"
  echo "  Got: $VM_NAME"
  exit 1
fi

# Check if VM already exists
if gcloud compute instances describe "$VM_NAME" --project="$PROJECT" --zone="$ZONE" &>/dev/null; then
  echo "ERROR: VM '$VM_NAME' already exists in zone $ZONE"
  echo "  Destroy it first: task wsloadtest:remote:destroy"
  exit 1
fi

# Verify image exists in registry
echo "Validating image exists: $REGISTRY/wsloadtest:$IMAGE_TAG"
if ! gcloud artifacts docker images describe "$REGISTRY/wsloadtest:$IMAGE_TAG" \
    --project="$PROJECT" &>/dev/null; then
  echo "ERROR: Image not found in registry."
  echo "  Push it first: task wsloadtest:push"
  exit 1
fi

# =============================================================================
# Create VM (this is the ONLY resource we create)
# =============================================================================
echo ""
echo "=== Creating wsloadtest VM ==="
echo "  VM Name:     $VM_NAME"
echo "  Project:     $PROJECT"
echo "  Zone:        $ZONE"
echo "  Network:     $NETWORK"
echo "  Target:      $WS_URL"
echo "  Connections: $TARGET_CONNECTIONS (ramp: $RAMP_RATE/s)"
echo "  Duration:    $DURATION"
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
  --labels="purpose=loadtest,tool=wsloadtest" \
  --metadata-from-file=startup-script="$SCRIPT_DIR/startup-script.sh" \
  --metadata=WS_URL="$WS_URL",TARGET_CONNECTIONS="$TARGET_CONNECTIONS",RAMP_RATE="$RAMP_RATE",DURATION="$DURATION",REGISTRY="$REGISTRY",IMAGE_TAG="$IMAGE_TAG"

echo ""
echo "=== VM Created Successfully ==="
echo "View logs:  task wsloadtest:remote:logs"
echo "SSH:        task wsloadtest:remote:ssh"
echo "Destroy:    task wsloadtest:remote:destroy"
