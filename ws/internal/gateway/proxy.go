package gateway

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
)

// Proxy handles bidirectional WebSocket message forwarding between client and backend.
// It intercepts subscribe messages to filter channels based on permissions.
type Proxy struct {
	clientConn  *websocket.Conn
	backendConn *websocket.Conn
	claims      *auth.Claims
	permissions *PermissionChecker
	logger      zerolog.Logger

	messageTimeout time.Duration
}

// NewProxy creates a new proxy for a client-backend connection pair.
func NewProxy(
	clientConn, backendConn *websocket.Conn,
	claims *auth.Claims,
	permissions *PermissionChecker,
	logger zerolog.Logger,
	messageTimeout time.Duration,
) *Proxy {
	return &Proxy{
		clientConn:     clientConn,
		backendConn:    backendConn,
		claims:         claims,
		permissions:    permissions,
		logger:         logger,
		messageTimeout: messageTimeout,
	}
}

// Run starts bidirectional message forwarding.
// Blocks until either connection closes or errors.
func (p *Proxy) Run() {
	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend (with message interception)
	go func() {
		defer wg.Done()
		p.proxyClientToBackend(errChan)
	}()

	// Backend -> Client (pass-through)
	go func() {
		defer wg.Done()
		p.proxyBackendToClient(errChan)
	}()

	// Wait for first error (connection close)
	err := <-errChan
	if err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			p.logger.Debug().Msg("Connection closed normally")
		} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
			p.logger.Warn().Err(err).Msg("Unexpected connection close")
		}
	}

	// Close both connections to unblock waiting goroutines
	_ = p.clientConn.Close()
	_ = p.backendConn.Close()

	// Wait for both goroutines to complete
	wg.Wait()
}

// proxyClientToBackend forwards messages from client to backend,
// intercepting subscribe messages to filter channels.
func (p *Proxy) proxyClientToBackend(errChan chan error) {
	for {
		messageType, message, err := p.clientConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				p.logger.Debug().Err(err).Msg("Client connection closed")
			}
			errChan <- err
			return
		}

		// Intercept and possibly modify the message
		modifiedMessage, err := p.interceptClientMessage(message)
		if err != nil {
			p.logger.Error().Err(err).Msg("Failed to intercept client message")
			// Forward original message on error
			modifiedMessage = message
		}

		// Forward to backend
		if err := p.backendConn.WriteMessage(messageType, modifiedMessage); err != nil {
			p.logger.Error().Err(err).Msg("Failed to write to backend")
			errChan <- err
			return
		}
	}
}

// proxyBackendToClient forwards messages from backend to client (pass-through).
func (p *Proxy) proxyBackendToClient(errChan chan error) {
	for {
		messageType, message, err := p.backendConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				p.logger.Debug().Err(err).Msg("Backend connection closed")
			}
			errChan <- err
			return
		}

		// Pass through to client
		if err := p.clientConn.WriteMessage(messageType, message); err != nil {
			p.logger.Error().Err(err).Msg("Failed to write to client")
			errChan <- err
			return
		}
	}
}

// ClientMessage represents a message from the client.
type ClientMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// SubscribeData represents the data for a subscribe message.
type SubscribeData struct {
	Channels []string `json:"channels"`
}

// interceptClientMessage intercepts client messages, filtering subscribe requests.
func (p *Proxy) interceptClientMessage(msg []byte) ([]byte, error) {
	var clientMsg ClientMessage
	if err := json.Unmarshal(msg, &clientMsg); err != nil {
		// Not valid JSON, pass through
		return msg, nil
	}

	// Only intercept subscribe messages
	if clientMsg.Type != "subscribe" {
		return msg, nil
	}

	// Parse subscribe data
	var subData SubscribeData
	if err := json.Unmarshal(clientMsg.Data, &subData); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to parse subscribe data")
		return msg, nil
	}

	// Filter channels based on permissions
	allowedChannels := p.permissions.FilterChannels(p.claims, subData.Channels)

	// Log denied channels
	for _, ch := range subData.Channels {
		if !contains(allowedChannels, ch) {
			p.logger.Warn().
				Str("channel", ch).
				Str("principal", p.claims.Subject).
				Msg("Subscription denied")
		}
	}

	// If all channels were allowed, pass through original message
	if len(allowedChannels) == len(subData.Channels) {
		return msg, nil
	}

	// Create modified message with only allowed channels
	modifiedData := SubscribeData{Channels: allowedChannels}
	dataBytes, err := json.Marshal(modifiedData)
	if err != nil {
		return msg, err
	}

	modifiedMsg := ClientMessage{
		Type: "subscribe",
		Data: dataBytes,
	}

	return json.Marshal(modifiedMsg)
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
