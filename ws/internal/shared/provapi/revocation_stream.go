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
	"github.com/klurvio/sukko/internal/shared/logging"
)

// RevocationEntry represents a single revocation received from the stream.
type RevocationEntry struct {
	TenantID  string
	Type      string // "user" or "token"
	Sub       string
	JTI       string
	RevokedAt int64
	ExpiresAt int64
	Removed   bool
}

// jtiEntry is an entry in the JTI revocation map.
type jtiEntry struct {
	TenantID  string
	ExpiresAt int64
}

// subEntry is an entry in the sub revocation map.
type subEntry struct {
	RevokedAt int64
	ExpiresAt int64
}

// RevocationSnapshot holds the current revocation state. Immutable — replaced atomically.
type RevocationSnapshot struct {
	JTIRevocations map[string]*jtiEntry // jti → entry
	SubRevocations map[string]*subEntry // "tenant:sub" → entry
}

// StreamRevocationRegistryConfig configures the revocation stream watcher.
type StreamRevocationRegistryConfig struct {
	GRPCAddr          string
	ReconnectDelay    time.Duration
	ReconnectMaxDelay time.Duration
	MetricPrefix      string // "gateway" or "push"
	Logger            zerolog.Logger
	OnRevocation      func(entry RevocationEntry) // Callback for force-disconnect / deletion
}

// StreamRevocationRegistry subscribes to WatchTokenRevocations and caches
// revocations in an atomic.Value for O(1) lock-free reads.
type StreamRevocationRegistry struct {
	conn   *grpc.ClientConn
	config StreamRevocationRegistryConfig
	logger zerolog.Logger

	snapshot    atomic.Value // *RevocationSnapshot
	streamState atomic.Int32
	reconnects  atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup

	streamStateGauge  prometheus.Gauge
	reconnectsCounter prometheus.Counter
	mapEntriesGauge   prometheus.Gauge
}

// NewStreamRevocationRegistry creates a watcher that subscribes to WatchTokenRevocations.
func NewStreamRevocationRegistry(cfg StreamRevocationRegistryConfig) (*StreamRevocationRegistry, error) {
	if cfg.GRPCAddr == "" {
		return nil, errors.New("revocation stream registry: GRPCAddr is required")
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

	r := &StreamRevocationRegistry{
		conn:   conn,
		config: cfg,
		logger: cfg.Logger.With().Str("component", "revocation_stream_registry").Logger(),
		cancel: cancel,
		streamStateGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: cfg.MetricPrefix + "_token_revocation_stream_state",
			Help: "State of the token revocation gRPC stream (0=disconnected, 1=connected)",
		}),
		reconnectsCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: cfg.MetricPrefix + "_token_revocation_stream_reconnects_total",
			Help: "Total reconnection attempts for the token revocation stream",
		}),
		mapEntriesGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: cfg.MetricPrefix + "_token_revocation_map_entries",
			Help: "Current number of entries in the token revocation map",
		}),
	}

	// Initialize with empty snapshot
	r.snapshot.Store(&RevocationSnapshot{
		JTIRevocations: make(map[string]*jtiEntry),
		SubRevocations: make(map[string]*subEntry),
	})

	r.wg.Go(func() {
		defer logging.RecoverPanic(r.logger, "revocation_stream_registry", nil)
		r.streamLoop(ctx)
	})

	return r, nil
}

// Close stops the watcher and releases resources.
func (r *StreamRevocationRegistry) Close() error {
	r.cancel()
	r.wg.Wait()
	if err := r.conn.Close(); err != nil {
		return fmt.Errorf("close gRPC connection: %w", err)
	}
	return nil
}

// IsRevoked checks if a token is revoked. O(1) — two map lookups (jti + sub).
// Called from JWT validation hot path — zero locks.
func (r *StreamRevocationRegistry) IsRevoked(jti, sub, tenantID string, iat int64) bool {
	snap := r.snapshot.Load().(*RevocationSnapshot)

	// Check jti revocation
	if jti != "" {
		if entry, ok := snap.JTIRevocations[jti]; ok && entry.TenantID == tenantID {
			return true
		}
	}

	// Check sub revocation
	if sub != "" {
		key := tenantID + ":" + sub
		if entry, ok := snap.SubRevocations[key]; ok && iat < entry.RevokedAt {
			return true
		}
	}

	return false
}

// streamLoop runs the gRPC stream with reconnection logic.
func (r *StreamRevocationRegistry) streamLoop(ctx context.Context) {
	client := provisioningv1.NewProvisioningInternalServiceClient(r.conn)
	delay := r.config.ReconnectDelay

	for {
		select {
		case <-ctx.Done():
			r.streamState.Store(StreamStateDisconnected)
			r.streamStateGauge.Set(StreamStateDisconnected)
			return
		default:
		}

		stream, err := client.WatchTokenRevocations(ctx, &provisioningv1.WatchTokenRevocationsRequest{})
		if err != nil {
			r.logger.Warn().Err(err).Dur("retry_in", delay).Msg("failed to start WatchTokenRevocations stream")
			r.streamState.Store(StreamStateDisconnected)
			r.streamStateGauge.Set(StreamStateDisconnected)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			delay = backoff(delay, r.config.ReconnectMaxDelay)
			r.reconnects.Add(1)
			r.reconnectsCounter.Inc()
			continue
		}

		r.streamState.Store(StreamStateConnected)
		r.streamStateGauge.Set(StreamStateConnected)
		delay = r.config.ReconnectDelay

		r.logger.Info().Msg("WatchTokenRevocations stream connected")

		for {
			resp, err := stream.Recv()
			if err != nil {
				r.logger.Warn().Err(err).Msg("WatchTokenRevocations stream disconnected")
				r.streamState.Store(StreamStateDisconnected)
				r.streamStateGauge.Set(StreamStateDisconnected)
				break
			}

			if resp.GetIsSnapshot() {
				r.applySnapshot(resp.GetRevocations())
			} else {
				r.applyDelta(resp.GetRevocations())
			}
		}
	}
}

// applySnapshot replaces the entire revocation map with the snapshot data.
func (r *StreamRevocationRegistry) applySnapshot(revocations []*provisioningv1.TokenRevocation) {
	now := time.Now().Unix()
	jtiMap := make(map[string]*jtiEntry, len(revocations))
	subMap := make(map[string]*subEntry, len(revocations))

	for _, rev := range revocations {
		if rev.GetExpiresAt() <= now {
			continue // skip expired
		}
		switch rev.GetType() {
		case "token":
			jtiMap[rev.GetJti()] = &jtiEntry{
				TenantID:  rev.GetTenantId(),
				ExpiresAt: rev.GetExpiresAt(),
			}
		case "user":
			key := rev.GetTenantId() + ":" + rev.GetSub()
			subMap[key] = &subEntry{
				RevokedAt: rev.GetRevokedAt(),
				ExpiresAt: rev.GetExpiresAt(),
			}
		}
	}

	r.snapshot.Store(&RevocationSnapshot{
		JTIRevocations: jtiMap,
		SubRevocations: subMap,
	})

	total := len(jtiMap) + len(subMap)
	r.mapEntriesGauge.Set(float64(total))
	r.logger.Info().Int("jti_count", len(jtiMap)).Int("sub_count", len(subMap)).Msg("revocation snapshot applied")

	// Fire callbacks for all entries in the snapshot (initial load)
	if r.config.OnRevocation != nil {
		for _, rev := range revocations {
			if rev.GetExpiresAt() > now {
				r.config.OnRevocation(protoToRevocationEntry(rev))
			}
		}
	}
}

// applyDelta updates the revocation map with delta changes.
func (r *StreamRevocationRegistry) applyDelta(revocations []*provisioningv1.TokenRevocation) {
	now := time.Now().Unix()
	old := r.snapshot.Load().(*RevocationSnapshot)

	// Copy maps (copy-on-write)
	jtiMap := make(map[string]*jtiEntry, len(old.JTIRevocations))
	for k, v := range old.JTIRevocations {
		if v.ExpiresAt > now {
			jtiMap[k] = v
		}
	}
	subMap := make(map[string]*subEntry, len(old.SubRevocations))
	for k, v := range old.SubRevocations {
		if v.ExpiresAt > now {
			subMap[k] = v
		}
	}

	for _, rev := range revocations {
		switch rev.GetType() {
		case "token":
			if rev.GetRemoved() {
				delete(jtiMap, rev.GetJti())
			} else if rev.GetExpiresAt() > now {
				jtiMap[rev.GetJti()] = &jtiEntry{
					TenantID:  rev.GetTenantId(),
					ExpiresAt: rev.GetExpiresAt(),
				}
			}
		case "user":
			key := rev.GetTenantId() + ":" + rev.GetSub()
			if rev.GetRemoved() {
				delete(subMap, key)
			} else if rev.GetExpiresAt() > now {
				subMap[key] = &subEntry{
					RevokedAt: rev.GetRevokedAt(),
					ExpiresAt: rev.GetExpiresAt(),
				}
			}
		}
	}

	r.snapshot.Store(&RevocationSnapshot{
		JTIRevocations: jtiMap,
		SubRevocations: subMap,
	})

	total := len(jtiMap) + len(subMap)
	r.mapEntriesGauge.Set(float64(total))

	// Fire callbacks for new revocations (not removals)
	if r.config.OnRevocation != nil {
		for _, rev := range revocations {
			if !rev.GetRemoved() && rev.GetExpiresAt() > now {
				r.config.OnRevocation(protoToRevocationEntry(rev))
			}
		}
	}
}

func protoToRevocationEntry(rev *provisioningv1.TokenRevocation) RevocationEntry {
	return RevocationEntry{
		TenantID:  rev.GetTenantId(),
		Type:      rev.GetType(),
		Sub:       rev.GetSub(),
		JTI:       rev.GetJti(),
		RevokedAt: rev.GetRevokedAt(),
		ExpiresAt: rev.GetExpiresAt(),
		Removed:   rev.GetRemoved(),
	}
}
