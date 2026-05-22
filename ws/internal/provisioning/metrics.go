package provisioning

import (
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

var reactivateTopicProvisionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "provisioning_reactivate_topic_provision_errors_total",
	Help: "Total errors provisioning infrastructure topics during tenant reactivation, by topic type",
}, []string{"topic_type"})

var createTenantTopicProvisionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "provisioning_create_tenant_topic_provision_errors_total",
	Help: "Total errors provisioning infrastructure topics during tenant creation saga, by topic type and phase",
}, []string{"topic_type", "phase"})

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
