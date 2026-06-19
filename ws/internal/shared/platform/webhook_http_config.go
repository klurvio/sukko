package platform

// WebhookHTTPConfig holds the WEBHOOK_ALLOW_HTTP setting shared by both
// WebhookWorkerConfig and ProvisioningConfig.
// Embed in both to eliminate the duplicate inline field (§X).
//
// Validate() is zero-argument — a bool field has no invalid state.
// The cross-field check (WebhookAllowHTTP && Environment != "local")
// belongs in the top-level Validate() of each embedding config,
// where c.Environment is available without a parameter.
type WebhookHTTPConfig struct {
	// WebhookAllowHTTP allows http:// webhook URLs for local dev/testing.
	// Must be false in production (enforced by top-level Validate()).
	WebhookAllowHTTP bool `env:"WEBHOOK_ALLOW_HTTP" envDefault:"false"`
}

// Validate satisfies the shared config struct convention.
// The cross-field guard lives in the embedding config's Validate():
//
//	if c.WebhookAllowHTTP && c.Environment != "local" { return error }
func (c WebhookHTTPConfig) Validate() error {
	return nil
}
