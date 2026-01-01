// Package gateway provides a WebSocket gateway that handles authentication
// and subscription permissions, proxying to the ws-server backend.
package gateway

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

// Config holds gateway configuration loaded from environment variables.
type Config struct {
	// Server settings
	Port         int           `env:"GATEWAY_PORT" envDefault:"3000"`
	ReadTimeout  time.Duration `env:"GATEWAY_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout time.Duration `env:"GATEWAY_WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout  time.Duration `env:"GATEWAY_IDLE_TIMEOUT" envDefault:"60s"`

	// Backend ws-server connection
	BackendURL     string        `env:"GATEWAY_BACKEND_URL" envDefault:"ws://localhost:3001/ws"`
	DialTimeout    time.Duration `env:"GATEWAY_DIAL_TIMEOUT" envDefault:"10s"`
	MessageTimeout time.Duration `env:"GATEWAY_MESSAGE_TIMEOUT" envDefault:"60s"`

	// Authentication
	AuthEnabled bool   `env:"AUTH_ENABLED" envDefault:"true"`
	JWTSecret   string `env:"JWT_SECRET"`

	// Permissions - channel patterns
	PublicPatterns      []string `env:"GATEWAY_PUBLIC_PATTERNS" envSeparator:"," envDefault:"*.trade,*.liquidity,*.metadata"`
	UserScopedPatterns  []string `env:"GATEWAY_USER_SCOPED_PATTERNS" envSeparator:"," envDefault:"balances.{principal},notifications.{principal}"`
	GroupScopedPatterns []string `env:"GATEWAY_GROUP_SCOPED_PATTERNS" envSeparator:"," envDefault:"community.{group_id},social.{group_id}"`

	// Rate limiting per principal
	RateLimitEnabled bool    `env:"GATEWAY_RATE_LIMIT_ENABLED" envDefault:"true"`
	RateLimitBurst   int     `env:"GATEWAY_RATE_LIMIT_BURST" envDefault:"100"`
	RateLimitRate    float64 `env:"GATEWAY_RATE_LIMIT_RATE" envDefault:"10.0"`

	// Logging
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// Environment
	Environment string `env:"ENVIRONMENT" envDefault:"development"`

	// NATS configuration for least-connections routing
	NATSEnabled bool     `env:"NATS_ENABLED" envDefault:"false"`
	NATSURLs    []string `env:"NATS_URLS" envSeparator:"," envDefault:"nats://localhost:4222"`

	// WS-Server pod port (used when routing directly to pods)
	WSServerPort int `env:"WS_SERVER_PORT" envDefault:"3001"`
}

// LoadConfig reads configuration from environment variables.
// Optionally loads from .env file if present.
func LoadConfig(logger *zerolog.Logger) (*Config, error) {
	// Load .env file (optional)
	if err := godotenv.Load(); err != nil {
		if logger != nil {
			logger.Debug().Msg("No .env file found (using environment variables only)")
		}
	}

	cfg := &Config{}

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks configuration for errors.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("GATEWAY_PORT must be between 1 and 65535, got %d", c.Port)
	}

	// Only require JWTSecret when auth is enabled
	if c.AuthEnabled {
		if c.JWTSecret == "" {
			return fmt.Errorf("JWT_SECRET is required when AUTH_ENABLED=true")
		}
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters for HS256 security")
		}
	}

	if c.BackendURL == "" {
		return fmt.Errorf("GATEWAY_BACKEND_URL is required")
	}

	if len(c.PublicPatterns) == 0 {
		return fmt.Errorf("GATEWAY_PUBLIC_PATTERNS must have at least one pattern")
	}

	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error (got: %s)", c.LogLevel)
	}

	validLogFormats := map[string]bool{"json": true, "text": true, "pretty": true}
	if !validLogFormats[c.LogFormat] {
		return fmt.Errorf("LOG_FORMAT must be one of: json, text, pretty (got: %s)", c.LogFormat)
	}

	return nil
}

// LogConfig logs configuration using structured logging.
func (c *Config) LogConfig(logger zerolog.Logger) {
	logger.Info().
		Str("environment", c.Environment).
		Int("port", c.Port).
		Bool("auth_enabled", c.AuthEnabled).
		Dur("read_timeout", c.ReadTimeout).
		Dur("write_timeout", c.WriteTimeout).
		Dur("idle_timeout", c.IdleTimeout).
		Str("backend_url", c.BackendURL).
		Dur("dial_timeout", c.DialTimeout).
		Strs("public_patterns", c.PublicPatterns).
		Strs("user_scoped_patterns", c.UserScopedPatterns).
		Strs("group_scoped_patterns", c.GroupScopedPatterns).
		Bool("rate_limit_enabled", c.RateLimitEnabled).
		Int("rate_limit_burst", c.RateLimitBurst).
		Float64("rate_limit_rate", c.RateLimitRate).
		Str("log_level", c.LogLevel).
		Str("log_format", c.LogFormat).
		Bool("nats_enabled", c.NATSEnabled).
		Strs("nats_urls", c.NATSURLs).
		Int("ws_server_port", c.WSServerPort).
		Msg("Gateway configuration loaded")
}
