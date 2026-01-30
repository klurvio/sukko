package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
	"github.com/Toniq-Labs/odin-ws/pkg/logging"
)

// Proxy handles bidirectional WebSocket message forwarding between client and backend.
// It intercepts subscribe messages to filter channels based on permissions.
type Proxy struct {
	clientConn  net.Conn
	backendConn net.Conn
	claims      *auth.Claims
	permissions *PermissionChecker
	logger      zerolog.Logger

	messageTimeout time.Duration
}

// NewProxy creates a new proxy for a client-backend connection pair.
func NewProxy(
	clientConn, backendConn net.Conn,
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
		defer logging.RecoverPanic(p.logger, "proxyClientToBackend", nil)
		defer wg.Done()
		p.proxyClientToBackend(errChan)
	}()

	// Backend -> Client (pass-through)
	go func() {
		defer logging.RecoverPanic(p.logger, "proxyBackendToClient", nil)
		defer wg.Done()
		p.proxyBackendToClient(errChan)
	}()

	// Wait for first error (connection close)
	err := <-errChan
	if err != nil && !errors.Is(err, io.EOF) {
		p.logger.Warn().Err(err).Msg("Connection closed with error")
	} else {
		p.logger.Debug().Msg("Connection closed normally")
	}

	// Close both connections to unblock waiting goroutines
	_ = p.clientConn.Close()
	_ = p.backendConn.Close()

	// Wait for both goroutines to complete
	wg.Wait()
}

// proxyClientToBackend forwards messages from client to backend,
// intercepting subscribe messages to filter channels.
// Handles all frame types including ping/pong transparently.
func (p *Proxy) proxyClientToBackend(errChan chan error) {
	for {
		// Read frame header
		header, err := ws.ReadHeader(p.clientConn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				RecordProxyError("client_read_error")
				p.logger.Debug().Err(err).Msg("Client read error")
			}
			errChan <- err
			return
		}

		// Read payload
		payload := make([]byte, header.Length)
		if header.Length > 0 {
			if _, err := io.ReadFull(p.clientConn, payload); err != nil {
				RecordProxyError("client_read_error")
				p.logger.Debug().Err(err).Msg("Client payload read error")
				errChan <- err
				return
			}
		}

		// Unmask client frames (client->server frames are always masked)
		if header.Masked {
			ws.Cipher(payload, header.Mask, 0)
		}

		// Handle close frame
		if header.OpCode == ws.OpClose {
			p.logger.Debug().Msg("Client sent close frame")
			p.forwardCloseFrame(p.backendConn, payload, true)
			errChan <- nil
			return
		}

		// For text frames, intercept and possibly modify (subscribe filtering)
		if header.OpCode == ws.OpText {
			payload, _ = p.interceptClientMessage(payload)
		}

		// Record message metrics
		RecordMessage("client_to_backend", len(payload))

		// Forward frame to backend (re-mask for server)
		if err := p.forwardFrame(p.backendConn, header.OpCode, payload, header.Fin, true); err != nil {
			RecordProxyError("backend_write_error")
			p.logger.Debug().Err(err).Msg("Backend write error")
			errChan <- err
			return
		}
	}
}

// proxyBackendToClient forwards messages from backend to client (pass-through).
// Handles all frame types including ping/pong transparently.
func (p *Proxy) proxyBackendToClient(errChan chan error) {
	for {
		// Read frame header
		header, err := ws.ReadHeader(p.backendConn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				RecordProxyError("backend_read_error")
				p.logger.Debug().Err(err).Msg("Backend read error")
			}
			errChan <- err
			return
		}

		// Read payload
		payload := make([]byte, header.Length)
		if header.Length > 0 {
			if _, err := io.ReadFull(p.backendConn, payload); err != nil {
				RecordProxyError("backend_read_error")
				p.logger.Debug().Err(err).Msg("Backend payload read error")
				errChan <- err
				return
			}
		}

		// Server frames are not masked, no need to unmask

		// Handle close frame
		if header.OpCode == ws.OpClose {
			p.logger.Debug().Msg("Backend sent close frame")
			p.forwardCloseFrame(p.clientConn, payload, false)
			errChan <- nil
			return
		}

		// Record message metrics
		RecordMessage("backend_to_client", len(payload))

		// Forward all frames to client (server->client: no masking)
		if err := p.forwardFrame(p.clientConn, header.OpCode, payload, header.Fin, false); err != nil {
			RecordProxyError("client_write_error")
			p.logger.Debug().Err(err).Msg("Client write error")
			errChan <- err
			return
		}
	}
}

// forwardFrame forwards a WebSocket frame to the destination.
// If mask is true, the frame will be masked (required for client->server frames).
func (p *Proxy) forwardFrame(dst net.Conn, opCode ws.OpCode, payload []byte, fin bool, mask bool) error {
	header := ws.Header{
		Fin:    fin,
		OpCode: opCode,
		Length: int64(len(payload)),
	}

	if mask {
		header.Masked = true
		header.Mask = ws.NewMask()
		ws.Cipher(payload, header.Mask, 0)
	}

	if err := ws.WriteHeader(dst, header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := dst.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// forwardCloseFrame forwards a close frame to the destination.
func (p *Proxy) forwardCloseFrame(dst net.Conn, payload []byte, mask bool) {
	header := ws.Header{
		Fin:    true,
		OpCode: ws.OpClose,
		Length: int64(len(payload)),
	}
	if mask {
		header.Masked = true
		header.Mask = ws.NewMask()
		ws.Cipher(payload, header.Mask, 0)
	}
	_ = ws.WriteHeader(dst, header)
	if len(payload) > 0 {
		_, _ = dst.Write(payload)
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
	// If no claims or anonymous, allow all channels (auth disabled)
	if p.claims == nil || p.claims.Subject == "anonymous" {
		return msg, nil
	}

	var clientMsg ClientMessage
	if err := json.Unmarshal(msg, &clientMsg); err != nil {
		// Not valid JSON, pass through (intentional: non-JSON messages are passed unchanged)
		return msg, nil //nolint:nilerr // Intentional: pass through non-JSON messages
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

	// Log denied channels and record metrics
	for _, ch := range subData.Channels {
		if contains(allowedChannels, ch) {
			RecordChannelCheck("allowed")
		} else {
			RecordChannelCheck("denied")
			RecordAccessDenial("channel", "unauthorized")
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
	return slices.Contains(slice, val)
}
