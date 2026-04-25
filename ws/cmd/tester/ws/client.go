package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Client is a WebSocket client for the tester service.
type Client struct {
	conn      net.Conn
	rw        io.ReadWriter // reads from br (if Dial buffered data), writes to conn
	closeOnce sync.Once
	logger    zerolog.Logger
	onMsg     func(Message)
	mu        sync.Mutex // protects writeJSON only
}

// Message represents a WebSocket message received from the server.
type Message struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ConnectConfig holds parameters for establishing a WebSocket connection.
type ConnectConfig struct {
	GatewayURL string
	Token      string
	APIKey     string
	Logger     zerolog.Logger
	OnMessage  func(Message)
}

// Connect dials the gateway and returns a connected Client.
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
	conn, br, _, err := dialer.Dial(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}

	// br may contain bytes buffered during the handshake that haven't been
	// consumed yet. Use it as the reader so those bytes aren't lost.
	var rw io.ReadWriter = conn
	if br != nil {
		rw = struct {
			io.Reader
			io.Writer
		}{br, conn}
	}

	c := &Client{
		conn:   conn,
		rw:     rw,
		logger: cfg.Logger,
		onMsg:  cfg.OnMessage,
	}
	return c, nil
}

// Subscribe sends a subscribe request for the given channels.
func (c *Client) Subscribe(channels []string) error {
	return c.writeJSON(map[string]any{
		"type": "subscribe",
		"data": map[string]any{"channels": channels},
	})
}

// Unsubscribe sends an unsubscribe request for the given channels.
func (c *Client) Unsubscribe(channels []string) error {
	return c.writeJSON(map[string]any{
		"type": "unsubscribe",
		"data": map[string]any{"channels": channels},
	})
}

// Publish sends a message to the given channel.
func (c *Client) Publish(channel string, data json.RawMessage) error {
	return c.writeJSON(map[string]any{
		"type": "publish",
		"data": map[string]any{"channel": channel, "data": data},
	})
}

// RefreshToken sends an auth refresh message to the gateway with a new JWT.
// The gateway responds asynchronously via auth_ack or auth_error messages
// which are dispatched through the ReadLoop's onMsg callback.
func (c *Client) RefreshToken(token string) error {
	return c.writeJSON(map[string]any{
		"type": "auth",
		"data": map[string]any{"token": token},
	})
}

// ReadLoop reads messages until the context is canceled or the connection closes.
// Returns the WebSocket close code if the server sent a close frame, or 0 otherwise.
func (c *Client) ReadLoop(ctx context.Context) (ws.StatusCode, error) {
	// When the context is canceled, set a short deadline to unblock any
	// in-progress read. This avoids polling with read deadlines (which can
	// corrupt WebSocket framing if a timeout hits mid-frame).
	stop := context.AfterFunc(ctx, func() {
		if tc, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = tc.SetReadDeadline(time.Now().Add(shutdownReadDeadline))
		}
	})
	defer stop()

	for {
		data, err := wsutil.ReadServerText(c.rw)
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil
			}
			if closeErr, ok := errors.AsType[wsutil.ClosedError](err); ok {
				return closeErr.Code, nil
			}
			c.logger.Debug().Err(err).Msg("read error")
			return 0, fmt.Errorf("read loop: %w", err)
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

// Close closes the underlying WebSocket connection. Safe to call multiple times.
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
		return errors.New("connection closed")
	}
	if err := wsutil.WriteClientText(c.conn, data); err != nil {
		return fmt.Errorf("write ws message: %w", err)
	}
	return nil
}
