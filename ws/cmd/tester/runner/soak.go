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
		Token:      run.Config.Token,
		Channels:   []string{testChannel},
		OnMessage: func(msg testerws.Message) {
			run.Collector.MessagesReceived.Add(1)
		},
	}, connections, rampRate); err != nil {
		return nil, fmt.Errorf("ramp up: %w", err)
	}

	run.Collector.ConnectionsActive.Store(pool.Active())
	run.Collector.ConnectionsTotal.Store(pool.Active())

	pub, pubErr := publisher.New(ctx, publisher.Config{
		Mode:       publisher.Mode(run.Config.MessageBackend),
		GatewayURL: run.Config.GatewayURL,
		Token:      run.Config.Token,
		Logger:     logger,
	})
	if pubErr != nil {
		return nil, fmt.Errorf("create publisher: %w", pubErr)
	}
	defer pub.Close() //nolint:errcheck // best-effort cleanup on teardown

	if err := pub.PublishAtRate(ctx, testChannel, publishRate, duration); err != nil && ctx.Err() == nil {
		logger.Warn().Err(err).Msg("publish error during soak test")
	}

	run.Collector.ConnectionsActive.Store(0)

	return &metrics.Report{
		TestType: "soak",
		Status:   "pass",
		Metrics:  run.Collector.Snapshot(),
	}, nil
}
