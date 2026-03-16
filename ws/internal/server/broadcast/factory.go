package broadcast

import (
	"fmt"

	"github.com/rs/zerolog"
)

// NewBus creates a new Bus based on the configuration type.
// Returns an error if the configuration is invalid or connection fails.
func NewBus(cfg Config, logger zerolog.Logger) (Bus, error) {
	switch cfg.Type {
	case "valkey", "redis":
		return newValkeyBus(cfg, logger)
	case "nats":
		return newNATSBus(cfg, logger)
	default:
		return nil, fmt.Errorf("unsupported broadcast bus type: %q (supported: valkey, nats)", cfg.Type)
	}
}
