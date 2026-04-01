package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
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

	// Channel mode: distribute across public/user/group channels
	if run.Config.ChannelMode {
		return runLoadChannelMode(ctx, run, logger, connections, publishRate, rampRate, duration)
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

	pub, err := publisher.NewDirectPublisher(ctx, run.Config.GatewayURL, run.authResult.TokenFunc(0))
	if err != nil {
		return nil, fmt.Errorf("create publisher: %w", err)
	}
	defer pub.Close() //nolint:errcheck // best-effort cleanup on teardown

	gen := publisher.NewGenerator()
	publishLoop(ctx, pub, gen, testChannel, publishRate, duration, run.Collector, logger)

	// Phase 3: Drain (handled by defer pool.Drain())
	run.Collector.ConnectionsActive.Store(0)

	return &metrics.Report{
		TestType: "load",
		Status:   "pass",
		Metrics:  run.Collector.Snapshot(),
	}, nil
}

// runLoadChannelMode distributes connections across public, user-scoped, and group-scoped channels.
// Split: 40% public, 30% user-scoped, 30% group-scoped.
func runLoadChannelMode(ctx context.Context, run *TestRun, logger zerolog.Logger, connections, publishRate, rampRate int, duration time.Duration) (*metrics.Report, error) {
	provClient := run.authResult.ProvClient
	tenantID := run.authResult.TenantID

	// Setup: channel rules + routing rules
	if err := provClient.SetChannelRules(ctx, tenantID, testChannelRules); err != nil {
		return nil, fmt.Errorf("set channel rules: %w", err)
	}
	if err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules); err != nil {
		return nil, fmt.Errorf("set routing rules: %w", err)
	}

	// Split connections: 40% public, 30% user-scoped, 30% group
	publicCount := connections * 40 / 100
	userCount := connections * 30 / 100
	groupCount := connections - publicCount - userCount

	logger.Info().
		Int("total", connections).
		Int("public", publicCount).
		Int("user_scoped", userCount).
		Int("group_scoped", groupCount).
		Msg("channel mode: distributing connections")

	minter := run.authResult.Minter

	// Create 3 pools with different token functions
	publicPool := testerws.NewPool(logger)
	defer publicPool.Drain()

	userPool := testerws.NewPool(logger)
	defer userPool.Drain()

	groupPool := testerws.NewPool(logger)
	defer groupPool.Drain()

	// Public pool: default tokens, subscribe to general.test
	if publicCount > 0 {
		if err := publicPool.RampUp(ctx, testerws.PoolConfig{
			GatewayURL: run.Config.GatewayURL,
			TokenFunc:  run.authResult.TokenFunc,
			Channels:   []string{"general.test"},
			OnMessage: func(_ testerws.Message) {
				run.Collector.PublicReceived.Add(1)
				run.Collector.MessagesReceived.Add(1)
			},
		}, publicCount, rampRate); err != nil {
			return nil, fmt.Errorf("ramp up public pool: %w", err)
		}
	}

	// User-scoped pool: each connection subscribes to dm.<subject>
	if userCount > 0 {
		if err := userPool.RampUp(ctx, testerws.PoolConfig{
			GatewayURL: run.Config.GatewayURL,
			TokenFunc: func(i int) string {
				token, err := minter.MintWithClaims(auth.MintOptions{
					ConnIndex: 1000 + i,
					Subject:   fmt.Sprintf("load-user-%04d", i),
				})
				if err != nil {
					logger.Error().Err(err).Int("conn", i).Msg("mint user-scoped token failed")
					return "" // empty token → connection will fail, tracked by pool
				}
				return token
			},
			Channels: []string{"dm.{principal}"}, // gateway resolves {principal} per connection
			OnMessage: func(_ testerws.Message) {
				run.Collector.UserScopedReceived.Add(1)
				run.Collector.MessagesReceived.Add(1)
			},
		}, userCount, rampRate); err != nil {
			return nil, fmt.Errorf("ramp up user pool: %w", err)
		}
	}

	// Group pool: vip group, subscribe to room.vip
	if groupCount > 0 {
		if err := groupPool.RampUp(ctx, testerws.PoolConfig{
			GatewayURL: run.Config.GatewayURL,
			TokenFunc: func(i int) string {
				token, err := minter.MintWithClaims(auth.MintOptions{
					ConnIndex: 2000 + i,
					Subject:   fmt.Sprintf("load-group-%04d", i),
					Groups:    []string{"vip"},
				})
				if err != nil {
					logger.Error().Err(err).Int("conn", i).Msg("mint group-scoped token failed")
					return "" // empty token → connection will fail, tracked by pool
				}
				return token
			},
			Channels: []string{"room.vip"},
			OnMessage: func(_ testerws.Message) {
				run.Collector.GroupScopedReceived.Add(1)
				run.Collector.MessagesReceived.Add(1)
			},
		}, groupCount, rampRate); err != nil {
			return nil, fmt.Errorf("ramp up group pool: %w", err)
		}
	}

	totalActive := publicPool.Active() + userPool.Active() + groupPool.Active()
	run.Collector.ConnectionsActive.Store(totalActive)
	run.Collector.ConnectionsTotal.Store(totalActive)

	// Phase 2: Sustain — publish to all channel types at rate
	logger.Info().Int("rate", publishRate).Str("duration", duration.String()).Msg("sustaining channel-mode load")

	pub, err := publisher.NewDirectPublisher(ctx, run.Config.GatewayURL, run.authResult.TokenFunc(0))
	if err != nil {
		return nil, fmt.Errorf("create publisher: %w", err)
	}
	defer pub.Close() //nolint:errcheck // best-effort cleanup

	gen := publisher.NewGenerator()

	// Distribute publish rate: 50% public, 50% group.
	// User-scoped channels are receive-only (each user has a unique dm.<subject> channel —
	// publishing to individual channels at scale is impractical for load testing).
	publicRate := max(publishRate/2, 1)
	groupRate := max(publishRate-publicRate, 1)

	endTime := time.Now().Add(duration)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for time.Now().Before(endTime) {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			// Publish to public channel
			for range publicRate {
				data, _ := gen.Next("general.test") // json.Marshal on literal map of primitives cannot fail
				if err := pub.Publish(ctx, "general.test", data); err == nil {
					run.Collector.PublicSent.Add(1)
					run.Collector.MessagesSent.Add(1)
				}
			}
			// Publish to group channel
			for range groupRate {
				data, _ := gen.Next("room.vip") // json.Marshal on literal map of primitives cannot fail
				if err := pub.Publish(ctx, "room.vip", data); err == nil {
					run.Collector.GroupScopedSent.Add(1)
					run.Collector.MessagesSent.Add(1)
				}
			}
		}
	}

done:
	run.Collector.ConnectionsActive.Store(0)

	return &metrics.Report{
		TestType: "load:channels",
		Status:   "pass",
		Metrics:  run.Collector.Snapshot(),
	}, nil
}
