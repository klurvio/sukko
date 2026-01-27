package gateway

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus metrics for the gateway service.
// Uses gateway_ prefix for service-specific metrics.

// =============================================================================
// Connection Metrics
// =============================================================================

var connectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gateway_connections_total",
	Help: "Total WebSocket connections to gateway",
})

var connectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "gateway_connections_active",
	Help: "Current active proxy sessions",
})

var connectionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "gateway_connection_duration_seconds",
	Help:    "Connection duration before disconnect",
	Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
}, []string{"close_reason"})

// =============================================================================
// Auth Metrics
// =============================================================================

var authValidations = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_auth_validations_total",
	Help: "JWT validation attempts by status",
}, []string{"status"}) // success, failed, skipped

var authLatency = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "gateway_auth_latency_seconds",
	Help:    "JWT validation latency",
	Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
})

// =============================================================================
// Permission Metrics
// =============================================================================

var channelChecks = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_channel_checks_total",
	Help: "Channel permission checks by result",
}, []string{"result"}) // allowed, denied

// =============================================================================
// Proxy Metrics
// =============================================================================

var messagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_messages_total",
	Help: "Messages proxied by direction",
}, []string{"direction"}) // client_to_backend, backend_to_client

var messageBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_message_bytes_total",
	Help: "Bytes proxied by direction",
}, []string{"direction"})

var proxyErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_proxy_errors_total",
	Help: "Proxy errors by type",
}, []string{"type"}) // read_error, write_error

// =============================================================================
// Backend Metrics
// =============================================================================

var backendConnects = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_backend_connects_total",
	Help: "Backend connection attempts by status",
}, []string{"status"}) // success, failed

var backendLatency = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "gateway_backend_latency_seconds",
	Help:    "Backend dial latency",
	Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
})

// =============================================================================
// Access Denial Metrics
// =============================================================================

var accessDenials = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_access_denials_total",
	Help: "Access denials by resource type and reason",
}, []string{"resource_type", "reason"})

// =============================================================================
// Key Cache Metrics
// =============================================================================

var keyCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gateway_key_cache_hits_total",
	Help: "Total key cache hits",
})

var keyCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gateway_key_cache_misses_total",
	Help: "Total key cache misses",
})

var keyCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "gateway_key_cache_size",
	Help: "Current number of keys in cache",
})

var keyCacheRefreshes = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_key_cache_refreshes_total",
	Help: "Key cache refresh operations by result",
}, []string{"result"}) // success, error

// =============================================================================
// Helper Functions
// =============================================================================

// RecordConnection increments connection counter and active gauge.
func RecordConnection() {
	connectionsTotal.Inc()
	connectionsActive.Inc()
}

// RecordDisconnection decrements active gauge and records duration.
func RecordDisconnection(reason string, duration time.Duration) {
	connectionsActive.Dec()
	connectionDuration.WithLabelValues(reason).Observe(duration.Seconds())
}

// RecordAuthValidation records auth attempt with status and latency.
func RecordAuthValidation(status string, latency time.Duration) {
	authValidations.WithLabelValues(status).Inc()
	authLatency.Observe(latency.Seconds())
}

// RecordChannelCheck records permission check result.
func RecordChannelCheck(result string) {
	channelChecks.WithLabelValues(result).Inc()
}

// RecordMessage records proxied message with direction and size.
func RecordMessage(direction string, bytes int) {
	messagesTotal.WithLabelValues(direction).Inc()
	messageBytesTotal.WithLabelValues(direction).Add(float64(bytes))
}

// RecordProxyError records proxy error by type.
func RecordProxyError(errorType string) {
	proxyErrors.WithLabelValues(errorType).Inc()
}

// RecordBackendConnect records backend connection attempt.
func RecordBackendConnect(status string, latency time.Duration) {
	backendConnects.WithLabelValues(status).Inc()
	backendLatency.Observe(latency.Seconds())
}

// RecordKeyCacheHit records a key cache hit.
func RecordKeyCacheHit() {
	keyCacheHits.Inc()
}

// RecordKeyCacheMiss records a key cache miss.
func RecordKeyCacheMiss() {
	keyCacheMisses.Inc()
}

// SetKeyCacheSize sets the current key cache size.
func SetKeyCacheSize(size int) {
	keyCacheSize.Set(float64(size))
}

// RecordKeyCacheRefresh records a cache refresh operation.
func RecordKeyCacheRefresh(success bool) {
	if success {
		keyCacheRefreshes.WithLabelValues("success").Inc()
	} else {
		keyCacheRefreshes.WithLabelValues("error").Inc()
	}
}

// RecordAccessDenial records an access denial with resource type and reason.
func RecordAccessDenial(resourceType, reason string) {
	accessDenials.WithLabelValues(resourceType, reason).Inc()
}

// AccessDenialMetricsAdapter implements auth.AccessDenialMetrics for Prometheus.
// This adapter allows the auth package to report access denial metrics without depending on gateway.
type AccessDenialMetricsAdapter struct{}

// OnAccessDenied records an access denial event.
func (a *AccessDenialMetricsAdapter) OnAccessDenied(resourceType, reason string) {
	accessDenials.WithLabelValues(resourceType, reason).Inc()
}

// KeyCacheMetricsAdapter implements auth.KeyCacheMetrics for Prometheus.
// This adapter allows the auth package to report metrics without depending on gateway.
type KeyCacheMetricsAdapter struct{}

// OnCacheHit records a key cache hit.
func (a *KeyCacheMetricsAdapter) OnCacheHit() {
	keyCacheHits.Inc()
}

// OnCacheMiss records a key cache miss.
func (a *KeyCacheMetricsAdapter) OnCacheMiss() {
	keyCacheMisses.Inc()
}

// OnCacheRefresh records a cache refresh operation.
func (a *KeyCacheMetricsAdapter) OnCacheRefresh(success bool, keyCount int) {
	if success {
		keyCacheRefreshes.WithLabelValues("success").Inc()
		keyCacheSize.Set(float64(keyCount))
	} else {
		keyCacheRefreshes.WithLabelValues("error").Inc()
	}
}

// HandleMetrics serves Prometheus metrics.
func HandleMetrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}
