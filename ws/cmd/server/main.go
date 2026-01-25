package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	_ "go.uber.org/automaxprocs"

	"github.com/Toniq-Labs/odin-ws/internal/broadcast"
	"github.com/Toniq-Labs/odin-ws/internal/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/limits"
	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
	"github.com/Toniq-Labs/odin-ws/internal/orchestration"
	"github.com/Toniq-Labs/odin-ws/internal/platform"
	"github.com/Toniq-Labs/odin-ws/internal/types"
)

// Helper function to split broker string
func splitBrokers(brokers string) []string {
	result := []string{}
	for b := range strings.SplitSeq(brokers, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

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

	// Resolve effective topic namespace for Kafka
	// KafkaTopicNamespace overrides Environment for topic naming
	topicNamespace := cfg.KafkaTopicNamespace
	if topicNamespace == "" {
		topicNamespace = kafka.NormalizeEnv(cfg.Environment)
	} else {
		topicNamespace = kafka.NormalizeEnv(topicNamespace)
	}
	logger.Printf("Topic namespace: %s (environment: %s)", topicNamespace, cfg.Environment)

	// Initialize SystemMonitor singleton FIRST (before creating any ResourceGuards)
	// This ensures all ResourceGuards share the same system metrics source
	structuredLogger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:  types.LogLevel(cfg.LogLevel),
		Format: types.LogFormat(cfg.LogFormat),
	})
	systemMonitor := monitoring.GetSystemMonitor(structuredLogger)
	systemMonitor.StartMonitoring(cfg.MetricsInterval, cfg.CPUPollInterval)
	logger.Printf("SystemMonitor started (metrics: %v, cpu poll: %v)", cfg.MetricsInterval, cfg.CPUPollInterval)

	// Create and configure server with loaded configuration
	kafkaBrokers := []string{}
	if cfg.KafkaBrokers != "" {
		kafkaBrokers = splitBrokers(cfg.KafkaBrokers)
	}

	// Calculate max connections per shard
	maxConnsPerShard := cfg.MaxConnections / *numShards
	if maxConnsPerShard == 0 {
		maxConnsPerShard = 1 // Ensure at least 1 connection per shard
	}
	logger.Printf("Total Max Connections: %d, Shards: %d, Max Connections per Shard: %d", cfg.MaxConnections, *numShards, maxConnsPerShard)

	// Initialize central BroadcastBus (configurable backend: Valkey or NATS)
	busLogger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:  types.LogLevel(cfg.LogLevel),
		Format: types.LogFormat(cfg.LogFormat),
	})

	// Build broadcast config based on BROADCAST_TYPE
	busCfg := broadcast.Config{
		Type:            cfg.BroadcastType,
		BufferSize:      1024,
		ShutdownTimeout: 5 * time.Second,
		Valkey: broadcast.ValkeyConfig{
			Addrs:      cfg.ValkeyAddrs,
			MasterName: cfg.ValkeyMasterName,
			Password:   cfg.ValkeyPassword,
			DB:         cfg.ValkeyDB,
			Channel:    cfg.ValkeyChannel,
		},
		NATS: broadcast.NATSConfig{
			URLs:        cfg.NATSURLs,
			ClusterMode: cfg.NATSClusterMode,
			Subject:     cfg.NATSSubject,
			Token:       cfg.NATSToken,
			User:        cfg.NATSUser,
			Password:    cfg.NATSPassword,
		},
	}

	broadcastBus, err := broadcast.NewBus(busCfg, busLogger)
	if err != nil {
		logger.Fatalf("Failed to create BroadcastBus: %v", err)
	}

	logger.Printf("BroadcastBus initialized (type: %s)", cfg.BroadcastType)
	broadcastBus.Run()

	// Create shared Kafka consumer pool (replaces per-shard consumers)
	// This eliminates 9x message duplication (3 consumers × 3 broadcasts)
	var kafkaPool *orchestration.KafkaConsumerPool
	var kafkaProducer *kafka.Producer
	if len(kafkaBrokers) > 0 {
		// Create resource guard for CPU brake (shared across pool)
		poolLogger := monitoring.NewLogger(monitoring.LoggerConfig{
			Level:  types.LogLevel(cfg.LogLevel),
			Format: types.LogFormat(cfg.LogFormat),
		})
		resourceGuard := limits.NewResourceGuard(types.ServerConfig{
			MaxKafkaMessagesPerSec:  cfg.MaxKafkaRate,
			MaxBroadcastsPerSec:     cfg.MaxBroadcastRate,
			CPUPauseThreshold:       cfg.CPUPauseThreshold,
			CPUPauseThresholdLower:  cfg.CPUPauseThresholdLower,
			CPURejectThreshold:      cfg.CPURejectThreshold,
			CPURejectThresholdLower: cfg.CPURejectThresholdLower,
		}, poolLogger, new(int64)) // Pass dummy connection counter for pool

		// Build SASL config if enabled
		var saslConfig *kafka.SASLConfig
		if cfg.KafkaSASLEnabled {
			saslConfig = &kafka.SASLConfig{
				Mechanism: cfg.KafkaSASLMechanism,
				Username:  cfg.KafkaSASLUsername,
				Password:  cfg.KafkaSASLPassword,
			}
		}

		// Build TLS config if enabled
		var tlsConfig *kafka.TLSConfig
		if cfg.KafkaTLSEnabled {
			tlsConfig = &kafka.TLSConfig{
				Enabled:            true,
				InsecureSkipVerify: cfg.KafkaTLSInsecure,
				CAPath:             cfg.KafkaTLSCAPath,
			}
		}

		var err error
		kafkaPool, err = orchestration.NewKafkaConsumerPool(orchestration.KafkaPoolConfig{
			Brokers:       kafkaBrokers,
			ConsumerGroup: cfg.ConsumerGroup, // Single group for all shards
			Environment:   topicNamespace,    // Resolved topic namespace for topic naming
			BroadcastBus:  broadcastBus,
			ResourceGuard: resourceGuard,
			Logger:        poolLogger,
			SASL:          saslConfig,
			TLS:           tlsConfig,
		})
		if err != nil {
			logger.Fatalf("Failed to create Kafka consumer pool: %v", err)
		}

		if err := kafkaPool.Start(); err != nil {
			logger.Fatalf("Failed to start Kafka consumer pool: %v", err)
		}

		logger.Printf("Shared Kafka consumer pool started (replaces %d per-shard consumers)", *numShards)

		// Create shared Kafka producer for client message publishing
		// Clients can publish messages to Kafka via the "publish" message type
		producerLogger := monitoring.NewLogger(monitoring.LoggerConfig{
			Level:  types.LogLevel(cfg.LogLevel),
			Format: types.LogFormat(cfg.LogFormat),
		})

		kafkaProducer, err = kafka.NewProducer(kafka.ProducerConfig{
			Brokers:        kafkaBrokers,
			TopicNamespace: topicNamespace,
			// ClientID auto-generated with hostname (odin-ws-producer-{hostname})
			Logger: &producerLogger,
			SASL:   saslConfig,
			TLS:    tlsConfig,
		})
		if err != nil {
			logger.Fatalf("Failed to create Kafka producer: %v", err)
		}

		logger.Printf("Kafka producer initialized (topic: %s)", kafkaProducer.Topic())
	}

	// Create and start shards
	shards := make([]*orchestration.Shard, *numShards)
	for i := range *numShards {
		// Bind address: 0.0.0.0 to accept both IPv4/IPv6 connections
		shardBindAddr := fmt.Sprintf("0.0.0.0:%d", *basePort+i)
		// Advertise address: 127.0.0.1 (IPv4 loopback) for LoadBalancer to connect to
		shardAdvertiseAddr := fmt.Sprintf("127.0.0.1:%d", *basePort+i)

		shardConfig := types.ServerConfig{
			Addr:           shardBindAddr,
			KafkaBrokers:   kafkaBrokers,
			ConsumerGroup:  cfg.ConsumerGroup, // Base consumer group name
			Environment:    cfg.Environment,   // Environment for logging (topic naming via shared pool)
			MaxConnections: maxConnsPerShard,  // Shard-specific max connections

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

			MetricsInterval: cfg.MetricsInterval,
			LogLevel:        types.LogLevel(cfg.LogLevel),
			LogFormat:       types.LogFormat(cfg.LogFormat),
		}

		// Get shared consumer for replay (if pool exists)
		var sharedConsumer any
		if kafkaPool != nil {
			sharedConsumer = kafkaPool.GetConsumer()
		}

		shard, err := orchestration.NewShard(orchestration.ShardConfig{
			ID:                  i,
			Addr:                shardBindAddr,      // Bind address for listening
			AdvertiseAddr:       shardAdvertiseAddr, // Address for LoadBalancer connections
			ServerConfig:        shardConfig,
			BroadcastBus:        broadcastBus,   // Pass reference to bus, shard will subscribe internally
			SharedKafkaConsumer: sharedConsumer, // Shared consumer for metrics (managed by pool)
			KafkaProducer:       kafkaProducer,  // Shared producer for client publishing (optional)
			Logger:              monitoring.NewLogger(monitoring.LoggerConfig{Level: types.LogLevel(cfg.LogLevel), Format: types.LogFormat(cfg.LogFormat)}),
			MaxConnections:      maxConnsPerShard,
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
		Logger: monitoring.NewLogger(monitoring.LoggerConfig{Level: types.LogLevel(cfg.LogLevel), Format: types.LogFormat(cfg.LogFormat)}),
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

	// Shutdown Kafka pool (before BroadcastBus)
	if kafkaPool != nil {
		if err := kafkaPool.Stop(); err != nil {
			logger.Printf("Error stopping Kafka pool: %v", err)
		}
	}

	// Shutdown Kafka producer
	if kafkaProducer != nil {
		if err := kafkaProducer.Close(); err != nil {
			logger.Printf("Error closing Kafka producer: %v", err)
		}
	}

	// Shutdown BroadcastBus
	broadcastBus.Shutdown()

	// Shutdown SystemMonitor
	systemMonitor.Shutdown()
	logger.Println("SystemMonitor shut down")

	logger.Println("Multi-core server gracefully shut down.")
}
