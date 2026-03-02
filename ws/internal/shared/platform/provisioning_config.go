package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// ProvisioningConfig holds all provisioning service configuration.
// Tags:
//
//	env: Environment variable name
//	envDefault: Default value if not set
//	required: Must be provided (no default)
type ProvisioningConfig struct {
	// Server
	Addr      string `env:"PROVISIONING_ADDR" envDefault:":8080"`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// Provisioning Mode: "api" (database-backed, default) or "config" (file-backed)
	ProvisioningMode string `env:"PROVISIONING_MODE" envDefault:"api"`
	ConfigFilePath   string `env:"PROVISIONING_CONFIG_PATH"`

	// Database — driver auto-detected from Helm values, not set directly by developers.
	// sqlite (default, embedded) or postgres (opt-in via Helm postgresql.enabled or externalDatabase).
	DatabaseDriver    string        `env:"DATABASE_DRIVER" envDefault:"sqlite"`
	DatabaseURL       string        `env:"DATABASE_URL"`
	DatabasePath      string        `env:"DATABASE_PATH" envDefault:"odin.db"`
	AutoMigrate       bool          `env:"AUTO_MIGRATE" envDefault:"true"`
	DBMaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25"`
	DBMaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5"`
	DBConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"5m"`

	// gRPC — internal service-to-service communication port
	GRPCPort int `env:"GRPC_PORT" envDefault:"9090"`

	// Admin Authentication — opaque admin token for operator access (separate from tenant JWT)
	AdminToken string `env:"PROVISIONING_ADMIN_TOKEN"`

	// Admin Auth Rate Limiting
	AdminAuthFailureThreshold int           `env:"ADMIN_AUTH_FAILURE_THRESHOLD" envDefault:"10"`
	AdminAuthBlockDuration    time.Duration `env:"ADMIN_AUTH_BLOCK_DURATION" envDefault:"60s"`
	AdminAuthCleanupInterval  time.Duration `env:"ADMIN_AUTH_CLEANUP_INTERVAL" envDefault:"5m"`
	AdminAuthCleanupMaxAge    time.Duration `env:"ADMIN_AUTH_CLEANUP_MAX_AGE" envDefault:"2m"`

	// Topic Defaults
	TopicNamespaceOverride string `env:"KAFKA_TOPIC_NAMESPACE_OVERRIDE" envDefault:""`
	ValidNamespaces    string `env:"VALID_NAMESPACES" envDefault:"local,dev,stag,prod"` // Comma-separated valid namespace prefixes
	DefaultPartitions  int    `env:"DEFAULT_PARTITIONS" envDefault:"3"`
	DefaultRetentionMs int64  `env:"DEFAULT_RETENTION_MS" envDefault:"604800000"` // 7 days

	// Quotas (defaults per tenant)
	MaxTopicsPerTenant     int   `env:"MAX_TOPICS_PER_TENANT" envDefault:"50"`
	MaxPartitionsPerTenant int   `env:"MAX_PARTITIONS_PER_TENANT" envDefault:"200"`
	MaxStorageBytes        int64 `env:"MAX_STORAGE_BYTES" envDefault:"10737418240"` // 10GB
	ProducerByteRate       int64 `env:"PRODUCER_BYTE_RATE" envDefault:"10485760"`   // 10MB/s
	ConsumerByteRate       int64 `env:"CONSUMER_BYTE_RATE" envDefault:"52428800"`   // 50MB/s

	// Tenant Lifecycle
	DeprovisionGraceDays    int           `env:"DEPROVISION_GRACE_DAYS" envDefault:"30"`
	LifecycleCheckInterval  time.Duration `env:"LIFECYCLE_CHECK_INTERVAL" envDefault:"1h"`
	LifecycleManagerEnabled bool          `env:"LIFECYCLE_MANAGER_ENABLED" envDefault:"true"`

	// Rate Limiting
	APIRateLimitPerMinute int `env:"API_RATE_LIMIT_PER_MIN" envDefault:"60"`

	// HTTP Server
	HTTPReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	HTTPWriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`
	HTTPIdleTimeout  time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`

	// Key Registry (for JWT validation in API mode with auth enabled)
	KeyRegistryRefreshInterval time.Duration `env:"KEY_REGISTRY_REFRESH_INTERVAL" envDefault:"1m"`
	KeyRegistryQueryTimeout    time.Duration `env:"KEY_REGISTRY_QUERY_TIMEOUT" envDefault:"5s"`

	// Graceful shutdown timeout
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`

	// Authentication (requires DATABASE_URL for key registry)
	AuthEnabled bool `env:"AUTH_ENABLED" envDefault:"false"`

	// OIDC/JWKS support (external IdP tokens)
	// OIDC is enabled when both IssuerURL and JWKSURL are set
	OIDCIssuerURL string `env:"OIDC_ISSUER_URL"`
	OIDCAudience  string `env:"OIDC_AUDIENCE"`
	OIDCJWKSURL   string `env:"OIDC_JWKS_URL"`

	// CORS settings
	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
	CORSMaxAge         int      `env:"CORS_MAX_AGE" envDefault:"3600"`

	// Environment
	Environment string `env:"ENVIRONMENT" envDefault:"development"`
}

// LoadProvisioningConfig reads provisioning service configuration from .env file
// and environment variables.
// Priority: ENV vars > .env file > defaults
func LoadProvisioningConfig(logger *zerolog.Logger) (*ProvisioningConfig, error) {
	// Load .env file (optional)
	if err := godotenv.Load(); err != nil {
		if logger != nil {
			logger.Info().Msg("No .env file found (using environment variables only)")
		} else {
			fmt.Println("Info: No .env file found (using environment variables only)")
		}
	} else {
		if logger != nil {
			logger.Info().Msg("Loaded configuration from .env file")
		}
	}

	cfg := &ProvisioningConfig{}

	// Parse environment variables into struct
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validation
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	if logger != nil {
		logger.Info().Msg("Configuration loaded and validated successfully")
	}

	return cfg, nil
}

// Validate checks provisioning configuration for errors.
func (c *ProvisioningConfig) Validate() error {
	// Required fields
	if c.Addr == "" {
		return errors.New("PROVISIONING_ADDR is required")
	}

	// Provisioning mode validation
	validModes := map[string]bool{"api": true, "config": true}
	if !validModes[c.ProvisioningMode] {
		return fmt.Errorf("PROVISIONING_MODE must be one of: api, config (got: %s)", c.ProvisioningMode)
	}

	// Mode-dependent validation
	if c.ProvisioningMode == "config" {
		if c.ConfigFilePath == "" {
			return errors.New("PROVISIONING_CONFIG_PATH is required when PROVISIONING_MODE=config")
		}
	} else {
		// API mode — validate database config
		validDrivers := map[string]bool{"sqlite": true, "postgres": true}
		if !validDrivers[c.DatabaseDriver] {
			return fmt.Errorf("DATABASE_DRIVER must be one of: sqlite, postgres (got: %s)", c.DatabaseDriver)
		}
		if c.DatabaseDriver == "postgres" && c.DatabaseURL == "" {
			return errors.New("DATABASE_URL is required when DATABASE_DRIVER=postgres")
		}
	}

	// gRPC port validation
	if c.GRPCPort < 1 || c.GRPCPort > 65535 {
		return fmt.Errorf("GRPC_PORT must be between 1 and 65535, got %d", c.GRPCPort)
	}

	// Admin token validation
	if c.AdminToken != "" && len(c.AdminToken) < 16 {
		env := strings.ToLower(strings.TrimSpace(c.Environment))
		if env != "dev" && env != "development" && env != "local" {
			return fmt.Errorf("PROVISIONING_ADMIN_TOKEN must be at least 16 characters in non-development environments (got %d)", len(c.AdminToken))
		}
		// In dev: warning is logged at startup, not a validation error
	}

	// Range checks
	if c.DefaultPartitions < 1 || c.DefaultPartitions > 100 {
		return fmt.Errorf("DEFAULT_PARTITIONS must be 1-100, got %d", c.DefaultPartitions)
	}
	if c.MaxTopicsPerTenant < 1 {
		return fmt.Errorf("MAX_TOPICS_PER_TENANT must be > 0, got %d", c.MaxTopicsPerTenant)
	}
	if c.MaxPartitionsPerTenant < 1 {
		return fmt.Errorf("MAX_PARTITIONS_PER_TENANT must be > 0, got %d", c.MaxPartitionsPerTenant)
	}
	if c.DeprovisionGraceDays < 0 {
		return fmt.Errorf("DEPROVISION_GRACE_DAYS must be >= 0, got %d", c.DeprovisionGraceDays)
	}
	if c.APIRateLimitPerMinute < 1 {
		return fmt.Errorf("API_RATE_LIMIT_PER_MIN must be > 0, got %d", c.APIRateLimitPerMinute)
	}

	// Database pool validation (only relevant for postgres in API mode)
	if c.ProvisioningMode == "api" && c.DatabaseDriver == "postgres" {
		if c.DBMaxOpenConns < 1 {
			return fmt.Errorf("DB_MAX_OPEN_CONNS must be > 0, got %d", c.DBMaxOpenConns)
		}
		if c.DBMaxIdleConns < 0 {
			return fmt.Errorf("DB_MAX_IDLE_CONNS must be >= 0, got %d", c.DBMaxIdleConns)
		}
		if c.DBMaxIdleConns > c.DBMaxOpenConns {
			return fmt.Errorf("DB_MAX_IDLE_CONNS (%d) must be <= DB_MAX_OPEN_CONNS (%d)",
				c.DBMaxIdleConns, c.DBMaxOpenConns)
		}
	}

	// Enum checks
	if err := ValidateLogLevel(c.LogLevel); err != nil {
		return err
	}

	if err := ValidateLogFormat(c.LogFormat); err != nil {
		return err
	}

	// Topic namespace validation (config-driven)
	validNS := parseNamespaces(c.ValidNamespaces)
	if len(validNS) == 0 {
		return errors.New("VALID_NAMESPACES must contain at least one namespace")
	}
	if c.TopicNamespaceOverride != "" && !validNS[c.TopicNamespaceOverride] {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE must be one of: %s (got: %s)",
			c.ValidNamespaces, c.TopicNamespaceOverride)
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

	// Validate CORS settings
	if c.CORSMaxAge < 0 {
		return fmt.Errorf("CORS_MAX_AGE must be >= 0, got %d", c.CORSMaxAge)
	}

	// Prod guard: namespace override is only for dev/stg
	env := strings.ToLower(strings.TrimSpace(c.Environment))
	if env == "prod" && c.TopicNamespaceOverride != "" {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE is not allowed in production (environment: %s)", c.Environment)
	}

	return nil
}

// OIDCEnabled returns true if OIDC is configured (both IssuerURL and JWKSURL are set).
func (c *ProvisioningConfig) OIDCEnabled() bool {
	return c.OIDCIssuerURL != "" && c.OIDCJWKSURL != ""
}

// Print logs provisioning configuration for debugging (human-readable format).
func (c *ProvisioningConfig) Print() {
	fmt.Println("=== Provisioning Service Configuration ===")
	fmt.Printf("Environment:        %s\n", c.Environment)
	fmt.Printf("Address:            %s\n", c.Addr)
	fmt.Printf("Provisioning Mode:  %s\n", c.ProvisioningMode)
	fmt.Printf("gRPC Port:          %d\n", c.GRPCPort)
	if c.AdminToken != "" {
		fmt.Printf("Admin Token:        [REDACTED]\n")
	}
	if c.ProvisioningMode == "config" {
		fmt.Printf("Config File:        %s\n", c.ConfigFilePath)
	}
	fmt.Println("\n=== Database ===")
	fmt.Printf("Database Driver:    %s\n", c.DatabaseDriver)
	if c.DatabaseDriver == "postgres" {
		fmt.Printf("Database URL:       %s\n", maskDatabaseURL(c.DatabaseURL))
		fmt.Printf("Max Open Conns:     %d\n", c.DBMaxOpenConns)
		fmt.Printf("Max Idle Conns:     %d\n", c.DBMaxIdleConns)
		fmt.Printf("Conn Max Lifetime:  %s\n", c.DBConnMaxLifetime)
	} else {
		fmt.Printf("Database Path:      %s\n", c.DatabasePath)
	}
	fmt.Printf("Auto Migrate:       %v\n", c.AutoMigrate)
	fmt.Println("\n=== Topic Defaults ===")
	if c.TopicNamespaceOverride != "" {
		fmt.Printf("Namespace Override: %s\n", c.TopicNamespaceOverride)
	}
	fmt.Printf("Partitions:         %d\n", c.DefaultPartitions)
	fmt.Printf("Retention:          %d ms (%d days)\n", c.DefaultRetentionMs, c.DefaultRetentionMs/86400000)
	fmt.Println("\n=== Tenant Quotas (Defaults) ===")
	fmt.Printf("Max Topics:         %d\n", c.MaxTopicsPerTenant)
	fmt.Printf("Max Partitions:     %d\n", c.MaxPartitionsPerTenant)
	fmt.Printf("Max Storage:        %d MB\n", c.MaxStorageBytes/(1024*1024))
	fmt.Printf("Producer Rate:      %d MB/s\n", c.ProducerByteRate/(1024*1024))
	fmt.Printf("Consumer Rate:      %d MB/s\n", c.ConsumerByteRate/(1024*1024))
	fmt.Println("\n=== Tenant Lifecycle ===")
	fmt.Printf("Grace Period:       %d days\n", c.DeprovisionGraceDays)
	fmt.Println("\n=== Rate Limiting ===")
	fmt.Printf("API Rate Limit:     %d req/min\n", c.APIRateLimitPerMinute)
	fmt.Println("\n=== HTTP Server ===")
	fmt.Printf("Read Timeout:       %s\n", c.HTTPReadTimeout)
	fmt.Printf("Write Timeout:      %s\n", c.HTTPWriteTimeout)
	fmt.Printf("Idle Timeout:       %s\n", c.HTTPIdleTimeout)
	fmt.Println("\n=== Logging ===")
	fmt.Printf("Level:              %s\n", c.LogLevel)
	fmt.Printf("Format:             %s\n", c.LogFormat)
	fmt.Println("==========================================")
}

// LogConfig logs provisioning configuration using structured logging.
func (c *ProvisioningConfig) LogConfig(logger zerolog.Logger) {
	event := logger.Info().
		Str("environment", c.Environment).
		Str("addr", c.Addr).
		Str("provisioning_mode", c.ProvisioningMode).
		Str("database_driver", c.DatabaseDriver).
		Int("grpc_port", c.GRPCPort).
		Bool("auto_migrate", c.AutoMigrate).
		Str("topic_namespace_override", c.TopicNamespaceOverride).
		Int("default_partitions", c.DefaultPartitions).
		Int64("default_retention_ms", c.DefaultRetentionMs).
		Int("max_topics_per_tenant", c.MaxTopicsPerTenant).
		Int("max_partitions_per_tenant", c.MaxPartitionsPerTenant).
		Int("deprovision_grace_days", c.DeprovisionGraceDays).
		Int("api_rate_limit_per_min", c.APIRateLimitPerMinute).
		Dur("http_read_timeout", c.HTTPReadTimeout).
		Dur("http_write_timeout", c.HTTPWriteTimeout).
		Dur("http_idle_timeout", c.HTTPIdleTimeout).
		Bool("auth_enabled", c.AuthEnabled).
		Strs("cors_allowed_origins", c.CORSAllowedOrigins).
		Int("cors_max_age", c.CORSMaxAge).
		Str("log_level", c.LogLevel).
		Str("log_format", c.LogFormat)

	// Admin token — redact, never log the value
	if c.AdminToken != "" {
		event = event.Str("admin_token", "[REDACTED]")
	}

	// Mode-specific fields
	if c.ProvisioningMode == "config" {
		event = event.Str("config_file_path", c.ConfigFilePath)
	} else if c.DatabaseDriver == "postgres" {
		event = event.
			Int("db_max_open_conns", c.DBMaxOpenConns).
			Int("db_max_idle_conns", c.DBMaxIdleConns).
			Dur("db_conn_max_lifetime", c.DBConnMaxLifetime)
	} else {
		event = event.Str("database_path", c.DatabasePath)
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

	event.Msg("Provisioning service configuration loaded")
}

// ParsedValidNamespaces returns the ValidNamespaces string as a set.
func (c *ProvisioningConfig) ParsedValidNamespaces() map[string]bool {
	return parseNamespaces(c.ValidNamespaces)
}

// parseNamespaces converts a comma-separated namespace string into a set.
func parseNamespaces(raw string) map[string]bool {
	ns := map[string]bool{}
	for s := range strings.SplitSeq(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			ns[s] = true
		}
	}
	return ns
}

// maskDatabaseURL masks the password in a database URL for logging.
func maskDatabaseURL(url string) string {
	// Simple masking - just indicate it's set
	if url == "" {
		return "(not set)"
	}
	return "(set, password masked)"
}
