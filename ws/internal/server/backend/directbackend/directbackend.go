// Package directbackend provides the zero-dependency implementation of the
// MessageBackend interface. It routes client-published messages directly to
// the broadcast bus with no persistence and no replay capability.
package directbackend

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/server/backend"
	"github.com/klurvio/sukko/internal/server/broadcast"
	"github.com/klurvio/sukko/internal/server/metrics"
)

// backendName identifies this backend in metrics labels and logging.
const backendName = "direct"

// DirectBackend routes client-published messages directly to the broadcast bus
// with zero external dependencies. No persistence, no replay.
type DirectBackend struct {
	bus    broadcast.Bus
	logger zerolog.Logger
}

// New creates a new direct backend with the given broadcast bus.
// Returns an error if bus is nil.
func New(bus broadcast.Bus, logger zerolog.Logger) (*DirectBackend, error) {
	if bus == nil {
		return nil, errors.New("direct backend: broadcast bus is required")
	}

	return &DirectBackend{
		bus:    bus,
		logger: logger.With().Str("component", "direct-backend").Logger(),
	}, nil
}

// Start is a no-op for direct mode (no consumption loop needed).
func (db *DirectBackend) Start(_ context.Context) error {
	db.logger.Info().Msg("Direct backend started (no external dependencies)")
	metrics.SetBackendHealthy(backendName, true)
	return nil
}

// Publish sends a message directly to the broadcast bus.
func (db *DirectBackend) Publish(_ context.Context, _ int64, channel string, data []byte) error {
	if channel == "" {
		return fmt.Errorf("%w: channel is required", backend.ErrPublishFailed)
	}
	start := time.Now()
	db.bus.Publish(&broadcast.Message{
		Subject: channel,
		Payload: data,
	})
	metrics.RecordBackendPublishLatency(backendName, time.Since(start).Seconds())
	metrics.RecordBackendPublish(backendName)
	return nil
}

// Replay returns nil, nil — direct mode has no persistence and cannot replay.
func (db *DirectBackend) Replay(_ context.Context, _ backend.ReplayRequest) ([]backend.ReplayMessage, error) {
	metrics.RecordBackendReplayRequest(backendName)
	return nil, nil
}

// IsHealthy always returns true — direct mode has no external dependencies.
func (db *DirectBackend) IsHealthy() bool {
	return true
}

// Shutdown is a no-op for direct mode.
func (db *DirectBackend) Shutdown(_ context.Context) error {
	metrics.SetBackendHealthy(backendName, false)
	db.logger.Info().Msg("Direct backend shut down")
	return nil
}

// Compile-time interface check.
var _ backend.MessageBackend = (*DirectBackend)(nil)
