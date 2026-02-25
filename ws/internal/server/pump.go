// Package server provides core WebSocket server functionality.
// This file contains the Pump struct for WebSocket read/write operations.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"runtime/debug"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	pkgmetrics "github.com/Toniq-Labs/odin-ws/internal/shared/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// PumpConfig holds timing configuration for pump operations.
type PumpConfig struct {
	PongWait   time.Duration
	WriteWait  time.Duration
	PingPeriod time.Duration
}

// FallbackPingPeriodRatio is the ratio used to calculate PingPeriod from PongWait
// when an invalid configuration is provided (PingPeriod >= PongWait).
// 75% (3/4) provides a 25% buffer for network round-trip latency.
// Example: 60s PongWait * 0.75 = 45s PingPeriod = 15s buffer
const (
	FallbackPingPeriodRatio = 3
	FallbackPingPeriodDenom = 4
)

// NewPumpConfig creates a PumpConfig with the specified timeouts.
// If pingPeriod >= pongWait (invalid), logs a warning and falls back to 75% ratio.
func NewPumpConfig(pongWait, pingPeriod, writeWait time.Duration, logger zerolog.Logger) PumpConfig {
	// Validation: pingPeriod must be less than pongWait
	if pingPeriod >= pongWait {
		correctedPingPeriod := (pongWait * FallbackPingPeriodRatio) / FallbackPingPeriodDenom
		logger.Warn().
			Dur("configured_ping_period", pingPeriod).
			Dur("configured_pong_wait", pongWait).
			Dur("corrected_ping_period", correctedPingPeriod).
			Msg("PingPeriod >= PongWait is invalid, using 75% ratio")
		pingPeriod = correctedPingPeriod
	}

	return PumpConfig{
		PongWait:   pongWait,
		WriteWait:  writeWait,
		PingPeriod: pingPeriod,
	}
}

// Pump handles WebSocket read/write operations with injectable dependencies.
// This enables unit testing of pump logic without real WebSocket connections.
type Pump struct {
	Config        PumpConfig
	Logger        Logger
	ZerologLogger zerolog.Logger // For panic recovery (zerolog-specific)
	RateLimiter   RateLimiter
	AuditLogger   AuditLogger
	Stats         *types.Stats
	Clock         Clock
}

// NewPump creates a Pump with the given dependencies.
func NewPump(config PumpConfig, logger Logger, zerologLogger zerolog.Logger, rateLimiter RateLimiter, auditLogger AuditLogger, stats *types.Stats, clock Clock) *Pump {
	return &Pump{
		Config:        config,
		Logger:        logger,
		ZerologLogger: zerologLogger,
		RateLimiter:   rateLimiter,
		AuditLogger:   auditLogger,
		Stats:         stats,
		Clock:         clock,
	}
}

// recoverPanic handles panic recovery for pump operations.
// Uses the shared logging.RecoverPanic for production (zerolog),
// falls back to basic logging for tests using the Logger interface.
func (p *Pump) recoverPanic(goroutineName string, fields map[string]any) {
	if r := recover(); r != nil {
		// Try zerolog first (production) using shared logging
		if p.ZerologLogger.GetLevel() != zerolog.Disabled {
			// Re-panic and let shared logging handle it
			defer logging.RecoverPanic(p.ZerologLogger, goroutineName, fields)
			panic(r)
		}

		// Fall back to interface logger (tests)
		if p.Logger != nil {
			stack := string(debug.Stack())
			p.Logger.Error().
				Str("goroutine", goroutineName).
				Interface("panic_value", r).
				Str("stack_trace", stack).
				Msg("GOROUTINE PANIC RECOVERED")
		}
	}
}

// now returns the current time using the injected clock or real time.
func (p *Pump) now() time.Time {
	if p.Clock != nil {
		return p.Clock.Now()
	}
	return time.Now()
}

// newTicker creates a ticker using the injected clock or real time.
func (p *Pump) newTicker(d time.Duration) Ticker {
	if p.Clock != nil {
		return p.Clock.NewTicker(d)
	}
	return &RealTicker{ticker: time.NewTicker(d)}
}

// ReadLoop reads messages from the WebSocket connection.
// disconnectFn is called when the client disconnects.
// handleMsgFn is called for each text message received.
//
// Uses wsutil.Reader with explicit frame handling to ensure the read deadline
// is refreshed when pong frames are received. This fixes a bug where
// wsutil.ReadClientData() would handle pongs internally without returning,
// causing the read deadline to never refresh and connections to timeout.
func (p *Pump) ReadLoop(ctx context.Context, c *Client, disconnectFn func(*Client, string, string), handleMsgFn func(*Client, []byte)) {
	// Panic recovery
	defer p.recoverPanic("readPump", map[string]any{
		"client_id": c.id,
	})

	var disconnectReason string
	var initiatedBy string

	defer func() {
		if disconnectReason == "" {
			disconnectReason = pkgmetrics.DisconnectReadError
			initiatedBy = pkgmetrics.InitiatedByClient
		}
		if disconnectFn != nil {
			disconnectFn(c, disconnectReason, initiatedBy)
		}
	}()

	_ = c.conn.SetReadDeadline(p.now().Add(p.Config.PongWait))

	// Create reader for explicit frame handling
	reader := wsutil.Reader{
		Source: c.conn,
		State:  ws.StateServerSide,
	}

	for {
		// Check for context cancellation (server shutdown)
		select {
		case <-ctx.Done():
			disconnectReason = pkgmetrics.DisconnectServerShutdown
			initiatedBy = pkgmetrics.InitiatedByServer
			return
		default:
		}

		// Read next frame header
		hdr, err := reader.NextFrame()
		if err != nil {
			disconnectReason = pkgmetrics.DisconnectReadError
			initiatedBy = pkgmetrics.InitiatedByClient
			break
		}

		// Refresh deadline on ANY frame (including pong) - this is the key fix
		_ = c.conn.SetReadDeadline(p.now().Add(p.Config.PongWait))

		// Handle control frames
		if hdr.OpCode.IsControl() {
			// Handle ping - send pong response
			if hdr.OpCode == ws.OpPing {
				payload := make([]byte, hdr.Length)
				if hdr.Length > 0 {
					if _, err := io.ReadFull(c.conn, payload); err != nil {
						break
					}
					if hdr.Masked {
						ws.Cipher(payload, hdr.Mask, 0)
					}
				}
				select {
				case c.control <- payload:
					if p.Logger != nil {
						p.Logger.Debug().Int64("client_id", c.id).Msg("Received ping, queued pong")
					}
				default:
					// Control channel full — drop this pong. The client's next ping will trigger
					// another pong attempt. This is safe: WriteLoop drains control with priority,
					// so a full channel means 2 pongs are already queued and will be written shortly.
					if p.Logger != nil {
						p.Logger.Debug().Int64("client_id", c.id).Msg("Control channel full, dropped pong")
					}
				}
				continue
			}
			// Handle close
			if hdr.OpCode == ws.OpClose {
				return
			}
			// Handle pong - deadline already refreshed, discard payload
			if hdr.OpCode == ws.OpPong {
				if p.Logger != nil {
					p.Logger.Debug().Int64("client_id", c.id).Msg("Received pong from client")
				}
				_ = reader.Discard()
				continue
			}
			continue
		}

		// Read data frame payload
		msg, err := io.ReadAll(&reader)
		if err != nil {
			disconnectReason = pkgmetrics.DisconnectReadError
			initiatedBy = pkgmetrics.InitiatedByClient
			break
		}

		// Update stats
		p.Stats.MessagesReceived.Add(1)
		p.Stats.BytesReceived.Add(int64(len(msg)))
		metrics.UpdateMessageMetrics(0, 1)
		metrics.UpdateBytesMetrics(0, int64(len(msg)))

		if hdr.OpCode == ws.OpText {
			// Rate limiting check
			if p.RateLimiter != nil && !p.RateLimiter.CheckLimit(c.id) {
				p.handleRateLimitExceeded(c)
				continue
			}

			// Process message
			if handleMsgFn != nil {
				handleMsgFn(c, msg)
			}
		}
	}
}

// handleRateLimitExceeded processes a rate limit violation.
func (p *Pump) handleRateLimitExceeded(c *Client) {
	if p.Logger != nil {
		p.Logger.Warn().
			Int64("client_id", c.id).
			Int("burst_limit", 100).
			Int("rate_limit_per_sec", 10).
			Msg("Client rate limited")
	}

	if p.AuditLogger != nil {
		p.AuditLogger.Warning("ClientRateLimited", "Client exceeded rate limit", map[string]any{
			"clientID": c.id,
			"limit":    "100 burst, 10/sec sustained",
		})
	}

	// Send error to client
	errorMsg := CreateRateLimitErrorMessage()
	select {
	case c.send <- RawMsg(errorMsg):
	default:
		// Client buffer full
	}

	p.Stats.RateLimitedMessages.Add(1)
	metrics.IncrementRateLimitedMessages()
}

// WriteLoop writes messages to the WebSocket connection.
// Implements message batching and ping/pong for connection health.
func (p *Pump) WriteLoop(ctx context.Context, c *Client) {
	// Panic recovery
	defer p.recoverPanic("writePump", map[string]any{
		"client_id": c.id,
	})

	writer := bufio.NewWriter(c.conn)
	ticker := p.newTicker(p.Config.PingPeriod)

	defer func() {
		ticker.Stop()
		c.closeOnce.Do(func() {
			if c.conn != nil {
				_ = c.conn.Close()
			}
		})
	}()

	for {
		// Priority: drain pong responses first (they're time-sensitive)
		select {
		case payload := <-c.control:
			_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPong, payload); err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to send pong")
				}
				return
			}
			if p.Logger != nil {
				p.Logger.Debug().Int64("client_id", c.id).Msg("Sent pong to client")
			}
			continue
		default:
		}

		// Normal select: data, control, ping, or shutdown
		select {
		case <-ctx.Done():
			return

		case payload := <-c.control:
			_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPong, payload); err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to send pong")
				}
				return
			}
			if p.Logger != nil {
				p.Logger.Debug().Int64("client_id", c.id).Msg("Sent pong to client")
			}

		case msg, ok := <-c.send:
			if !ok {
				if p.Logger != nil {
					p.Logger.Debug().Int64("client_id", c.id).Msg("Send channel closed")
				}
				// Guard against nil conn (client may already be cleaned up)
				if c.conn != nil {
					_ = wsutil.WriteServerMessage(c.conn, ws.OpClose, []byte{})
				}
				return
			}

			// Guard against race condition: connection may be nil if client was
			// disconnected and returned to pool while message was pending
			if c.conn == nil {
				return
			}
			_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))

			// Resolve OutgoingMsg to bytes: raw passthrough or envelope.Build(seq)
			message := msg.Bytes()
			if message == nil {
				// Zero-value OutgoingMsg — skip to avoid writing empty WebSocket frame
				continue
			}

			// Batch metrics
			var batchMsgCount int64 = 1
			batchByteCount := int64(len(message))

			// Write first message
			err := wsutil.WriteServerMessage(writer, ws.OpText, message)
			if err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to write message")
				}
				// Record dropped messages: 1 (current) + remaining in buffer
				p.recordSendTimeoutDrops(c, 1)
				return
			}

			// Batch additional messages
			n := len(c.send)
		batchLoop:
			for range n {
				var batchOutgoing OutgoingMsg
				var batchOk bool
				select {
				case batchOutgoing, batchOk = <-c.send:
					if !batchOk {
						break batchLoop
					}
				default:
					break batchLoop
				}
				batchMsg := batchOutgoing.Bytes()
				if batchMsg == nil {
					continue
				}
				err := wsutil.WriteServerMessage(writer, ws.OpText, batchMsg)
				if err != nil {
					if p.Logger != nil {
						p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to write message")
					}
					// Record dropped messages: 1 (current) + remaining in buffer
					p.recordSendTimeoutDrops(c, 1)
					return
				}
				batchMsgCount++
				batchByteCount += int64(len(batchMsg))
			}

			// Flush
			if err := writer.Flush(); err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to flush writer")
				}
				// Record remaining messages in buffer as dropped
				p.recordSendTimeoutDrops(c, 0)
				return
			}

			// Update metrics (once per batch)
			p.Stats.MessagesSent.Add(batchMsgCount)
			p.Stats.BytesSent.Add(batchByteCount)
			metrics.UpdateMessageMetrics(batchMsgCount, 0)
			metrics.UpdateBytesMetrics(batchByteCount, 0)

		case <-ticker.C():
			// Guard against race condition: connection may be nil if client was
			// disconnected and returned to pool while ticker was pending
			if c.conn == nil {
				return
			}
			_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPing, nil); err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to send ping")
				}
				return
			}
			if p.Logger != nil {
				p.Logger.Debug().Int64("client_id", c.id).Msg("Sent ping to client")
			}
		}
	}
}

// recordSendTimeoutDrops records dropped messages when a write operation fails.
// failedCount is the number of messages that failed during the current write.
// It also drains and counts remaining messages in the client's send buffer.
// Uses "delivery" as channel since actual channel info is not available at write time.
func (p *Pump) recordSendTimeoutDrops(c *Client, failedCount int) {
	// Count remaining messages in send buffer
	remainingCount := len(c.send)
	totalDropped := failedCount + remainingCount

	if totalDropped > 0 {
		// Record each drop (using "delivery" as pseudo-channel since we don't have channel info)
		for range totalDropped {
			metrics.RecordDroppedBroadcastWithStats(p.Stats, "delivery", pkgmetrics.DropReasonSendTimeout)
		}
	}

	// Drain the send buffer to prevent blocking senders
	for range remainingCount {
		select {
		case <-c.send:
		default:
			return
		}
	}
}

// =============================================================================
// Pure Functions
// =============================================================================

// CreateRateLimitErrorMessage creates the error response for rate limiting.
func CreateRateLimitErrorMessage() []byte {
	msg := map[string]any{
		"type":    "error",
		"code":    "RATE_LIMIT_EXCEEDED",
		"message": "Too many messages, please slow down (limit: 10/sec)",
	}
	data, _ := json.Marshal(msg)
	return data
}
