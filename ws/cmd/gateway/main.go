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

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/gateway"
)

// Version information (set by build flags)
var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)

func main() {
	// Initialize logger
	logger := initLogger()

	logger.Info().
		Str("version", Version).
		Str("commit", CommitHash).
		Str("build_time", BuildTime).
		Msg("Starting ws-gateway")

	// Load configuration
	config, err := gateway.LoadConfig(&logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	config.LogConfig(logger)

	// Create gateway
	gw := gateway.New(config, logger)

	// Create HTTP server
	server := gw.NewServer()

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
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

// initLogger creates a zerolog logger based on environment.
func initLogger() zerolog.Logger {
	// Check environment for log format
	logFormat := os.Getenv("LOG_FORMAT")
	if logFormat == "" {
		logFormat = "json"
	}

	// Check environment for log level
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	// Set log level
	var level zerolog.Level
	switch logLevel {
	case "debug":
		level = zerolog.DebugLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	default:
		level = zerolog.InfoLevel
	}

	// Create logger
	var logger zerolog.Logger
	if logFormat == "pretty" || logFormat == "text" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
			Level(level).
			With().
			Timestamp().
			Str("service", "ws-gateway").
			Logger()
	} else {
		logger = zerolog.New(os.Stdout).
			Level(level).
			With().
			Timestamp().
			Str("service", "ws-gateway").
			Logger()
	}

	return logger
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
