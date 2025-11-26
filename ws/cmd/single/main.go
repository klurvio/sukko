package main

import (
	"fmt"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/adred-codev/ws_poc/internal/shared"
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
		debug = flag.Bool("debug", false, "enable debug logging (overrides LOG_LEVEL)")
	)
	flag.Parse()

	// Create basic logger for startup
	logger := log.New(os.Stdout, "[WS] ", log.LstdFlags)

	// automaxprocs automatically sets GOMAXPROCS based on container CPU limits
	// IMPORTANT: automaxprocs rounds DOWN (e.g., 1.5 cores → GOMAXPROCS=1)
	// This is correct for Go scheduler, but we use actual CPU limit for worker sizing
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

	// Initialize SystemMonitor singleton FIRST (before creating ResourceGuard)
	// This ensures all ResourceGuards share the same system metrics source
	structuredLogger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:  types.LogLevel(cfg.LogLevel),
		Format: types.LogFormat(cfg.LogFormat),
	})
	systemMonitor := monitoring.GetSystemMonitor(structuredLogger)
	systemMonitor.StartMonitoring(cfg.MetricsInterval)
	logger.Printf("SystemMonitor singleton started (centralizes CPU/memory measurement)")

	// Create and configure server with loaded configuration
	// Parse Kafka brokers from comma-separated string
	kafkaBrokers := []string{}
	if cfg.KafkaBrokers != "" {
		kafkaBrokers = splitBrokers(cfg.KafkaBrokers)
	}

	serverConfig := types.ServerConfig{
		Addr:           cfg.Addr,
		KafkaBrokers:   kafkaBrokers,
		ConsumerGroup:  cfg.ConsumerGroup,
		MaxConnections: cfg.MaxConnections,

		// Static resource limits (explicit from config)
		CPULimit:    cfg.CPULimit,
		MemoryLimit: cfg.MemoryLimit,

		// Rate limiting (CRITICAL - prevents overload)
		MaxKafkaMessagesPerSec: cfg.MaxKafkaRate,
		MaxBroadcastsPerSec:    cfg.MaxBroadcastRate,
		MaxGoroutines:          cfg.MaxGoroutines,

		// Connection rate limiting (DoS protection)
		ConnectionRateLimitEnabled: cfg.ConnectionRateLimitEnabled,
		ConnRateLimitIPBurst:       cfg.ConnRateLimitIPBurst,
		ConnRateLimitIPRate:        cfg.ConnRateLimitIPRate,
		ConnRateLimitGlobalBurst:   cfg.ConnRateLimitGlobalBurst,
		ConnRateLimitGlobalRate:    cfg.ConnRateLimitGlobalRate,

		// Safety thresholds (emergency brakes) with hysteresis
		CPURejectThreshold:      cfg.CPURejectThreshold,
		CPURejectThresholdLower: cfg.CPURejectThresholdLower,
		CPUPauseThreshold:       cfg.CPUPauseThreshold,
		CPUPauseThresholdLower:  cfg.CPUPauseThresholdLower,

		// Monitoring intervals
		MetricsInterval: cfg.MetricsInterval,

		// Logging configuration
		LogLevel:  types.LogLevel(cfg.LogLevel),
		LogFormat: types.LogFormat(cfg.LogFormat),
	}

	var server *shared.Server

	// Create broadcast function that will be called for each Kafka message
	// For the single-core server, this directly calls the server's broadcast method.
	broadcastFunc := func(tokenID string, eventType string, message []byte) {
		// Format subject as: "odin.token.{tokenID}.{eventType}"
		subject := fmt.Sprintf("odin.token.%s.%s", tokenID, eventType)
		server.Broadcast(subject, message)
	}

	server, err = shared.NewServer(serverConfig, broadcastFunc)
	if err != nil {
		logger.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	if err := server.Start(); err != nil {
		logger.Fatalf("Failed to start server: %v", err)
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Println("Shutting down server...")
	if err := server.Shutdown(); err != nil {
		logger.Printf("Error during shutdown: %v", err)
	}

	// Shutdown SystemMonitor
	systemMonitor.Shutdown()
	logger.Println("SystemMonitor shut down")
}
