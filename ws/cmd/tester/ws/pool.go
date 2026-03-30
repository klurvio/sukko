package ws

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/rs/zerolog"
)

// defaultPoolRampRate is the fallback connections-per-second rate when not specified.
const defaultPoolRampRate = 100

// Pool manages a set of WebSocket client connections.
type Pool struct {
	mu             sync.Mutex
	clients        []*Client
	active         atomic.Int64
	lastDrainCount atomic.Int64
	wg             sync.WaitGroup
	logger         zerolog.Logger
}

// NewPool creates a Pool with the given logger.
func NewPool(logger zerolog.Logger) *Pool {
	return &Pool{logger: logger}
}

// PoolConfig configures connections created during ramp-up.
type PoolConfig struct {
	GatewayURL string
	Token      string                     // static token (backward compat)
	TokenFunc  func(connIndex int) string // per-connection token (takes precedence over Token)
	APIKey     string
	Channels   []string
	OnMessage  func(Message)
}

// RampUp creates `count` connections at the given rate (connections per second).
func (p *Pool) RampUp(ctx context.Context, cfg PoolConfig, count, rate int) error {
	if cfg.GatewayURL == "" {
		return errors.New("ramp up: gateway URL is required")
	}
	if rate <= 0 {
		rate = defaultPoolRampRate
	}
	interval := time.Second / time.Duration(rate)

	for i := range count {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ramp up: %w", ctx.Err())
		default:
		}

		token := cfg.Token
		if cfg.TokenFunc != nil {
			token = cfg.TokenFunc(i)
		}

		client, err := Connect(ctx, ConnectConfig{
			GatewayURL: cfg.GatewayURL,
			Token:      token,
			APIKey:     cfg.APIKey,
			Logger:     p.logger.With().Int("client", i).Logger(),
			OnMessage:  cfg.OnMessage,
		})
		if err != nil {
			p.logger.Warn().Err(err).Int("client", i).Msg("connection failed")
			continue
		}

		if len(cfg.Channels) > 0 {
			if err := client.Subscribe(cfg.Channels); err != nil {
				p.logger.Warn().Err(err).Msg("subscribe failed")
				_ = client.Close() // best-effort: subscribe already failed
				continue
			}
		}

		// Start read loop
		p.wg.Go(func() {
			defer logging.RecoverPanic(p.logger, "pool-read-loop", nil)
			client.ReadLoop(ctx)
		})

		p.mu.Lock()
		p.clients = append(p.clients, client)
		p.mu.Unlock()
		p.active.Add(1)

		if i < count-1 {
			time.Sleep(interval)
		}
	}

	return nil
}

// Active returns the number of currently active connections.
func (p *Pool) Active() int64 {
	return p.active.Load()
}

// Drain closes all connections and waits for read loops to finish.
func (p *Pool) Drain() {
	p.mu.Lock()
	clients := p.clients
	p.clients = nil
	p.mu.Unlock()

	count := p.active.Load()
	p.lastDrainCount.Store(count)

	var closeWg sync.WaitGroup
	for _, c := range clients {
		closeWg.Go(func() {
			defer logging.RecoverPanic(p.logger, "pool-drain-close", nil)
			_ = c.Close() // best-effort: multi-step drain continues on individual close failure
		})
	}
	closeWg.Wait()
	p.active.Add(-int64(len(clients)))

	// Wait for all read loop goroutines to finish
	p.wg.Wait()
}

// RefreshAll sends auth refresh messages to all active connections with freshly
// minted tokens. Returns the count of successful and failed refreshes.
// Auth refresh is fire-and-forget at the send level — gateway responses
// (auth_ack/auth_error) arrive asynchronously via the ReadLoop's onMsg callback.
func (p *Pool) RefreshAll(tokenFunc func(connIndex int) string) (refreshed, failed int) {
	p.mu.Lock()
	clients := append([]*Client(nil), p.clients...)
	p.mu.Unlock()

	for i, c := range clients {
		token := tokenFunc(i)
		if err := c.RefreshToken(token); err != nil {
			p.logger.Warn().Err(err).Int("client", i).Msg("auth refresh failed")
			failed++
		} else {
			refreshed++
		}
	}
	return refreshed, failed
}

// DrainSummary returns a human-readable summary of the last drain operation.
func (p *Pool) DrainSummary() string {
	return fmt.Sprintf("drained %d connections", p.lastDrainCount.Load())
}
