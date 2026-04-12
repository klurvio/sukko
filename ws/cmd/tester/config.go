package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/klurvio/sukko/internal/shared/platform"
)

// TesterConfig holds configuration for the sukko-tester service.
type TesterConfig struct {
	platform.BaseConfig
	Port              int    `env:"TESTER_PORT" envDefault:"8090"`
	AuthToken         string `env:"TESTER_AUTH_TOKEN"` // no default — must be explicitly set for production
	GatewayURL        string `env:"GATEWAY_URL" envDefault:"ws://localhost:3000"`
	ProvisioningURL   string `env:"PROVISIONING_URL" envDefault:"http://localhost:8080"`
	KafkaBrokers      string `env:"KAFKA_BROKERS" envDefault:""`
	NATSJetStreamURLs string `env:"NATS_JETSTREAM_URLS" envDefault:""`
	MessageBackend    string `env:"MESSAGE_BACKEND" envDefault:"direct"`

	// License reload suite — Ed25519 private key for signing test license keys
	SigningKeyFile string `env:"TESTER_SIGNING_KEY_FILE" envDefault:""`

	// JWT auth configuration
	JWTLifetime      time.Duration `env:"TESTER_JWT_LIFETIME" envDefault:"15m"`
	JWTRefreshBefore time.Duration `env:"TESTER_JWT_REFRESH_BEFORE" envDefault:"2m"`
	KeyExpiry        time.Duration `env:"TESTER_KEY_EXPIRY" envDefault:"24h"`
}

func (c *TesterConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return fmt.Errorf("base config: %w", err)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("TESTER_PORT must be between 1 and 65535, got %d", c.Port)
	}
	if c.GatewayURL == "" {
		return errors.New("GATEWAY_URL is required")
	}
	if c.ProvisioningURL == "" {
		return errors.New("PROVISIONING_URL is required")
	}
	switch c.MessageBackend {
	case "direct", "kafka", "nats":
		// valid
	default:
		return fmt.Errorf("MESSAGE_BACKEND must be 'direct', 'kafka', or 'nats', got %q", c.MessageBackend)
	}
	if c.MessageBackend == "kafka" && c.KafkaBrokers == "" {
		return errors.New("KAFKA_BROKERS required when MESSAGE_BACKEND=kafka")
	}
	if c.MessageBackend == "nats" && c.NATSJetStreamURLs == "" {
		return errors.New("NATS_JETSTREAM_URLS required when MESSAGE_BACKEND=nats")
	}
	if c.JWTLifetime <= 0 {
		return errors.New("TESTER_JWT_LIFETIME must be positive")
	}
	if c.JWTRefreshBefore <= 0 || c.JWTRefreshBefore >= c.JWTLifetime {
		return fmt.Errorf("TESTER_JWT_REFRESH_BEFORE must be positive and less than TESTER_JWT_LIFETIME (%v), got %v", c.JWTLifetime, c.JWTRefreshBefore)
	}
	if c.KeyExpiry < c.JWTLifetime {
		return fmt.Errorf("TESTER_KEY_EXPIRY (%v) must be >= TESTER_JWT_LIFETIME (%v)", c.KeyExpiry, c.JWTLifetime)
	}
	return nil
}
