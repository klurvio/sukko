package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

// Channel rules fixture for pub-sub validation.
// dm.{principal} resolves to JWT subject — each user can subscribe/publish to their own dm.<subject>.
var testChannelRules = map[string]any{
	"public": []string{"general.*", "announce.*", "dm.{principal}"},
	"group_mappings": map[string][]string{
		"vip":     {"room.vip"},
		"traders": {"room.traders"},
	},
	"default":        []string{"general.*"},
	"publish_public": []string{"general.*", "dm.{principal}"},
	"publish_group_mappings": map[string][]string{
		"vip":     {"room.vip"},
		"traders": {"room.traders"},
	},
	"publish_default": []string{"general.*"},
}

// Catch-all routing rule for test messages.
var testRoutingRules = []map[string]any{
	{"pattern": "*.*", "topic_suffix": "test-default"},
}

// Test user profiles for scoping checks.
var testUserProfiles = []auth.MintOptions{
	{Subject: "test-user-a", Groups: []string{"vip"}},
	{Subject: "test-user-b", Groups: []string{"traders"}},
	{Subject: "test-user-c", Groups: nil}, // no groups — default rules only
}

func validatePubSub(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	provClient := run.authResult.ProvClient
	tenantID := run.authResult.TenantID

	// Setup: channel rules + routing rules on throwaway tenant
	if err := provClient.SetChannelRules(ctx, tenantID, testChannelRules); err != nil {
		return []metrics.CheckResult{{Name: "setup channel rules", Status: "fail", Error: err.Error()}}, nil
	}
	if err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules); err != nil {
		return []metrics.CheckResult{{Name: "setup routing rules", Status: "fail", Error: err.Error()}}, nil
	}

	// Create pub-sub engine
	engine := NewPubSubEngine(PubSubEngineConfig{
		GatewayURL: run.Config.GatewayURL,
		Logger:     logger,
	})

	// Create test users
	users := make([]*TestUser, len(testUserProfiles))
	for i, profile := range testUserProfiles {
		profile.ConnIndex = i
		user, err := engine.CreateUser(ctx, run.authResult.Minter, profile)
		if err != nil {
			return []metrics.CheckResult{{Name: "create user " + profile.Subject, Status: "fail", Error: err.Error()}}, nil
		}
		users[i] = user
	}

	// Cleanup: close all clients on exit (context.Background to survive cancellation)
	defer func() {
		for _, u := range users {
			if u != nil && u.Client != nil {
				_ = u.Client.Close() // best-effort: test cleanup, multi-step continues on failure
			}
		}
	}()

	userA, userB, userC := users[0], users[1], users[2]

	// Subscribe users to their authorized channels
	if err := userA.Client.Subscribe([]string{"general.test", "dm.test-user-a", "room.vip"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe userA", Status: "fail", Error: err.Error()}}, nil
	}
	if err := userB.Client.Subscribe([]string{"general.test", "room.traders"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe userB", Status: "fail", Error: err.Error()}}, nil
	}
	if err := userC.Client.Subscribe([]string{"general.test"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe userC", Status: "fail", Error: err.Error()}}, nil
	}

	// Allow subscriptions to propagate
	time.Sleep(500 * time.Millisecond)

	var checks []metrics.CheckResult

	// Check 1: Public channel round-trip — all 3 receive
	result := engine.PublishAndVerify(ctx, userA.AsPublisher(), "general.test", []*TestUser{userA, userB, userC}, users)
	checks = append(checks, deliveryCheck("public round-trip", result))
	clearAll(users)

	// Check 2: User-scoped isolation — only userA receives dm.test-user-a
	result = engine.PublishAndVerify(ctx, userA.AsPublisher(), "dm.test-user-a", []*TestUser{userA}, users)
	checks = append(checks, deliveryCheck("user-scoped isolation", result))
	clearAll(users)

	// Check 3: Group-scoped vip — only userA receives room.vip
	result = engine.PublishAndVerify(ctx, userA.AsPublisher(), "room.vip", []*TestUser{userA}, users)
	checks = append(checks, deliveryCheck("group-scoped vip", result))
	clearAll(users)

	// Check 4: Group-scoped traders — only userB receives room.traders
	result = engine.PublishAndVerify(ctx, userB.AsPublisher(), "room.traders", []*TestUser{userB}, users)
	checks = append(checks, deliveryCheck("group-scoped traders", result))
	clearAll(users)

	// Check 5: Publish authorization — userC publishes to room.vip, should be rejected
	err := userC.Client.Publish("room.vip", []byte(`{"msg_id":"auth-test","ts":0}`))
	if err != nil {
		checks = append(checks, metrics.CheckResult{Name: "publish auth rejection", Status: "pass", Latency: "rejected"})
	} else {
		// Publish didn't error — check if any user received it (shouldn't)
		time.Sleep(1 * time.Second)
		anyReceived := false
		for _, u := range users {
			if u.HasReceived("auth-test") {
				anyReceived = true
				break
			}
		}
		if anyReceived {
			checks = append(checks, metrics.CheckResult{Name: "publish auth rejection", Status: "fail", Error: "unauthorized publish was delivered"})
		} else {
			checks = append(checks, metrics.CheckResult{Name: "publish auth rejection", Status: "pass", Latency: "silently dropped"})
		}
	}

	return checks, nil
}

func deliveryCheck(name string, result DeliveryResult) metrics.CheckResult {
	if result.Delivered && len(result.MisroutedTo) == 0 {
		return metrics.CheckResult{
			Name:    name,
			Status:  "pass",
			Latency: result.Latency.Round(time.Millisecond).String(),
		}
	}

	errMsg := ""
	if !result.Delivered {
		errMsg = fmt.Sprintf("missing: %v", result.Missing)
	}
	if len(result.MisroutedTo) > 0 {
		if errMsg != "" {
			errMsg += "; "
		}
		errMsg += fmt.Sprintf("misrouted to: %v", result.MisroutedTo)
	}

	return metrics.CheckResult{
		Name:   name,
		Status: "fail",
		Error:  errMsg,
	}
}

func clearAll(users []*TestUser) {
	for _, u := range users {
		u.ClearReceived()
	}
}
