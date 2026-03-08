package grpcserver

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/types"
)

// maxTenantsFetchLimit is the upper bound for listing all tenants in bulk
// operations (snapshot/delta loading). This prevents unbounded queries while
// being high enough to cover all tenants in practice.
const maxTenantsFetchLimit = 10000

// Server implements the ProvisioningInternalServiceServer gRPC interface.
// It streams provisioning data (keys, tenant config, topics) to gateway and ws-server
// using an event bus for real-time change notifications.
type Server struct {
	provisioningv1.UnimplementedProvisioningInternalServiceServer

	service  *provisioning.Service
	eventBus *eventbus.Bus
	logger   zerolog.Logger
}

// NewServer creates a new gRPC stream server.
func NewServer(service *provisioning.Service, eventBus *eventbus.Bus, logger zerolog.Logger) *Server {
	return &Server{
		service:  service,
		eventBus: eventBus,
		logger:   logger.With().Str("component", "grpc_server").Logger(),
	}
}

// WatchKeys streams active keys to the caller. Sends a snapshot on connect,
// then streams deltas when keys change via event bus.
func (s *Server) WatchKeys(_ *provisioningv1.WatchKeysRequest, stream grpc.ServerStreamingServer[provisioningv1.WatchKeysResponse]) error {
	ctx := stream.Context()
	logger := s.logger.With().Str("rpc", "WatchKeys").Logger()

	// Load and send initial snapshot
	keys, err := s.service.GetActiveKeys(ctx)
	if err != nil {
		return status.Errorf(codes.Internal, "load active keys: %v", err)
	}

	snapshot := &provisioningv1.WatchKeysResponse{
		IsSnapshot: true,
		Keys:       convertKeys(keys),
	}
	if err := stream.Send(snapshot); err != nil {
		return status.Errorf(codes.Unavailable, "send keys snapshot: %v", err)
	}

	logger.Info().Int("key_count", len(keys)).Msg("sent keys snapshot")

	// Subscribe to event bus for changes
	subID, events := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(subID)

	// Stream deltas on change
	for {
		select {
		case <-ctx.Done():
			logger.Debug().Msg("stream context cancelled")
			return nil

		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.Type != eventbus.KeysChanged {
				continue
			}

			// Reload all active keys and send as delta
			updatedKeys, err := s.service.GetActiveKeys(ctx)
			if err != nil {
				logger.Error().Err(err).Msg("failed to reload keys for delta")
				continue
			}

			delta := &provisioningv1.WatchKeysResponse{
				IsSnapshot: false,
				Keys:       convertKeys(updatedKeys),
			}
			if err := stream.Send(delta); err != nil {
				return status.Errorf(codes.Unavailable, "send keys delta: %v", err)
			}

			logger.Debug().Int("key_count", len(updatedKeys)).Msg("sent keys delta")
		}
	}
}

// WatchTenantConfig streams tenant configuration (OIDC, channel rules) to the caller.
func (s *Server) WatchTenantConfig(_ *provisioningv1.WatchTenantConfigRequest, stream grpc.ServerStreamingServer[provisioningv1.WatchTenantConfigResponse]) error {
	ctx := stream.Context()
	logger := s.logger.With().Str("rpc", "WatchTenantConfig").Logger()

	// Load and send initial snapshot
	tenantConfigs, err := s.loadTenantConfigs(ctx)
	if err != nil {
		return status.Errorf(codes.Internal, "load tenant configs: %v", err)
	}

	snapshot := &provisioningv1.WatchTenantConfigResponse{
		IsSnapshot: true,
		Tenants:    tenantConfigs,
	}
	if err := stream.Send(snapshot); err != nil {
		return status.Errorf(codes.Unavailable, "send tenant config snapshot: %v", err)
	}

	logger.Info().Int("tenant_count", len(tenantConfigs)).Msg("sent tenant config snapshot")

	// Subscribe to event bus for changes
	subID, events := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(subID)

	for {
		select {
		case <-ctx.Done():
			logger.Debug().Msg("stream context cancelled")
			return nil

		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.Type != eventbus.TenantConfigChanged {
				continue
			}

			updatedConfigs, err := s.loadTenantConfigs(ctx)
			if err != nil {
				logger.Error().Err(err).Msg("failed to reload tenant configs for delta")
				continue
			}

			delta := &provisioningv1.WatchTenantConfigResponse{
				IsSnapshot: false,
				Tenants:    updatedConfigs,
			}
			if err := stream.Send(delta); err != nil {
				return status.Errorf(codes.Unavailable, "send tenant config delta: %v", err)
			}

			logger.Debug().Int("tenant_count", len(updatedConfigs)).Msg("sent tenant config delta")
		}
	}
}

// WatchTopics streams topic discovery data to the caller.
func (s *Server) WatchTopics(req *provisioningv1.WatchTopicsRequest, stream grpc.ServerStreamingServer[provisioningv1.WatchTopicsResponse]) error {
	ctx := stream.Context()
	namespace := req.GetNamespace()
	logger := s.logger.With().Str("rpc", "WatchTopics").Str("namespace", namespace).Logger()

	// Load and send initial snapshot
	topicsResp, err := s.loadTopicsUpdate(ctx, namespace)
	if err != nil {
		return status.Errorf(codes.Internal, "load topics: %v", err)
	}

	topicsResp.IsSnapshot = true
	if err := stream.Send(topicsResp); err != nil {
		return status.Errorf(codes.Unavailable, "send topics snapshot: %v", err)
	}

	logger.Info().
		Int("shared_topics", len(topicsResp.SharedTopics)).
		Int("dedicated_tenants", len(topicsResp.DedicatedTenants)).
		Msg("sent topics snapshot")

	// Subscribe to event bus for changes
	subID, events := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(subID)

	for {
		select {
		case <-ctx.Done():
			logger.Debug().Msg("stream context cancelled")
			return nil

		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.Type != eventbus.TopicsChanged {
				continue
			}

			updatedTopics, err := s.loadTopicsUpdate(ctx, namespace)
			if err != nil {
				logger.Error().Err(err).Msg("failed to reload topics for delta")
				continue
			}

			updatedTopics.IsSnapshot = false
			if err := stream.Send(updatedTopics); err != nil {
				return status.Errorf(codes.Unavailable, "send topics delta: %v", err)
			}

			logger.Debug().
				Int("shared_topics", len(updatedTopics.SharedTopics)).
				Int("dedicated_tenants", len(updatedTopics.DedicatedTenants)).
				Msg("sent topics delta")
		}
	}
}

// loadTenantConfigs loads all tenant OIDC configs, channel rules, and routing rules.
func (s *Server) loadTenantConfigs(ctx context.Context) ([]*provisioningv1.TenantConfig, error) {
	tenants, _, err := s.service.ListTenants(ctx, provisioning.ListOptions{Limit: maxTenantsFetchLimit})
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}

	var configs []*provisioningv1.TenantConfig

	for _, tenant := range tenants {
		if tenant.Status != provisioning.StatusActive {
			continue
		}

		tc := &provisioningv1.TenantConfig{
			TenantId: tenant.ID,
		}

		// Load OIDC config (optional)
		oidcConfig, err := s.service.GetOIDCConfig(ctx, tenant.ID)
		if err == nil && oidcConfig != nil {
			tc.Oidc = &provisioningv1.OIDCConfig{
				IssuerUrl: oidcConfig.IssuerURL,
				JwksUrl:   oidcConfig.JWKSURL,
				Audience:  oidcConfig.Audience,
				Enabled:   oidcConfig.Enabled,
			}
		}

		// Load channel rules (optional)
		channelRules, err := s.service.GetChannelRules(ctx, tenant.ID)
		if err == nil && channelRules != nil {
			tc.ChannelRules = convertChannelRules(&channelRules.Rules)
		}

		// Load routing rules (optional)
		routingRules, err := s.service.GetRoutingRules(ctx, tenant.ID)
		if err == nil && len(routingRules) > 0 {
			tc.RoutingRules = convertRoutingRules(routingRules)
		}

		configs = append(configs, tc)
	}

	return configs, nil
}

// loadTopicsUpdate builds a WatchTopicsResponse from service data.
func (s *Server) loadTopicsUpdate(ctx context.Context, namespace string) (*provisioningv1.WatchTopicsResponse, error) {
	tenants, _, err := s.service.ListTenants(ctx, provisioning.ListOptions{Limit: maxTenantsFetchLimit})
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}

	var sharedTopics []string
	var dedicatedTenants []*provisioningv1.DedicatedTenant

	for _, tenant := range tenants {
		if tenant.Status != provisioning.StatusActive {
			continue
		}

		rules, err := s.service.GetRoutingRules(ctx, tenant.ID)
		if err != nil {
			s.logger.Debug().Err(err).Str("tenant_id", tenant.ID).
				Msg("Skipping tenant in topics update: no routing rules")
			continue
		}

		var topics []string
		for _, suffix := range types.UniqueTopicSuffixes(rules) {
			topic := kafka.BuildTopicName(namespace, tenant.ID, suffix)
			topics = append(topics, topic)
		}

		if len(topics) == 0 {
			continue
		}

		switch tenant.ConsumerType {
		case provisioning.ConsumerShared:
			sharedTopics = append(sharedTopics, topics...)
		case provisioning.ConsumerDedicated:
			dedicatedTenants = append(dedicatedTenants, &provisioningv1.DedicatedTenant{
				TenantId: tenant.ID,
				Topics:   topics,
			})
		default:
			sharedTopics = append(sharedTopics, topics...)
		}
	}

	return &provisioningv1.WatchTopicsResponse{
		SharedTopics:     sharedTopics,
		DedicatedTenants: dedicatedTenants,
	}, nil
}

// convertKeys converts provisioning keys to proto KeyInfo messages.
func convertKeys(keys []*provisioning.TenantKey) []*provisioningv1.KeyInfo {
	result := make([]*provisioningv1.KeyInfo, 0, len(keys))
	for _, k := range keys {
		ki := &provisioningv1.KeyInfo{
			KeyId:        k.KeyID,
			TenantId:     k.TenantID,
			Algorithm:    string(k.Algorithm),
			PublicKeyPem: k.PublicKey,
			IsActive:     k.RevokedAt == nil,
		}
		if k.ExpiresAt != nil {
			ki.ExpiresAtUnix = k.ExpiresAt.Unix()
		}
		result = append(result, ki)
	}
	return result
}

// convertRoutingRules converts types.TopicRoutingRule to proto TopicRoutingRule.
func convertRoutingRules(rules []types.TopicRoutingRule) []*provisioningv1.TopicRoutingRule {
	result := make([]*provisioningv1.TopicRoutingRule, 0, len(rules))
	for _, r := range rules {
		result = append(result, &provisioningv1.TopicRoutingRule{
			Pattern:     r.Pattern,
			TopicSuffix: r.TopicSuffix,
		})
	}
	return result
}

// convertChannelRules converts types.ChannelRules to proto ChannelRules.
func convertChannelRules(rules *types.ChannelRules) *provisioningv1.ChannelRules {
	cr := &provisioningv1.ChannelRules{
		PublicChannels:  rules.Public,
		DefaultChannels: rules.Default,
	}

	if len(rules.GroupMappings) > 0 {
		cr.GroupMappings = make(map[string]*provisioningv1.GroupChannels)
		for group, channels := range rules.GroupMappings {
			cr.GroupMappings[group] = &provisioningv1.GroupChannels{
				Channels: channels,
			}
		}
	}

	return cr
}
