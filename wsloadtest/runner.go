package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
)

// LoadRunner coordinates the load test
type LoadRunner struct {
	config      *Config
	logger      zerolog.Logger
	stats       *Stats
	connections []*Connection
	token       string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
}

// NewLoadRunner creates a new load runner
func NewLoadRunner(config *Config, logger zerolog.Logger) *LoadRunner {
	return &LoadRunner{
		config:      config,
		logger:      logger,
		stats:       NewStats(),
		connections: make([]*Connection, 0, config.TargetConnections),
	}
}

// Run executes the full load test lifecycle
func (r *LoadRunner) Run(ctx context.Context) error {
	r.ctx, r.cancel = context.WithCancel(ctx)
	defer r.cancel()

	// Generate or use provided token
	if err := r.setupToken(); err != nil {
		return fmt.Errorf("setup token: %w", err)
	}

	// Start health checker
	if r.config.HealthURL != "" {
		healthChecker := NewHealthChecker(r.config.HealthURL, r.config.HealthInterval, r.logger)

		// Wait for server to be healthy before starting
		r.logger.Info().Str("url", r.config.HealthURL).Msg("Waiting for server to be healthy")
		if err := healthChecker.WaitForHealthy(r.ctx, 30*time.Second); err != nil {
			r.logger.Warn().Err(err).Msg("Server health check failed, continuing anyway")
		}

		r.wg.Go(func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.logger.Error().Interface("panic", rec).Msg("Panic in health checker")
				}
			}()
			healthChecker.Run(r.ctx)
		})
	}

	// Start stats reporter
	r.wg.Add(1)
	go r.statsReporter()

	// Ramp up connections
	if err := r.rampUp(); err != nil {
		return fmt.Errorf("ramp up: %w", err)
	}

	// Sustain phase
	r.sustain()

	// Shutdown
	r.shutdown()

	// Wait for background workers
	r.wg.Wait()

	// Final report
	r.stats.LogFinalReport(r.logger)

	return nil
}

// setupToken generates a JWT token if needed
func (r *LoadRunner) setupToken() error {
	if r.config.Token != "" {
		r.token = r.config.Token
		return nil
	}

	if r.config.JWTSecret == "" {
		// No auth configured
		return nil
	}

	// Generate JWT token
	claims := jwt.MapClaims{
		"sub":       r.config.Principal,
		"tenant_id": r.config.TenantID,
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(r.config.JWTSecret))
	if err != nil {
		return fmt.Errorf("sign token: %w", err)
	}

	r.token = signedToken
	r.logger.Debug().Msg("Generated JWT token")
	return nil
}

// rampUp creates connections at the configured rate
func (r *LoadRunner) rampUp() error {
	r.logger.Info().
		Int("target_connections", r.config.TargetConnections).
		Int("ramp_rate", r.config.RampRate).
		Msg("Starting ramp-up")

	r.stats.SetPhase(PhaseRamping)

	// Calculate interval between connections to achieve ramp rate
	interval := time.Second / time.Duration(r.config.RampRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for i := range r.config.TargetConnections {
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-ticker.C:
			if err := r.createConnection(i); err != nil {
				r.logger.Debug().Err(err).Int("id", i).Msg("Failed to create connection")
				r.stats.FailedConnections.Add(1)
				r.stats.RecordError("connection_failed")
			}
		}
	}

	r.logger.Info().
		Int64("active_connections", r.stats.ActiveConnections.Load()).
		Int64("failed_connections", r.stats.FailedConnections.Load()).
		Msg("Ramp-up complete")

	return nil
}

// createConnection creates a single connection and subscribes to channels
func (r *LoadRunner) createConnection(id int) error {
	// Dial WebSocket
	conn, err := Dial(r.ctx, r.config.WSURL, r.token, r.config.ConnectionTimeout)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	r.stats.TotalCreated.Add(1)
	r.stats.ActiveConnections.Add(1)

	// Select channels based on subscription mode
	channels := r.selectChannels()

	// Create connection wrapper
	c := NewConnection(r.ctx, id, conn, channels, r.stats, r.logger, r.config.PongWait, r.config.PingPeriod)

	// Store connection
	r.mu.Lock()
	r.connections = append(r.connections, c)
	r.mu.Unlock()

	// Start read/write pumps
	c.Start()

	// Subscribe to channels
	if err := c.Subscribe(); err != nil {
		r.logger.Debug().Err(err).Int("id", id).Msg("Failed to subscribe")
		r.stats.RecordError("subscribe_failed")
	}

	r.logger.Debug().Int("id", id).Strs("channels", channels).Msg("Connection established")

	return nil
}

// selectChannels selects channels based on the subscription mode
func (r *LoadRunner) selectChannels() []string {
	switch r.config.SubscriptionMode {
	case "all":
		return r.config.Channels

	case "single":
		// Pick a random single channel
		idx := rand.Intn(len(r.config.Channels))
		return []string{r.config.Channels[idx]}

	case "random":
		// Pick random subset of channels
		n := min(r.config.ChannelsPerClient, len(r.config.Channels))

		// Shuffle and take first n
		perm := rand.Perm(len(r.config.Channels))
		selected := make([]string, n)
		for i := range n {
			selected[i] = r.config.Channels[perm[i]]
		}
		return selected

	default:
		return r.config.Channels
	}
}

// sustain maintains connections for the configured duration
func (r *LoadRunner) sustain() {
	r.stats.SetPhase(PhaseSustaining)

	r.logger.Info().
		Dur("duration", r.config.SustainDuration).
		Msg("Starting sustain phase")

	select {
	case <-r.ctx.Done():
		return
	case <-time.After(r.config.SustainDuration):
		r.logger.Info().Msg("Sustain phase complete")
	}
}

// shutdown gracefully closes all connections
func (r *LoadRunner) shutdown() {
	r.stats.SetPhase(PhaseCompleted)
	r.logger.Info().Msg("Shutting down connections")

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, c := range r.connections {
		c.Close()
	}
}

// statsReporter periodically logs stats
func (r *LoadRunner) statsReporter() {
	defer r.wg.Done()
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error().Interface("panic", rec).Msg("Panic in stats reporter")
		}
	}()

	ticker := time.NewTicker(r.config.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.stats.LogReport(r.logger)
		}
	}
}
