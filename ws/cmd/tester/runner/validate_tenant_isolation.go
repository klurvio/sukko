package runner

import (
	"context"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

func validateTenantIsolation(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	// Setup: create two throwaway tenants with identical channel/routing rules
	setupA, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:          run.ID + "-isolation-a",
		ProvisioningURL: run.Config.ProvisioningURL,
		AdminToken:      run.Config.Token,
		Logger:          logger,
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "setup tenant A", Status: "fail", Error: err.Error()}}, nil
	}
	defer setupA.Cleanup(context.Background()) //nolint:contextcheck // NFR-002: cleanup must survive parent cancellation

	setupB, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:          run.ID + "-isolation-b",
		ProvisioningURL: run.Config.ProvisioningURL,
		AdminToken:      run.Config.Token,
		Logger:          logger,
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "setup tenant B", Status: "fail", Error: err.Error()}}, nil
	}
	defer setupB.Cleanup(context.Background()) //nolint:contextcheck // NFR-002: cleanup must survive parent cancellation

	// Set identical channel rules + routing rules on both tenants
	for _, setup := range []*auth.SetupResult{setupA, setupB} {
		if err := setup.ProvClient.SetChannelRules(ctx, setup.TenantID, testChannelRules); err != nil {
			return []metrics.CheckResult{{Name: "set channel rules " + setup.TenantID, Status: "fail", Error: err.Error()}}, nil
		}
		if err := setup.ProvClient.SetRoutingRules(ctx, setup.TenantID, testRoutingRules); err != nil {
			return []metrics.CheckResult{{Name: "set routing rules " + setup.TenantID, Status: "fail", Error: err.Error()}}, nil
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
		return []metrics.CheckResult{{Name: "create user A", Status: "fail", Error: err.Error()}}, nil
	}
	defer func() { _ = userA.Client.Close() }()

	userB, err := engine.CreateUser(ctx, setupB.Minter, auth.MintOptions{
		Subject: "isolation-user-b",
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "create user B", Status: "fail", Error: err.Error()}}, nil
	}
	defer func() { _ = userB.Client.Close() }()

	// Both subscribe to the same channel name (different tenants internally)
	if err := userA.Client.Subscribe([]string{"general.test"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe user A", Status: "fail", Error: err.Error()}}, nil
	}
	if err := userB.Client.Subscribe([]string{"general.test"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe user B", Status: "fail", Error: err.Error()}}, nil
	}

	time.Sleep(500 * time.Millisecond) // allow subscriptions to propagate

	allUsers := []*TestUser{userA, userB}
	checks := make([]metrics.CheckResult, 0, 2)

	// Check 1: Publish from tenant A → only user A receives
	result := engine.PublishAndVerify(ctx, userA, "general.test", []*TestUser{userA}, allUsers)
	checks = append(checks, deliveryCheck("tenant A → only A receives", result))
	clearAll(allUsers)

	// Check 2: Publish from tenant B → only user B receives
	result = engine.PublishAndVerify(ctx, userB, "general.test", []*TestUser{userB}, allUsers)
	checks = append(checks, deliveryCheck("tenant B → only B receives", result))

	return checks, nil
}
