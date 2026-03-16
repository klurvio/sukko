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
)

// GatewayConfig holds gateway configuration loaded from environment variables.
type GatewayConfig struct {
	BaseConfig

	// Server settings
	Port         int           `env:"GATEWAY_PORT" envDefault:"3000"`
	ReadTimeout  time.Duration `env:"GATEWAY_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout time.Duration `env:"GATEWAY_WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout  time.Duration `env:"GATEWAY_IDLE_TIMEOUT" envDefault:"60s"`

	// Backend ws-server connection
	BackendURL     string        `env:"GATEWAY_BACKEND_URL" envDefault:"ws://localhost:3001/ws"`
	DialTimeout    time.Duration `env:"GATEWAY_DIAL_TIMEOUT" envDefault:"10s"`
	MessageTimeout time.Duration `env:"GATEWAY_MESSAGE_TIMEOUT" envDefault:"60s"`

	// MaxFrameSize is the maximum allowed WebSocket frame size in bytes.
	// Frames exceeding this are rejected before payload allocation to prevent OOM.
	MaxFrameSize int `env:"GATEWAY_MAX_FRAME_SIZE" envDefault:"1048576"` // 1MB = protocol.DefaultMaxFrameSize

	// Authentication (multi-tenant with asymmetric keys)
	// TODO: Change envDefault to "true" when sukko-api auth integration is production-ready
	AuthEnabled bool `env:"AUTH_ENABLED" envDefault:"false"`

	// DefaultTenantID disables multi-tenant support. All connections
	// are routed to this tenant. Only used when AUTH_ENABLED=false.
	DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"sukko"`

	// Provisioning service gRPC connection (provides keys, OIDC config, channel rules via streaming)
	ProvisioningGRPCAddr  string        `env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`
	GRPCReconnectDelay    time.Duration `env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`
	GRPCReconnectMaxDelay time.Duration `env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`

	// OIDC/JWKS support (external IdP tokens)
	// OIDC is enabled when both IssuerURL and JWKSURL are set
	OIDCIssuerURL string `env:"OIDC_ISSUER_URL"`
	OIDCAudience  string `env:"OIDC_AUDIENCE"`
	OIDCJWKSURL   string `env:"OIDC_JWKS_URL"`

	// Multi-issuer OIDC (Feature Flag)
	// When enabled, each tenant can register their own IdP via provisioning API
	MultiIssuerOIDCEnabled bool `env:"GATEWAY_MULTI_ISSUER_OIDC_ENABLED" envDefault:"false"`

	// Per-tenant channel rules (Feature Flag)
	// When enabled, channel permissions come from per-tenant rules via gRPC streaming
	PerTenantChannelRulesEnabled bool `env:"GATEWAY_PER_TENANT_CHANNEL_RULES" envDefault:"false"`

	// OIDC keyfunc cache settings (per-issuer JWKS key caching — not affected by gRPC migration)
	OIDCKeyfuncCacheTTL time.Duration `env:"GATEWAY_OIDC_KEYFUNC_CACHE_TTL" envDefault:"1h"`

	// JWKS fetch settings
	JWKSFetchTimeout    time.Duration `env:"GATEWAY_JWKS_FETCH_TIMEOUT" envDefault:"10s"`
	JWKSRefreshInterval time.Duration `env:"GATEWAY_JWKS_REFRESH_INTERVAL" envDefault:"1h"`

	// Fallback channel rules (when tenant has none configured)
	FallbackPublicChannels []string `env:"GATEWAY_FALLBACK_PUBLIC_CHANNELS" envSeparator:"," envDefault:"*.metadata"`

	// Multi-tenant settings
	RequireTenantID              bool `env:"REQUIRE_TENANT_ID" envDefault:"true"`
	DefaultTenantConnectionLimit int  `env:"DEFAULT_TENANT_CONNECTION_LIMIT" envDefault:"1000"`
	TenantConnectionLimitEnabled bool `env:"TENANT_CONNECTION_LIMIT_ENABLED" envDefault:"true"`

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

	// Tenant registry cache TTLs (postgres-backed registry)
	IssuerCacheTTL       time.Duration `env:"GATEWAY_ISSUER_CACHE_TTL" envDefault:"5m"`
	ChannelRulesCacheTTL time.Duration `env:"GATEWAY_CHANNEL_RULES_CACHE_TTL" envDefault:"1m"`
	RegistryQueryTimeout time.Duration `env:"GATEWAY_REGISTRY_QUERY_TIMEOUT" envDefault:"5s"`
}

// LoadGatewayConfig reads gateway configuration from environment variables.
// Optionally loads from .env file if present.
func LoadGatewayConfig(logger *zerolog.Logger) (*GatewayConfig, error) {
	// Load .env file (optional)
	if err := godotenv.Load(); err != nil {
		if logger != nil {
			logger.Debug().Msg("No .env file found (using environment variables only)")
		}
	}

	cfg := &GatewayConfig{}

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse gateway config: %w", err)
	}

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

	// Require provisioning gRPC address when auth is enabled
	if c.AuthEnabled {
		if c.ProvisioningGRPCAddr == "" {
			return errors.New("PROVISIONING_GRPC_ADDR is required when AUTH_ENABLED=true")
		}
	} else {
		if c.DefaultTenantID == "" {
			return errors.New("DEFAULT_TENANT_ID is required when AUTH_ENABLED=false")
		}
	}

	// Validate gRPC reconnection settings
	if c.GRPCReconnectDelay < 100*time.Millisecond {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_DELAY must be >= 100ms, got %v", c.GRPCReconnectDelay)
	}
	if c.GRPCReconnectMaxDelay < c.GRPCReconnectDelay {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_MAX_DELAY (%v) must be >= PROVISIONING_GRPC_RECONNECT_DELAY (%v)",
			c.GRPCReconnectMaxDelay, c.GRPCReconnectDelay)
	}

	// Validate OIDC settings - if one URL is set, both must be set
	hasIssuer := c.OIDCIssuerURL != ""
	hasJWKS := c.OIDCJWKSURL != ""
	if hasIssuer != hasJWKS {
		if !hasIssuer {
			return errors.New("OIDC_ISSUER_URL is required when OIDC_JWKS_URL is set")
		}
		return errors.New("OIDC_JWKS_URL is required when OIDC_ISSUER_URL is set")
	}

	// Validate multi-issuer OIDC settings
	if c.MultiIssuerOIDCEnabled {
		if c.JWKSFetchTimeout < time.Second {
			return fmt.Errorf("GATEWAY_JWKS_FETCH_TIMEOUT must be >= 1s, got %v", c.JWKSFetchTimeout)
		}
		if c.OIDCKeyfuncCacheTTL < time.Second {
			return fmt.Errorf("GATEWAY_OIDC_KEYFUNC_CACHE_TTL must be >= 1s, got %v", c.OIDCKeyfuncCacheTTL)
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

	// JWKS refresh interval (when multi-issuer enabled)
	if c.MultiIssuerOIDCEnabled && c.JWKSRefreshInterval < time.Minute {
		return fmt.Errorf("GATEWAY_JWKS_REFRESH_INTERVAL must be >= 1m, got %v", c.JWKSRefreshInterval)
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
	if c.IssuerCacheTTL <= 0 {
		return fmt.Errorf("GATEWAY_ISSUER_CACHE_TTL must be > 0, got %v", c.IssuerCacheTTL)
	}
	if c.ChannelRulesCacheTTL <= 0 {
		return fmt.Errorf("GATEWAY_CHANNEL_RULES_CACHE_TTL must be > 0, got %v", c.ChannelRulesCacheTTL)
	}
	if c.RegistryQueryTimeout <= 0 {
		return fmt.Errorf("GATEWAY_REGISTRY_QUERY_TIMEOUT must be > 0, got %v", c.RegistryQueryTimeout)
	}

	return nil
}

// OIDCEnabled returns true if OIDC is configured (both IssuerURL and JWKSURL are set).
func (c *GatewayConfig) OIDCEnabled() bool {
	return c.OIDCIssuerURL != "" && c.OIDCJWKSURL != ""
}

// LogConfig logs gateway configuration using structured logging.
func (c *GatewayConfig) LogConfig(logger zerolog.Logger) {
	event := logger.Info().
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

	// Add OIDC-specific fields when enabled
	if c.OIDCEnabled() {
		event = event.
			Bool("oidc_enabled", true).
			Str("oidc_issuer_url", c.OIDCIssuerURL).
			Str("oidc_jwks_url", c.OIDCJWKSURL)
		if c.OIDCAudience != "" {
			event = event.Str("oidc_audience", c.OIDCAudience)
		}
	}

	// Add multi-issuer OIDC fields when enabled
	if c.MultiIssuerOIDCEnabled {
		event = event.
			Bool("multi_issuer_oidc_enabled", true).
			Dur("oidc_keyfunc_cache_ttl", c.OIDCKeyfuncCacheTTL).
			Dur("jwks_fetch_timeout", c.JWKSFetchTimeout).
			Dur("jwks_refresh_interval", c.JWKSRefreshInterval)
	}

	// Add per-tenant channel rules fields when enabled
	if c.PerTenantChannelRulesEnabled {
		event = event.
			Bool("per_tenant_channel_rules_enabled", true).
			Strs("fallback_public_channels", c.FallbackPublicChannels)
	}

	event.Msg("Gateway configuration loaded")
}

// MultiIssuerOIDCReady returns true if multi-issuer OIDC is enabled and provisioning gRPC is configured.
func (c *GatewayConfig) MultiIssuerOIDCReady() bool {
	return c.MultiIssuerOIDCEnabled && c.ProvisioningGRPCAddr != ""
}

// PerTenantChannelRulesReady returns true if per-tenant channel rules are enabled and provisioning gRPC is configured.
func (c *GatewayConfig) PerTenantChannelRulesReady() bool {
	return c.PerTenantChannelRulesEnabled && c.ProvisioningGRPCAddr != ""
}
