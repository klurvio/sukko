package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/rs/zerolog"
)

const (
	rateLimitBurstCount     = 500
	rateLimitMaxWriteErrors = 5 // stop burst after this many consecutive write failures
)

func validateRateLimit(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	provClient := run.authResult.ProvClient
	tenantID := run.authResult.TenantID

	// Setup: routing rules so publishes are routed (not silently dropped).
	// Error ignored: if this fails, the test still measures write behavior.
	_ = provClient.SetRoutingRules(ctx, tenantID, testRoutingRules)

	engine := NewPubSubEngine(PubSubEngineConfig{
		GatewayURL: run.Config.GatewayURL,
		Logger:     logger,
	})

	user, err := engine.CreateUser(ctx, run.authResult.Minter, auth.MintOptions{
		Subject: "ratelimit-test",
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "connect", Status: "fail", Error: err.Error()}}, nil
	}
	defer func() { _ = user.Client.Close() }()

	if err := user.Client.Subscribe([]string{"general.test"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe", Status: "fail", Error: err.Error()}}, nil
	}

	time.Sleep(300 * time.Millisecond)

	// Burst: send messages as fast as possible with zero delay.
	// If rate limiting is active, writes will eventually fail or the connection
	// will be closed by the gateway.
	var sent, writeErrors int
	for i := range rateLimitBurstCount {
		payload, _ := json.Marshal(map[string]any{ // json.Marshal on literal map of primitives cannot fail
			"msg_id": fmt.Sprintf("rl-%04d", i),
			"ts":     time.Now().UnixMilli(),
		})
		if err := user.Client.Publish("general.test", payload); err != nil {
			writeErrors++
			if writeErrors >= rateLimitMaxWriteErrors {
				break // connection likely closed by rate limiter
			}
		} else {
			sent++
		}
	}

	// Probe: wait briefly, then test if the connection is still alive.
	time.Sleep(1 * time.Second)

	probePayload, _ := json.Marshal(map[string]any{ // json.Marshal on literal map of primitives cannot fail
		"msg_id": "rl-probe",
		"ts":     time.Now().UnixMilli(),
	})
	probeErr := user.Client.Publish("general.test", probePayload)

	checks := make([]metrics.CheckResult, 0, 1)

	switch {
	case writeErrors > 0:
		// Burst was interrupted — rate limiter closed connection or rejected writes
		checks = append(checks, metrics.CheckResult{
			Name: "rate limit enforcement", Status: "pass",
			Latency: fmt.Sprintf("sent %d, %d write errors during burst", sent, writeErrors),
		})
	case probeErr != nil:
		// Burst succeeded but post-burst probe failed — rate limiter killed connection after the burst
		checks = append(checks, metrics.CheckResult{
			Name: "rate limit enforcement", Status: "pass",
			Latency: fmt.Sprintf("sent %d, connection closed after burst", sent),
		})
	default:
		// All messages accepted and connection still alive
		checks = append(checks, metrics.CheckResult{
			Name: "rate limit enforcement", Status: "skip",
			Latency: fmt.Sprintf("all %d messages accepted — rate limiting may be disabled or threshold exceeds burst size", sent),
		})
	}

	return checks, nil
}
