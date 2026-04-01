package publisher

import (
	"context"
	"errors"
	"fmt"
)

// Config holds parameters for creating a Publisher via the factory.
type Config struct {
	Mode         string        // "direct", "kafka", "nats"
	GatewayURL   string        // required for direct mode
	Token        string        // JWT token for direct mode
	BackendURLs  string        // broker URLs for kafka, JetStream URLs for nats
	Namespace    string        // Kafka topic namespace (from ENVIRONMENT)
	TenantID     string        // tenant ID for topic resolution
	RoutingRules []RoutingRule // for Kafka topic resolution
}

// NewPublisher creates a Publisher based on the configured mode.
func NewPublisher(ctx context.Context, cfg Config) (Publisher, error) {
	switch cfg.Mode {
	case "direct", "":
		return NewDirectPublisher(ctx, cfg.GatewayURL, cfg.Token)
	case "kafka":
		if cfg.BackendURLs == "" {
			return nil, errors.New("kafka publisher: KAFKA_BROKERS / message_backend_urls required")
		}
		resolver := NewTopicResolver(cfg.Namespace, cfg.TenantID, cfg.RoutingRules)
		return NewKafkaPublisher(cfg.BackendURLs, resolver)
	case "nats":
		if cfg.BackendURLs == "" {
			return nil, errors.New("nats publisher: NATS_JETSTREAM_URLS / message_backend_urls required")
		}
		return NewNATSPublisher(cfg.BackendURLs)
	default:
		return nil, fmt.Errorf("unsupported publisher mode: %q", cfg.Mode)
	}
}
