package metrics

import "time"

// CacheMetrics is a callback interface for reporting cache metrics.
// Implementations can report to Prometheus or other monitoring systems.
type CacheMetrics interface {
	// OnCacheHit is called when a key is found in cache.
	OnCacheHit()
	// OnCacheMiss is called when a key is not found in cache.
	OnCacheMiss()
	// OnCacheRefresh is called after a cache refresh attempt.
	OnCacheRefresh(success bool, keyCount int)
}

// NoopCacheMetrics is a no-op implementation of CacheMetrics.
type NoopCacheMetrics struct{}

func (NoopCacheMetrics) OnCacheHit()                  {}
func (NoopCacheMetrics) OnCacheMiss()                 {}
func (NoopCacheMetrics) OnCacheRefresh(_ bool, _ int) {}

// AccessDenialMetrics is a callback interface for recording access denial metrics.
// This allows auth packages to report metrics without depending on specific monitoring packages.
type AccessDenialMetrics interface {
	// OnAccessDenied is called when access is denied.
	// resourceType is "channel" or "topic", reason explains why access was denied.
	OnAccessDenied(resourceType, reason string)
}

// NoopAccessDenialMetrics is a no-op implementation of AccessDenialMetrics.
type NoopAccessDenialMetrics struct{}

func (NoopAccessDenialMetrics) OnAccessDenied(_, _ string) {}

// PoolMetrics is a callback interface for reporting multi-tenant consumer pool metrics.
// This allows orchestration packages to report metrics without creating circular dependencies.
type PoolMetrics interface {
	// OnMessageRouted is called when a message is routed to the broadcast bus.
	OnMessageRouted()
	// OnRefresh is called after a topic refresh operation.
	OnRefresh(success bool, topicsSubscribed, dedicatedConsumers int)
}

// NoopPoolMetrics is a no-op implementation of PoolMetrics.
type NoopPoolMetrics struct{}

func (NoopPoolMetrics) OnMessageRouted()           {}
func (NoopPoolMetrics) OnRefresh(_ bool, _, _ int) {}

// DisconnectMetrics is a callback interface for recording disconnect events.
type DisconnectMetrics interface {
	// OnDisconnect is called when a connection is closed.
	// reason should be one of the Disconnect* constants.
	// initiatedBy should be InitiatedByClient or InitiatedByServer.
	OnDisconnect(reason, initiatedBy string, duration time.Duration)
}

// NoopDisconnectMetrics is a no-op implementation of DisconnectMetrics.
type NoopDisconnectMetrics struct{}

func (NoopDisconnectMetrics) OnDisconnect(_, _ string, _ time.Duration) {}
