package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

//nolint:gocritic // appendCombine: sequential checks have data flow between them (apiKeyID captured in check 4, used in 5-6, check 6 is conditional). Combining into one append is not possible.
func validateProvisioning(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	// Create a dedicated throwaway tenant for lifecycle testing
	setup, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:          run.ID + "-prov",
		ProvisioningURL: run.Config.ProvisioningURL,
		Logger:          logger,
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "setup", Status: "fail", Error: err.Error()}}, nil
	}
	defer setup.Cleanup(context.Background()) //nolint:contextcheck // cleanup must survive parent cancellation

	provClient := setup.ProvClient
	tenantID := setup.TenantID

	var checks []metrics.CheckResult

	// 1. Get tenant by ID (admin-authenticated, tests GET /tenants/{id})
	checks = append(checks, provCheck("get tenant", func() error {
		body, err := provClient.GetTenantByID(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get tenant: %w", err)
		}
		if !strings.Contains(string(body), tenantID) {
			return fmt.Errorf("tenant %s not found in response", tenantID)
		}
		return nil
	}))

	// 2. List tenants — verify our tenant appears
	checks = append(checks, provCheck("list tenants", func() error {
		body, err := provClient.ListTenants(ctx)
		if err != nil {
			return fmt.Errorf("list tenants: %w", err)
		}
		if !strings.Contains(string(body), tenantID) {
			return fmt.Errorf("tenant %s not found in list", tenantID)
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

	// 8. Set routing rules
	checks = append(checks, provCheck("set routing rules", func() error {
		if err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules); err != nil {
			return fmt.Errorf("set routing rules: %w", err)
		}
		return nil
	}))

	// 9. Get routing rules
	checks = append(checks, provCheck("get routing rules", func() error {
		body, err := provClient.GetRoutingRules(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get routing rules: %w", err)
		}
		if !strings.Contains(string(body), "default") {
			return fmt.Errorf("expected 'default' topic suffix in rules, got: %s", string(body))
		}
		return nil
	}))

	// 10. Delete routing rules
	checks = append(checks, provCheck("delete routing rules", func() error {
		if err := provClient.DeleteRoutingRules(ctx, tenantID); err != nil {
			return fmt.Errorf("delete routing rules: %w", err)
		}
		return nil
	}))

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

	// 14. Get quota
	checks = append(checks, provCheck("get quota", func() error {
		if _, err := provClient.GetQuota(ctx, tenantID); err != nil {
			return fmt.Errorf("get quota: %w", err)
		}
		return nil
	}))

	// 15. Update quota
	checks = append(checks, provCheck("update quota", func() error {
		if err := provClient.UpdateQuota(ctx, tenantID, map[string]any{"max_connections": 500}); err != nil {
			return fmt.Errorf("update quota: %w", err)
		}
		return nil
	}))

	// 16. Get audit log
	checks = append(checks, provCheck("get audit log", func() error {
		body, err := provClient.GetAuditLog(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get audit log: %w", err)
		}
		if len(body) < 10 {
			return fmt.Errorf("audit log appears empty (len=%d)", len(body))
		}
		return nil
	}))

	// 17. Suspend tenant
	checks = append(checks, provCheck("suspend tenant", func() error {
		if err := provClient.SuspendTenant(ctx, tenantID); err != nil {
			return fmt.Errorf("suspend tenant: %w", err)
		}
		return nil
	}))

	// 18. Reactivate tenant
	checks = append(checks, provCheck("reactivate tenant", func() error {
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
		return metrics.CheckResult{Name: name, Status: "fail", Error: err.Error()}
	}
	return metrics.CheckResult{Name: name, Status: "pass"}
}
