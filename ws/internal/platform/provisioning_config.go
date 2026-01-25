package platform

import (
	"fmt"
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

	// Database
	DatabaseURL       string        `env:"DATABASE_URL,required"`
	DBMaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25"`
	DBMaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5"`
	DBConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"5m"`

	// Kafka/Redpanda Admin
	KafkaBrokers      string        `env:"KAFKA_BROKERS"`
	KafkaAdminTimeout time.Duration `env:"KAFKA_ADMIN_TIMEOUT" envDefault:"30s"`

	// Kafka Security - SASL Authentication
	KafkaSASLEnabled   bool   `env:"KAFKA_SASL_ENABLED" envDefault:"false"`
	KafkaSASLMechanism string `env:"KAFKA_SASL_MECHANISM"`
	KafkaSASLUsername  string `env:"KAFKA_SASL_USERNAME"`
	KafkaSASLPassword  string `env:"KAFKA_SASL_PASSWORD"`

	// Kafka Security - TLS Encryption
	KafkaTLSEnabled  bool   `env:"KAFKA_TLS_ENABLED" envDefault:"false"`
	KafkaTLSInsecure bool   `env:"KAFKA_TLS_INSECURE" envDefault:"false"`
	KafkaTLSCAPath   string `env:"KAFKA_TLS_CA_PATH"`

	// Topic Defaults
	TopicNamespace     string `env:"KAFKA_TOPIC_NAMESPACE" envDefault:"main"`
	DefaultPartitions  int    `env:"DEFAULT_PARTITIONS" envDefault:"3"`
	DefaultRetentionMs int64  `env:"DEFAULT_RETENTION_MS" envDefault:"604800000"` // 7 days

	// Quotas (defaults per tenant)
	MaxTopicsPerTenant     int   `env:"MAX_TOPICS_PER_TENANT" envDefault:"50"`
	MaxPartitionsPerTenant int   `env:"MAX_PARTITIONS_PER_TENANT" envDefault:"200"`
	MaxStorageBytes        int64 `env:"MAX_STORAGE_BYTES" envDefault:"10737418240"` // 10GB
	ProducerByteRate       int64 `env:"PRODUCER_BYTE_RATE" envDefault:"10485760"`   // 10MB/s
	ConsumerByteRate       int64 `env:"CONSUMER_BYTE_RATE" envDefault:"52428800"`   // 50MB/s

	// Tenant Lifecycle
	DeprovisionGraceDays int `env:"DEPROVISION_GRACE_DAYS" envDefault:"30"`

	// Rate Limiting
	APIRateLimitPerMinute int `env:"API_RATE_LIMIT_PER_MIN" envDefault:"60"`

	// HTTP Server
	HTTPReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	HTTPWriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`
	HTTPIdleTimeout  time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`

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
		return fmt.Errorf("PROVISIONING_ADDR is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
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

	// Database pool validation
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

	// Enum checks
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error (got: %s)", c.LogLevel)
	}

	validLogFormats := map[string]bool{"json": true, "text": true, "pretty": true}
	if !validLogFormats[c.LogFormat] {
		return fmt.Errorf("LOG_FORMAT must be one of: json, text, pretty (got: %s)", c.LogFormat)
	}

	// Kafka SASL validation
	if c.KafkaSASLEnabled {
		validMechanisms := map[string]bool{"scram-sha-256": true, "scram-sha-512": true}
		if !validMechanisms[c.KafkaSASLMechanism] {
			return fmt.Errorf("KAFKA_SASL_MECHANISM must be 'scram-sha-256' or 'scram-sha-512', got: %s",
				c.KafkaSASLMechanism)
		}
		if c.KafkaSASLUsername == "" {
			return fmt.Errorf("KAFKA_SASL_USERNAME is required when KAFKA_SASL_ENABLED=true")
		}
		if c.KafkaSASLPassword == "" {
			return fmt.Errorf("KAFKA_SASL_PASSWORD is required when KAFKA_SASL_ENABLED=true")
		}
	}

	// Topic namespace validation
	validNamespaces := map[string]bool{"local": true, "dev": true, "staging": true, "main": true}
	if !validNamespaces[c.TopicNamespace] {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE must be one of: local, dev, staging, main (got: %s)",
			c.TopicNamespace)
	}

	return nil
}

// Print logs provisioning configuration for debugging (human-readable format).
func (c *ProvisioningConfig) Print() {
	fmt.Println("=== Provisioning Service Configuration ===")
	fmt.Printf("Environment:        %s\n", c.Environment)
	fmt.Printf("Address:            %s\n", c.Addr)
	fmt.Println("\n=== Database ===")
	fmt.Printf("Database URL:       %s\n", maskDatabaseURL(c.DatabaseURL))
	fmt.Printf("Max Open Conns:     %d\n", c.DBMaxOpenConns)
	fmt.Printf("Max Idle Conns:     %d\n", c.DBMaxIdleConns)
	fmt.Printf("Conn Max Lifetime:  %s\n", c.DBConnMaxLifetime)
	fmt.Println("\n=== Kafka/Redpanda ===")
	fmt.Printf("Brokers:            %s\n", c.KafkaBrokers)
	fmt.Printf("Admin Timeout:      %s\n", c.KafkaAdminTimeout)
	fmt.Printf("SASL Enabled:       %v\n", c.KafkaSASLEnabled)
	if c.KafkaSASLEnabled {
		fmt.Printf("SASL Mechanism:     %s\n", c.KafkaSASLMechanism)
		fmt.Printf("SASL Username:      %s\n", c.KafkaSASLUsername)
	}
	fmt.Printf("TLS Enabled:        %v\n", c.KafkaTLSEnabled)
	fmt.Println("\n=== Topic Defaults ===")
	fmt.Printf("Namespace:          %s\n", c.TopicNamespace)
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
	logger.Info().
		Str("environment", c.Environment).
		Str("addr", c.Addr).
		Str("kafka_brokers", c.KafkaBrokers).
		Bool("kafka_sasl_enabled", c.KafkaSASLEnabled).
		Bool("kafka_tls_enabled", c.KafkaTLSEnabled).
		Str("topic_namespace", c.TopicNamespace).
		Int("default_partitions", c.DefaultPartitions).
		Int64("default_retention_ms", c.DefaultRetentionMs).
		Int("max_topics_per_tenant", c.MaxTopicsPerTenant).
		Int("max_partitions_per_tenant", c.MaxPartitionsPerTenant).
		Int("deprovision_grace_days", c.DeprovisionGraceDays).
		Int("api_rate_limit_per_min", c.APIRateLimitPerMinute).
		Int("db_max_open_conns", c.DBMaxOpenConns).
		Int("db_max_idle_conns", c.DBMaxIdleConns).
		Dur("db_conn_max_lifetime", c.DBConnMaxLifetime).
		Dur("http_read_timeout", c.HTTPReadTimeout).
		Dur("http_write_timeout", c.HTTPWriteTimeout).
		Dur("http_idle_timeout", c.HTTPIdleTimeout).
		Str("log_level", c.LogLevel).
		Str("log_format", c.LogFormat).
		Msg("Provisioning service configuration loaded")
}

// maskDatabaseURL masks the password in a database URL for logging.
func maskDatabaseURL(url string) string {
	// Simple masking - just indicate it's set
	if url == "" {
		return "(not set)"
	}
	return "(set, password masked)"
}
