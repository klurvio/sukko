package provisioning

import (
	"time"
)

// CreateTenantRequest is the request to create a new tenant.
type CreateTenantRequest struct {
	// TenantID is the unique identifier.
	TenantID string `json:"id"`

	// Name is the display name.
	Name string `json:"name"`

	// ConsumerType is the consumer group assignment (optional, defaults to shared).
	ConsumerType ConsumerType `json:"consumer_type,omitempty"`

	// Metadata is optional key-value data.
	Metadata Metadata `json:"metadata,omitempty"`

	// PublicKey is the initial public key to register (optional).
	PublicKey *CreateKeyRequest `json:"public_key,omitempty"`
}

// CreateTenantResponse is the response from creating a tenant.
type CreateTenantResponse struct {
	// Tenant is the created tenant.
	Tenant *Tenant `json:"tenant"`

	// Key is the registered key (if provided).
	Key *TenantKey `json:"key,omitempty"`
}

// UpdateTenantRequest is the request to update a tenant.
type UpdateTenantRequest struct {
	// Name is the new display name (optional).
	Name *string `json:"name,omitempty"`

	// ConsumerType is the new consumer type (optional).
	ConsumerType *ConsumerType `json:"consumer_type,omitempty"`

	// Metadata is the new metadata (replaces existing).
	Metadata Metadata `json:"metadata,omitempty"`
}

// CreateKeyRequest is the request to register a public key.
type CreateKeyRequest struct {
	// KeyID is the unique identifier (kid in JWT header).
	KeyID string `json:"key_id"`

	// Algorithm is the signing algorithm.
	Algorithm Algorithm `json:"algorithm"`

	// PublicKey is the PEM-encoded public key.
	PublicKey string `json:"public_key"`

	// ExpiresAt is the optional expiration time.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SetRoutingRulesRequest is the request to set routing rules for a tenant.
type SetRoutingRulesRequest struct {
	// Rules are the ordered topic routing rules.
	Rules []TopicRoutingRule `json:"rules"`
}

// UpdateQuotaRequest is the request to update quotas.
type UpdateQuotaRequest struct {
	// MaxTopics is the new max topics limit.
	MaxTopics *int `json:"max_topics,omitempty"`

	// MaxPartitions is the new max partitions limit.
	MaxPartitions *int `json:"max_partitions,omitempty"`

	// MaxStorageBytes is the new max storage limit.
	MaxStorageBytes *int64 `json:"max_storage_bytes,omitempty"`

	// ProducerByteRate is the new producer rate limit.
	ProducerByteRate *int64 `json:"producer_byte_rate,omitempty"`

	// ConsumerByteRate is the new consumer rate limit.
	ConsumerByteRate *int64 `json:"consumer_byte_rate,omitempty"`
}

// CreateOIDCConfigRequest is the request to register OIDC configuration.
type CreateOIDCConfigRequest struct {
	// IssuerURL is the OIDC issuer URL (required).
	IssuerURL string `json:"issuer_url"`

	// JWKSURL is the custom JWKS URL (optional, defaults to {issuer}/.well-known/jwks.json).
	JWKSURL string `json:"jwks_url,omitempty"`

	// Audience is the expected audience claim (optional).
	Audience string `json:"audience,omitempty"`
}

// UpdateOIDCConfigRequest is the request to update OIDC configuration.
type UpdateOIDCConfigRequest struct {
	// IssuerURL is the new OIDC issuer URL (optional).
	IssuerURL *string `json:"issuer_url,omitempty"`

	// JWKSURL is the new custom JWKS URL (optional).
	JWKSURL *string `json:"jwks_url,omitempty"`

	// Audience is the new audience claim (optional).
	Audience *string `json:"audience,omitempty"`

	// Enabled controls whether the OIDC config is active (optional).
	Enabled *bool `json:"enabled,omitempty"`
}

// SetChannelRulesRequest is the request to set channel rules for a tenant.
type SetChannelRulesRequest struct {
	// Public contains channel patterns accessible to all authenticated users.
	Public []string `json:"public,omitempty"`

	// GroupMappings maps IdP group names to allowed channel patterns.
	GroupMappings map[string][]string `json:"group_mappings,omitempty"`

	// Default contains channel patterns allowed when no groups match.
	Default []string `json:"default,omitempty"`
}

// TestAccessRequest is the request to test channel access for given groups.
type TestAccessRequest struct {
	// Groups are the IdP groups to test access for.
	Groups []string `json:"groups"`
}

// TestAccessResponse is the response from testing channel access.
type TestAccessResponse struct {
	// AllowedPatterns are the channel patterns the groups have access to.
	AllowedPatterns []string `json:"allowed_patterns"`
}
