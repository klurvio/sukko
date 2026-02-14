package main

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Maximum message size allowed from peer
	maxMessageSize = 512 * 1024 // 512KB
)

// Connection represents a single WebSocket client
type Connection struct {
	id     int
	ws     *websocket.Conn
	logger zerolog.Logger
	stats  *Stats

	// Subscription state
	channels   []string
	subscribed atomic.Bool

	// WebSocket ping/pong timing
	pongWait   time.Duration
	pingPeriod time.Duration

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	writeMu   sync.Mutex

	connectedAt time.Time
}

// NewConnection creates a new connection
func NewConnection(ctx context.Context, id int, ws *websocket.Conn, channels []string, stats *Stats, logger zerolog.Logger, pongWait, pingPeriod time.Duration) *Connection {
	connCtx, cancel := context.WithCancel(ctx)
	return &Connection{
		id:          id,
		ws:          ws,
		channels:    channels,
		stats:       stats,
		logger:      logger.With().Int("conn_id", id).Logger(),
		pongWait:    pongWait,
		pingPeriod:  pingPeriod,
		ctx:         connCtx,
		cancel:      cancel,
		connectedAt: time.Now(),
	}
}

// Start starts the connection's read and write pumps
func (c *Connection) Start() {
	go c.readPump()
	go c.writePump()
}

// Subscribe sends a subscribe message for the connection's channels
func (c *Connection) Subscribe() error {
	msg, err := NewSubscribeMessage(c.channels)
	if err != nil {
		return err
	}

	c.stats.SubscriptionsSent.Add(1)
	c.logger.Debug().Strs("channels", c.channels).Msg("Sending subscribe")

	return c.writeMessage(websocket.TextMessage, msg)
}

// Close closes the connection
func (c *Connection) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		if c.ws != nil {
			c.ws.Close()
		}
		c.stats.ActiveConnections.Add(-1)
		c.logger.Debug().Msg("Connection closed")
	})
}

// readPump reads messages from the WebSocket connection
func (c *Connection) readPump() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error().
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("Panic in readPump")
		}
	}()
	defer c.Close()

	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(c.pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(c.pongWait))
		c.logger.Debug().Msg("Received pong, refreshed read deadline")
		return nil
	})

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		_, message, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Debug().Err(err).Msg("Read error")
				c.stats.RecordError("read_error")
			}
			return
		}

		c.handleMessage(message)
	}
}

// writePump sends pings to keep the connection alive
func (c *Connection) writePump() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error().
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("Panic in writePump")
		}
	}()
	defer c.Close()

	ticker := time.NewTicker(c.pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.writeMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Debug().Err(err).Msg("Ping error")
				return
			}
			c.logger.Debug().Msg("Sent ping to server")
		}
	}
}

// handleMessage processes an incoming message
func (c *Connection) handleMessage(data []byte) {
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Warn().Err(err).Msg("Failed to unmarshal message")
		return
	}

	switch msg.Type {
	case RespTypeSubscriptionAck:
		var ack SubscriptionAck
		if err := json.Unmarshal(data, &ack); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to unmarshal subscription_ack")
			return
		}
		c.subscribed.Store(true)
		c.stats.SubscriptionsConfirmed.Add(1)
		c.logger.Debug().
			Strs("subscribed", ack.Subscribed).
			Int("count", ack.Count).
			Msg("Received subscription_ack")

	case RespTypeUnsubscriptionAck:
		var ack UnsubscriptionAck
		if err := json.Unmarshal(data, &ack); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to unmarshal unsubscription_ack")
			return
		}
		c.logger.Debug().
			Strs("unsubscribed", ack.Unsubscribed).
			Int("count", ack.Count).
			Msg("Received unsubscription_ack")

	case RespTypeMessage:
		var env MessageEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to unmarshal message envelope")
			return
		}
		c.stats.RecordChannelMessage(env.Channel)
		c.logger.Trace().
			Str("channel", env.Channel).
			Int64("seq", env.Seq).
			Msg("Received message")

	case RespTypePong:
		c.logger.Trace().Msg("Received pong")

	case RespTypeSubscribeError:
		var errResp ErrorResponse
		if err := json.Unmarshal(data, &errResp); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to unmarshal subscribe_error")
			return
		}
		c.stats.SubscriptionsFailed.Add(1)
		c.stats.RecordError("subscribe_error:" + errResp.Code)
		c.logger.Warn().
			Str("code", errResp.Code).
			Str("message", errResp.Message).
			Msg("Received subscribe_error")

	case RespTypeError:
		var errResp ErrorResponse
		if err := json.Unmarshal(data, &errResp); err != nil {
			c.logger.Warn().Err(err).Msg("Failed to unmarshal error")
			return
		}
		c.stats.RecordError("error:" + errResp.Code)
		c.logger.Warn().
			Str("code", errResp.Code).
			Str("message", errResp.Message).
			Msg("Received error")

	default:
		c.logger.Debug().Str("type", msg.Type).Msg("Received unknown message type")
	}
}

// writeMessage writes a message to the WebSocket connection
func (c *Connection) writeMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(messageType, data)
}

// Dial creates a new WebSocket connection to the given URL
func Dial(ctx context.Context, url string, token string, timeout time.Duration) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: timeout,
	}

	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
