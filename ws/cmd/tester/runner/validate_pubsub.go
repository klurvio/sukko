package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/internal/shared/routing"
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
	{"pattern": "**", "topics": []string{routing.DefaultTopicSuffix}, "priority": routing.DefaultCatchAllPriority},
}

// isRoutingRulesEditionGated reports whether a SetRoutingRules error is the
// provisioning RequireFeature gate (HTTP 403, code EDITION_LIMIT, message
// "This feature requires <edition> edition or higher"). Matched narrowly on the
// feature-gate message so the per-tenant rule COUNT boundary is never mistaken
// for it: that boundary is a DISTINCT rejection — 400 TOO_MANY_ROUTING_RULES
// from the ReplaceRoutingRules handler's config count validator, which runs
// before the service-level edition check (edition_limits.go:33-46). The
// service-level 403 EDITION_LIMIT_ROUTING_RULES_PER_TENANT is unreachable at
// default config, so the count path never surfaces as EDITION_LIMIT.
func isRoutingRulesEditionGated(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), "EDITION_LIMIT") &&
		strings.Contains(err.Error(), "edition or higher")
}

// setupSuiteRoutingRules applies the catch-all test routing rules to the suite
// tenant, tolerating the Pro edition gate on the routing-rules API: routing
// rules only affect the Kafka message backend, which is itself Pro-gated. On a
// Community stack the backend is therefore direct — publishes are never
// topic-routed and the rules are unnecessary — so the feature-gate 403 means
// "rules not needed here", not "setup broken". Every other failure is returned:
// on the Kafka backend a missing catch-all rule silently drops every publish,
// which would surface as opaque delivery failures in later checks.
func setupSuiteRoutingRules(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string, logger zerolog.Logger) error {
	err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules)
	if err == nil {
		return nil
	}
	if isRoutingRulesEditionGated(err) {
		logger.Info().Str("tenant", tenantID).
			Msg("routing rules are edition-gated (Community): skipping — direct backend does not use routing rules")
		return nil
	}
	return fmt.Errorf("suite routing rules: %w", err)
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
	if err := setupSuiteRoutingRules(ctx, provClient, tenantID, logger); err != nil {
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
	generalChannel := tenantChannel(tenantID, "general.test")
	dmAChannel := tenantChannel(tenantID, "dm.test-user-a")
	vipChannel := tenantChannel(tenantID, "room.vip")
	tradersChannel := tenantChannel(tenantID, "room.traders")
	if err := userA.Client.Subscribe([]string{generalChannel, dmAChannel, vipChannel}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe userA", Status: "fail", Error: err.Error()}}, nil
	}
	if err := userB.Client.Subscribe([]string{generalChannel, tradersChannel}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe userB", Status: "fail", Error: err.Error()}}, nil
	}
	if err := userC.Client.Subscribe([]string{generalChannel}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe userC", Status: "fail", Error: err.Error()}}, nil
	}

	// Warm up the delivery loop before the scoping checks. A fixed sleep is a direct-mode
	// assumption; in Kafka mode the ws-server consumer joins this freshly-provisioned tenant's topic
	// at AtEnd only after a cold-start window, so early publishes are dropped. All test channels for
	// this tenant route to one topic, so proving liveness on generalChannel covers every check below.
	// (The scoping checks verify unique per-publish UUIDs, so a warmup straggler cannot corrupt them;
	// clearAll resets trackers anyway.)
	if err := waitForDeliveryLive(ctx, userA, generalChannel, logger); err != nil {
		return []metrics.CheckResult{{Name: "delivery warmup", Status: "fail", Error: err.Error()}}, nil
	}
	clearAll(users)

	var checks []metrics.CheckResult

	// Check 1: Public channel round-trip — all 3 receive
	result := engine.PublishAndVerify(ctx, userA.AsPublisher(), generalChannel, []*TestUser{userA, userB, userC}, users)
	checks = append(checks, deliveryCheck("public round-trip", result))
	clearAll(users)

	// Check 2: User-scoped isolation — only userA receives dm.test-user-a
	result = engine.PublishAndVerify(ctx, userA.AsPublisher(), dmAChannel, []*TestUser{userA}, users)
	checks = append(checks, deliveryCheck("user-scoped isolation", result))
	clearAll(users)

	// Check 3: Group-scoped vip — only userA receives room.vip
	result = engine.PublishAndVerify(ctx, userA.AsPublisher(), vipChannel, []*TestUser{userA}, users)
	checks = append(checks, deliveryCheck("group-scoped vip", result))
	clearAll(users)

	// Check 4: Group-scoped traders — only userB receives room.traders
	result = engine.PublishAndVerify(ctx, userB.AsPublisher(), tradersChannel, []*TestUser{userB}, users)
	checks = append(checks, deliveryCheck("group-scoped traders", result))
	clearAll(users)

	// Check 5: Publish authorization — userC publishes to room.vip, should be rejected
	err := userC.Client.Publish(vipChannel, []byte(`{"msg_id":"auth-test","ts":0}`))
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
