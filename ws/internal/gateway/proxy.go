package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	"github.com/Toniq-Labs/odin-ws/internal/shared/protocol"
)

// Proxy handles bidirectional WebSocket message forwarding between client and backend.
// It intercepts subscribe and publish messages to:
// - Validate tenant prefix on all channels (always)
// - Filter subscribe channels based on permissions (auth only)
// - Validate publish rate limits and message size
type Proxy struct {
	clientConn    net.Conn
	clientWriteMu sync.Mutex // protects all writes to clientConn
	backendConn   net.Conn
	logger        zerolog.Logger

	messageTimeout time.Duration

	// Auth (only used when authEnabled=true)
	claims      *auth.Claims
	permissions *PermissionChecker
	authEnabled bool

	// Tenant (always set — from JWT when auth enabled, from config when disabled)
	tenantID string

	// Publish rate limiting and validation
	publishLimiter *rate.Limiter
	maxPublishSize int
}

// ProxyConfig holds configuration for creating a Proxy.
type ProxyConfig struct {
	ClientConn     net.Conn
	BackendConn    net.Conn
	Logger         zerolog.Logger
	MessageTimeout time.Duration

	// Auth (claims is nil when auth disabled — proxy won't use it)
	AuthEnabled bool
	Claims      *auth.Claims
	Permissions *PermissionChecker

	// Tenant (always set — from JWT or config)
	TenantID string

	// Publish rate limiting (tokens per second, burst size)
	// Default: 10/sec, 100 burst
	PublishRateLimit float64
	PublishBurst     int

	// Max publish message size in bytes (default: 64KB)
	MaxPublishSize int
}

// NewProxy creates a new proxy for a client-backend connection pair.
func NewProxy(cfg ProxyConfig) *Proxy {
	// Set defaults using shared constants
	publishRateLimit := cfg.PublishRateLimit
	if publishRateLimit == 0 {
		publishRateLimit = protocol.DefaultPublishRateLimit
	}
	publishBurst := cfg.PublishBurst
	if publishBurst == 0 {
		publishBurst = protocol.DefaultPublishBurst
	}
	maxPublishSize := cfg.MaxPublishSize
	if maxPublishSize == 0 {
		maxPublishSize = protocol.DefaultMaxPublishSize
	}

	return &Proxy{
		clientConn:     cfg.ClientConn,
		backendConn:    cfg.BackendConn,
		logger:         cfg.Logger,
		messageTimeout: cfg.MessageTimeout,
		authEnabled:    cfg.AuthEnabled,
		claims:         cfg.Claims,
		permissions:    cfg.Permissions,
		tenantID:       cfg.TenantID,
		publishLimiter: rate.NewLimiter(rate.Limit(publishRateLimit), publishBurst),
		maxPublishSize: maxPublishSize,
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

		// For text frames, intercept and possibly modify (subscribe filtering, publish mapping)
		if header.OpCode == ws.OpText {
			payload, _ = p.interceptClientMessage(payload)
			// If payload is nil, message was handled (e.g., error sent to client)
			if payload == nil {
				continue
			}
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
			p.sendCloseToClient(payload)
			errChan <- nil
			return
		}

		// Record message metrics
		RecordMessage("backend_to_client", len(payload))

		// Forward all frames to client (server->client: no masking)
		if err := p.sendToClient(header.OpCode, payload, header.Fin); err != nil {
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

// sendToClient sends a WebSocket frame to the client under the write mutex.
// This ensures atomicity of the two-phase write (header + payload) when
// multiple goroutines may write to clientConn concurrently.
func (p *Proxy) sendToClient(opCode ws.OpCode, payload []byte, fin bool) error {
	p.clientWriteMu.Lock()
	defer p.clientWriteMu.Unlock()
	return p.forwardFrame(p.clientConn, opCode, payload, fin, false)
}

// sendCloseToClient sends a close frame to the client under the write mutex.
func (p *Proxy) sendCloseToClient(payload []byte) {
	p.clientWriteMu.Lock()
	defer p.clientWriteMu.Unlock()
	p.forwardCloseFrame(p.clientConn, payload, false)
}

// interceptClientMessage intercepts client messages for subscribe and publish requests.
func (p *Proxy) interceptClientMessage(msg []byte) ([]byte, error) {
	// Defensive guard — tenantID is always set in practice (from JWT or config default).
	// This only triggers if config has empty DefaultTenantID AND auth is disabled.
	if p.tenantID == "" {
		return msg, nil
	}

	var clientMsg protocol.ClientMessage
	if err := json.Unmarshal(msg, &clientMsg); err != nil {
		// Not valid JSON, pass through (intentional: non-JSON messages are passed unchanged)
		return msg, nil //nolint:nilerr // Intentional: pass through non-JSON messages
	}

	switch clientMsg.Type {
	case protocol.MsgTypeSubscribe:
		return p.interceptSubscribe(clientMsg)
	case protocol.MsgTypePublish:
		return p.interceptPublish(clientMsg)
	default:
		return msg, nil
	}
}

// interceptSubscribe intercepts subscribe messages to:
// 1. Validate tenant prefix on each channel (always)
// 2. Filter channels based on permissions (auth only — skipped when auth disabled)
func (p *Proxy) interceptSubscribe(clientMsg protocol.ClientMessage) ([]byte, error) {
	// Parse subscribe data
	var subData protocol.SubscribeData
	if err := json.Unmarshal(clientMsg.Data, &subData); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to parse subscribe data")
		return json.Marshal(clientMsg)
	}

	// 1. Tenant prefix and channel format validation (always — filter out invalid channels)
	channels := make([]string, 0, len(subData.Channels))
	for _, ch := range subData.Channels {
		if !p.validateChannelTenant(ch) {
			RecordChannelCheck("denied")
			RecordAccessDenial("channel", "wrong_tenant")
			p.logger.Warn().
				Str("channel", ch).
				Msg("Subscription denied: wrong tenant prefix")
		} else if len(strings.Split(ch, ".")) < protocol.MinInternalChannelParts {
			RecordChannelCheck("denied")
			RecordAccessDenial("channel", "invalid_format")
			p.logger.Warn().
				Str("channel", ch).
				Msg("Subscription denied: invalid channel format")
		} else {
			channels = append(channels, ch)
		}
	}

	// 2. Permission filtering (auth only — never runs when auth disabled)
	if p.authEnabled {
		// Strip tenant prefix locally for permission matching — patterns like *.trade
		// operate on the non-tenant portion (e.g., "BTC.trade" from "odin.BTC.trade")
		stripped := make([]string, len(channels))
		for i, ch := range channels {
			stripped[i] = p.stripTenantPrefix(ch)
		}
		allowedStripped := p.permissions.FilterChannels(p.claims, stripped)

		// Map allowed stripped names back to full tenant-prefixed channels
		allowed := make([]string, 0, len(allowedStripped))
		for _, s := range allowedStripped {
			allowed = append(allowed, p.tenantID+"."+s)
		}

		// Log denied channels and record metrics
		for _, ch := range channels {
			if contains(allowed, ch) {
				RecordChannelCheck("allowed")
			} else {
				RecordChannelCheck("denied")
				RecordAccessDenial("channel", "unauthorized")
				p.logger.Warn().
					Str("channel", ch).
					Msg("Subscription denied")
			}
		}

		channels = allowed
	}

	// 3. Rebuild message with validated channels
	modifiedData := protocol.SubscribeData{Channels: channels}
	dataBytes, err := json.Marshal(modifiedData)
	if err != nil {
		return json.Marshal(clientMsg)
	}

	return json.Marshal(protocol.ClientMessage{
		Type: protocol.MsgTypeSubscribe,
		Data: dataBytes,
	})
}

// validateChannelTenant checks that the channel's tenant prefix matches the connection's tenant.
// Returns true if channel starts with "{tenantID}." — simple string prefix check.
func (p *Proxy) validateChannelTenant(channel string) bool {
	return strings.HasPrefix(channel, p.tenantID+".")
}

// stripTenantPrefix removes the tenant prefix from a channel for permission matching.
// "odin.BTC.trade" → "BTC.trade". Only used locally in the gateway for permission checks —
// the full tenant-prefixed channel is always forwarded to the server.
func (p *Proxy) stripTenantPrefix(channel string) string {
	return strings.TrimPrefix(channel, p.tenantID+".")
}

// interceptPublish intercepts publish messages to:
// 1. Validate tenant prefix
// 2. Validate channel format (minimum 3 dot-separated parts)
// 3. Rate limit publishes
// 4. Validate message size
func (p *Proxy) interceptPublish(clientMsg protocol.ClientMessage) ([]byte, error) {
	start := time.Now()
	defer func() {
		RecordPublishLatency(time.Since(start).Seconds())
	}()

	// 1. Rate limit check
	if !p.publishLimiter.Allow() {
		RecordPublishResult(p.tenantID, string(protocol.ErrCodeRateLimited))
		return p.sendPublishErrorToClient(protocol.ErrCodeRateLimited)
	}

	// 2. Parse publish data
	var pubData protocol.PublishData
	if err := json.Unmarshal(clientMsg.Data, &pubData); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to parse publish data")
		RecordPublishResult(p.tenantID, string(protocol.ErrCodeInvalidRequest))
		return p.sendPublishErrorToClient(protocol.ErrCodeInvalidRequest)
	}

	// 3. Size check
	if len(pubData.Data) > p.maxPublishSize {
		p.logger.Warn().
			Int("size", len(pubData.Data)).
			Int("max_size", p.maxPublishSize).
			Msg("Publish message too large")
		RecordPublishResult(p.tenantID, string(protocol.ErrCodeMessageTooLarge))
		return p.sendPublishErrorToClient(protocol.ErrCodeMessageTooLarge)
	}

	// 4. Validate channel has minimum parts (tenant.identifier.category)
	parts := strings.Split(pubData.Channel, ".")
	if len(parts) < protocol.MinInternalChannelParts {
		p.logger.Warn().
			Str("channel", pubData.Channel).
			Msg("Invalid publish channel format")
		RecordPublishResult(p.tenantID, string(protocol.ErrCodeInvalidChannel))
		return p.sendPublishErrorToClient(protocol.ErrCodeInvalidChannel)
	}

	// 5. Validate tenant prefix
	if !p.validateChannelTenant(pubData.Channel) {
		p.logger.Warn().
			Str("channel", pubData.Channel).
			Msg("Publish access denied: wrong tenant prefix")
		RecordPublishResult(p.tenantID, string(protocol.ErrCodeForbidden))
		return p.sendPublishErrorToClient(protocol.ErrCodeForbidden)
	}

	p.logger.Debug().
		Str("channel", pubData.Channel).
		Msg("Publish channel validated")

	RecordPublishResult(p.tenantID, "success")

	// Forward original message as-is (no channel transformation)
	return json.Marshal(clientMsg)
}

// sendPublishErrorToClient sends a publish error directly to the client WebSocket.
// Returns nil bytes to signal that the message should NOT be forwarded to backend.
//
//nolint:unparam // Always returns nil bytes by design - indicates message should not be forwarded
func (p *Proxy) sendPublishErrorToClient(code protocol.PublishErrorCode) ([]byte, error) {
	errMsg := map[string]string{
		"type":    protocol.RespTypePublishError,
		"code":    string(code),
		"message": protocol.PublishErrorMessages[code],
	}

	errBytes, err := json.Marshal(errMsg)
	if err != nil {
		return nil, err
	}

	// Send error directly to client (mutex-protected - may race with proxyBackendToClient)
	if sendErr := p.sendToClient(ws.OpText, errBytes, true); sendErr != nil {
		p.logger.Warn().Err(sendErr).Msg("Failed to send publish error to client")
	}

	// Return nil to signal that this message should not be forwarded
	return nil, nil
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	return slices.Contains(slice, val)
}
