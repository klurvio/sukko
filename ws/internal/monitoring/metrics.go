package monitoring

import (
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Toniq-Labs/odin-ws/internal/platform"
	"github.com/Toniq-Labs/odin-ws/internal/types"
)

// Prometheus metrics for WebSocket server
// These metrics can be scraped by Prometheus and visualized in Grafana
var (
	// Connection metrics
	connectionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_connections_total",
		Help: "Total number of WebSocket connections established",
	})

	connectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_connections_active",
		Help: "Current number of active WebSocket connections",
	})

	connectionsMax = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_connections_max",
		Help: "Maximum allowed WebSocket connections",
	})

	ConnectionsFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_connections_failed_total",
		Help: "Total number of failed connection attempts",
	})

	// Disconnect tracking with categorization
	disconnectsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_disconnects_total",
		Help: "Total disconnections by reason and who initiated",
	}, []string{"reason", "initiated_by"})

	connectionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ws_connection_duration_seconds",
		Help:    "Connection duration before disconnect",
		Buckets: []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600}, // 1s to 1hr
	}, []string{"reason"})

	// Message metrics
	messagesSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_messages_sent_total",
		Help: "Total number of messages sent to clients",
	})

	messagesReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_messages_received_total",
		Help: "Total number of messages received from clients",
	})

	bytesSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_bytes_sent_total",
		Help: "Total number of bytes sent to clients",
	})

	bytesReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_bytes_received_total",
		Help: "Total number of bytes received from clients",
	})

	// Reliability metrics
	slowClientsDisconnected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_slow_clients_disconnected_total",
		Help: "Total number of slow clients disconnected",
	})

	rateLimitedMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_rate_limited_messages_total",
		Help: "Total number of rate limited messages",
	})

	replayRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_replay_requests_total",
		Help: "Total number of replay requests served",
	})

	connectionRateLimited = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_connection_rate_limited_total",
		Help: "Total number of connections rate limited by type (per_ip or global)",
	}, []string{"type"})

	// Enhanced drop tracking with categorization
	droppedBroadcastsDetailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_dropped_broadcasts_detailed_total",
		Help: "Total broadcast messages dropped by channel and reason",
	}, []string{"channel", "reason"})

	clientSendBufferSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ws_client_send_buffer_size",
		Help:    "Distribution of client send buffer usage",
		Buckets: []float64{0, 64, 128, 256, 384, 448, 480, 496, 504, 510, 511, 512}, // Track saturation
	}, []string{"percentile"})

	slowClientAttempts = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "ws_slow_client_attempts_before_disconnect",
		Help:    "Distribution of send attempts before slow client disconnect",
		Buckets: []float64{1, 2, 3, 4, 5, 10, 20},
	})

	// System metrics
	memoryUsageBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_memory_bytes",
		Help: "Current memory usage in bytes",
	})

	memoryLimitBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_memory_limit_bytes",
		Help: "Memory limit in bytes (from cgroup)",
	})

	CpuUsagePercent = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_usage_percent",
		Help: "Current CPU usage percentage (container-aware: % of allocated CPUs)",
	})

	// Enhanced CPU metrics (container-aware)
	CpuContainerPercent = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_container_percent",
		Help: "CPU usage as percentage of container allocation (0-100%)",
	})

	CpuHostPercent = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_host_percent",
		Help: "CPU usage as percentage of total host CPUs (for reference)",
	})

	CpuAllocationCores = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_cpu_allocation_cores",
		Help: "Number of CPU cores allocated to container",
	})

	CpuThrottledSecondsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_cpu_throttled_seconds_total",
		Help: "Total time (seconds) container CPU was throttled by cgroup",
	})

	CpuThrottleEventsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_cpu_throttle_events_total",
		Help: "Total number of times container hit CPU limit and was throttled",
	})

	goroutinesActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_goroutines_active",
		Help: "Current number of active goroutines",
	})

	// Kafka metrics
	kafkaConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_kafka_connected",
		Help: "Kafka consumer status (1=running, 0=stopped)",
	})

	kafkaMessagesReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_messages_received_total",
		Help: "Total number of messages received from Kafka",
	})

	kafkaMessagesDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_messages_dropped_total",
		Help: "Total number of Kafka messages dropped due to backpressure",
	})

	// Kafka producer metrics (client-to-Kafka publishing)
	kafkaMessagesPublished = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_messages_published_total",
		Help: "Total number of messages published to Kafka by clients",
	})

	kafkaPublishErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_kafka_publish_errors_total",
		Help: "Total number of failed publish attempts to Kafka",
	})

	// Dynamic capacity metrics
	capacityMaxConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_capacity_max_connections",
		Help: "Current dynamic maximum connections allowed",
	})

	capacityCPUThreshold = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ws_capacity_cpu_threshold_percent",
		Help: "CPU threshold for rejecting new connections",
	})

	capacityRejectionsCPU = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_capacity_rejections_total",
		Help: "Total connection rejections by reason",
	}, []string{"reason"})

	capacityAvailableHeadroom = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ws_capacity_headroom_percent",
		Help: "Available resource headroom (CPU and memory)",
	}, []string{"resource"})

	// Error tracking
	errorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_errors_total",
		Help: "Total errors by type and severity",
	}, []string{"type", "severity"})
)

func init() {
	// Register all metrics with Prometheus
	prometheus.MustRegister(connectionsTotal)
	prometheus.MustRegister(connectionsActive)
	prometheus.MustRegister(connectionsMax)
	prometheus.MustRegister(ConnectionsFailed)
	prometheus.MustRegister(disconnectsTotal)
	prometheus.MustRegister(connectionDuration)

	prometheus.MustRegister(messagesSent)
	prometheus.MustRegister(messagesReceived)
	prometheus.MustRegister(bytesSent)
	prometheus.MustRegister(bytesReceived)

	prometheus.MustRegister(slowClientsDisconnected)
	prometheus.MustRegister(rateLimitedMessages)
	prometheus.MustRegister(replayRequests)
	prometheus.MustRegister(connectionRateLimited)
	prometheus.MustRegister(droppedBroadcastsDetailed)
	prometheus.MustRegister(clientSendBufferSize)
	prometheus.MustRegister(slowClientAttempts)

	prometheus.MustRegister(memoryUsageBytes)
	prometheus.MustRegister(memoryLimitBytes)
	prometheus.MustRegister(CpuUsagePercent)
	prometheus.MustRegister(CpuContainerPercent)
	prometheus.MustRegister(CpuHostPercent)
	prometheus.MustRegister(CpuAllocationCores)
	prometheus.MustRegister(CpuThrottledSecondsTotal)
	prometheus.MustRegister(CpuThrottleEventsTotal)
	prometheus.MustRegister(goroutinesActive)

	prometheus.MustRegister(kafkaConnected)
	prometheus.MustRegister(kafkaMessagesReceived)
	prometheus.MustRegister(kafkaMessagesDropped)
	prometheus.MustRegister(kafkaMessagesPublished)
	prometheus.MustRegister(kafkaPublishErrors)

	prometheus.MustRegister(capacityMaxConnections)
	prometheus.MustRegister(capacityCPUThreshold)
	prometheus.MustRegister(capacityRejectionsCPU)
	prometheus.MustRegister(capacityAvailableHeadroom)

	prometheus.MustRegister(errorsTotal)
}

// ServerMetrics is an interface that provides metrics access to server state
// This avoids circular dependencies between monitoring and core packages
type ServerMetrics interface {
	GetConfig() types.ServerConfig
	GetStats() *types.Stats
	GetKafkaConsumer() any // Returns kafka.Consumer but we only check if nil
}

// MetricsCollector handles periodic collection of system metrics
type MetricsCollector struct {
	server   ServerMetrics
	stopChan chan struct{}
}

func NewMetricsCollector(server ServerMetrics) *MetricsCollector {
	return &MetricsCollector{
		server:   server,
		stopChan: make(chan struct{}),
	}
}

// Start begins collecting metrics periodically
func (m *MetricsCollector) Start() {
	config := m.server.GetConfig()

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
				m.collect()
			case <-m.stopChan:
				return
			}
		}
	}()
}

// Stop stops the metrics collector
func (m *MetricsCollector) Stop() {
	close(m.stopChan)
}

// collect gathers current metrics
func (m *MetricsCollector) collect() {
	stats := m.server.GetStats()

	// Connection metrics - DISABLED per-shard setting
	// connectionsActive is now set by LoadBalancer.aggregateMetrics() which sums all shards
	// This fixes the multi-shard overwrite bug where each shard's collector would overwrite the gauge
	_ = atomic.LoadInt64(&stats.CurrentConnections) // Keep read for stats object usage

	// Memory metrics
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	memoryUsageBytes.Set(float64(mem.Alloc))

	// CPU metrics (estimated)
	CpuUsagePercent.Set(m.estimateCPU())

	// Goroutine metrics
	goroutinesActive.Set(float64(runtime.NumGoroutine()))

	// Kafka status
	if m.server.GetKafkaConsumer() != nil {
		kafkaConnected.Set(1)
	} else {
		kafkaConnected.Set(0)
	}
}

// estimateCPU gets CPU usage from server stats
func (m *MetricsCollector) estimateCPU() float64 {
	// Read CPU percentage from server stats (collected by collectMetrics goroutine)
	stats := m.server.GetStats()
	stats.Mu.RLock()
	cpuPercent := stats.CPUPercent
	stats.Mu.RUnlock()
	return cpuPercent
}

// UpdateConnectionMetrics updates connection-related metrics
func UpdateConnectionMetrics(server ServerMetrics) {
	stats := server.GetStats()
	connectionsTotal.Inc()
	// NOTE: connectionsActive is now set by LoadBalancer.aggregateMetrics() to fix
	// the multi-shard overwrite bug. Per-shard collectors were overwriting each other.
	_ = atomic.LoadInt64(&stats.CurrentConnections) // Keep the read for potential future use
}

// SetAggregatedConnectionMetrics sets the aggregated connection metrics from LoadBalancer
// This should be called by the LoadBalancer which has visibility into all shards
func SetAggregatedConnectionMetrics(totalConnections, totalMaxConnections int64) {
	connectionsActive.Set(float64(totalConnections))
	connectionsMax.Set(float64(totalMaxConnections))
}

// UpdateMessageMetrics updates message-related metrics
func UpdateMessageMetrics(sent, received int64) {
	if sent > 0 {
		messagesSent.Add(float64(sent))
	}
	if received > 0 {
		messagesReceived.Add(float64(received))
	}
}

// UpdateBytesMetrics updates bytes sent/received metrics
func UpdateBytesMetrics(sent, received int64) {
	if sent > 0 {
		bytesSent.Add(float64(sent))
	}
	if received > 0 {
		bytesReceived.Add(float64(received))
	}
}

// IncrementSlowClientDisconnects increments slow client disconnect counter
func IncrementSlowClientDisconnects() {
	slowClientsDisconnected.Inc()
}

// IncrementRateLimitedMessages increments rate limited message counter
func IncrementRateLimitedMessages() {
	rateLimitedMessages.Inc()
}

// IncrementReplayRequests increments replay request counter
func IncrementReplayRequests() {
	replayRequests.Inc()
}

// IncrementConnectionRateLimit increments connection rate limit counter
func IncrementConnectionRateLimit(limitType string) {
	connectionRateLimited.WithLabelValues(limitType).Inc()
}

// IncrementKafkaMessages increments Kafka message counter
func IncrementKafkaMessages() {
	kafkaMessagesReceived.Inc()
}

// IncrementKafkaDropped increments dropped Kafka message counter
func IncrementKafkaDropped() {
	kafkaMessagesDropped.Inc()
}

// IncrementMessagesPublished increments the Kafka publish success counter
func IncrementMessagesPublished() {
	kafkaMessagesPublished.Inc()
}

// IncrementPublishErrors increments the Kafka publish error counter
func IncrementPublishErrors() {
	kafkaPublishErrors.Inc()
}

// UpdateCapacityMetrics updates dynamic capacity metrics
func UpdateCapacityMetrics(maxConnections int, cpuThreshold float64) {
	capacityMaxConnections.Set(float64(maxConnections))
	capacityCPUThreshold.Set(cpuThreshold)
}

// IncrementCapacityRejection records a connection rejection with reason
func IncrementCapacityRejection(reason string) {
	capacityRejectionsCPU.WithLabelValues(reason).Inc()
}

// UpdateCapacityHeadroom updates available resource headroom
func UpdateCapacityHeadroom(cpuHeadroom, memHeadroom float64) {
	capacityAvailableHeadroom.WithLabelValues("cpu").Set(cpuHeadroom)
	capacityAvailableHeadroom.WithLabelValues("memory").Set(memHeadroom)
}

// Error severity levels for metrics and logging
const (
	ErrorSeverityWarning  = "warning"  // Non-critical, service continues
	ErrorSeverityCritical = "critical" // Critical but recoverable
	ErrorSeverityFatal    = "fatal"    // Service cannot continue
)

// Error types for categorization
const (
	ErrorTypeKafka         = "kafka"
	ErrorTypeBroadcast     = "broadcast"
	ErrorTypeSerialization = "serialization"
	ErrorTypeConnection    = "connection"
	ErrorTypeHealth        = "health"
)

// RecordError tracks an error in both Prometheus and returns true to signal logging needed
func RecordError(errorType, severity string) {
	errorsTotal.WithLabelValues(errorType, severity).Inc()
}

// RecordKafkaError tracks Kafka-related errors
func RecordKafkaError(severity string) {
	errorsTotal.WithLabelValues(ErrorTypeKafka, severity).Inc()
}

// RecordBroadcastError tracks broadcast-related errors
func RecordBroadcastError(severity string) {
	errorsTotal.WithLabelValues(ErrorTypeBroadcast, severity).Inc()
}

// RecordSerializationError tracks serialization errors
func RecordSerializationError(severity string) {
	errorsTotal.WithLabelValues(ErrorTypeSerialization, severity).Inc()
}

// RecordConnectionError tracks connection errors
func RecordConnectionError(severity string) {
	errorsTotal.WithLabelValues(ErrorTypeConnection, severity).Inc()
}

// Disconnect reasons - standardized constants for categorization
const (
	DisconnectReasonReadError         = "read_error"          // Client stopped reading (network issue, crash)
	DisconnectReasonWriteTimeout      = "write_timeout"       // Slow client (send buffer full)
	DisconnectReasonPingTimeout       = "ping_timeout"        // Client didn't respond to ping
	DisconnectReasonRateLimitExceeded = "rate_limit_exceeded" // Client sent too many messages
	DisconnectReasonServerShutdown    = "server_shutdown"     // Graceful shutdown
	DisconnectReasonClientInitiated   = "client_initiated"    // Normal close from client
	DisconnectReasonSubscriptionError = "subscription_error"  // Invalid subscription
	DisconnectReasonSendChannelClosed = "send_channel_closed" // Server closed send channel
)

// Who initiated the disconnect
const (
	DisconnectInitiatedByClient = "client"
	DisconnectInitiatedByServer = "server"
)

// Drop reasons - why broadcast messages were dropped
const (
	DropReasonSendTimeout        = "send_timeout"        // Timed out trying to send to client
	DropReasonBufferFull         = "buffer_full"         // Client send buffer is full
	DropReasonClientDisconnected = "client_disconnected" // Client already disconnected
)

// RecordDisconnect tracks a disconnect with reason, initiator, and duration
func RecordDisconnect(reason, initiatedBy string, duration time.Duration) {
	disconnectsTotal.WithLabelValues(reason, initiatedBy).Inc()
	connectionDuration.WithLabelValues(reason).Observe(duration.Seconds())
}

// RecordDisconnectWithStats tracks a disconnect and updates both Prometheus and Stats
func RecordDisconnectWithStats(stats *types.Stats, reason, initiatedBy string, duration time.Duration) {
	// Update Prometheus metrics
	disconnectsTotal.WithLabelValues(reason, initiatedBy).Inc()
	connectionDuration.WithLabelValues(reason).Observe(duration.Seconds())

	// Update Stats struct for /health endpoint
	stats.DisconnectsMu.Lock()
	stats.DisconnectsByReason[reason]++
	stats.DisconnectsMu.Unlock()
}

// RecordDroppedBroadcast tracks a dropped broadcast message with channel and reason
func RecordDroppedBroadcast(channel, reason string) {
	droppedBroadcastsDetailed.WithLabelValues(channel, reason).Inc()
}

// RecordDroppedBroadcastWithStats tracks a dropped broadcast and updates both Prometheus and Stats
func RecordDroppedBroadcastWithStats(stats *types.Stats, channel, reason string) {
	// Update Prometheus metrics
	droppedBroadcastsDetailed.WithLabelValues(channel, reason).Inc()

	// Update Stats struct for /health endpoint
	stats.DropsMu.Lock()
	stats.DroppedBroadcastsByChannel[channel]++
	stats.DropsMu.Unlock()
}

// RecordSlowClientAttempt records the number of send attempts before slow client disconnect
func RecordSlowClientAttempt(attempts int) {
	slowClientAttempts.Observe(float64(attempts))
}

// RecordClientBufferSize samples a client's send buffer usage
func RecordClientBufferSize(bufferLen, bufferCap int) {
	clientSendBufferSize.WithLabelValues("all").Observe(float64(bufferLen))
}

// RecordClientBufferSizeWithStats samples a client's send buffer and updates both Prometheus and Stats
func RecordClientBufferSizeWithStats(stats *types.Stats, bufferLen, bufferCap int) {
	// Update Prometheus metrics
	clientSendBufferSize.WithLabelValues("all").Observe(float64(bufferLen))

	// Update Stats struct for /health endpoint (keep last 100 samples)
	usagePercent := int(float64(bufferLen) / float64(bufferCap) * 100)
	stats.BuffersMu.Lock()
	stats.BufferSaturationSamples = append(stats.BufferSaturationSamples, usagePercent)
	if len(stats.BufferSaturationSamples) > 100 {
		stats.BufferSaturationSamples = stats.BufferSaturationSamples[1:]
	}
	stats.BuffersMu.Unlock()
}

// handleMetrics serves Prometheus metrics at /metrics endpoint
func HandleMetrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}
