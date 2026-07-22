package runner

import (
	"context"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

func validateTenantIsolation(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	// Setup: create two throwaway tenants with identical channel/routing rules.
	// RequireAdminProvider: true — isolation suite is only valid in remote mode.
	setupA, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:               run.ID + "-isolation-a",
		ProvisioningURL:      run.Config.ProvisioningURL,
		Logger:               logger,
		AdminProvider:        run.authResult.AdminProvider,
		RequireAdminProvider: true,
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "setup tenant A", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}
	defer setupA.Cleanup(context.Background()) //nolint:contextcheck // NFR-002: cleanup must survive parent cancellation

	setupB, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:               run.ID + "-isolation-b",
		ProvisioningURL:      run.Config.ProvisioningURL,
		Logger:               logger,
		AdminProvider:        run.authResult.AdminProvider,
		RequireAdminProvider: true,
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "setup tenant B", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}
	defer setupB.Cleanup(context.Background()) //nolint:contextcheck // NFR-002: cleanup must survive parent cancellation

	// Set identical channel rules + routing rules on both tenants
	for _, setup := range []*auth.SetupResult{setupA, setupB} {
		if err := setup.ProvClient.SetChannelRules(ctx, setup.TenantID, testChannelRules); err != nil {
			return []metrics.CheckResult{{Name: "set channel rules " + setup.TenantID, Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
		}
		// Edition-gate-tolerant (FR-010): routing rules are Pro-gated (ChannelTopicRouting) and moot on
		// the direct backend, so on Community this skips the 403 while still erroring on any other failure.
		// Matches the pubsub/rest-publish parallel — the isolation suite was the lone raw caller.
		if err := setupSuiteRoutingRules(ctx, setup.ProvClient, setup.TenantID, logger); err != nil {
			return []metrics.CheckResult{{Name: "set routing rules " + setup.TenantID, Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
		}
	}

	// Create engine + users
	engine := NewPubSubEngine(PubSubEngineConfig{
		GatewayURL: run.Config.GatewayURL,
		Logger:     logger,
	})

	userA, err := engine.CreateUser(ctx, setupA.Minter, auth.MintOptions{
		Subject: "isolation-user-a",
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "create user A", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}
	defer func() { _ = userA.Client.Close() }()

	userB, err := engine.CreateUser(ctx, setupB.Minter, auth.MintOptions{
		Subject: "isolation-user-b",
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "create user B", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}
	defer func() { _ = userB.Client.Close() }()

	// Each user subscribes to the same channel suffix under its OWN tenant prefix — isolation
	// depends on the full channel names differing by tenant.
	chanA := tenantChannel(setupA.TenantID, "general.test")
	chanB := tenantChannel(setupB.TenantID, "general.test")
	if err := userA.Client.Subscribe([]string{chanA}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe user A", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}
	if err := userB.Client.Subscribe([]string{chanB}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe user B", Status: metrics.CheckStatusFail, Error: err.Error()}}, nil
	}

	allUsers := []*TestUser{userA, userB}

	// Warm each tenant's OWN delivery path before the measured isolation publishes (FR-003/FR-004),
	// replacing a fixed sleep. In kafka mode a freshly-provisioned tenant's consumer joins its topic
	// at AtEnd only after control-plane propagation, so an early publish is dropped and the expected
	// receiver misses its own message (the flake this fix targets). Both warmups are attempted so a
	// cold tenant is named (FR-005); waitForDeliveryLive self-resets only its OWN user, so clearAll
	// then clears BOTH trackers before measurement (FR-007). Warmup never crosses the tenant boundary.
	var warmupChecks []metrics.CheckResult
	if err := waitForDeliveryLive(ctx, userA, chanA, logger); err != nil {
		warmupChecks = append(warmupChecks, metrics.CheckResult{Name: "delivery warmup tenant A", Status: metrics.CheckStatusFail, Error: err.Error()})
	}
	if err := waitForDeliveryLive(ctx, userB, chanB, logger); err != nil {
		warmupChecks = append(warmupChecks, metrics.CheckResult{Name: "delivery warmup tenant B", Status: metrics.CheckStatusFail, Error: err.Error()})
	}
	if len(warmupChecks) > 0 {
		return warmupChecks, nil // fail-closed: explicit failed check, never a silent skip (FR-005)
	}
	clearAll(allUsers)

	checks := make([]metrics.CheckResult, 0, 2)

	// Check 1: Publish from tenant A → only user A receives
	result := engine.PublishAndVerify(ctx, userA.AsPublisher(), chanA, []*TestUser{userA}, allUsers)
	checks = append(checks, deliveryCheck("tenant A → only A receives", result))
	clearAll(allUsers)

	// Check 2: Publish from tenant B → only user B receives
	result = engine.PublishAndVerify(ctx, userB.AsPublisher(), chanB, []*TestUser{userB}, allUsers)
	checks = append(checks, deliveryCheck("tenant B → only B receives", result))

	return checks, nil
}
