// Package main provides the entry point for the WebSocket server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	_ "go.uber.org/automaxprocs"

	"github.com/Toniq-Labs/odin-ws/internal/server/backend"
	"github.com/Toniq-Labs/odin-ws/internal/server/backend/directbackend"
	"github.com/Toniq-Labs/odin-ws/internal/server/backend/jetstreambackend"
	"github.com/Toniq-Labs/odin-ws/internal/server/backend/kafkabackend"
	"github.com/Toniq-Labs/odin-ws/internal/server/broadcast"
	"github.com/Toniq-Labs/odin-ws/internal/server/metrics"
	"github.com/Toniq-Labs/odin-ws/internal/server/orchestration"
	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	"github.com/Toniq-Labs/odin-ws/internal/shared/platform"
	"github.com/Toniq-Labs/odin-ws/internal/shared/provapi"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

func main() {
	var (
		debug     = flag.Bool("debug", false, "enable debug logging (overrides LOG_LEVEL)")
		numShards = flag.Int("shards", 3, "number of shards to run")
		basePort  = flag.Int("base-port", 3002, "base port for shards (e.g., 3002, 3003, ...)")
		lbAddr    = flag.String("lb-addr", ":3005", "address for the load balancer to listen on")
	)
	flag.Parse()

	// Create basic logger for startup
	logger := log.New(os.Stdout, "[WS-MULTI] ", log.LstdFlags)

	// automaxprocs automatically sets GOMAXPROCS based on container CPU limits
	maxProcs := runtime.GOMAXPROCS(0)
	logger.Printf("GOMAXPROCS: %d (via automaxprocs - rounds down to integer)", maxProcs)

	// Load configuration from .env file and environment variables
	cfg, err := platform.LoadServerConfig(nil) // Pass nil for now, structured logger created after
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Override debug mode if flag set
	if *debug {
		cfg.LogLevel = "debug"
		logger.Printf("Debug mode enabled via flag")
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
	systemMonitor := metrics.GetSystemMonitor(structuredLogger)
	systemMonitor.StartMonitoring(cfg.MetricsInterval, cfg.CPUPollInterval)
	logger.Printf("SystemMonitor started (metrics: %v, cpu poll: %v)", cfg.MetricsInterval, cfg.CPUPollInterval)

	// Calculate max connections per shard
	maxConnsPerShard := cfg.MaxConnections / *numShards
	if maxConnsPerShard == 0 {
		maxConnsPerShard = 1 // Ensure at least 1 connection per shard
	}
	logger.Printf("Total Max Connections: %d, Shards: %d, Max Connections per Shard: %d", cfg.MaxConnections, *numShards, maxConnsPerShard)

	// Initialize central BroadcastBus (configurable backend: Valkey or NATS)
	busLogger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: "ws-server",
	})

	// Build broadcast config based on BROADCAST_TYPE
	busCfg := broadcast.Config{
		Type:            cfg.BroadcastType,
		BufferSize:      1024,
		ShutdownTimeout: 5 * time.Second,
		Valkey: broadcast.ValkeyConfig{
			Addrs:       cfg.ValkeyAddrs,
			MasterName:  cfg.ValkeyMasterName,
			Password:    cfg.ValkeyPassword,
			DB:          cfg.ValkeyDB,
			Channel:     cfg.ValkeyChannel,
			TLSEnabled:  cfg.ValkeyTLSEnabled,
			TLSInsecure: cfg.ValkeyTLSInsecure,
			TLSCAPath:   cfg.ValkeyTLSCAPath,
		},
		NATS: broadcast.NATSConfig{
			URLs:        cfg.NATSURLs,
			ClusterMode: cfg.NATSClusterMode,
			Subject:     cfg.NATSSubject,
			Token:       cfg.NATSToken,
			User:        cfg.NATSUser,
			Password:    cfg.NATSPassword,
			TLSEnabled:  cfg.NATSTLSEnabled,
			TLSInsecure: cfg.NATSTLSInsecure,
			TLSCAPath:   cfg.NATSTLSCAPath,
		},
	}

	broadcastBus, err := broadcast.NewBus(busCfg, busLogger)
	if err != nil {
		logger.Fatalf("Failed to create BroadcastBus: %v", err)
	}

	logger.Printf("BroadcastBus initialized (type: %s)", cfg.BroadcastType)
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
			MaxKafkaMessagesPerSec:   cfg.MaxKafkaRate,
			MaxBroadcastsPerSec:      cfg.MaxBroadcastRate,
			CPUPauseThreshold:        cfg.CPUPauseThreshold,
			CPUPauseThresholdLower:   cfg.CPUPauseThresholdLower,
			CPURejectThreshold:       cfg.CPURejectThreshold,
			CPURejectThresholdLower:  cfg.CPURejectThresholdLower,
			LogLevel:                 cfg.LogLevel,
			LogFormat:                cfg.LogFormat,
			ProvisioningGRPCAddr:     cfg.ProvisioningGRPCAddr,
			GRPCReconnectDelay:       cfg.GRPCReconnectDelay,
			GRPCReconnectMaxDelay:    cfg.GRPCReconnectMaxDelay,
			TopicRefreshInterval:     cfg.TopicRefreshInterval,
			BroadcastBus:             broadcastBus,
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
			logger.Fatalf("Failed to create topic registry for JetStream: %v", regErr)
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
			MaxAge:          cfg.NATSJetStreamMaxAge,
			Namespace:       topicNamespace,
			BroadcastBus:    broadcastBus,
			Registry:        topicRegistry,
			RefreshInterval: cfg.TopicRefreshInterval,
			LogLevel:        cfg.LogLevel,
			LogFormat:       cfg.LogFormat,
		})
	default:
		logger.Fatalf("Unknown message backend: %q (valid: direct, kafka, nats)", cfg.MessageBackend)
	}
	if err != nil {
		logger.Fatalf("Failed to create message backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := msgBackend.Start(ctx); err != nil {
		logger.Fatalf("Failed to start message backend: %v", err)
	}
	logger.Printf("Message backend started (type: %s)", cfg.MessageBackend)

	// Create and start shards
	shards := make([]*orchestration.Shard, *numShards)
	for i := range *numShards {
		// Bind address: 0.0.0.0 to accept both IPv4/IPv6 connections
		shardBindAddr := fmt.Sprintf("0.0.0.0:%d", *basePort+i)
		// Advertise address: 127.0.0.1 (IPv4 loopback) for LoadBalancer to connect to
		shardAdvertiseAddr := fmt.Sprintf("127.0.0.1:%d", *basePort+i)

		shardConfig := types.ServerConfig{
			Addr:           shardBindAddr,
			Environment:    cfg.Environment,
			MaxConnections: maxConnsPerShard,

			MemoryLimit:            cfg.MemoryLimit,
			MaxKafkaMessagesPerSec: cfg.MaxKafkaRate,
			MaxBroadcastsPerSec:    cfg.MaxBroadcastRate,
			MaxGoroutines:          cfg.MaxGoroutines,

			// Connection rate limiting (DoS protection)
			ConnectionRateLimitEnabled: cfg.ConnectionRateLimitEnabled,
			ConnRateLimitIPBurst:       cfg.ConnRateLimitIPBurst,
			ConnRateLimitIPRate:        cfg.ConnRateLimitIPRate,
			ConnRateLimitGlobalBurst:   cfg.ConnRateLimitGlobalBurst,
			ConnRateLimitGlobalRate:    cfg.ConnRateLimitGlobalRate,

			CPURejectThreshold:      cfg.CPURejectThreshold,
			CPURejectThresholdLower: cfg.CPURejectThresholdLower,
			CPUPauseThreshold:       cfg.CPUPauseThreshold,
			CPUPauseThresholdLower:  cfg.CPUPauseThresholdLower,

			// Client buffer configuration
			ClientSendBufferSize: cfg.ClientSendBufferSize,

			// Slow client detection
			SlowClientMaxAttempts: cfg.SlowClientMaxAttempts,

			// TCP/HTTP tuning (trading platform burst tolerance)
			TCPListenBacklog: cfg.TCPListenBacklog,
			HTTPReadTimeout:  cfg.HTTPReadTimeout,
			HTTPWriteTimeout: cfg.HTTPWriteTimeout,
			HTTPIdleTimeout:  cfg.HTTPIdleTimeout,

			MetricsInterval: cfg.MetricsInterval,
			LogLevel:        types.LogLevel(cfg.LogLevel),
			LogFormat:       types.LogFormat(cfg.LogFormat),

			// WebSocket ping/pong timing
			PongWait:   cfg.PongWait,
			PingPeriod: cfg.PingPeriod,
			WriteWait:  cfg.WriteWait,
		}

		shard, err := orchestration.NewShard(orchestration.ShardConfig{
			ID:             i,
			Addr:           shardBindAddr,
			AdvertiseAddr:  shardAdvertiseAddr,
			ServerConfig:   shardConfig,
			BroadcastBus:   broadcastBus,
			MessageBackend: msgBackend,
			Logger:         logging.NewLogger(logging.LoggerConfig{Level: logging.LogLevel(cfg.LogLevel), Format: logging.LogFormat(cfg.LogFormat), ServiceName: "ws-server"}),
			MaxConnections: maxConnsPerShard,
		})
		if err != nil {
			logger.Fatalf("Failed to create shard %d: %v", i, err)
		}

		if err := shard.Start(); err != nil {
			logger.Fatalf("Failed to start shard %d: %v", i, err)
		}
		shards[i] = shard
	}

	// Initialize and start LoadBalancer
	lb, err := orchestration.NewLoadBalancer(orchestration.LoadBalancerConfig{
		Addr:   *lbAddr,
		Shards: shards,
		Logger: logging.NewLogger(logging.LoggerConfig{Level: logging.LogLevel(cfg.LogLevel), Format: logging.LogFormat(cfg.LogFormat), ServiceName: "ws-server"}),
		// TCP/HTTP tuning for trading platform burst tolerance
		HTTPReadTimeout:  cfg.HTTPReadTimeout,
		HTTPWriteTimeout: cfg.HTTPWriteTimeout,
		HTTPIdleTimeout:  cfg.HTTPIdleTimeout,
	})
	if err != nil {
		logger.Fatalf("Failed to create load balancer: %v", err)
	}
	if err := lb.Start(); err != nil {
		logger.Fatalf("Failed to start load balancer: %v", err)
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Println("Shutting down multi-core server...")

	// Shutdown LoadBalancer
	lb.Shutdown()

	// Shutdown shards
	for _, shard := range shards {
		if err := shard.Shutdown(); err != nil {
			logger.Printf("Error during shard %d shutdown: %v", shard.ID, err)
		}
	}

	// Shutdown message backend (stops pool, producer, registry)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := msgBackend.Shutdown(shutdownCtx); err != nil {
		logger.Printf("Error shutting down message backend: %v", err)
	}

	// Shutdown BroadcastBus
	broadcastBus.Shutdown()

	// Shutdown SystemMonitor
	systemMonitor.Shutdown()
	logger.Println("SystemMonitor shut down")

	logger.Println("Multi-core server gracefully shut down.")
}
