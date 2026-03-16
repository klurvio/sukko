// Package provisioning provides tenant lifecycle management, key registration,
// and Kafka topic/ACL provisioning for multi-tenant WebSocket infrastructure.
package provisioning

import (
	"context"

	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// TenantStore handles tenant persistence operations.
type TenantStore interface {
	// Ping verifies database connectivity.
	Ping(ctx context.Context) error

	// Create creates a new tenant record.
	Create(ctx context.Context, tenant *Tenant) error

	// Get retrieves a tenant by ID.
	Get(ctx context.Context, tenantID string) (*Tenant, error)

	// Update updates an existing tenant record.
	Update(ctx context.Context, tenant *Tenant) error

	// List returns tenants matching the given options.
	List(ctx context.Context, opts ListOptions) ([]*Tenant, int, error)

	// UpdateStatus updates a tenant's status.
	UpdateStatus(ctx context.Context, tenantID string, status TenantStatus) error

	// SetDeprovisionAt sets the deprovision deadline for a tenant.
	SetDeprovisionAt(ctx context.Context, tenantID string, deprovisionAt *Time) error

	// GetTenantsForDeletion returns tenants past their deprovision deadline.
	GetTenantsForDeletion(ctx context.Context) ([]*Tenant, error)
}

// KeyStore handles public key persistence operations.
type KeyStore interface {
	// Create creates a new key record.
	Create(ctx context.Context, key *TenantKey) error

	// Get retrieves a key by ID.
	Get(ctx context.Context, keyID string) (*TenantKey, error)

	// ListByTenant returns all keys for a tenant.
	ListByTenant(ctx context.Context, tenantID string) ([]*TenantKey, error)

	// Revoke revokes a key by setting its revoked_at timestamp.
	Revoke(ctx context.Context, keyID string) error

	// RevokeAllForTenant revokes all active keys for a tenant.
	RevokeAllForTenant(ctx context.Context, tenantID string) error

	// GetActiveKeys returns all active, non-expired, non-revoked keys.
	// Used by WS Gateway to refresh its key cache.
	GetActiveKeys(ctx context.Context) ([]*TenantKey, error)
}

// RoutingRulesStore handles per-tenant topic routing rules persistence.
// Rules are stored as a JSONB array of {pattern, topic_suffix} objects.
type RoutingRulesStore interface {
	// Get retrieves routing rules for a tenant.
	// Returns ErrRoutingRulesNotFound if not found.
	Get(ctx context.Context, tenantID string) ([]TopicRoutingRule, error)

	// Set creates or updates routing rules for a tenant (upsert).
	Set(ctx context.Context, tenantID string, rules []TopicRoutingRule) error

	// Delete deletes routing rules for a tenant.
	Delete(ctx context.Context, tenantID string) error

	// ListAll returns routing rules for all tenants.
	ListAll(ctx context.Context) (map[string][]TopicRoutingRule, error)
}

// QuotaStore handles tenant quota operations.
type QuotaStore interface {
	// Get retrieves quotas for a tenant.
	Get(ctx context.Context, tenantID string) (*TenantQuota, error)

	// Create creates quota record for a tenant.
	Create(ctx context.Context, quota *TenantQuota) error

	// Update updates quota record for a tenant.
	Update(ctx context.Context, quota *TenantQuota) error
}

// AuditStore handles audit log operations.
type AuditStore interface {
	// Log records an audit entry.
	Log(ctx context.Context, entry *AuditEntry) error

	// ListByTenant returns audit entries for a tenant.
	ListByTenant(ctx context.Context, tenantID string, opts ListOptions) ([]*AuditEntry, int, error)
}

// OIDCConfigStore handles tenant OIDC configuration persistence.
// Used for multi-issuer OIDC support where each tenant can register their IdP.
type OIDCConfigStore interface {
	// Create creates a new OIDC configuration for a tenant.
	Create(ctx context.Context, config *types.TenantOIDCConfig) error

	// Get retrieves OIDC configuration by tenant ID.
	// Returns types.ErrOIDCNotConfigured if not found.
	Get(ctx context.Context, tenantID string) (*types.TenantOIDCConfig, error)

	// GetByIssuer retrieves OIDC configuration by issuer URL.
	// This is the hot path for token validation - lookup tenant from issuer.
	// Returns types.ErrIssuerNotFound if not found.
	GetByIssuer(ctx context.Context, issuerURL string) (*types.TenantOIDCConfig, error)

	// Update updates an existing OIDC configuration.
	Update(ctx context.Context, config *types.TenantOIDCConfig) error

	// Delete deletes OIDC configuration for a tenant.
	Delete(ctx context.Context, tenantID string) error

	// ListEnabled returns all enabled OIDC configurations.
	// Used by gateway to build issuer→tenant cache.
	ListEnabled(ctx context.Context) ([]*types.TenantOIDCConfig, error)
}

// ChannelRulesStore handles per-tenant channel access rules persistence.
// Used for mapping IdP groups to allowed channel patterns.
type ChannelRulesStore interface {
	// Create creates channel rules for a tenant.
	Create(ctx context.Context, tenantID string, rules *types.ChannelRules) error

	// Get retrieves channel rules with metadata by tenant ID.
	// Returns types.ErrChannelRulesNotFound if not found.
	Get(ctx context.Context, tenantID string) (*types.TenantChannelRules, error)

	// GetRules retrieves just the channel rules (not metadata) by tenant ID.
	// Returns types.ErrChannelRulesNotFound if not found.
	GetRules(ctx context.Context, tenantID string) (*types.ChannelRules, error)

	// Update updates channel rules for a tenant (upsert).
	Update(ctx context.Context, tenantID string, rules *types.ChannelRules) error

	// Delete deletes channel rules for a tenant.
	Delete(ctx context.Context, tenantID string) error

	// List returns all channel rules (used by gateway to build cache).
	List(ctx context.Context) ([]*types.TenantChannelRules, error)
}

// KafkaAdmin handles Redpanda/Kafka topic and ACL management.
type KafkaAdmin interface {
	// CreateTopic creates a new Kafka topic.
	CreateTopic(ctx context.Context, name string, partitions int, config map[string]string) error

	// DeleteTopic deletes a Kafka topic.
	DeleteTopic(ctx context.Context, name string) error

	// TopicExists checks if a topic exists.
	TopicExists(ctx context.Context, name string) (bool, error)

	// SetTopicConfig updates topic configuration.
	SetTopicConfig(ctx context.Context, name string, config map[string]string) error

	// CreateACL creates an ACL for a tenant.
	CreateACL(ctx context.Context, acl ACLBinding) error

	// DeleteACL deletes an ACL.
	DeleteACL(ctx context.Context, acl ACLBinding) error

	// SetQuota sets resource quotas for a tenant principal.
	SetQuota(ctx context.Context, tenantID string, quota QuotaConfig) error
}

// QuotaEnforcer checks resource limits before provisioning.
type QuotaEnforcer interface {
	// CheckTopicQuota checks if creating additional topics would exceed quota.
	CheckTopicQuota(ctx context.Context, tenantID string, additionalTopics int) error

	// CheckPartitionQuota checks if creating additional partitions would exceed quota.
	CheckPartitionQuota(ctx context.Context, tenantID string, additionalPartitions int) error
}

// ListOptions defines pagination and filtering for list operations.
type ListOptions struct {
	// Limit is the maximum number of results to return.
	Limit int

	// Offset is the number of results to skip.
	Offset int

	// Status filters by tenant status (optional).
	Status *TenantStatus
}

// QuotaConfig defines Kafka quotas for a tenant.
type QuotaConfig struct {
	// ProducerByteRate is the maximum bytes/second for producers.
	ProducerByteRate int64

	// ConsumerByteRate is the maximum bytes/second for consumers.
	ConsumerByteRate int64
}

// ACLBinding defines an ACL rule.
type ACLBinding struct {
	// Principal is the Kafka principal (e.g., "User:acme").
	// MUST use "User:" prefix per Kafka protocol - use FormatPrincipal() helper.
	Principal string

	// ResourceType is the resource type (e.g., "TOPIC", "GROUP", "CLUSTER").
	ResourceType string

	// ResourceName is the resource name or pattern (e.g., "main.acme.*").
	ResourceName string

	// PatternType is the pattern type (e.g., "PREFIXED", "LITERAL").
	PatternType string

	// Operation is the allowed operation (e.g., "ALL", "READ", "WRITE").
	Operation string

	// Permission is ALLOW or DENY.
	Permission string
}

// ACL constants for resource types.
const (
	ACLResourceTopic           = "TOPIC"
	ACLResourceGroup           = "GROUP"
	ACLResourceCluster         = "CLUSTER"
	ACLResourceTransactionalID = "TRANSACTIONAL_ID"
)

// ACL constants for pattern types.
const (
	ACLPatternLiteral  = "LITERAL"
	ACLPatternPrefixed = "PREFIXED"
)

// ACL constants for operations.
const (
	ACLOpAll             = "ALL"
	ACLOpRead            = "READ"
	ACLOpWrite           = "WRITE"
	ACLOpCreate          = "CREATE"
	ACLOpDelete          = "DELETE"
	ACLOpAlter           = "ALTER"
	ACLOpDescribe        = "DESCRIBE"
	ACLOpClusterAction   = "CLUSTER_ACTION"
	ACLOpDescribeConfigs = "DESCRIBE_CONFIGS"
	ACLOpAlterConfigs    = "ALTER_CONFIGS"
	ACLOpIdempotentWrite = "IDEMPOTENT_WRITE"
)

// ACL constants for permissions.
const (
	ACLPermissionAllow = "ALLOW"
	ACLPermissionDeny  = "DENY"
)

// FormatPrincipal formats a tenant ID as a Kafka principal.
// Kafka ACL principals MUST use the format "User:{username}" for SASL/SCRAM auth.
// This is a Kafka protocol requirement, not optional.
func FormatPrincipal(tenantID string) string {
	return "User:" + tenantID
}

// ParsePrincipal extracts the tenant ID from a Kafka principal.
// Returns empty string if the principal is not in "User:{id}" format.
func ParsePrincipal(principal string) string {
	const prefix = "User:"
	if len(principal) > len(prefix) && principal[:len(prefix)] == prefix {
		return principal[len(prefix):]
	}
	return ""
}

// ValidatePrincipal checks if a principal is in valid Kafka format.
func ValidatePrincipal(principal string) bool {
	return len(principal) > 5 && principal[:5] == "User:"
}
