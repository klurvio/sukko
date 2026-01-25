package api

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics for the provisioning service.
// Follows the ws_ prefix pattern used throughout the codebase.
var (
	// Tenant operations
	tenantsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_provisioning_tenants_created_total",
		Help: "Total number of tenants created",
	})

	tenantsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_provisioning_tenants_active",
		Help: "Current number of active tenants",
	})

	tenantOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_provisioning_tenant_operations_total",
		Help: "Total tenant operations by type and result",
	}, []string{"operation", "result"})

	// Key operations
	keysCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_provisioning_keys_created_total",
		Help: "Total number of keys created",
	})

	keysRevoked = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_provisioning_keys_revoked_total",
		Help: "Total number of keys revoked",
	})

	keysActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_provisioning_keys_active",
		Help: "Current number of active keys",
	})

	// Topic operations
	topicsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_provisioning_topics_created_total",
		Help: "Total number of topics created",
	})

	// API request metrics
	apiRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_provisioning_api_requests_total",
		Help: "Total API requests by endpoint and status",
	}, []string{"endpoint", "method", "status"})

	apiLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ws_provisioning_api_latency_seconds",
		Help:    "API request latency in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"endpoint", "method"})

	// Auth metrics for provisioning API
	authAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_provisioning_auth_attempts_total",
		Help: "Total authentication attempts",
	}, []string{"result", "failure_reason"})

	authorizationDenials = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_provisioning_authorization_denials_total",
		Help: "Total authorization denials by reason",
	}, []string{"reason"})
)

// RecordTenantCreated increments the tenant created counter.
func RecordTenantCreated() {
	tenantsCreated.Inc()
}

// RecordTenantOperation records a tenant operation result.
func RecordTenantOperation(operation, result string) {
	tenantOperations.WithLabelValues(operation, result).Inc()
}

// SetActiveTenants sets the current active tenant count.
func SetActiveTenants(count int) {
	tenantsActive.Set(float64(count))
}

// RecordKeyCreated increments the key created counter.
func RecordKeyCreated() {
	keysCreated.Inc()
}

// RecordKeyRevoked increments the key revoked counter.
func RecordKeyRevoked() {
	keysRevoked.Inc()
}

// SetActiveKeys sets the current active key count.
func SetActiveKeys(count int) {
	keysActive.Set(float64(count))
}

// RecordTopicCreated increments the topic created counter.
func RecordTopicCreated(count int) {
	topicsCreated.Add(float64(count))
}

// RecordAPIRequest records an API request with its result.
func RecordAPIRequest(endpoint, method, status string) {
	apiRequests.WithLabelValues(endpoint, method, status).Inc()
}

// RecordAPILatency records API request latency.
func RecordAPILatency(endpoint, method string, seconds float64) {
	apiLatency.WithLabelValues(endpoint, method).Observe(seconds)
}

// RecordAuthAttempt records an authentication attempt.
func RecordAuthAttempt(result, failureReason string) {
	authAttempts.WithLabelValues(result, failureReason).Inc()
}

// RecordAuthorizationDenial records an authorization denial.
func RecordAuthorizationDenial(reason string) {
	authorizationDenials.WithLabelValues(reason).Inc()
}
