package auth

import (
	"context"
	"errors"
	"fmt"
)

// SetupAPIKeyOnly initializes auth state for api-key mode:
// tenant lookup only (no tenant creation, no JWT keypair registration).
// Returns a SetupResult with Minter=nil and TokenFunc=nil.
// All code paths that call TokenFunc or dereference Minter MUST nil-check first.
func SetupAPIKeyOnly(ctx context.Context, cfg SetupConfig) (*SetupResult, error) {
	if cfg.TenantID == "" {
		return nil, errors.New("SetupAPIKeyOnly: TenantID is required (set tenant_id in the test request; api-key mode does not create throwaway tenants)")
	}

	authProvider := cfg.AdminProvider
	if authProvider == nil {
		if cfg.RequireAdminProvider {
			return nil, errors.New("SetupAPIKeyOnly: AdminProvider is nil but RequireAdminProvider is true (remote mode requires TESTER_ADMIN_KEY_FILE)")
		}
		ephemeral, _, err := NewEphemeralAuthProvider()
		if err != nil {
			return nil, fmt.Errorf("SetupAPIKeyOnly: generate ephemeral provider: %w", err)
		}
		authProvider = ephemeral
	}

	provClient := NewProvisioningClient(cfg.ProvisioningURL, authProvider, cfg.Logger)

	return &SetupResult{
		TenantID:      cfg.TenantID,
		Minter:        nil, // no JWT keypair in api-key mode
		TokenFunc:     nil, // no JWT tokens in api-key mode
		ProvClient:    provClient,
		Cleanup:       func(_ context.Context) {}, // nothing to revoke
		AdminProvider: authProvider,
	}, nil
}
