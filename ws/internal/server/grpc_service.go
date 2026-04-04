package server

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	serverv1 "github.com/klurvio/sukko/gen/proto/sukko/server/v1"
	srvmetrics "github.com/klurvio/sukko/internal/server/metrics"
	"github.com/klurvio/sukko/internal/shared/logging"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
)

// GRPCService implements the RealtimeService gRPC server.
// Handles Publish (unary) and Subscribe (server-streaming) RPCs.
// Lives in the server package — direct access to Server internals, no interface indirection.
type GRPCService struct {
	serverv1.UnimplementedRealtimeServiceServer
	servers []*Server
	logger  zerolog.Logger
}

// NewGRPCService creates a RealtimeService implementation.
// Shard selection is handled internally — main.go just passes the server list.
func NewGRPCService(servers []*Server, logger zerolog.Logger) *GRPCService {
	return &GRPCService{
		servers: servers,
		logger:  logger,
	}
}

// selectServer picks a server for an incoming request.
// Single shard: returns the one server (zero overhead).
// Multi-shard: returns the shard with fewest active connections (same logic as WebSocket LoadBalancer).
func (svc *GRPCService) selectServer() *Server {
	if len(svc.servers) == 1 {
		return svc.servers[0]
	}
	best := svc.servers[0]
	bestLoad := best.stats.CurrentConnections.Load()
	for _, s := range svc.servers[1:] {
		load := s.stats.CurrentConnections.Load()
		if load < bestLoad {
			best = s
			bestLoad = load
		}
	}
	return best
}

// Publish sends a message to a channel via the message backend.
// The gateway validates auth/permissions before calling this RPC.
func (svc *GRPCService) Publish(ctx context.Context, req *serverv1.PublishRequest) (*serverv1.PublishResponse, error) {
	// Validate request
	if req.GetChannel() == "" {
		return nil, status.Error(codes.InvalidArgument, "channel is required")
	}
	if len(req.GetData()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	// Select any server (message goes to Kafka → NATS broadcast → all shards)
	s := svc.selectServer()

	// Publish to message backend (routing rules → Kafka topic)
	if err := s.backend.Publish(ctx, 0, req.GetChannel(), req.GetData()); err != nil {
		svc.logger.Error().Err(err).
			Str("channel", req.GetChannel()).
			Str("tenant_id", req.GetTenantId()).
			Str("principal", req.GetPrincipal()).
			Msg("gRPC Publish failed")
		return nil, status.Errorf(codes.Internal, "publish: %v", err)
	}

	svc.logger.Debug().
		Str("channel", req.GetChannel()).
		Str("tenant_id", req.GetTenantId()).
		Msg("gRPC Publish accepted")

	return &serverv1.PublishResponse{
		Status:  "accepted",
		Channel: req.GetChannel(),
	}, nil
}

// Subscribe opens a server-streaming RPC for receiving messages.
// Each stream maps to one virtual client in a shard's subscription index.
func (svc *GRPCService) Subscribe(req *serverv1.SubscribeRequest, stream serverv1.RealtimeService_SubscribeServer) error {
	// Validate request
	if len(req.GetChannels()) == 0 {
		return status.Error(codes.InvalidArgument, "at least one channel is required")
	}

	// Select least-loaded shard (same distribution as WebSocket LoadBalancer)
	s := svc.selectServer()

	// Acquire connection slot (FR-019: SSE counts toward same pool as WebSocket)
	select {
	case s.connectionsSem <- struct{}{}:
		// acquired
	default:
		return status.Error(codes.ResourceExhausted, "server at connection capacity")
	}

	// Record connection metrics
	srvmetrics.UpdateConnectionMetrics(string(TransportGRPCStream))

	// Create cancellable context for the virtual client's transport
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// Create virtual client with gRPC stream transport
	client := s.connections.Get()
	client.transport = NewGRPCStreamTransport(stream, cancel, req.GetRemoteAddr())
	client.server = s
	client.id = s.clientCount.Add(1)
	client.remoteAddr = req.GetRemoteAddr()

	// Track client
	s.clients.Store(client, true)
	s.stats.TotalConnections.Add(1)
	s.stats.CurrentConnections.Add(1)

	// Register channel subscriptions
	for _, ch := range req.GetChannels() {
		client.subscriptions.Add(ch)
		s.subscriptionIndex.Add(ch, client)
	}

	// Start write pump
	s.wg.Go(func() {
		defer logging.RecoverPanic(s.logger, "sse_writePump", nil)
		s.pump.WriteLoop(ctx, client)
	})

	svc.logger.Info().
		Int64("client_id", client.id).
		Str("tenant_id", req.GetTenantId()).
		Str("principal", req.GetPrincipal()).
		Strs("channels", req.GetChannels()).
		Str("remote_addr", req.GetRemoteAddr()).
		Msg("SSE Subscribe stream started")

	// Block until stream closes (client disconnect or server shutdown)
	<-ctx.Done()

	// Cleanup — same pattern as WebSocket disconnectClient
	duration := time.Since(client.connectedAt)
	srvmetrics.RecordDisconnectWithStats(s.stats, string(TransportGRPCStream),
		pkgmetrics.DisconnectClientInitiated, pkgmetrics.InitiatedByClient, duration)

	client.closeOnce.Do(func() {
		if client.transport != nil {
			_ = client.transport.Close()
		}
	})
	client.closeSend()

	s.clients.Delete(client)
	s.stats.CurrentConnections.Add(-1)
	s.subscriptionIndex.RemoveClient(client)
	s.connections.Put(client)
	<-s.connectionsSem // Release connection slot

	svc.logger.Info().
		Int64("client_id", client.id).
		Str("tenant_id", req.GetTenantId()).
		Dur("connection_duration", duration).
		Msg("SSE Subscribe stream ended")

	return nil
}
