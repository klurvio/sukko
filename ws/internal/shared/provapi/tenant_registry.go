package provapi

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	provisioningv1 "github.com/Toniq-Labs/odin-ws/gen/proto/odin/provisioning/v1"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// StreamTenantRegistryConfig configures the gRPC stream-backed tenant registry.
type StreamTenantRegistryConfig struct {
	GRPCAddr          string
	ReconnectDelay    time.Duration
	ReconnectMaxDelay time.Duration
	MetricPrefix      string // "gateway" or "ws"
	Logger            zerolog.Logger
}

// StreamTenantRegistry implements gateway.TenantRegistry backed by a gRPC
// streaming connection to the provisioning service. It caches tenant OIDC
// configurations and channel rules.
type StreamTenantRegistry struct {
	mu             sync.RWMutex
	issuerToTenant map[string]string                  // issuerURL → tenantID
	oidcConfigs    map[string]*types.TenantOIDCConfig // tenantID → config
	channelRules   map[string]*types.ChannelRules     // tenantID → rules

	conn   *grpc.ClientConn
	config StreamTenantRegistryConfig
	logger zerolog.Logger

	streamState atomic.Int32
	reconnects  atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup

	streamStateGauge  prometheus.Gauge
	reconnectsCounter prometheus.Counter
}

// NewStreamTenantRegistry creates a new gRPC stream-backed tenant registry.
func NewStreamTenantRegistry(cfg StreamTenantRegistryConfig) (*StreamTenantRegistry, error) {
	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial provisioning gRPC: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &StreamTenantRegistry{
		issuerToTenant: make(map[string]string),
		oidcConfigs:    make(map[string]*types.TenantOIDCConfig),
		channelRules:   make(map[string]*types.ChannelRules),
		conn:           conn,
		config:         cfg,
		logger:         cfg.Logger.With().Str("component", "stream_tenant_registry").Logger(),
		cancel:         cancel,
		streamStateGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: cfg.MetricPrefix + "_provisioning_config_stream_state",
			Help: "State of the provisioning config gRPC stream (0=disconnected, 1=connected)",
		}),
		reconnectsCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: cfg.MetricPrefix + "_provisioning_config_stream_reconnects_total",
			Help: "Total reconnection attempts for the provisioning config stream",
		}),
	}

	r.wg.Add(1)
	go func() {
		defer logging.RecoverPanic(r.logger, "tenant_registry_stream", nil)
		defer r.wg.Done()

		r.streamLoop(ctx)
	}()

	return r, nil
}

// GetTenantByIssuer returns the tenant ID for an OIDC issuer.
func (r *StreamTenantRegistry) GetTenantByIssuer(_ context.Context, issuerURL string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tenantID, ok := r.issuerToTenant[issuerURL]
	if !ok {
		return "", types.ErrIssuerNotFound
	}
	return tenantID, nil
}

// GetOIDCConfig returns the OIDC configuration for a tenant.
func (r *StreamTenantRegistry) GetOIDCConfig(_ context.Context, tenantID string) (*types.TenantOIDCConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg, ok := r.oidcConfigs[tenantID]
	if !ok {
		return nil, types.ErrOIDCNotConfigured
	}
	return cfg, nil
}

// GetChannelRules returns the channel rules for a tenant.
func (r *StreamTenantRegistry) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules, ok := r.channelRules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return rules, nil
}

// State returns the current stream state (0=disconnected, 1=connected).
func (r *StreamTenantRegistry) State() int32 {
	return r.streamState.Load()
}

// Close stops the stream and releases resources.
func (r *StreamTenantRegistry) Close() error {
	r.cancel()
	r.wg.Wait()
	if err := r.conn.Close(); err != nil {
		return fmt.Errorf("close tenant registry gRPC connection: %w", err)
	}
	return nil
}

// streamLoop runs the gRPC stream with reconnection logic.
func (r *StreamTenantRegistry) streamLoop(ctx context.Context) {
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

		stream, err := client.WatchTenantConfig(ctx, &provisioningv1.WatchTenantConfigRequest{})
		if err != nil {
			r.logger.Warn().Err(err).Dur("retry_in", delay).Msg("failed to start WatchTenantConfig stream")
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
		delay = r.config.ReconnectDelay

		r.logger.Info().Msg("WatchTenantConfig stream connected")

		for {
			resp, err := stream.Recv()
			if err != nil {
				r.logger.Warn().Err(err).Msg("WatchTenantConfig stream disconnected")
				r.streamState.Store(streamStateDisconnected)
				r.streamStateGauge.Set(streamStateDisconnected)
				break
			}

			r.updateTenantConfigs(resp)
		}
	}
}

// updateTenantConfigs processes a WatchTenantConfigResponse and updates caches.
func (r *StreamTenantRegistry) updateTenantConfigs(resp *provisioningv1.WatchTenantConfigResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if resp.GetIsSnapshot() {
		r.issuerToTenant = make(map[string]string)
		r.oidcConfigs = make(map[string]*types.TenantOIDCConfig)
		r.channelRules = make(map[string]*types.ChannelRules)
	}

	// Remove tenants
	for _, tenantID := range resp.GetRemovedTenantIds() {
		// Remove OIDC config and issuer mapping
		if cfg, ok := r.oidcConfigs[tenantID]; ok {
			delete(r.issuerToTenant, cfg.IssuerURL)
			delete(r.oidcConfigs, tenantID)
		}
		delete(r.channelRules, tenantID)
	}

	// Add/update tenants
	for _, tc := range resp.GetTenants() {
		// OIDC config
		if tc.GetOidc() != nil && tc.GetOidc().GetEnabled() {
			oidcCfg := &types.TenantOIDCConfig{
				TenantID:  tc.GetTenantId(),
				IssuerURL: tc.GetOidc().GetIssuerUrl(),
				JWKSURL:   tc.GetOidc().GetJwksUrl(),
				Audience:  tc.GetOidc().GetAudience(),
				Enabled:   tc.GetOidc().GetEnabled(),
			}

			// Remove old issuer mapping if updating
			if old, ok := r.oidcConfigs[tc.GetTenantId()]; ok {
				delete(r.issuerToTenant, old.IssuerURL)
			}

			r.oidcConfigs[tc.GetTenantId()] = oidcCfg
			r.issuerToTenant[tc.GetOidc().GetIssuerUrl()] = tc.GetTenantId()
		} else {
			// OIDC disabled or nil — remove
			if old, ok := r.oidcConfigs[tc.GetTenantId()]; ok {
				delete(r.issuerToTenant, old.IssuerURL)
				delete(r.oidcConfigs, tc.GetTenantId())
			}
		}

		// Channel rules
		if tc.GetChannelRules() != nil {
			rules := protoToChannelRules(tc.GetChannelRules())
			r.channelRules[tc.GetTenantId()] = rules
		}
	}

	r.logger.Debug().
		Bool("snapshot", resp.GetIsSnapshot()).
		Int("oidc_configs", len(r.oidcConfigs)).
		Int("channel_rules", len(r.channelRules)).
		Msg("tenant config cache updated")
}

// protoToChannelRules converts proto ChannelRules to types.ChannelRules.
func protoToChannelRules(cr *provisioningv1.ChannelRules) *types.ChannelRules {
	rules := &types.ChannelRules{
		Public:         cr.GetPublicChannels(),
		Default:        cr.GetDefaultChannels(),
		PublishPublic:  cr.GetPublishPublicChannels(),
		PublishDefault: cr.GetPublishDefaultChannels(),
	}

	if len(cr.GetGroupMappings()) > 0 {
		rules.GroupMappings = make(map[string][]string, len(cr.GetGroupMappings()))
		for group, gc := range cr.GetGroupMappings() {
			rules.GroupMappings[group] = gc.GetChannels()
		}
	}

	if len(cr.GetPublishGroupMappings()) > 0 {
		rules.PublishGroupMappings = make(map[string][]string, len(cr.GetPublishGroupMappings()))
		for group, gc := range cr.GetPublishGroupMappings() {
			rules.PublishGroupMappings[group] = gc.GetChannels()
		}
	}

	return rules
}
