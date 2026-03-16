#!/bin/bash
# Test WebSocket authentication flow locally
# Prerequisites:
#   - Kind cluster running with Helm chart deployed
#   - wscat installed: npm install -g wscat
#   - jq installed: brew install jq

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== WebSocket Auth Test ===${NC}\n"

# Configuration
# Note: Kind maps NodePort 30080 → host port 3005
# Auth-service requires port-forward: kubectl port-forward -n odin-local svc/odin-auth-service 3002:3002
AUTH_URL="${AUTH_URL:-http://localhost:3002}"
WS_URL="${WS_URL:-ws://localhost:3005}"
APP_ID="odin-web"
APP_SECRET="change-me-odin-web-secret"

# Step 1: Get JWT token from auth-service
echo -e "${YELLOW}1. Getting JWT token from auth-service...${NC}"
RESPONSE=$(curl -s -X POST "${AUTH_URL}/auth/token" \
  -H "Content-Type: application/json" \
  -d "{\"app_id\": \"${APP_ID}\", \"app_secret\": \"${APP_SECRET}\"}")

TOKEN=$(echo "$RESPONSE" | jq -r '.token // empty')
EXPIRES_AT=$(echo "$RESPONSE" | jq -r '.expires_at // empty')
TENANT_ID=$(echo "$RESPONSE" | jq -r '.tenant_id // empty')

if [ -z "$TOKEN" ]; then
  echo -e "${RED}Failed to get token!${NC}"
  echo "Response: $RESPONSE"
  exit 1
fi

echo -e "${GREEN}Token received!${NC}"
echo "  Tenant: $TENANT_ID"
echo "  Expires: $(date -r $EXPIRES_AT 2>/dev/null || date -d @$EXPIRES_AT)"
echo "  Token: ${TOKEN:0:50}..."
echo ""

# Step 2: Test connection without token (should fail)
echo -e "${YELLOW}2. Testing connection WITHOUT token (should fail with 401)...${NC}"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  "${WS_URL}/ws")

if [ "$HTTP_CODE" = "401" ]; then
  echo -e "${GREEN}Correctly rejected with 401 Unauthorized${NC}"
else
  echo -e "${YELLOW}Got HTTP $HTTP_CODE (expected 401 when auth is enabled)${NC}"
fi
echo ""

# Step 3: Test connection with invalid token (should fail)
echo -e "${YELLOW}3. Testing connection with INVALID token (should fail)...${NC}"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  "${WS_URL}/ws?token=invalid-token")

if [ "$HTTP_CODE" = "401" ]; then
  echo -e "${GREEN}Correctly rejected with 401 Unauthorized${NC}"
else
  echo -e "${YELLOW}Got HTTP $HTTP_CODE (expected 401)${NC}"
fi
echo ""

# Step 4: Connect with valid token
echo -e "${YELLOW}4. Testing connection WITH valid token...${NC}"
echo -e "   Run this command to connect interactively:"
echo ""
echo -e "${GREEN}   wscat -c \"${WS_URL}/ws?token=${TOKEN}\"${NC}"
echo ""
echo "   Or use websocat:"
echo -e "${GREEN}   websocat \"${WS_URL}/ws?token=${TOKEN}\"${NC}"
echo ""

# Step 5: Quick connection test with timeout
echo -e "${YELLOW}5. Quick connection test (5 second timeout)...${NC}"
if command -v websocat &> /dev/null; then
  timeout 5 websocat -t "${WS_URL}/ws?token=${TOKEN}" <<< '{"action":"ping"}' 2>/dev/null && echo -e "${GREEN}Connection successful!${NC}" || echo -e "${YELLOW}Connection test completed (timeout expected)${NC}"
elif command -v wscat &> /dev/null; then
  echo '{"action":"ping"}' | timeout 5 wscat -c "${WS_URL}/ws?token=${TOKEN}" -w 3 2>/dev/null && echo -e "${GREEN}Connection successful!${NC}" || echo -e "${YELLOW}Connection test completed${NC}"
else
  echo -e "${YELLOW}Install wscat or websocat for interactive testing${NC}"
fi

echo ""
echo -e "${GREEN}=== Auth test complete ===${NC}"
