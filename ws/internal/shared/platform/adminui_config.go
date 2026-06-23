package platform

import "time"

// AdminUIConfig holds Admin UI session-cookie configuration.
// Embedded anonymously in ProvisioningConfig — fields are promoted:
// use cfg.AdminUIEnabled, cfg.AdminToken, etc.
type AdminUIConfig struct {
	AdminUIEnabled  bool          `env:"ADMIN_UI_ENABLED" envDefault:"false"`
	AdminToken      string        `env:"ADMIN_TOKEN"      redact:"true"` // no envDefault; required when UI enabled; min 32 chars
	AdminSessionTTL time.Duration `env:"ADMIN_SESSION_TTL" envDefault:"8h"`
	// AdminOIDCIssuer is reserved; activating it returns a startup error.
	AdminOIDCIssuer string `env:"ADMIN_OIDC_ISSUER"`
	// WSServerHealthURL and GatewayHealthURL are fanned out to by GET /admin/system/health.
	// Empty → "unconfigured" in health response; no envDefault.
	WSServerHealthURL string `env:"WS_SERVER_HEALTH_URL"`
	GatewayHealthURL  string `env:"GATEWAY_HEALTH_URL"`
}
