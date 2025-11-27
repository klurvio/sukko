package shared

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/adred-codev/ws_poc/internal/shared/monitoring"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// readPump reads messages from the WebSocket connection
func (s *Server) readPump(c *Client) {
	// CRITICAL: Panic recovery must be FIRST defer (executes LAST in LIFO order)
	// This catches any panics including those in cleanup code
	defer monitoring.RecoverPanic(s.logger, "readPump", map[string]any{
		"client_id": c.id,
	})

	var disconnectReason string
	var initiatedBy string

	defer func() {
		// Determine disconnect reason if not already set
		if disconnectReason == "" {
			disconnectReason = monitoring.DisconnectReasonReadError
			initiatedBy = monitoring.DisconnectInitiatedByClient
		}
		s.disconnectClient(c, disconnectReason, initiatedBy)
	}()

	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))

	for {
		msg, op, err := wsutil.ReadClientData(c.conn)
		if err != nil {
			// All read errors treated as client-initiated disconnect
			disconnectReason = monitoring.DisconnectReasonReadError
			initiatedBy = monitoring.DisconnectInitiatedByClient
			break
		}

		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))

		atomic.AddInt64(&s.stats.MessagesReceived, 1)
		atomic.AddInt64(&s.stats.BytesReceived, int64(len(msg)))
		monitoring.UpdateMessageMetrics(0, 1)
		monitoring.UpdateBytesMetrics(0, int64(len(msg)))

		if op == ws.OpText {
			// Rate limiting check - prevent client from flooding server
			// Important for:
			// 1. DoS prevention (malicious client can't overwhelm server)
			// 2. Bug protection (client code bug causing infinite loop)
			// 3. Fair resource sharing (one client can't starve others)
			//
			// Rate limit: 100 burst, 10/sec sustained
			// This allows legitimate bursts (rapid order entry) while preventing abuse
			if !s.rateLimiter.CheckLimit(c.id) {
				s.logger.Warn().
					Int64("client_id", c.id).
					Int("burst_limit", 100).
					Int("rate_limit_per_sec", 10).
					Msg("Client rate limited")

				// Audit log rate limiting event
				s.auditLogger.Warning("ClientRateLimited", "Client exceeded rate limit", map[string]any{
					"clientID": c.id,
					"limit":    "100 burst, 10/sec sustained",
				})

				// Send error message to client
				// This helps client-side debugging (they know why messages are being dropped)
				errorMsg := map[string]any{
					"type":    "error",
					"code":    "RATE_LIMIT_EXCEEDED",
					"message": "Too many messages, please slow down (limit: 10/sec)",
				}

				if data, err := json.Marshal(errorMsg); err == nil {
					// Best effort send (don't block if client slow)
					select {
					case c.send <- data:
					default:
						// Client buffer full, they won't see error anyway
					}
				}

				// Increment rate limit counter for monitoring
				atomic.AddInt64(&s.stats.RateLimitedMessages, 1)
				monitoring.IncrementRateLimitedMessages()

				// Drop the message but don't disconnect
				// Rationale: Might be temporary spike, give them a chance
				// If persistent, they'll see error messages and fix their code
				continue
			}

			// Process client message (replay requests, heartbeats, etc.)
			s.handleClientMessage(c, msg)

		} else if op == ws.OpPing {
			// The gobwas library handles pongs automatically by default
		} else if op == ws.OpClose {
			return
		}
	}
}
