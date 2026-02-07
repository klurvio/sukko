package main

import (
	"errors"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
)

// TimingMode defines how messages are scheduled.
type TimingMode string

const (
	TimingModePoisson TimingMode = "poisson"
	TimingModeUniform TimingMode = "uniform"
	TimingModeBurst   TimingMode = "burst"
)

// Config holds all configuration for wspublisher.
type Config struct {
	// Kafka settings
	KafkaBrokers   string `env:"KAFKA_BROKERS,required"`
	KafkaNamespace string `env:"KAFKA_NAMESPACE,required"` // local, dev, stag, prod

	// Channel settings - follows asyncapi spec: {tenant}.{identifier}.{category}
	// Similar to wsloadtest CHANNELS format
	Channels string `env:"CHANNELS"` // Static channel list (comma-separated), e.g., "odin.BTC.trade,odin.ETH.trade"

	// Dynamic channel generation (used if CHANNELS is empty)
	ChannelPattern string `env:"CHANNEL_PATTERN" envDefault:"{tenant}.{identifier}.{category}"` // Pattern for channels
	TenantID       string `env:"TENANT_ID" envDefault:"odin"`
	Identifiers    string `env:"IDENTIFIERS" envDefault:"BTC,ETH,SOL,all"` // Token symbols or "all" for aggregate
	Categories     string `env:"CATEGORIES" envDefault:"trade,liquidity,orderbook"` // Event categories (synced with wsloadtest)

	// Timing settings
	TimingMode    TimingMode    `env:"TIMING_MODE" envDefault:"poisson"`
	PoissonLambda float64       `env:"POISSON_LAMBDA" envDefault:"100"` // Events/second
	MinInterval   time.Duration `env:"MIN_INTERVAL" envDefault:"10ms"`
	MaxInterval   time.Duration `env:"MAX_INTERVAL" envDefault:"1s"`
	BurstCount    int           `env:"BURST_COUNT" envDefault:"10"`
	BurstPause    time.Duration `env:"BURST_PAUSE" envDefault:"1s"`

	// Runtime settings
	Duration time.Duration `env:"DURATION" envDefault:"0"` // 0 = infinite
	LogLevel string        `env:"LOG_LEVEL" envDefault:"info"`

	// SASL settings
	KafkaSASLMechanism string `env:"KAFKA_SASL_MECHANISM"`
	KafkaSASLUsername  string `env:"KAFKA_SASL_USERNAME"`
	KafkaSASLPassword  string `env:"KAFKA_SASL_PASSWORD"`
	KafkaTLSEnabled    bool   `env:"KAFKA_TLS_ENABLED" envDefault:"false"`

	// Safety settings
	AllowProd bool `env:"ALLOW_PROD" envDefault:"false"`
}

// ParseConfig loads configuration from environment variables.
func ParseConfig() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	// Check namespace safety
	ns := strings.ToLower(c.KafkaNamespace)
	if ns == "prod" || ns == "production" {
		if !c.AllowProd {
			return errors.New("refusing to run against production namespace without ALLOW_PROD=true")
		}
	}

	// Validate namespace is one of the allowed values
	validNamespaces := map[string]bool{"local": true, "dev": true, "stag": true, "prod": true}
	if !validNamespaces[ns] {
		return errors.New("KAFKA_NAMESPACE must be one of: local, dev, stag, prod")
	}

	// Check timing mode
	switch c.TimingMode {
	case TimingModePoisson:
		if c.PoissonLambda <= 0 {
			return errors.New("POISSON_LAMBDA must be positive")
		}
	case TimingModeUniform:
		if c.MinInterval <= 0 {
			return errors.New("MIN_INTERVAL must be positive")
		}
		if c.MaxInterval < c.MinInterval {
			return errors.New("MAX_INTERVAL must be >= MIN_INTERVAL")
		}
	case TimingModeBurst:
		if c.BurstCount <= 0 {
			return errors.New("BURST_COUNT must be positive")
		}
		if c.BurstPause <= 0 {
			return errors.New("BURST_PAUSE must be positive")
		}
	default:
		return errors.New("invalid TIMING_MODE: must be poisson, uniform, or burst")
	}

	// Need either static channels or dynamic generation components
	if c.Channels == "" {
		if c.Identifiers == "" {
			return errors.New("either CHANNELS or IDENTIFIERS must be set")
		}
		if c.Categories == "" {
			return errors.New("either CHANNELS or CATEGORIES must be set")
		}
	}

	// Validate channel format if static channels provided
	if c.Channels != "" {
		for _, ch := range c.GetChannels() {
			parts := strings.Split(ch, ".")
			if len(parts) < 3 {
				return errors.New("channel must have format {tenant}.{identifier}.{category}: " + ch)
			}
		}
	}

	// Check SASL consistency
	if c.KafkaSASLMechanism != "" {
		if c.KafkaSASLUsername == "" || c.KafkaSASLPassword == "" {
			return errors.New("KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD required when KAFKA_SASL_MECHANISM is set")
		}
		mechanism := strings.ToLower(c.KafkaSASLMechanism)
		if mechanism != "scram-sha-256" && mechanism != "scram-sha-512" {
			return errors.New("KAFKA_SASL_MECHANISM must be scram-sha-256 or scram-sha-512")
		}
	}

	return nil
}

// GetBrokers returns the list of Kafka brokers.
func (c *Config) GetBrokers() []string {
	return splitAndTrim(c.KafkaBrokers)
}

// GetChannels returns the list of static channels.
// Channel format: {tenant}.{identifier}.{category}
func (c *Config) GetChannels() []string {
	if c.Channels == "" {
		return nil
	}
	return splitAndTrim(c.Channels)
}

// GetIdentifiers returns the list of identifiers (e.g., BTC, ETH, all).
func (c *Config) GetIdentifiers() []string {
	return splitAndTrim(c.Identifiers)
}

// GetCategories returns the list of categories (e.g., trade, liquidity).
func (c *Config) GetCategories() []string {
	return splitAndTrim(c.Categories)
}

// splitAndTrim splits a comma-separated string and trims whitespace.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
