// Package platform provides configuration loading for all components.
// Config loading happens here (at entry point), not in library packages.
package platform

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/license"
)

// GatewayConfig holds gateway configuration loaded from environment variables.
type GatewayConfig struct {
	BaseConfig
	AuthConfig
	ProvisioningClientConfig

	// Server settings
	Port         int           `env:"GATEWAY_PORT" envDefault:"3000"`
	ReadTimeout  time.Duration `env:"GATEWAY_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout time.Duration `env:"GATEWAY_WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout  time.Duration `env:"GATEWAY_IDLE_TIMEOUT" envDefault:"60s"`

	// Backend ws-server connection
	BackendURL     string        `env:"GATEWAY_BACKEND_URL" envDefault:"ws://localhost:3005/ws"`
	DialTimeout    time.Duration `env:"GATEWAY_DIAL_TIMEOUT" envDefault:"10s"`
	MessageTimeout time.Duration `env:"GATEWAY_MESSAGE_TIMEOUT" envDefault:"60s"`

	// MaxFrameSize is the maximum allowed WebSocket frame size in bytes.
	// Frames exceeding this are rejected before payload allocation to prevent OOM.
	MaxFrameSize int `env:"GATEWAY_MAX_FRAME_SIZE" envDefault:"1048576"` // 1MB = protocol.DefaultMaxFrameSize

	// DefaultTenantID disables multi-tenant support. All connections
	// are routed to this tenant. Only used when AUTH_ENABLED=false.
	DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"sukko"`

	// Per-tenant channel rules (Feature Flag)
	// When enabled, channel permissions come from per-tenant rules via gRPC streaming
	PerTenantChannelRulesEnabled bool `env:"GATEWAY_PER_TENANT_CHANNEL_RULES" envDefault:"false"`

	// Fallback channel rules (when tenant has none configured)
	FallbackPublicChannels []string `env:"GATEWAY_FALLBACK_PUBLIC_CHANNELS" envSeparator:"," envDefault:"*.metadata"`

	// Multi-tenant settings
	RequireTenantID              bool `env:"REQUIRE_TENANT_ID" envDefault:"true"`
	DefaultTenantConnectionLimit int  `env:"DEFAULT_TENANT_CONNECTION_LIMIT" envDefault:"1000"`
	TenantConnectionLimitEnabled bool `env:"TENANT_CONNECTION_LIMIT_ENABLED" envDefault:"false"`

	// Permissions - channel patterns
	// Patterns support wildcards: *.trade matches BTC.trade, *.trade.* matches BTC.trade.user123
	// Aggregate channels (all.trade, all.liquidity, etc.) allow subscribing to ALL events of a type
	PublicPatterns      []string `env:"GATEWAY_PUBLIC_PATTERNS" envSeparator:"," envDefault:"*.trade,*.liquidity,*.metadata,*.analytics,*.creation,*.social,all.trade,all.liquidity,all.metadata,all.analytics,all.creation,all.social"`
	UserScopedPatterns  []string `env:"GATEWAY_USER_SCOPED_PATTERNS" envSeparator:"," envDefault:"*.balances.{principal},*.trade.{principal},balances.{principal},notifications.{principal}"`
	GroupScopedPatterns []string `env:"GATEWAY_GROUP_SCOPED_PATTERNS" envSeparator:"," envDefault:"*.community.{group_id},community.{group_id},social.{group_id}"`

	// Rate limiting per principal
	RateLimitEnabled bool    `env:"GATEWAY_RATE_LIMIT_ENABLED" envDefault:"true"`
	RateLimitBurst   int     `env:"GATEWAY_RATE_LIMIT_BURST" envDefault:"100"`
	RateLimitRate    float64 `env:"GATEWAY_RATE_LIMIT_RATE" envDefault:"10.0"`

	// Publish-specific settings
	PublishRateLimit float64 `env:"GATEWAY_PUBLISH_RATE_LIMIT" envDefault:"10.0"` // Messages per second
	PublishBurst     int     `env:"GATEWAY_PUBLISH_BURST" envDefault:"100"`       // Burst capacity
	MaxPublishSize   int     `env:"GATEWAY_MAX_PUBLISH_SIZE" envDefault:"65536"`  // Max message size (64KB = protocol.DefaultMaxPublishSize)

	// Auth refresh settings
	AuthRefreshRateInterval time.Duration `env:"GATEWAY_AUTH_REFRESH_RATE_INTERVAL" envDefault:"30s"`

	// Auth validation timeout for intercepting auth refresh operations
	AuthValidationTimeout time.Duration `env:"GATEWAY_AUTH_VALIDATION_TIMEOUT" envDefault:"5s"`

	// Graceful shutdown timeout
	ShutdownTimeout time.Duration `env:"GATEWAY_SHUTDOWN_TIMEOUT" envDefault:"30s"`

	// Channel rules provider cache TTLs
	ChannelRulesCacheTTL time.Duration `env:"GATEWAY_CHANNEL_RULES_CACHE_TTL" envDefault:"1m"`
	RegistryQueryTimeout time.Duration `env:"GATEWAY_REGISTRY_QUERY_TIMEOUT" envDefault:"5s"`

	// editionManager holds the license-resolved edition and limits.
	// Set by LoadGatewayConfig() before Validate(). Not an env var — derived from SUKKO_LICENSE_KEY.
	editionManager *license.Manager
}

// EditionManager returns the license manager for this config.
func (c *GatewayConfig) EditionManager() *license.Manager {
	return c.editionManager
}

// LoadGatewayConfig reads gateway configuration from environment variables.
// Optionally loads from .env file if present.
func LoadGatewayConfig(logger zerolog.Logger) (*GatewayConfig, error) {
	// Load .env file (optional)
	if err := godotenv.Load(); err != nil {
		logger.Debug().Msg("No .env file found (using environment variables only)")
	}

	cfg := &GatewayConfig{}

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse gateway config: %w", err)
	}

	// Create license manager before validation — Validate() uses edition gates.
	mgr, err := license.NewManager(cfg.LicenseKey, logger)
	if err != nil {
		return nil, fmt.Errorf("license: %w", err)
	}
	cfg.editionManager = mgr

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("gateway config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks gateway configuration for errors.
func (c *GatewayConfig) Validate() error {
	if c.Port < 1 || c.Port > MaxPort {
		return fmt.Errorf("GATEWAY_PORT must be between 1 and %d, got %d", MaxPort, c.Port)
	}

	// MaxFrameSize validation (1KB-10MB, default already applied above)
	if c.MaxFrameSize < MinFrameSize || c.MaxFrameSize > MaxFrameSizeLimit {
		return fmt.Errorf("GATEWAY_MAX_FRAME_SIZE must be between %d and %d, got %d", MinFrameSize, MaxFrameSizeLimit, c.MaxFrameSize)
	}

	// HTTP timeout validation
	if c.ReadTimeout < MinTimeout || c.ReadTimeout > MaxReadWriteTimeout {
		return fmt.Errorf("GATEWAY_READ_TIMEOUT must be %v-%v, got %v", MinTimeout, MaxReadWriteTimeout, c.ReadTimeout)
	}
	if c.WriteTimeout < MinTimeout || c.WriteTimeout > MaxReadWriteTimeout {
		return fmt.Errorf("GATEWAY_WRITE_TIMEOUT must be %v-%v, got %v", MinTimeout, MaxReadWriteTimeout, c.WriteTimeout)
	}
	if c.IdleTimeout < MinTimeout || c.IdleTimeout > MaxIdleTimeout {
		return fmt.Errorf("GATEWAY_IDLE_TIMEOUT must be %v-%v, got %v", MinTimeout, MaxIdleTimeout, c.IdleTimeout)
	}

	// Backend connection validation
	if c.DialTimeout < MinTimeout || c.DialTimeout > MaxDialTimeout {
		return fmt.Errorf("GATEWAY_DIAL_TIMEOUT must be %v-%v, got %v", MinTimeout, MaxDialTimeout, c.DialTimeout)
	}
	if c.MessageTimeout < MinTimeout || c.MessageTimeout > MaxMessageTimeout {
		return fmt.Errorf("GATEWAY_MESSAGE_TIMEOUT must be %v-%v, got %v", MinTimeout, MaxMessageTimeout, c.MessageTimeout)
	}

	// Auth-dependent validation
	if c.AuthEnabled {
		if err := c.ProvisioningClientConfig.Validate(); err != nil {
			return err
		}
	} else {
		if c.DefaultTenantID == "" {
			return errors.New("DEFAULT_TENANT_ID is required when AUTH_ENABLED=false")
		}
	}

	if c.AuthRefreshRateInterval < time.Second {
		return fmt.Errorf("GATEWAY_AUTH_REFRESH_RATE_INTERVAL must be >= 1s, got %v", c.AuthRefreshRateInterval)
	}

	if c.BackendURL == "" {
		return errors.New("GATEWAY_BACKEND_URL is required")
	}

	if len(c.PublicPatterns) == 0 {
		return errors.New("GATEWAY_PUBLIC_PATTERNS must have at least one pattern")
	}

	if err := c.BaseConfig.Validate(); err != nil {
		return err
	}

	// Rate limit validation (when enabled)
	if c.RateLimitEnabled {
		if c.RateLimitBurst < 1 {
			return fmt.Errorf("GATEWAY_RATE_LIMIT_BURST must be >= 1, got %d", c.RateLimitBurst)
		}
		if c.RateLimitRate <= 0 {
			return fmt.Errorf("GATEWAY_RATE_LIMIT_RATE must be > 0, got %f", c.RateLimitRate)
		}
	}

	// Publish settings validation
	if c.PublishBurst < 1 {
		return fmt.Errorf("GATEWAY_PUBLISH_BURST must be >= 1, got %d", c.PublishBurst)
	}
	if c.PublishRateLimit <= 0 {
		return fmt.Errorf("GATEWAY_PUBLISH_RATE_LIMIT must be > 0, got %f", c.PublishRateLimit)
	}
	if c.MaxPublishSize < MinFrameSize || c.MaxPublishSize > MaxFrameSizeLimit {
		return fmt.Errorf("GATEWAY_MAX_PUBLISH_SIZE must be %d-%d, got %d", MinFrameSize, MaxFrameSizeLimit, c.MaxPublishSize)
	}

	// Tenant connection limit (when enabled)
	if c.TenantConnectionLimitEnabled && c.DefaultTenantConnectionLimit < 1 {
		return fmt.Errorf("DEFAULT_TENANT_CONNECTION_LIMIT must be >= 1, got %d", c.DefaultTenantConnectionLimit)
	}

	// New externalized duration fields (FR-026a: reject non-positive)
	if c.AuthValidationTimeout <= 0 {
		return fmt.Errorf("GATEWAY_AUTH_VALIDATION_TIMEOUT must be > 0, got %v", c.AuthValidationTimeout)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("GATEWAY_SHUTDOWN_TIMEOUT must be > 0, got %v", c.ShutdownTimeout)
	}
	if c.ChannelRulesCacheTTL <= 0 {
		return fmt.Errorf("GATEWAY_CHANNEL_RULES_CACHE_TTL must be > 0, got %v", c.ChannelRulesCacheTTL)
	}
	if c.RegistryQueryTimeout <= 0 {
		return fmt.Errorf("GATEWAY_REGISTRY_QUERY_TIMEOUT must be > 0, got %v", c.RegistryQueryTimeout)
	}

	// Edition gates — uses startup-resolved Edition(), not expiry-aware CurrentEdition().
	if c.editionManager != nil {
		edition := c.editionManager.Edition()

		if c.PerTenantChannelRulesEnabled && !license.EditionHasFeature(edition, license.PerTenantChannelRules) {
			return license.NewFeatureError(license.PerTenantChannelRules, edition)
		}
		if c.TenantConnectionLimitEnabled && !license.EditionHasFeature(edition, license.PerTenantConnectionLimits) {
			return license.NewFeatureError(license.PerTenantConnectionLimits, edition)
		}
	}

	return nil
}

// LogConfig logs gateway configuration using structured logging.
func (c *GatewayConfig) LogConfig(logger zerolog.Logger) {
	edition := "community"
	if c.editionManager != nil {
		edition = c.editionManager.Edition().String()
	}

	event := logger.Info().
		Str("edition", edition).
		Str("environment", c.Environment).
		Int("port", c.Port).
		Bool("auth_enabled", c.AuthEnabled).
		Dur("read_timeout", c.ReadTimeout).
		Dur("write_timeout", c.WriteTimeout).
		Dur("idle_timeout", c.IdleTimeout).
		Str("backend_url", c.BackendURL).
		Dur("dial_timeout", c.DialTimeout).
		Strs("public_patterns", c.PublicPatterns).
		Strs("user_scoped_patterns", c.UserScopedPatterns).
		Strs("group_scoped_patterns", c.GroupScopedPatterns).
		Bool("rate_limit_enabled", c.RateLimitEnabled).
		Int("rate_limit_burst", c.RateLimitBurst).
		Float64("rate_limit_rate", c.RateLimitRate).
		Int("max_frame_size", c.MaxFrameSize).
		Dur("auth_refresh_rate_interval", c.AuthRefreshRateInterval).
		Str("log_level", c.LogLevel).
		Str("log_format", c.LogFormat)

	// Add routing fields when auth is disabled
	if !c.AuthEnabled {
		event = event.Str("default_tenant_id", c.DefaultTenantID)
	}

	// Add auth-specific fields when enabled
	if c.AuthEnabled {
		event = event.
			Bool("require_tenant_id", c.RequireTenantID).
			Str("provisioning_grpc_addr", c.ProvisioningGRPCAddr).
			Dur("grpc_reconnect_delay", c.GRPCReconnectDelay).
			Dur("grpc_reconnect_max_delay", c.GRPCReconnectMaxDelay)
	}

	// Add per-tenant channel rules fields when enabled
	if c.PerTenantChannelRulesEnabled {
		event = event.
			Bool("per_tenant_channel_rules_enabled", true).
			Strs("fallback_public_channels", c.FallbackPublicChannels)
	}

	event.Msg("Gateway configuration loaded")
}

// PerTenantChannelRulesReady returns true if per-tenant channel rules are enabled and provisioning gRPC is configured.
func (c *GatewayConfig) PerTenantChannelRulesReady() bool {
	return c.PerTenantChannelRulesEnabled && c.ProvisioningGRPCAddr != ""
}
