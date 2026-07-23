package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/internal/shared/routing"
	"github.com/rs/zerolog"
)

//nolint:gocritic // appendCombine: sequential checks have data flow between them (apiKeyID captured in check 4, used in 5-6, check 6 is conditional). Combining into one append is not possible.
func validateProvisioning(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	// Create a dedicated throwaway tenant for lifecycle testing.
	// RequireAdminProvider: true — the provisioning suite is only valid in remote mode;
	// an ephemeral keypair would be rejected by a deployed provisioning service.
	setup, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:               run.ID + "-prov",
		ProvisioningURL:      run.Config.ProvisioningURL,
		Logger:               logger,
		AdminProvider:        run.authResult.AdminProvider,
		RequireAdminProvider: true,
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "setup", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}
	defer setup.Cleanup(context.Background()) //nolint:contextcheck // cleanup must survive parent cancellation

	provClient := setup.ProvClient
	tenantID := setup.TenantID

	// Fetch the edition once. Drives the gate-tolerant classification of the edition-gated checks
	// (routing rules, quota, lifecycle, audit) and the routing block's Community-only branches.
	// A fetch failure is non-fatal — treat as unknown edition; the gate-tolerant wrapper still
	// classifies each check from its own response (R3/R5).
	edition := ""
	if info, edErr := fetchEditionFrom(ctx, run.Config.ProvisioningURL); edErr != nil {
		logger.Debug().Err(edErr).Msg("provisioning /edition unreachable — edition-gated checks classify from response only")
	} else {
		edition = info.Edition
	}

	var checks []metrics.CheckResult

	// 1. Get tenant by slug (admin-authenticated, tests GET /tenants/{slug})
	checks = append(checks, provCheck("get tenant", func() error {
		body, err := provClient.GetTenantByID(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get tenant: %w", err)
		}
		var resp struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		}
		if parseErr := json.Unmarshal(body, &resp); parseErr == nil {
			// Assert slug matches the input identifier (URL param)
			if resp.Slug != tenantID {
				return fmt.Errorf("tenant slug = %q, want %q", resp.Slug, tenantID)
			}
			// Assert id is UUID format (36 chars with 4 dashes)
			if len(resp.ID) != 36 || strings.Count(resp.ID, "-") != 4 {
				return fmt.Errorf("tenant id = %q, want UUID format (xxxx-xxxx-xxxx-xxxx-xxxx)", resp.ID)
			}
		} else if !strings.Contains(string(body), tenantID) {
			return fmt.Errorf("tenant %s not found in response", tenantID)
		}
		return nil
	}))

	// 2. List tenants — verify our tenant appears (by slug)
	checks = append(checks, provCheck("list tenants", func() error {
		body, err := provClient.ListTenants(ctx)
		if err != nil {
			return fmt.Errorf("list tenants: %w", err)
		}
		if !strings.Contains(string(body), tenantID) {
			return fmt.Errorf("tenant slug %q not found in list", tenantID)
		}
		return nil
	}))

	// 3. Update tenant
	checks = append(checks, provCheck("update tenant", func() error {
		if err := provClient.UpdateTenant(ctx, tenantID, map[string]any{"name": "Updated by provisioning suite"}); err != nil {
			return fmt.Errorf("update tenant: %w", err)
		}
		return nil
	}))

	// 4. Create API key
	var apiKeyID string
	checks = append(checks, provCheck("create api key", func() error {
		body, err := provClient.CreateAPIKey(ctx, tenantID, "test-api-key")
		if err != nil {
			return fmt.Errorf("create api key: %w", err)
		}
		var resp struct {
			KeyID string `json:"key_id"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.KeyID != "" {
			apiKeyID = resp.KeyID
		}
		return nil
	}))

	// 5. List API keys
	checks = append(checks, provCheck("list api keys", func() error {
		body, err := provClient.ListAPIKeys(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list api keys: %w", err)
		}
		if apiKeyID != "" && !strings.Contains(string(body), apiKeyID) {
			return fmt.Errorf("api key %s not found in list", apiKeyID)
		}
		return nil
	}))

	// 6. Revoke API key
	if apiKeyID != "" {
		checks = append(checks, provCheck("revoke api key", func() error {
			if err := provClient.RevokeAPIKey(ctx, tenantID, apiKeyID); err != nil {
				return fmt.Errorf("revoke api key: %w", err)
			}
			return nil
		}))
	}

	// 7. List signing keys (already has one from auth.Setup)
	checks = append(checks, provCheck("list signing keys", func() error {
		if _, err := provClient.ListKeys(ctx, tenantID); err != nil {
			return fmt.Errorf("list keys: %w", err)
		}
		return nil
	}))

	// 8. Set routing rules (edition-gated → gate-tolerant: skips on Community, R5)
	checks = append(checks, provRoutingCheck("set routing rules", func() error {
		if err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules); err != nil {
			return fmt.Errorf("set routing rules: %w", err)
		}
		return nil
	}))

	// 9. Get routing rules (edition-gated → gate-tolerant)
	checks = append(checks, provRoutingCheck("get routing rules", func() error {
		body, err := provClient.GetRoutingRules(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get routing rules: %w", err)
		}
		// testRoutingRules writes topics:[routing.DefaultTopicSuffix] ("default"), so a
		// non-vacuous round-trip asserts that suffix appears in the GET body. The prior
		// literal "test-default" never matched the fixture and reddened the suite (FR-001).
		if !strings.Contains(string(body), routing.DefaultTopicSuffix) {
			return fmt.Errorf("expected %q topic in rules, got: %s", routing.DefaultTopicSuffix, string(body))
		}
		return nil
	}))

	// 10. Delete routing rules (edition-gated → gate-tolerant). Its populated delete pairs with
	// the routing block's `routing delete idempotent` empty-delete ([[one-sided-green]]).
	checks = append(checks, provRoutingCheck("delete routing rules", func() error {
		if err := provClient.DeleteRoutingRules(ctx, tenantID); err != nil {
			return fmt.Errorf("delete routing rules: %w", err)
		}
		return nil
	}))

	// --- E2E-unique routing-rules coverage block (FR-002…FR-007), placed immediately after
	// check 10 so its idempotent-delete pairs with check 10's populated delete. On Community the
	// routing-rules API is feature-gated; the block's checks skip explicitly (never fail).
	checks = append(checks, validateProvisioningRouting(ctx, run, setup, edition, logger)...)

	// 11. Set channel rules
	checks = append(checks, provCheck("set channel rules", func() error {
		if err := provClient.SetChannelRules(ctx, tenantID, testChannelRules); err != nil {
			return fmt.Errorf("set channel rules: %w", err)
		}
		return nil
	}))

	// 12. Get channel rules
	checks = append(checks, provCheck("get channel rules", func() error {
		body, err := provClient.GetChannelRules(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get channel rules: %w", err)
		}
		if !strings.Contains(string(body), "general") {
			return fmt.Errorf("expected 'general' pattern in channel rules, got: %s", string(body))
		}
		return nil
	}))

	// 13. Delete channel rules
	checks = append(checks, provCheck("delete channel rules", func() error {
		if err := provClient.DeleteChannelRules(ctx, tenantID); err != nil {
			return fmt.Errorf("delete channel rules: %w", err)
		}
		return nil
	}))

	// 14. Get quota (Pro-gated → gate-tolerant: skips on Community, R5)
	checks = append(checks, provRoutingCheck("get quota", func() error {
		if _, err := provClient.GetQuota(ctx, tenantID); err != nil {
			return fmt.Errorf("get quota: %w", err)
		}
		return nil
	}))

	// 15. Update quota (Pro-gated → gate-tolerant)
	checks = append(checks, provRoutingCheck("update quota", func() error {
		if err := provClient.UpdateQuota(ctx, tenantID, map[string]any{"max_connections": 500}); err != nil {
			return fmt.Errorf("update quota: %w", err)
		}
		return nil
	}))

	// 16. Get audit log (Enterprise-gated → gate-tolerant: skips on Pro and Community, R5)
	checks = append(checks, provRoutingCheck("get audit log", func() error {
		body, err := provClient.GetAuditLog(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get audit log: %w", err)
		}
		if len(body) < 10 {
			return fmt.Errorf("audit log appears empty (len=%d)", len(body))
		}
		return nil
	}))

	// 17. Suspend tenant (Pro-gated → gate-tolerant)
	checks = append(checks, provRoutingCheck("suspend tenant", func() error {
		if err := provClient.SuspendTenant(ctx, tenantID); err != nil {
			return fmt.Errorf("suspend tenant: %w", err)
		}
		return nil
	}))

	// 18. Reactivate tenant (Pro-gated → gate-tolerant)
	checks = append(checks, provRoutingCheck("reactivate tenant", func() error {
		if err := provClient.ReactivateTenant(ctx, tenantID); err != nil {
			return fmt.Errorf("reactivate tenant: %w", err)
		}
		return nil
	}))

	// 19. Deprovision: skip — deferred cleanup handles this.
	// Testing deprovision separately would conflict with the cleanup.

	return checks, nil
}

func provCheck(name string, fn func() error) metrics.CheckResult {
	if err := fn(); err != nil {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail, Error: err.Error()}
	}
	return metrics.CheckResult{Name: name, Status: metrics.CheckStatusPass}
}
