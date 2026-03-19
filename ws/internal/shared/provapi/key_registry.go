// Package provapi provides gRPC stream-backed registries that consume
// provisioning data from the provisioning service's gRPC streaming API.
// Used by both gateway and ws-server to get real-time provisioning updates.
package provapi

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// Stream state constants for the gRPC streaming connection gauge.
const (
	streamStateDisconnected = 0
	streamStateConnected    = 1
)

// Jitter constants for exponential backoff reconnection.
const (
	jitterBase  = 0.75 // Minimum jitter multiplier (75% of delay)
	jitterRange = 0.25 // Additional random jitter range (up to 25%)
)

// StreamKeyRegistryConfig configures the gRPC stream-backed key registry.
type StreamKeyRegistryConfig struct {
	GRPCAddr          string
	ReconnectDelay    time.Duration
	ReconnectMaxDelay time.Duration
	MetricPrefix      string // "gateway" or "ws"
	Logger            zerolog.Logger
}

// StreamKeyRegistry implements auth.KeyRegistry and auth.KeyRegistryWithRefresh
// backed by a gRPC streaming connection to the provisioning service.
type StreamKeyRegistry struct {
	mu           sync.RWMutex
	keysByID     map[string]*auth.KeyInfo
	keysByTenant map[string][]*auth.KeyInfo

	conn   *grpc.ClientConn
	config StreamKeyRegistryConfig
	logger zerolog.Logger

	streamState atomic.Int32
	reconnects  atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Prometheus metrics (registered with prefix)
	streamStateGauge  prometheus.Gauge
	reconnectsCounter prometheus.Counter
}

// NewStreamKeyRegistry creates a new gRPC stream-backed key registry.
// It dials the gRPC server and starts a background goroutine to receive updates.
func NewStreamKeyRegistry(cfg StreamKeyRegistryConfig) (*StreamKeyRegistry, error) {
	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial provisioning gRPC: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &StreamKeyRegistry{
		keysByID:     make(map[string]*auth.KeyInfo),
		keysByTenant: make(map[string][]*auth.KeyInfo),
		conn:         conn,
		config:       cfg,
		logger:       cfg.Logger.With().Str("component", "stream_key_registry").Logger(),
		cancel:       cancel,
		streamStateGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: cfg.MetricPrefix + "_provisioning_keys_stream_state",
			Help: "State of the provisioning keys gRPC stream (0=disconnected, 1=connected)",
		}),
		reconnectsCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: cfg.MetricPrefix + "_provisioning_keys_stream_reconnects_total",
			Help: "Total reconnection attempts for the provisioning keys stream",
		}),
	}

	r.wg.Add(1)
	go func() {
		defer logging.RecoverPanic(r.logger, "key_registry_stream", nil)
		defer r.wg.Done()

		r.streamLoop(ctx)
	}()

	return r, nil
}

// GetKey retrieves a key by its ID.
func (r *StreamKeyRegistry) GetKey(_ context.Context, keyID string) (*auth.KeyInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key, ok := r.keysByID[keyID]
	if !ok {
		return nil, auth.ErrKeyNotFound
	}

	if key.RevokedAt != nil {
		return nil, auth.ErrKeyRevoked
	}
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		return nil, auth.ErrKeyExpired
	}

	return key, nil
}

// GetKeysByTenant retrieves all active keys for a tenant.
func (r *StreamKeyRegistry) GetKeysByTenant(_ context.Context, tenantID string) ([]*auth.KeyInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := r.keysByTenant[tenantID]
	if len(keys) == 0 {
		return nil, nil
	}

	// Filter to active, non-expired keys
	var active []*auth.KeyInfo
	now := time.Now()
	for _, k := range keys {
		if k.IsActive && k.RevokedAt == nil && (k.ExpiresAt == nil || k.ExpiresAt.After(now)) {
			active = append(active, k)
		}
	}

	return active, nil
}

// Refresh forces a refresh by reconnecting the stream.
func (r *StreamKeyRegistry) Refresh(_ context.Context) error {
	// The stream will naturally send the latest data on reconnect
	return nil
}

// Stats returns cache statistics.
func (r *StreamKeyRegistry) Stats() auth.KeyRegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active int
	now := time.Now()
	for _, k := range r.keysByID {
		if k.IsActive && k.RevokedAt == nil && (k.ExpiresAt == nil || k.ExpiresAt.After(now)) {
			active++
		}
	}

	return auth.KeyRegistryStats{
		TotalKeys:  len(r.keysByID),
		ActiveKeys: active,
	}
}

// State returns the current stream state (0=disconnected, 1=connected).
func (r *StreamKeyRegistry) State() int32 {
	return r.streamState.Load()
}

// Close stops the stream and releases resources.
func (r *StreamKeyRegistry) Close() error {
	r.cancel()
	r.wg.Wait()
	if err := r.conn.Close(); err != nil {
		return fmt.Errorf("close key registry gRPC connection: %w", err)
	}
	return nil
}

// streamLoop runs the gRPC stream with reconnection logic.
func (r *StreamKeyRegistry) streamLoop(ctx context.Context) {
	client := provisioningv1.NewProvisioningInternalServiceClient(r.conn)
	delay := r.config.ReconnectDelay

	for {
		select {
		case <-ctx.Done():
			r.streamState.Store(streamStateDisconnected)
			r.streamStateGauge.Set(streamStateDisconnected)
			return
		default:
		}

		stream, err := client.WatchKeys(ctx, &provisioningv1.WatchKeysRequest{})
		if err != nil {
			r.logger.Warn().Err(err).Dur("retry_in", delay).Msg("failed to start WatchKeys stream")
			r.streamState.Store(streamStateDisconnected)
			r.streamStateGauge.Set(streamStateDisconnected)

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

		r.streamState.Store(streamStateConnected)
		r.streamStateGauge.Set(streamStateConnected)
		delay = r.config.ReconnectDelay // Reset on successful connect

		r.logger.Info().Msg("WatchKeys stream connected")

		// Receive loop
		for {
			resp, err := stream.Recv()
			if err != nil {
				r.logger.Warn().Err(err).Msg("WatchKeys stream disconnected")
				r.streamState.Store(streamStateDisconnected)
				r.streamStateGauge.Set(streamStateDisconnected)
				break
			}

			r.updateKeys(resp)
		}
	}
}

// updateKeys processes a WatchKeysResponse and updates the cache.
func (r *StreamKeyRegistry) updateKeys(resp *provisioningv1.WatchKeysResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if resp.GetIsSnapshot() {
		// Full replacement
		r.keysByID = make(map[string]*auth.KeyInfo, len(resp.GetKeys()))
		r.keysByTenant = make(map[string][]*auth.KeyInfo)
	}

	// Remove keys
	for _, keyID := range resp.GetRemovedKeyIds() {
		if existing, ok := r.keysByID[keyID]; ok {
			delete(r.keysByID, keyID)
			// Remove from tenant list
			tenantKeys := r.keysByTenant[existing.TenantID]
			for i, k := range tenantKeys {
				if k.KeyID == keyID {
					r.keysByTenant[existing.TenantID] = append(tenantKeys[:i], tenantKeys[i+1:]...)
					break
				}
			}
		}
	}

	// Add/update keys
	for _, ki := range resp.GetKeys() {
		keyInfo, err := protoToKeyInfo(ki)
		if err != nil {
			r.logger.Warn().Err(err).Str("key_id", ki.GetKeyId()).Msg("failed to parse key")
			continue
		}

		r.keysByID[keyInfo.KeyID] = keyInfo

		// Update tenant index
		if resp.GetIsSnapshot() {
			r.keysByTenant[keyInfo.TenantID] = append(r.keysByTenant[keyInfo.TenantID], keyInfo)
		} else {
			// For deltas, replace existing or append
			tenantKeys := r.keysByTenant[keyInfo.TenantID]
			found := false
			for i, k := range tenantKeys {
				if k.KeyID == keyInfo.KeyID {
					tenantKeys[i] = keyInfo
					found = true
					break
				}
			}
			if !found {
				r.keysByTenant[keyInfo.TenantID] = append(tenantKeys, keyInfo)
			}
		}
	}

	r.logger.Debug().
		Bool("snapshot", resp.GetIsSnapshot()).
		Int("keys", len(r.keysByID)).
		Msg("key cache updated")
}

// protoToKeyInfo converts a proto KeyInfo to auth.KeyInfo.
func protoToKeyInfo(ki *provisioningv1.KeyInfo) (*auth.KeyInfo, error) {
	pubKey, err := parsePEMPublicKey(ki.GetPublicKeyPem())
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	info := &auth.KeyInfo{
		KeyID:        ki.GetKeyId(),
		TenantID:     ki.GetTenantId(),
		Algorithm:    ki.GetAlgorithm(),
		PublicKey:    pubKey,
		PublicKeyPEM: ki.GetPublicKeyPem(),
		IsActive:     ki.GetIsActive(),
	}

	if ki.GetExpiresAtUnix() > 0 {
		t := time.Unix(ki.GetExpiresAtUnix(), 0)
		info.ExpiresAt = &t
	}

	return info, nil
}

// parsePEMPublicKey parses a PEM-encoded public key.
func parsePEMPublicKey(pemStr string) (crypto.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX public key: %w", err)
	}
	return key, nil
}

// backoff calculates the next backoff delay with jitter, capped at maxDelay.
func backoff(current, maxDelay time.Duration) time.Duration {
	next := min(time.Duration(float64(current)*2), maxDelay)
	// Add jitter: 75%-100% of calculated delay
	jitter := jitterBase + rand.Float64()*jitterRange //nolint:gosec // G404: jitter for backoff delay does not need cryptographic randomness
	return time.Duration(math.Round(float64(next) * jitter))
}

// Ensure StreamKeyRegistry implements the interfaces.
var (
	_ auth.KeyRegistry            = (*StreamKeyRegistry)(nil)
	_ auth.KeyRegistryWithRefresh = (*StreamKeyRegistry)(nil)
)
