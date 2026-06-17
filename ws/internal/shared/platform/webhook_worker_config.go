package platform

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// WebhookWorkerConfig holds all configuration for the webhook-worker service.
// It embeds BaseConfig (shared logging/env/license/OTEL/pprof fields) and
// CredentialsConfig (shared AES-256 encryption key).
type WebhookWorkerConfig struct {
	BaseConfig
	CredentialsConfig

	// HTTP server (health + metrics endpoints)
	Port int `env:"WEBHOOK_HTTP_PORT" envDefault:"8083"`

	// WebhookWorkerGRPCAddr is the gRPC address of the WebhookWorkerService endpoint
	// on the provisioning server (WEBHOOK_WORKER_GRPC_PORT, default :9091).
	// Distinct from PROVISIONING_GRPC_ADDR (:9090) used by ws-server and ws-gateway
	// for ProvisioningInternalService.
	WebhookWorkerGRPCAddr string        `env:"WEBHOOK_WORKER_PROVISIONING_GRPC_ADDR"  envDefault:"localhost:9091"`
	GRPCReconnectDelay    time.Duration `env:"PROVISIONING_GRPC_RECONNECT_DELAY"      envDefault:"1s"`
	GRPCReconnectMaxDelay time.Duration `env:"PROVISIONING_GRPC_RECONNECT_MAX_DELAY"  envDefault:"30s"`

	// Worker pool
	WorkerConcurrency int           `env:"WEBHOOK_WORKER_CONCURRENCY" envDefault:"50"`
	RetryQueueSize    int           `env:"WEBHOOK_RETRY_QUEUE_SIZE"   envDefault:"10000"`
	DeliveryTimeout   time.Duration `env:"WEBHOOK_DELIVERY_TIMEOUT"   envDefault:"10s"`
	CacheTTL          time.Duration `env:"WEBHOOK_CACHE_TTL"          envDefault:"30s"`

	// WebhookAllowHTTP allows http:// URLs for local dev/test only.
	// Must match ProvisioningConfig.WebhookAllowHTTP (§XVIII consistency).
	WebhookAllowHTTP bool `env:"WEBHOOK_ALLOW_HTTP" envDefault:"false"`

	// ValkeyConfig is the Valkey broadcast bus connection for this service.
	// envPrefix:"WEBHOOK_WORKER_" produces WEBHOOK_WORKER_VALKEY_ADDRS etc.,
	// consistent with PROVISIONING_VALKEY_ADDRS naming pattern (§XVIII).
	ValkeyConfig ValkeyClientConfig `envPrefix:"WEBHOOK_WORKER_"`

	// InternalToken is the shared secret for webhook-worker → provisioning gRPC auth.
	// Both services must be deployed with the same value.
	InternalToken string `env:"WEBHOOK_INTERNAL_TOKEN" redact:"true"`
}

// Validate checks all WebhookWorkerConfig bounds and required fields.
func (c *WebhookWorkerConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return err
	}
	if c.WorkerConcurrency < 1 || c.WorkerConcurrency > 500 {
		return fmt.Errorf("WEBHOOK_WORKER_CONCURRENCY must be between 1 and 500, got %d", c.WorkerConcurrency)
	}
	if c.RetryQueueSize < 100 || c.RetryQueueSize > 100_000 {
		return fmt.Errorf("WEBHOOK_RETRY_QUEUE_SIZE must be between 100 and 100000, got %d", c.RetryQueueSize)
	}
	if c.DeliveryTimeout < time.Second || c.DeliveryTimeout > 30*time.Second {
		return fmt.Errorf("WEBHOOK_DELIVERY_TIMEOUT must be between 1s and 30s, got %s", c.DeliveryTimeout)
	}
	if c.CacheTTL < 5*time.Second || c.CacheTTL > 5*time.Minute {
		return fmt.Errorf("WEBHOOK_CACHE_TTL must be between 5s and 5m, got %s", c.CacheTTL)
	}
	if c.Port < 1 || c.Port > MaxPort {
		return fmt.Errorf("WEBHOOK_HTTP_PORT must be between 1 and %d, got %d", MaxPort, c.Port)
	}
	if c.WebhookAllowHTTP && c.Environment != "local" {
		return fmt.Errorf("WEBHOOK_ALLOW_HTTP is not permitted outside the local environment (current ENVIRONMENT=%s)", c.Environment)
	}
	if err := c.CredentialsConfig.Validate(); err != nil {
		return err
	}
	if c.InternalToken == "" {
		return errors.New("WEBHOOK_INTERNAL_TOKEN is required")
	}
	if len(c.InternalToken) < 32 {
		return errors.New("WEBHOOK_INTERNAL_TOKEN must be at least 32 characters")
	}
	if !slices.ContainsFunc(c.ValkeyConfig.Addrs, func(s string) bool { return strings.TrimSpace(s) != "" }) {
		return errors.New("WEBHOOK_WORKER_VALKEY_ADDRS is required and must contain at least one non-empty address")
	}
	if c.WebhookWorkerGRPCAddr == "" {
		return errors.New("WEBHOOK_WORKER_PROVISIONING_GRPC_ADDR is required")
	}
	if c.GRPCReconnectDelay < 100*time.Millisecond {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_DELAY must be >= 100ms, got %s", c.GRPCReconnectDelay)
	}
	if c.GRPCReconnectMaxDelay < c.GRPCReconnectDelay {
		return fmt.Errorf("PROVISIONING_GRPC_RECONNECT_MAX_DELAY (%s) must be >= PROVISIONING_GRPC_RECONNECT_DELAY (%s)", c.GRPCReconnectMaxDelay, c.GRPCReconnectDelay)
	}
	return nil
}

// LoadWebhookWorkerConfig loads configuration from environment and .env file.
// Follows the same pattern as LoadProvisioningConfig, LoadServerConfig, LoadGatewayConfig:
// godotenv.Load() first (no-op if no .env file), then env.Parse, then Validate.
func LoadWebhookWorkerConfig() (*WebhookWorkerConfig, error) {
	_ = godotenv.Load()
	cfg := &WebhookWorkerConfig{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}
