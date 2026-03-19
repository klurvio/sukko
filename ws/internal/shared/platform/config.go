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

// OIDCConfig holds OIDC/JWKS settings for external IdP token validation.
// Embedded by gateway and provisioning.
// OIDC is enabled when both IssuerURL and JWKSURL are set.
type OIDCConfig struct {
	OIDCIssuerURL string `env:"OIDC_ISSUER_URL"`
	OIDCAudience  string `env:"OIDC_AUDIENCE"`
	OIDCJWKSURL   string `env:"OIDC_JWKS_URL"`
}

// OIDCEnabled returns true if OIDC is configured (both IssuerURL and JWKSURL are set).
func (c *OIDCConfig) OIDCEnabled() bool {
	return c.OIDCIssuerURL != "" && c.OIDCJWKSURL != ""
}

// Validate checks OIDC config — if one URL is set, both must be set.
func (c *OIDCConfig) Validate() error {
	hasIssuer := c.OIDCIssuerURL != ""
	hasJWKS := c.OIDCJWKSURL != ""
	if hasIssuer != hasJWKS {
		if !hasIssuer {
			return errors.New("OIDC_ISSUER_URL is required when OIDC_JWKS_URL is set")
		}
		return errors.New("OIDC_JWKS_URL is required when OIDC_ISSUER_URL is set")
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
