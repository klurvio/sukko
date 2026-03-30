package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/cmd/tester/publisher"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/rs/zerolog"
)

const (
	defaultSoakConnections = 100
	defaultSoakPublishRate = 10
	defaultSoakDuration    = 2 * time.Hour
)

func runSoak(ctx context.Context, run *TestRun, logger zerolog.Logger) (*metrics.Report, error) {
	connections := run.Config.Connections
	if connections <= 0 {
		connections = defaultSoakConnections
	}
	publishRate := run.Config.PublishRate
	if publishRate <= 0 {
		publishRate = defaultSoakPublishRate
	}
	rampRate := run.Config.RampRate
	if rampRate <= 0 {
		rampRate = defaultRampRate
	}
	duration, err := time.ParseDuration(run.Config.Duration)
	if err != nil || duration <= 0 {
		duration = defaultSoakDuration
	}

	testChannel := soakTestChannel

	pool := testerws.NewPool(logger)
	defer pool.Drain()

	logger.Info().Int("connections", connections).Str("duration", duration.String()).Msg("soak test starting")

	if err := pool.RampUp(ctx, testerws.PoolConfig{
		GatewayURL: run.Config.GatewayURL,
		TokenFunc:  run.authResult.TokenFunc,
		Channels:   []string{testChannel},
		OnMessage: func(msg testerws.Message) {
			switch msg.Type {
			case "auth_ack":
				run.Collector.AuthRefreshTotal.Add(1)
			case "auth_error":
				run.Collector.AuthRefreshFailed.Add(1)
				logger.Warn().Str("type", msg.Type).Msg("auth refresh rejected by gateway")
			default:
				run.Collector.MessagesReceived.Add(1)
			}
		},
	}, connections, rampRate); err != nil {
		return nil, fmt.Errorf("ramp up: %w", err)
	}

	run.Collector.ConnectionsActive.Store(pool.Active())
	run.Collector.ConnectionsTotal.Store(pool.Active())

	pub, pubErr := publisher.New(ctx, publisher.Config{
		Mode:       publisher.Mode(run.Config.MessageBackend),
		GatewayURL: run.Config.GatewayURL,
		Token:      run.authResult.TokenFunc(0),
		Logger:     logger,
	})
	if pubErr != nil {
		return nil, fmt.Errorf("create publisher: %w", pubErr)
	}
	defer pub.Close() //nolint:errcheck // best-effort cleanup on teardown

	// Sustain phase: publish at rate with periodic token refresh
	publishInterval := time.Second / time.Duration(publishRate)
	publishTicker := time.NewTicker(publishInterval)
	defer publishTicker.Stop()

	refreshInterval := run.jwtLifetime - run.jwtRefreshBefore
	if refreshInterval <= 0 {
		refreshInterval = 13 * time.Minute // safe fallback: default 15m lifetime - 2m buffer
	}
	refreshTicker := time.NewTicker(refreshInterval)
	defer refreshTicker.Stop()

	deadline := time.After(duration)

	for {
		select {
		case <-ctx.Done():
			run.Collector.ConnectionsActive.Store(0)
			return &metrics.Report{
				TestType: "soak",
				Status:   "stopped",
				Metrics:  run.Collector.Snapshot(),
			}, nil
		case <-deadline:
			run.Collector.ConnectionsActive.Store(0)
			return &metrics.Report{
				TestType: "soak",
				Status:   "pass",
				Metrics:  run.Collector.Snapshot(),
			}, nil
		case <-publishTicker.C:
			if err := pub.Publish(ctx, testChannel); err != nil {
				logger.Warn().Err(err).Msg("publish failed")
			}
		case <-refreshTicker.C:
			refreshed, failed := pool.RefreshAll(run.authResult.Minter.TokenFunc())
			logger.Debug().Int("refreshed", refreshed).Int("failed", failed).Msg("auth refresh cycle")
		}
	}
}
