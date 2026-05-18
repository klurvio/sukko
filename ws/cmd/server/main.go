// Package main provides the entry point for the WebSocket server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"net"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"

	serverv1 "github.com/klurvio/sukko/gen/proto/sukko/server/v1"
	"github.com/klurvio/sukko/internal/server"
	"github.com/klurvio/sukko/internal/server/backend"
	"github.com/klurvio/sukko/internal/server/backend/directbackend"
	"github.com/klurvio/sukko/internal/server/backend/jetstreambackend"
	"github.com/klurvio/sukko/internal/server/backend/kafkabackend"
	"github.com/klurvio/sukko/internal/server/broadcast"
	"github.com/klurvio/sukko/internal/server/metrics"
	"github.com/klurvio/sukko/internal/server/orchestration"
	"github.com/klurvio/sukko/internal/shared/alerting"
	"github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/profiling"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/tracing"
)

const serviceName = "ws-server"

func main() {
	// Bootstrap logger for pre-config startup (zerolog without config dependency)
	bootLogger := logging.BootstrapLogger(serviceName)

	// Go 1.25+ runtime automatically sets GOMAXPROCS from container cgroup CPU limits
	maxProcs := runtime.GOMAXPROCS(0)
	bootLogger.Info().Int("gomaxprocs", maxProcs).Msg("GOMAXPROCS set by Go runtime (container-aware)")

	// Load configuration from .env file and environment variables
	cfg, err := platform.LoadServerConfig(bootLogger)
	if err != nil {
		bootLogger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// CLI flags use env var config as defaults (CLI overrides env overrides envDefault)
	var (
		debug          = flag.Bool("debug", cfg.LogLevel == "debug", "enable debug logging (overrides LOG_LEVEL)")
		validateConfig = flag.Bool("validate-config", false, "validate configuration and exit")
		numShards      = flag.Int("shards", cfg.NumShards, "number of shards to run")
		basePort       = flag.Int("base-port", cfg.BasePort, "base port for shards (e.g., 3002, 3003, ...)")
		lbAddr         = flag.String("lb-addr", cfg.LBAddr, "address for the load balancer to listen on")
	)
	flag.Parse()

	// Override debug mode if flag set
	if *debug {
		cfg.LogLevel = "debug"
		bootLogger.Info().Msg("Debug mode enabled via flag")
	}

	// --validate-config: validate and exit
	if *validateConfig {
		cfg.Print()
		bootLogger.Info().Msg("Configuration is valid")
		os.Exit(0)
	}

	// Print human-readable config for startup logs
	cfg.Print()

	// Initialize SystemMonitor singleton FIRST (before creating any ResourceGuards)
	// This ensures all ResourceGuards share the same system metrics source
	structuredLogger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: serviceName,
	})
	systemMonitor := metrics.GetSystemMonitor(structuredLogger, cfg.CPUEWMABeta)
	systemMonitor.StartMonitoring(cfg.MetricsInterval, cfg.CPUPollInterval)
	structuredLogger.Info().
		Dur("metrics_interval", cfg.MetricsInterval).
		Dur("cpu_poll_interval", cfg.CPUPollInterval).
		Msg("SystemMonitor started")

	// Log edition
	structuredLogger.Info().
		Str("edition", cfg.EditionManager().Edition().String()).
		Str("org", cfg.EditionManager().Org()).
		Msg("Sukko edition resolved")

	// Context for goroutine lifecycle — cancel() signals server error or license downgrade to main.
	// Must be declared before licenseWatcher so OnDowngrade can capture cancel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// License watcher — subscribe to WatchLicense gRPC stream for runtime updates
	backendMismatch := &atomic.Bool{}
	backendMismatchGauge := promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_license_backend_mismatch",
		Help: "Whether the current MESSAGE_BACKEND is unsupported by the current license edition (1=mismatch, 0=ok)",
	})

	// rulesProvider is declared before licenseWatcher so its UpdateEdition method
	// can be called from the OnReload closure. atomic.Pointer guards concurrent
	// access between the main goroutine (Store) and the licenseWatcher goroutine (Load).
	var rulesProvider atomic.Pointer[provapi.StreamChannelRulesProvider]

	licenseWatcher, licErr := provapi.NewStreamLicenseWatcher(provapi.StreamLicenseWatcherConfig{
		GRPCAddr:          cfg.ProvisioningGRPCAddr,
		ReconnectDelay:    cfg.GRPCReconnectDelay,
		ReconnectMaxDelay: cfg.GRPCReconnectMaxDelay,
		MetricPrefix:      "ws",
		Manager:           cfg.EditionManager(),
		Logger:            structuredLogger,
		OnReload: func() {
			edition := cfg.EditionManager().Edition()
			if rp := rulesProvider.Load(); rp != nil {
				rp.UpdateEdition(edition)
			}
			mismatch := checkBackendMismatch(cfg.MessageBackend, edition)
			backendMismatch.Store(mismatch)
			if mismatch {
				backendMismatchGauge.Set(1)
				structuredLogger.Warn().
					Str("backend", cfg.MessageBackend).
					Str("edition", string(edition)).
					Msg("Backend mismatch: MESSAGE_BACKEND is not supported by current edition. Restart required.")
			} else {
				backendMismatchGauge.Set(0)
			}
		},
		OnDowngrade: provapi.LicenseDowngradeShutdown(structuredLogger, cancel),
	})
	if licErr != nil {
		structuredLogger.Fatal().Err(licErr).Msg("Failed to create license watcher")
	}

	rp, err := provapi.NewStreamChannelRulesProvider(provapi.StreamChannelRulesProviderConfig{
		GRPCAddr:          cfg.ProvisioningGRPCAddr,
		ReconnectDelay:    cfg.GRPCReconnectDelay,
		ReconnectMaxDelay: cfg.GRPCReconnectMaxDelay,
		MetricPrefix:      "ws",
		Logger:            structuredLogger,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create channel rules provider")
	}
	rulesProvider.Store(rp)

	// Initialize tracing (cold-path only, noop when disabled)
	tracingShutdown, err := tracing.Init(context.Background(), tracing.Config{
		Enabled:      cfg.OTELTracingEnabled,
		ExporterType: cfg.OTELExporterType,
		Endpoint:     cfg.OTELExporterEndpoint,
		ServiceName:  serviceName,
		Environment:  cfg.Environment,
	}, structuredLogger)
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to initialize tracing")
	}
	defer func() { _ = tracingShutdown(context.Background()) }()

	// Initialize profiling (Pyroscope continuous profiling, noop when disabled)
	// pprof endpoints are registered on the LB's HTTP mux (see LoadBalancerConfig.PprofEnabled)
	pyroscopeStop, err := profiling.InitPyroscope(profiling.PyroscopeConfig{
		Enabled:     cfg.PyroscopeEnabled,
		Addr:        cfg.PyroscopeAddr,
		ServiceName: serviceName,
		Environment: cfg.Environment,
	}, structuredLogger)
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to initialize Pyroscope")
	}
	defer pyroscopeStop()

	// Calculate max connections per shard
	maxConnsPerShard := cfg.MaxConnections / *numShards
	if maxConnsPerShard == 0 {
		maxConnsPerShard = 1 // Ensure at least 1 connection per shard
	}
	structuredLogger.Info().
		Int("total_max_connections", cfg.MaxConnections).
		Int("shards", *numShards).
		Int("max_connections_per_shard", maxConnsPerShard).
		Msg("Shard capacity calculated")

	// Build broadcast config based on BROADCAST_TYPE
	busCfg := broadcast.Config{
		Type:            cfg.BroadcastType,
		BufferSize:      cfg.BroadcastBufferSize,
		ShutdownTimeout: cfg.BroadcastShutdownTimeout,
		Valkey: broadcast.ValkeyConfig{
			Addrs:                     cfg.ValkeyAddrs,
			MasterName:                cfg.ValkeyMasterName,
			Password:                  cfg.ValkeyPassword,
			DB:                        cfg.ValkeyDB,
			Channel:                   cfg.ValkeyChannel,
			TLSEnabled:                cfg.ValkeyTLSEnabled,
			TLSInsecure:               cfg.ValkeyTLSInsecure,
			TLSCAPath:                 cfg.ValkeyTLSCAPath,
			PoolSize:                  cfg.ValkeyPoolSize,
			MinIdleConns:              cfg.ValkeyMinIdleConns,
			DialTimeout:               cfg.ValkeyDialTimeout,
			ReadTimeout:               cfg.ValkeyReadTimeout,
			WriteTimeout:              cfg.ValkeyWriteTimeout,
			MaxRetries:                cfg.ValkeyMaxRetries,
			MinRetryBackoff:           cfg.ValkeyMinRetryBackoff,
			MaxRetryBackoff:           cfg.ValkeyMaxRetryBackoff,
			PublishTimeout:            cfg.ValkeyPublishTimeout,
			StartupPingTimeout:        cfg.ValkeyStartupPingTimeout,
			ReconnectInitialBackoff:   cfg.ValkeyReconnectInitialBackoff,
			ReconnectMaxBackoff:       cfg.ValkeyReconnectMaxBackoff,
			ReconnectMaxAttempts:      cfg.ValkeyReconnectMaxAttempts,
			HealthCheckInterval:       cfg.ValkeyHealthCheckInterval,
			HealthCheckTimeout:        cfg.ValkeyHealthCheckTimeout,
			PublishStalenessThreshold: cfg.ValkeyPublishStalenessThreshold,
		},
		NATS: broadcast.NATSConfig{
			URLs:                cfg.NATSURLs,
			ClusterMode:         cfg.NATSClusterMode,
			Subject:             cfg.NATSSubject,
			Token:               cfg.NATSToken,
			User:                cfg.NATSUser,
			Password:            cfg.NATSPassword,
			TLSEnabled:          cfg.NATSTLSEnabled,
			TLSInsecure:         cfg.NATSTLSInsecure,
			TLSCAPath:           cfg.NATSTLSCAPath,
			ReconnectWait:       cfg.NATSReconnectWait,
			MaxReconnects:       cfg.NATSMaxReconnects,
			ReconnectBufSize:    cfg.NATSReconnectBufSize,
			PingInterval:        cfg.NATSPingInterval,
			MaxPingsOutstanding: cfg.NATSMaxPingsOutstanding,
			HealthCheckInterval: cfg.NATSHealthCheckInterval,
			FlushTimeout:        cfg.NATSFlushTimeout,
		},
	}

	broadcastBus, err := broadcast.NewBus(busCfg, structuredLogger)
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create BroadcastBus")
	}

	structuredLogger.Info().Str("type", cfg.BroadcastType).Msg("BroadcastBus initialized")
	broadcastBus.Run()

	// Create pluggable message backend based on MESSAGE_BACKEND env var
	topicNamespace := kafka.ResolveNamespace(cfg.KafkaTopicNamespaceOverride, cfg.Environment)
	var msgBackend backend.MessageBackend
	switch cfg.MessageBackend {
	case "direct":
		msgBackend, err = directbackend.New(broadcastBus, structuredLogger)
	case "kafka":
		msgBackend, err = kafkabackend.New(kafkabackend.Config{
			Brokers:                  kafkabackend.SplitBrokers(cfg.KafkaBrokers),
			Namespace:                topicNamespace,
			Environment:              cfg.Environment,
			SASLEnabled:              cfg.KafkaSASLEnabled,
			SASLMechanism:            cfg.KafkaSASLMechanism,
			SASLUsername:             cfg.KafkaSASLUsername,
			SASLPassword:             cfg.KafkaSASLPassword,
			TLSEnabled:               cfg.KafkaTLSEnabled,
			TLSInsecure:              cfg.KafkaTLSInsecure,
			TLSCAPath:                cfg.KafkaTLSCAPath,
			KafkaConsumerEnabled:     cfg.KafkaConsumerEnabled,
			DefaultPartitions:        cfg.KafkaDefaultPartitions,
			DefaultReplicationFactor: cfg.KafkaDefaultReplicationFactor,
			MaxKafkaMessagesPerSec:   cfg.MaxKafkaMessagesPerSec,
			MaxBroadcastsPerSec:      cfg.MaxBroadcastsPerSec,
			RateLimitBurstMultiplier: cfg.RateLimitBurstMultiplier,
			CPUPauseThreshold:        cfg.CPUPauseThreshold,
			CPUPauseThresholdLower:   cfg.CPUPauseThresholdLower,
			CPURejectThreshold:       cfg.CPURejectThreshold,
			CPURejectThresholdLower:  cfg.CPURejectThresholdLower,
			Logger:                   structuredLogger,
			ProvisioningGRPCAddr:     cfg.ProvisioningGRPCAddr,
			GRPCReconnectDelay:       cfg.GRPCReconnectDelay,
			GRPCReconnectMaxDelay:    cfg.GRPCReconnectMaxDelay,
			TopicRefreshInterval:     cfg.TopicRefreshInterval,
			TopicCreationTimeout:     cfg.TopicCreationTimeout,
			BroadcastBus:             broadcastBus,
			// Kafka producer tuning
			ProducerBatchMaxBytes:             cfg.KafkaProducerBatchMaxBytes,
			ProducerMaxBufferedRecs:           cfg.KafkaProducerMaxBufferedRecords,
			ProducerRecordRetries:             cfg.KafkaProducerRecordRetries,
			ProducerCircuitBreakerTimeout:     cfg.KafkaProducerCBTimeout,
			ProducerCircuitBreakerMaxFailures: cfg.KafkaProducerCBMaxFailures,
			ProducerCircuitBreakerHalfOpen:    cfg.KafkaProducerCBHalfOpenReqs,
			// Kafka consumer tuning
			KafkaBatchSize:    cfg.KafkaBatchSize,
			KafkaBatchTimeout: cfg.KafkaBatchTimeout,
			// Kafka consumer transport tuning
			KafkaFetchMaxWait:              cfg.KafkaFetchMaxWait,
			KafkaFetchMinBytes:             cfg.KafkaFetchMinBytes,
			KafkaFetchMaxBytes:             cfg.KafkaFetchMaxBytes,
			KafkaSessionTimeout:            cfg.KafkaSessionTimeout,
			KafkaRebalanceTimeout:          cfg.KafkaRebalanceTimeout,
			KafkaReplayFetchMaxBytes:       cfg.KafkaReplayFetchMaxBytes,
			KafkaBackpressureCheckInterval: cfg.KafkaBackpressureCheckInterval,
		})
	case "nats":
		// Create a StreamTopicRegistry for tenant discovery via gRPC
		topicRegistry, regErr := provapi.NewStreamTopicRegistry(provapi.StreamTopicRegistryConfig{
			GRPCAddr:          cfg.ProvisioningGRPCAddr,
			Namespace:         topicNamespace,
			ReconnectDelay:    cfg.GRPCReconnectDelay,
			ReconnectMaxDelay: cfg.GRPCReconnectMaxDelay,
			MetricPrefix:      "ws",
			Logger:            structuredLogger,
		})
		if regErr != nil {
			structuredLogger.Fatal().Err(regErr).Msg("Failed to create topic registry for JetStream")
		}

		msgBackend, err = jetstreambackend.New(jetstreambackend.Config{
			URLs:            jetstreambackend.SplitURLs(cfg.NATSJetStreamURLs),
			Token:           cfg.NATSJetStreamToken,
			User:            cfg.NATSJetStreamUser,
			Password:        cfg.NATSJetStreamPassword,
			TLSEnabled:      cfg.NATSJetStreamTLSEnabled,
			TLSInsecure:     cfg.NATSJetStreamTLSInsecure,
			TLSCAPath:       cfg.NATSJetStreamTLSCAPath,
			Replicas:        cfg.NATSJetStreamReplicas,
			MaxAge:          cfg.JetStreamMaxAge,
			Namespace:       topicNamespace,
			BroadcastBus:    broadcastBus,
			Registry:        topicRegistry,
			RefreshInterval: cfg.JetStreamRefreshInterval,
			LogLevel:        cfg.LogLevel,
			LogFormat:       cfg.LogFormat,
			ReconnectWait:   cfg.JetStreamReconnectWait,
			MaxDeliver:      cfg.JetStreamMaxDeliver,
			AckWait:         cfg.JetStreamAckWait,
			RefreshTimeout:  cfg.JetStreamRefreshTimeout,
			ReplayFetchWait: cfg.JetStreamReplayFetchWait,
		})
	default:
		structuredLogger.Fatal().Str("backend", cfg.MessageBackend).Msg("Unknown message backend (valid: direct, kafka, nats)")
	}
	if err != nil {
		structuredLogger.Fatal().Err(err).Str("backend", cfg.MessageBackend).Msg("Failed to create message backend")
	}

	if err := msgBackend.Start(ctx); err != nil {
		structuredLogger.Fatal().Err(err).Str("backend", cfg.MessageBackend).Msg("Failed to start message backend")
	}
	structuredLogger.Info().Str("backend", cfg.MessageBackend).Msg("Message backend started")

	// Initialize alerter from validated config
	alertCfg := cfg.AlertConfig(serviceName)
	alertCfg.Logger = structuredLogger
	alerter := alerting.NewFromConfig(alertCfg)

	// Create and start shards
	shards := make([]*orchestration.Shard, *numShards)
	for i := range *numShards {
		// Bind address: 0.0.0.0 to accept both IPv4/IPv6 connections
		shardBindAddr := fmt.Sprintf("0.0.0.0:%d", *basePort+i)
		// Advertise address: 127.0.0.1 (IPv4 loopback) for LoadBalancer to connect to
		shardAdvertiseAddr := fmt.Sprintf("127.0.0.1:%d", *basePort+i)

		shard, err := orchestration.NewShard(orchestration.ShardConfig{
			ID:             i,
			Addr:           shardBindAddr,
			AdvertiseAddr:  shardAdvertiseAddr,
			Config:         cfg,
			BroadcastBus:   broadcastBus,
			MessageBackend: msgBackend,
			Alerter:        alerter,
			Logger:         logging.NewLogger(logging.LoggerConfig{Level: logging.LogLevel(cfg.LogLevel), Format: logging.LogFormat(cfg.LogFormat), ServiceName: serviceName}),
			MaxConnections: maxConnsPerShard,
		})
		if err != nil {
			structuredLogger.Fatal().Err(err).Int("shard_id", i).Msg("Failed to create shard")
		}

		if err := shard.Start(); err != nil {
			structuredLogger.Fatal().Err(err).Int("shard_id", i).Msg("Failed to start shard")
		}
		shards[i] = shard
	}

	// Initialize and start LoadBalancer
	lb, err := orchestration.NewLoadBalancer(orchestration.LoadBalancerConfig{
		Addr:   *lbAddr,
		Shards: shards,
		Logger: logging.NewLogger(logging.LoggerConfig{Level: logging.LogLevel(cfg.LogLevel), Format: logging.LogFormat(cfg.LogFormat), ServiceName: serviceName}),
		// TCP/HTTP tuning for trading platform burst tolerance
		HTTPReadTimeout:            cfg.HTTPReadTimeout,
		HTTPWriteTimeout:           cfg.HTTPWriteTimeout,
		HTTPIdleTimeout:            cfg.HTTPIdleTimeout,
		ConfigHandler:              platform.ConfigHandler(cfg),
		ShardDialTimeout:           cfg.ShardDialTimeout,
		ShardMessageTimeout:        cfg.ShardMessageTimeout,
		MetricsAggregationInterval: cfg.MetricsAggregationInterval,
		EditionManager:             cfg.EditionManager(),
		PprofEnabled:               cfg.PprofEnabled,
		BackendMismatch:            backendMismatch,
		MessageBackend:             cfg.MessageBackend,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create load balancer")
	}
	if err := lb.Start(); err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to start load balancer")
	}

	// Start gRPC server for RealtimeService (SSE Subscribe + REST Publish)
	// Runs at orchestration level (single port, not per-shard).
	grpcAddr := fmt.Sprintf(":%d", cfg.GRPCPort)
	grpcListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", grpcAddr)
	if err != nil {
		structuredLogger.Fatal().Err(err).Str("addr", grpcAddr).Msg("Failed to create gRPC listener")
	}

	// Collect servers from shards for gRPC service
	servers := make([]*server.Server, len(shards))
	for i, shard := range shards {
		servers[i] = shard.Server()
	}

	grpcSrv := grpc.NewServer(
		tracing.StatsHandler(),
		grpc.ChainUnaryInterceptor(
			server.RecoveryUnaryInterceptor(structuredLogger),
			server.LoggingUnaryInterceptor(structuredLogger),
			server.MetricsUnaryInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			server.RecoveryStreamInterceptor(structuredLogger),
			server.LoggingStreamInterceptor(structuredLogger),
			server.MetricsStreamInterceptor(),
		),
	)
	serverv1.RegisterRealtimeServiceServer(grpcSrv, server.NewGRPCService(servers, structuredLogger))

	var grpcWg sync.WaitGroup
	grpcWg.Go(func() {
		defer logging.RecoverPanic(structuredLogger, "grpc.Serve", nil)
		structuredLogger.Info().Str("addr", grpcAddr).Msg("Starting gRPC server")
		if err := grpcSrv.Serve(grpcListener); err != nil {
			structuredLogger.Error().Err(err).Msg("gRPC server error")
			cancel() // FR-008: gRPC serve error triggers graceful shutdown
		}
	})

	// Wait for interrupt signal or context cancellation (from server error or license downgrade)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigCh:
	case <-ctx.Done():
	}

	structuredLogger.Info().Msg("Shutting down multi-core server")
	cancel() // Signal ctx.Done() to all goroutines using ctx

	// Shutdown gRPC server first (stops accepting new Subscribe/Publish)
	grpcSrv.GracefulStop()
	grpcWg.Wait()
	structuredLogger.Info().Msg("gRPC server stopped")

	// Shutdown LoadBalancer
	lb.Shutdown()

	// Shutdown license watcher
	if err := licenseWatcher.Close(); err != nil {
		structuredLogger.Warn().Err(err).Msg("License watcher close failed")
	}

	// Shutdown channel rules provider (owns its own internal context and gRPC connection)
	if err := rp.Close(); err != nil {
		structuredLogger.Warn().Err(err).Msg("Channel rules provider close failed")
	}

	// Shutdown shards
	for _, shard := range shards {
		if err := shard.Shutdown(); err != nil {
			structuredLogger.Error().Err(err).Int("shard_id", shard.ID).Msg("Error during shard shutdown")
		}
	}

	// Shutdown message backend (stops pool, producer, registry)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
	defer shutdownCancel()
	if err := msgBackend.Shutdown(shutdownCtx); err != nil {
		structuredLogger.Error().Err(err).Msg("Error shutting down message backend")
	}

	// Shutdown BroadcastBus
	broadcastBus.Shutdown()

	// Shutdown SystemMonitor
	systemMonitor.Shutdown()
	structuredLogger.Info().Msg("SystemMonitor shut down")

	structuredLogger.Info().Msg("Multi-core server gracefully shut down")
}
