package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// defaultKeyExpiry is the default expiration for registered test keys.
// Acts as a crash safety net — if the tester terminates abnormally,
// orphaned keys auto-expire rather than persisting indefinitely.
const defaultKeyExpiry = 24 * time.Hour

// SetupConfig configures the auth setup for a test run.
type SetupConfig struct {
	// TestID is the unique test run identifier (used for key ID and throwaway tenant ID).
	TestID string

	// TenantID is the target tenant. If empty, a throwaway tenant is created.
	TenantID string

	// ProvisioningURL is the base URL of the provisioning API.
	ProvisioningURL string

	// AdminToken is the admin token for provisioning API management operations.
	AdminToken string

	// JWTLifetime is the JWT expiration duration.
	JWTLifetime time.Duration

	// KeyExpiry is the registered key expiration (crash safety net). Defaults to 24h.
	KeyExpiry time.Duration

	// Logger for structured logging.
	Logger zerolog.Logger
}

// SetupResult contains the resolved auth state for a test run.
type SetupResult struct {
	// TenantID is the resolved tenant (provided or auto-created).
	TenantID string

	// Minter creates signed JWTs for this test run.
	Minter *Minter

	// TokenFunc returns a signed JWT for the given connection index.
	TokenFunc func(int) string

	// ProvClient is the provisioning API client (exposed for validate:auth suite).
	ProvClient *ProvisioningClient

	// Cleanup revokes the test key and deletes the throwaway tenant (if created).
	// Idempotent — logs errors but does not fail.
	Cleanup func(ctx context.Context)
}

// Setup orchestrates the full auth setup for a test run:
// 1. Resolve tenant (create throwaway if TenantID is empty)
// 2. Generate ES256 keypair
// 3. Register public key with provisioning (with expires_at safety net)
// 4. Create JWT minter
// 5. Return SetupResult with cleanup function
func Setup(ctx context.Context, cfg SetupConfig) (*SetupResult, error) {
	keyExpiry := cfg.KeyExpiry
	if keyExpiry <= 0 {
		keyExpiry = defaultKeyExpiry
	}

	logger := cfg.Logger.With().Str("test_id", cfg.TestID).Logger()

	provClient := NewProvisioningClient(cfg.ProvisioningURL, cfg.AdminToken, logger)

	// Step 1: Resolve tenant
	tenantID := cfg.TenantID
	createdTenant := false
	if tenantID == "" {
		tenantID = "tester-" + cfg.TestID
		if err := provClient.CreateTenant(ctx, tenantID, "Tester auto-created tenant"); err != nil {
			return nil, fmt.Errorf("auth setup: create tenant: %w", err)
		}
		createdTenant = true
		logger.Info().Str("tenant_id", tenantID).Msg("throwaway tenant created")
	}

	// Step 2: Generate keypair
	keypair, err := GenerateKeypair(cfg.TestID)
	if err != nil {
		if createdTenant {
			cleanupTenant(context.Background(), provClient, tenantID, logger)
		}
		return nil, fmt.Errorf("auth setup: generate keypair: %w", err)
	}

	// Step 3: Register public key
	expiresAt := time.Now().Add(keyExpiry)
	if err := provClient.RegisterKey(ctx, tenantID, RegisterKeyRequest{
		KeyID:     keypair.KeyID,
		Algorithm: "ES256",
		PublicKey: keypair.PublicPEM,
		ExpiresAt: &expiresAt,
	}); err != nil {
		if createdTenant {
			cleanupTenant(context.Background(), provClient, tenantID, logger)
		}
		return nil, fmt.Errorf("auth setup: register key: %w", err)
	}

	// Step 4: Create minter
	minter := NewMinter(MinterConfig{
		Keypair:  keypair,
		TenantID: tenantID,
		Lifetime: cfg.JWTLifetime,
	})

	logger.Info().
		Str("tenant_id", tenantID).
		Str("key_id", keypair.KeyID).
		Bool("throwaway", createdTenant).
		Time("key_expires_at", expiresAt).
		Msg("auth setup complete")

	// Step 5: Build cleanup function
	cleanup := func(ctx context.Context) {
		// Revoke key (best-effort)
		if err := provClient.RevokeKey(ctx, tenantID, keypair.KeyID); err != nil {
			logger.Warn().Err(err).Str("key_id", keypair.KeyID).Msg("cleanup: failed to revoke key")
		}
		// Delete throwaway tenant (best-effort)
		if createdTenant {
			cleanupTenant(ctx, provClient, tenantID, logger)
		}
	}

	return &SetupResult{
		TenantID:   tenantID,
		Minter:     minter,
		TokenFunc:  minter.TokenFunc(),
		ProvClient: provClient,
		Cleanup:    cleanup,
	}, nil
}

func cleanupTenant(ctx context.Context, client *ProvisioningClient, tenantID string, logger zerolog.Logger) {
	if err := client.DeleteTenant(ctx, tenantID); err != nil {
		logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("cleanup: failed to delete tenant")
	}
}
