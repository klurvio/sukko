package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
)

// Mode selects the message publishing transport.
type Mode string

// ModeDirect publishes via WebSocket; ModeKafka publishes via Kafka.
const (
	ModeDirect Mode = "direct"
	ModeKafka  Mode = "kafka"
)

// gatewayWSPath is the WebSocket endpoint path on the gateway.
const gatewayWSPath = "/ws"

// Publisher sends test messages to a channel via WebSocket or Kafka.
type Publisher struct {
	mu        sync.Mutex // protects publishDirect write serialization only
	closeOnce sync.Once
	mode      Mode
	generator *Generator
	logger    zerolog.Logger
	// Direct mode
	conn net.Conn
}

// Config holds Publisher construction parameters.
type Config struct {
	Mode         Mode
	GatewayURL   string
	KafkaBrokers string
	Token        string
	Logger       zerolog.Logger
}

// New creates a Publisher with the given configuration.
func New(ctx context.Context, cfg Config) (*Publisher, error) {
	p := &Publisher{
		mode:      cfg.Mode,
		generator: NewGenerator(),
		logger:    cfg.Logger,
	}

	switch cfg.Mode {
	case ModeDirect:
		wsURL := cfg.GatewayURL + gatewayWSPath
		header := http.Header{}
		if cfg.Token != "" {
			header.Set("Authorization", "Bearer "+cfg.Token)
		}
		dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(header)}
		conn, _, _, err := dialer.Dial(ctx, wsURL)
		if err != nil {
			return nil, fmt.Errorf("dial gateway for publishing: %w", err)
		}
		p.conn = conn
	case ModeKafka:
		// Kafka publisher would be initialized here with franz-go
		// For now, log that kafka mode is selected
		p.logger.Info().Str("brokers", cfg.KafkaBrokers).Msg("kafka publisher initialized")
	default:
		return nil, fmt.Errorf("unsupported publisher mode: %s", cfg.Mode)
	}

	return p, nil
}

// Publish sends a single generated message to the given channel.
func (p *Publisher) Publish(ctx context.Context, channel string) error {
	data, err := p.generator.Next(channel)
	if err != nil {
		return fmt.Errorf("generate message: %w", err)
	}

	switch p.mode {
	case ModeDirect:
		return p.publishDirect(channel, data)
	case ModeKafka:
		return p.publishKafka(ctx, channel, data)
	default:
		return fmt.Errorf("unsupported mode: %s", p.mode)
	}
}

// PublishAtRate publishes messages at the given rate for the specified duration.
func (p *Publisher) PublishAtRate(ctx context.Context, channel string, rate int, duration time.Duration) error {
	if rate <= 0 {
		return errors.New("rate must be positive")
	}

	interval := time.Second / time.Duration(rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.After(duration)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("publish at rate: %w", ctx.Err())
		case <-deadline:
			return nil
		case <-ticker.C:
			if err := p.Publish(ctx, channel); err != nil {
				p.logger.Warn().Err(err).Msg("publish failed")
			}
		}
	}
}

// Close closes the publisher connection. Safe to call multiple times.
func (p *Publisher) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.conn != nil {
			if err := p.conn.Close(); err != nil {
				closeErr = fmt.Errorf("close publisher: %w", err)
			}
			p.conn = nil
		}
	})
	return closeErr
}

func (p *Publisher) publishDirect(channel string, data json.RawMessage) error {
	msg := map[string]any{
		"type": "publish",
		"data": map[string]any{
			"channel": channel,
			"data":    data,
		},
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// Lock held across I/O: gobwas/ws requires write serialization on a single
	// connection. A channel-based writer is unnecessary for a test tool with
	// a single publisher goroutine.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return errors.New("publisher connection closed")
	}
	if err := wsutil.WriteClientText(p.conn, payload); err != nil {
		return fmt.Errorf("write ws message: %w", err)
	}
	return nil
}

func (p *Publisher) publishKafka(_ context.Context, channel string, _ json.RawMessage) error {
	// TODO: Implement Kafka publishing with franz-go
	p.logger.Debug().Str("channel", channel).Msg("kafka publish (not yet implemented)")
	return nil
}
