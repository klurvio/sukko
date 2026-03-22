package grpcserver

import (
	"context"
	"errors"
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

// ServerConfig holds configuration for the gRPC stream server.
type ServerConfig struct {
	// MaxTenantsFetchLimit is the upper bound for listing all tenants in bulk
	// operations (snapshot/delta loading). This prevents unbounded queries while
	// being high enough to cover all tenants in practice.
	MaxTenantsFetchLimit int
}

// Server implements the ProvisioningInternalServiceServer gRPC interface.
// It streams provisioning data (keys, tenant config, topics) to gateway and ws-server
// using an event bus for real-time change notifications.
type Server struct {
	provisioningv1.UnimplementedProvisioningInternalServiceServer

	service              *provisioning.Service
	eventBus             *eventbus.Bus
	logger               zerolog.Logger
	maxTenantsFetchLimit int
}

// NewServer creates a new gRPC stream server.
func NewServer(service *provisioning.Service, eventBus *eventbus.Bus, logger zerolog.Logger, cfg ServerConfig) (*Server, error) {
	if service == nil {
		return nil, errors.New("grpc server: service is required")
	}
	if eventBus == nil {
		return nil, errors.New("grpc server: event bus is required")
	}
	if cfg.MaxTenantsFetchLimit <= 0 {
		return nil, errors.New("grpc server: MaxTenantsFetchLimit must be > 0")
	}

	return &Server{
		service:              service,
		eventBus:             eventBus,
		logger:               logger.With().Str("component", "grpc_server").Logger(),
		maxTenantsFetchLimit: cfg.MaxTenantsFetchLimit,
	}, nil
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
			logger.Debug().Msg("stream context canceled")
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

// WatchTenantConfig streams tenant configuration (channel rules, routing rules) to the caller.
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
			logger.Debug().Msg("stream context canceled")
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
		Int("shared_topics", len(topicsResp.GetSharedTopics())).
		Int("dedicated_tenants", len(topicsResp.GetDedicatedTenants())).
		Msg("sent topics snapshot")

	// Subscribe to event bus for changes
	subID, events := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(subID)

	for {
		select {
		case <-ctx.Done():
			logger.Debug().Msg("stream context canceled")
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
				Int("shared_topics", len(updatedTopics.GetSharedTopics())).
				Int("dedicated_tenants", len(updatedTopics.GetDedicatedTenants())).
				Msg("sent topics delta")
		}
	}
}

// WatchAPIKeys streams active API keys to the caller. Sends a snapshot on connect,
// then streams deltas when API keys change via event bus.
func (s *Server) WatchAPIKeys(_ *provisioningv1.WatchAPIKeysRequest, stream grpc.ServerStreamingServer[provisioningv1.WatchAPIKeysResponse]) error {
	ctx := stream.Context()
	logger := s.logger.With().Str("rpc", "WatchAPIKeys").Logger()

	// Load and send initial snapshot
	keys, err := s.service.GetActiveAPIKeys(ctx)
	if err != nil {
		return status.Errorf(codes.Internal, "load active api keys: %v", err)
	}

	snapshot := &provisioningv1.WatchAPIKeysResponse{
		IsSnapshot: true,
		ApiKeys:    convertAPIKeys(keys),
	}
	if err := stream.Send(snapshot); err != nil {
		return status.Errorf(codes.Unavailable, "send api keys snapshot: %v", err)
	}

	logger.Info().Int("key_count", len(keys)).Msg("sent api keys snapshot")

	// Subscribe to event bus for changes
	subID, events := s.eventBus.Subscribe()
	defer s.eventBus.Unsubscribe(subID)

	// Stream deltas on change
	for {
		select {
		case <-ctx.Done():
			logger.Debug().Msg("stream context canceled")
			return nil

		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.Type != eventbus.APIKeysChanged {
				continue
			}

			// Reload all active API keys and send as delta
			updatedKeys, err := s.service.GetActiveAPIKeys(ctx)
			if err != nil {
				logger.Error().Err(err).Msg("failed to reload api keys for delta")
				continue
			}

			delta := &provisioningv1.WatchAPIKeysResponse{
				IsSnapshot: false,
				ApiKeys:    convertAPIKeys(updatedKeys),
			}
			if err := stream.Send(delta); err != nil {
				return status.Errorf(codes.Unavailable, "send api keys delta: %v", err)
			}

			logger.Debug().Int("key_count", len(updatedKeys)).Msg("sent api keys delta")
		}
	}
}

// loadTenantConfigs loads all tenant channel rules and routing rules.
func (s *Server) loadTenantConfigs(ctx context.Context) ([]*provisioningv1.TenantConfig, error) {
	tenants, _, err := s.service.ListTenants(ctx, provisioning.ListOptions{Limit: s.maxTenantsFetchLimit})
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

		// Load channel rules (optional — not all tenants have rules configured)
		channelRules, err := s.service.GetChannelRules(ctx, tenant.ID)
		if err != nil && !errors.Is(err, types.ErrChannelRulesNotFound) && !errors.Is(err, provisioning.ErrChannelRulesNotConfigured) {
			s.logger.Warn().Err(err).Str("tenant_id", tenant.ID).Msg("failed to load channel rules")
		}
		if err == nil && channelRules != nil {
			tc.ChannelRules = convertChannelRules(&channelRules.Rules)
		}

		// Load routing rules (optional — not all tenants have rules configured)
		routingRules, err := s.service.GetRoutingRules(ctx, tenant.ID)
		if err != nil && !errors.Is(err, provisioning.ErrRoutingRulesNotConfigured) && !errors.Is(err, provisioning.ErrRoutingRulesNotFound) {
			s.logger.Debug().Err(err).Str("tenant_id", tenant.ID).Msg("failed to load routing rules")
		}
		if err == nil && len(routingRules) > 0 {
			tc.RoutingRules = convertRoutingRules(routingRules)
		}

		configs = append(configs, tc)
	}

	return configs, nil
}

// loadTopicsUpdate builds a WatchTopicsResponse from service data.
func (s *Server) loadTopicsUpdate(ctx context.Context, namespace string) (*provisioningv1.WatchTopicsResponse, error) {
	tenants, _, err := s.service.ListTenants(ctx, provisioning.ListOptions{Limit: s.maxTenantsFetchLimit})
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
		for _, suffix := range provisioning.UniqueTopicSuffixes(rules) {
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

// convertRoutingRules converts provisioning.TopicRoutingRule to proto TopicRoutingRule.
func convertRoutingRules(rules []provisioning.TopicRoutingRule) []*provisioningv1.TopicRoutingRule {
	result := make([]*provisioningv1.TopicRoutingRule, 0, len(rules))
	for _, r := range rules {
		result = append(result, &provisioningv1.TopicRoutingRule{
			Pattern:     r.Pattern,
			TopicSuffix: r.TopicSuffix,
		})
	}
	return result
}

// convertAPIKeys converts provisioning API keys to proto APIKeyInfo messages.
func convertAPIKeys(keys []*provisioning.APIKey) []*provisioningv1.APIKeyInfo {
	result := make([]*provisioningv1.APIKeyInfo, 0, len(keys))
	for _, k := range keys {
		result = append(result, &provisioningv1.APIKeyInfo{
			KeyId:    k.KeyID,
			TenantId: k.TenantID,
			Name:     k.Name,
			IsActive: k.IsActive,
		})
	}
	return result
}

// convertChannelRules converts types.ChannelRules to proto ChannelRules.
func convertChannelRules(rules *types.ChannelRules) *provisioningv1.ChannelRules {
	cr := &provisioningv1.ChannelRules{
		PublicChannels:         rules.Public,
		DefaultChannels:        rules.Default,
		PublishPublicChannels:  rules.PublishPublic,
		PublishDefaultChannels: rules.PublishDefault,
	}

	if len(rules.GroupMappings) > 0 {
		cr.GroupMappings = make(map[string]*provisioningv1.GroupChannels)
		for group, channels := range rules.GroupMappings {
			cr.GroupMappings[group] = &provisioningv1.GroupChannels{
				Channels: channels,
			}
		}
	}

	if len(rules.PublishGroupMappings) > 0 {
		cr.PublishGroupMappings = make(map[string]*provisioningv1.GroupChannels)
		for group, channels := range rules.PublishGroupMappings {
			cr.PublishGroupMappings[group] = &provisioningv1.GroupChannels{
				Channels: channels,
			}
		}
	}

	return cr
}
