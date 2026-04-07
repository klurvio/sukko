// Provisioning service entry point for tenant management.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
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

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	provisioningv1 "github.com/klurvio/sukko/gen/proto/sukko/provisioning/v1"
	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/api"
	provauth "github.com/klurvio/sukko/internal/provisioning/auth"
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

// base64 encoding variants for bootstrap key decoding.
var (
	base64Std = base64.StdEncoding
	base64Raw = base64.RawStdEncoding
)

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

	// Create cancellable context for service lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	// Create event bus for gRPC streaming notifications
	bus := eventbus.New(structuredLogger)

	// Open database connection pool (PostgreSQL via pgxpool)
	pool, err := repository.OpenDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to open database")
	}
	defer pool.Close()
	structuredLogger.Info().Msg("Database pool opened")

	// Initialize repositories
	tenantRepo := repository.NewTenantRepository(pool)
	keyRepo := repository.NewKeyRepository(pool)
	apiKeyRepo := repository.NewAPIKeyStore(pool)
	routingRulesRepo := repository.NewRoutingRulesRepository(pool)
	quotaRepo := repository.NewQuotaRepository(pool)
	auditRepo := repository.NewAuditRepository(pool)
	channelRulesRepo := repository.NewChannelRulesRepository(pool)
	pushCredentialsRepo, err := repository.NewCredentialsRepository(pool, cfg.CredentialsEncryptionKey)
	if err != nil {
		structuredLogger.Fatal().Err(err).Msg("Failed to create push credentials repository")
	}
	pushChannelConfigRepo := repository.NewChannelConfigRepository(pool)

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
		keyRegistry, err := auth.NewKeyRegistry(auth.KeyRegistryConfig{
			Pool:            pool,
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

	// Initialize admin key repository and registry
	adminKeyRepo := repository.NewAdminKeyRepository(pool)
	adminKeyRegistry := provauth.NewAdminKeyRegistry()

	// Bootstrap: auto-register admin key from env var if no keys exist
	if cfg.AdminBootstrapKey != "" {
		if err := bootstrapAdminKey(ctx, cfg.AdminBootstrapKey, adminKeyRepo, adminKeyRegistry, structuredLogger); err != nil {
			structuredLogger.Warn().Err(err).Msg("admin key bootstrap failed")
		}
	} else {
		// Load existing keys into cache
		loadAdminKeyCache(ctx, adminKeyRepo, adminKeyRegistry, structuredLogger)
	}

	adminValidator := provauth.NewAdminValidator(adminKeyRegistry)

	// Initialize HTTP router
	router, err := api.NewRouter(api.RouterConfig{
		Service:            svc,
		Logger:             structuredLogger,
		RateLimit:          cfg.APIRateLimitPerMinute,
		AuthEnabled:        cfg.AuthEnabled,
		Validator:          validator,
		AdminValidator:     adminValidator,
		AdminKeyRegistry:   adminKeyRegistry,
		AdminKeyRepo:       adminKeyRepo,
		EventBus:           bus,
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
		MaxTenantsFetchLimit:  cfg.MaxTenantsFetchLimit,
		PushCredentialsRepo:   pushCredentialsRepo,
		PushChannelConfigRepo: pushChannelConfigRepo,
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

	// Cancel context to stop background goroutines
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

	// Wait for background goroutines
	wg.Wait()

	structuredLogger.Info().Msg("Provisioning service gracefully shut down")
}

// bootstrapAdminKey auto-registers the bootstrap public key if no admin keys exist.
// One-time only — ignored if keys already exist in the database.
func bootstrapAdminKey(ctx context.Context, bootstrapKeyBase64 string, repo *repository.AdminKeyRepository, registry *provauth.AdminKeyRegistry, logger zerolog.Logger) error {
	count, err := repo.CountActive(ctx)
	if err != nil {
		return fmt.Errorf("count active admin keys: %w", err)
	}
	if count > 0 {
		logger.Debug().Int("existing_keys", count).Msg("admin keys exist, skipping bootstrap")
		loadAdminKeyCache(ctx, repo, registry, logger)
		return nil
	}

	// Decode base64 → Ed25519 public key → PEM for storage
	decoded, err := decodeBootstrapKey(bootstrapKeyBase64)
	if err != nil {
		return fmt.Errorf("decode bootstrap key: %w", err)
	}

	pemKey, err := marshalPublicKeyPEM(decoded)
	if err != nil {
		return fmt.Errorf("marshal bootstrap key to PEM: %w", err)
	}

	key := &repository.AdminKey{
		KeyID:        "bootstrap-0",
		Name:         "bootstrap",
		Algorithm:    "Ed25519",
		PublicKey:    pemKey,
		RegisteredBy: "system",
	}
	if err := repo.Create(ctx, key); err != nil {
		return fmt.Errorf("register bootstrap key: %w", err)
	}

	logger.Info().Str("key_id", "bootstrap-0").Msg("admin key auto-registered from bootstrap")

	// Load into cache
	loadAdminKeyCache(ctx, repo, registry, logger)
	return nil
}

// loadAdminKeyCache loads all active admin keys from DB into the in-memory registry.
func loadAdminKeyCache(ctx context.Context, repo *repository.AdminKeyRepository, registry *provauth.AdminKeyRegistry, logger zerolog.Logger) {
	keys, err := repo.ListActive(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("failed to load admin keys into cache")
		return
	}

	keyInfos := make([]*auth.KeyInfo, 0, len(keys))
	for _, k := range keys {
		pubKey, err := provauth.ParsePublicKeyPEM(k.PublicKey)
		if err != nil {
			logger.Warn().Err(err).Str("key_id", k.KeyID).Msg("skipping admin key with invalid key material")
			continue
		}
		keyInfos = append(keyInfos, provauth.AdminKeyToKeyInfo(k.KeyID, k.Name, k.Algorithm, pubKey))
	}

	registry.Refresh(keyInfos)
	logger.Info().Int("keys", len(keyInfos)).Msg("admin key cache loaded")
}

// decodeBootstrapKey decodes a base64-encoded Ed25519 public key (padded or unpadded).
func decodeBootstrapKey(b64 string) ([]byte, error) {
	decoded, err := base64Std.DecodeString(b64)
	if err != nil {
		decoded, err = base64Raw.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("invalid base64: %w", err)
		}
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("expected 32 bytes for Ed25519 public key, got %d", len(decoded))
	}
	return decoded, nil
}

// marshalPublicKeyPEM encodes an Ed25519 public key as PEM.
func marshalPublicKeyPEM(pubKeyBytes []byte) (string, error) {
	pubKey := ed25519.PublicKey(pubKeyBytes)
	der, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("marshal PKIX: %w", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return string(pemBlock), nil
}
