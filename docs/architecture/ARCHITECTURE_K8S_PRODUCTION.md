# Sukko WebSocket Service - Production Architecture

> **Version**: 1.0
> **Date**: 2025-12-04
> **Status**: Planned

## Overview

Production architecture for the Sukko WebSocket service using Cloudflare as the edge layer and Kubernetes for orchestration. This design prioritizes simplicity, security, and scalability while remaining flexible for future multi-tenant support.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLIENTS                                         │
│                     wss://ws.yourdomain.com/ws                              │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            CLOUDFLARE                                        │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │  Automatic (no config needed)          │  Optional (dashboard config)  │ │
│  │  ├─ DDoS Protection (L3/L4/L7)         │  ├─ WAF Rules                 │ │
│  │  ├─ TLS Termination (wss → ws)         │  ├─ Rate Limiting Rules       │ │
│  │  ├─ WebSocket Proxy                    │  ├─ Bot Fight Mode            │ │
│  │  └─ Global Anycast                     │  └─ IP Access Rules           │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│  Timeout: 100s idle (requires ping/pong in ws-server)                       │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
                                  │ (Origin: your-cluster-ip or domain)
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         KUBERNETES CLUSTER                                   │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │  Service: LoadBalancer (External IP)                                    │ │
│  │  └─ Exposes ws-server to Cloudflare                                    │ │
│  └────────────────────────────────┬───────────────────────────────────────┘ │
│                                   │                                          │
│                     ┌─────────────┼─────────────┐                           │
│                     │             │             │                           │
│                     ▼             ▼             ▼                           │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  WS-SERVER PODS                                                      │   │
│  │                                                                      │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │   │
│  │  │    Pod 1     │  │    Pod 2     │  │    Pod 3     │   (HPA)       │   │
│  │  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │               │   │
│  │  │  │Shard 0 │  │  │  │Shard 0 │  │  │  │Shard 0 │  │               │   │
│  │  │  │Shard 1 │  │  │  │Shard 1 │  │  │  │Shard 1 │  │               │   │
│  │  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │               │   │
│  │  └──────────────┘  └──────────────┘  └──────────────┘               │   │
│  │                                                                      │   │
│  │  Features:                                                           │   │
│  │  ├─ Auth (first message JWT) - NOT YET IMPLEMENTED                  │   │
│  │  ├─ Ping/Pong (27s interval, keeps Cloudflare alive) ✓ IMPLEMENTED  │   │
│  │  ├─ Rate Limiting (backup to Cloudflare) ✓ IMPLEMENTED              │   │
│  │  ├─ Shard Distribution (multi-core utilization)                     │   │
│  │  ├─ Subscription Management                                          │   │
│  │  └─ Graceful Shutdown                                                │   │
│  └──────────────────────────────────┬──────────────────────────────────┘   │
│                                     │                                        │
│                    ┌────────────────┴────────────────┐                      │
│                    │                                 │                      │
│                    │ connects to         consumes from                      │
│                    ▼                                 ▼                      │
│  ┌────────────────────────────┐    ┌────────────────────────────┐          │
│  │  VALKEY (Redis)            │    │  REDPANDA (Kafka)          │          │
│  │  BroadcastBus Pub/Sub      │    │  Event Ingestion           │          │
│  │                            │    │                            │          │
│  │  WS-Server publishes here  │    │  Publishers → Kafka        │          │
│  │  All pods subscribe        │    │  WS-Server consumes        │          │
│  │  = cross-pod broadcast     │    │                            │          │
│  └────────────────────────────┘    └────────────────────────────┘          │
│                                                                              │
│  Data Flow:                                                                  │
│  Publishers → Kafka → WS-Server → Valkey (BroadcastBus) → All Pods → Clients│
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │  MONITORING                                                          │   │
│  │  ├─ Prometheus (metrics collection)                                 │   │
│  │  ├─ Grafana (dashboards)                                            │   │
│  │  ├─ Loki (log aggregation)                                          │   │
│  │  └─ Promtail (log collection)                                       │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **Cloudflare** | DDoS protection, TLS termination, WAF, WebSocket proxy, Edge rate limiting |
| **K8s Service (LoadBalancer)** | Expose origin to Cloudflare, distribute traffic to pods |
| **WS-Server** | Ping/pong (✓), subscriptions (✓), shard distribution (✓), rate limiting (✓), Auth (planned) |
| **Valkey** | Cross-pod message broadcasting via Redis Pub/Sub |
| **Redpanda** | Event stream ingestion from publishers |
| **Monitoring** | Metrics, dashboards, log aggregation |

## Traffic Flow

```
1. Client connects: wss://ws.yourdomain.com/ws
                         │
2. Cloudflare:          │
   ├─ DDoS check        ✓
   ├─ TLS terminate     ✓ (wss → ws)
   ├─ WAF check         ✓
   └─ Proxy to origin   → ws://[K8s-LB-IP]:3001/ws
                              │
3. K8s Service:               │
   └─ Route to pod      → Pod (round-robin)
                              │
4. WS-Server:                 │
   ├─ Accept connection ✓
   ├─ Wait for auth msg (5s timeout)  ← NOT YET IMPLEMENTED
   ├─ Validate JWT      ✓ or close    ← NOT YET IMPLEMENTED
   ├─ Start ping/pong   every 27s (keeps Cloudflare alive) ✓
   └─ Handle subscriptions
                              │
5. Message Flow:              │
   Publisher → Kafka → WS-Server → BroadcastBus → All Pods → Clients
```

## Configuration

### Cloudflare Dashboard Settings

| Setting | Value | Location |
|---------|-------|----------|
| DNS | `ws.yourdomain.com → [K8s LB IP]` | DNS → Records |
| Proxy Status | Proxied (orange cloud) | DNS → Records |
| SSL/TLS Mode | Full (Strict) | SSL/TLS → Overview |
| WebSockets | ON | Network → WebSockets |
| Minimum TLS | 1.2 | SSL/TLS → Edge Certificates |

### Helm Values (Production)

```yaml
# values-production.yaml
global:
  namespace: sukko-prod

ws-server:
  enabled: true
  replicaCount: 3

  image:
    repository: gcr.io/your-project/sukko
    tag: "1.0.0"
    pullPolicy: Always

  service:
    type: LoadBalancer
    port: 3001
    annotations:
      # GKE
      cloud.google.com/load-balancer-type: "External"
      # AWS (alternative)
      # service.beta.kubernetes.io/aws-load-balancer-type: "nlb"

  config:
    shards: 4
    logLevel: info
    logFormat: json

    # Ping/Pong (already hardcoded in ws-server: 27s ping, 30s pong wait)
    # No config needed - compatible with Cloudflare's 100s idle timeout

    # Auth (NOT YET IMPLEMENTED - add when needed)
    # authEnabled: true
    # authTimeout: "5s"
    # jwtSecret from secret

    # Rate limiting (backup to Cloudflare)
    connRateLimit:
      enabled: true
      ipBurst: 100
      ipRate: 100.0
      globalBurst: 1000
      globalRate: 500.0

    # CPU protection
    cpuRejectThreshold: 85.0
    cpuPauseThreshold: 80.0

  resources:
    requests:
      cpu: "2"
      memory: "1Gi"
    limits:
      cpu: "4"
      memory: "2Gi"

  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 20
    targetCPUUtilizationPercentage: 70

valkey:
  enabled: true
  replicaCount: 1  # Use managed Redis for HA
  auth:
    enabled: true
  resources:
    requests:
      cpu: "500m"
      memory: "512Mi"
    limits:
      cpu: "1"
      memory: "1Gi"

redpanda:
  enabled: true
  replicaCount: 3
  resources:
    requests:
      cpu: "1"
      memory: "2Gi"
    limits:
      cpu: "2"
      memory: "4Gi"
  storage:
    enabled: true
    size: 50Gi

monitoring:
  enabled: true
  prometheus:
    enabled: true
    retention: "30d"
  grafana:
    enabled: true
  loki:
    enabled: true
    retention: "168h"
```

## WS-Server Current Implementation

### 1. Ping/Pong - ALREADY IMPLEMENTED

**File**: `ws/internal/wsserver/pump.go`

The ws-server already implements ping/pong with Cloudflare-compatible settings:

```go
// Current implementation (hardcoded in DefaultPumpConfig)
type PumpConfig struct {
    PongWait   time.Duration  // 30 seconds - time to wait for pong response
    WriteWait  time.Duration  // 5 seconds - timeout for write operations
    PingPeriod time.Duration  // 27 seconds - calculated as (PongWait * 9) / 10
}

func DefaultPumpConfig() PumpConfig {
    pongWait := 30 * time.Second
    return PumpConfig{
        PongWait:   pongWait,
        WriteWait:  5 * time.Second,
        PingPeriod: (pongWait * 9) / 10, // 27 seconds
    }
}
```

| Setting | Value | Cloudflare Compatibility |
|---------|-------|-------------------------|
| **PingPeriod** | 27 seconds | Well within 100s idle timeout |
| **PongWait** | 30 seconds | Connection closed if no pong |
| **WriteWait** | 5 seconds | Timeout for write operations |

> **Note**: These values are hardcoded. To make them configurable, add environment variables to `platform/config.go`.

## WS-Server Code Changes Required

### 1. Ping/Pong - OPTIONAL: Make Configurable

To make ping/pong configurable via environment variables (optional enhancement):

```go
// Add to platform/config.go
PingPeriod time.Duration `env:"WS_PING_PERIOD" envDefault:"27s"`
PongWait   time.Duration `env:"WS_PONG_WAIT" envDefault:"30s"`
WriteWait  time.Duration `env:"WS_WRITE_WAIT" envDefault:"5s"`
```

### 2. Authentication (First Message JWT)

```go
type AuthMessage struct {
    Type  string `json:"type"`
    Token string `json:"token"`
}

type Claims struct {
    UserID   string `json:"user_id"`
    // TenantID string `json:"tenant_id"`  // Future: multi-tenant
    jwt.StandardClaims
}

func (s *Shard) authenticateConnection(conn *websocket.Conn) (*Client, error) {
    // Set auth timeout
    conn.SetReadDeadline(time.Now().Add(s.config.AuthTimeout))

    // Read first message
    _, msg, err := conn.ReadMessage()
    if err != nil {
        return nil, fmt.Errorf("auth read failed: %w", err)
    }

    var authMsg AuthMessage
    if err := json.Unmarshal(msg, &authMsg); err != nil {
        return nil, fmt.Errorf("invalid auth message format")
    }

    if authMsg.Type != "auth" {
        return nil, fmt.Errorf("first message must be auth")
    }

    // Validate JWT
    claims, err := s.validateJWT(authMsg.Token)
    if err != nil {
        return nil, fmt.Errorf("invalid token: %w", err)
    }

    // Clear deadline for normal operation
    conn.SetReadDeadline(time.Time{})

    // Send auth success
    conn.WriteJSON(map[string]string{
        "type":    "auth_success",
        "user_id": claims.UserID,
    })

    return &Client{
        Conn:   conn,
        UserID: claims.UserID,
        // TenantID: claims.TenantID,  // Future
    }, nil
}
```

### 3. Config Additions (for Auth - when needed)

```go
// Add to platform/config.go when implementing auth
type Config struct {
    // Existing fields...

    // Auth (add when implementing authentication)
    AuthEnabled  bool          `env:"AUTH_ENABLED" envDefault:"false"`
    AuthTimeout  time.Duration `env:"AUTH_TIMEOUT" envDefault:"5s"`
    JWTSecret    string        `env:"JWT_SECRET"`
}
```

## Client Integration

### JavaScript Example

```javascript
class SukkoWebSocket {
    constructor(url, token) {
        this.url = url;
        this.token = token;
        this.authenticated = false;
    }

    connect() {
        this.ws = new WebSocket(this.url);

        this.ws.onopen = () => {
            // First message must be auth
            this.ws.send(JSON.stringify({
                type: 'auth',
                token: this.token
            }));
        };

        this.ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);

            if (msg.type === 'auth_success') {
                this.authenticated = true;
                this.onAuthenticated(msg);
            } else if (msg.type === 'error') {
                this.onError(msg);
            } else {
                this.onMessage(msg);
            }
        };

        this.ws.onclose = () => this.onClose();
        this.ws.onerror = (err) => this.onError(err);
    }

    subscribe(topics) {
        if (!this.authenticated) {
            throw new Error('Not authenticated');
        }
        this.ws.send(JSON.stringify({
            type: 'subscribe',
            topics: topics
        }));
    }

    // Override these
    onAuthenticated(msg) {}
    onMessage(msg) {}
    onError(err) {}
    onClose() {}
}

// Usage
const ws = new SukkoWebSocket('wss://ws.yourdomain.com/ws', 'jwt-token-here');
ws.onAuthenticated = () => {
    ws.subscribe(['sukko.trades', 'sukko.liquidity']);
};
ws.onMessage = (msg) => {
    console.log('Received:', msg);
};
ws.connect();
```

## Security Considerations

| Layer | Protection |
|-------|------------|
| **Edge (Cloudflare)** | DDoS, WAF, Bot protection, TLS |
| **Transport** | TLS 1.2+ (Cloudflare terminates) |
| **Auth** | JWT validation with expiry |
| **Rate Limiting** | Edge (Cloudflare) + Application (ws-server) |
| **Input Validation** | JSON schema validation on messages |

## Scaling Strategy

| Metric | Threshold | Action |
|--------|-----------|--------|
| CPU > 70% | Scale up | Add ws-server pods |
| CPU < 30% | Scale down | Remove ws-server pods |
| Connections > 50k/pod | Scale up | Add ws-server pods |
| Memory > 80% | Alert | Investigate leak |

## Future: Multi-Tenant Extension

When multi-tenant is needed, extend the architecture:

1. **Add tenant ID to JWT claims**
2. **Scope subscriptions by tenant**
3. **Add per-tenant rate limiting in ws-server**
4. **Optional: Add API Gateway (Kong) for tenant API key validation**

```go
// Future multi-tenant client
type Client struct {
    Conn     *websocket.Conn
    UserID   string
    TenantID string  // Add this
}

// Future tenant-scoped subscriptions
func (s *Shard) Subscribe(tenantID, topic string, client *Client) {
    key := fmt.Sprintf("%s:%s", tenantID, topic)
    s.subscriptions[key] = append(s.subscriptions[key], client)
}
```

## Deployment Checklist

- [ ] Configure Cloudflare DNS (proxied)
- [ ] Set Cloudflare SSL mode to Full (Strict)
- [ ] Enable WebSockets in Cloudflare Network settings
- [ ] Deploy Helm chart with production values
- [ ] Verify LoadBalancer gets external IP
- [ ] Update Cloudflare DNS with LoadBalancer IP
- [ ] Test WebSocket connection through Cloudflare
- [ ] Verify ping/pong keeps connection alive
- [ ] Test auth flow with JWT
- [ ] Verify monitoring dashboards
- [ ] Configure alerts
