// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"errors"
	"fmt"
	"strings"
)

// TopicAction represents an action on a Kafka topic.
type TopicAction string

const (
	// TopicActionPublish allows publishing to a topic.
	TopicActionPublish TopicAction = "publish"
	// TopicActionConsume allows consuming from a topic.
	TopicActionConsume TopicAction = "consume"
)

// TopicIsolator enforces tenant boundaries on Kafka/Redpanda topics.
// Topics follow the format: {environment}.{tenant_id}.{category}
//
// Example:
//
//	prod.acme.trade     - Production, Acme tenant, trade events
//	dev.globex.liquidity - Development, Globex tenant, liquidity events
type TopicIsolator struct {
	config TopicIsolationConfig
}

// TopicIsolationConfig configures topic isolation behavior.
type TopicIsolationConfig struct {
	// Environment is the deployment environment (dev, stag, prod).
	// Used as the first part of topic names.
	Environment string `yaml:"environment" json:"environment"`

	// TenantPosition is the position of tenant in topic name (0-indexed).
	// For format {env}.{tenant}.{category}, position is 1.
	TenantPosition int `yaml:"tenant_position" json:"tenant_position"`

	// Separator between topic parts (default: ".").
	Separator string `yaml:"separator" json:"separator"`

	// CrossTenantRoles are roles that can access any tenant's topics.
	// Example: ["admin", "system"]
	CrossTenantRoles []string `yaml:"cross_tenant_roles" json:"cross_tenant_roles"`

	// SharedTopicPatterns are topics accessible by all tenants.
	// These must be explicitly configured - no implicit sharing.
	// Example: ["*.system.*", "prod.shared.*"]
	SharedTopicPatterns []string `yaml:"shared_topic_patterns" json:"shared_topic_patterns"`

	// ValidNamespaces restricts which namespace prefixes are accepted by ValidateTopicFormat.
	// If empty, namespace validation is skipped (any non-empty namespace is accepted).
	// Example: {"local": true, "dev": true, "stag": true, "prod": true}
	ValidNamespaces map[string]bool `yaml:"valid_namespaces" json:"valid_namespaces"`
}

// DefaultTopicIsolationConfig returns sensible defaults.
// Always fail-secure: topics without valid tenant are rejected.
func DefaultTopicIsolationConfig() TopicIsolationConfig {
	return TopicIsolationConfig{
		Environment:         "prod",
		TenantPosition:      1, // {env}.{tenant}.{category}
		Separator:           ".",
		CrossTenantRoles:    []string{"admin", "system"},
		SharedTopicPatterns: []string{},
		ValidNamespaces:     map[string]bool{"local": true, "dev": true, "stag": true, "prod": true},
	}
}

// NewTopicIsolator creates a topic isolator with the given configuration.
func NewTopicIsolator(config TopicIsolationConfig) *TopicIsolator {
	if config.Separator == "" {
		config.Separator = "."
	}
	if config.Environment == "" {
		config.Environment = "prod"
	}
	return &TopicIsolator{config: config}
}

// TopicCheckResult contains the result of a topic access check.
type TopicCheckResult struct {
	// Allowed indicates whether access is permitted.
	Allowed bool

	// TopicTenant is the tenant extracted from the topic name.
	TopicTenant string

	// ClaimsTenant is the tenant from the JWT claims.
	ClaimsTenant string

	// Reason explains why access was allowed or denied.
	Reason string

	// IsCrossTenant indicates cross-tenant access was granted (for audit).
	IsCrossTenant bool

	// IsSharedTopic indicates the topic is shared across tenants.
	IsSharedTopic bool
}

// CheckTopicAccess verifies that the claims allow access to the topic.
// Returns a detailed result explaining the decision.
func (t *TopicIsolator) CheckTopicAccess(claims *Claims, topic string, _ TopicAction) *TopicCheckResult {
	result := &TopicCheckResult{
		ClaimsTenant: "",
	}

	// Extract claims tenant if available
	if claims != nil {
		result.ClaimsTenant = claims.TenantID
	}

	// No claims = auth disabled, allow all
	if claims == nil || claims.TenantID == "" {
		result.Allowed = true
		result.Reason = "auth disabled or no tenant in claims"
		return result
	}

	// Check if shared topic (must be explicitly configured)
	if t.isSharedTopic(topic) {
		result.Allowed = true
		result.IsSharedTopic = true
		result.Reason = "shared topic accessible by all tenants"
		return result
	}

	// Extract tenant from topic
	topicTenant := t.ExtractTenantFromTopic(topic)
	result.TopicTenant = topicTenant

	// Fail-secure: reject topics without valid tenant segment
	// Topics must either be in SharedTopicPatterns or have a valid tenant
	if topicTenant == "" {
		result.Allowed = false
		result.Reason = "topic missing tenant segment (not in shared patterns)"
		return result
	}

	// Check tenant match
	if topicTenant == claims.TenantID {
		result.Allowed = true
		result.Reason = "tenant match"
		return result
	}

	// Check cross-tenant roles
	for _, role := range t.config.CrossTenantRoles {
		if claims.HasRole(role) {
			result.Allowed = true
			result.IsCrossTenant = true
			result.Reason = "cross-tenant access via role: " + role
			return result
		}
	}

	// Denied: tenant mismatch
	result.Allowed = false
	result.Reason = fmt.Sprintf("tenant mismatch: claims=%s, topic=%s",
		claims.TenantID, topicTenant)
	return result
}

// ExtractTenantFromTopic extracts the tenant ID from a topic name.
// Returns empty string if tenant cannot be extracted.
//
// Example:
//
//	"prod.acme.trade" → "acme" (position 1)
//	"acme.trade" → "" (not enough parts for position 1)
func (t *TopicIsolator) ExtractTenantFromTopic(topic string) string {
	parts := strings.Split(topic, t.config.Separator)

	if len(parts) <= t.config.TenantPosition {
		return ""
	}

	return parts[t.config.TenantPosition]
}

// BuildTopicName constructs a topic name from environment, tenant, and category.
//
// Example:
//
//	BuildTopicName("acme", "trade") → "prod.acme.trade"
func (t *TopicIsolator) BuildTopicName(tenant, category string) string {
	return strings.Join([]string{t.config.Environment, tenant, category}, t.config.Separator)
}

// BuildTopicNameWithEnv constructs a topic name with a specified environment.
func (t *TopicIsolator) BuildTopicNameWithEnv(env, tenant, category string) string {
	return strings.Join([]string{env, tenant, category}, t.config.Separator)
}

// isSharedTopic checks if the topic matches any shared topic pattern.
func (t *TopicIsolator) isSharedTopic(topic string) bool {
	for _, pattern := range t.config.SharedTopicPatterns {
		if matchTopicPattern(pattern, topic, t.config.Separator) {
			return true
		}
	}
	return false
}

// matchTopicPattern checks if a topic matches a pattern.
// Supports * as wildcard for any single segment.
func matchTopicPattern(pattern, topic, separator string) bool {
	if pattern == topic {
		return true
	}

	patternParts := strings.Split(pattern, separator)
	topicParts := strings.Split(topic, separator)

	if len(patternParts) != len(topicParts) {
		return false
	}

	for i, pp := range patternParts {
		if pp == "*" {
			continue // Wildcard matches any segment
		}
		if pp != topicParts[i] {
			return false
		}
	}

	return true
}

// TopicParts represents the parsed components of a topic name.
type TopicParts struct {
	Environment string // First segment (e.g., "prod", "dev")
	Tenant      string // Tenant ID (e.g., "acme")
	Category    string // Rest of the topic (e.g., "trade", "trade.v2")
	Original    string // Original topic string
}

// ParseTopic parses a topic into its components.
//
// Example:
//
//	"prod.acme.trade" → {Environment: "prod", Tenant: "acme", Category: "trade"}
//	"prod.acme.trade.v2" → {Environment: "prod", Tenant: "acme", Category: "trade.v2"}
func (t *TopicIsolator) ParseTopic(topic string) *TopicParts {
	parts := strings.Split(topic, t.config.Separator)
	result := &TopicParts{Original: topic}

	if len(parts) > 0 {
		result.Environment = parts[0]
	}

	if len(parts) > t.config.TenantPosition {
		result.Tenant = parts[t.config.TenantPosition]
	}

	// Category is everything after tenant
	if len(parts) > t.config.TenantPosition+1 {
		result.Category = strings.Join(parts[t.config.TenantPosition+1:], t.config.Separator)
	}

	return result
}

// ValidateTopicFormat checks if a topic follows the expected format.
// Returns an error describing the issue if invalid.
// When ValidNamespaces is configured, also validates the namespace prefix.
func (t *TopicIsolator) ValidateTopicFormat(topic string) error {
	if topic == "" {
		return errors.New("topic cannot be empty")
	}

	parts := strings.Split(topic, t.config.Separator)

	// Minimum parts: env.tenant.category
	if len(parts) < 3 {
		return fmt.Errorf("topic must have at least 3 parts (env.tenant.category), got %d", len(parts))
	}

	// Check environment
	if parts[0] == "" {
		return errors.New("environment (first part) cannot be empty")
	}

	// Validate namespace against configured set (if configured)
	if len(t.config.ValidNamespaces) > 0 && !t.config.ValidNamespaces[parts[0]] {
		return fmt.Errorf("namespace %q is not valid", parts[0])
	}

	// Check tenant
	if parts[t.config.TenantPosition] == "" {
		return fmt.Errorf("tenant (position %d) cannot be empty", t.config.TenantPosition)
	}

	// Check category
	if parts[t.config.TenantPosition+1] == "" {
		return errors.New("category cannot be empty")
	}

	return nil
}

// TopicAccessChecker provides a simplified interface for topic access checks.
// Useful when you don't need the full TopicCheckResult.
type TopicAccessChecker interface {
	CanPublish(claims *Claims, topic string) bool
	CanConsume(claims *Claims, topic string) bool
}

// CanPublish checks if claims allow publishing to the topic.
func (t *TopicIsolator) CanPublish(claims *Claims, topic string) bool {
	return t.CheckTopicAccess(claims, topic, TopicActionPublish).Allowed
}

// CanConsume checks if claims allow consuming from the topic.
func (t *TopicIsolator) CanConsume(claims *Claims, topic string) bool {
	return t.CheckTopicAccess(claims, topic, TopicActionConsume).Allowed
}

// BuildTopicPrefix returns the topic prefix for a tenant.
// Useful for topic discovery and regex patterns.
//
// Example:
//
//	BuildTopicPrefix("acme") → "prod.acme."
func (t *TopicIsolator) BuildTopicPrefix(tenant string) string {
	return t.config.Environment + t.config.Separator + tenant + t.config.Separator
}

// ListAllowedTopicPatterns returns regex patterns for topics a tenant can access.
// Useful for Kafka consumer group patterns.
//
// Example:
//
//	ListAllowedTopicPatterns("acme") → ["prod\\.acme\\..*"]
func (t *TopicIsolator) ListAllowedTopicPatterns(tenant string) []string {
	// Escape separator for regex
	escapedSep := strings.ReplaceAll(t.config.Separator, ".", "\\.")

	patterns := make([]string, 0, 1+len(t.config.SharedTopicPatterns))
	// Tenant's own topics
	patterns = append(patterns, t.config.Environment+escapedSep+tenant+escapedSep+".*")

	// Add shared topic patterns (convert to regex)
	for _, pattern := range t.config.SharedTopicPatterns {
		regexPattern := strings.ReplaceAll(pattern, ".", "\\.")
		regexPattern = strings.ReplaceAll(regexPattern, "*", "[^.]+")
		patterns = append(patterns, regexPattern)
	}

	return patterns
}
