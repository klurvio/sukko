package provisioning

import "time"

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

	// Categories are the topic categories to create initially (optional).
	Categories []string `json:"categories,omitempty"`

	// PublicKey is the initial public key to register (optional).
	PublicKey *CreateKeyRequest `json:"public_key,omitempty"`
}

// CreateTenantResponse is the response from creating a tenant.
type CreateTenantResponse struct {
	// Tenant is the created tenant.
	Tenant *Tenant `json:"tenant"`

	// Key is the registered key (if provided).
	Key *TenantKey `json:"key,omitempty"`

	// Topics are the created topic names.
	Topics []string `json:"topics,omitempty"`

	// ACL describes the created ACL.
	ACL *ACLInfo `json:"acl,omitempty"`
}

// ACLInfo describes an ACL for API responses.
type ACLInfo struct {
	Principal  string   `json:"principal"`
	Pattern    string   `json:"pattern"`
	Operations []string `json:"operations"`
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

// CreateTopicsRequest is the request to create topics.
type CreateTopicsRequest struct {
	// Categories are the topic categories to create.
	Categories []string `json:"categories"`

	// Partitions overrides the default partition count (optional).
	Partitions *int `json:"partitions,omitempty"`

	// RetentionMs overrides the default retention (optional).
	RetentionMs *int64 `json:"retention_ms,omitempty"`
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
