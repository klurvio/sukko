# WebSocket Gateway Service

## Overview

The **ws-gateway** service:
1. Handles JWT authentication (removes auth from ws-server)
2. Validates subscription permissions (public/user/group channels)
3. Proxies WebSocket connections to ws-server
4. Keeps ws-server as a dumb broadcaster

## Architecture

```
┌──────────┐                  ┌─────────────┐                  ┌────────────┐
│  Client  │ ──── WSS ──────► │ ws-gateway  │ ──── WS ───────► │ ws-server  │
│          │                  │             │                  │ (dumb pipe)│
└──────────┘                  │ • Auth JWT  │                  │            │
     │                        │ • Check sub │                  │ • Broadcast│
     │                        │ • Proxy     │                  │ • No auth  │
     ▼                        └─────────────┘                  └────────────┘
┌──────────┐                         │
│ sukko-api │ ◄───────────────────────┘ (same JWT secret)
│ (tokens) │
└──────────┘
```

## Authentication Policy

**All connections require a valid JWT token.** No anonymous access.

| Token State | Result |
|-------------|--------|
| No token | 401 Rejected |
| Invalid/expired token | 401 Rejected |
| Valid token | Connected |

**Why no anonymous access:**
- Rate limit per identity (not just IP) - can't bypass with rotating IPs
- Can revoke/ban bad actors by token
- Full audit trail for all connections
- Prevents connection flooding attacks

## Permission Model

### Channel Categories

| Category | Pattern | Validation |
|----------|---------|------------|
| Public | `*.trade`, `*.liquidity`, `*.metadata` | Valid token (any user) |
| User-scoped | `balances.{principal}`, `notifications.{principal}` | JWT.sub == {principal} |
| Group-scoped | `community.{group_id}`, `social.{group_id}` | {group_id} in JWT.groups |

### Channel Routing

Kafka topics are shared, WebSocket channels are scoped:

```
Kafka Topic:                     WebSocket Channels:
┌────────────────────────┐       ┌─────────────────────────┐
│ sukko.balances          │ ────► │ balances.user1          │
│ (ALL user updates)     │       │ balances.user2          │
│                        │       │ balances.userN          │
│ Message:               │       └─────────────────────────┘
│ { "user_id": "abc" }   │               ▲
└────────────────────────┘       ws-server routes by user_id
```

- **Kafka**: Few topics (`sukko.balances`, `sukko.community`, etc.)
- **WebSocket channels**: Many (per-user, per-group) - just map entries
- **Routing**: ws-server extracts ID from message payload, broadcasts to correct channel

### JWT Claims Structure

```go
type Claims struct {
    jwt.RegisteredClaims
    TenantID string   `json:"tenant_id"`
    Groups   []string `json:"groups,omitempty"`  // For group-scoped channels
}
```

## Files to Create

| Path | Purpose |
|------|---------|
| `ws/cmd/gateway/main.go` | Gateway entrypoint |
| `ws/internal/gateway/gateway.go` | Core gateway logic |
| `ws/internal/gateway/permissions.go` | Channel permission validation |
| `ws/internal/gateway/proxy.go` | WebSocket proxy (reuse SlotAwareProxy pattern) |
| `ws/internal/gateway/config.go` | Configuration loading |
| `deployments/k8s/helm/sukko/charts/ws-gateway/` | Helm subchart |
| `deployments/k8s/helm/sukko/charts/ws-server/templates/networkpolicy.yaml` | Restrict access to gateway only |

## Files to Modify

| Path | Changes |
|------|---------|
| `ws/internal/auth/jwt.go` | Add `Groups` field to Claims (for gateway) |
| `ws/internal/wsserver/handlers_ws.go` | Remove all auth validation code |
| `ws/internal/wsserver/server.go` | Remove token monitor, auth config |
| `ws/cmd/server/main.go` | Remove auth-related env var loading |
| `deployments/k8s/helm/sukko/values.yaml` | Add ws-gateway config, remove ws-server auth config |
| `deployments/k8s/helm/sukko/Chart.yaml` | Add ws-gateway dependency |
| `deployments/k8s/helm/sukko/charts/ws-server/values.yaml` | Remove auth section |
| `deployments/k8s/helm/sukko/charts/ws-server/templates/deployment.yaml` | Remove auth env vars |

## Implementation Steps

### Phase 1: Gateway Core

1. **Create `ws/cmd/gateway/main.go`**
   - Load config (JWT secret, ws-server address, permissions)
   - Start HTTP server with WebSocket upgrade handler
   - Graceful shutdown

2. **Create `ws/internal/gateway/gateway.go`**
   ```go
   type Gateway struct {
       jwtValidator *auth.JWTValidator
       permissions  *PermissionChecker
       wsServerURL  string
       logger       zerolog.Logger
   }

   func (gw *Gateway) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
       // 1. Extract token (query param or header)
       // 2. Validate JWT (reject if invalid)
       // 3. Upgrade to WebSocket
       // 4. Connect to ws-server backend
       // 5. Proxy messages with interception
   }
   ```

3. **Create `ws/internal/gateway/permissions.go`**
   ```go
   type PermissionChecker struct {
       publicPatterns      []string  // *.trade, *.liquidity
       userScopedPatterns  []string  // balances.{principal}
       groupScopedPatterns []string  // community.{group_id}
   }

   func (pc *PermissionChecker) CanSubscribe(claims *Claims, channel string) bool {
       // Check public patterns (any authenticated user)
       // Check user-scoped (JWT.sub must match)
       // Check group-scoped (JWT.groups must contain)
   }
   ```

4. **Create `ws/internal/gateway/proxy.go`**
   - Reuse SlotAwareProxy pattern from `ws/internal/orchestration/proxy.go`
   - Bidirectional message forwarding
   - Intercept `subscribe` messages for permission check

### Phase 2: Message Interception

```go
func (gw *Gateway) interceptClientMessage(msg []byte, claims *Claims) ([]byte, error) {
    var req struct {
        Type string          `json:"type"`
        Data json.RawMessage `json:"data"`
    }
    json.Unmarshal(msg, &req)

    if req.Type == "subscribe" {
        var subReq struct {
            Channels []string `json:"channels"`
        }
        json.Unmarshal(req.Data, &subReq)

        // Filter to allowed channels only
        allowed := []string{}
        for _, ch := range subReq.Channels {
            if gw.permissions.CanSubscribe(claims, ch) {
                allowed = append(allowed, ch)
            } else {
                gw.logger.Warn().Str("channel", ch).Msg("subscription denied")
            }
        }

        // Return modified request with only allowed channels
        return json.Marshal(map[string]any{
            "type": "subscribe",
            "data": map[string]any{"channels": allowed},
        })
    }

    return msg, nil  // Pass through other messages
}
```

### Phase 3: ws-server Changes

1. **Remove all auth code from ws-server**
   - Delete JWT validation logic
   - Delete auth-related environment variables (`WS_AUTH_ENABLED`, `WS_JWT_SECRET`, etc.)
   - Delete token monitor / expiry handling
   - ws-server becomes a pure broadcaster with no auth awareness

2. **Security enforced at network level**
   - NetworkPolicy restricts access to gateway pods only
   - No application-level auth needed in ws-server

### Phase 4: Helm Charts

1. **Create `charts/ws-gateway/`**
   ```
   charts/ws-gateway/
   ├── Chart.yaml
   ├── values.yaml
   ├── templates/
   │   ├── deployment.yaml
   │   ├── service.yaml
   │   ├── configmap.yaml
   │   └── _helpers.tpl
   ```

2. **Gateway values.yaml**
   ```yaml
   ws-gateway:
     enabled: true
     replicaCount: 2

     config:
       port: 3000
       wsServerURL: "ws://sukko-server:3001/ws"
       logLevel: info

     permissions:
       public:
         - "*.trade"
         - "*.liquidity"
         - "*.metadata"
       userScoped:
         - "balances.{principal}"
         - "notifications.{principal}"
       groupScoped:
         - "community.{group_id}"
         - "social.{group_id}"

     service:
       type: LoadBalancer  # External access
       port: 443

     resources:
       requests:
         cpu: "250m"
         memory: "128Mi"
       limits:
         cpu: "500m"
         memory: "256Mi"
   ```

3. **Update parent Chart.yaml**
   ```yaml
   dependencies:
     - name: ws-gateway
       version: "0.1.0"
       condition: ws-gateway.enabled
   ```

### Phase 5: Kubernetes Topology

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              GKE Autopilot                              │
│                                                                         │
│   ┌───────────────────────────────────────────────────────────────┐    │
│   │                     External LoadBalancer                      │    │
│   │                        (HTTPS/WSS :443)                        │    │
│   └───────────────────────────┬───────────────────────────────────┘    │
│                               │                                         │
│   ┌───────────────────────────▼───────────────────────────────────┐    │
│   │                      ws-gateway (2+ pods)                      │    │
│   │   • JWT required         • Permission checks                   │    │
│   │   • Rate limit/principal • Subscribe filtering                 │    │
│   └───────────────────────────┬───────────────────────────────────┘    │
│                               │ ClusterIP (internal)                    │
│                               │ NetworkPolicy: only gateway allowed     │
│   ┌───────────────────────────▼───────────────────────────────────┐    │
│   │                      ws-server (N pods, HPA)                   │    │
│   │   • Dumb broadcaster     • No auth logic                       │    │
│   │   • Kafka consumer       • Subscription index                  │    │
│   └───────────────────────────────────────────────────────────────┘    │
│                                                                         │
│   ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐    │
│   │    Redpanda     │  │      NATS       │  │     Prometheus      │    │
│   │    (Kafka)      │  │  (Broadcast)    │  │     + Grafana       │    │
│   └─────────────────┘  └─────────────────┘  └─────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────┘
```

### Phase 6: Network Security

**NetworkPolicy to restrict ws-server access to gateway only:**

```yaml
# templates/ws-server-networkpolicy.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: ws-server-ingress
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: ws-server
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app.kubernetes.io/name: ws-gateway
      ports:
        - port: 3001
          protocol: TCP
```

This ensures:
- ws-server only accepts connections from ws-gateway pods
- Direct access to ws-server (bypassing gateway) is blocked at network level
- No auth code needed in ws-server - security enforced by network

## Configuration Files

### Gateway Config (ConfigMap)

```yaml
# /etc/gateway/config.yaml
server:
  port: 3000
  readTimeout: 15s
  writeTimeout: 15s

backend:
  url: "ws://sukko-server:3001/ws"
  dialTimeout: 10s

auth:
  jwtSecret: "${JWT_SECRET}"  # From secret

permissions:
  public:
    - "*.trade"
    - "*.liquidity"
    - "*.metadata"
  userScoped:
    - "balances.{principal}"
    - "notifications.{principal}"
  groupScoped:
    - "community.{group_id}"
    - "social.{group_id}"

logging:
  level: info
  format: json
```

## Testing Plan

1. **Unit tests**
   - JWT validation (valid, expired, invalid signature)
   - Permission checker: public/user/group patterns
   - Message interception: subscribe filtering

2. **Integration tests**
   - No token → 401 rejected
   - Invalid token → 401 rejected
   - Valid token → public + user channels allowed
   - Group member → group channels allowed
   - Non-member → group channels denied
   - Subscription denial logged

3. **Local testing**
   ```bash
   # Get token from sukko-api first
   TOKEN=$(curl -s -X POST http://sukko-api/auth -d '...' | jq -r '.token')

   # No token - rejected
   wscat -c "ws://localhost:3000/ws"
   # Connection rejected: 401 Unauthorized

   # With valid token
   wscat -c "ws://localhost:3000/ws?token=$TOKEN"
   > {"type":"subscribe","data":{"channels":["BTC.trade","balances.abc123"]}}
   < {"type":"subscription_ack","subscribed":["BTC.trade","balances.abc123"]}

   # Try subscribing to another user's channel - denied
   > {"type":"subscribe","data":{"channels":["balances.xyz789"]}}
   < {"type":"subscription_ack","subscribed":[]}  # Empty - denied
   ```

## Rollout Strategy

1. Remove auth code from ws-server (clean up unused code)
2. Add NetworkPolicy to ws-server (gateway-only access)
3. Deploy ws-gateway alongside ws-server
4. Test with internal traffic
5. Switch external LoadBalancer from ws-server to ws-gateway
6. Monitor for permission denials

## Token Issuance

JWT tokens are issued by **sukko-api** (Firebase Functions). The flow is:

1. Client authenticates with IC Principal to sukko-api
2. sukko-api validates the IC Principal
3. sukko-api issues a JWT token with claims:
   - `sub`: User's principal ID
   - `tenant_id`: Tenant identifier
   - `groups`: List of group IDs (for group-scoped channels)
4. Client connects to ws-gateway with the JWT token

## Dependencies

- sukko-api must include `groups` in JWT claims for group-scoped channels
- Same `JWT_SECRET` shared between sukko-api (issuer) and ws-gateway (validator)

## Local Development

### Build and Deploy

```bash
# Setup everything (kind cluster, build, deploy)
task k8s:local:setup

# Build gateway only
task k8s:common:build:gateway

# View logs
task k8s:local:logs:gateway
```
