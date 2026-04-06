package push

import (
	"errors"
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/platform"
)

// Config holds all push notification service configuration.
// Tags:
//
//	env: Environment variable name
//	envDefault: Default value if not set
type Config struct {
	platform.BaseConfig
	platform.ProvisioningClientConfig
	platform.MessageBackendConfig
	platform.KafkaNamespaceConfig
	platform.DatabaseConfig

	// Worker pool for sending push notifications
	WorkerPoolSize int `env:"PUSH_WORKER_POOL_SIZE" envDefault:"200"`
	JobQueueSize   int `env:"PUSH_JOB_QUEUE_SIZE" envDefault:"10000"`

	// Dry run mode — logs push payloads without sending to push services
	DryRun bool `env:"PUSH_DRY_RUN" envDefault:"false"`

	// gRPC — internal service-to-service communication port
	GRPCPort int `env:"PUSH_GRPC_PORT" envDefault:"3008"`

	// HTTP — health, metrics, and admin endpoints
	HTTPPort int `env:"PUSH_HTTP_PORT" envDefault:"3009"`

	// Push notification defaults
	DefaultTTL     int    `env:"PUSH_DEFAULT_TTL" envDefault:"2419200"`    // 28 days in seconds
	DefaultUrgency string `env:"PUSH_DEFAULT_URGENCY" envDefault:"normal"` // Web Push urgency header

	// Retry configuration for failed push deliveries
	MaxRetries int `env:"PUSH_MAX_RETRIES" envDefault:"3"`

	// editionManager holds the license-resolved edition and limits.
	// Not parsed from env — created from LicenseKey during LoadConfig.
	editionManager *license.Manager
}

// EditionManager returns the license manager for this config.
func (c *Config) EditionManager() *license.Manager {
	return c.editionManager
}

// Validate checks push service configuration for errors.
func (c *Config) Validate() error {
	// Shared field validation (LogLevel, LogFormat, Environment)
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config: %w", err)
	}

	// Provisioning client validation (gRPC address, reconnect delays)
	if err := c.ProvisioningClientConfig.Validate(); err != nil {
		return fmt.Errorf("provisioning client config: %w", err)
	}

	// Message backend validation (backend type, broker/NATS settings)
	if err := c.MessageBackendConfig.Validate(); err != nil {
		return fmt.Errorf("message backend config: %w", err)
	}

	// Topic namespace validation (includes prod guard)
	if err := c.KafkaNamespaceConfig.Validate(c.Environment); err != nil {
		return fmt.Errorf("kafka namespace config: %w", err)
	}

	// Push notifications require persistent message ingestion — direct mode has no
	// persistence, no replay, and no consumer groups, making it unsuitable for
	// reliable push delivery.
	if c.MessageBackend == "direct" {
		return errors.New("push notifications require kafka or nats message backend")
	}

	// Database URL validation (PostgreSQL via pgxpool)
	if err := c.DatabaseConfig.Validate(); err != nil {
		return fmt.Errorf("database config: %w", err)
	}

	// Worker pool validation
	if c.WorkerPoolSize < 1 {
		return fmt.Errorf("PUSH_WORKER_POOL_SIZE must be >= 1, got %d", c.WorkerPoolSize)
	}
	if c.JobQueueSize < 1 {
		return fmt.Errorf("PUSH_JOB_QUEUE_SIZE must be >= 1, got %d", c.JobQueueSize)
	}

	// Port validation
	if c.GRPCPort < 1 {
		return fmt.Errorf("PUSH_GRPC_PORT must be > 0, got %d", c.GRPCPort)
	}
	if c.HTTPPort < 1 {
		return fmt.Errorf("PUSH_HTTP_PORT must be > 0, got %d", c.HTTPPort)
	}

	// Push notification defaults
	validUrgencies := map[string]bool{"very-low": true, "low": true, "normal": true, "high": true}
	if !validUrgencies[c.DefaultUrgency] {
		return fmt.Errorf("PUSH_DEFAULT_URGENCY=%q is invalid (valid: very-low, low, normal, high)", c.DefaultUrgency)
	}

	// Retry validation
	if c.MaxRetries < 0 {
		return fmt.Errorf("PUSH_MAX_RETRIES must be >= 0, got %d", c.MaxRetries)
	}

	return nil
}

// LoadConfig reads push service configuration from .env file and environment variables.
// Priority: ENV vars > .env file > defaults
func LoadConfig(logger zerolog.Logger) (*Config, error) {
	// Load .env file (optional)
	if err := godotenv.Load(); err != nil {
		logger.Info().Msg("No .env file found (using environment variables only)")
	} else {
		logger.Info().Msg("Loaded configuration from .env file")
	}

	cfg := &Config{}

	// Parse environment variables into struct
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validation
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Resolve edition from license key
	mgr, err := license.NewManager(cfg.LicenseKey, logger)
	if err != nil {
		return nil, fmt.Errorf("license initialization failed: %w", err)
	}
	cfg.editionManager = mgr

	// Push service is Enterprise-only (Constitution XIII)
	if !mgr.HasFeature(license.PushNotifications) {
		return nil, fmt.Errorf("push notifications require Enterprise edition (current: %s)", mgr.Edition())
	}

	logger.Info().
		Str("edition", mgr.Edition().String()).
		Msg("Configuration loaded and validated successfully")

	return cfg, nil
}
