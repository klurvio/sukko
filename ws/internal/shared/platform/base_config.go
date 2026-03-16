package platform

import (
	"errors"
	"strings"
)

// BaseConfig contains configuration fields shared across all services.
// Embed this in service-specific configs to avoid duplication.
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
