# Generic Multi-Tenant Authentication System for WebSocket

**Date:** 2026-01-20 (Updated)

---

## Overview

Design and implement a generic, secure, flexible, testable, and robust multi-tenant authentication and authorization system for WebSocket servers. Not odin-specific - reusable for any SaaS.

---

## Goals

1. **Generic** - Not product-specific, reusable for any multi-tenant SaaS
2. **Secure** - Tenant isolation enforced, fail-secure defaults, audit logging
3. **Flexible** - Config-driven rules, custom placeholders, extensible claims
4. **Testable** - Unit testable policies, integration test patterns
5. **Robust** - Handle edge cases, graceful degradation

---

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Infrastructure** | Multi-tenant shared | Cost-effective, standard for SaaS |
| **Kafka Cluster** | Shared + tenant topics | One cluster, ACL-enforced isolation |
| **Topic Naming** | `{env}.{tenant}.{resource}` | Environment + tenant prevents cross-contamination |
| **Channel Naming** | Tenant implicit | Client sees `BTC.trade`, server maps to `{tenant}.BTC.trade` |
| **Consumer Strategy** | Hybrid (Option C) | Shared default, dedicated for large tenants |
| **Migration** | Hard cutover | No backward compatibility period |

**Naming Examples:**
```
Client subscribes:  BTC.trade
Server internal:    acme.BTC.trade      (tenant from JWT)
Kafka topic:        main.acme.trade     (env + tenant + resource)
```

---

## Industry Standards Followed

- **Pusher/Ably**: Channel naming conventions with tenant prefixes
- **RBAC + ABAC**: Hybrid permission model (roles + attributes)
- **Tenant Isolation First**: Check tenant before any other authorization
- **Declarative Policies**: YAML-driven rules, not hardcoded logic
- **Short-lived Tokens**: JWT with refresh capability

---

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Gateway                                  │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │ JWT         │  │ Tenant      │  │ Policy Engine           │ │
│  │ Validator   │──│ Isolator    │──│ (Rule Evaluation)       │ │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘ │
│         │                │                      │               │
│         ▼                ▼                      ▼               │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                  Authorization Flow                          ││
│  │  1. Validate JWT → 2. Check Tenant → 3. Evaluate Rules      ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

---

## Current Implementation Status

> **As of 2026-01-20:** Foundation components exist, full PolicyEngine is planned.

| Component | Status | Location |
|-----------|--------|----------|
| JWT Validator | **Implemented** | `ws/internal/auth/jwt.go` |
| Claims (basic) | **Implemented** | `ws/internal/auth/jwt.go` - has TenantID, Groups |
| Claims (enhanced) | Planned | Need Attributes, Roles, Scopes, Custom |
| PermissionChecker | **Implemented** | `ws/internal/gateway/permissions.go` |
| Gateway auth flow | **Implemented** | `ws/internal/gateway/gateway.go` |
| Subscribe filtering | **Implemented** | `ws/internal/gateway/proxy.go` |
| Auth config | **Implemented** | `ws/internal/platform/gateway_config.go` |
| PolicyEngine | Planned | Full YAML-driven rule engine |
| TenantIsolator | Planned | Channel-level tenant boundaries |
| TopicIsolator | Planned | Kafka topic tenant boundaries |
| Audit logging | Planned | Authorization decision logging |

---

## Design Principle: Auth Implies Isolation

**Single master switch:** `auth.enabled` controls everything.

```
┌─────────────────────────────────────────────────────────────────┐
│                      auth.enabled: false                         │
├─────────────────────────────────────────────────────────────────┤
│  - No JWT validation                                             │
│  - No tenant isolation                                           │
│  - No topic isolation                                            │
│  - No policy rules evaluated                                     │
│  - Open access (for development/POC)                             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      auth.enabled: true                          │
├─────────────────────────────────────────────────────────────────┤
│  - JWT validation enforced                                       │
│  - Tenant isolation enforced (channels)                          │
│  - Topic isolation enforced (Kafka)                              │
│  - Policy rules evaluated                                        │
│  - Secure multi-tenant mode                                      │
└─────────────────────────────────────────────────────────────────┘
```

**Why no separate `enabled` flags?**

| Problem with separate flags | Result |
|-----------------------------|--------|
| `auth: on, tenant_isolation: off` | Security hole - authenticated but no boundaries |
| `auth: on, topic_isolation: off` | Security hole - tenant A can access tenant B topics |
| `auth: off, isolation: on` | Impossible - no claims to check |

**Conclusion:** Auth and isolation are inseparable. One switch, no confusion.

---

## Implementation Plan

### Phase 1: Enhanced Claims Structure

**File:** `ws/internal/auth/jwt.go` (modify existing Claims struct)

> **Current State:** Claims struct exists with `TenantID` and `Groups`. Enhancement needed to add `Attributes`, `Roles`, `Scopes`, and `Custom` fields.

```go
type Claims struct {
    jwt.RegisteredClaims

    // Tenant isolation (REQUIRED)
    TenantID string `json:"tenant_id"`

    // Identity attributes (for placeholder resolution)
    Attributes map[string]string `json:"attrs,omitempty"`

    // RBAC
    Roles []string `json:"roles,omitempty"`

    // Group memberships
    Groups []string `json:"groups,omitempty"`

    // Permission scopes
    Scopes []string `json:"scopes,omitempty"`

    // Extension point
    Custom map[string]any `json:"custom,omitempty"`
}

// Helper methods
func (c *Claims) UserID() string              // Returns sub (user identifier)
func (c *Claims) TenantID() string            // Returns tenant_id
func (c *Claims) HasRole(role string) bool
func (c *Claims) HasScope(scope string) bool
func (c *Claims) HasGroup(group string) bool
func (c *Claims) GetAttribute(key string) string
func (c *Claims) ResolveTemplate(template string) string
```

---

### Phase 2: Generic Permission Rule Engine

**File:** `ws/internal/auth/engine.go` (new)

```go
// PermissionRule - single authorization rule
type PermissionRule struct {
    ID          string      `yaml:"id"`
    Description string      `yaml:"description"`
    Priority    int         `yaml:"priority"`      // Higher = evaluated first
    Match       RuleMatch   `yaml:"match"`         // Channel pattern
    Actions     []Action    `yaml:"actions"`       // subscribe, publish, presence
    Conditions  []Condition `yaml:"conditions"`    // AND logic
    Effect      Effect      `yaml:"effect"`        // allow or deny
}

// Condition - requirement that must be met
type Condition struct {
    Type   ConditionType `yaml:"type"`   // claim, attribute, channel, time
    Field  string        `yaml:"field"`  // claim path (e.g., "roles", "attrs.tier")
    Op     Operator      `yaml:"op"`     // eq, contains, matches, exists, in
    Value  any           `yaml:"value"`
    Negate bool          `yaml:"negate"` // NOT operator
}

// PolicyEngine - evaluates rules
type PolicyEngine struct {
    rules         []*PermissionRule
    tenantRules   map[string][]*PermissionRule
    placeholders  *PlaceholderResolver
    auditLogger   AuditLogger
    defaultEffect Effect
}

func (e *PolicyEngine) Authorize(ctx context.Context, req *AuthzRequest) *AuthzResult
```

---

### Phase 3: Placeholder Resolution

**File:** `ws/internal/auth/placeholders.go` (new)

```go
type PlaceholderResolver struct {
    builtins map[string]PlaceholderFunc
    custom   map[string]PlaceholderFunc
}

// Built-in placeholders (generic, not product-specific)
var DefaultPlaceholders = map[string]PlaceholderFunc{
    "user_id":   func(c *Claims) string { return c.Subject },
    "tenant_id": func(c *Claims) string { return c.TenantID },
    "tenant":    func(c *Claims) string { return c.TenantID },
    "sub":       func(c *Claims) string { return c.Subject },
}

// Resolve {placeholder} in patterns
func (r *PlaceholderResolver) Resolve(pattern string, claims *Claims) string

// Extract values from channel given pattern
func (r *PlaceholderResolver) Extract(pattern, channel string) (map[string]string, bool)
```

---

### Phase 4: Tenant Isolation

**File:** `ws/internal/auth/tenant.go` (new)

```go
type TenantIsolator struct {
    config TenantIsolationConfig
    parser *ChannelParser
}

type TenantIsolationConfig struct {
    // No "Enabled" field - auth enabled = isolation enforced
    StrictMode            bool     `yaml:"strict_mode"`
    CrossTenantRoles      []string `yaml:"cross_tenant_roles"`
    SharedChannelPatterns []string `yaml:"shared_channel_patterns"`
}

func (t *TenantIsolator) CheckTenantAccess(claims *Claims, channel string) *TenantCheckResult
```

---

### Phase 5: Channel Naming (Tenant Implicit)

**File:** `ws/internal/auth/channel.go` (new)

**Design Decision:** Tenant is implicit in channels (derived from JWT), not explicit in channel name.

```
┌─────────────────────────────────────────────────────────────┐
│ Client API (Simple)          │ Server Internal (Tenant-Aware)│
├──────────────────────────────┼───────────────────────────────┤
│ subscribe("BTC.trade")       │ → acme.BTC.trade              │
│ subscribe("ETH.liquidity")   │ → acme.ETH.liquidity          │
│ subscribe("balances")        │ → acme.user123.balances       │
└──────────────────────────────┴───────────────────────────────┘
        Client sees                    Server maps (from JWT)
```

**Why Tenant Implicit:**
- Cleaner client API (matches Pusher/Ably industry standard)
- Tenant derived from JWT claims (already authenticated)
- Less error-prone (client can't subscribe to wrong tenant)
- No breaking change to channel format

```go
type ChannelMapper struct {
    config ChannelConfig
}

type ChannelConfig struct {
    Separator string `yaml:"separator"` // default: "."
}

// MapToInternal converts client channel to internal tenant-prefixed channel
// Client: "BTC.trade" → Internal: "acme.BTC.trade"
func (m *ChannelMapper) MapToInternal(claims *Claims, clientChannel string) string {
    return fmt.Sprintf("%s.%s", claims.TenantID, clientChannel)
}

// MapToClient converts internal channel back to client format
// Internal: "acme.BTC.trade" → Client: "BTC.trade"
func (m *ChannelMapper) MapToClient(internalChannel string) string {
    parts := strings.SplitN(internalChannel, ".", 2)
    if len(parts) == 2 {
        return parts[1]
    }
    return internalChannel
}

// Channel format (internal): {tenant_id}.{symbol}.{event_type}
// Examples:
//   acme.BTC.trade              - Public market data (tenant: acme)
//   acme.user123.balances       - User-scoped balances
//   acme.vip.community          - Group-scoped community
```

---

### Phase 6: Topic Isolation (Kafka/Redpanda)

**File:** `ws/internal/auth/topic.go` (new)

**Design Decision:** Topics include BOTH environment AND tenant for isolation. No prefix needed (dedicated cluster assumed).

```
┌─────────────────────────────────────────────────────────┐
│ Topic Format: {environment}.{tenant_id}.{resource}      │
├─────────────────────────────────────────────────────────┤
│ dev.acme.trade           ← Dev, Acme tenant, trades     │
│ main.acme.trade          ← Prod, Acme tenant, trades    │
│ main.globex.trade        ← Prod, Globex tenant, trades  │
│ main.acme.client-events  ← Prod, client-published       │
└─────────────────────────────────────────────────────────┘
```

**Why Environment + Tenant (no prefix):**
- Prevents dev code from accidentally consuming prod data
- Prevents cross-environment contamination
- ACLs can enforce both environment AND tenant boundaries
- No prefix needed - dedicated Kafka cluster assumed
- Simpler, shorter topic names

```go
// TopicIsolator enforces tenant boundaries on Kafka/Redpanda topics
// CRITICAL: Tenants can ONLY publish/consume topics they own
type TopicIsolator struct {
    config TopicIsolationConfig
}

type TopicIsolationConfig struct {
    // No "Enabled" field - auth enabled = isolation enforced

    // Environment deployment environment (dev, staging, main)
    Environment string `yaml:"environment" json:"environment"`

    // TenantPosition position of tenant in topic name (0-indexed)
    TenantPosition int `yaml:"tenant_position" json:"tenant_position"` // 1 for env.tenant.resource

    // Separator between topic parts (default: ".")
    Separator string `yaml:"separator" json:"separator"`

    // StrictMode rejects topics without tenant (default: true)
    StrictMode bool `yaml:"strict_mode" json:"strict_mode"`

    // CrossTenantRoles roles that can access cross-tenant topics (e.g., admin)
    CrossTenantRoles []string `yaml:"cross_tenant_roles" json:"cross_tenant_roles"`

    // SharedTopicPatterns topics accessible by all tenants (e.g., system broadcasts)
    SharedTopicPatterns []string `yaml:"shared_topic_patterns" json:"shared_topic_patterns"`
}

// BuildTopicName constructs a topic name with environment and tenant
func BuildTopicName(env, tenant, resource string) string {
    return fmt.Sprintf("%s.%s.%s", env, tenant, resource)
    // Example: main.acme.trade
}

// ExtractTenantFromTopic extracts tenant_id from topic name
func ExtractTenantFromTopic(topic string) string {
    // main.acme.trade → acme (position 1)
    parts := strings.Split(topic, ".")
    if len(parts) >= 3 {
        return parts[1]
    }
    return ""
}

// CheckTopicAccess verifies tenant can access topic
func (t *TopicIsolator) CheckTopicAccess(claims *Claims, topic string, action TopicAction) *TopicCheckResult

type TopicAction string
const (
    TopicActionPublish TopicAction = "publish"
    TopicActionConsume TopicAction = "consume"
)

type TopicCheckResult struct {
    Allowed       bool
    TopicTenant   string // Tenant extracted from topic
    ClaimsTenant  string // Tenant from claims
    Reason        string
    IsCrossTenant bool   // True if cross-tenant access was granted (for audit)
}
```

**Topic Naming Convention:**
```
{environment}.{tenant_id}.{resource}

Examples:
  main.acme.trade          - Prod, Acme, trade events
  main.acme.liquidity      - Prod, Acme, liquidity events
  main.globex.trade        - Prod, Globex, trade events
  dev.acme.trade           - Dev, Acme, trade events
  main.acme.client-events  - Prod, Acme, client-published
  main.system.broadcast    - Prod, cross-tenant system messages
```

---

### Phase 7: Consumer Strategy (Hybrid)

**Design Decision:** Shared consumers by default, dedicated consumers for large tenants.

```
┌─────────────────────────────────────────────────────────────────┐
│                         Redpanda Cluster                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  main.acme.trade      ──┐                                        │
│  main.smallco.trade    ├──► Shared Consumer Group               │
│  main.startup.trade   ──┘    "ws-shared"                        │
│                              Pattern: main\..*\.trade            │
│                              (excludes dedicated tenants)        │
│                                                                  │
│  main.whale.trade     ────► Dedicated Consumer Group            │
│                              "ws-whale"                          │
│                                                                  │
│  main.enterprise.trade ───► Dedicated Consumer Group            │
│                              "ws-enterprise"                     │
└─────────────────────────────────────────────────────────────────┘
```

**File:** `ws/internal/kafka/consumer_pool.go` (new)

```go
type ConsumerPoolConfig struct {
    Environment      string
    SharedGroupID    string              // "ws-shared"
    ExcludeTenants   []string            // ["whale", "enterprise"]
    DedicatedTenants []DedicatedConfig
}

type DedicatedConfig struct {
    TenantID string
    GroupID  string
}

type ConsumerPool struct {
    shared    *Consumer              // Handles most tenants (regex pattern)
    dedicated map[string]*Consumer   // tenant_id → dedicated consumer
}

// Shared consumer uses regex to match all tenants EXCEPT dedicated ones
// Pattern: main\.(?!whale|enterprise)[^.]+\.trade
func (p *ConsumerPool) buildSharedPattern() string

// Promotes tenant from shared to dedicated (zero downtime)
func (p *ConsumerPool) PromoteToDedicated(tenantID string) error
```

**When to Graduate Tenant to Dedicated:**

| Metric | Threshold | Action |
|--------|-----------|--------|
| Messages/sec | >10K | Consider dedicated |
| Consumer lag | >5 min sustained | Dedicated needed |
| Connection count | >1000 concurrent | Dedicated needed |
| SLA tier | Enterprise | Dedicated by default |

**Configuration:**
```yaml
consumer_strategy:
  shared:
    enabled: true
    group_id: "odin-ws-shared"
    pattern: "odin.{env}.*.{topic}"
    exclude_tenants: []  # Start empty, add as needed

  dedicated: []  # Start empty, add large tenants as needed
    # - tenant_id: "whale"
    #   group_id: "odin-ws-whale"
```

---

### Phase 8: Configuration Schema

**File:** `config/auth-policy.yaml` (new)

```yaml
version: "1.0"

# MASTER SWITCH: auth.enabled controls everything
# - auth.enabled: false → No JWT validation, no isolation (open access for dev/POC)
# - auth.enabled: true  → JWT required, all isolation enforced (secure multi-tenant)
auth:
  enabled: true                  # Master switch - enables JWT + all isolation
  signing_method: HS256
  required_claims: [tenant_id, sub]

settings:
  default_effect: deny           # Fail-secure (only applies when auth enabled)
  audit_denials: true

# Channel naming: tenant IMPLICIT (derived from JWT, not in channel name)
# Client sees: BTC.trade → Server maps to: {tenant_id}.BTC.trade
channels:
  separator: "."
  tenant_implicit: true                # Tenant derived from JWT, not channel name
  # Client subscribes to "BTC.trade", server internally uses "acme.BTC.trade"

# No "enabled" field - controlled by auth.enabled
tenant_isolation:
  strict_mode: true
  cross_tenant_roles: [admin, system]
  shared_channel_patterns:
    - "system.*"                       # Cross-tenant system broadcasts

# Topic naming: {environment}.{tenant_id}.{resource}
# Includes BOTH environment AND tenant for isolation (no prefix - dedicated cluster)
topic_isolation:
  strict_mode: true                    # Reject topics without tenant
  environment: "main"                  # dev, staging, main
  tenant_position: 1                   # env.{tenant_id}.resource
  separator: "."
  cross_tenant_roles: [admin, system]  # Roles that can access other tenants
  shared_topic_patterns:               # Topics accessible by all tenants
    - "*.system.*"                     # Cross-tenant system messages
  # ENFORCEMENT: Tenants can ONLY access {env}.{their_tenant_id}.*
  # Example: tenant "acme" in prod can only access main.acme.* topics

# Consumer strategy: Hybrid (shared default, dedicated for large tenants)
consumer_strategy:
  shared:
    group_id: "ws-shared"
    pattern: "{environment}.*.{resource}"
    exclude_tenants: []                # Large tenants get dedicated consumers
  dedicated: []                        # Add as needed: [{tenant_id, group_id}]

placeholders:
  user_id: "sub"
  tenant_id: "tenant_id"
  sub: "sub"

# Rules apply to INTERNAL channel format: {tenant_id}.{symbol}.{event}
# Client channel "BTC.trade" becomes internal "{tenant_id}.BTC.trade"
rules:
  - id: public-market-data
    description: "Public market data channels for all authenticated users"
    priority: 100
    match:
      # Internal: acme.BTC.trade, acme.ETH.liquidity
      channel_pattern: "{tenant_id}.*.{trade|liquidity|metadata|analytics}"
    actions: [subscribe]
    effect: allow

  - id: user-own-channels
    description: "Users can access their own scoped channels"
    priority: 90
    match:
      # Internal: acme.user123.balances, acme.user123.notifications
      channel_pattern: "{tenant_id}.{user_id}.*"
    actions: [subscribe]
    effect: allow

  - id: group-member-channels
    description: "Group members can access group channels"
    priority: 80
    match:
      # Internal: acme.vip.community, acme.traders.social
      channel_pattern: "{tenant_id}.{group_id}.*"
    actions: [subscribe]
    conditions:
      - type: claim
        field: groups
        op: contains
        value: "{group_id}"
    effect: allow
```

---

### Phase 9: Documentation Updates

**Files to Update:**
- `docs/CLIENT_GUIDE.md` - Client-facing documentation
- `ws/asyncapi/asyncapi.yaml` - AsyncAPI specification
- `ws/asyncapi/channel/*.yaml` - Channel definitions

#### 9.1 CLIENT_GUIDE.md Updates

Add Multi-Tenant Architecture section explaining:
- Tenant-implicit channel naming (industry standard: Pusher/Ably)
- How tenant_id from JWT maps channels internally
- Client API unchanged (simple channel names)

**Changes:**

| Section | Change |
|---------|--------|
| Table of Contents | Add "Multi-Tenant Architecture" link |
| After Authentication | Add new "Multi-Tenant Architecture" section |
| Available Channels | Add note about tenant scoping |
| Related Documentation | Add link to MULTI_TENANT_AUTH.md |

**New Section Content:**
```markdown
## Multi-Tenant Architecture

Odin uses a **tenant-implicit** channel naming pattern. Your channels are
automatically scoped to your tenant based on your JWT token.

### How It Works

When you subscribe to a channel like `BTC.trade`, the server automatically
maps it to your tenant:

| Layer              | Example                                |
|--------------------|----------------------------------------|
| Client subscribes  | BTC.trade                              |
| Server internal    | acme.BTC.trade     ← tenant from JWT   |
| Kafka topic        | main.acme.trade    ← env + tenant      |

### What This Means for You

1. **Simple API**: Subscribe to `BTC.trade`, not `acme.BTC.trade`
2. **Automatic isolation**: You only receive your tenant's data
3. **No cross-tenant access**: Cannot subscribe to another tenant's channels
4. **Tenant from JWT**: The `tenant_id` claim in your token determines your tenant
```

#### 9.2 AsyncAPI Specification Updates

Update topic naming to reflect multi-tenant pattern: `{env}.{tenant}.{category}` (no prefix - dedicated cluster).

**asyncapi.yaml Changes:**

| Section | Current | New |
|---------|---------|-----|
| Description | `odin.<category>` | `{env}.{tenant}.{category}` |
| Topics Overview | `odin.trades` | `{env}.{tenant}.trades` |
| Channel names | `odin.trades` | `{env}.{tenant}.trades` |
| Examples | `producer.Produce("odin.trades", ...)` | `producer.Produce(kafka.BuildTopicName(...), ...)` |

**New Description Section:**
```yaml
description: |
  Topic naming follows the multi-tenant pattern: `{env}.{tenant}.{category}`

  | Component | Description | Example |
  |-----------|-------------|---------|
  | `{env}` | Environment | `dev`, `staging`, `main` |
  | `{tenant}` | Tenant ID from JWT | `acme`, `globex` |
  | `{category}` | Event category | `trades`, `liquidity` |

  Example: `main.acme.trades`

  ## Multi-Tenant Isolation
  - Each tenant's data is isolated to their own topics
  - ACLs enforce tenant boundaries at the Kafka level
  - Environment prevents cross-environment contamination
  - No prefix needed - dedicated Kafka/Redpanda cluster assumed

  ## Consumer Strategy
  - **Shared consumers**: Small tenants share consumer groups with regex patterns
  - **Dedicated consumers**: Large tenants get dedicated consumer groups
```

**Channel Reference Updates:**
```yaml
channels:
  {env}.{tenant}.trades:
    $ref: './channel/trades.yaml#/OdinTrades'
  {env}.{tenant}.liquidity:
    $ref: './channel/liquidity.yaml#/OdinLiquidity'
  # ... etc
```

**Operation Updates:**
- Update all `$ref` paths to use new channel names
- Update code examples to use `kafka.BuildTopicName()`

#### 9.3 Regenerate bundled.yaml

After updating asyncapi.yaml and channel/*.yaml:
```bash
cd ws/asyncapi
npx @asyncapi/cli bundle asyncapi.yaml -o bundled.yaml
```

---

## Files to Create/Modify

### Code Files

| File | Action | Purpose |
|------|--------|---------|
| `ws/internal/auth/jwt.go` | Modify | Enhance Claims struct with Attributes, Roles, Scopes, Custom |
| `ws/internal/auth/engine.go` | Create | Policy engine |
| `ws/internal/auth/placeholders.go` | Create | Placeholder resolution |
| `ws/internal/auth/tenant.go` | Create | Tenant isolation (channels) |
| `ws/internal/auth/topic.go` | Create | Topic isolation (Kafka) |
| `ws/internal/auth/channel.go` | Create | Channel mapper (tenant implicit) |
| `ws/internal/kafka/consumer_pool.go` | Create | Hybrid consumer strategy |
| `ws/internal/kafka/topics.go` | Modify | Add tenant to topic building |
| `ws/internal/auth/audit.go` | Create | Audit logging |
| `ws/internal/platform/gateway_config.go` | Modify | Load policy config |
| `ws/internal/gateway/gateway.go` | Modify | Integrate PolicyEngine |
| `ws/internal/gateway/proxy.go` | Modify | Add publish permission check |
| `ws/internal/kafka/producer.go` | Modify | Add topic isolation check |
| `config/auth-policy.yaml` | Create | Default policy configuration |

### Documentation Files

| File | Action | Purpose |
|------|--------|---------|
| `docs/CLIENT_GUIDE.md` | Modify | Add Multi-Tenant Architecture section, update channel docs |
| `ws/asyncapi/asyncapi.yaml` | Modify | Update topic naming to `{env}.{tenant}.{category}` |
| `ws/asyncapi/channel/*.yaml` | Modify | Update channel references for new naming |
| `ws/asyncapi/bundled.yaml` | Regenerate | Bundle updated AsyncAPI spec |

> **Note:** Gateway config was refactored from `gateway/config.go` to `platform/gateway_config.go` following idiomatic Go patterns (config via dependency injection).

---

## Test Strategy

### Unit Tests

```go
// Test tenant isolation (channels)
func TestPolicyEngine_TenantIsolation(t *testing.T)

// Test topic isolation (Kafka)
func TestTopicIsolator_TenantAccess(t *testing.T)

// Test placeholder resolution
func TestPlaceholderResolver_CustomPlaceholders(t *testing.T)

// Test rule priority
func TestRuleEvaluation_Priority(t *testing.T)

// Test deny overrides allow
func TestRuleEvaluation_DenyOverride(t *testing.T)

// Test channel parsing
func TestChannelParser_Formats(t *testing.T)

// Test topic parsing
func TestTopicParser_Formats(t *testing.T)
```

### Integration Tests

```go
// Multi-tenant WebSocket integration
func TestGateway_MultiTenant_Integration(t *testing.T)

// Cross-tenant channel access denied
func TestGateway_CrossTenant_ChannelDenied(t *testing.T)

// Cross-tenant topic access denied
func TestGateway_CrossTenant_TopicDenied(t *testing.T)

// Role-based access
func TestGateway_RBAC_Integration(t *testing.T)

// Topic publish isolation
func TestKafkaProducer_TopicIsolation(t *testing.T)
```

### Security Tests

- Tenant A cannot access Tenant B channels
- Tenant A cannot publish to Tenant B topics
- Tenant A cannot consume from Tenant B topics
- Expired tokens rejected
- Invalid signatures rejected
- Missing required claims rejected
- Audit logs capture all denials

---

## Implementation Order

### Step 1: Core Components
- Create claims.go with enhanced Claims structure
- Create placeholders.go with resolver
- Create channel.go and topic.go parsers

### Step 2: Isolation Enforcement
- Create tenant.go for channel isolation
- Add topic isolation to topic.go
- Create audit.go for logging

### Step 3: Policy Engine
- Create engine.go with rule evaluation
- Load rules from auth-policy.yaml
- Integrate with gateway

### Step 4: Documentation Updates
- Update `docs/CLIENT_GUIDE.md` with Multi-Tenant Architecture section
- Update `ws/asyncapi/asyncapi.yaml` with new topic naming convention
- Update `ws/asyncapi/channel/*.yaml` with new channel references
- Regenerate `ws/asyncapi/bundled.yaml`

### Step 5: Testing & Verification
- Unit tests for all components
- Integration tests for multi-tenant scenarios
- Security tests for isolation enforcement
- Verify documentation accuracy

---

## Verification

1. **Unit tests pass**: `go test ./internal/auth/...`
2. **Integration tests pass**: `go test ./internal/gateway/... -tags=integration`
3. **Channel tenant isolation**: Cross-tenant channel access blocked
4. **Topic tenant isolation**: Cross-tenant topic access blocked
5. **Audit logging**: All denials logged with context
6. **Config reload**: Policy changes without restart

---

## Security Checklist

- [ ] Single master switch: `auth.enabled` controls all security features
- [ ] Default effect is `deny` (fail-secure)
- [ ] Channel tenant isolation checked before rule evaluation
- [ ] Topic tenant isolation checked before publish/consume
- [ ] All authorization decisions audited
- [ ] JWT signature verified
- [ ] JWT expiration enforced
- [ ] Required claims validated
- [ ] No implicit trust between tenants
- [ ] No cross-tenant topic access
- [ ] Rate limiting per user
