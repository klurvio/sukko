package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// BaseConfig contains configuration fields shared across ALL services.
type BaseConfig struct {
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`  // Logging level (debug, info, warn, error). info is recommended for production.
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"` // Log output format: json for machine parsing (production), text for human-readable (development).

	// Environment — deployment identity label, used for Kafka topic namespace, consumer
	// group naming, and safety guards. Free-form: any string works as deployment identity.
	// Sukko uses: local | dev | stg | prod by convention.
	Environment string `env:"ENVIRONMENT" envDefault:"local"` // Deployment environment name (e.g. local, dev, stag, prod). Used for Kafka topic namespace resolution.

	// LicenseKey is the signed Ed25519 license token that determines the edition
	// (Community/Pro/Enterprise). Empty = Community. Parsed by license.Manager at startup.
	// The editionManager field lives on each service config (ServerConfig, GatewayConfig,
	// ProvisioningConfig) — not here — to avoid BaseConfig depending on the license package.
	LicenseKey string `env:"SUKKO_LICENSE_KEY" redact:"true"` // Sukko license token (JWT). Community edition has no key; Pro/Enterprise obtain via Sukko account. Leave empty for Community.

	// Tracing (OpenTelemetry) — cold-path only, disabled by default.
	OTELTracingEnabled   bool   `env:"OTEL_TRACING_ENABLED" envDefault:"false"`            // Enable distributed tracing via OpenTelemetry. Disabled by default; configure OTEL_EXPORTER_TYPE and OTEL_EXPORTER_ENDPOINT when enabled.
	OTELExporterType     string `env:"OTEL_EXPORTER_TYPE" envDefault:"otlp-grpc"`          // OpenTelemetry trace exporter type: otlp-grpc, otlp-http, or stdout. Required when OTEL_TRACING_ENABLED=true.
	OTELExporterEndpoint string `env:"OTEL_EXPORTER_ENDPOINT" envDefault:"localhost:4317"` // OpenTelemetry collector endpoint (e.g. otelcol:4317). Required when OTEL_EXPORTER_TYPE is otlp-grpc or otlp-http.

	// Profiling — pprof endpoints and Pyroscope continuous profiling, disabled by default.
	PprofEnabled     bool   `env:"PPROF_ENABLED" envDefault:"false"`                  // Enable Go pprof profiling endpoints (/debug/pprof/). Disabled by default; enable only in controlled environments.
	PyroscopeEnabled bool   `env:"PYROSCOPE_ENABLED" envDefault:"false"`              // Enable continuous profiling via Pyroscope. Disabled by default.
	PyroscopeAddr    string `env:"PYROSCOPE_ADDR" envDefault:"http://localhost:4040"` // Pyroscope server address (e.g. http://pyroscope:4040). Required when PYROSCOPE_ENABLED=true.
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

// AuthConfig holds the authentication mode.
// Embedded by gateway and provisioning (not server — server relies on network-level security).
type AuthConfig struct {
	// AuthMode is the authentication model. Only "required" is supported.
	// Kept temporarily for backward compatibility with deployments that set AUTH_MODE=required.
	// Extensible to "public-read" in the future (anonymous subscribe for public channels).
	AuthMode string `env:"AUTH_MODE" envDefault:"required"` // Authentication enforcement mode. Only "required" is supported; tokens without valid signatures are rejected.
}

// Validate checks that AuthMode is valid. Only "required" is accepted.
// AUTH_MODE=disabled was removed — see docs/migration/auth-mode-removal.md.
func (c *AuthConfig) Validate() error {
	if c.AuthMode == "disabled" {
		return errors.New("AUTH_MODE=disabled has been removed. " +
			"See docs/migration/auth-mode-removal.md. " +
			"Set up admin + tenant JWT auth and remove AUTH_MODE/DEFAULT_TENANT_ID env vars")
	}
	if c.AuthMode != "required" {
		return fmt.Errorf("AUTH_MODE must be \"required\", got %q", c.AuthMode)
	}
	return nil
}

// ProvisioningClientConfig holds gRPC client settings for connecting to the provisioning service.
// Embedded by gateway and server (not provisioning — it IS the gRPC server).
// GRPCReconnectConfig is embedded to eliminate the duplicate inline fields
// that previously appeared here, in WebhookWorkerConfig, and in ProvisioningConfig (§X).
type ProvisioningClientConfig struct {
	GRPCReconnectConfig         // embeds PROVISIONING_GRPC_RECONNECT_* env vars; Validate() enforces bounds
	ProvisioningGRPCAddr string `env:"PROVISIONING_GRPC_ADDR" envDefault:"localhost:9090"` // gRPC address of the provisioning service for internal communication (e.g. channel rule streaming).
}

// Validate checks provisioning client config for errors.
func (c *ProvisioningClientConfig) Validate() error {
	if c.ProvisioningGRPCAddr == "" {
		return errors.New("PROVISIONING_GRPC_ADDR is required")
	}
	return c.GRPCReconnectConfig.Validate()
}

// HTTPTimeoutConfig holds HTTP server timeout settings.
// Embedded by server and provisioning (gateway uses GATEWAY_*_TIMEOUT env var names).
type HTTPTimeoutConfig struct {
	HTTPReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`  // Maximum duration for reading a complete HTTP request. Protects against slow-loris attacks.
	HTTPWriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"` // Maximum duration for writing the HTTP response.
	HTTPIdleTimeout  time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`  // Maximum idle time on a keep-alive connection before it is closed.
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
	// Intended for dev/staging environments where a different namespace is needed.
	KafkaTopicNamespaceOverride string `env:"KAFKA_TOPIC_NAMESPACE_OVERRIDE" envDefault:""` // Override for the Kafka topic namespace (normally derived from ENVIRONMENT). Use with care in production deployments.

	// ValidNamespaces is a comma-separated list of allowed topic namespace prefixes.
	ValidNamespaces string `env:"VALID_NAMESPACES" envDefault:"local,dev,stag,prod"` // Comma-separated list of allowed Kafka topic namespaces. Used to validate KAFKA_TOPIC_NAMESPACE_OVERRIDE.
}

// MessageBackend constants for MESSAGE_BACKEND env var.
const (
	MessageBackendDirect = "direct"
	MessageBackendKafka  = "kafka"
)

// MessageBackendBase holds the two fields shared between ws-server, push, and tester
// that need message backend selection without the full Kafka SASL/TLS options.
// Embedded by MessageBackendConfig and TesterConfig.
type MessageBackendBase struct {
	MessageBackend string `env:"MESSAGE_BACKEND" envDefault:"direct"`        // Message ingestion backend: direct (no persistence, no Kafka dependency) or kafka (full Kafka/Redpanda integration with replay).
	KafkaBrokers   string `env:"KAFKA_BROKERS" envDefault:"localhost:19092"` // Comma-separated Kafka/Redpanda broker addresses for ws-server, push, and tester (provisioning uses PROVISIONING_KAFKA_BROKERS instead); required when MESSAGE_BACKEND=kafka.
}

// MessageBackendConfig holds message ingestion/persistence configuration.
// Embedded by ServerConfig (ws-server) and future services that need message backend access.
//
// Controls which message ingestion/persistence layer the service uses:
//   - "direct": Messages flow directly to broadcast bus. No persistence, no replay.
//     Zero external dependencies beyond the broadcast bus. Default for lowest friction.
//   - "kafka": Full Kafka/Redpanda integration. Persistence, offset-based replay,
//     multi-tenant consumer isolation. Requires Kafka infrastructure.
type MessageBackendConfig struct {
	MessageBackendBase

	// Kafka Security - SASL Authentication
	//
	// For connecting to managed Kafka/Redpanda services that require authentication.
	// When KafkaSASLEnabled=false (default), connects without authentication (local dev).
	//
	// Supported mechanisms:
	// - scram-sha-256: SCRAM-SHA-256 (recommended, widely supported)
	// - scram-sha-512: SCRAM-SHA-512 (stronger, less common)
	KafkaSASLEnabled   bool   `env:"KAFKA_SASL_ENABLED" envDefault:"false"` // Enable SASL authentication for Kafka connections. Required for most managed Kafka/Redpanda services.
	KafkaSASLMechanism string `env:"KAFKA_SASL_MECHANISM"`                  // SASL mechanism: scram-sha-256 or scram-sha-512. Required when KAFKA_SASL_ENABLED=true.
	KafkaSASLUsername  string `env:"KAFKA_SASL_USERNAME"`                   // SASL username for Kafka authentication. Required when KAFKA_SASL_ENABLED=true.
	KafkaSASLPassword  string `env:"KAFKA_SASL_PASSWORD" redact:"true"`     // SASL password for Kafka authentication. Required when KAFKA_SASL_ENABLED=true.

	// Kafka Security - TLS Encryption
	//
	// For encrypted connections to Kafka. Required for most managed services.
	// When KafkaTLSEnabled=false (default), connects without TLS (local dev).
	//
	// KafkaTLSInsecure: Skip server certificate verification (NOT for production)
	// KafkaTLSCAPath: Path to CA certificate for server verification
	KafkaTLSEnabled  bool   `env:"KAFKA_TLS_ENABLED" envDefault:"false"`  // Enable TLS encryption for Kafka connections. Required for most managed Kafka/Redpanda services.
	KafkaTLSInsecure bool   `env:"KAFKA_TLS_INSECURE" envDefault:"false"` // Skip TLS certificate verification. For development only — never use in production.
	KafkaTLSCAPath   string `env:"KAFKA_TLS_CA_PATH"`                     // Path to CA certificate file for verifying the Kafka broker's TLS certificate.
}

// Validate checks MessageBackendConfig for errors.
func (c *MessageBackendConfig) Validate() error {
	// Backend type validation
	validBackends := map[string]bool{MessageBackendDirect: true, MessageBackendKafka: true}
	if !validBackends[c.MessageBackend] {
		return fmt.Errorf("[CONFIG ERROR] MESSAGE_BACKEND=%q is invalid (valid: %s, %s)", c.MessageBackend, MessageBackendDirect, MessageBackendKafka)
	}

	// Kafka-specific validation (when MESSAGE_BACKEND=kafka)
	if c.MessageBackend == MessageBackendKafka {
		if c.KafkaBrokers == "" {
			return fmt.Errorf("KAFKA_BROKERS is required when MESSAGE_BACKEND=%s", MessageBackendKafka)
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

	return nil
}

// Validate checks Kafka namespace config for errors.
//
// No environment-name guard on KAFKA_TOPIC_NAMESPACE_OVERRIDE: ENVIRONMENT is operator-defined
// free text, so a check like env == "prod" silently misses "production", "live", etc.
// The VALID_NAMESPACES allowlist is the value-based enforcement boundary. Operators are expected
// to leave KAFKA_TOPIC_NAMESPACE_OVERRIDE unset in production. Do NOT add an env-name guard here.
func (c *KafkaNamespaceConfig) Validate() error {
	validNS := parseNamespaces(c.ValidNamespaces)
	if len(validNS) == 0 {
		return errors.New("VALID_NAMESPACES must contain at least one namespace")
	}
	if c.KafkaTopicNamespaceOverride != "" && !validNS[c.KafkaTopicNamespaceOverride] {
		return fmt.Errorf("KAFKA_TOPIC_NAMESPACE_OVERRIDE must be one of: %s (got: %s)",
			c.ValidNamespaces, c.KafkaTopicNamespaceOverride)
	}
	return nil
}

// DatabaseConfig holds PostgreSQL connection settings.
// Embedded by services that use a database (provisioning, push).
// Gateway and ws-server are stateless — they don't embed this.
type DatabaseConfig struct {
	DatabaseURL string `env:"DATABASE_URL" redact:"true"` // PostgreSQL connection URL (postgres://user:pass@host:5432/db). Required for services that use the database (provisioning, push).
}

// Validate checks that DATABASE_URL is configured.
func (c *DatabaseConfig) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	return nil
}
