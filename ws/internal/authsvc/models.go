// Package authservice provides the standalone auth service for multi-tenant JWT token issuance.
package authsvc

import "github.com/adred-codev/odin-ws/internal/version"

// Tenant represents an organization/company using the platform.
type Tenant struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
	Apps []App  `json:"apps" yaml:"apps"`
}

// App represents an application belonging to a tenant.
type App struct {
	ID        string `json:"id" yaml:"id"`
	TenantID  string `json:"tenant_id"` // Set at runtime from parent tenant
	Name      string `json:"name" yaml:"name"`
	SecretEnv string `json:"secret_env" yaml:"secret_env"` // Env var name containing the secret
	secret    string // Loaded at runtime from env var
}

// Secret returns the app's secret (loaded from env var at startup).
func (a *App) Secret() string {
	return a.secret
}

// SetSecret sets the app's secret (used during config loading).
func (a *App) SetSecret(s string) {
	a.secret = s
}

// TokenRequest represents a request to issue a token.
type TokenRequest struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

// TokenResponse represents the response from token issuance.
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	TenantID  string `json:"tenant_id"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status  string       `json:"status"`
	Version version.Info `json:"version"`
}
