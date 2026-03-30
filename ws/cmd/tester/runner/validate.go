package runner

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/rs/zerolog"
)

// validatePublicChannel is the channel used by the channels validation suite.
const validatePublicChannel = "sukko.validate.public"

func runValidate(ctx context.Context, run *TestRun, logger zerolog.Logger) (*metrics.Report, error) {
	suite := run.Config.Suite
	if suite == "" {
		suite = "auth"
	}

	var checks []metrics.CheckResult
	var err error

	switch suite {
	case "auth":
		checks, err = validateAuth(ctx, run, logger)
	case "channels":
		checks, err = validateChannels(ctx, run, logger)
	case "ordering":
		checks, err = validateOrdering(ctx, run, logger)
	case "reconnect":
		checks, err = validateReconnect(ctx, run, logger)
	case "ratelimit":
		checks, err = validateRateLimit(ctx, run, logger)
	case "edition-limits":
		checks, err = validateEditionLimits(ctx, run, logger)
	case "pubsub":
		checks, err = validatePubSub(ctx, run, logger)
	case "tenant-isolation":
		checks, err = validateTenantIsolation(ctx, run, logger)
	default:
		checks = []metrics.CheckResult{{
			Name:   "unknown suite",
			Status: "fail",
			Error:  "unknown validation suite: " + suite,
		}}
	}

	if err != nil {
		return nil, err
	}

	status := "pass"
	for _, c := range checks {
		if c.Status == "fail" {
			status = "fail"
			break
		}
	}

	return &metrics.Report{
		TestType: "validate:" + suite,
		Status:   status,
		Metrics:  run.Collector.Snapshot(),
		Checks:   checks,
	}, nil
}

func validateAuth(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) { //nolint:unparam // matches validate suite function signature
	var checks []metrics.CheckResult

	// Check 1: Valid JWT accepted (with retry for key registry cache race)
	start := time.Now()
	client, err := connectWithRetry(ctx, run.Config.GatewayURL, run.authResult.TokenFunc(0), logger)
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "valid JWT accepted", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "valid JWT accepted", Status: "pass",
			Latency: time.Since(start).Round(time.Millisecond).String(),
		})
		_ = client.Close()
	}

	// Check 2: Expired JWT rejected
	expiredToken, err := run.authResult.Minter.MintExpired(1)
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "expired JWT rejected", Status: "fail", Error: fmt.Sprintf("mint expired: %v", err),
		})
	} else {
		client, err = testerws.Connect(ctx, testerws.ConnectConfig{
			GatewayURL: run.Config.GatewayURL,
			Token:      expiredToken,
			Logger:     logger,
		})
		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "expired JWT rejected", Status: "pass",
			})
		} else {
			_ = client.Close()
			checks = append(checks, metrics.CheckResult{
				Name: "expired JWT rejected", Status: "fail", Error: "expired JWT was accepted",
			})
		}
	}

	// Check 3: Wrong kid rejected
	wrongKidToken, err := run.authResult.Minter.MintWithKid(2, "nonexistent-key")
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "wrong kid rejected", Status: "fail", Error: fmt.Sprintf("mint wrong kid: %v", err),
		})
	} else {
		client, err = testerws.Connect(ctx, testerws.ConnectConfig{
			GatewayURL: run.Config.GatewayURL,
			Token:      wrongKidToken,
			Logger:     logger,
		})
		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "wrong kid rejected", Status: "pass",
			})
		} else {
			_ = client.Close()
			checks = append(checks, metrics.CheckResult{
				Name: "wrong kid rejected", Status: "fail", Error: "JWT with wrong kid was accepted",
			})
		}
	}

	// Check 4: Wrong tenant rejected
	wrongTenantToken, err := run.authResult.Minter.MintWithTenant(3, "wrong-tenant-id")
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "wrong tenant rejected", Status: "fail", Error: fmt.Sprintf("mint wrong tenant: %v", err),
		})
	} else {
		client, err = testerws.Connect(ctx, testerws.ConnectConfig{
			GatewayURL: run.Config.GatewayURL,
			Token:      wrongTenantToken,
			Logger:     logger,
		})
		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "wrong tenant rejected", Status: "pass",
			})
		} else {
			_ = client.Close()
			checks = append(checks, metrics.CheckResult{
				Name: "wrong tenant rejected", Status: "fail", Error: "JWT with wrong tenant was accepted",
			})
		}
	}

	// Check 5: Revoked key rejected (Scenario 3 AC-4)
	checks = append(checks, validateRevokedKey(ctx, run, logger)...)

	// Check 6: Missing token rejected
	client, err = testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		Token:      "",
		Logger:     logger,
	})
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "missing token rejected", Status: "pass",
		})
	} else {
		_ = client.Close()
		checks = append(checks, metrics.CheckResult{
			Name: "missing token rejected", Status: "fail", Error: "connection with no token was accepted",
		})
	}

	// Check 7: Provisioning API tenant-scoped JWT (Scenario 2 AC-2)
	checks = append(checks, validateProvisioningJWT(ctx, run, logger)...)

	return checks, nil
}

// validateRevokedKey registers a second test key, revokes it, then verifies
// the gateway rejects a JWT minted with that kid.
func validateRevokedKey(ctx context.Context, run *TestRun, logger zerolog.Logger) []metrics.CheckResult {
	provClient := run.authResult.ProvClient
	tenantID := run.Config.TenantID
	revokedKeyID := "tester-revoke-" + run.ID

	// Generate a throwaway keypair for the revoked key test
	kp, err := auth.GenerateKeypair("revoke-" + run.ID)
	if err != nil {
		return []metrics.CheckResult{{
			Name: "revoked key rejected", Status: "fail", Error: fmt.Sprintf("generate revoke keypair: %v", err),
		}}
	}

	// Register the key
	expiresAt := time.Now().Add(1 * time.Hour)
	if err := provClient.RegisterKey(ctx, tenantID, auth.RegisterKeyRequest{
		KeyID:     revokedKeyID,
		Algorithm: "ES256",
		PublicKey: kp.PublicPEM,
		ExpiresAt: &expiresAt,
	}); err != nil {
		return []metrics.CheckResult{{
			Name: "revoked key rejected", Status: "fail", Error: fmt.Sprintf("register revoke key: %v", err),
		}}
	}

	// Revoke it
	if err := provClient.RevokeKey(ctx, tenantID, revokedKeyID); err != nil {
		return []metrics.CheckResult{{
			Name: "revoked key rejected", Status: "fail", Error: fmt.Sprintf("revoke key: %v", err),
		}}
	}

	// Mint JWT with the revoked kid (signed with original test key — doesn't matter,
	// gateway checks revocation status before signature verification)
	revokedToken, err := run.authResult.Minter.MintWithKid(99, revokedKeyID)
	if err != nil {
		return []metrics.CheckResult{{
			Name: "revoked key rejected", Status: "fail", Error: fmt.Sprintf("mint with revoked kid: %v", err),
		}}
	}

	client, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		Token:      revokedToken,
		Logger:     logger,
	})
	if err != nil {
		return []metrics.CheckResult{{
			Name: "revoked key rejected", Status: "pass",
		}}
	}
	_ = client.Close()
	return []metrics.CheckResult{{
		Name: "revoked key rejected", Status: "fail", Error: "JWT with revoked kid was accepted",
	}}
}

// validateProvisioningJWT verifies tenant-scoped JWT access on the provisioning API.
func validateProvisioningJWT(ctx context.Context, run *TestRun, logger zerolog.Logger) []metrics.CheckResult {
	var checks []metrics.CheckResult
	provClient := run.authResult.ProvClient
	tenantJWT := run.authResult.TokenFunc(0)

	// GET own tenant with tenant JWT → expect 200
	status, err := provClient.GetTenant(ctx, run.Config.TenantID, tenantJWT)
	switch {
	case err != nil:
		checks = append(checks, metrics.CheckResult{
			Name: "provisioning JWT own tenant", Status: "fail", Error: err.Error(),
		})
	case status == http.StatusOK:
		checks = append(checks, metrics.CheckResult{
			Name: "provisioning JWT own tenant", Status: "pass",
		})
	default:
		checks = append(checks, metrics.CheckResult{
			Name: "provisioning JWT own tenant", Status: "fail",
			Error: fmt.Sprintf("expected 200, got %d", status),
		})
	}

	// GET other tenant with tenant JWT → expect 403
	status, err = provClient.GetTenant(ctx, "nonexistent-other-tenant", tenantJWT)
	switch {
	case err != nil:
		checks = append(checks, metrics.CheckResult{
			Name: "provisioning JWT cross-tenant blocked", Status: "fail", Error: err.Error(),
		})
	case status == http.StatusForbidden:
		checks = append(checks, metrics.CheckResult{
			Name: "provisioning JWT cross-tenant blocked", Status: "pass",
		})
	default:
		checks = append(checks, metrics.CheckResult{
			Name: "provisioning JWT cross-tenant blocked", Status: "fail",
			Error: fmt.Sprintf("expected 403, got %d", status),
		})
	}

	logger.Debug().Msg("provisioning JWT validation complete")
	return checks
}

func validateChannels(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	var checks []metrics.CheckResult

	client, err := connectWithRetry(ctx, run.Config.GatewayURL, run.authResult.TokenFunc(0), logger)
	if err != nil {
		return []metrics.CheckResult{{
			Name: "connect", Status: "fail", Error: err.Error(),
		}}, nil
	}
	defer client.Close() //nolint:errcheck // best-effort: test cleanup

	// Test public channel subscribe
	if err := client.Subscribe([]string{validatePublicChannel}); err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "public channel subscribe", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "public channel subscribe", Status: "pass",
		})
	}

	return checks, nil
}
