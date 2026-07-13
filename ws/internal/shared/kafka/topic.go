// Package kafka provides Kafka/Redpanda integration for the WebSocket server.
// This file contains the shared BuildTopicName function for constructing topic names.
package kafka

import (
	"fmt"
	"strings"
)

// RetentionMsConfigKey is the Kafka topic configuration key for retention in milliseconds.
// Used when creating or reconfiguring topics via the Kafka admin API.
// Defined here (not in each consumer) to prevent magic-string duplication (§I).
const RetentionMsConfigKey = "retention.ms"

// BuildTopicName constructs a Kafka topic name from components.
// This is the single source of truth for topic name format across the codebase.
//
// Format: {namespace}.{tenantID}.{category}
//
// Example:
//
//	BuildTopicName("prod", "sukko", "trade") -> "prod.sukko.trade"
//	BuildTopicName("dev", "acme", "analytics") -> "dev.acme.analytics"
//
// Components:
//   - namespace: the explicit KAFKA_TOPIC_NAMESPACE value (e.g., "local", "dev", "stag", "prod")
//   - tenantID: Tenant identifier (e.g., "sukko", "acme")
//   - category: Topic category (e.g., "trade", "liquidity", "metadata")
//
// This function is used by:
//   - Producer (producer.go): Building topic names when publishing messages
//   - TenantRegistry (topic_registry.go): Building topic names when querying categories
//   - Provisioning Service (service.go): Building topic names when creating Kafka topics
//
// The namespace is NOT stored in the database - only the category is stored.
// This allows the same database to be used across environments, with the
// namespace determined at runtime from configuration.
func BuildTopicName(namespace, tenantID, category string) string {
	return fmt.Sprintf("%s.%s.%s", namespace, tenantID, category)
}

// ExtractTenantID reverses BuildTopicName, extracting tenantID and channel from a topic name.
// Returns an error if the topic does not start with the namespace prefix or is malformed.
//
// Example:
//
//	ExtractTenantID("prod.acme.BTC-USD", "prod") -> ("acme", "BTC-USD", nil)
//	ExtractTenantID("dev.tenant.multi.dot.channel", "dev") -> ("tenant", "multi.dot.channel", nil)
func ExtractTenantID(topicName, namespace string) (tenantID, channel string, err error) {
	prefix := namespace + "."
	if !strings.HasPrefix(topicName, prefix) {
		return "", "", fmt.Errorf("kafka: topic %q does not start with namespace prefix %q", topicName, prefix)
	}
	rest := topicName[len(prefix):]
	dot := strings.Index(rest, ".")
	if dot < 0 || dot == 0 || dot == len(rest)-1 {
		return "", "", fmt.Errorf("kafka: topic %q has no tenantID.channel segment after namespace prefix", topicName)
	}
	return rest[:dot], rest[dot+1:], nil
}
