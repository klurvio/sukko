// Package main is the entrypoint for the ws-gateway service.
// The gateway authenticates WebSocket connections and proxies them
// to the ws-server backend with permission-based channel filtering.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Toniq-Labs/odin-ws/internal/gateway"
	"github.com/Toniq-Labs/odin-ws/internal/platform"
	"github.com/Toniq-Labs/odin-ws/pkg/logging"
)

// Version information (set by build flags)
var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)

func main() {
	// Initialize logger using shared logging package
	logger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(os.Getenv("LOG_LEVEL")),
		Format:      logging.LogFormat(os.Getenv("LOG_FORMAT")),
		ServiceName: "ws-gateway",
	})

	logger.Info().
		Str("version", Version).
		Str("commit", CommitHash).
		Str("build_time", BuildTime).
		Msg("Starting ws-gateway")

	// Load configuration
	config, err := platform.LoadGatewayConfig(&logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	config.LogConfig(logger)

	// Create gateway
	gw, err := gateway.New(config, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create gateway")
	}
	defer func() {
		if err := gw.Close(); err != nil {
			logger.Error().Err(err).Msg("Gateway cleanup error")
		}
	}()

	// Create HTTP server
	server := gw.NewServer()

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		defer logging.RecoverPanic(logger, "http.ListenAndServe", nil)
		logger.Info().
			Int("port", config.Port).
			Msg("Gateway listening")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Server failed")
		}
	}()

	// Wait for shutdown signal
	sig := <-shutdown
	logger.Info().
		Str("signal", sig.String()).
		Msg("Shutdown signal received")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Server shutdown error")
	}

	logger.Info().Msg("Gateway stopped")
}

func init() {
	// Print banner
	fmt.Print(`
 _      ______       _____       __
| | /| / / __/______/ ___/___ _ / /_ ___  _    __ ___ _ __ __
| |/ |/ /\ \ /___// (_ // _ '// __// -_)| |/|/ // _ '// // /
|__/|__/___/      \___/ \_,_/ \__/ \__/ |__,__/ \_,_/ \_, /
                                                     /___/
`)
}
