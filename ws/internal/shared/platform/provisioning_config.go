package platform

import (
	"errors"
	"fmt"
	"os"
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
	BaseConfig
	AuthConfig
	OIDCConfig
	KafkaNamespaceConfig
	HTTPTimeoutConfig

	// Server
	Addr string `env:"PROVISIONING_ADDR" envDefault:":8080"`

	// Database — driver auto-detected from Helm values, not set directly by developers.
	// sqlite (default, embedded) or postgres (opt-in via Helm postgresql.enabled or externalDatabase).
	DatabaseDriver    string        `env:"DATABASE_DRIVER" envDefault:"sqlite"`
	DatabaseURL       string        `env:"DATABASE_URL" redact:"true"`
	DatabasePath      string        `env:"DATABASE_PATH" envDefault:"sukko.db"`
	AutoMigrate       bool          `env:"AUTO_MIGRATE" envDefault:"true"`
	DBMaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25"`
	DBMaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5"`
	DBConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"5m"`

	// gRPC — internal service-to-service communication port
	GRPCPort int `env:"GRPC_PORT" envDefault:"9090"`

	// Admin Authentication — opaque admin token for operator access (separate from tenant JWT)
	AdminToken string `env:"PROVISIONING_ADMIN_TOKEN" redact:"true"`

	// Admin Auth Rate Limiting
	AdminAuthFailureThreshold int           `env:"ADMIN_AUTH_FAILURE_THRESHOLD" envDefault:"10"`
	AdminAuthBlockDuration    time.Duration `env:"ADMIN_AUTH_BLOCK_DURATION" envDefault:"60s"`
	AdminAuthCleanupInterval  time.Duration `env:"ADMIN_AUTH_CLEANUP_INTERVAL" envDefault:"5m"`
	AdminAuthCleanupMaxAge    time.Duration `env:"ADMIN_AUTH_CLEANUP_MAX_AGE" envDefault:"2m"`

	// Topic Defaults
	DefaultPartitions  int   `env:"DEFAULT_PARTITIONS" envDefault:"3"`
	DefaultRetentionMs int64 `env:"DEFAULT_RETENTION_MS" envDefault:"604800000"` // 7 days

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

	// Routing Rules
	MaxRoutingRules int `env:"MAX_ROUTING_RULES" envDefault:"100"` // Max routing rules per tenant

	// Rate Limiting
	APIRateLimitPerMinute int `env:"API_RATE_LIMIT_PER_MIN" envDefault:"60"`

	// Key Registry (for JWT validation in API mode with auth enabled)
	KeyRegistryRefreshInterval time.Duration `env:"KEY_REGISTRY_REFRESH_INTERVAL" envDefault:"1m"`
	KeyRegistryQueryTimeout    time.Duration `env:"KEY_REGISTRY_QUERY_TIMEOUT" envDefault:"5s"`

	// Graceful shutdown timeout
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`

	// CORS settings
	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
	CORSMaxAge         int      `env:"CORS_MAX_AGE" envDefault:"3600"`

	// Provisioning-specific externalized constants
	MaxTenantsFetchLimit int           `env:"PROVISIONING_MAX_TENANTS_FETCH_LIMIT" envDefault:"10000"`
	DeletionTimeout      time.Duration `env:"PROVISIONING_DELETION_TIMEOUT" envDefault:"5m"`
}

// LoadProvisioningConfig reads provisioning service configuration from .env file
// and environment variables.
// Priority: ENV vars > .env file > defaults
func LoadProvisioningConfig(logger zerolog.Logger) (*ProvisioningConfig, error) {
	// Load .env file (optional)
	if err := godotenv.Load(); err != nil {
		logger.Info().Msg("No .env file found (using environment variables only)")
	} else {
		logger.Info().Msg("Loaded configuration from .env file")
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

	logger.Info().Msg("Configuration loaded and validated successfully")

	return cfg, nil
}

// Validate checks provisioning configuration for errors.
func (c *ProvisioningConfig) Validate() error {
	// Required fields
	if c.Addr == "" {
		return errors.New("PROVISIONING_ADDR is required")
	}

	// Database driver validation
	validDrivers := map[string]bool{"sqlite": true, "postgres": true}
	if !validDrivers[c.DatabaseDriver] {
		return fmt.Errorf("[CONFIG ERROR] DATABASE_DRIVER=%q is invalid (valid: sqlite, postgres)", c.DatabaseDriver)
	}
	if c.DatabaseDriver == "postgres" && c.DatabaseURL == "" {
		return errors.New("[CONFIG ERROR] DATABASE_URL is required when DATABASE_DRIVER=postgres")
	}

	// gRPC port validation
	if c.GRPCPort < 1 || c.GRPCPort > MaxPort {
		return fmt.Errorf("GRPC_PORT must be between 1 and %d, got %d", MaxPort, c.GRPCPort)
	}

	// Admin token validation
	if c.AdminToken != "" && len(c.AdminToken) < 16 {
		envName := strings.ToLower(strings.TrimSpace(c.Environment))
		if envName != "dev" && envName != "development" && envName != "local" {
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
	if c.MaxRoutingRules < 1 {
		return fmt.Errorf("MAX_ROUTING_RULES must be > 0, got %d", c.MaxRoutingRules)
	}
	if c.APIRateLimitPerMinute < 1 {
		return fmt.Errorf("API_RATE_LIMIT_PER_MIN must be > 0, got %d", c.APIRateLimitPerMinute)
	}

	// Database pool validation (only relevant for postgres)
	if c.DatabaseDriver == "postgres" {
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

	// Shared field validation (LogLevel, LogFormat, Environment)
	if err := c.BaseConfig.Validate(); err != nil {
		return err
	}

	// Topic namespace validation (includes prod guard)
	if err := c.KafkaNamespaceConfig.Validate(c.Environment); err != nil {
		return err
	}

	// Validate OIDC settings
	if err := c.OIDCConfig.Validate(); err != nil {
		return err
	}

	// Validate CORS settings
	if c.CORSMaxAge < 0 {
		return fmt.Errorf("CORS_MAX_AGE must be >= 0, got %d", c.CORSMaxAge)
	}

	// DB connection max lifetime (only relevant for postgres)
	if c.DatabaseDriver == "postgres" && c.DBConnMaxLifetime < time.Minute {
		return fmt.Errorf("DB_CONN_MAX_LIFETIME must be >= 1m when DATABASE_DRIVER=postgres, got %s", c.DBConnMaxLifetime)
	}

	// Topic retention
	if c.DefaultRetentionMs < 60000 {
		return fmt.Errorf("DEFAULT_RETENTION_MS must be >= 60000 (1 minute), got %d", c.DefaultRetentionMs)
	}

	// Quota minimums
	if c.MaxStorageBytes < MinStorageBytes {
		return fmt.Errorf("MAX_STORAGE_BYTES must be >= %d (1MB), got %d", MinStorageBytes, c.MaxStorageBytes)
	}
	if c.ProducerByteRate < MinByteRate {
		return fmt.Errorf("PRODUCER_BYTE_RATE must be >= %d, got %d", MinByteRate, c.ProducerByteRate)
	}
	if c.ConsumerByteRate < MinByteRate {
		return fmt.Errorf("CONSUMER_BYTE_RATE must be >= %d, got %d", MinByteRate, c.ConsumerByteRate)
	}

	// Admin auth rate limiting
	if c.AdminAuthFailureThreshold < 1 {
		return fmt.Errorf("ADMIN_AUTH_FAILURE_THRESHOLD must be >= 1, got %d", c.AdminAuthFailureThreshold)
	}
	if c.AdminAuthBlockDuration < time.Second {
		return fmt.Errorf("ADMIN_AUTH_BLOCK_DURATION must be >= 1s, got %s", c.AdminAuthBlockDuration)
	}
	if c.AdminAuthCleanupInterval < time.Second {
		return fmt.Errorf("ADMIN_AUTH_CLEANUP_INTERVAL must be >= 1s, got %s", c.AdminAuthCleanupInterval)
	}
	if c.AdminAuthCleanupMaxAge < time.Second {
		return fmt.Errorf("ADMIN_AUTH_CLEANUP_MAX_AGE must be >= 1s, got %s", c.AdminAuthCleanupMaxAge)
	}

	// HTTP timeouts
	if err := c.HTTPTimeoutConfig.Validate(); err != nil {
		return err
	}

	// Key registry
	if c.KeyRegistryRefreshInterval < time.Second {
		return fmt.Errorf("KEY_REGISTRY_REFRESH_INTERVAL must be >= 1s, got %s", c.KeyRegistryRefreshInterval)
	}
	if c.KeyRegistryQueryTimeout < time.Second {
		return fmt.Errorf("KEY_REGISTRY_QUERY_TIMEOUT must be >= 1s, got %s", c.KeyRegistryQueryTimeout)
	}

	// Graceful shutdown
	if c.ShutdownTimeout < time.Second {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be >= 1s, got %s", c.ShutdownTimeout)
	}

	// Lifecycle check interval (only when lifecycle manager is enabled)
	if c.LifecycleManagerEnabled && c.LifecycleCheckInterval < time.Minute {
		return fmt.Errorf("LIFECYCLE_CHECK_INTERVAL must be >= 1m when LIFECYCLE_MANAGER_ENABLED=true, got %s", c.LifecycleCheckInterval)
	}

	// Provisioning-specific externalized fields
	if c.MaxTenantsFetchLimit < 1 {
		return fmt.Errorf("PROVISIONING_MAX_TENANTS_FETCH_LIMIT must be > 0, got %d", c.MaxTenantsFetchLimit)
	}
	if c.DeletionTimeout <= 0 {
		return fmt.Errorf("PROVISIONING_DELETION_TIMEOUT must be > 0, got %v", c.DeletionTimeout)
	}

	return nil
}

// Print logs provisioning configuration for debugging (human-readable format).
// Uses fmt.Fprint* to os.Stdout for startup display before zerolog is initialized.
// fmt.Fprint* errors are non-actionable: writing to os.Stdout cannot be retried or reported.
func (c *ProvisioningConfig) Print() {
	w := os.Stdout
	_, _ = fmt.Fprintln(w, "=== Provisioning Service Configuration ===")
	_, _ = fmt.Fprintf(w, "Environment:        %s\n", c.Environment)
	_, _ = fmt.Fprintf(w, "Address:            %s\n", c.Addr)
	_, _ = fmt.Fprintf(w, "gRPC Port:          %d\n", c.GRPCPort)
	if c.AdminToken != "" {
		_, _ = fmt.Fprintf(w, "Admin Token:        [REDACTED]\n")
	}
	_, _ = fmt.Fprintln(w, "\n=== Database ===")
	_, _ = fmt.Fprintf(w, "Database Driver:    %s\n", c.DatabaseDriver)
	if c.DatabaseDriver == "postgres" {
		_, _ = fmt.Fprintf(w, "Database URL:       %s\n", maskDatabaseURL(c.DatabaseURL))
		_, _ = fmt.Fprintf(w, "Max Open Conns:     %d\n", c.DBMaxOpenConns)
		_, _ = fmt.Fprintf(w, "Max Idle Conns:     %d\n", c.DBMaxIdleConns)
		_, _ = fmt.Fprintf(w, "Conn Max Lifetime:  %s\n", c.DBConnMaxLifetime)
	} else {
		_, _ = fmt.Fprintf(w, "Database Path:      %s\n", c.DatabasePath)
	}
	_, _ = fmt.Fprintf(w, "Auto Migrate:       %v\n", c.AutoMigrate)
	_, _ = fmt.Fprintln(w, "\n=== Topic Defaults ===")
	if c.KafkaTopicNamespaceOverride != "" {
		_, _ = fmt.Fprintf(w, "Namespace Override: %s\n", c.KafkaTopicNamespaceOverride)
	}
	_, _ = fmt.Fprintf(w, "Partitions:         %d\n", c.DefaultPartitions)
	_, _ = fmt.Fprintf(w, "Retention:          %d ms (%d days)\n", c.DefaultRetentionMs, c.DefaultRetentionMs/86400000)
	_, _ = fmt.Fprintln(w, "\n=== Tenant Quotas (Defaults) ===")
	_, _ = fmt.Fprintf(w, "Max Topics:         %d\n", c.MaxTopicsPerTenant)
	_, _ = fmt.Fprintf(w, "Max Partitions:     %d\n", c.MaxPartitionsPerTenant)
	_, _ = fmt.Fprintf(w, "Max Storage:        %d MB\n", c.MaxStorageBytes/(1024*1024))
	_, _ = fmt.Fprintf(w, "Producer Rate:      %d MB/s\n", c.ProducerByteRate/(1024*1024))
	_, _ = fmt.Fprintf(w, "Consumer Rate:      %d MB/s\n", c.ConsumerByteRate/(1024*1024))
	_, _ = fmt.Fprintln(w, "\n=== Tenant Lifecycle ===")
	_, _ = fmt.Fprintf(w, "Grace Period:       %d days\n", c.DeprovisionGraceDays)
	_, _ = fmt.Fprintln(w, "\n=== Rate Limiting ===")
	_, _ = fmt.Fprintf(w, "API Rate Limit:     %d req/min\n", c.APIRateLimitPerMinute)
	_, _ = fmt.Fprintln(w, "\n=== HTTP Server ===")
	_, _ = fmt.Fprintf(w, "Read Timeout:       %s\n", c.HTTPReadTimeout)
	_, _ = fmt.Fprintf(w, "Write Timeout:      %s\n", c.HTTPWriteTimeout)
	_, _ = fmt.Fprintf(w, "Idle Timeout:       %s\n", c.HTTPIdleTimeout)
	_, _ = fmt.Fprintln(w, "\n=== Logging ===")
	_, _ = fmt.Fprintf(w, "Level:              %s\n", c.LogLevel)
	_, _ = fmt.Fprintf(w, "Format:             %s\n", c.LogFormat)
	_, _ = fmt.Fprintln(w, "==========================================")
}

// LogConfig logs provisioning configuration using structured logging.
func (c *ProvisioningConfig) LogConfig(logger zerolog.Logger) {
	event := logger.Info().
		Str("environment", c.Environment).
		Str("addr", c.Addr).
		Str("database_driver", c.DatabaseDriver).
		Int("grpc_port", c.GRPCPort).
		Bool("auto_migrate", c.AutoMigrate).
		Str("topic_namespace_override", c.KafkaTopicNamespaceOverride).
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

	// Database-specific fields
	if c.DatabaseDriver == "postgres" {
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
