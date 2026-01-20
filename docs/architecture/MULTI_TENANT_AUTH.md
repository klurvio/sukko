# Generic Multi-Tenant Authentication System for WebSocket

**Date:** 2026-01-17

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

**File:** `ws/internal/auth/claims.go` (new)

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

### Phase 5: Channel Parser

**File:** `ws/internal/auth/channel.go` (new)

```go
type ChannelParser struct {
    config ChannelConfig
}

type ChannelConfig struct {
    Separator      string   `yaml:"separator"`       // default: "."
    RequiredPrefix string   `yaml:"required_prefix"` // e.g., "ws"
    TenantPosition int      `yaml:"tenant_position"` // 0-indexed
    MinParts       int      `yaml:"min_parts"`
    MaxParts       int      `yaml:"max_parts"`
    ResourceTypes  []string `yaml:"resource_types"`
}

// Channel format: ws.{tenant_id}.{scope}.{resource}.{action}
// Examples:
//   ws.acme.public.BTC.trade           - Public market data
//   ws.acme.user.user123.notifications - User notifications
//   ws.acme.group.traders.chat         - Group chat
```

---

### Phase 6: Topic Isolation (Kafka/Message Bus)

**File:** `ws/internal/auth/topic.go` (new)

```go
// TopicIsolator enforces tenant boundaries on Kafka/message bus topics
// CRITICAL: Tenants can ONLY publish/consume topics they own
type TopicIsolator struct {
    config TopicIsolationConfig
}

type TopicIsolationConfig struct {
    // No "Enabled" field - auth enabled = isolation enforced

    // TopicPrefix required prefix for all topics (e.g., "odin")
    TopicPrefix string `yaml:"topic_prefix" json:"topic_prefix"`

    // TenantPosition position of tenant in topic name (0-indexed after prefix)
    TenantPosition int `yaml:"tenant_position" json:"tenant_position"`

    // Separator between topic parts (default: ".")
    Separator string `yaml:"separator" json:"separator"`

    // StrictMode rejects topics without tenant prefix (default: true)
    StrictMode bool `yaml:"strict_mode" json:"strict_mode"`

    // CrossTenantRoles roles that can access cross-tenant topics (e.g., admin)
    CrossTenantRoles []string `yaml:"cross_tenant_roles" json:"cross_tenant_roles"`

    // SharedTopicPatterns topics accessible by all tenants (e.g., system broadcasts)
    SharedTopicPatterns []string `yaml:"shared_topic_patterns" json:"shared_topic_patterns"`
}

// Topic format: {prefix}.{tenant_id}.{resource}[.{sub_resource}]
// Examples:
//   odin.acme.trade               - Trade events for tenant acme
//   odin.acme.client-events       - Client-published events for acme
//   odin.system.broadcast         - Cross-tenant system messages (shared)

// CheckTopicAccess verifies tenant can access topic
// ENFORCEMENT RULES:
//   1. Extract tenant_id from topic name
//   2. Compare with claims.TenantID
//   3. DENY if tenant mismatch (unless cross-tenant role or shared topic)
//   4. DENY if topic has no tenant prefix (strict mode)
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
{prefix}.{tenant_id}.{resource_type}[.{sub_resource}]

Examples:
  odin.acme.trade               - Trade events
  odin.acme.liquidity           - Liquidity events
  odin.acme.user.notifications  - User notifications
  odin.acme.client-events       - Client-published events
  odin.system.broadcast         - Cross-tenant system messages
```

---

### Phase 7: Configuration Schema

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

channels:
  separator: "."
  required_prefix: "ws"
  tenant_position: 1
  resource_types: [public, user, group, private]

# No "enabled" field - controlled by auth.enabled
tenant_isolation:
  strict_mode: true
  cross_tenant_roles: [admin, system]
  shared_channel_patterns:
    - "ws.system.*"

# No "enabled" field - controlled by auth.enabled
topic_isolation:
  strict_mode: true                    # Reject topics without tenant prefix
  topic_prefix: "odin"
  tenant_position: 1                   # odin.{tenant_id}.resource
  separator: "."
  cross_tenant_roles: [admin, system]  # Roles that can access other tenants
  shared_topic_patterns:               # Topics accessible by all tenants
    - "odin.system.*"
  # ENFORCEMENT: Tenants can ONLY access odin.{their_tenant_id}.*
  # Example: tenant "acme" can only access odin.acme.* topics

placeholders:
  user_id: "sub"
  tenant_id: "tenant_id"
  sub: "sub"

rules:
  - id: public-channels
    description: "Public channels for all authenticated users"
    priority: 100
    match:
      channel_pattern: "ws.{tenant_id}.public.*"
    actions: [subscribe]
    effect: allow

  - id: user-own-channels
    description: "Users can access their own channels"
    priority: 90
    match:
      channel_pattern: "ws.{tenant_id}.user.{user_id}.*"
    actions: [subscribe, publish]
    effect: allow

  - id: group-member-channels
    description: "Group members can access group channels"
    priority: 80
    match:
      channel_pattern: "ws.{tenant_id}.group.{group_id}.*"
    actions: [subscribe, publish]
    conditions:
      - type: claim
        field: groups
        op: contains
        value: "{group_id}"
    effect: allow
```

---

## Files to Create/Modify

| File | Action | Purpose |
|------|--------|---------|
| `ws/internal/auth/claims.go` | Create | Enhanced claims structure |
| `ws/internal/auth/engine.go` | Create | Policy engine |
| `ws/internal/auth/placeholders.go` | Create | Placeholder resolution |
| `ws/internal/auth/tenant.go` | Create | Tenant isolation (channels) |
| `ws/internal/auth/topic.go` | Create | Topic isolation (Kafka) |
| `ws/internal/auth/channel.go` | Create | Channel parser |
| `ws/internal/auth/audit.go` | Create | Audit logging |
| `ws/internal/auth/jwt.go` | Modify | Add claim helpers |
| `ws/internal/gateway/config.go` | Modify | Load policy config |
| `ws/internal/gateway/gateway.go` | Modify | Integrate PolicyEngine |
| `ws/internal/gateway/proxy.go` | Modify | Add publish permission check |
| `ws/internal/kafka/producer.go` | Modify | Add topic isolation check |
| `config/auth-policy.yaml` | Create | Default policy configuration |

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

### Step 4: Testing & Verification
- Unit tests for all components
- Integration tests for multi-tenant scenarios
- Security tests for isolation enforcement

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
