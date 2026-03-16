package platform

// Shared default value constants for envDefault tags that appear in multiple service configs.
//
// These constants are NOT used at runtime. They exist solely as a single source of truth
// for test verification. A reflection-based test in defaults_test.go extracts envDefault
// tags from GatewayConfig, ServerConfig, and ProvisioningConfig and asserts they match
// these constants. This catches drift when someone updates a default in one config but
// forgets the others.
//
// Note: LogLevel, LogFormat, and Environment defaults are structurally guaranteed by
// BaseConfig embedding and are NOT tracked here.
//
// Helm overrides are unaffected — services read env vars via caarlos0/env struct tags
// as before. These constants are never referenced in production code paths.

// DefaultAuthEnabled is the authentication toggle shared across gateway and provisioning.
const DefaultAuthEnabled = "false"

// DefaultTenantID is the fallback tenant ID shared across gateway and server.
const DefaultTenantID = "sukko"

// Provisioning gRPC connection defaults shared across gateway and server.
const (
	DefaultProvisioningGRPCAddr  = "localhost:9090"
	DefaultGRPCReconnectDelay    = "1s"
	DefaultGRPCReconnectMaxDelay = "30s"
)

// HTTP timeout defaults shared across all three services.
// Gateway uses different env var names (GATEWAY_*_TIMEOUT) but the same default values.
const (
	DefaultHTTPReadTimeout  = "15s"
	DefaultHTTPWriteTimeout = "15s"
	DefaultHTTPIdleTimeout  = "60s"
)

// Kafka/Namespace defaults shared across server and provisioning.
const (
	DefaultKafkaTopicNamespaceOverride = ""
	DefaultValidNamespaces             = "local,dev,stag,prod"
)
