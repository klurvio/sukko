// Package kafka provides Kafka/Redpanda integration for the WebSocket server.
// This file contains the shared BuildTopicName function for constructing topic names.
package kafka

import "fmt"

// BuildTopicName constructs a Kafka topic name from components.
// This is the single source of truth for topic name format across the codebase.
//
// Format: {namespace}.{tenantID}.{category}
//
// Example:
//
//	BuildTopicName("prod", "odin", "trade") -> "prod.odin.trade"
//	BuildTopicName("dev", "acme", "analytics") -> "dev.acme.analytics"
//
// Components:
//   - namespace: From KAFKA_TOPIC_NAMESPACE env var (e.g., "local", "dev", "stag", "prod")
//   - tenantID: Tenant identifier (e.g., "odin", "acme")
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
