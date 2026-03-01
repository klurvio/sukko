// Package configstore implements in-memory store backends for YAML config file mode.
package configstore

// ConfigFile is the top-level YAML configuration file structure.
type ConfigFile struct {
	Tenants []TenantConfig `yaml:"tenants"`
}

// TenantConfig defines a tenant's complete configuration in YAML.
type TenantConfig struct {
	ID           string              `yaml:"id"`
	Name         string              `yaml:"name"`
	ConsumerType string              `yaml:"consumer_type"`
	Metadata     map[string]any      `yaml:"metadata,omitempty"`
	Categories   []CategoryConfig    `yaml:"categories"`
	Keys         []KeyConfig         `yaml:"keys,omitempty"`
	Quotas       *QuotaConfig        `yaml:"quotas,omitempty"`
	OIDC         *OIDCConfig         `yaml:"oidc,omitempty"`
	ChannelRules *ChannelRulesConfig `yaml:"channel_rules,omitempty"`
}

// CategoryConfig defines a topic category for a tenant.
type CategoryConfig struct {
	Name        string `yaml:"name"`
	Partitions  int    `yaml:"partitions,omitempty"`
	RetentionMs int64  `yaml:"retention_ms,omitempty"`
}

// KeyConfig defines a public key for JWT validation.
type KeyConfig struct {
	ID           string `yaml:"id"`
	Algorithm    string `yaml:"algorithm"`
	PublicKey    string `yaml:"public_key"`
	Active       *bool  `yaml:"active,omitempty"`
	ExpiresAtUnix *int64 `yaml:"expires_at_unix,omitempty"`
}

// QuotaConfig defines resource quotas for a tenant.
type QuotaConfig struct {
	MaxTopics        int   `yaml:"max_topics,omitempty"`
	MaxPartitions    int   `yaml:"max_partitions,omitempty"`
	MaxStorageBytes  int64 `yaml:"max_storage_bytes,omitempty"`
	ProducerByteRate int64 `yaml:"producer_byte_rate,omitempty"`
	ConsumerByteRate int64 `yaml:"consumer_byte_rate,omitempty"`
	MaxConnections   int   `yaml:"max_connections,omitempty"`
}

// OIDCConfig defines OIDC (OpenID Connect) configuration for a tenant.
type OIDCConfig struct {
	IssuerURL string `yaml:"issuer_url"`
	JWKSURL   string `yaml:"jwks_url,omitempty"`
	Audience  string `yaml:"audience,omitempty"`
	Enabled   *bool  `yaml:"enabled,omitempty"`
}

// ChannelRulesConfig defines channel access rules for a tenant.
type ChannelRulesConfig struct {
	PublicChannels  []string            `yaml:"public_channels,omitempty"`
	GroupMappings   map[string][]string `yaml:"group_mappings,omitempty"`
	DefaultChannels []string            `yaml:"default_channels,omitempty"`
}
