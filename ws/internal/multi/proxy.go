package multi

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// SlotAwareProxy is a WebSocket proxy with guaranteed slot lifecycle management.
// It solves the slot leak bug by acquiring slots AFTER successful WebSocket upgrade,
// ensuring slots are only held for valid connections and always released.
type SlotAwareProxy struct {
	shard      *Shard
	backendURL *url.URL
	logger     zerolog.Logger

	upgrader websocket.Upgrader
	dialer   *websocket.Dialer

	// Timeouts
	dialTimeout    time.Duration
	messageTimeout time.Duration
}

// NewSlotAwareProxy creates a new WebSocket proxy for a specific shard.
// The proxy guarantees slot release in all code paths, preventing resource leaks.
func NewSlotAwareProxy(shard *Shard, backendURL *url.URL, logger zerolog.Logger) *SlotAwareProxy {
	return &SlotAwareProxy{
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

// ServeHTTP handles the WebSocket proxy request with guaranteed slot management.
//
// CRITICAL BUG FIX: This implementation solves the slot leak bug by:
// 1. Upgrading to WebSocket BEFORE acquiring slot
// 2. Only acquiring slot if upgrade succeeds
// 3. Using defer to GUARANTEE slot release
// 4. Proper cleanup in ALL error paths
//
// Previous bug: koding/websocketproxy could leak slots when handshake failed
// after HTTP hijack. This implementation makes slot leaks IMPOSSIBLE by design.
func (p *SlotAwareProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// STEP 1: Upgrade client to WebSocket (NO slot acquired yet!)
	// If this fails, no slot to leak - perfect!
	clientConn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade failed - no slot acquired, nothing to clean up
		p.logger.Warn().
			Err(err).
			Str("remote_addr", r.RemoteAddr).
			Msg("Client WebSocket upgrade failed")
		return
	}

	// STEP 2: Try to acquire slot (upgrade succeeded)
	// Client receives: WebSocket Close Frame (code: 1012, reason: "Server overloaded")
	// See: docs/API_REJECTION_RESPONSES.md (Scenario 5)
	if !p.shard.TryAcquireSlot() {
		p.logger.Warn().
			Int("available_slots", p.shard.GetAvailableSlots()).
			Msg("No available slots in shard")
		// Send close message to client
		closeMsg := websocket.FormatCloseMessage(websocket.CloseServiceRestart, "Server overloaded")
		_ = clientConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		_ = clientConn.Close()
		return
	}

	// STEP 3: Guarantee slot release with defer
	// This ensures slot is ALWAYS released, even if:
	// - Backend dial fails
	// - Message proxying errors
	// - Panic occurs
	// - Any other error
	slotReleased := false
	slotReleaseMutex := sync.Mutex{}
	releaseSlot := func() {
		slotReleaseMutex.Lock()
		defer slotReleaseMutex.Unlock()
		if !slotReleased {
			p.shard.ReleaseSlot()
			slotReleased = true
			p.logger.Debug().
				Dur("duration", time.Since(startTime)).
				Int("available_slots", p.shard.GetAvailableSlots()).
				Msg("Released slot")
		}
	}
	defer releaseSlot()
	defer func() { _ = clientConn.Close() }()

	p.logger.Info().
		Int("available_slots", p.shard.GetAvailableSlots()).
		Str("remote_addr", r.RemoteAddr).
		Msg("Slot acquired, proxying connection to shard")

	// STEP 4: Connect to backend shard
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
		return // Slot released by defer
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

	// STEP 5: Proxy messages bidirectionally
	p.proxyMessages(clientConn, backendConn)

	p.logger.Info().
		Dur("total_duration", time.Since(startTime)).
		Msg("Connection closed normally")
}

// proxyMessages forwards WebSocket messages bidirectionally between client and backend.
// Uses goroutines for concurrent forwarding in both directions.
// Returns when either connection closes or errors, after ensuring both goroutines complete.
func (p *SlotAwareProxy) proxyMessages(client, backend *websocket.Conn) {
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
func (p *SlotAwareProxy) copyMessages(src, dst *websocket.Conn, direction string, errChan chan error) {
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
