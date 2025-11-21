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

	"github.com/adred-codev/ws_poc/internal/multi"
	"github.com/adred-codev/ws_poc/internal/shared/limits"
	"github.com/adred-codev/ws_poc/internal/shared/monitoring"
	"github.com/adred-codev/ws_poc/internal/shared/platform"
	"github.com/adred-codev/ws_poc/internal/shared/types"
	_ "go.uber.org/automaxprocs"
)

// Helper function to split broker string
func splitBrokers(brokers string) []string {
	result := []string{}
	for _, b := range strings.Split(brokers, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func main() {
	var (
		debug      = flag.Bool("debug", false, "enable debug logging (overrides LOG_LEVEL)")
		numShards  = flag.Int("shards", 3, "number of shards to run")
		basePort   = flag.Int("base-port", 3002, "base port for shards (e.g., 3002, 3003, ...)")
		lbAddr     = flag.String("lb-addr", ":3005", "address for the load balancer to listen on")
	)
	flag.Parse()

	// Create basic logger for startup
	logger := log.New(os.Stdout, "[WS-MULTI] ", log.LstdFlags)

	// automaxprocs automatically sets GOMAXPROCS based on container CPU limits
	maxProcs := runtime.GOMAXPROCS(0)
	logger.Printf("GOMAXPROCS: %d (via automaxprocs - rounds down to integer)", maxProcs)

	// Load configuration from .env file and environment variables
	cfg, err := platform.LoadConfig(nil) // Pass nil for now, structured logger created after
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
	structuredLogger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:  types.LogLevel(cfg.LogLevel),
		Format: types.LogFormat(cfg.LogFormat),
	})
	systemMonitor := monitoring.GetSystemMonitor(structuredLogger)
	systemMonitor.StartMonitoring(cfg.MetricsInterval)
	logger.Printf("SystemMonitor singleton started (centralizes CPU/memory measurement)")

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

	// Initialize central BroadcastBus (Redis Pub/Sub for multi-instance communication)
	busLogger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:  types.LogLevel(cfg.LogLevel),
		Format: types.LogFormat(cfg.LogFormat),
	})

	broadcastBus, err := multi.NewBroadcastBus(multi.BroadcastBusConfig{
		SentinelAddrs: cfg.RedisSentinelAddrs,
		MasterName:    cfg.RedisMasterName,
		Password:      cfg.RedisPassword,
		DB:            cfg.RedisDB,
		Channel:       cfg.RedisChannel,
		BufferSize:    1024,
		Logger:        busLogger,
	})
	if err != nil {
		logger.Fatalf("Failed to create BroadcastBus: %v", err)
	}

	logger.Printf("BroadcastBus initialized (Redis mode: %d sentinel addrs)", len(cfg.RedisSentinelAddrs))
	broadcastBus.Run()

	// Create shared Kafka consumer pool (replaces per-shard consumers)
	// This eliminates 9x message duplication (3 consumers × 3 broadcasts)
	var kafkaPool *multi.KafkaConsumerPool
	if len(kafkaBrokers) > 0 {
		// Create resource guard for CPU brake (shared across pool)
		poolLogger := monitoring.NewLogger(monitoring.LoggerConfig{
			Level:  types.LogLevel(cfg.LogLevel),
			Format: types.LogFormat(cfg.LogFormat),
		})
		resourceGuard := limits.NewResourceGuard(types.ServerConfig{
			MaxKafkaMessagesPerSec: cfg.MaxKafkaRate,
			MaxBroadcastsPerSec:    cfg.MaxBroadcastRate,
			CPUPauseThreshold:      cfg.CPUPauseThreshold,
			CPURejectThreshold:     cfg.CPURejectThreshold,
		}, poolLogger, new(int64)) // Pass dummy connection counter for pool

		var err error
		kafkaPool, err = multi.NewKafkaConsumerPool(multi.KafkaPoolConfig{
			Brokers:       kafkaBrokers,
			ConsumerGroup: cfg.ConsumerGroup, // Single group for all shards
			BroadcastBus:  broadcastBus,
			ResourceGuard: resourceGuard,
			Logger:        poolLogger,
		})
		if err != nil {
			logger.Fatalf("Failed to create Kafka consumer pool: %v", err)
		}

		if err := kafkaPool.Start(); err != nil {
			logger.Fatalf("Failed to start Kafka consumer pool: %v", err)
		}

		logger.Printf("Shared Kafka consumer pool started (replaces %d per-shard consumers)", *numShards)
	}

	// Create and start shards
	shards := make([]*multi.Shard, *numShards)
	for i := 0; i < *numShards; i++ {
		// Bind address: 0.0.0.0 to accept both IPv4/IPv6 connections
		shardBindAddr := fmt.Sprintf("0.0.0.0:%d", *basePort+i)
		// Advertise address: 127.0.0.1 (IPv4 loopback) for LoadBalancer to connect to
		shardAdvertiseAddr := fmt.Sprintf("127.0.0.1:%d", *basePort+i)

		shardConfig := types.ServerConfig{
			Addr:           shardBindAddr,
			KafkaBrokers:   kafkaBrokers,
			ConsumerGroup:  cfg.ConsumerGroup, // Base consumer group name
			MaxConnections: maxConnsPerShard,  // Shard-specific max connections

			CPULimit:               cfg.CPULimit,
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

			CPURejectThreshold:     cfg.CPURejectThreshold,
			CPUPauseThreshold:      cfg.CPUPauseThreshold,
			MetricsInterval:        cfg.MetricsInterval,
			LogLevel:               types.LogLevel(cfg.LogLevel),
			LogFormat:              types.LogFormat(cfg.LogFormat),
		}

		// Get shared consumer for replay (if pool exists)
		var sharedConsumer interface{}
		if kafkaPool != nil {
			sharedConsumer = kafkaPool.GetConsumer()
		}

		shard, err := multi.NewShard(multi.ShardConfig{
			ID:                   i,
			Addr:                 shardBindAddr,      // Bind address for listening
			AdvertiseAddr:        shardAdvertiseAddr, // Address for LoadBalancer connections
			ServerConfig:         shardConfig,
			BroadcastBus:         broadcastBus,              // Pass reference to bus, shard will subscribe internally
			DisableKafkaConsumer: len(kafkaBrokers) > 0,     // Disable per-shard consumer when using shared pool
			SharedKafkaConsumer:  sharedConsumer,            // Pass shared consumer for message replay
			Logger:               monitoring.NewLogger(monitoring.LoggerConfig{Level: types.LogLevel(cfg.LogLevel), Format: types.LogFormat(cfg.LogFormat)}),
			MaxConnections:       maxConnsPerShard,
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
	lb, err := multi.NewLoadBalancer(multi.LoadBalancerConfig{
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

	// Shutdown BroadcastBus
	broadcastBus.Shutdown()

	// Shutdown SystemMonitor
	systemMonitor.Shutdown()
	logger.Println("SystemMonitor shut down")

	logger.Println("Multi-core server gracefully shut down.")
}
