package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Config holds all configuration for the load test
type Config struct {
	// Connection
	WSURL             string
	HealthURL         string
	TargetConnections int
	RampRate          int
	SustainDuration   time.Duration
	ConnectionTimeout time.Duration

	// WebSocket ping/pong timing
	PongWait   time.Duration // Timeout for pong response
	PingPeriod time.Duration // How often to send pings

	// Subscriptions
	Channels          []string
	SubscriptionMode  string
	ChannelsPerClient int

	// Authentication
	TenantID  string
	Token     string
	JWTSecret string
	Principal string

	// Reporting
	ReportInterval time.Duration
	HealthInterval time.Duration
	LogLevel       string
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.WSURL == "" {
		return errors.New("WS_URL is required")
	}
	if c.TargetConnections < 1 {
		return fmt.Errorf("TARGET_CONNECTIONS must be >= 1, got %d", c.TargetConnections)
	}
	if c.RampRate < 1 {
		return fmt.Errorf("RAMP_RATE must be >= 1, got %d", c.RampRate)
	}

	// Validate channels are provided
	if len(c.Channels) == 0 {
		return errors.New("CHANNELS is required (at least one channel)")
	}

	// Validate channels format (must have at least 3 dot-separated parts)
	for _, ch := range c.Channels {
		parts := strings.Split(ch, ".")
		if len(parts) < 3 {
			return fmt.Errorf("channel %q must have format {tenant}.{identifier}.{category}", ch)
		}
	}

	validModes := map[string]bool{"all": true, "single": true, "random": true}
	if !validModes[c.SubscriptionMode] {
		return fmt.Errorf("SUBSCRIPTION_MODE must be all/single/random, got %s", c.SubscriptionMode)
	}

	// Validate ChannelsPerClient for random mode
	if c.SubscriptionMode == "random" {
		if c.ChannelsPerClient < 1 {
			return fmt.Errorf("CHANNELS_PER_CLIENT must be >= 1, got %d", c.ChannelsPerClient)
		}
		if c.ChannelsPerClient > len(c.Channels) {
			return fmt.Errorf("CHANNELS_PER_CLIENT (%d) cannot exceed number of channels (%d)",
				c.ChannelsPerClient, len(c.Channels))
		}
	}

	if err := validateLogLevel(c.LogLevel); err != nil {
		return err
	}

	// Validate ping/pong timing
	if c.PingPeriod >= c.PongWait {
		return fmt.Errorf("WS_PING_PERIOD (%v) must be less than WS_PONG_WAIT (%v)", c.PingPeriod, c.PongWait)
	}

	return nil
}

// validateLogLevel validates the log level string
func validateLogLevel(level string) error {
	validLevels := map[string]bool{
		"trace": true,
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
		"panic": true,
	}
	if !validLevels[level] {
		return fmt.Errorf("invalid log level %q, must be one of: trace, debug, info, warn, error, fatal, panic", level)
	}
	return nil
}

// ParseConfig parses configuration from flags and environment variables
func ParseConfig() (*Config, error) {
	cfg := &Config{}

	// Connection flags
	flag.StringVar(&cfg.WSURL, "url", getEnv("WS_URL", "ws://localhost:3000/ws"), "WebSocket URL")
	flag.StringVar(&cfg.HealthURL, "health", getEnv("HEALTH_URL", "http://localhost:3000/health"), "Health URL")
	flag.IntVar(&cfg.TargetConnections, "connections", getEnvInt("TARGET_CONNECTIONS", 1000), "Target connections")
	flag.IntVar(&cfg.RampRate, "ramp-rate", getEnvInt("RAMP_RATE", 100), "Connections per second during ramp-up")
	flag.DurationVar(&cfg.SustainDuration, "duration", getEnvDuration("DURATION", 30*time.Minute), "Sustain duration")
	flag.DurationVar(&cfg.ConnectionTimeout, "timeout", getEnvDuration("CONNECTION_TIMEOUT", 10*time.Second), "Connection timeout")

	// WebSocket ping/pong flags
	flag.DurationVar(&cfg.PongWait, "pong-wait", getEnvDuration("WS_PONG_WAIT", 120*time.Second), "Timeout for pong response")
	flag.DurationVar(&cfg.PingPeriod, "ping-period", getEnvDuration("WS_PING_PERIOD", 90*time.Second), "How often to send pings")

	// Subscription flags
	defaultChannels := "sukko.all.trade,sukko.BTC.trade,sukko.ETH.trade,sukko.SOL.trade,sukko.BTC.orderbook,sukko.ETH.liquidity"
	channelsStr := flag.String("channels", getEnv("CHANNELS", defaultChannels), "Comma-separated channels (format: tenant.identifier.category)")
	flag.StringVar(&cfg.SubscriptionMode, "mode", getEnv("SUBSCRIPTION_MODE", "random"), "Subscription mode: all/single/random")
	flag.IntVar(&cfg.ChannelsPerClient, "channels-per-client", getEnvInt("CHANNELS_PER_CLIENT", 3), "Channels per client (for random mode)")

	// Auth flags
	flag.StringVar(&cfg.TenantID, "tenant", getEnv("TENANT_ID", "sukko"), "Tenant ID for JWT token generation")
	flag.StringVar(&cfg.Token, "token", getEnv("JWT_TOKEN", ""), "Pre-generated JWT token")
	flag.StringVar(&cfg.JWTSecret, "jwt-secret", getEnv("JWT_SECRET", ""), "JWT secret to generate tokens")
	flag.StringVar(&cfg.Principal, "principal", getEnv("PRINCIPAL", "loadtest-user"), "Principal for JWT")

	// Reporting
	flag.DurationVar(&cfg.ReportInterval, "report-interval", getEnvDuration("REPORT_INTERVAL", 10*time.Second), "Stats report interval")
	flag.DurationVar(&cfg.HealthInterval, "health-interval", getEnvDuration("HEALTH_INTERVAL", 5*time.Second), "Health check interval")

	// Logging
	flag.StringVar(&cfg.LogLevel, "log-level", getEnv("LOG_LEVEL", "info"), "Log level: debug/info/warn/error")

	flag.Parse()

	// Parse comma-separated channels
	if *channelsStr != "" {
		cfg.Channels = strings.Split(*channelsStr, ",")
		for i := range cfg.Channels {
			cfg.Channels[i] = strings.TrimSpace(cfg.Channels[i])
		}
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an environment variable as an integer with a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// getEnvDuration gets an environment variable as a duration with a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// GetLogLevel returns the zerolog level for the configured log level string
func (c *Config) GetLogLevel() zerolog.Level {
	switch c.LogLevel {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}
