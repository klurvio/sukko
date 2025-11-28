// Package shared provides core WebSocket server functionality.
// This file contains the Pump struct for WebSocket read/write operations.
package shared

import (
	"bufio"
	"context"
	"encoding/json"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/adred-codev/ws_poc/internal/shared/monitoring"
	"github.com/adred-codev/ws_poc/internal/shared/types"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
)

// PumpConfig holds timing configuration for pump operations.
type PumpConfig struct {
	PongWait   time.Duration
	WriteWait  time.Duration
	PingPeriod time.Duration
}

// DefaultPumpConfig returns the default pump configuration.
func DefaultPumpConfig() PumpConfig {
	pongWait := 30 * time.Second
	return PumpConfig{
		PongWait:   pongWait,
		WriteWait:  5 * time.Second,
		PingPeriod: (pongWait * 9) / 10, // 27 seconds
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
// Uses zerolog for production, falls back to basic logging for tests.
func (p *Pump) recoverPanic(goroutineName string, fields map[string]any) {
	if r := recover(); r != nil {
		stack := string(debug.Stack())

		// Try zerolog first (production)
		if p.ZerologLogger.GetLevel() != zerolog.Disabled {
			event := p.ZerologLogger.Error().
				Str("goroutine", goroutineName).
				Interface("panic_value", r).
				Str("stack_trace", stack).
				Str("recovery_mode", "captured_panic_continuing_execution")

			for k, v := range fields {
				event = event.Interface(k, v)
			}

			event.Msg("GOROUTINE PANIC RECOVERED")
			return
		}

		// Fall back to interface logger (tests)
		if p.Logger != nil {
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
func (p *Pump) ReadLoop(ctx context.Context, c *Client, disconnectFn func(*Client, string, string), handleMsgFn func(*Client, []byte)) {
	// Panic recovery
	defer p.recoverPanic("readPump", map[string]any{
		"client_id": c.id,
	})

	var disconnectReason string
	var initiatedBy string

	defer func() {
		if disconnectReason == "" {
			disconnectReason = monitoring.DisconnectReasonReadError
			initiatedBy = monitoring.DisconnectInitiatedByClient
		}
		if disconnectFn != nil {
			disconnectFn(c, disconnectReason, initiatedBy)
		}
	}()

	_ = c.conn.SetReadDeadline(p.now().Add(p.Config.PongWait))

	for {
		// Check for context cancellation (server shutdown)
		select {
		case <-ctx.Done():
			disconnectReason = monitoring.DisconnectReasonServerShutdown
			initiatedBy = monitoring.DisconnectInitiatedByServer
			return
		default:
		}

		msg, op, err := wsutil.ReadClientData(c.conn)
		if err != nil {
			disconnectReason = monitoring.DisconnectReasonReadError
			initiatedBy = monitoring.DisconnectInitiatedByClient
			break
		}

		_ = c.conn.SetReadDeadline(p.now().Add(p.Config.PongWait))

		// Update stats
		atomic.AddInt64(&p.Stats.MessagesReceived, 1)
		atomic.AddInt64(&p.Stats.BytesReceived, int64(len(msg)))
		monitoring.UpdateMessageMetrics(0, 1)
		monitoring.UpdateBytesMetrics(0, int64(len(msg)))

		if op == ws.OpText {
			// Rate limiting check
			if p.RateLimiter != nil && !p.RateLimiter.CheckLimit(c.id) {
				p.handleRateLimitExceeded(c)
				continue
			}

			// Process message
			if handleMsgFn != nil {
				handleMsgFn(c, msg)
			}
		} else if op == ws.OpPing {
			// gobwas library handles pongs automatically
		} else if op == ws.OpClose {
			return
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
	case c.send <- errorMsg:
	default:
		// Client buffer full
	}

	atomic.AddInt64(&p.Stats.RateLimitedMessages, 1)
	monitoring.IncrementRateLimitedMessages()
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
		select {
		case <-ctx.Done():
			return

		case message, ok := <-c.send:
			if !ok {
				if p.Logger != nil {
					p.Logger.Debug().Int64("client_id", c.id).Msg("Send channel closed")
				}
				_ = wsutil.WriteServerMessage(c.conn, ws.OpClose, []byte{})
				return
			}

			_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))

			// Batch metrics
			var batchMsgCount int64 = 1
			batchByteCount := int64(len(message))

			// Write first message
			err := wsutil.WriteServerMessage(writer, ws.OpText, message)
			if err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to write message")
				}
				return
			}

			// Batch additional messages
			n := len(c.send)
		batchLoop:
			for range n {
				var batchMsg []byte
				var batchOk bool
				select {
				case batchMsg, batchOk = <-c.send:
					if !batchOk {
						break batchLoop
					}
				default:
					break batchLoop
				}
				err := wsutil.WriteServerMessage(writer, ws.OpText, batchMsg)
				if err != nil {
					if p.Logger != nil {
						p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to write message")
					}
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
				return
			}

			// Update metrics (once per batch)
			atomic.AddInt64(&p.Stats.MessagesSent, batchMsgCount)
			atomic.AddInt64(&p.Stats.BytesSent, batchByteCount)
			monitoring.UpdateMessageMetrics(batchMsgCount, 0)
			monitoring.UpdateBytesMetrics(batchByteCount, 0)

		case <-ticker.C():
			_ = c.conn.SetWriteDeadline(p.now().Add(p.Config.WriteWait))
			if err := wsutil.WriteServerMessage(c.conn, ws.OpPing, nil); err != nil {
				if p.Logger != nil {
					p.Logger.Debug().Err(err).Int64("client_id", c.id).Msg("Failed to send ping")
				}
				return
			}
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

