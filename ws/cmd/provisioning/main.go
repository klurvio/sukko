// Provisioning service entry point for tenant management.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"google.golang.org/grpc"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/api"
	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/grpcserver"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/profiling"
	"github.com/klurvio/sukko/internal/shared/tracing"
)

const serviceName = "provisioning"

func main() {
	// Bootstrap logger for pre-config startup (zerolog without config dependency)
	bootLogger := logging.BootstrapLogger(serviceName)

	// Load configuration first (env vars + envDefaults)
	cfg, err := platform.LoadProvisioningConfig(bootLogger)
	if err != nil {
		bootLogger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// CLI flags use env var config as defaults (CLI overrides env overrides envDefault)
	var (
		debug          = flag.Bool("debug", cfg.LogLevel == "debug", "enable debug logging (overrides LOG_LEVEL)")
		validateConfig = flag.Bool("validate-config", false, "validate configuration and exit")
	)
	flag.Parse()

	// Override debug mode if flag set
	if *debug {
		cfg.LogLevel = "debug"
		bootLogger.Info().Msg("Debug mode enabled via flag")
	}

	// Print configuration
	cfg.Print()

	// --validate-config: validate and exit
	if *validateConfig {
		bootLogger.Info().Msg("Configuration is valid")
		os.Exit(0)
	}

	// Resolve effective topic namespace for Kafka
	topicNamespace := kafka.ResolveNamespace(cfg.KafkaTopicNamespaceOverride, cfg.Environment)
	bootLogger.Info().Str("namespace", topicNamespace).Str("environment", cfg.Environment).Msg("Topic namespace resolved")

	// Initialize structured logger
	structuredLogger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: serviceName,
	})

	structuredLogger.Info().Int("gomaxprocs", runtime.GOMAXPROCS(0)).Msg("GOMAXPROCS set by Go runtime (container-aware)")

	// Log edition
	structuredLogger.Info().
		Str("edition", cfg.EditionManager().Edition().String()).
		Str("org", cfg.EditionManager().Org()).
		Msg("Sukko edition resolved")

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
	// pprof endpoints are registered on the HTTP server mux via the router
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

	// Create event bus for gRPC streaming notifications
	bus := eventbus.New(structuredLogger)

	// Open database using factory (supports both SQLite and PostgreSQL)
	db, err := repository.OpenDatabase(repository.DatabaseConfig{
		Driver:          cfg.DatabaseDriver,
		URL:             cfg.DatabaseURL,
		Path:            cfg.DatabasePath,
		AutoMigrate:     cfg.AutoMigrate,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		Logger:          structuredLogger,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to open database")
	}
	defer func() { _ = db.Close() }() // Close error non-actionable during shutdown
	structuredLogger.Info().Str("driver", cfg.DatabaseDriver).Msg("Database opened")

	// Initialize repositories
	tenantRepo := repository.NewPostgresTenantRepository(db)
	keyRepo := repository.NewPostgresKeyRepository(db)
	apiKeyRepo := repository.NewPostgresAPIKeyStore(db)
	routingRulesRepo := repository.NewPostgresRoutingRulesRepository(db)
	quotaRepo := repository.NewPostgresQuotaRepository(db)
	auditRepo := repository.NewPostgresAuditRepository(db)
	channelRulesRepo := repository.NewPostgresChannelRulesRepository(db)

	// Kafka admin disabled — topic creation is handled by ws-server's KafkaBackend
	kafkaAdmin := provisioning.NewNoopKafkaAdmin()
	structuredLogger.Info().Msg("Kafka admin disabled (topic creation moved to ws-server)")

	svc, err := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:          tenantRepo,
		KeyStore:             keyRepo,
		APIKeyStore:          apiKeyRepo,
		RoutingRulesStore:    routingRulesRepo,
		QuotaStore:           quotaRepo,
		AuditStore:           auditRepo,
		ChannelRulesStore:    channelRulesRepo,
		KafkaAdmin:           kafkaAdmin,
		EventBus:             bus,
		TopicNamespace:       topicNamespace,
		DefaultPartitions:    cfg.DefaultPartitions,
		DefaultRetentionMs:   cfg.DefaultRetentionMs,
		MaxTopicsPerTenant:   cfg.MaxTopicsPerTenant,
		MaxStorageBytes:      cfg.MaxStorageBytes,
		DefaultProducerRate:  cfg.ProducerByteRate,
		DefaultConsumerRate:  cfg.ConsumerByteRate,
		DeprovisionGraceDays: cfg.DeprovisionGraceDays,
		MaxRoutingRules:      cfg.MaxRoutingRules,
		EditionManager:       cfg.EditionManager(),
		Logger:               structuredLogger,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create provisioning service")
	}

	// Set up authentication if enabled
	var validator *auth.MultiTenantValidator
	if cfg.AuthEnabled {
		// Create key registry from existing database connection
		keyRegistry, err := auth.NewPostgresKeyRegistry(auth.PostgresKeyRegistryConfig{
			DB:              db,
			RefreshInterval: cfg.KeyRegistryRefreshInterval,
			QueryTimeout:    cfg.KeyRegistryQueryTimeout,
			Logger:          structuredLogger.With().Str("component", "key_registry").Logger(),
		})
		if err != nil {
			structuredLogger.Fatal().Err(err).Msg("Failed to create key registry")
		}
		defer func() { _ = keyRegistry.Close() }() // Close error non-actionable during shutdown

		// Build validator config
		validatorCfg := auth.MultiTenantValidatorConfig{
			KeyRegistry:     keyRegistry,
			RequireTenantID: true,
		}

		// Create multi-tenant validator
		validator, err = auth.NewMultiTenantValidator(validatorCfg)
		if err != nil {
			structuredLogger.Fatal().Err(err).Msg("Failed to create validator")
		}

		structuredLogger.Info().Msg("Authentication enabled")
	}

	// Set up admin auth middleware (if admin token is configured)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	var adminAuth *api.AdminAuth
	if cfg.AdminToken != "" {
		var err error
		adminAuth, err = api.NewAdminAuth(ctx, &wg, cfg.AdminToken, api.AdminAuthConfig{
			FailureThreshold: cfg.AdminAuthFailureThreshold,
			BlockDuration:    cfg.AdminAuthBlockDuration,
			CleanupInterval:  cfg.AdminAuthCleanupInterval,
			CleanupMaxAge:    cfg.AdminAuthCleanupMaxAge,
		}, structuredLogger)
		if err != nil {
			structuredLogger.Fatal().Err(err).Msg("Failed to create admin auth")
		}
		structuredLogger.Info().Msg("Admin token authentication enabled")
	}

	// Initialize HTTP router
	router, err := api.NewRouter(api.RouterConfig{
		Service:            svc,
		Logger:             structuredLogger,
		RateLimit:          cfg.APIRateLimitPerMinute,
		AuthEnabled:        cfg.AuthEnabled,
		Validator:          validator,
		AdminAuth:          adminAuth,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		CORSMaxAge:         cfg.CORSMaxAge,
		ConfigHandler:      platform.ConfigHandler(cfg),
		EditionManager:     cfg.EditionManager(),
		PprofEnabled:       cfg.PprofEnabled,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to initialize HTTP router")
	}

	// Wrap router with tracing middleware (noop when tracing disabled)
	tracedHandler := tracing.HTTPMiddleware(router)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      tracedHandler,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: cfg.HTTPWriteTimeout,
		IdleTimeout:  cfg.HTTPIdleTimeout,
	}

	// Create gRPC server with interceptors
	grpcAddr := fmt.Sprintf(":%d", cfg.GRPCPort)
	grpcListener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", grpcAddr)
	if err != nil {
		structuredLogger.Fatal().Err(err).Int("port", cfg.GRPCPort).Msg("Failed to listen on gRPC port")
	}

	grpcSrv := grpc.NewServer(
		tracing.StatsHandler(),
		grpc.ChainStreamInterceptor(
			grpcserver.RecoveryStreamInterceptor(structuredLogger),
			grpcserver.LoggingStreamInterceptor(structuredLogger),
			grpcserver.MetricsStreamInterceptor(),
		),
	)
	grpcStreamServer, err := grpcserver.NewServer(svc, bus, structuredLogger, grpcserver.ServerConfig{
		MaxTenantsFetchLimit: cfg.MaxTenantsFetchLimit,
	})
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create gRPC stream server")
	}
	provisioningv1.RegisterProvisioningInternalServiceServer(grpcSrv, grpcStreamServer)

	// Start lifecycle manager (background job for tenant cleanup)
	var lifecycleManager *provisioning.LifecycleManager
	if cfg.LifecycleManagerEnabled {
		lm, lmErr := provisioning.NewLifecycleManager(provisioning.LifecycleManagerConfig{
			Service:         svc,
			Interval:        cfg.LifecycleCheckInterval,
			DeletionTimeout: cfg.DeletionTimeout,
			Logger:          structuredLogger,
		})
		if lmErr != nil {
			structuredLogger.Fatal().Err(lmErr).Msg("Failed to create lifecycle manager")
		}
		lifecycleManager = lm
		lifecycleManager.Start()
		defer lifecycleManager.Stop()
		structuredLogger.Info().Dur("interval", cfg.LifecycleCheckInterval).Msg("Lifecycle manager started")
	}

	// Start gRPC server in goroutine
	wg.Go(func() {
		defer logging.RecoverPanic(structuredLogger, "grpc.Serve", nil)
		structuredLogger.Info().Str("addr", grpcAddr).Msg("Starting gRPC server")
		if err := grpcSrv.Serve(grpcListener); err != nil {
			structuredLogger.Error().Err(err).Msg("gRPC server error")
			cancel()
		}
	})

	// Start HTTP server in goroutine
	wg.Go(func() {
		defer logging.RecoverPanic(structuredLogger, "http.ListenAndServe", nil)
		structuredLogger.Info().Str("addr", cfg.Addr).Msg("Starting provisioning HTTP server")
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			structuredLogger.Error().Err(err).Msg("HTTP server error")
			cancel()
		}
	})

	// Wait for interrupt signal or context cancellation (from server error)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigCh:
	case <-ctx.Done():
	}

	structuredLogger.Info().Msg("Shutting down provisioning service")

	// Cancel context to stop admin auth and other background goroutines
	cancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		structuredLogger.Error().Err(err).Msg("Error during HTTP server shutdown")
	}

	// Graceful stop gRPC server
	grpcSrv.GracefulStop()

	// Wait for background goroutines (admin auth cleanup, etc.)
	wg.Wait()

	structuredLogger.Info().Msg("Provisioning service gracefully shut down")
}
