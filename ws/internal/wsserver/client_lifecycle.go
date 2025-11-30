package wsserver

import (
	"sync/atomic"
	"time"

	"github.com/adred-codev/ws_poc/internal/monitoring"
)

// Client connection lifecycle management
func (s *Server) disconnectClient(c *Client, reason, initiatedBy string) {
	// Calculate connection duration for metrics
	duration := time.Since(c.connectedAt)

	// Record disconnect metrics (both Prometheus and Stats)
	monitoring.RecordDisconnectWithStats(s.stats, reason, initiatedBy, duration)

	// Log disconnect with enhanced context (Phase 4: Structured logging)
	bufferLen := len(c.send)
	bufferCap := cap(c.send)
	bufferUsagePercent := float64(bufferLen) / float64(bufferCap) * 100

	s.logger.Info().
		Int64("client_id", c.id).
		Str("reason", reason).
		Str("initiated_by", initiatedBy).
		Dur("connection_duration", duration).
		Int("subscriptions_count", c.subscriptions.Count()).
		Int64("sequence_number", c.seqGen.Current()).
		Int("send_buffer_len", bufferLen).
		Int("send_buffer_cap", bufferCap).
		Float64("send_buffer_usage_pct", bufferUsagePercent).
		Int32("send_attempts", atomic.LoadInt32(&c.sendAttempts)).
		Time("connected_at", c.connectedAt).
		Msg("Client disconnected")

	// Use sync.Once to ensure connection is only closed once
	// Prevents race condition with multiple goroutines trying to close
	c.closeOnce.Do(func() {
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})

	// Cleanup resources
	s.clients.Delete(c)
	atomic.AddInt64(&s.stats.CurrentConnections, -1)

	// Remove client from subscription index (prevent memory leak)
	s.subscriptionIndex.RemoveClient(c)

	// Return client to pool for reuse
	s.connections.Put(c)
	<-s.connectionsSem // Release connection slot

	// Clean up rate limiter state
	s.rateLimiter.RemoveClient(c.id)
}
