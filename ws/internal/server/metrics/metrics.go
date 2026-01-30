// Package metrics provides Prometheus metrics for the WebSocket server.
// All metrics use promauto for automatic registration.
package metrics

import (
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	pkgmetrics "github.com/Toniq-Labs/odin-ws/internal/shared/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/shared/platform"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// =============================================================================
// Connection Metrics
// =============================================================================

var (
	connectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_connections_total",
		Help: "Total number of WebSocket connections established",
	})

	connectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_connections_active",
		Help: "Current number of active WebSocket connections",
	})

	connectionsMax = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_connections_max",
		Help: "Maximum allowed WebSocket connections",
	})

	// ConnectionsFailed tracks total failed connection attempts.
	ConnectionsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_connections_failed_total",
		Help: "Total number of failed connection attempts",
	})

	disconnectsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_disconnects_total",
		Help: "Total disconnections by reason and who initiated",
	}, []string{"reason", "initiated_by"})

	connectionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ws_connection_duration_seconds",
		Help:    "Connection duration before disconnect",
		Buckets: pkgmetrics.ConnectionDurationBuckets,
	}, []string{"reason"})
)

// =============================================================================
// Message Metrics
// =============================================================================

var (
	messagesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_messages_sent_total",
		Help: "Total number of messages sent to clients",
	})

	messagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_messages_received_total",
		Help: "Total number of messages received from clients",
	})

	bytesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_bytes_sent_total",
		Help: "Total number of bytes sent to clients",
	})

	bytesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_bytes_received_total",
		Help: "Total number of bytes received from clients",
	})
)

// =============================================================================
// Reliability Metrics
// =============================================================================

var (
	slowClientsDisconnected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_slow_clients_disconnected_total",
		Help: "Total number of slow clients disconnected",
	})

	rateLimitedMessages = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_rate_limited_messages_total",
		Help: "Total number of rate limited messages",
	})

	replayRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_replay_requests_total",
		Help: "Total number of replay requests served",
	})

	connectionRateLimited = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_connection_rate_limited_total",
		Help: "Total number of connections rate limited by type (per_ip or global)",
	}, []string{"type"})

	droppedBroadcastsDetailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_dropped_broadcasts_detailed_total",
		Help: "Total broadcast messages dropped by channel and reason",
	}, []string{"channel", "reason"})

	clientSendBufferSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ws_client_send_buffer_size",
		Help:    "Distribution of client send buffer usage",
		Buckets: pkgmetrics.BufferSaturationBuckets,
	}, []string{"percentile"})

	slowClientAttempts = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ws_slow_client_attempts_before_disconnect",
		Help:    "Distribution of send attempts before slow client disconnect",
		Buckets: []float64{1, 2, 3, 4, 5, 10, 20},
	})
)

// =============================================================================
// System Metrics
// =============================================================================

var (
	memoryUsageBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_memory_bytes",
		Help: "Current memory usage in bytes",
	})

	memoryLimitBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_memory_limit_bytes",
		Help: "Memory limit in bytes (from cgroup)",
	})

	// CPUUsagePercent tracks current CPU usage as a percentage.
	CPUUsagePercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_usage_percent",
		Help: "Current CPU usage percentage (container-aware: % of allocated CPUs)",
	})

	// CPUContainerPercent tracks CPU usage as a percentage of container allocation.
	CPUContainerPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_container_percent",
		Help: "CPU usage as percentage of container allocation (0-100%)",
	})

	// CPUHostPercent tracks CPU usage as a percentage of total host CPUs.
	CPUHostPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_host_percent",
		Help: "CPU usage as percentage of total host CPUs (for reference)",
	})

	// CPUAllocationCores tracks the number of CPU cores allocated to the container.
	CPUAllocationCores = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_allocation_cores",
		Help: "Number of CPU cores allocated to container",
	})

	// CPUThrottledSecondsTotal tracks the total time container CPU was throttled.
	CPUThrottledSecondsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_cpu_throttled_seconds_total",
		Help: "Total time (seconds) container CPU was throttled by cgroup",
	})

	// CPUThrottleEventsTotal tracks the number of times the container hit its CPU limit.
	CPUThrottleEventsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_cpu_throttle_events_total",
		Help: "Total number of times container hit CPU limit and was throttled",
	})

	goroutinesActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_goroutines_active",
		Help: "Current number of active goroutines",
	})
)

// =============================================================================
// Kafka Metrics
// =============================================================================

var (
	kafkaConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_kafka_connected",
		Help: "Kafka consumer status (1=running, 0=stopped)",
	})

	kafkaMessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_messages_received_total",
		Help: "Total number of messages received from Kafka",
	})

	kafkaMessagesDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_messages_dropped_total",
		Help: "Total number of Kafka messages dropped due to backpressure",
	})

	kafkaMessagesPublished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_messages_published_total",
		Help: "Total number of messages published to Kafka by clients",
	})

	kafkaPublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_publish_errors_total",
		Help: "Total number of failed publish attempts to Kafka",
	})
)

// =============================================================================
// Capacity Metrics
// =============================================================================

var (
	capacityMaxConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_capacity_max_connections",
		Help: "Current dynamic maximum connections allowed",
	})

	capacityCPUThreshold = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_capacity_cpu_threshold_percent",
		Help: "CPU threshold for rejecting new connections",
	})

	capacityRejectionsCPU = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_capacity_rejections_total",
		Help: "Total connection rejections by reason",
	}, []string{"reason"})

	capacityAvailableHeadroom = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ws_capacity_headroom_percent",
		Help: "Available resource headroom (CPU and memory)",
	}, []string{"resource"})
)

// =============================================================================
// Error Metrics
// =============================================================================

var (
	errorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_errors_total",
		Help: "Total errors by type and severity",
	}, []string{"type", "severity"})
)

// =============================================================================
// Multi-Tenant Pool Metrics
// =============================================================================

var (
	multitenantTopicsSubscribed = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_multitenant_topics_subscribed",
		Help: "Number of topics subscribed by shared consumer",
	})

	multitenantDedicatedConsumers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_multitenant_dedicated_consumers",
		Help: "Number of dedicated consumer instances for isolated tenants",
	})

	multitenantMessagesRouted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_multitenant_messages_routed_total",
		Help: "Total messages routed through multi-tenant pool",
	})

	multitenantRefreshTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_multitenant_refresh_total",
		Help: "Topic refresh operations by result",
	}, []string{"result"})
)

// =============================================================================
// Connection Helper Functions
// =============================================================================

// UpdateConnectionMetrics increments the connection counter.
func UpdateConnectionMetrics() {
	connectionsTotal.Inc()
}

// SetAggregatedConnectionMetrics sets the aggregated connection metrics from LoadBalancer.
func SetAggregatedConnectionMetrics(totalConnections, totalMaxConnections int64) {
	connectionsActive.Set(float64(totalConnections))
	connectionsMax.Set(float64(totalMaxConnections))
}

// SetMaxConnections sets the maximum connections gauge.
func SetMaxConnections(maxConns int) {
	connectionsMax.Set(float64(maxConns))
}

// RecordDisconnect tracks a disconnect with reason, initiator, and duration.
func RecordDisconnect(reason, initiatedBy string, duration time.Duration) {
	disconnectsTotal.WithLabelValues(reason, initiatedBy).Inc()
	connectionDuration.WithLabelValues(reason).Observe(duration.Seconds())
}

// RecordDisconnectWithStats tracks a disconnect and updates both Prometheus and Stats.
func RecordDisconnectWithStats(stats *types.Stats, reason, initiatedBy string, duration time.Duration) {
	disconnectsTotal.WithLabelValues(reason, initiatedBy).Inc()
	connectionDuration.WithLabelValues(reason).Observe(duration.Seconds())

	stats.DisconnectsMu.Lock()
	stats.DisconnectsByReason[reason]++
	stats.DisconnectsMu.Unlock()
}

// =============================================================================
// Message Helper Functions
// =============================================================================

// UpdateMessageMetrics updates message-related metrics.
func UpdateMessageMetrics(sent, received int64) {
	if sent > 0 {
		messagesSent.Add(float64(sent))
	}
	if received > 0 {
		messagesReceived.Add(float64(received))
	}
}

// UpdateBytesMetrics updates bytes sent/received metrics.
func UpdateBytesMetrics(sent, received int64) {
	if sent > 0 {
		bytesSent.Add(float64(sent))
	}
	if received > 0 {
		bytesReceived.Add(float64(received))
	}
}

// =============================================================================
// Reliability Helper Functions
// =============================================================================

// IncrementSlowClientDisconnects increments slow client disconnect counter.
func IncrementSlowClientDisconnects() {
	slowClientsDisconnected.Inc()
}

// IncrementRateLimitedMessages increments rate limited message counter.
func IncrementRateLimitedMessages() {
	rateLimitedMessages.Inc()
}

// IncrementReplayRequests increments replay request counter.
func IncrementReplayRequests() {
	replayRequests.Inc()
}

// IncrementConnectionRateLimit increments connection rate limit counter.
func IncrementConnectionRateLimit(limitType string) {
	connectionRateLimited.WithLabelValues(limitType).Inc()
}

// RecordDroppedBroadcast tracks a dropped broadcast message.
func RecordDroppedBroadcast(channel, reason string) {
	droppedBroadcastsDetailed.WithLabelValues(channel, reason).Inc()
}

// RecordDroppedBroadcastWithStats tracks a dropped broadcast and updates Stats.
func RecordDroppedBroadcastWithStats(stats *types.Stats, channel, reason string) {
	droppedBroadcastsDetailed.WithLabelValues(channel, reason).Inc()

	stats.DropsMu.Lock()
	stats.DroppedBroadcastsByChannel[channel]++
	stats.DropsMu.Unlock()
}

// RecordSlowClientAttempt records send attempts before slow client disconnect.
func RecordSlowClientAttempt(attempts int) {
	slowClientAttempts.Observe(float64(attempts))
}

// RecordClientBufferSize samples a client's send buffer usage.
func RecordClientBufferSize(bufferLen, _ int) {
	clientSendBufferSize.WithLabelValues("all").Observe(float64(bufferLen))
}

// RecordClientBufferSizeWithStats samples buffer and updates Stats.
func RecordClientBufferSizeWithStats(stats *types.Stats, bufferLen, bufferCap int) {
	clientSendBufferSize.WithLabelValues("all").Observe(float64(bufferLen))

	usagePercent := int(float64(bufferLen) / float64(bufferCap) * 100)
	stats.BuffersMu.Lock()
	stats.BufferSaturationSamples = append(stats.BufferSaturationSamples, usagePercent)
	if len(stats.BufferSaturationSamples) > 100 {
		stats.BufferSaturationSamples = stats.BufferSaturationSamples[1:]
	}
	stats.BuffersMu.Unlock()
}

// =============================================================================
// System Helper Functions
// =============================================================================

// SetMemoryUsage sets the current memory usage.
func SetMemoryUsage(bytes float64) {
	memoryUsageBytes.Set(bytes)
}

// SetMemoryLimit sets the memory limit.
func SetMemoryLimit(bytes float64) {
	memoryLimitBytes.Set(bytes)
}

// SetCPUUsage sets the CPU usage percentage.
func SetCPUUsage(percent float64) {
	CPUUsagePercent.Set(percent)
}

// SetGoroutines sets the active goroutine count.
func SetGoroutines(count int) {
	goroutinesActive.Set(float64(count))
}

// =============================================================================
// Kafka Helper Functions
// =============================================================================

// SetKafkaConnected sets the Kafka connection status.
func SetKafkaConnected(connected bool) {
	if connected {
		kafkaConnected.Set(1)
	} else {
		kafkaConnected.Set(0)
	}
}

// IncrementKafkaMessages increments Kafka message counter.
func IncrementKafkaMessages() {
	kafkaMessagesReceived.Inc()
}

// IncrementKafkaDropped increments dropped Kafka message counter.
func IncrementKafkaDropped() {
	kafkaMessagesDropped.Inc()
}

// IncrementMessagesPublished increments Kafka publish success counter.
func IncrementMessagesPublished() {
	kafkaMessagesPublished.Inc()
}

// IncrementPublishErrors increments Kafka publish error counter.
func IncrementPublishErrors() {
	kafkaPublishErrors.Inc()
}

// =============================================================================
// Capacity Helper Functions
// =============================================================================

// UpdateCapacityMetrics updates dynamic capacity metrics.
func UpdateCapacityMetrics(maxConnections int, cpuThreshold float64) {
	capacityMaxConnections.Set(float64(maxConnections))
	capacityCPUThreshold.Set(cpuThreshold)
}

// IncrementCapacityRejection records a connection rejection with reason.
func IncrementCapacityRejection(reason string) {
	capacityRejectionsCPU.WithLabelValues(reason).Inc()
}

// UpdateCapacityHeadroom updates available resource headroom.
func UpdateCapacityHeadroom(cpuHeadroom, memHeadroom float64) {
	capacityAvailableHeadroom.WithLabelValues("cpu").Set(cpuHeadroom)
	capacityAvailableHeadroom.WithLabelValues("memory").Set(memHeadroom)
}

// =============================================================================
// Error Helper Functions
// =============================================================================

// RecordError tracks an error.
func RecordError(errorType, severity string) {
	errorsTotal.WithLabelValues(errorType, severity).Inc()
}

// RecordKafkaError tracks Kafka-related errors.
func RecordKafkaError(severity string) {
	errorsTotal.WithLabelValues(pkgmetrics.ErrorTypeKafka, severity).Inc()
}

// RecordBroadcastError tracks broadcast-related errors.
func RecordBroadcastError(severity string) {
	errorsTotal.WithLabelValues(pkgmetrics.ErrorTypeBroadcast, severity).Inc()
}

// RecordSerializationError tracks serialization errors.
func RecordSerializationError(severity string) {
	errorsTotal.WithLabelValues(pkgmetrics.ErrorTypeSerialization, severity).Inc()
}

// RecordConnectionError tracks connection errors.
func RecordConnectionError(severity string) {
	errorsTotal.WithLabelValues(pkgmetrics.ErrorTypeConnection, severity).Inc()
}

// =============================================================================
// Multi-Tenant Pool Metrics Adapter
// =============================================================================

// MultiTenantPoolMetricsAdapter implements pkgmetrics.PoolMetrics for Prometheus.
type MultiTenantPoolMetricsAdapter struct{}

// Interface compliance check.
var _ pkgmetrics.PoolMetrics = (*MultiTenantPoolMetricsAdapter)(nil)

// OnMessageRouted records a routed message.
func (a *MultiTenantPoolMetricsAdapter) OnMessageRouted() {
	multitenantMessagesRouted.Inc()
}

// OnRefresh records a topic refresh operation and updates gauges.
func (a *MultiTenantPoolMetricsAdapter) OnRefresh(success bool, topicsSubscribed, dedicatedConsumers int) {
	if success {
		multitenantRefreshTotal.WithLabelValues("success").Inc()
		multitenantTopicsSubscribed.Set(float64(topicsSubscribed))
		multitenantDedicatedConsumers.Set(float64(dedicatedConsumers))
	} else {
		multitenantRefreshTotal.WithLabelValues("error").Inc()
	}
}

// =============================================================================
// Metrics Collector
// =============================================================================

// ServerMetrics is the interface required for metrics collection.
// Implemented by *Server.
type ServerMetrics interface {
	GetConfig() types.ServerConfig
	GetStats() *types.Stats
	GetKafkaConsumer() any // Returns kafka.Consumer but we only check if nil
}

// Collector handles periodic collection of system metrics.
type Collector struct {
	server   ServerMetrics
	stopChan chan struct{}
}

// NewCollector creates a new Collector for the given server.
func NewCollector(server ServerMetrics) *Collector {
	return &Collector{
		server:   server,
		stopChan: make(chan struct{}),
	}
}

// Start begins collecting metrics periodically.
func (c *Collector) Start() {
	config := c.server.GetConfig()

	// Set static metrics
	connectionsMax.Set(float64(config.MaxConnections))

	// Get memory limit from cgroup
	memLimit, err := platform.GetMemoryLimit()
	if err == nil && memLimit > 0 {
		memoryLimitBytes.Set(float64(memLimit))
	}

	// Collect metrics at configured interval
	ticker := time.NewTicker(config.MetricsInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.collect()
			case <-c.stopChan:
				return
			}
		}
	}()
}

// Stop stops the metrics collector.
func (c *Collector) Stop() {
	close(c.stopChan)
}

// collect gathers current metrics.
func (c *Collector) collect() {
	stats := c.server.GetStats()

	// Connection metrics - DISABLED per-shard setting
	// connectionsActive is now set by LoadBalancer.aggregateMetrics() which sums all shards
	// This fixes the multi-shard overwrite bug where each shard's collector would overwrite the gauge
	_ = stats.CurrentConnections.Load() // Keep read for stats object usage

	// Memory metrics
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	memoryUsageBytes.Set(float64(mem.Alloc))

	// CPU metrics (estimated)
	c.estimateCPU()

	// Goroutine metrics
	goroutinesActive.Set(float64(runtime.NumGoroutine()))

	// Kafka status
	if c.server.GetKafkaConsumer() != nil {
		kafkaConnected.Set(1)
	} else {
		kafkaConnected.Set(0)
	}
}

// estimateCPU gets CPU usage from server stats.
func (c *Collector) estimateCPU() float64 {
	// Read CPU percentage from server stats (collected by collectMetrics goroutine)
	stats := c.server.GetStats()
	stats.Mu.RLock()
	cpuPercent := stats.CPUPercent
	stats.Mu.RUnlock()
	CPUUsagePercent.Set(cpuPercent)
	return cpuPercent
}

// =============================================================================
// HTTP Handler
// =============================================================================

// HandleMetrics serves Prometheus metrics at /metrics endpoint.
func HandleMetrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}
