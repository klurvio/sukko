package provisioning

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Topic type label values used by topic provision error metrics.
// Named constants per §I — label values are symbolic strings matched by dashboards and tests.
const (
	topicTypeDLQ     = "dlq"
	topicTypeDefault = "default"
)

// Phase label values for CreateTenant saga error metrics.
const (
	sagaPhaseCreate         = "create"
	sagaPhaseRollback       = "rollback"        // rollback attempt initiated (DB failure triggered cleanup)
	sagaPhaseRollbackFailed = "rollback_failed" // rollback delete also failed
)

// Rename status label values for tenant rename metrics.
const (
	renameStatusSuccess        = "success"
	renameStatusPartialFailure = "partial_failure" // quota re-apply failed; rename committed
)

var reactivateTopicProvisionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "provisioning_reactivate_topic_provision_errors_total",
	Help: "Total errors provisioning infrastructure topics during tenant reactivation, by topic type",
}, []string{"topic_type"})

var createTenantTopicProvisionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "provisioning_create_tenant_topic_provision_errors_total",
	Help: "Total errors provisioning infrastructure topics during tenant creation saga, by topic type and phase",
}, []string{"topic_type", "phase"})

var tenantRenamesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "provisioning_tenant_renames_total",
	Help: "Total tenant slug renames attempted, by outcome status",
}, []string{"status"})

var tenantRenameDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "provisioning_tenant_rename_duration_seconds",
	Help:    "Duration of the tenant slug rename saga from start to completion",
	Buckets: prometheus.DefBuckets,
})

var startupPendingRenames = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "provisioning_startup_pending_renames",
	Help: "Number of tenants with slug_rename_state='pending' found during startup scan (stuck sagas requiring operator action)",
})

var startupCompleteRenames = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "provisioning_startup_complete_renames",
	Help: "Number of tenants with slug_rename_state='complete' found during startup scan (hold period active, re-emitting TenantConfigChanged)",
})

// recordReactivateTopicProvisionError increments the reactivation topic provision error counter.
func recordReactivateTopicProvisionError(topicType string) {
	reactivateTopicProvisionErrors.WithLabelValues(topicType).Inc()
}

// recordCreateTenantTopicProvisionError increments the CreateTenant saga error counter.
// topicType MUST be topicTypeDLQ or topicTypeDefault.
// phase MUST be sagaPhaseCreate, sagaPhaseRollback, or sagaPhaseRollbackFailed.
func recordCreateTenantTopicProvisionError(topicType, phase string) {
	createTenantTopicProvisionErrors.WithLabelValues(topicType, phase).Inc()
}

// recordTenantRenamed records the outcome and duration of a tenant slug rename.
// status MUST be renameStatusSuccess or renameStatusPartialFailure.
func recordTenantRenamed(status string, dur time.Duration) {
	tenantRenamesTotal.WithLabelValues(status).Inc()
	tenantRenameDuration.Observe(dur.Seconds())
}

// recordStartupScanFindings sets the startup scan Prometheus gauges.
// Called once during Service.Start after scanning for pending/complete renames.
func recordStartupScanFindings(pending, complete int) {
	startupPendingRenames.Set(float64(pending))
	startupCompleteRenames.Set(float64(complete))
}
