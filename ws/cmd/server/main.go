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
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/provapi"
)

func main() {
	// Bootstrap logger for pre-config startup (zerolog without config dependency)
	bootLogger := logging.BootstrapLogger("ws-server")

	// Go 1.25+ runtime automatically sets GOMAXPROCS from container cgroup CPU limits
	maxProcs := runtime.GOMAXPROCS(0)
	bootLogger.Info().Int("gomaxprocs", maxProcs).Msg("GOMAXPROCS set by Go runtime (container-aware)")

	// Load configuration from .env file and environment variables
	cfg, err := platform.LoadServerConfig(nil) // Pass nil for now, structured logger created after
	if err != nil {
		bootLogger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// CLI flags use env var config as defaults (CLI overrides env overrides envDefault)
	var (
		debug          = flag.Bool("debug", false, "enable debug logging (overrides LOG_LEVEL)")
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
		ServiceName: "ws-server",
	})
	systemMonitor := metrics.GetSystemMonitor(structuredLogger, cfg.CPUEWMABeta)
	systemMonitor.StartMonitoring(cfg.MetricsInterval, cfg.CPUPollInterval)
	structuredLogger.Info().
		Dur("metrics_interval", cfg.MetricsInterval).
		Dur("cpu_poll_interval", cfg.CPUPollInterval).
		Msg("SystemMonitor started")

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

	// Initialize central BroadcastBus (configurable backend: Valkey or NATS)
	busLogger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: "ws-server",
	})

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

	broadcastBus, err := broadcast.NewBus(busCfg, busLogger)
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
			DefaultTenantID:          cfg.DefaultTenantID,
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
			ProducerTopicCacheTTL:             cfg.KafkaProducerTopicCacheTTL,
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
		registryLogger := logging.NewLogger(logging.LoggerConfig{
			Level:       logging.LogLevel(cfg.LogLevel),
			Format:      logging.LogFormat(cfg.LogFormat),
			ServiceName: "ws-server",
		})
		topicRegistry, regErr := provapi.NewStreamTopicRegistry(provapi.StreamTopicRegistryConfig{
			GRPCAddr:          cfg.ProvisioningGRPCAddr,
			Namespace:         topicNamespace,
			ReconnectDelay:    cfg.GRPCReconnectDelay,
			ReconnectMaxDelay: cfg.GRPCReconnectMaxDelay,
			MetricPrefix:      "ws",
			Logger:            registryLogger,
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := msgBackend.Start(ctx); err != nil {
		structuredLogger.Fatal().Err(err).Str("backend", cfg.MessageBackend).Msg("Failed to start message backend")
	}
	structuredLogger.Info().Str("backend", cfg.MessageBackend).Msg("Message backend started")

	// Initialize alerter from validated config
	alertCfg := cfg.AlertConfig()
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
			Logger:         logging.NewLogger(logging.LoggerConfig{Level: logging.LogLevel(cfg.LogLevel), Format: logging.LogFormat(cfg.LogFormat), ServiceName: "ws-server"}),
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
		Logger: logging.NewLogger(logging.LoggerConfig{Level: logging.LogLevel(cfg.LogLevel), Format: logging.LogFormat(cfg.LogFormat), ServiceName: "ws-server"}),
		// TCP/HTTP tuning for trading platform burst tolerance
		HTTPReadTimeout:            cfg.HTTPReadTimeout,
		HTTPWriteTimeout:           cfg.HTTPWriteTimeout,
		HTTPIdleTimeout:            cfg.HTTPIdleTimeout,
		ConfigHandler:              platform.ConfigHandler(cfg),
		ShardDialTimeout:           cfg.ShardDialTimeout,
		ShardMessageTimeout:        cfg.ShardMessageTimeout,
		MetricsAggregationInterval: cfg.MetricsAggregationInterval,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create load balancer")
	}
	if err := lb.Start(); err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to start load balancer")
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	structuredLogger.Info().Msg("Shutting down multi-core server")

	// Shutdown LoadBalancer
	lb.Shutdown()

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
