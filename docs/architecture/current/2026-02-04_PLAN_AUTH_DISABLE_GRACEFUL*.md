# Plan: Graceful Auth Disable Without Breaking Code

**Status:** Planned
**Date:** 2026-02-04
**Scope:** Ensure all features degrade gracefully when `AUTH_ENABLED=false`

---

## Summary

When `AUTH_ENABLED=false`, the system should continue to function for development and testing without authentication. This plan documents which features work, which are unavailable, and the minimal changes needed.

**Key finding:** Most features already work correctly when auth is disabled. Three issues need fixing:
1. Publish channel mapping uses wrong tenant (`anonymous` instead of `odin`)
2. Subscribe channel mapping is bypassed entirely, causing subscription/broadcast mismatch
3. Auth data (`auth.Claims`) is inspected even when auth is disabled — routing should be separate from auth

---

## Current Behavior Analysis

### Gateway Message Interception (`proxy.go:291-294`)

```go
// When anonymous, ALL messages pass through unchanged
if p.claims == nil || p.claims.Subject == "anonymous" {
    return msg, nil  // Bypass all interception
}
```

This means when auth is disabled:
- Subscribe messages pass through unchanged (no filtering)
- Publish messages pass through unchanged (no tenant prefix added)

### What Actually Breaks

**Two issues need fixing:**

**1. Publish channel mapping (`proxy.go:410-420`):**
```go
if p.channelMapper != nil && p.claims != nil && p.claims.TenantID != "" {
    internalChannel = p.channelMapper.MapToInternal(p.claims, pubData.Channel)
}
```
When `TenantID = "anonymous"`, channels become `anonymous.BTC.trade` instead of `odin.BTC.trade`, breaking Kafka topic routing.

**2. Subscribe channel mapping bypassed (`proxy.go:291-294`):**
```go
if p.claims == nil || p.claims.Subject == "anonymous" {
    return msg, nil  // Bypass ALL interception including channel mapping
}
```
When auth disabled, subscriptions pass through unchanged. Client subscribes to `BTC.trade`, but CDC publishes with Key = `odin.BTC.trade`. The broadcast subject won't match the subscription.

---

## Features Status When Auth Disabled

### Works Correctly (No Changes Needed)

| Feature | Reason |
|---------|--------|
| WebSocket connections | Anonymous connections allowed (no claims object created) |
| Backend proxying | Messages forwarded unchanged |
| Rate limiting | Per-connection, not per-tenant |
| Message size limits | Checked before tenant logic |
| Provisioning API | `AuthEnabled` flag already respected |

### Optional Features (Not Available)

| Feature | Reason | Impact |
|---------|--------|--------|
| User-scoped patterns (`{principal}`) | Requires JWT subject | Subscription filtering only |
| Group-scoped patterns (`{group_id}`) | Requires JWT groups | Subscription filtering only |
| Multi-issuer OIDC | Not initialized | N/A for local dev |

**Note:** User/group patterns are ONLY used for subscription filtering (`PermissionChecker.FilterChannels`). When `authEnabled=false`, filtering is skipped entirely — no claims are inspected. Channel mapping (routing) still runs using `tenantID` from config.

### Needs Fix

| Feature | Issue | Solution |
|---------|-------|----------|
| Publish channel mapping | `anonymous.` prefix breaks routing | Use default tenant from config |
| Subscribe channel mapping | Bypassed entirely, no tenant prefix added | Add channel mapping to subscribe interception |
| Auth/routing coupling | Proxy uses `auth.Claims` for routing decisions | Separate `tenantID` (routing) from `claims` (auth) on proxy |

---

## Solution: Default Tenant ID

### Configuration

Add to Helm values and gateway config:

```yaml
# values/local.yaml
ws-gateway:
  config:
    authEnabled: false
    defaultTenantId: "odin"  # Used when auth disabled
```

```go
// gateway_config.go
// DefaultTenantID disables multi-tenant support. All connections
// are routed to this tenant. Only used when AUTH_ENABLED=false.
DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"odin"`
```

### Code Change

**File:** `ws/internal/gateway/gateway.go` (`HandleWebSocket`)

Resolve `principal` and `tenantID` as local variables at the top of `HandleWebSocket`. No claims object is created when auth is disabled — claims is nil. All logging and connection tracking uses the local variables, not claims fields.

```go
var claims *auth.Claims
var principal string
var tenantID string

if gw.config.AuthEnabled {
    // ... validate JWT ...
    claims = validatedClaims
    principal = claims.Subject
    tenantID = claims.TenantID
} else {
    // No auth = no claims object
    claims = nil
    principal = "anonymous"
    tenantID = gw.config.DefaultTenantID  // "odin"
}

// Connection tracking uses tenantID directly (not claims)
if gw.connTracker != nil && tenantID != "" {
    if !gw.connTracker.TryAcquire(tenantID) { ... }
    defer gw.connTracker.Release(tenantID)
}

// Logging uses local variables (not claims)
gw.logger.Info().
    Str("principal", principal).
    Str("tenant_id", tenantID).
    Msg("Client connected")

// Proxy gets claims (nil when auth disabled) + tenantID (always set)
proxy := NewProxy(ProxyConfig{
    AuthEnabled: gw.config.AuthEnabled,
    Claims:      claims,     // nil when auth disabled — proxy won't use it
    TenantID:    tenantID,   // always set — routing is independent of auth
    Logger:      gw.logger.With().Str("principal", principal).Logger(),
    // ...
})
```

**Result:** No claims object when auth disabled. Channel mapping uses `tenantID` from config. Logging and tracking use local variables.

---

## Implementation

### Phase 1: Add Config Field

**File:** `ws/internal/shared/platform/gateway_config.go`

```go
// DefaultTenantID disables multi-tenant support. All connections
// are routed to this tenant. Only used when AUTH_ENABLED=false.
DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"odin"`
```

### Phase 2: Separate Identity from Auth in Gateway

**File:** `ws/internal/gateway/gateway.go`

Refactor `HandleWebSocket` to resolve `principal` and `tenantID` as local variables. Remove fake anonymous claims creation. When auth disabled, `claims = nil` — all logging and connection tracking uses the local variables directly. Pass `authEnabled`, `tenantID`, and `claims` (nil when auth disabled) to the proxy as separate fields.

### Phase 3: Add Helm Value

**File:** `deployments/helm/odin/values/local.yaml`

```yaml
ws-gateway:
  config:
    defaultTenantId: "odin"
```

### Phase 4: Startup Warning

```go
if !config.AuthEnabled {
    logger.Warn().
        Str("default_tenant", config.DefaultTenantID).
        Msg("AUTH DISABLED - all connections use default tenant")
}
```

### Phase 5: Separate Routing from Auth on Proxy

**File:** `ws/internal/gateway/proxy.go`

**Design principle:** Auth data (`auth.Claims`) should never be inspected when auth is disabled. Channel mapping is **routing logic** (which Kafka topic/partition), not auth logic (who is allowed). These must be separate concerns on the proxy.

**Add two routing fields to Proxy (independent of claims):**
```go
type Proxy struct {
    // Auth (only used when authEnabled=true)
    claims      *auth.Claims
    permissions *PermissionChecker
    authEnabled bool

    // Routing (always used — from JWT when auth enabled, from config when disabled)
    tenantID      string
    channelMapper *auth.ChannelMapper

    // ... existing fields (rate limiter, size limits, etc.)
}
```

**Gateway sets these at connection time (see Phase 2):**
```go
// gateway.go — when creating proxy:
proxy := NewProxy(ProxyConfig{
    // Auth (claims is nil when auth disabled)
    AuthEnabled: gw.config.AuthEnabled,
    Claims:      claims,        // real JWT claims or nil
    Permissions: gw.permissions,

    // Routing (always set — from JWT or config)
    TenantID:      tenantID,    // from JWT claims.TenantID or config.DefaultTenantID
    ChannelMapper: gw.channelMapper,
})
```

**Fix `interceptClientMessage` — bypass only when no routing possible:**
```go
func (p *Proxy) interceptClientMessage(msg []byte) ([]byte, error) {
    // Defensive guard — tenantID is always set in practice (from JWT or config default).
    // This only triggers if config has empty DefaultTenantID AND auth is disabled.
    if p.tenantID == "" {
        return msg, nil
    }

    var clientMsg protocol.ClientMessage
    if err := json.Unmarshal(msg, &clientMsg); err != nil {
        return msg, nil
    }

    switch clientMsg.Type {
    case protocol.MsgTypeSubscribe:
        return p.interceptSubscribe(clientMsg)
    case protocol.MsgTypePublish:
        return p.interceptPublish(clientMsg)
    default:
        return msg, nil
    }
}
```

**Fix `interceptSubscribe` — filter only when auth enabled, map always:**
```go
func (p *Proxy) interceptSubscribe(clientMsg protocol.ClientMessage) ([]byte, error) {
    var subData protocol.SubscribeData
    if err := json.Unmarshal(clientMsg.Data, &subData); err != nil {
        return json.Marshal(clientMsg)
    }

    // 1. Permission filtering (auth only — never runs when auth disabled)
    channels := subData.Channels
    if p.authEnabled {
        channels = p.permissions.FilterChannels(p.claims, channels)
        // ... existing denied channel logging ...
    }

    // 2. Channel mapping (routing — always runs)
    channels = p.mapChannelsToInternal(channels)

    // 3. Rebuild message with mapped channels
    modifiedData := protocol.SubscribeData{Channels: channels}
    dataBytes, _ := json.Marshal(modifiedData)
    return json.Marshal(protocol.ClientMessage{Type: protocol.MsgTypeSubscribe, Data: dataBytes})
}
```

**Add `MapToInternalWithTenant` to ChannelMapper** (`shared/auth/channel.go`):

This new method takes `tenantID` directly instead of `*Claims`, so the proxy can map channels without needing auth data. It respects the `TenantImplicit` config flag and uses `config.Separator`.

```go
// MapToInternalWithTenant converts a client channel using an explicit tenant ID.
// Unlike MapToInternal, this does not require Claims — used when routing
// is separated from auth (e.g., auth disabled, tenant from config).
func (m *ChannelMapper) MapToInternalWithTenant(tenantID, clientChannel string) string {
    if tenantID == "" || !m.config.TenantImplicit {
        return clientChannel
    }
    return tenantID + m.config.Separator + clientChannel
}
```

**Add `mapChannelsToInternal` to Proxy** — uses `MapToInternalWithTenant`, not claims:
```go
func (p *Proxy) mapChannelsToInternal(channels []string) []string {
    if p.channelMapper == nil || p.tenantID == "" {
        return channels
    }
    mapped := make([]string, len(channels))
    for i, ch := range channels {
        if strings.HasPrefix(ch, p.tenantID+".") {
            mapped[i] = ch  // Already prefixed, avoid double-prefix
        } else {
            mapped[i] = p.channelMapper.MapToInternalWithTenant(p.tenantID, ch)
        }
    }
    return mapped
}
```

**Update `interceptPublish` — use `MapToInternalWithTenant` and `p.authEnabled`:**
```go
// Channel mapping (routing — always runs):
if p.channelMapper != nil && p.tenantID != "" {
    if strings.HasPrefix(pubData.Channel, p.tenantID+".") {
        internalChannel = pubData.Channel
    } else {
        internalChannel = p.channelMapper.MapToInternalWithTenant(p.tenantID, pubData.Channel)
    }
}

// Access validation (auth only — skipped when auth disabled):
if p.authEnabled && p.channelMapper != nil {
    if !p.channelMapper.ValidateChannelAccess(p.claims, internalChannel, p.crossTenantRoles) {
        return p.sendPublishErrorToClient(protocol.ErrCodeForbidden)
    }
}
```

**Why this works:**
1. When auth disabled: `authEnabled=false`, `tenantID="odin"` (from config)
2. No `auth.Claims` inspected — filtering and access checks skipped entirely
3. Channel mapping uses `p.tenantID` directly: `BTC.trade` → `odin.BTC.trade`
4. When auth enabled: `authEnabled=true`, `tenantID=claims.TenantID` (from JWT)
5. Full filtering and access checks run against real JWT claims
6. Same mapping logic, different source for tenantID

---

## Files to Change

| File | Change | Removable |
|------|--------|-----------|
| `shared/platform/gateway_config.go` | Add `DefaultTenantID` field | No |
| `shared/platform/server_config.go` | Add `DefaultTenantID` field (for producer) | No |
| `shared/auth/channel.go` | Add `MapToInternalWithTenant` method | No |
| `gateway/gateway.go` | Remove fake claims, resolve `principal`/`tenantID` as local vars, pass to proxy | No |
| `gateway/proxy.go` | Separate routing (`tenantID`) from auth (`claims`), add subscribe mapping | No |
| `server/server.go` | Add optional `ChannelMapper` field | Yes |
| `server/broadcast.go` | Strip tenant prefix before envelope | Yes |
| `shared/kafka/producer.go` | Add `DefaultTenantID`, update `parseChannel` | No |
| `cmd/server/main.go` | Pass `DefaultTenantID` from platform config to `ProducerConfig` | No |
| `values/local.yaml` | Add `defaultTenantId: "odin"` | No |

**Removable** = specific to odin CDC flow. When SaaS client-to-client is the primary flow, set `ChannelMapper` to nil and these lines become dead code to delete.

---

## Consumer Flow: No Changes Needed

**Verified:** The consumer does NOT need default tenant logic.

**Reason:** Consumer subscribes to **topics** (not channels). Topics come from TenantRegistry and are already fully qualified with tenant:

```
TenantRegistry.GetSharedTenantTopics(namespace)
  → queries tenant_categories table
  → builds topics: kafka.BuildTopicName("local", "odin", "trade") → "local.odin.trade"
```

**Message flow (corrected):**
```
Kafka Topic: local.odin.trade
Kafka Key:   odin.BTC.trade     ← internal channel WITH tenant prefix (for partitioning)
Kafka Value: {...}              ← message payload

Consumer reads:
  record.Topic = "local.odin.trade"  (from TenantRegistry - already has tenant)
  record.Key   = "odin.BTC.trade"    (internal channel = broadcast subject)
  record.Value = {...}

Broadcast to clients:
  subject = "odin.BTC.trade"         (clients must subscribe to internal channel format)
```

**Key insight:** The Kafka Key is the INTERNAL channel format (with tenant prefix). Clients subscribe to internal format after gateway mapping: `BTC.trade` → `odin.BTC.trade`. The consumer broadcasts to `odin.BTC.trade`, matching the mapped subscription.

**Why this works:** With the gateway fix (not bypassing channel mapping for anonymous), subscriptions are mapped to internal format even when auth is disabled. So client subscribes to `BTC.trade`, gateway maps to `odin.BTC.trade`, and broadcast matches.

### Reverse Mapping in Broadcast (Removable)

There is currently no conversion from internal channel (`odin.BTC.trade`) back to implicit client channel (`BTC.trade`) in the broadcast path. Clients receive `channel: "odin.BTC.trade"` in message envelopes instead of `channel: "BTC.trade"`.

**Why this exists:** The odin tenant's CDC pipeline publishes directly to Redpanda with tenant-prefixed keys (`odin.BTC.trade`). The consumer uses the Kafka Key as the broadcast subject. Without reverse mapping, the tenant prefix leaks to clients.

**Why it's removable:** In a pure client-to-client SaaS flow, messages go:
```
Client A → Gateway (adds tenant) → Server → Kafka (Key = internal channel) → Consumer → Broadcast → Client B
```
The Kafka Key will always contain the tenant prefix because the gateway adds it. However, in a future SaaS model where CDC is no longer the primary publisher, the producer could be changed to use the client channel as the Key instead of the internal channel, making this strip unnecessary.

**Design for removal:** Inject an optional `ChannelMapper` into the Server. If present, `Broadcast()` strips the tenant prefix. If nil, pass-through. Removing it later is a one-line config change.

**File:** `ws/internal/server/broadcast.go`

```go
// In Broadcast(), before building envelope:
clientChannel := channel
if s.channelMapper != nil {
    clientChannel = s.channelMapper.MapToClient(channel)
}

baseEnvelope := &messaging.MessageEnvelope{
    Channel: clientChannel,  // "BTC.trade" (stripped) or "odin.BTC.trade" (pass-through)
    // ...
}
```

**File:** `ws/internal/server/server.go`

```go
// Server struct - add optional field:
channelMapper *auth.ChannelMapper  // nil = no channel stripping (future SaaS mode)

// NewServer - inject from config:
if config.ChannelMapper != nil {
    s.channelMapper = config.ChannelMapper
}
```

**Note:** Subscription index continues to use INTERNAL format (`odin.BTC.trade`) for matching. Only the envelope channel sent to clients is stripped. This keeps broadcast O(1) lookup unchanged.

---

## CDC/Producer Flow: Default Tenant

### Current Behavior

**`producer.go:parseChannel()`** expects 3+ parts:
```go
// Channel format: {tenant}.{identifier}.{category}
// "odin.BTC.trade" → tenant: "odin", category: "trade"

func parseChannel(channel string) (tenant, category string, err error) {
    parts := strings.Split(channel, ".")
    if len(parts) < 3 {
        return "", "", fmt.Errorf("channel must have at least 3 parts")
    }
    tenant = parts[0]
    category = parts[len(parts)-1]
    return tenant, category, nil
}
```

**Problem:** If CDC sends `BTC.trade` (2 parts) or `trade` (1 part), it fails.

### Proposed Change

Add `DefaultTenantID` to `ProducerConfig` and use it when tenant is missing:

**File:** `ws/internal/shared/kafka/producer.go`

```go
type ProducerConfig struct {
    // ... existing fields ...

    // DefaultTenantID is used when channel has no tenant prefix.
    // For channels like "BTC.trade", prepends this tenant.
    // Default: "odin"
    DefaultTenantID string
}
```

**Updated `parseChannel` logic:**

```go
// parseChannelWithDefault extracts tenant and category, using default if needed.
//
// Examples with DefaultTenantID = "odin":
//   "odin.BTC.trade"  → tenant: "odin", category: "trade" (explicit tenant)
//   "BTC.trade"       → tenant: "odin", category: "trade" (2 parts: use default)
//   "trade"           → tenant: "odin", category: "trade" (1 part: use default)
//
func (p *Producer) parseChannelWithDefault(channel string) (tenant, category string, err error) {
    parts := strings.Split(channel, ".")

    switch len(parts) {
    case 1:
        // Just category: "trade" → use default tenant
        return p.defaultTenantID, parts[0], nil
    case 2:
        // identifier.category: "BTC.trade" → use default tenant
        return p.defaultTenantID, parts[1], nil
    default:
        // 3+ parts: tenant.identifier.category
        return parts[0], parts[len(parts)-1], nil
    }
}
```

### Topic Building Examples

| Input Channel | Tenant | Category | Built Topic |
|---------------|--------|----------|-------------|
| `odin.BTC.trade` | `odin` | `trade` | `local.odin.trade` |
| `BTC.trade` | `odin` (default) | `trade` | `local.odin.trade` |
| `trade` | `odin` (default) | `trade` | `local.odin.trade` |

### Files to Change

| File | Change |
|------|--------|
| `shared/kafka/producer.go` | Add `DefaultTenantID` to config, update parsing |
| `shared/platform/server_config.go` | Add `DefaultTenantID` env var field |
| `cmd/server/main.go` | Pass `cfg.DefaultTenantID` to `ProducerConfig` |

---

## Complete Implementation Summary

### Configuration Changes

**Gateway Config (`gateway_config.go`):**
```go
// DefaultTenantID disables multi-tenant support. All connections
// are routed to this tenant. Only used when AUTH_ENABLED=false.
DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"odin"`
```

**Server Config (`server_config.go`):**
```go
// DefaultTenantID disables multi-tenant support. All messages
// are routed to this tenant. Only used when AUTH_ENABLED=false.
DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"odin"`
```

**Producer Config (`producer.go`):**
```go
DefaultTenantID string // Default: "odin"
```

**Helm Values (`local.yaml`):**
```yaml
global:
  defaultTenantId: "odin"

ws-gateway:
  config:
    defaultTenantId: "{{ .Values.global.defaultTenantId }}"

ws-server:
  config:
    defaultTenantId: "{{ .Values.global.defaultTenantId }}"
```

### Code Changes Summary

| Component | Change | Lines | Removable |
|-----------|--------|-------|-----------|
| Gateway config | Add `DefaultTenantID` field | ~3 | No |
| Server config | Add `DefaultTenantID` field | ~3 | No |
| ChannelMapper | Add `MapToInternalWithTenant(tenantID, channel)` method | ~8 | No |
| Gateway `HandleWebSocket` | Remove fake claims, resolve `principal`/`tenantID` as local vars | ~15 | No |
| Proxy struct | Add `authEnabled bool`, `tenantID string` fields | ~5 | No |
| Proxy bypass | Change to `if p.tenantID == ""` (defensive guard) | ~2 | No |
| Proxy `interceptSubscribe` | Add channel mapping via `mapChannelsToInternal` | ~15 | No |
| Proxy `interceptPublish` | Use `MapToInternalWithTenant` and `p.authEnabled` | ~10 | No |
| Proxy `mapChannelsToInternal` | New helper using `MapToInternalWithTenant` | ~12 | No |
| Server struct | Add optional `ChannelMapper` field | ~3 | Yes |
| Server broadcast | Strip tenant prefix via `ChannelMapper` | ~5 | Yes |
| Producer config | Add `DefaultTenantID` field | ~3 | No |
| Producer parsing | Use default when tenant missing | ~15 | No |
| `cmd/server/main.go` | Pass `DefaultTenantID` to `ProducerConfig` | ~1 | No |
| Helm local values | Add global default | ~5 | No |

**Total:** ~105 lines (~8 removable when CDC flow is retired)

---

## Feature Availability Summary

| Feature | Auth Enabled | Auth Disabled |
|---------|--------------|---------------|
| WebSocket connections | ✅ | ✅ |
| Subscribe (channel mapping) | ✅ Mapped via JWT tenant | ✅ Mapped via config tenant |
| Subscribe (filtering) | ✅ Per channel rules | Skipped (`authEnabled=false`) |
| Publish (channel mapping) | ✅ Via JWT tenant | ✅ Via config tenant |
| Publish (access check) | ✅ Per claims | Skipped (`authEnabled=false`) |
| CDC publish (with tenant) | ✅ | ✅ |
| CDC publish (no tenant) | ❌ Error | ✅ (with default tenant fix) |
| User-scoped patterns | ✅ | N/A (no JWT) |
| Group-scoped patterns | ✅ | N/A (no JWT) |
| Broadcast reverse mapping | ✅ Strip tenant from envelope | ✅ Strip tenant from envelope |
| Provisioning API | ✅ Protected | ✅ Open |

**Legend:**
- ✅ Works
- N/A - Feature not applicable (requires JWT, not available when auth disabled)
- Skipped - Auth logic not executed, no claims inspected
