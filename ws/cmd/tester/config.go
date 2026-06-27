package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	testerapi "github.com/klurvio/sukko/cmd/tester/api"
	testerauth "github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/runner"
	"github.com/klurvio/sukko/internal/shared/platform"
)

// TesterConfig holds configuration for the sukko-tester service.
type TesterConfig struct {
	platform.BaseConfig
	platform.MessageBackendBase
	Port            int    `env:"TESTER_PORT" envDefault:"8090"`
	AuthToken       string `env:"TESTER_AUTH_TOKEN"` // no default — must be explicitly set for production
	GatewayURL      string `env:"GATEWAY_URL" envDefault:"ws://localhost:3000"`
	ProvisioningURL string `env:"PROVISIONING_URL" envDefault:"http://localhost:8080"`

	// License reload suite — Ed25519 private key for signing test license keys
	SigningKeyFile string `env:"TESTER_SIGNING_KEY_FILE" envDefault:""`

	// Admin keypair for remote mode (required when targeting a deployed provisioning service).
	// When unset, the tester generates an ephemeral keypair per test run (local dev mode only).
	AdminKeyFile string `env:"TESTER_ADMIN_KEY_FILE" envDefault:""`
	// AdminKeyID is the kid embedded in admin JWTs; must match BootstrapAdminKeyID unless a
	// custom key was registered with provisioning under a different ID.
	// envDefault must stay in sync with provauth.BootstrapAdminKeyID; enforced by TestTesterConfig_DefaultAdminKeyID_EqualsBootstrapConstant.
	AdminKeyID string `env:"TESTER_ADMIN_KEY_ID" envDefault:"bootstrap-0"`

	// JWT auth configuration
	JWTLifetime      time.Duration `env:"TESTER_JWT_LIFETIME" envDefault:"15m"`
	JWTRefreshBefore time.Duration `env:"TESTER_JWT_REFRESH_BEFORE" envDefault:"2m"`
	KeyExpiry        time.Duration `env:"TESTER_KEY_EXPIRY" envDefault:"24h"`

	// Auth mode configuration (FR-001, FR-002, SC-001)
	// AuthMode controls the credential mode for connections: jwt, api-key, upgrade, or mixed.
	AuthMode runner.AuthMode `env:"TESTER_AUTH_MODE" envDefault:"jwt"`
	// APIKey is a static pre-provisioned API key for api-key, upgrade, and mixed modes.
	// Tagged redact:"true" so the /config endpoint never echoes it (§IX).
	APIKey             string        `env:"TESTER_API_KEY" envDefault:"" redact:"true"`
	AuthMixRatio       float64       `env:"TESTER_AUTH_MIX_RATIO" envDefault:"0.5"`
	AuthUpgradeTimeout time.Duration `env:"TESTER_AUTH_UPGRADE_TIMEOUT" envDefault:"10s"`

	// Gateway metrics scraping for revocation load suites.
	// GatewayMetricsURL is the Prometheus metrics endpoint of the gateway pod.
	// When empty, metric-drift checks are skipped (not an error — recorded as skipped_checks).
	GatewayMetricsURL      string        `env:"TESTER_GATEWAY_METRICS_URL" envDefault:""`
	GatewayMetricsInterval time.Duration `env:"TESTER_GATEWAY_METRICS_INTERVAL" envDefault:"30s"`
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
	case platform.MessageBackendDirect, platform.MessageBackendKafka:
		// valid
	default:
		return fmt.Errorf("MESSAGE_BACKEND must be 'direct' or 'kafka', got %q", c.MessageBackend)
	}
	if c.MessageBackend == platform.MessageBackendKafka && c.KafkaBrokers == "" {
		return errors.New("KAFKA_BROKERS required when MESSAGE_BACKEND=kafka")
	}
	if c.AdminKeyFile != "" {
		if _, err := testerauth.LoadEd25519PrivateKey(c.AdminKeyFile); err != nil {
			return fmt.Errorf("TESTER_ADMIN_KEY_FILE: %w", err)
		}
		if c.AdminKeyID == "" {
			return errors.New("TESTER_ADMIN_KEY_ID must not be empty when TESTER_ADMIN_KEY_FILE is set")
		}
		if err := testerapi.ValidateAdminKeyID(c.AdminKeyID); err != nil {
			return fmt.Errorf("TESTER_ADMIN_KEY_ID: %w", err)
		}
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
	// Auth mode validation
	switch c.AuthMode {
	case runner.AuthModeJWT, runner.AuthModeAPIKey, runner.AuthModeUpgrade, runner.AuthModeMixed:
		// valid
	default:
		return fmt.Errorf("TESTER_AUTH_MODE must be jwt|api-key|upgrade|mixed, got %q", c.AuthMode)
	}
	if c.AuthMixRatio < runner.AuthMixRatioMin || c.AuthMixRatio > runner.AuthMixRatioMax {
		return fmt.Errorf("TESTER_AUTH_MIX_RATIO must be in [%.1f, %.1f], got %v", runner.AuthMixRatioMin, runner.AuthMixRatioMax, c.AuthMixRatio)
	}
	if c.AuthUpgradeTimeout <= 0 || c.AuthUpgradeTimeout > 60*time.Second {
		return fmt.Errorf("TESTER_AUTH_UPGRADE_TIMEOUT must be > 0 and ≤ 60s, got %v", c.AuthUpgradeTimeout)
	}
	if (c.AuthMode == runner.AuthModeAPIKey || c.AuthMode == runner.AuthModeUpgrade) && c.APIKey == "" {
		return fmt.Errorf("TESTER_API_KEY is required when TESTER_AUTH_MODE is %q", c.AuthMode)
	}
	// GatewayMetricsInterval must be positive unconditionally — time.NewTicker(0) panics.
	if c.GatewayMetricsInterval <= 0 {
		return fmt.Errorf("TESTER_GATEWAY_METRICS_INTERVAL must be positive, got %v", c.GatewayMetricsInterval)
	}
	if c.GatewayMetricsURL != "" {
		if !strings.HasPrefix(c.GatewayMetricsURL, "http://") && !strings.HasPrefix(c.GatewayMetricsURL, "https://") {
			return fmt.Errorf("TESTER_GATEWAY_METRICS_URL must start with http:// or https://, got %q", c.GatewayMetricsURL)
		}
	}
	return nil
}
