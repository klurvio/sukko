package orchestration

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// ShardProxy is a WebSocket proxy that forwards connections to a backend shard.
// Capacity control is handled by the shard's ResourceGuard - the proxy simply
// forwards the connection and lets the shard decide whether to accept it.
type ShardProxy struct {
	shard      *Shard
	backendURL *url.URL
	logger     zerolog.Logger

	upgrader websocket.Upgrader
	dialer   *websocket.Dialer

	// Timeouts
	dialTimeout    time.Duration
	messageTimeout time.Duration
}

// NewShardProxy creates a new WebSocket proxy for a specific shard.
func NewShardProxy(shard *Shard, backendURL *url.URL, logger zerolog.Logger) *ShardProxy {
	return &ShardProxy{
		shard:      shard,
		backendURL: backendURL,
		logger:     logger.With().Str("component", "proxy").Int("shard_id", shard.ID).Logger(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins (LoadBalancer handles CORS)
			},
		},
		dialer: &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 10 * time.Second,
		},
		dialTimeout:    10 * time.Second,
		messageTimeout: 60 * time.Second,
	}
}

// ServeHTTP handles the WebSocket proxy request.
// The proxy upgrades the client connection, then dials the backend shard.
// Capacity control is delegated to the shard's ResourceGuard which will
// return 503 if the shard is at capacity (connection limit, CPU, memory, etc.)
func (p *ShardProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// STEP 1: Upgrade client to WebSocket
	clientConn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logger.Warn().
			Err(err).
			Str("remote_addr", r.RemoteAddr).
			Msg("Client WebSocket upgrade failed")
		return
	}
	defer func() { _ = clientConn.Close() }()

	p.logger.Debug().
		Str("remote_addr", r.RemoteAddr).
		Msg("Client upgraded, connecting to backend shard")

	// STEP 2: Connect to backend shard
	ctx, cancel := context.WithTimeout(context.Background(), p.dialTimeout)
	defer cancel()

	// DEBUG: Log before backend dial attempt
	dialStart := time.Now()
	p.logger.Debug().
		Str("backend_url", p.backendURL.String()).
		Dur("dial_timeout", p.dialTimeout).
		Str("client_remote_addr", r.RemoteAddr).
		Msg("Attempting to dial backend shard")

	backendConn, resp, err := p.dialer.DialContext(ctx, p.backendURL.String(), nil)
	dialDuration := time.Since(dialStart)

	if err != nil {
		// DEBUG: Enhanced backend dial failure logging
		logEvent := p.logger.Error().
			Err(err).
			Str("backend_url", p.backendURL.String()).
			Dur("dial_duration_ms", dialDuration).
			Dur("elapsed_since_start_ms", time.Since(startTime)).
			Str("client_remote_addr", r.RemoteAddr).
			Int("shard_id", p.shard.ID)

		// Add HTTP response details if available
		if resp != nil {
			logEvent = logEvent.
				Int("http_status", resp.StatusCode).
				Str("http_status_text", resp.Status)
		}

		logEvent.Msg("Backend dial failed")

		// Send error to client
		// Client receives: WebSocket Close Frame (code: 1011, reason: "Backend unavailable")
		// See: docs/API_REJECTION_RESPONSES.md (Scenario 6)
		closeMsg := websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Backend unavailable")
		_ = clientConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		return
	}
	defer func() { _ = backendConn.Close() }()

	// DEBUG: Backend dial successful
	p.logger.Debug().
		Str("backend_url", p.backendURL.String()).
		Dur("dial_duration_ms", dialDuration).
		Int("shard_id", p.shard.ID).
		Msg("Backend dial successful")

	p.logger.Debug().
		Str("backend_url", p.backendURL.String()).
		Msg("Backend connected, starting bidirectional proxy")

	// STEP 3: Proxy messages bidirectionally
	p.proxyMessages(clientConn, backendConn)

	p.logger.Info().
		Dur("total_duration", time.Since(startTime)).
		Msg("Connection closed normally")
}

// proxyMessages forwards WebSocket messages bidirectionally between client and backend.
// Uses goroutines for concurrent forwarding in both directions.
// Returns when either connection closes or errors, after ensuring both goroutines complete.
func (p *ShardProxy) proxyMessages(client, backend *websocket.Conn) {
	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend
	go func() {
		defer wg.Done()
		p.copyMessages(client, backend, "client->backend", errChan)
	}()

	// Backend -> Client
	go func() {
		defer wg.Done()
		p.copyMessages(backend, client, "backend->client", errChan)
	}()

	// Wait for first error (connection close)
	err := <-errChan
	if err != nil {
		// Check if it's a normal closure
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			p.logger.Debug().Msg("Connection closed normally")
		} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
			p.logger.Warn().Err(err).Msg("Unexpected connection close")
		}
	}

	// Close both connections to unblock any goroutines waiting on read/write
	_ = client.Close()
	_ = backend.Close()

	// Wait for both goroutines to complete before returning
	wg.Wait()
}

// copyMessages copies WebSocket messages from src to dst.
// Handles all message types (text, binary, close, ping, pong).
// Sends error to errChan when connection closes or fails.
func (p *ShardProxy) copyMessages(src, dst *websocket.Conn, direction string, errChan chan error) {
	for {
		messageType, message, err := src.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				p.logger.Debug().
					Err(err).
					Str("direction", direction).
					Msg("Connection closed")
			}
			errChan <- err
			return
		}

		err = dst.WriteMessage(messageType, message)
		if err != nil {
			p.logger.Error().
				Err(err).
				Str("direction", direction).
				Msg("Write failed")
			errChan <- err
			return
		}
	}
}
