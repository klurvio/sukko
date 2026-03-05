package gateway

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
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
	Buckets: pkgmetrics.ConnectionDurationBuckets,
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
	Buckets: pkgmetrics.AuthLatencyBuckets,
})

// =============================================================================
// Auth Refresh Metrics
// =============================================================================

var authRefreshTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_auth_refresh_total",
	Help: "Auth refresh attempts by result",
}, []string{"result"}) // success, invalid_token, token_expired, tenant_mismatch, rate_limited, not_available

var authRefreshLatency = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "gateway_auth_refresh_latency_seconds",
	Help:    "Auth refresh processing latency",
	Buckets: pkgmetrics.AuthLatencyBuckets,
})

var forcedUnsubscriptionsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gateway_forced_unsubscriptions_total",
	Help: "Total forced unsubscriptions due to auth refresh permission changes",
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
// Publish Metrics
// =============================================================================

var publishTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_publish_total",
	Help: "Publish message intercepts by tenant and result",
}, []string{"tenant", "result"}) // success, rate_limited, forbidden, etc.

var publishLatency = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "gateway_publish_latency_seconds",
	Help:    "Publish message interception latency",
	Buckets: pkgmetrics.AuthLatencyBuckets,
})

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
	Buckets: pkgmetrics.BackendLatencyBuckets,
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
// Multi-Issuer OIDC Metrics
// =============================================================================

var oidcValidationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_oidc_validation_total",
	Help: "Total OIDC token validations",
}, []string{"tenant_id", "result"}) // result: success, invalid_signature, unknown_issuer, expired

var oidcValidationLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "gateway_oidc_validation_latency_seconds",
	Help:    "OIDC token validation latency",
	Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
}, []string{"tenant_id"})

var issuerCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gateway_issuer_cache_hits_total",
	Help: "Issuer cache hits",
})

var issuerCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "gateway_issuer_cache_misses_total",
	Help: "Issuer cache misses",
})

// =============================================================================
// Channel Rules Metrics
// =============================================================================

var channelRulesLookupTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_channel_rules_lookup_total",
	Help: "Channel rules lookups",
}, []string{"tenant_id", "source"}) // source: cache, database, fallback

var channelAuthorizationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_channel_authorization_total",
	Help: "Channel authorization decisions",
}, []string{"tenant_id", "result"}) // result: allowed, denied

// =============================================================================
// JWKS Fetch Metrics
// =============================================================================

var jwksFetchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gateway_jwks_fetch_total",
	Help: "JWKS endpoint fetches",
}, []string{"issuer", "result"}) // result: success, timeout, error

var jwksFetchLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "gateway_jwks_fetch_latency_seconds",
	Help:    "JWKS fetch latency",
	Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
}, []string{"issuer"})

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

// RecordAuthRefresh records an auth refresh attempt result.
func RecordAuthRefresh(result string) {
	authRefreshTotal.WithLabelValues(result).Inc()
}

// RecordAuthRefreshLatency records auth refresh processing latency.
func RecordAuthRefreshLatency(seconds float64) {
	authRefreshLatency.Observe(seconds)
}

// RecordForcedUnsubscription records a forced unsubscription event.
func RecordForcedUnsubscription() {
	forcedUnsubscriptionsTotal.Inc()
}

// RecordPublishResult records a publish message interception result.
func RecordPublishResult(tenant, result string) {
	if tenant == "" {
		tenant = "unknown"
	}
	publishTotal.WithLabelValues(tenant, result).Inc()
}

// RecordPublishLatency records the latency of publish message interception.
func RecordPublishLatency(seconds float64) {
	publishLatency.Observe(seconds)
}

// RecordOIDCValidation records an OIDC token validation result.
func RecordOIDCValidation(tenantID, result string, latency time.Duration) {
	if tenantID == "" {
		tenantID = "unknown"
	}
	oidcValidationTotal.WithLabelValues(tenantID, result).Inc()
	oidcValidationLatency.WithLabelValues(tenantID).Observe(latency.Seconds())
}

// RecordIssuerCacheHit records an issuer cache hit.
func RecordIssuerCacheHit() {
	issuerCacheHits.Inc()
}

// RecordIssuerCacheMiss records an issuer cache miss.
func RecordIssuerCacheMiss() {
	issuerCacheMisses.Inc()
}

// RecordChannelRulesLookup records a channel rules lookup.
func RecordChannelRulesLookup(tenantID, source string) {
	if tenantID == "" {
		tenantID = "unknown"
	}
	channelRulesLookupTotal.WithLabelValues(tenantID, source).Inc()
}

// RecordChannelAuthorization records a channel authorization decision.
func RecordChannelAuthorization(tenantID, result string) {
	if tenantID == "" {
		tenantID = "unknown"
	}
	channelAuthorizationTotal.WithLabelValues(tenantID, result).Inc()
}

// RecordJWKSFetch records a JWKS fetch operation.
func RecordJWKSFetch(issuer, result string, latency time.Duration) {
	if issuer == "" {
		issuer = "unknown"
	}
	jwksFetchTotal.WithLabelValues(issuer, result).Inc()
	jwksFetchLatency.WithLabelValues(issuer).Observe(latency.Seconds())
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

// Interface compliance checks.
var (
	_ pkgmetrics.AccessDenialMetrics = (*AccessDenialMetricsAdapter)(nil)
	_ pkgmetrics.CacheMetrics        = (*KeyCacheMetricsAdapter)(nil)
)
