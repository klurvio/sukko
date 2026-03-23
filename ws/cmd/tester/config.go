package main

import (
	"fmt"

	"github.com/klurvio/sukko/internal/shared/platform"
)

type TesterConfig struct {
	platform.BaseConfig
	Port            int    `env:"TESTER_PORT" envDefault:"8090"`
	AuthToken       string `env:"TESTER_AUTH_TOKEN"` // no default — must be explicitly set for production
	GatewayURL      string `env:"GATEWAY_URL" envDefault:"ws://localhost:3000"`
	ProvisioningURL string `env:"PROVISIONING_URL" envDefault:"http://localhost:8080"`
	KafkaBrokers    string `env:"KAFKA_BROKERS" envDefault:""`
	MessageBackend  string `env:"MESSAGE_BACKEND" envDefault:"direct"`
}

func (c *TesterConfig) Validate() error {
	if err := c.BaseConfig.Validate(); err != nil {
		return err
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("TESTER_PORT must be between 1 and 65535, got %d", c.Port)
	}
	if c.GatewayURL == "" {
		return fmt.Errorf("GATEWAY_URL is required")
	}
	if c.ProvisioningURL == "" {
		return fmt.Errorf("PROVISIONING_URL is required")
	}
	if c.MessageBackend != "direct" && c.MessageBackend != "kafka" {
		return fmt.Errorf("MESSAGE_BACKEND must be 'direct' or 'kafka', got %q", c.MessageBackend)
	}
	if c.MessageBackend == "kafka" && c.KafkaBrokers == "" {
		return fmt.Errorf("KAFKA_BROKERS required when MESSAGE_BACKEND=kafka")
	}
	return nil
}
