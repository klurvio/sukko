package kafka

// Kafka message header names used for channel-topic routing.
const (
	HeaderChannel         = "x-sukko-channel"
	HeaderReason          = "x-sukko-reason"
	HeaderFailedTopics    = "x-sukko-failed-topics"
	HeaderSucceededTopics = "x-sukko-succeeded-topics"

	// Client message provenance headers stamped by the producer on every outbound record.
	HeaderClientID  = "client_id"
	HeaderSource    = "source"
	HeaderTimestamp = "timestamp"
	SourceWSClient  = "ws-client"
)

// Reason codes embedded in dead-letter message headers.
const (
	ReasonNoRoutingRuleMatched   = "no_routing_rule_matched"
	ReasonMissingChannelHeader   = "missing_channel_header"
	ReasonTenantPrefixMismatch   = "tenant_prefix_mismatch"
	ReasonInvalidChannelKey      = "invalid_channel_key"
	ReasonFanoutTopicWriteFailed = "fanout_topic_write_failed"
)

// Prometheus metric names for routing and DLQ operations.
const (
	MetricDeadLetterTotal        = "ws_routing_dead_letter_total"
	MetricFanoutWriteFailedTotal = "ws_routing_fanout_write_failed_total"
	MetricDLQWriteFailedTotal    = "ws_routing_dlq_write_failed_total"
	MetricMalformedTopicTotal    = "ws_routing_malformed_topic_total"
	MetricFanoutDroppedTotal     = "ws_routing_fanout_dropped_total"
)

// Prometheus label key constants for routing metrics.
const (
	LabelTenant = "tenant"
	LabelTopic  = "topic"
	LabelReason = "reason"
)

// Consumer type identifiers used as Prometheus label values and log field values.
const (
	ConsumerTypeKindShared    = "shared"
	ConsumerTypeKindDedicated = "dedicated"
)

// Structured log field keys used across kafka and orchestration packages.
const (
	LogFieldPartition = "partition" // pre-existing call sites in consumer.go and producer.go
	LogFieldTenantID  = "tenant_id"
)

// Log message constants for broker-deleted-topic and fetch error events.
const (
	MsgTopicDeletedAtBroker = "topic deleted at broker — pausing fetches"
	MsgFetchError           = "kafka fetch error"
)

// Prometheus metric and label names for the broker-deleted-topic counter.
const (
	MetricConsumerTopicDeletedTotal = "ws_consumer_topic_deleted_total"
	LabelConsumerType               = "consumer_type"
)
