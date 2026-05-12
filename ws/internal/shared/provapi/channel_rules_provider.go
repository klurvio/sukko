package provapi

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/routing"
	"github.com/klurvio/sukko/internal/shared/types"
)

// 20s ping + 10s timeout = 30s worst-case detection, matching NFR-002's 30-second propagation budget.
// Using 30s would allow 40s detection, violating NFR-002.
const (
	provAPIKeepaliveTime    = 20 * time.Second
	provAPIKeepaliveTimeout = 10 * time.Second
)

// TenantRoutingSnapshot is a point-in-time snapshot of routing rules and edition for a tenant.
type TenantRoutingSnapshot struct {
	Rules   []types.RoutingRule
	Edition license.Edition
}

// StreamChannelRulesProviderConfig configures the gRPC stream-backed channel rules provider.
type StreamChannelRulesProviderConfig struct {
	GRPCAddr          string
	ReconnectDelay    time.Duration
	ReconnectMaxDelay time.Duration
	MetricPrefix      string // "gateway" or "ws"
	Logger            zerolog.Logger
}

// StreamChannelRulesProvider provides per-tenant channel rules backed by a gRPC
// streaming connection to the provisioning service. Caches rules in memory.
type StreamChannelRulesProvider struct {
	mu           sync.RWMutex
	channelRules map[string]*types.ChannelRules // tenantID → rules

	snapshotMu       sync.Mutex   // serializes concurrent UpdateEdition + updateTenantConfigs COW writes
	routingSnapshots atomic.Value // holds map[string]TenantRoutingSnapshot

	conn   *grpc.ClientConn
	config StreamChannelRulesProviderConfig
	logger zerolog.Logger

	streamState atomic.Int32
	reconnects  atomic.Int64

	cancel context.CancelFunc
	wg     sync.WaitGroup

	streamStateGauge       prometheus.Gauge
	reconnectsCounter      prometheus.Counter
	invalidPatternsCounter prometheus.Counter
}

// NewStreamChannelRulesProvider creates a new gRPC stream-backed channel rules provider.
func NewStreamChannelRulesProvider(cfg StreamChannelRulesProviderConfig) (*StreamChannelRulesProvider, error) {
	if cfg.GRPCAddr == "" {
		return nil, errors.New("stream channel rules provider: GRPCAddr is required")
	}
	if cfg.ReconnectDelay <= 0 {
		return nil, errors.New("stream channel rules provider: ReconnectDelay must be > 0")
	}
	if cfg.ReconnectMaxDelay < cfg.ReconnectDelay {
		return nil, errors.New("stream channel rules provider: ReconnectMaxDelay must be >= ReconnectDelay")
	}
	if cfg.MetricPrefix == "" {
		return nil, errors.New("stream channel rules provider: MetricPrefix is required")
	}

	conn, err := grpc.NewClient(cfg.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                provAPIKeepaliveTime,
			Timeout:             provAPIKeepaliveTimeout,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial provisioning gRPC: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	r := &StreamChannelRulesProvider{
		channelRules: make(map[string]*types.ChannelRules),
		conn:         conn,
		config:       cfg,
		logger:       cfg.Logger.With().Str("component", "stream_channel_rules_provider").Logger(),
		cancel:       cancel,
		streamStateGauge: promauto.NewGauge(prometheus.GaugeOpts{
			Name: cfg.MetricPrefix + "_provisioning_config_stream_state",
			Help: "State of the provisioning config gRPC stream (0=disconnected, 1=connected)",
		}),
		reconnectsCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: cfg.MetricPrefix + "_provisioning_config_stream_reconnects_total",
			Help: "Total reconnection attempts for the provisioning config stream",
		}),
		invalidPatternsCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: cfg.MetricPrefix + "_" + routing.MetricInvalidPatternBase,
			Help: "Total routing patterns skipped due to invalid syntax",
		}),
	}
	r.routingSnapshots.Store(make(map[string]TenantRoutingSnapshot))

	r.wg.Go(func() {
		defer logging.RecoverPanic(r.logger, "channel_rules_stream", nil)
		r.streamLoop(ctx)
	})

	return r, nil
}

// GetChannelRules returns the channel rules for a tenant.
func (r *StreamChannelRulesProvider) GetChannelRules(_ context.Context, tenantID string) (*types.ChannelRules, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules, ok := r.channelRules[tenantID]
	if !ok {
		return nil, types.ErrChannelRulesNotFound
	}
	return rules, nil
}

// GetRoutingSnapshot returns the routing snapshot for a tenant, or false if not found.
func (r *StreamChannelRulesProvider) GetRoutingSnapshot(tenantID string) (TenantRoutingSnapshot, bool) {
	m, _ := r.routingSnapshots.Load().(map[string]TenantRoutingSnapshot)
	snap, ok := m[tenantID]
	return snap, ok
}

// UpdateEdition propagates a new edition to all cached routing snapshots.
// Called from the license watcher's OnReload callback.
func (r *StreamChannelRulesProvider) UpdateEdition(edition license.Edition) {
	r.snapshotMu.Lock()
	defer r.snapshotMu.Unlock()
	old, _ := r.routingSnapshots.Load().(map[string]TenantRoutingSnapshot)
	next := make(map[string]TenantRoutingSnapshot, len(old))
	for k, v := range old {
		next[k] = TenantRoutingSnapshot{Rules: v.Rules, Edition: edition}
	}
	r.routingSnapshots.Store(next)
}

// State returns the current stream state (0=disconnected, 1=connected).
func (r *StreamChannelRulesProvider) State() int32 {
	return r.streamState.Load()
}

// Close stops the stream and releases resources.
func (r *StreamChannelRulesProvider) Close() error {
	r.cancel()
	r.wg.Wait()
	if err := r.conn.Close(); err != nil {
		return fmt.Errorf("close channel rules provider gRPC connection: %w", err)
	}
	return nil
}

// streamLoop runs the gRPC stream with reconnection logic.
func (r *StreamChannelRulesProvider) streamLoop(ctx context.Context) {
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

		stream, err := client.WatchTenantConfig(ctx, &provisioningv1.WatchTenantConfigRequest{})
		if err != nil {
			r.logger.Warn().Err(err).Dur("retry_in", delay).Msg("failed to start WatchTenantConfig stream")
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

		r.logger.Info().Msg("WatchTenantConfig stream connected")

		for {
			resp, err := stream.Recv()
			if err != nil {
				r.logger.Warn().Err(err).Msg("WatchTenantConfig stream disconnected")
				r.streamState.Store(StreamStateDisconnected)
				r.streamStateGauge.Set(StreamStateDisconnected)
				break
			}

			r.updateTenantConfigs(resp)
		}
	}
}

// updateTenantConfigs processes a WatchTenantConfigResponse and updates caches.
func (r *StreamChannelRulesProvider) updateTenantConfigs(resp *provisioningv1.WatchTenantConfigResponse) {
	isSnapshot := resp.GetIsSnapshot()
	removedIDs := resp.GetRemovedTenantIds()
	tenants := resp.GetTenants()

	// Update channel rules under the mutex.
	r.mu.Lock()
	if isSnapshot {
		r.channelRules = make(map[string]*types.ChannelRules)
	}
	for _, tenantID := range removedIDs {
		delete(r.channelRules, tenantID)
	}
	for _, tc := range tenants {
		if tc.GetChannelRules() != nil {
			r.channelRules[tc.GetTenantId()] = protoToChannelRules(tc.GetChannelRules())
		}
	}
	channelRulesLen := len(r.channelRules)
	r.mu.Unlock()

	// COW update for routing snapshots — snapshotMu serializes concurrent UpdateEdition calls.
	r.snapshotMu.Lock()
	defer r.snapshotMu.Unlock()
	old, _ := r.routingSnapshots.Load().(map[string]TenantRoutingSnapshot)
	next := make(map[string]TenantRoutingSnapshot, len(old))
	if !isSnapshot {
		maps.Copy(next, old)
	}
	for _, tenantID := range removedIDs {
		delete(next, tenantID)
	}
	for _, tc := range tenants {
		existing := next[tc.GetTenantId()]
		protoRules := tc.GetRoutingRules()
		rules := make([]types.RoutingRule, 0, len(protoRules))
		for _, pr := range protoRules {
			pattern := routing.NormalizePattern(pr.GetPattern())
			if _, err := routing.MatchRoutingPattern(pattern, "x"); err != nil {
				r.invalidPatternsCounter.Inc()
				continue
			}
			topics := pr.GetTopics()
			if len(topics) == 0 && pr.GetTopicSuffix() != "" {
				topics = []string{pr.GetTopicSuffix()}
			}
			if len(topics) == 0 {
				continue // skip misconfigured rules with no topic destinations
			}
			rules = append(rules, types.RoutingRule{
				Pattern:  pattern,
				Topics:   topics,
				Priority: int(pr.GetPriority()),
			})
		}
		next[tc.GetTenantId()] = TenantRoutingSnapshot{
			Rules:   rules,
			Edition: existing.Edition,
		}
	}
	r.routingSnapshots.Store(next)

	r.logger.Debug().
		Bool("snapshot", isSnapshot).
		Int("channel_rules", channelRulesLen).
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
