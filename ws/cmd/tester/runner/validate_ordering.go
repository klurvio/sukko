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

const orderingMessageCount = 20

func validateOrdering(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	provClient := run.authResult.ProvClient
	tenantID := run.authResult.TenantID

	// Setup: routing rules so publishes are routed to the bus.
	// Error ignored: if this fails, delivery checks below will fail with clear errors.
	_ = provClient.SetRoutingRules(ctx, tenantID, testRoutingRules)

	engine := NewPubSubEngine(PubSubEngineConfig{
		GatewayURL: run.Config.GatewayURL,
		Logger:     logger,
	})

	user, err := engine.CreateUser(ctx, run.authResult.Minter, auth.MintOptions{
		Subject: "ordering-test",
	})
	if err != nil {
		return []metrics.CheckResult{{Name: "create user", Status: "fail", Error: err.Error()}}, nil
	}
	defer func() { _ = user.Client.Close() }()

	if err := user.Client.Subscribe([]string{"general.test"}); err != nil {
		return []metrics.CheckResult{{Name: "subscribe", Status: "fail", Error: err.Error()}}, nil
	}

	time.Sleep(500 * time.Millisecond) // allow subscription to propagate

	// Publish N messages in strict sequence
	for i := range orderingMessageCount {
		payload, _ := json.Marshal(map[string]any{ // json.Marshal on literal map of primitives cannot fail
			"msg_id": fmt.Sprintf("order-%04d", i),
			"ts":     time.Now().UnixMilli(),
		})
		if err := user.Client.Publish("general.test", payload); err != nil {
			return []metrics.CheckResult{{
				Name: "publish sequence", Status: "fail",
				Error: fmt.Sprintf("publish msg %d: %v", i, err),
			}}, nil
		}
	}

	// Wait for all messages to arrive
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			goto check
		case <-ctx.Done():
			goto check
		case <-ticker.C:
			if user.ReceivedCount() >= orderingMessageCount {
				time.Sleep(200 * time.Millisecond) // grace period for stragglers
				goto check
			}
		}
	}

check:
	received := user.ReceivedOrder()
	checks := make([]metrics.CheckResult, 0, 2)

	if len(received) < orderingMessageCount {
		checks = append(checks, metrics.CheckResult{
			Name: "all messages received", Status: "fail",
			Error: fmt.Sprintf("received %d/%d", len(received), orderingMessageCount),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "all messages received", Status: "pass",
			Latency: fmt.Sprintf("%d/%d", len(received), orderingMessageCount),
		})
	}

	// Verify FIFO order: each message ID should be lexicographically >= the previous.
	// Works because format is "order-%04d" — lexicographic order == numeric order.
	inOrder := true
	firstOutOfOrder := ""
	for i := 1; i < len(received); i++ {
		if received[i] < received[i-1] {
			inOrder = false
			firstOutOfOrder = fmt.Sprintf("%s arrived after %s", received[i], received[i-1])
			break
		}
	}

	switch {
	case len(received) == 0:
		checks = append(checks, metrics.CheckResult{
			Name: "message ordering", Status: "fail", Error: "no messages received",
		})
	case inOrder:
		checks = append(checks, metrics.CheckResult{
			Name: "message ordering", Status: "pass",
			Latency: fmt.Sprintf("%d messages in FIFO order", len(received)),
		})
	default:
		checks = append(checks, metrics.CheckResult{
			Name: "message ordering", Status: "fail",
			Error: "out-of-order: " + firstOutOfOrder,
		})
	}

	return checks, nil
}
