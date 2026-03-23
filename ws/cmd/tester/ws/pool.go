package ws

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/rs/zerolog"
)

// defaultPoolRampRate is the fallback connections-per-second rate when not specified.
const defaultPoolRampRate = 100

type Pool struct {
	mu             sync.Mutex
	clients        []*Client
	active         atomic.Int64
	lastDrainCount atomic.Int64
	wg             sync.WaitGroup
	logger         zerolog.Logger
}

func NewPool(logger zerolog.Logger) *Pool {
	return &Pool{logger: logger}
}

type PoolConfig struct {
	GatewayURL string
	Token      string
	APIKey     string
	Channels   []string
	OnMessage  func(Message)
}

// RampUp creates `count` connections at the given rate (connections per second).
func (p *Pool) RampUp(ctx context.Context, cfg PoolConfig, count int, rate int) error {
	if cfg.GatewayURL == "" {
		return fmt.Errorf("ramp up: gateway URL is required")
	}
	if rate <= 0 {
		rate = defaultPoolRampRate
	}
	interval := time.Second / time.Duration(rate)

	for i := range count {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		client, err := Connect(ctx, ConnectConfig{
			GatewayURL: cfg.GatewayURL,
			Token:      cfg.Token,
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
		p.wg.Add(1)
		go func() {
			defer logging.RecoverPanic(p.logger, "pool-read-loop", nil)
			defer p.wg.Done()
			client.ReadLoop(ctx)
		}()

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

func (p *Pool) Active() int64 {
	return p.active.Load()
}

func (p *Pool) Drain() {
	p.mu.Lock()
	clients := p.clients
	p.clients = nil
	p.mu.Unlock()

	count := p.active.Load()
	p.lastDrainCount.Store(count)

	var closeWg sync.WaitGroup
	for _, c := range clients {
		closeWg.Add(1)
		go func() {
			defer logging.RecoverPanic(p.logger, "pool-drain-close", nil)
			defer closeWg.Done()
			_ = c.Close() // best-effort: multi-step drain continues on individual close failure
		}()
	}
	closeWg.Wait()
	p.active.Add(-int64(len(clients)))

	// Wait for all read loop goroutines to finish
	p.wg.Wait()
}

func (p *Pool) DrainSummary() string {
	return fmt.Sprintf("drained %d connections", p.lastDrainCount.Load())
}
