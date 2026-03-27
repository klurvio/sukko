package runner

import (
	"context"
	"time"

	"github.com/klurvio/sukko/cmd/tester/metrics"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/rs/zerolog"
)

// validatePublicChannel is the channel used by the channels validation suite.
const validatePublicChannel = "sukko.validate.public"

// invalidTokenFixture is a known-bad token used to verify the gateway rejects invalid auth.
const invalidTokenFixture = "invalid-token"

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

func validateAuth(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) { //nolint:unparam // error return reserved for future validation checks that may fail
	var checks []metrics.CheckResult

	// Test valid JWT
	start := time.Now()
	client, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		Token:      run.Config.Token,
		Logger:     logger,
	})
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "valid JWT accepted", Status: "fail", Error: err.Error(),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "valid JWT accepted", Status: "pass",
			Latency: time.Since(start).Round(time.Millisecond).String(),
		})
		_ = client.Close() // best-effort: test cleanup
	}

	// Test invalid JWT
	start = time.Now()
	client, err = testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		Token:      invalidTokenFixture,
		Logger:     logger,
	})
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: "invalid JWT rejected", Status: "pass",
			Latency: time.Since(start).Round(time.Millisecond).String(),
		})
	} else {
		_ = client.Close() // best-effort: test cleanup
		checks = append(checks, metrics.CheckResult{
			Name: "invalid JWT rejected", Status: "fail",
			Error: "invalid JWT was accepted",
		})
	}

	return checks, nil
}

func validateChannels(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	var checks []metrics.CheckResult

	client, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: run.Config.GatewayURL,
		Token:      run.Config.Token,
		Logger:     logger,
	})
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

func validateOrdering(_ context.Context, _ *TestRun, _ zerolog.Logger) ([]metrics.CheckResult, error) { //nolint:unparam // error return reserved for future implementation
	return []metrics.CheckResult{{
		Name: "message ordering", Status: "skip",
	}}, nil
}

func validateReconnect(_ context.Context, _ *TestRun, _ zerolog.Logger) ([]metrics.CheckResult, error) { //nolint:unparam // error return reserved for future implementation
	return []metrics.CheckResult{{
		Name: "reconnect recovery", Status: "skip",
	}}, nil
}

func validateRateLimit(_ context.Context, _ *TestRun, _ zerolog.Logger) ([]metrics.CheckResult, error) { //nolint:unparam // error return reserved for future implementation
	return []metrics.CheckResult{{
		Name: "rate limit enforcement", Status: "skip",
	}}, nil
}
