package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
)

// shutdownReadDeadline is a shorter read deadline used during shutdown
// to unblock ReadLoop quickly when the context is canceled.
const shutdownReadDeadline = 100 * time.Millisecond

// readDeadlineTimeout is the read deadline for WebSocket connections in the read loop.
// Kept short (1s) so the loop re-checks ctx.Done frequently during shutdown.
const readDeadlineTimeout = 1 * time.Second

type Client struct {
	conn      net.Conn
	closeOnce sync.Once
	logger    zerolog.Logger
	onMsg     func(Message)
	mu        sync.Mutex // protects writeJSON only
}

type Message struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type ConnectConfig struct {
	GatewayURL string
	Token      string
	APIKey     string
	Logger     zerolog.Logger
	OnMessage  func(Message)
}

func Connect(ctx context.Context, cfg ConnectConfig) (*Client, error) {
	wsURL := cfg.GatewayURL + "/ws"
	header := http.Header{}
	if cfg.Token != "" {
		header.Set("Authorization", "Bearer "+cfg.Token)
	}
	if cfg.APIKey != "" {
		header.Set("X-API-Key", cfg.APIKey)
	}

	dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(header)}
	conn, _, _, err := dialer.Dial(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}

	c := &Client{
		conn:   conn,
		logger: cfg.Logger,
		onMsg:  cfg.OnMessage,
	}
	return c, nil
}

func (c *Client) Subscribe(channels []string) error {
	return c.writeJSON(map[string]any{
		"type": "subscribe",
		"data": map[string]any{"channels": channels},
	})
}

func (c *Client) Publish(channel string, data json.RawMessage) error {
	return c.writeJSON(map[string]any{
		"type": "publish",
		"data": map[string]any{"channel": channel, "data": data},
	})
}

func (c *Client) ReadLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// Set a short deadline to unblock any in-progress read quickly
			if tc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
				_ = tc.SetReadDeadline(time.Now().Add(shutdownReadDeadline))
			}
			return
		default:
		}

		// Set read deadline so we periodically re-check ctx.Done
		if tc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = tc.SetReadDeadline(time.Now().Add(readDeadlineTimeout)) // error ignored: next read will surface any connection issue
		}

		data, err := wsutil.ReadServerText(c.conn)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug().Err(err).Msg("read error")
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.logger.Debug().Err(err).Msg("unmarshal error")
			continue
		}

		if c.onMsg != nil {
			c.onMsg(msg)
		}
	}
}

func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		if c.conn != nil {
			if err := c.conn.Close(); err != nil {
				closeErr = fmt.Errorf("close websocket: %w", err)
			}
		}
	})
	return closeErr
}

func (c *Client) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// Lock held across I/O: gobwas/ws requires write serialization on a single
	// net.Conn. Acceptable for tester client where concurrent writes are infrequent.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("connection closed")
	}
	return wsutil.WriteClientText(c.conn, data)
}
