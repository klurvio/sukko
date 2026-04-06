package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// BaseConfig contains configuration fields shared across ALL services.
type BaseConfig struct {
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// Environment — deployment identity label, used for Kafka topic namespace, consumer
	// group naming, and safety guards. Free-form: any string works as deployment identity.
	// Sukko uses: local | dev | stg | prod by convention.
	Environment string `env:"ENVIRONMENT" envDefault:"local"`

	// LicenseKey is the signed Ed25519 license token that determines the edition
	// (Community/Pro/Enterprise). Empty = Community. Parsed by license.Manager at startup.
	// The editionManager field lives on each service config (ServerConfig, GatewayConfig,
	// ProvisioningConfig) — not here — to avoid BaseConfig depending on the license package.
	LicenseKey string `env:"SUKKO_LICENSE_KEY" redact:"true"`

	// Tracing (OpenTelemetry) — cold-path only, disabled by default.
	OTELTracingEnabled   bool   `env:"OTEL_TRACING_ENABLED" envDefault:"false"`
	OTELExporterType     string `env:"OTEL_EXPORTER_TYPE" envDefault:"otlp-grpc"`
	OTELExporterEndpoint string `env:"OTEL_EXPORTER_ENDPOINT" envDefault:"localhost:4317"`

	// Profiling — pprof endpoints and Pyroscope continuous profiling, disabled by default.
	PprofEnabled     bool   `env:"PPROF_ENABLED" envDefault:"false"`
	PyroscopeEnabled bool   `env:"PYROSCOPE_ENABLED" envDefault:"false"`
	PyroscopeAddr    string `env:"PYROSCOPE_ADDR" envDefault:"http://localhost:4040"`
}

// Validate checks that all BaseConfig fields have valid values.
func (c *BaseConfig) Validate() error {
	if err := validateLogLevel(c.LogLevel); err != nil {
		return err
	}
	if err := validateLogFormat(c.LogFormat); err != nil {
		return err
	}
	if strings.TrimSpace(c.Environment) == "" {
		return errors.New("ENVIRONMENT must not be empty")
	}
	if c.OTELTracingEnabled {
		switch c.OTELExporterType {
		case "otlp-grpc", "otlp-http", "stdout":
			// valid
		default:
			return fmt.Errorf("OTEL_EXPORTER_TYPE must be one of: otlp-grpc, otlp-http, stdout (got: %s)", c.OTELExporterType)
		}
		if c.OTELExporterType != "stdout" && c.OTELExporterEndpoint == "" {
			return errors.New("OTEL_EXPORTER_ENDPOINT is required when OTEL_TRACING_ENABLED=true and exporter is not stdout")
		}
	}
	return nil
}

// AuthConfig holds the authentication toggle.
// Embedded by gateway and provisioning (not server — server relies on network-level security).
type AuthConfig struct {
	AuthEnabled bool `env:"AUTH_ENABLED" envDefault:"true"`
}

// ProvisioningClientConfig holds gRPC client settings for connecting to the provisioning service.
// Embedded by gateway and server (not provisioning — it IS the gRPC server).
type ProvisioningClientConfig struct {
	ProvisioningGRPCAddr  string        `env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"`
	GRPCReconnectDelay    time.Duration `env:"PROVISIONING_GRPC_RECONNECT_DELAY" envDefault:"1s"`
	GRPCReconnectMaxDelay time.Duration `env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY" envDefault:"30s"`
}

// Validate checks provisioning client config for errors.
func (c *ProvisioningClientConfig) Validate() error {
	if c.ProvisioningGRPCAddr == "" {
		return errors.New("PROVISIONING_GRPC_ADDR is required")
	}
	if c.GRPCReconnectDelay < 100*time.Millisecond {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_DELAY must be >= 100ms, got %v", c.GRPCReconnectDelay)
	}
	if c.GRPCReconnectMaxDelay < c.GRPCReconnectDelay {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_MAX_DELAY (%v) must be >= PROVISIONING_GRPC_RECONNECT_DELAY (%v)",
			c.GRPCReconnectMaxDelay, c.GRPCReconnectDelay)
	}
	return nil
}

// HTTPTimeoutConfig holds HTTP server timeout settings.
// Embedded by server and provisioning (gateway uses GATEWAY_*_TIMEOUT env var names).
type HTTPTimeoutConfig struct {
	HTTPReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	HTTPWriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`
	HTTPIdleTimeout  time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
}

// Validate checks HTTP timeout config for errors.
func (c *HTTPTimeoutConfig) Validate() error {
	if c.HTTPReadTimeout < MinTimeout || c.HTTPReadTimeout > MaxReadWriteTimeout {
		return fmt.Errorf("HTTP_READ_TIMEOUT must be between %v and %v, got %v", MinTimeout, MaxReadWriteTimeout, c.HTTPReadTimeout)
	}
	if c.HTTPWriteTimeout < MinTimeout || c.HTTPWriteTimeout > MaxReadWriteTimeout {
		return fmt.Errorf("HTTP_WRITE_TIMEOUT must be between %v and %v, got %v", MinTimeout, MaxReadWriteTimeout, c.HTTPWriteTimeout)
	}
	if c.HTTPIdleTimeout < MinTimeout || c.HTTPIdleTimeout > MaxIdleTimeout {
		return fmt.Errorf("HTTP_IDLE_TIMEOUT must be between %v and %v, got %v", MinTimeout, MaxIdleTimeout, c.HTTPIdleTimeout)
	}
	return nil
}

// KafkaNamespaceConfig holds Kafka topic namespace settings.
// Embedded by server and provisioning.
type KafkaNamespaceConfig struct {
	// KafkaTopicNamespaceOverride overrides ENVIRONMENT for Kafka topic naming only.
	// If empty, defaults to normalized ENVIRONMENT value via kafka.ResolveNamespace().
	// NOT allowed in production — startup validation blocks this.
	KafkaTopicNamespaceOverride string `env:"KAFKA_TOPIC_NAMESPACE_OVERRIDE" envDefault:""`

	// ValidNamespaces is a comma-separated list of allowed topic namespace prefixes.
	ValidNamespaces string `env:"VALID_NAMESPACES" envDefault:"local,dev,stag,prod"`
}

// MessageBackendConfig holds message ingestion/persistence configuration.
// Embedded by ServerConfig (ws-server) and future services that need message backend access.
//
// Controls which message ingestion/persistence layer the service uses:
//   - "direct": Messages flow directly to broadcast bus. No persistence, no replay.
//     Zero external dependencies beyond the broadcast bus. Default for lowest friction.
//   - "kafka": Full Kafka/Redpanda integration. Persistence, offset-based replay,
//     multi-tenant consumer isolation. Requires Kafka infrastructure.
//   - "nats": NATS JetStream persistent streams. Sequence-based replay,
//     stream-per-tenant isolation. Lighter than Kafka.
type MessageBackendConfig struct {
	// Message Backend Selection
	MessageBackend string `env:"MESSAGE_BACKEND" envDefault:"direct"`

	// Kafka Configuration (only used when MESSAGE_BACKEND=kafka)
	KafkaBrokers string `env:"KAFKA_BROKERS" envDefault:"localhost:19092"`

	// Kafka Security - SASL Authentication
	//
	// For connecting to managed Kafka/Redpanda services that require authentication.
	// When KafkaSASLEnabled=false (default), connects without authentication (local dev).
	//
	// Supported mechanisms:
	// - scram-sha-256: SCRAM-SHA-256 (recommended, widely supported)
	// - scram-sha-512: SCRAM-SHA-512 (stronger, less common)
	KafkaSASLEnabled   bool   `env:"KAFKA_SASL_ENABLED" envDefault:"false"`
	KafkaSASLMechanism string `env:"KAFKA_SASL_MECHANISM"` // scram-sha-256 or scram-sha-512
	KafkaSASLUsername  string `env:"KAFKA_SASL_USERNAME"`
	KafkaSASLPassword  string `env:"KAFKA_SASL_PASSWORD" redact:"true"`

	// Kafka Security - TLS Encryption
	//
	// For encrypted connections to Kafka. Required for most managed services.
	// When KafkaTLSEnabled=false (default), connects without TLS (local dev).
	//
	// KafkaTLSInsecure: Skip server certificate verification (NOT for production)
	// KafkaTLSCAPath: Path to CA certificate for server verification
	KafkaTLSEnabled  bool   `env:"KAFKA_TLS_ENABLED" envDefault:"false"`
	KafkaTLSInsecure bool   `env:"KAFKA_TLS_INSECURE" envDefault:"false"`
	KafkaTLSCAPath   string `env:"KAFKA_TLS_CA_PATH"`

	// NATS JetStream Configuration (only used when MESSAGE_BACKEND=nats)
	//
	// Connects to an external NATS server with JetStream enabled for persistent
	// message streams and sequence-based replay. Each tenant gets its own stream
	// for natural noisy-tenant isolation.
	NATSJetStreamURLs        string        `env:"NATS_JETSTREAM_URLS"`                            // Comma-separated NATS URLs
	NATSJetStreamToken       string        `env:"NATS_JETSTREAM_TOKEN" redact:"true"`             // Auth token
	NATSJetStreamUser        string        `env:"NATS_JETSTREAM_USER"`                            // Username
	NATSJetStreamPassword    string        `env:"NATS_JETSTREAM_PASSWORD" redact:"true"`          // Password
	NATSJetStreamReplicas    int           `env:"NATS_JETSTREAM_REPLICAS" envDefault:"1"`         // Stream replicas
	NATSJetStreamMaxAge      time.Duration `env:"NATS_JETSTREAM_MAX_AGE" envDefault:"24h"`        // Message retention
	NATSJetStreamTLSEnabled  bool          `env:"NATS_JETSTREAM_TLS_ENABLED" envDefault:"false"`  // TLS for managed NATS (Synadia Cloud, etc.)
	NATSJetStreamTLSInsecure bool          `env:"NATS_JETSTREAM_TLS_INSECURE" envDefault:"false"` // Skip TLS verification (not for production)
	NATSJetStreamTLSCAPath   string        `env:"NATS_JETSTREAM_TLS_CA_PATH"`                     // Custom CA certificate path
}

// Validate checks MessageBackendConfig for errors.
func (c *MessageBackendConfig) Validate() error {
	// Backend type validation
	validBackends := map[string]bool{"direct": true, "kafka": true, "nats": true}
	if !validBackends[c.MessageBackend] {
		return fmt.Errorf("[CONFIG ERROR] MESSAGE_BACKEND=%q is invalid (valid: direct, kafka, nats)", c.MessageBackend)
	}

	// Kafka-specific validation (when MESSAGE_BACKEND=kafka)
	if c.MessageBackend == "kafka" {
		if c.KafkaBrokers == "" {
			return errors.New("KAFKA_BROKERS is required when MESSAGE_BACKEND=kafka")
		}
		if c.KafkaSASLEnabled {
			if err := validateKafkaSASLMechanism(c.KafkaSASLMechanism); err != nil {
				return err
			}
			if c.KafkaSASLUsername == "" {
				return errors.New("KAFKA_SASL_USERNAME is required when KAFKA_SASL_ENABLED=true")
			}
			if c.KafkaSASLPassword == "" {
				return errors.New("KAFKA_SASL_PASSWORD is required when KAFKA_SASL_ENABLED=true")
			}
		}
	}

	// NATS JetStream validation (when MESSAGE_BACKEND=nats)
	if c.MessageBackend == "nats" {
		if c.NATSJetStreamURLs == "" {
			return errors.New("NATS_JETSTREAM_URLS is required when MESSAGE_BACKEND=nats")
		}
		if c.NATSJetStreamReplicas < 1 {
			return fmt.Errorf("NATS_JETSTREAM_REPLICAS must be >= 1, got %d", c.NATSJetStreamReplicas)
		}
		if c.NATSJetStreamMaxAge < 1*time.Minute {
			return fmt.Errorf("NATS_JETSTREAM_MAX_AGE must be >= 1m, got %v", c.NATSJetStreamMaxAge)
		}
	}

	return nil
}

// Validate checks Kafka namespace config for errors.
// The environment parameter is needed for the prod guard (namespace override is
// forbidden in production). Callers pass their BaseConfig.Environment.
func (c *KafkaNamespaceConfig) Validate(environment string) error {
	validNS := parseNamespaces(c.ValidNamespaces)
	if len(validNS) == 0 {
		return errors.New("VALID_NAMESPACES must contain at least one namespace")
	}
	if c.KafkaTopicNamespaceOverride != "" && !validNS[c.KafkaTopicNamespaceOverride] {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE must be one of: %s (got: %s)",
			c.ValidNamespaces, c.KafkaTopicNamespaceOverride)
	}
	// Prod guard: namespace override is only for dev/stg
	env := strings.ToLower(strings.TrimSpace(environment))
	if env == "prod" && c.KafkaTopicNamespaceOverride != "" {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE is not allowed in production (environment: %s)", environment)
	}
	return nil
}

// DatabaseConfig holds PostgreSQL connection settings.
// Embedded by services that use a database (provisioning, push).
// Gateway and ws-server are stateless — they don't embed this.
type DatabaseConfig struct {
	DatabaseURL string `env:"DATABASE_URL" redact:"true"`
}

// Validate checks that DATABASE_URL is configured.
func (c *DatabaseConfig) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	return nil
}
