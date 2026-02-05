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
// - Filter subscribe channels based on permissions (auth only)
// - Map channels to internal format with tenant prefix (routing, always)
// - Validate publish access and rate limits
type Proxy struct {
	clientConn  net.Conn
	backendConn net.Conn
	logger      zerolog.Logger

	messageTimeout time.Duration

	// Auth (only used when authEnabled=true)
	claims      *auth.Claims
	permissions *PermissionChecker
	authEnabled bool

	// Routing (always used — from JWT when auth enabled, from config when disabled)
	tenantID         string
	channelMapper    *auth.ChannelMapper
	crossTenantRoles []string

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

	// Routing (always set — from JWT or config)
	TenantID         string
	ChannelMapper    *auth.ChannelMapper
	CrossTenantRoles []string

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
		clientConn:       cfg.ClientConn,
		backendConn:      cfg.BackendConn,
		logger:           cfg.Logger,
		messageTimeout:   cfg.MessageTimeout,
		authEnabled:      cfg.AuthEnabled,
		claims:           cfg.Claims,
		permissions:      cfg.Permissions,
		tenantID:         cfg.TenantID,
		channelMapper:    cfg.ChannelMapper,
		crossTenantRoles: cfg.CrossTenantRoles,
		publishLimiter:   rate.NewLimiter(rate.Limit(publishRateLimit), publishBurst),
		maxPublishSize:   maxPublishSize,
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
// 1. Filter channels based on permissions (auth only — skipped when auth disabled)
// 2. Map channels to internal format with tenant prefix (routing — always runs)
func (p *Proxy) interceptSubscribe(clientMsg protocol.ClientMessage) ([]byte, error) {
	// Parse subscribe data
	var subData protocol.SubscribeData
	if err := json.Unmarshal(clientMsg.Data, &subData); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to parse subscribe data")
		return json.Marshal(clientMsg)
	}

	// 1. Permission filtering (auth only — never runs when auth disabled)
	channels := subData.Channels
	if p.authEnabled {
		allowedChannels := p.permissions.FilterChannels(p.claims, channels)

		// Log denied channels and record metrics
		for _, ch := range channels {
			if contains(allowedChannels, ch) {
				RecordChannelCheck("allowed")
			} else {
				RecordChannelCheck("denied")
				RecordAccessDenial("channel", "unauthorized")
				p.logger.Warn().
					Str("channel", ch).
					Msg("Subscription denied")
			}
		}

		channels = allowedChannels
	}

	// 2. Channel mapping (routing — always runs)
	channels = p.mapChannelsToInternal(channels)

	// 3. Rebuild message with mapped channels
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

// mapChannelToInternal maps a single client channel to internal tenant-prefixed format.
// Avoids double-prefixing if the channel already starts with the tenant prefix.
func (p *Proxy) mapChannelToInternal(ch string) string {
	if p.channelMapper == nil || p.tenantID == "" {
		return ch
	}
	if strings.HasPrefix(ch, p.tenantID+".") {
		return ch // Already prefixed
	}
	return p.channelMapper.MapToInternalWithTenant(p.tenantID, ch)
}

// mapChannelsToInternal maps client channels to internal tenant-prefixed format.
// Uses MapToInternalWithTenant which respects TenantImplicit and config.Separator.
func (p *Proxy) mapChannelsToInternal(channels []string) []string {
	if p.channelMapper == nil || p.tenantID == "" {
		return channels
	}
	mapped := make([]string, len(channels))
	for i, ch := range channels {
		mapped[i] = p.mapChannelToInternal(ch)
	}
	return mapped
}

// interceptPublish intercepts publish messages to:
// 1. Rate limit publishes
// 2. Validate message size
// 3. Map client channel to internal format (routing — always runs)
// 4. Validate channel access (auth only — skipped when auth disabled)
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

	// 4. Validate channel has minimum parts (identifier.category)
	parts := strings.Split(pubData.Channel, ".")
	if len(parts) < protocol.MinClientChannelParts {
		p.logger.Warn().
			Str("channel", pubData.Channel).
			Msg("Invalid publish channel format")
		RecordPublishResult(p.tenantID, string(protocol.ErrCodeInvalidChannel))
		return p.sendPublishErrorToClient(protocol.ErrCodeInvalidChannel)
	}

	// 5. Map channel to internal format (routing — always runs)
	internalChannel := p.mapChannelToInternal(pubData.Channel)

	// 6. Validate channel access (auth only — skipped when auth disabled)
	if p.authEnabled && p.channelMapper != nil {
		if !p.channelMapper.ValidateChannelAccess(p.claims, internalChannel, p.crossTenantRoles) {
			p.logger.Warn().
				Str("channel", pubData.Channel).
				Str("internal_channel", internalChannel).
				Msg("Publish access denied")
			RecordPublishResult(p.tenantID, string(protocol.ErrCodeForbidden))
			return p.sendPublishErrorToClient(protocol.ErrCodeForbidden)
		}
	}

	// 7. Rebuild message with mapped channel
	originalChannel := pubData.Channel
	pubData.Channel = internalChannel
	newData, err := json.Marshal(pubData)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to marshal publish data")
		return json.Marshal(clientMsg)
	}

	p.logger.Debug().
		Str("original_channel", originalChannel).
		Str("internal_channel", internalChannel).
		Msg("Mapped publish channel")

	RecordPublishResult(p.tenantID, "success")

	return json.Marshal(protocol.ClientMessage{Type: protocol.MsgTypePublish, Data: newData})
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

	// Send error directly to client (no masking - server->client)
	if sendErr := p.forwardFrame(p.clientConn, ws.OpText, errBytes, true, false); sendErr != nil {
		p.logger.Warn().Err(sendErr).Msg("Failed to send publish error to client")
	}

	// Return nil to signal that this message should not be forwarded
	return nil, nil
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	return slices.Contains(slice, val)
}
