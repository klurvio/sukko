// Package main is the entrypoint for the ws-gateway service.
// The gateway authenticates WebSocket connections and proxies them
// to the ws-server backend with permission-based channel filtering.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/klurvio/sukko/internal/gateway"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/profiling"
	"github.com/klurvio/sukko/internal/shared/tracing"
)

// Version information (set by build flags)
var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)

const serviceName = "ws-gateway"

func main() {
	// Bootstrap logger for pre-config startup (zerolog without config dependency)
	bootLogger := logging.BootstrapLogger(serviceName)

	bootLogger.Info().
		Str("version", Version).
		Str("commit", CommitHash).
		Str("build_time", BuildTime).
		Msg("Starting ws-gateway")
	bootLogger.Info().Int("gomaxprocs", runtime.GOMAXPROCS(0)).Msg("GOMAXPROCS set by Go runtime (container-aware)")

	// Load configuration
	config, err := platform.LoadGatewayConfig(bootLogger)
	if err != nil {
		bootLogger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// CLI flags use env var config as defaults (CLI overrides env overrides envDefault)
	var (
		debug          = flag.Bool("debug", config.LogLevel == "debug", "enable debug logging (overrides LOG_LEVEL)")
		validateConfig = flag.Bool("validate-config", false, "validate configuration and exit")
	)
	flag.Parse()

	// Override debug mode if flag set
	if *debug {
		config.LogLevel = "debug"
	}

	// Create structured logger from config (after flags parsed)
	logger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(config.LogLevel),
		Format:      logging.LogFormat(config.LogFormat),
		ServiceName: serviceName,
	})

	config.LogConfig(logger)

	// Log edition
	logger.Info().
		Str("edition", config.EditionManager().Edition().String()).
		Str("org", config.EditionManager().Org()).
		Msg("Sukko edition resolved")

	// --validate-config: validate and exit
	if *validateConfig {
		logger.Info().Msg("Configuration is valid")
		os.Exit(0)
	}

	// Initialize tracing (cold-path only, noop when disabled)
	tracingShutdown, err := tracing.Init(context.Background(), tracing.Config{
		Enabled:      config.OTELTracingEnabled,
		ExporterType: config.OTELExporterType,
		Endpoint:     config.OTELExporterEndpoint,
		ServiceName:  serviceName,
		Environment:  config.Environment,
	}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize tracing")
	}
	defer func() { _ = tracingShutdown(context.Background()) }()

	// Initialize Pyroscope continuous profiling (noop when disabled)
	// pprof endpoints are registered on the gateway's HTTP mux via the gateway
	pyroscopeStop, err := profiling.InitPyroscope(profiling.PyroscopeConfig{
		Enabled:     config.PyroscopeEnabled,
		Addr:        config.PyroscopeAddr,
		ServiceName: serviceName,
		Environment: config.Environment,
	}, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize Pyroscope")
	}
	defer pyroscopeStop()

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

	// Create gRPC client to ws-server (SSE + REST Publish)
	serverClient, err := gateway.NewServerClient(config.ServerGRPCAddr, logger)
	if err != nil {
		logger.Fatal().Err(err).Str("addr", config.ServerGRPCAddr).Msg("Failed to create ws-server gRPC client")
	}
	defer func() {
		if err := serverClient.Close(); err != nil {
			logger.Error().Err(err).Msg("Server client cleanup error")
		}
	}()
	gw.SetServerClient(serverClient)

	// Create gRPC client to push service (Enterprise only — Constitution XIII)
	if config.EditionManager().HasFeature(license.WebPushTransport) {
		pushClient, err := gateway.NewPushClient(config.PushGRPCAddr, logger)
		if err != nil {
			logger.Fatal().Err(err).Str("addr", config.PushGRPCAddr).Msg("Failed to create push service gRPC client")
		}
		defer func() {
			if err := pushClient.Close(); err != nil {
				logger.Error().Err(err).Msg("Push client cleanup error")
			}
		}()
		gw.SetPushClient(pushClient)
		logger.Info().Str("addr", config.PushGRPCAddr).Msg("Push service client connected")
	} else {
		logger.Info().Msg("Push service disabled (requires Enterprise edition)")
	}

	// Create publish rate limiter (background cleanup goroutine)
	var limiterWg sync.WaitGroup
	limiterCtx, limiterCancel := context.WithCancel(context.Background())
	defer limiterCancel()
	publishLimiter := gateway.NewPublishRateLimiter(limiterCtx, &limiterWg, config.PublishRateLimit, config.PublishBurst, logger)
	gw.SetPublishRateLimiter(publishLimiter)

	// Create HTTP server
	server := gw.NewServer()

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Context for goroutine lifecycle — cancel() signals server error to main
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in goroutine
	var wg sync.WaitGroup
	wg.Go(func() {
		defer logging.RecoverPanic(logger, "http.ListenAndServe", nil)
		logger.Info().
			Int("port", config.Port).
			Msg("Gateway listening")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("Server failed")
			cancel()
		}
	})

	// Wait for shutdown signal or server error
	select {
	case sig := <-shutdown:
		logger.Info().
			Str("signal", sig.String()).
			Msg("Shutdown signal received")
	case <-ctx.Done():
		logger.Info().Msg("Shutdown triggered by server error")
	}

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Server shutdown error")
	}

	wg.Wait()
	logger.Info().Msg("Gateway stopped")
}

func init() {
	// Print banner
	//nolint:forbidigo // startup banner is visual stdout output printed before logger init, not operational logging
	fmt.Print(`
 _      ______       _____       __
| | /| / / __/______/ ___/___ _ / /_ ___  _    __ ___ _ __ __
| |/ |/ /\ \ /___// (_ // _ '// __// -_)| |/|/ // _ '// // /
|__/|__/___/      \___/ \_,_/ \__/ \__/ |__,__/ \_,_/ \_, /
                                                     /___/
`)
}
