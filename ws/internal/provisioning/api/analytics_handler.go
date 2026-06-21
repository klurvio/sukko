package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/httputil"
)

// analyticsSSEDroppedCounter counts events dropped due to slow SSE clients (§VI prefix).
var analyticsSSEDroppedCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "provisioning_analytics_sse_dropped_total",
	Help: "Total SSE analytics events dropped due to slow client (provisioning service).",
})

// sseIdleTimeout is the max time an SSE connection can be idle before server closes it.
const sseIdleTimeout = 90 * time.Second

// AnalyticsSSEHandler serves GET /api/v1/admin/analytics/stream?tenant_id={id}.
// It is a polling reader of the analytics DB — not a flush-event subscriber —
// so it always reflects the multi-pod aggregate, independent of per-pod flush timing.
type AnalyticsSSEHandler struct {
	pool     *pgxpool.Pool
	maxConns int
	sem      chan struct{} // semaphore: cap = ANALYTICS_SSE_MAX_CONNS
	interval time.Duration
	logger   zerolog.Logger
}

// NewAnalyticsSSEHandler creates an AnalyticsSSEHandler.
func NewAnalyticsSSEHandler(pool *pgxpool.Pool, maxConns int, interval time.Duration, logger zerolog.Logger) *AnalyticsSSEHandler {
	return &AnalyticsSSEHandler{
		pool:     pool,
		maxConns: maxConns,
		sem:      make(chan struct{}, maxConns),
		interval: interval,
		logger:   logger.With().Str("component", "analytics_sse").Logger(),
	}
}

// ServeHTTP handles the SSE stream. Admin-JWT is enforced by the router group.
func (h *AnalyticsSSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	default:
		httputil.WriteError(w, http.StatusTooManyRequests, "TOO_MANY_SSE_CONNECTIONS",
			fmt.Sprintf("max concurrent analytics stream connections (%d) reached", h.maxConns))
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.logger.Error().Msg("SSE: response writer does not support flushing")
		return
	}

	// Initial snapshot on connect.
	if err := h.sendSnapshot(r.Context(), w, flusher, tenantID); err != nil {
		h.logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("SSE initial snapshot failed")
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	idleTimer := time.NewTimer(sseIdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := h.sendSnapshot(r.Context(), w, flusher, tenantID); err != nil {
				analyticsSSEDroppedCounter.Inc()
				return // client gone or write error
			}
			idleTimer.Reset(sseIdleTimeout)
		case <-idleTimer.C:
			return // client idle
		}
	}
}

func (h *AnalyticsSSEHandler) sendSnapshot(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, tenantID string) error {
	snap := h.queryMetrics(ctx, tenantID)
	return sseWriteEvent(w, flusher, "snapshot", snap)
}

// analyticsSnapshot is the SSE payload for the analytics stream.
type analyticsSnapshot struct {
	TenantID    string       `json:"tenant_id,omitempty"`
	Connections *connMetrics `json:"connections,omitempty"`
	Messages    *msgMetrics  `json:"messages,omitempty"`
	Push        *pushMetrics `json:"push,omitempty"`
	BucketStart *time.Time   `json:"bucket_start,omitempty"`
}

type connMetrics struct {
	Active      int64 `json:"active"`
	WebSocket   int64 `json:"websocket"`
	SSE         int64 `json:"sse"`
	Connects    int64 `json:"connects"`
	Disconnects int64 `json:"disconnects"`
}

type msgMetrics struct {
	Published int64 `json:"published"`
	Delivered int64 `json:"delivered"`
	Failed    int64 `json:"failed"`
}

type pushMetrics struct {
	Sent        int64 `json:"sent"`
	Success     int64 `json:"success"`
	Failed      int64 `json:"failed"`
	Expired     int64 `json:"expired"`
	RateLimited int64 `json:"rate_limited"`
}

// queryMetrics reads the most recent complete 1-minute bucket from the analytics tables.
// tenantID="" aggregates across all tenants.
func (h *AnalyticsSSEHandler) queryMetrics(ctx context.Context, tenantID string) *analyticsSnapshot {
	snap := &analyticsSnapshot{TenantID: tenantID}
	bucketCutoff := time.Now().UTC().Truncate(time.Minute)
	var tid *string
	if tenantID != "" {
		tid = &tenantID
	}

	// Connections — aggregate across all pods, last complete minute.
	var cm connMetrics
	var bs *time.Time
	err := h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(active_count), 0),
			COALESCE(SUM(CASE WHEN transport='websocket' THEN active_count ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN transport='sse'       THEN active_count ELSE 0 END), 0),
			COALESCE(SUM(connect_count), 0),
			COALESCE(SUM(disconnect_count), 0),
			MAX(bucket_start)
		FROM analytics_connections
		WHERE bucket_start < $1
		  AND ($2::uuid IS NULL OR tenant_id = $2)
		  AND bucket_size = 'minute'`,
		bucketCutoff, tid,
	).Scan(&cm.Active, &cm.WebSocket, &cm.SSE, &cm.Connects, &cm.Disconnects, &bs)
	if err == nil {
		snap.Connections = &cm
		snap.BucketStart = bs
	}

	// Messages.
	var mm msgMetrics
	_ = h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(published_count), 0),
			COALESCE(SUM(delivered_count), 0),
			COALESCE(SUM(failed_count), 0)
		FROM analytics_messages
		WHERE bucket_start < $1
		  AND ($2::uuid IS NULL OR tenant_id = $2)
		  AND bucket_size = 'minute'`,
		bucketCutoff, tid,
	).Scan(&mm.Published, &mm.Delivered, &mm.Failed)
	snap.Messages = &mm

	// Push.
	var pm pushMetrics
	_ = h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(sent_count), 0),
			COALESCE(SUM(success_count), 0),
			COALESCE(SUM(failed_count), 0),
			COALESCE(SUM(expired_count), 0),
			COALESCE(SUM(rate_limited_count), 0)
		FROM analytics_push
		WHERE bucket_start < $1
		  AND ($2::uuid IS NULL OR tenant_id = $2)
		  AND bucket_size = 'minute'`,
		bucketCutoff, tid,
	).Scan(&pm.Sent, &pm.Success, &pm.Failed, &pm.Expired, &pm.RateLimited)
	snap.Push = &pm

	return snap
}

// sseWriteEvent serializes data as JSON and writes a named SSE event.
func sseWriteEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal SSE event %s: %w", eventType, err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, b); err != nil {
		return fmt.Errorf("write SSE event %s: %w", eventType, err)
	}
	flusher.Flush()
	return nil
}
