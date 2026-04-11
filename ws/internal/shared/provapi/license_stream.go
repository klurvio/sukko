package provapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// StreamLicenseWatcherConfig configures the StreamLicenseWatcher.
type StreamLicenseWatcherConfig struct {
	GRPCAddr          string
	ReconnectDelay    time.Duration
	ReconnectMaxDelay time.Duration
	MetricPrefix      string // "gateway", "ws", or "push"
	Manager           *license.Manager
	Logger            zerolog.Logger
	OnReload          func() // Optional callback invoked after each successful Manager.Reload(). Runs in the watcher goroutine (RecoverPanic covers panics).
}

// StreamLicenseWatcher subscribes to the WatchLicense gRPC stream and calls
// Manager.Reload() on each update. Reconnects with exponential backoff.
type StreamLicenseWatcher struct {
	conn    *grpc.ClientConn
	config  StreamLicenseWatcherConfig
	logger  zerolog.Logger
	manager *license.Manager

	streamState atomic.Int32
	reconnects  atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup

	streamStateGauge  prometheus.Gauge
	reconnectsCounter prometheus.Counter
}

// NewStreamLicenseWatcher creates a watcher that subscribes to WatchLicense
// and reloads the Manager on each license key update.
func NewStreamLicenseWatcher(cfg StreamLicenseWatcherConfig) (*StreamLicenseWatcher, error) {
	if cfg.GRPCAddr == "" {
		return nil, errors.New("license stream watcher: GRPCAddr is required")
	}
	if cfg.Manager == nil {
		return nil, errors.New("license stream watcher: Manager is required")
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}
	if cfg.ReconnectMaxDelay < cfg.ReconnectDelay {
		cfg.ReconnectMaxDelay = 2 * time.Minute
	}
	if cfg.MetricPrefix == "" {
		cfg.MetricPrefix = "unknown"
	}

	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial provisioning gRPC: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &StreamLicenseWatcher{
		conn:    conn,
		config:  cfg,
		logger:  cfg.Logger.With().Str("component", "license_stream_watcher").Logger(),
		manager: cfg.Manager,
		cancel:  cancel,
		streamStateGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: cfg.MetricPrefix + "_provisioning_license_stream_state",
			Help: "State of the provisioning license gRPC stream (0=disconnected, 1=connected)",
		}),
		reconnectsCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: cfg.MetricPrefix + "_provisioning_license_stream_reconnects_total",
			Help: "Total reconnection attempts for the provisioning license stream",
		}),
	}

	w.wg.Go(func() {
		defer logging.RecoverPanic(w.logger, "license_stream_watcher", nil)
		w.streamLoop(ctx)
	})

	return w, nil
}

// Close stops the watcher and releases resources.
func (w *StreamLicenseWatcher) Close() error {
	w.cancel()
	w.wg.Wait()
	if err := w.conn.Close(); err != nil {
		return fmt.Errorf("close gRPC connection: %w", err)
	}
	return nil
}

// State returns the current stream state (0=disconnected, 1=connected).
func (w *StreamLicenseWatcher) State() int32 {
	return w.streamState.Load()
}

// streamLoop runs the gRPC stream with reconnection logic.
func (w *StreamLicenseWatcher) streamLoop(ctx context.Context) {
	client := provisioningv1.NewProvisioningInternalServiceClient(w.conn)
	delay := w.config.ReconnectDelay

	for {
		select {
		case <-ctx.Done():
			w.streamState.Store(StreamStateDisconnected)
			w.streamStateGauge.Set(StreamStateDisconnected)
			return
		default:
		}

		stream, err := client.WatchLicense(ctx, &provisioningv1.WatchLicenseRequest{})
		if err != nil {
			w.logger.Warn().Err(err).Dur("retry_in", delay).Msg("failed to start WatchLicense stream")
			w.streamState.Store(StreamStateDisconnected)
			w.streamStateGauge.Set(StreamStateDisconnected)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			delay = backoff(delay, w.config.ReconnectMaxDelay)
			w.reconnects.Add(1)
			w.reconnectsCounter.Inc()
			continue
		}

		w.streamState.Store(StreamStateConnected)
		w.streamStateGauge.Set(StreamStateConnected)
		delay = w.config.ReconnectDelay

		w.logger.Info().Msg("WatchLicense stream connected")

		// Receive loop
		for {
			resp, err := stream.Recv()
			if err != nil {
				w.logger.Warn().Err(err).Msg("WatchLicense stream disconnected")
				w.streamState.Store(StreamStateDisconnected)
				w.streamStateGauge.Set(StreamStateDisconnected)
				break
			}

			// Empty key = Community / no license — skip reload
			if resp.GetLicenseKey() == "" {
				w.logger.Debug().Msg("received empty license key (Community)")
				continue
			}

			if err := w.manager.Reload(resp.GetLicenseKey()); err != nil {
				w.logger.Warn().Err(err).Msg("license reload from stream failed")
				continue
			}

			w.logger.Info().
				Str("edition", w.manager.CurrentEdition().String()).
				Msg("license reloaded from stream")

			if w.config.OnReload != nil {
				w.config.OnReload()
			}
		}
	}
}
