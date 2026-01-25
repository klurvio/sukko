package provisioning

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Time is a wrapper around time.Time for nullable timestamps.
type Time = time.Time

// TenantStatus represents the lifecycle state of a tenant.
type TenantStatus string

const (
	// StatusActive indicates the tenant is fully operational.
	StatusActive TenantStatus = "active"

	// StatusSuspended indicates the tenant is temporarily disabled.
	StatusSuspended TenantStatus = "suspended"

	// StatusDeprovisioning indicates the tenant is in the grace period before deletion.
	StatusDeprovisioning TenantStatus = "deprovisioning"

	// StatusDeleted indicates the tenant has been permanently removed.
	StatusDeleted TenantStatus = "deleted"
)

// IsValid checks if the status is a valid value.
func (s TenantStatus) IsValid() bool {
	switch s {
	case StatusActive, StatusSuspended, StatusDeprovisioning, StatusDeleted:
		return true
	}
	return false
}

// ConsumerType defines how a tenant consumes from Kafka.
type ConsumerType string

const (
	// ConsumerShared indicates the tenant uses the shared consumer group.
	ConsumerShared ConsumerType = "shared"

	// ConsumerDedicated indicates the tenant has a dedicated consumer group.
	ConsumerDedicated ConsumerType = "dedicated"
)

// IsValid checks if the consumer type is valid.
func (c ConsumerType) IsValid() bool {
	return c == ConsumerShared || c == ConsumerDedicated
}

// Metadata is a JSON object for flexible tenant metadata.
type Metadata map[string]interface{}

// Value implements driver.Valuer for database storage.
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner for database retrieval.
func (m *Metadata) Scan(value interface{}) error {
	if value == nil {
		*m = make(Metadata)
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("expected []byte, got %T", value)
	}
	return json.Unmarshal(b, m)
}

// Tenant represents a tenant in the system.
type Tenant struct {
	// ID is the unique identifier (e.g., "acme").
	// Must match pattern: ^[a-z][a-z0-9-]{2,62}$
	ID string `json:"id"`

	// Name is the display name (e.g., "Acme Corporation").
	Name string `json:"name"`

	// Status is the lifecycle state.
	Status TenantStatus `json:"status"`

	// ConsumerType determines consumer group assignment.
	ConsumerType ConsumerType `json:"consumer_type"`

	// Metadata holds flexible key-value data.
	Metadata Metadata `json:"metadata,omitempty"`

	// CreatedAt is when the tenant was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the tenant was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// SuspendedAt is when the tenant was suspended (if applicable).
	SuspendedAt *time.Time `json:"suspended_at,omitempty"`

	// DeprovisionAt is when the grace period ends and deletion will occur.
	DeprovisionAt *time.Time `json:"deprovision_at,omitempty"`

	// DeletedAt is when the tenant was permanently deleted.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// tenantIDPattern validates tenant IDs.
var tenantIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)

// ValidateTenantID checks if a tenant ID is valid.
func ValidateTenantID(id string) error {
	if !tenantIDPattern.MatchString(id) {
		return fmt.Errorf("invalid tenant ID: must be 3-63 lowercase alphanumeric characters, "+
			"starting with a letter, may contain hyphens (got: %q)", id)
	}
	return nil
}

// Validate checks if the tenant is valid for creation.
func (t *Tenant) Validate() error {
	if err := ValidateTenantID(t.ID); err != nil {
		return err
	}
	if t.Name == "" {
		return fmt.Errorf("tenant name is required")
	}
	if len(t.Name) > 256 {
		return fmt.Errorf("tenant name must be <= 256 characters")
	}
	if !t.ConsumerType.IsValid() {
		return fmt.Errorf("invalid consumer type: %q", t.ConsumerType)
	}
	return nil
}

// IsActive returns true if the tenant is in active status.
func (t *Tenant) IsActive() bool {
	return t.Status == StatusActive
}

// Algorithm represents a JWT signing algorithm.
type Algorithm string

const (
	AlgorithmES256 Algorithm = "ES256"
	AlgorithmRS256 Algorithm = "RS256"
	AlgorithmEdDSA Algorithm = "EdDSA"
)

// IsValid checks if the algorithm is supported.
func (a Algorithm) IsValid() bool {
	switch a {
	case AlgorithmES256, AlgorithmRS256, AlgorithmEdDSA:
		return true
	}
	return false
}

// TenantKey represents a public key for JWT validation.
type TenantKey struct {
	// KeyID is the unique identifier (kid in JWT header).
	// Must match pattern: ^[a-z][a-z0-9-]{2,62}$
	KeyID string `json:"key_id"`

	// TenantID is the owning tenant.
	TenantID string `json:"tenant_id"`

	// Algorithm is the signing algorithm (ES256, RS256, EdDSA).
	Algorithm Algorithm `json:"algorithm"`

	// PublicKey is the PEM-encoded public key.
	PublicKey string `json:"public_key"`

	// IsActive indicates if the key is currently valid.
	IsActive bool `json:"is_active"`

	// CreatedAt is when the key was registered.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt is when the key expires (optional).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// RevokedAt is when the key was revoked (optional).
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// keyIDPattern validates key IDs.
var keyIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)

// ValidateKeyID checks if a key ID is valid.
func ValidateKeyID(id string) error {
	if !keyIDPattern.MatchString(id) {
		return fmt.Errorf("invalid key ID: must be 3-63 lowercase alphanumeric characters, "+
			"starting with a letter, may contain hyphens (got: %q)", id)
	}
	return nil
}

// Validate checks if the key is valid for creation.
func (k *TenantKey) Validate() error {
	if err := ValidateKeyID(k.KeyID); err != nil {
		return err
	}
	if err := ValidateTenantID(k.TenantID); err != nil {
		return fmt.Errorf("invalid tenant ID: %w", err)
	}
	if !k.Algorithm.IsValid() {
		return fmt.Errorf("invalid algorithm: %q (must be ES256, RS256, or EdDSA)", k.Algorithm)
	}
	if k.PublicKey == "" {
		return fmt.Errorf("public key is required")
	}
	// Basic PEM format check
	if len(k.PublicKey) < 50 {
		return fmt.Errorf("public key appears too short to be valid PEM")
	}
	return nil
}

// IsExpired returns true if the key has expired.
func (k *TenantKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return k.ExpiresAt.Before(time.Now())
}

// TenantTopic represents a provisioned Kafka topic.
type TenantTopic struct {
	// ID is the database ID.
	ID int64 `json:"id,omitempty"`

	// TenantID is the owning tenant.
	TenantID string `json:"tenant_id"`

	// TopicName is the full Kafka topic name (e.g., "main.acme.trade").
	TopicName string `json:"topic_name"`

	// Category is the topic category (e.g., "trade").
	Category string `json:"category"`

	// Partitions is the number of partitions.
	Partitions int `json:"partitions"`

	// RetentionMs is the retention period in milliseconds.
	RetentionMs int64 `json:"retention_ms"`

	// CreatedAt is when the topic was provisioned.
	CreatedAt time.Time `json:"created_at"`

	// DeletedAt is when the topic was deleted (soft delete).
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// TenantQuota represents resource quotas for a tenant.
type TenantQuota struct {
	// TenantID is the tenant these quotas apply to.
	TenantID string `json:"tenant_id"`

	// MaxTopics is the maximum number of topics.
	MaxTopics int `json:"max_topics"`

	// MaxPartitions is the maximum total partitions.
	MaxPartitions int `json:"max_partitions"`

	// MaxStorageBytes is the maximum storage in bytes.
	MaxStorageBytes int64 `json:"max_storage_bytes"`

	// ProducerByteRate is the maximum producer throughput (bytes/sec).
	ProducerByteRate int64 `json:"producer_byte_rate"`

	// ConsumerByteRate is the maximum consumer throughput (bytes/sec).
	ConsumerByteRate int64 `json:"consumer_byte_rate"`

	// UpdatedAt is when quotas were last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// AuditEntry represents an audit log entry.
type AuditEntry struct {
	// ID is the database ID.
	ID int64 `json:"id,omitempty"`

	// TenantID is the affected tenant (may be empty for system actions).
	TenantID string `json:"tenant_id,omitempty"`

	// Action is what was done (e.g., "create_tenant", "revoke_key").
	Action string `json:"action"`

	// Actor is who performed the action (principal/user ID).
	Actor string `json:"actor"`

	// ActorType is the type of actor (user, system, api_key).
	ActorType string `json:"actor_type"`

	// IPAddress is the client IP address (if available).
	IPAddress string `json:"ip_address,omitempty"`

	// Details contains action-specific data.
	Details Metadata `json:"details,omitempty"`

	// CreatedAt is when the action occurred.
	CreatedAt time.Time `json:"created_at"`
}

// Audit action constants.
const (
	ActionCreateTenant      = "create_tenant"
	ActionUpdateTenant      = "update_tenant"
	ActionSuspendTenant     = "suspend_tenant"
	ActionReactivateTenant  = "reactivate_tenant"
	ActionDeprovisionTenant = "deprovision_tenant"
	ActionDeleteTenant      = "delete_tenant"
	ActionCreateKey         = "create_key"
	ActionRevokeKey         = "revoke_key"
	ActionCreateTopic       = "create_topic"
	ActionDeleteTopic       = "delete_topic"
	ActionUpdateQuota       = "update_quota"
)

// Actor type constants.
const (
	ActorTypeUser   = "user"
	ActorTypeSystem = "system"
	ActorTypeAPIKey = "api_key"
)
