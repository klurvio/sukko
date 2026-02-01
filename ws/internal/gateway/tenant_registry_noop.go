package gateway

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// NoopTenantRegistry provides fallback when database is unavailable.
// Used for graceful degradation - OIDC multi-issuer disabled, tenant-signed JWTs only.
type NoopTenantRegistry struct {
	logger zerolog.Logger
}

// NewNoopTenantRegistry creates a noop registry that always returns not-found errors.
// This enables graceful degradation when the database is unavailable.
func NewNoopTenantRegistry(logger zerolog.Logger) *NoopTenantRegistry {
	logger.Warn().Msg("Using NoopTenantRegistry - OIDC multi-issuer disabled")
	return &NoopTenantRegistry{logger: logger}
}

// GetTenantByIssuer always returns ErrIssuerNotFound.
// This forces fallback to tenant-signed JWT validation.
func (n *NoopTenantRegistry) GetTenantByIssuer(_ context.Context, issuerURL string) (string, error) {
	n.logger.Debug().
		Str("issuer_url", issuerURL).
		Msg("NoopTenantRegistry: issuer lookup returning not found")
	return "", types.ErrIssuerNotFound
}

// GetOIDCConfig always returns ErrOIDCNotConfigured.
func (n *NoopTenantRegistry) GetOIDCConfig(_ context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	n.logger.Debug().
		Str("tenant_id", tenantID).
		Msg("NoopTenantRegistry: OIDC config lookup returning not configured")
	return nil, types.ErrOIDCNotConfigured
}

// GetChannelRules always returns ErrChannelRulesNotFound.
// This forces fallback to default/global channel rules.
func (n *NoopTenantRegistry) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	n.logger.Debug().
		Str("tenant_id", tenantID).
		Msg("NoopTenantRegistry: channel rules lookup returning not found")
	return nil, types.ErrChannelRulesNotFound
}

// Close is a no-op for NoopTenantRegistry.
func (n *NoopTenantRegistry) Close() error {
	return nil
}

// Compile-time check that NoopTenantRegistry implements TenantRegistry.
var _ TenantRegistry = (*NoopTenantRegistry)(nil)
