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
	defaultLoadConnections = 100
	defaultLoadPublishRate = 10
	defaultLoadDuration    = 5 * time.Minute
)

func runLoad(ctx context.Context, run *TestRun, logger zerolog.Logger) (*metrics.Report, error) {
	connections := run.Config.Connections
	if connections <= 0 {
		connections = defaultLoadConnections
	}
	publishRate := run.Config.PublishRate
	if publishRate <= 0 {
		publishRate = defaultLoadPublishRate
	}
	rampRate := run.Config.RampRate
	if rampRate <= 0 {
		rampRate = defaultRampRate
	}
	duration, err := time.ParseDuration(run.Config.Duration)
	if err != nil || duration <= 0 {
		duration = defaultLoadDuration
	}

	testChannel := loadTestChannel

	// Phase 1: Ramp up connections
	logger.Info().Int("connections", connections).Int("ramp_rate", rampRate).Msg("ramping up")

	pool := testerws.NewPool(logger)
	defer pool.Drain()

	if err := pool.RampUp(ctx, testerws.PoolConfig{
		GatewayURL: run.Config.GatewayURL,
		TokenFunc:  run.authResult.TokenFunc,
		Channels:   []string{testChannel},
		OnMessage: func(msg testerws.Message) {
			run.Collector.MessagesReceived.Add(1)
		},
	}, connections, rampRate); err != nil {
		return nil, fmt.Errorf("ramp up: %w", err)
	}

	run.Collector.ConnectionsActive.Store(pool.Active())
	run.Collector.ConnectionsTotal.Store(pool.Active())

	// Phase 2: Sustain — publish at rate
	logger.Info().Int("rate", publishRate).Str("duration", duration.String()).Msg("sustaining load")

	pub, err := publisher.New(ctx, publisher.Config{
		Mode:       publisher.Mode(run.Config.MessageBackend),
		GatewayURL: run.Config.GatewayURL,
		Token:      run.authResult.TokenFunc(0),
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create publisher: %w", err)
	}
	defer pub.Close() //nolint:errcheck // best-effort cleanup on teardown

	if err := pub.PublishAtRate(ctx, testChannel, publishRate, duration); err != nil && ctx.Err() == nil {
		logger.Warn().Err(err).Msg("publish error during load test")
	}

	// Phase 3: Drain (handled by defer pool.Drain())
	run.Collector.ConnectionsActive.Store(0)

	return &metrics.Report{
		TestType: "load",
		Status:   "pass",
		Metrics:  run.Collector.Snapshot(),
	}, nil
}
