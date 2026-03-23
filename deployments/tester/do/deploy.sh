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

[ -z "${ENVIRONMENT:-}" ] && ERRORS+=("ENVIRONMENT is required")
[ -z "${REGION:-}" ] && ERRORS+=("REGION is required")
[ -z "${GHCR_TOKEN:-}" ] && ERRORS+=("GHCR_TOKEN is required (GitHub PAT with read:packages scope)")
[ -z "${GHCR_IMAGE:-}" ] && ERRORS+=("GHCR_IMAGE is required (e.g., ghcr.io/klurvio/sukko-tester:latest)")

# GATEWAY_URL validation: reject empty, placeholder, or localhost
if [ -z "${GATEWAY_URL:-}" ]; then
  ERRORS+=("GATEWAY_URL is required (get via: task k8s:external-ips ENV=demo)")
elif [[ "$GATEWAY_URL" == *"<"* ]]; then
  ERRORS+=("GATEWAY_URL contains a placeholder — replace <gateway-ip> with the actual IP")
elif [[ "$GATEWAY_URL" =~ ^wss?://localhost ]]; then
  ERRORS+=("GATEWAY_URL starts with localhost — the Droplet cannot reach your local machine")
fi

if [ ${#ERRORS[@]} -gt 0 ]; then
  echo "ERROR: Missing or invalid configuration:"
  for err in "${ERRORS[@]}"; do
    echo "  - $err"
  done
  exit 1
fi

# Defaults
DROPLET_SIZE="${DROPLET_SIZE:-s-2vcpu-4gb}"
IMAGE="${IMAGE:-docker-20-04}"
TESTER_PORT="${TESTER_PORT:-8090}"
MESSAGE_BACKEND="${MESSAGE_BACKEND:-direct}"
PROVISIONING_URL="${PROVISIONING_URL:-}"
TESTER_AUTH_TOKEN="${TESTER_AUTH_TOKEN:-}"
KAFKA_BROKERS="${KAFKA_BROKERS:-}"

DROPLET_NAME="sukko-tester-${ENVIRONMENT}"
FIREWALL_NAME="sukko-tester-fw"
TAG_NAME="sukko-tester"

# =============================================================================
# Safety Checks
# =============================================================================

# Check for existing Droplet (idempotency)
if doctl compute droplet list --format Name --no-header | grep -q "^${DROPLET_NAME}$"; then
  EXISTING_IP=$(doctl compute droplet get "$DROPLET_NAME" --format PublicIPv4 --no-header)
  echo "ERROR: Droplet '${DROPLET_NAME}' already exists (IP: ${EXISTING_IP})"
  echo "  Destroy it first: task tester:do:destroy"
  echo "  Or use it:        http://${EXISTING_IP}:${TESTER_PORT}"
  exit 1
fi

# Query DO account SSH keys
SSH_KEYS=$(doctl compute ssh-key list --format ID --no-header | tr '\n' ',' | sed 's/,$//')
if [ -z "$SSH_KEYS" ]; then
  echo "ERROR: No SSH keys found in your DigitalOcean account"
  echo ""
  echo "Add an SSH key first:"
  echo "  doctl compute ssh-key import my-key --public-key-file ~/.ssh/id_ed25519.pub"
  echo ""
  echo "Or add via DO dashboard: https://cloud.digitalocean.com/account/security"
  exit 1
fi

# Verify image exists in GHCR before creating Droplet
echo "Verifying image exists: ${GHCR_IMAGE}"
if ! docker manifest inspect "${GHCR_IMAGE}" &>/dev/null; then
  echo "ERROR: Image not found in registry: ${GHCR_IMAGE}"
  echo "  Push it first: task local:build:push:tester"
  echo ""
  echo "  Make sure you are logged into GHCR:"
  echo "    echo \$GHCR_TOKEN | docker login ghcr.io -u oauth --password-stdin"
  exit 1
fi

# =============================================================================
# Create/Ensure Cloud Firewall
# =============================================================================
# NOTE: Firewall is created once with the current TESTER_PORT. If you change
# TESTER_PORT after initial creation, delete the firewall manually:
#   doctl compute firewall delete sukko-tester-fw --force
if ! doctl compute firewall list --format Name --no-header | grep -q "^${FIREWALL_NAME}$"; then
  echo "Creating Cloud Firewall: ${FIREWALL_NAME}"
  doctl compute firewall create \
    --name "$FIREWALL_NAME" \
    --tag-names "$TAG_NAME" \
    --inbound-rules "protocol:tcp,ports:22,address:0.0.0.0/0,address:::/0 protocol:tcp,ports:${TESTER_PORT},address:0.0.0.0/0,address:::/0" \
    --outbound-rules "protocol:tcp,ports:all,address:0.0.0.0/0,address:::/0 protocol:udp,ports:all,address:0.0.0.0/0,address:::/0 protocol:icmp,address:0.0.0.0/0,address:::/0"
else
  echo "Firewall '${FIREWALL_NAME}' already exists"
fi

# =============================================================================
# Build cloud-init user-data
# =============================================================================
# Embed startup script + env vars into cloud-init
USER_DATA=$(cat <<USERDATA
#!/bin/bash
set -euo pipefail

# Export env vars for startup script
export GHCR_TOKEN="${GHCR_TOKEN}"
export GHCR_IMAGE="${GHCR_IMAGE}"
export GATEWAY_URL="${GATEWAY_URL}"
export PROVISIONING_URL="${PROVISIONING_URL}"
export TESTER_AUTH_TOKEN="${TESTER_AUTH_TOKEN}"
export TESTER_PORT="${TESTER_PORT}"
export MESSAGE_BACKEND="${MESSAGE_BACKEND}"
export KAFKA_BROKERS="${KAFKA_BROKERS}"

$(tail -n +3 "$SCRIPT_DIR/startup-script.sh")
USERDATA
)

# =============================================================================
# Create Droplet
# =============================================================================
echo ""
echo "=== Creating sukko-tester Droplet ==="
echo "  Name:        $DROPLET_NAME"
echo "  Region:      $REGION"
echo "  Size:        $DROPLET_SIZE"
echo "  Image:       $IMAGE"
echo "  Gateway:     $GATEWAY_URL"
echo "  Tester Port: $TESTER_PORT"
echo ""

doctl compute droplet create "$DROPLET_NAME" \
  --region "$REGION" \
  --size "$DROPLET_SIZE" \
  --image "$IMAGE" \
  --ssh-keys "$SSH_KEYS" \
  --tag-names "$TAG_NAME" \
  --user-data "$USER_DATA" \
  --wait

# =============================================================================
# Wait for IP Assignment
# =============================================================================
echo "Waiting for IP assignment..."
DROPLET_IP=""
for i in $(seq 1 30); do
  DROPLET_IP=$(doctl compute droplet get "$DROPLET_NAME" --format PublicIPv4 --no-header 2>/dev/null || echo "")
  if [ -n "$DROPLET_IP" ]; then
    break
  fi
  sleep 2
done

if [ -z "$DROPLET_IP" ]; then
  echo "ERROR: Could not get Droplet IP after 60 seconds"
  exit 1
fi

echo "Droplet IP: $DROPLET_IP"

# =============================================================================
# Wait for Health Check
# =============================================================================
echo "Waiting for health check (http://${DROPLET_IP}:${TESTER_PORT}/health)..."
HEALTH_URL="http://${DROPLET_IP}:${TESTER_PORT}/health"
TIMEOUT=300  # 5 minutes
INTERVAL=10
ELAPSED=0

while [ $ELAPSED -lt $TIMEOUT ]; do
  if curl -sf "$HEALTH_URL" &>/dev/null; then
    echo ""
    echo "=== sukko-tester is ready ==="
    echo "  Droplet:    $DROPLET_NAME"
    echo "  IP:         $DROPLET_IP"
    echo "  Tester URL: http://${DROPLET_IP}:${TESTER_PORT}"
    echo ""
    echo "Usage:"
    echo "  sukko test smoke --tester-url http://${DROPLET_IP}:${TESTER_PORT}"
    echo ""
    echo "Management:"
    echo "  task tester:do:logs       View container logs"
    echo "  task tester:do:ssh        SSH into Droplet"
    echo "  task tester:do:destroy    Destroy Droplet"
    exit 0
  fi
  echo "  ... waiting (${ELAPSED}s / ${TIMEOUT}s)"
  sleep $INTERVAL
  ELAPSED=$((ELAPSED + INTERVAL))
done

echo ""
echo "ERROR: Health check timed out after ${TIMEOUT} seconds"
echo "  The Droplet was created but the tester service may not be running."
echo "  Debug with: task tester:do:ssh"
echo "  Then run:   docker logs sukko-tester"
exit 1
