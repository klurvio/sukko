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
	// Server settings
	Port         int           `env:"GATEWAY_PORT" envDefault:"3000"`
	ReadTimeout  time.Duration `env:"GATEWAY_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout time.Duration `env:"GATEWAY_WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout  time.Duration `env:"GATEWAY_IDLE_TIMEOUT" envDefault:"60s"`

	// Backend ws-server connection
	BackendURL     string        `env:"GATEWAY_BACKEND_URL" envDefault:"ws://localhost:3001/ws"`
	DialTimeout    time.Duration `env:"GATEWAY_DIAL_TIMEOUT" envDefault:"10s"`
	MessageTimeout time.Duration `env:"GATEWAY_MESSAGE_TIMEOUT" envDefault:"60s"`

	// Authentication (multi-tenant with asymmetric keys)
	AuthEnabled bool `env:"AUTH_ENABLED" envDefault:"true"`

	// DefaultTenantID disables multi-tenant support. All connections
	// are routed to this tenant. Only used when AUTH_ENABLED=false.
	DefaultTenantID string `env:"DEFAULT_TENANT_ID" envDefault:"odin"`

	// Provisioning database connection (required when auth is enabled)
	ProvisioningDBURL string `env:"PROVISIONING_DATABASE_URL"`

	// OIDC/JWKS support (external IdP tokens)
	// OIDC is enabled when both IssuerURL and JWKSURL are set
	OIDCIssuerURL string `env:"OIDC_ISSUER_URL"`
	OIDCAudience  string `env:"OIDC_AUDIENCE"`
	OIDCJWKSURL   string `env:"OIDC_JWKS_URL"`

	// Multi-issuer OIDC (Feature Flag)
	// When enabled, each tenant can register their own IdP via provisioning API
	MultiIssuerOIDCEnabled bool `env:"GATEWAY_MULTI_ISSUER_OIDC_ENABLED" envDefault:"false"`

	// Per-tenant channel rules (Feature Flag)
	// When enabled, channel permissions come from per-tenant rules in the database
	PerTenantChannelRulesEnabled bool `env:"GATEWAY_PER_TENANT_CHANNEL_RULES" envDefault:"false"`

	// TenantRegistry cache settings (used when multi-issuer OIDC is enabled)
	IssuerCacheTTL       time.Duration `env:"GATEWAY_ISSUER_CACHE_TTL" envDefault:"5m"`
	ChannelRulesCacheTTL time.Duration `env:"GATEWAY_CHANNEL_RULES_CACHE_TTL" envDefault:"1m"`
	OIDCKeyfuncCacheTTL  time.Duration `env:"GATEWAY_OIDC_KEYFUNC_CACHE_TTL" envDefault:"1h"`

	// JWKS fetch settings
	JWKSFetchTimeout    time.Duration `env:"GATEWAY_JWKS_FETCH_TIMEOUT" envDefault:"10s"`
	JWKSRefreshInterval time.Duration `env:"GATEWAY_JWKS_REFRESH_INTERVAL" envDefault:"1h"`

	// Fallback channel rules (when tenant has none configured)
	FallbackPublicChannels []string `env:"GATEWAY_FALLBACK_PUBLIC_CHANNELS" envSeparator:"," envDefault:"*.metadata"`

	// Key cache settings
	KeyCacheRefreshInterval time.Duration `env:"KEY_CACHE_REFRESH_INTERVAL" envDefault:"1m"`
	KeyCacheQueryTimeout    time.Duration `env:"KEY_CACHE_QUERY_TIMEOUT" envDefault:"5s"`

	// Multi-tenant settings
	RequireTenantID              bool `env:"REQUIRE_TENANT_ID" envDefault:"true"`
	DefaultTenantConnectionLimit int  `env:"DEFAULT_TENANT_CONNECTION_LIMIT" envDefault:"1000"`
	TenantConnectionLimitEnabled bool `env:"TENANT_CONNECTION_LIMIT_ENABLED" envDefault:"true"`

	// Database connection pool settings
	DBMaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS" envDefault:"10"`
	DBMaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5"`
	DBConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"5m"`
	DBConnMaxIdleTime time.Duration `env:"DB_CONN_MAX_IDLE_TIME" envDefault:"1m"`
	DBPingTimeout     time.Duration `env:"DB_PING_TIMEOUT" envDefault:"5s"`

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
	MaxPublishSize   int     `env:"GATEWAY_MAX_PUBLISH_SIZE" envDefault:"65536"`  // Max message size (64KB)

	// Auth refresh settings
	AuthRefreshRateInterval time.Duration `env:"GATEWAY_AUTH_REFRESH_RATE_INTERVAL" envDefault:"30s"`

	// Logging
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// Environment
	Environment string `env:"ENVIRONMENT" envDefault:"development"`
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
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("GATEWAY_PORT must be between 1 and 65535, got %d", c.Port)
	}

	// Require provisioning database when auth is enabled
	if c.AuthEnabled {
		if c.ProvisioningDBURL == "" {
			return errors.New("PROVISIONING_DATABASE_URL is required when AUTH_ENABLED=true")
		}
	} else {
		if c.DefaultTenantID == "" {
			return errors.New("DEFAULT_TENANT_ID is required when AUTH_ENABLED=false")
		}
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
		if c.IssuerCacheTTL < time.Second {
			return fmt.Errorf("GATEWAY_ISSUER_CACHE_TTL must be >= 1s, got %v", c.IssuerCacheTTL)
		}
		if c.JWKSFetchTimeout < time.Second {
			return fmt.Errorf("GATEWAY_JWKS_FETCH_TIMEOUT must be >= 1s, got %v", c.JWKSFetchTimeout)
		}
		if c.OIDCKeyfuncCacheTTL < time.Second {
			return fmt.Errorf("GATEWAY_OIDC_KEYFUNC_CACHE_TTL must be >= 1s, got %v", c.OIDCKeyfuncCacheTTL)
		}
	}

	// Validate per-tenant channel rules settings
	if c.PerTenantChannelRulesEnabled {
		if c.ChannelRulesCacheTTL < time.Second {
			return fmt.Errorf("GATEWAY_CHANNEL_RULES_CACHE_TTL must be >= 1s, got %v", c.ChannelRulesCacheTTL)
		}
	}

	// Validate DB pool settings
	if c.DBMaxOpenConns < 1 {
		return fmt.Errorf("DB_MAX_OPEN_CONNS must be at least 1, got %d", c.DBMaxOpenConns)
	}
	if c.DBMaxIdleConns < 0 {
		return fmt.Errorf("DB_MAX_IDLE_CONNS must be non-negative, got %d", c.DBMaxIdleConns)
	}
	if c.DBMaxIdleConns > c.DBMaxOpenConns {
		return fmt.Errorf("DB_MAX_IDLE_CONNS (%d) cannot exceed DB_MAX_OPEN_CONNS (%d)", c.DBMaxIdleConns, c.DBMaxOpenConns)
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

	if err := ValidateLogLevel(c.LogLevel); err != nil {
		return err
	}

	if err := ValidateLogFormat(c.LogFormat); err != nil {
		return err
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
			Dur("key_cache_refresh_interval", c.KeyCacheRefreshInterval).
			Dur("key_cache_query_timeout", c.KeyCacheQueryTimeout).
			Int("db_max_open_conns", c.DBMaxOpenConns).
			Int("db_max_idle_conns", c.DBMaxIdleConns).
			Dur("db_conn_max_lifetime", c.DBConnMaxLifetime).
			Dur("db_conn_max_idle_time", c.DBConnMaxIdleTime).
			Dur("db_ping_timeout", c.DBPingTimeout)
		// Don't log the DB URL for security
		if c.ProvisioningDBURL != "" {
			event = event.Bool("provisioning_db_configured", true)
		}
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
			Dur("issuer_cache_ttl", c.IssuerCacheTTL).
			Dur("oidc_keyfunc_cache_ttl", c.OIDCKeyfuncCacheTTL).
			Dur("jwks_fetch_timeout", c.JWKSFetchTimeout).
			Dur("jwks_refresh_interval", c.JWKSRefreshInterval)
	}

	// Add per-tenant channel rules fields when enabled
	if c.PerTenantChannelRulesEnabled {
		event = event.
			Bool("per_tenant_channel_rules_enabled", true).
			Dur("channel_rules_cache_ttl", c.ChannelRulesCacheTTL).
			Strs("fallback_public_channels", c.FallbackPublicChannels)
	}

	event.Msg("Gateway configuration loaded")
}

// MultiIssuerOIDCReady returns true if multi-issuer OIDC is enabled and configured.
func (c *GatewayConfig) MultiIssuerOIDCReady() bool {
	return c.MultiIssuerOIDCEnabled && c.ProvisioningDBURL != ""
}

// PerTenantChannelRulesReady returns true if per-tenant channel rules are enabled and configured.
func (c *GatewayConfig) PerTenantChannelRulesReady() bool {
	return c.PerTenantChannelRulesEnabled && c.ProvisioningDBURL != ""
}
