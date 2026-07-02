package publisher

import (
	"context"
	"errors"
	"fmt"

	kafkashared "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/platform"
)

// ErrUnsupportedPublisherMode is returned when Config.Mode is not a recognized value.
var ErrUnsupportedPublisherMode = errors.New("unsupported publisher mode")

// Config holds parameters for creating a Publisher via the factory.
type Config struct {
	Mode         string        // "direct" or "kafka"
	GatewayURL   string        // required for direct mode
	Token        string        // JWT token for direct mode
	BackendURLs  string        // broker URLs for kafka mode
	Namespace    string        // Kafka topic namespace (from ENVIRONMENT)
	TenantID     string        // tenant ID for topic resolution
	RoutingRules []RoutingRule // for Kafka topic resolution
	SASL         *kafkashared.SASLConfig
	TLS          *kafkashared.TLSConfig
}

// NewPublisher creates a Publisher based on the configured mode.
func NewPublisher(ctx context.Context, cfg Config) (Publisher, error) {
	switch cfg.Mode {
	case platform.MessageBackendDirect, "":
		return NewDirectPublisher(ctx, cfg.GatewayURL, cfg.Token)
	case platform.MessageBackendKafka:
		if cfg.BackendURLs == "" {
			return nil, errors.New("kafka publisher: KAFKA_BROKERS / message_backend_urls required")
		}
		resolver := NewTopicResolver(cfg.Namespace, cfg.TenantID, cfg.RoutingRules)
		return NewKafkaPublisher(cfg.BackendURLs, resolver, cfg.SASL, cfg.TLS)
	default:
		return nil, fmt.Errorf("unsupported publisher mode %q: %w", cfg.Mode, ErrUnsupportedPublisherMode)
	}
}
