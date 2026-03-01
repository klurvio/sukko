// Provisioning service entry point for tenant management.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"

	provisioningv1 "github.com/Toniq-Labs/odin-ws/gen/proto/odin/provisioning/v1"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/api"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/configstore"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/eventbus"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/grpcserver"
	provkafka "github.com/Toniq-Labs/odin-ws/internal/provisioning/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/repository"
	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	"github.com/Toniq-Labs/odin-ws/internal/shared/platform"
)

// Config reload metrics.
var (
	configReloadTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "provisioning_config_reload_total",
		Help: "Total number of config file reload attempts.",
	})
	configReloadFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "provisioning_config_reload_failures_total",
		Help: "Total number of failed config file reload attempts.",
	})
	configReloadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "provisioning_config_reload_duration_seconds",
		Help:    "Duration of config file reload operations.",
		Buckets: prometheus.DefBuckets,
	})
)

func main() {
	var (
		debug = flag.Bool("debug", false, "enable debug logging (overrides LOG_LEVEL)")
	)
	flag.Parse()

	// Create basic logger for startup
	logger := log.New(os.Stdout, "[PROVISIONING] ", log.LstdFlags)

	// Load configuration
	cfg, err := platform.LoadProvisioningConfig(nil)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Override debug mode if flag set
	if *debug {
		cfg.LogLevel = "debug"
		logger.Printf("Debug mode enabled via flag")
	}

	// Print configuration
	cfg.Print()

	// Resolve effective topic namespace for Kafka
	topicNamespace := kafka.ResolveNamespace(cfg.TopicNamespaceOverride, cfg.Environment)
	logger.Printf("Topic namespace: %s (environment: %s)", topicNamespace, cfg.Environment)

	// Initialize structured logger
	structuredLogger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: "provisioning-service",
	})

	// Create event bus for gRPC streaming notifications
	bus := eventbus.New(structuredLogger)

	// Initialize stores and service based on provisioning mode
	var svc *provisioning.Service
	var configLoader *configstore.Loader
	var db *sql.DB // non-nil only in API mode

	switch cfg.ProvisioningMode {
	case "config":
		// Config file mode — read-only, in-memory stores from YAML
		configLoader = configstore.NewLoader(cfg.ConfigFilePath, structuredLogger)
		if err := configLoader.Load(); err != nil {
			logger.Fatalf("Failed to load config file: %v", err)
		}

		stores := configLoader.Stores()
		svc = provisioning.NewService(provisioning.ServiceConfig{
			TenantStore:          stores.TenantStore(),
			KeyStore:             stores.KeyStore(),
			TopicStore:           stores.TopicStore(),
			QuotaStore:           stores.QuotaStore(),
			AuditStore:           stores.AuditStore(),
			OIDCConfigStore:      stores.OIDCConfigStore(),
			ChannelRulesStore:    stores.ChannelRulesStore(),
			KafkaAdmin:           provisioning.NewNoopKafkaAdmin(),
			EventBus:             bus,
			TopicNamespace:       topicNamespace,
			DefaultPartitions:    cfg.DefaultPartitions,
			DefaultRetentionMs:   cfg.DefaultRetentionMs,
			MaxTopicsPerTenant:   cfg.MaxTopicsPerTenant,
			MaxStorageBytes:      cfg.MaxStorageBytes,
			DefaultProducerRate:  cfg.ProducerByteRate,
			DefaultConsumerRate:  cfg.ConsumerByteRate,
			DeprovisionGraceDays: cfg.DeprovisionGraceDays,
			Logger:               structuredLogger,
		})
		logger.Printf("Config file mode: loaded from %s", cfg.ConfigFilePath)

	default: // "api" mode — database-backed
		// Open database using factory (supports both SQLite and PostgreSQL)
		var err error
		db, err = repository.OpenDatabase(repository.DatabaseConfig{
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
			logger.Fatalf("Failed to open database: %v", err)
		}
		defer func() { _ = db.Close() }()
		logger.Printf("Database opened (driver: %s)", cfg.DatabaseDriver)

		// Initialize repositories
		tenantRepo := repository.NewPostgresTenantRepository(db)
		keyRepo := repository.NewPostgresKeyRepository(db)
		topicRepo := repository.NewPostgresTopicRepository(db)
		quotaRepo := repository.NewPostgresQuotaRepository(db)
		auditRepo := repository.NewPostgresAuditRepository(db)
		oidcRepo := repository.NewPostgresOIDCConfigRepository(db)
		channelRulesRepo := repository.NewPostgresChannelRulesRepository(db)

		// Initialize Kafka admin
		var kafkaAdmin provisioning.KafkaAdmin
		if cfg.KafkaBrokers != "" {
			brokers := strings.Split(cfg.KafkaBrokers, ",")
			for i := range brokers {
				brokers[i] = strings.TrimSpace(brokers[i])
			}

			adminCfg := provkafka.AdminConfig{
				Brokers: brokers,
				Timeout: cfg.KafkaAdminTimeout,
				Logger:  structuredLogger,
			}

			if cfg.KafkaSASLEnabled {
				adminCfg.SASL = &provkafka.SASLConfig{
					Mechanism: cfg.KafkaSASLMechanism,
					Username:  cfg.KafkaSASLUsername,
					Password:  cfg.KafkaSASLPassword,
				}
			}

			if cfg.KafkaTLSEnabled {
				adminCfg.TLS = &provkafka.TLSConfig{
					Enabled:            true,
					InsecureSkipVerify: cfg.KafkaTLSInsecure,
					CAPath:             cfg.KafkaTLSCAPath,
				}
			}

			admin, err := provkafka.NewAdmin(adminCfg)
			if err != nil {
				logger.Printf("Warning: Failed to connect to Kafka, using noop admin: %v", err)
				kafkaAdmin = provisioning.NewNoopKafkaAdmin()
			} else {
				kafkaAdmin = admin
				logger.Printf("Kafka admin connected to %v", brokers)
				defer admin.Close()
			}
		} else {
			kafkaAdmin = provisioning.NewNoopKafkaAdmin()
			logger.Printf("Kafka admin disabled (no brokers configured)")
		}

		svc = provisioning.NewService(provisioning.ServiceConfig{
			TenantStore:          tenantRepo,
			KeyStore:             keyRepo,
			TopicStore:           topicRepo,
			QuotaStore:           quotaRepo,
			AuditStore:           auditRepo,
			OIDCConfigStore:      oidcRepo,
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
			Logger:               structuredLogger,
		})
	}

	// Set up authentication if enabled (requires database — API mode only)
	var validator *auth.MultiTenantValidator
	var oidcCloser *auth.OIDCKeyfuncResult
	if cfg.AuthEnabled && db != nil {
		// Create key registry from existing database connection
		keyRegistry, err := auth.NewPostgresKeyRegistry(auth.PostgresKeyRegistryConfig{
			DB:              db,
			RefreshInterval: cfg.KeyRegistryRefreshInterval,
			QueryTimeout:    cfg.KeyRegistryQueryTimeout,
			Logger:          structuredLogger.With().Str("component", "key_registry").Logger(),
		})
		if err != nil {
			logger.Fatalf("Failed to create key registry: %v", err)
		}
		defer func() { _ = keyRegistry.Close() }()

		// Build validator config
		validatorCfg := auth.MultiTenantValidatorConfig{
			KeyRegistry:     keyRegistry,
			RequireTenantID: true,
			RequireKeyID:    true,
		}

		// Set up OIDC keyfunc if configured (graceful degradation on failure)
		if cfg.OIDCEnabled() {
			oidcResult, err := auth.NewOIDCKeyfunc(context.Background(), auth.OIDCConfig{
				IssuerURL: cfg.OIDCIssuerURL,
				JWKSURL:   cfg.OIDCJWKSURL,
				Audience:  cfg.OIDCAudience,
			}, structuredLogger.With().Str("component", "oidc").Logger())

			if err != nil {
				// Graceful degradation: log warning but continue without OIDC
				structuredLogger.Warn().
					Err(err).
					Str("jwks_url", cfg.OIDCJWKSURL).
					Msg("Failed to create OIDC keyfunc, continuing without OIDC support")
			} else {
				oidcCloser = oidcResult
				validatorCfg.OIDCKeyfunc = oidcResult.Keyfunc
				validatorCfg.OIDCIssuer = cfg.OIDCIssuerURL
				validatorCfg.OIDCAudience = cfg.OIDCAudience
				defer oidcCloser.Close()

				structuredLogger.Info().
					Str("issuer_url", cfg.OIDCIssuerURL).
					Str("jwks_url", cfg.OIDCJWKSURL).
					Msg("OIDC support enabled")
			}
		}

		// Create multi-tenant validator
		validator, err = auth.NewMultiTenantValidator(validatorCfg)
		if err != nil {
			logger.Fatalf("Failed to create validator: %v", err)
		}

		structuredLogger.Info().
			Bool("oidc_enabled", cfg.OIDCEnabled()).
			Msg("Authentication enabled")
	}

	// Set up admin auth middleware (if admin token is configured)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	var adminAuth *api.AdminAuth
	if cfg.AdminToken != "" {
		adminAuth = api.NewAdminAuth(ctx, &wg, cfg.AdminToken, api.AdminAuthConfig{
			FailureThreshold: cfg.AdminAuthFailureThreshold,
			BlockDuration:    cfg.AdminAuthBlockDuration,
			CleanupInterval:  cfg.AdminAuthCleanupInterval,
			CleanupMaxAge:    cfg.AdminAuthCleanupMaxAge,
		}, structuredLogger)
		logger.Printf("Admin token authentication enabled")
	}

	// Initialize HTTP router
	router := api.NewRouter(api.RouterConfig{
		Service:            svc,
		Logger:             structuredLogger,
		RateLimit:          cfg.APIRateLimitPerMinute,
		AuthEnabled:        cfg.AuthEnabled,
		Validator:          validator,
		AdminAuth:          adminAuth,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		CORSMaxAge:         cfg.CORSMaxAge,
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: cfg.HTTPWriteTimeout,
		IdleTimeout:  cfg.HTTPIdleTimeout,
	}

	// Create gRPC server with interceptors
	grpcAddr := fmt.Sprintf(":%d", cfg.GRPCPort)
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Fatalf("Failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainStreamInterceptor(
			grpcserver.RecoveryStreamInterceptor(structuredLogger),
			grpcserver.LoggingStreamInterceptor(structuredLogger),
			grpcserver.MetricsStreamInterceptor(),
		),
	)
	provisioningv1.RegisterProvisioningInternalServiceServer(grpcSrv, grpcserver.NewServer(svc, bus, structuredLogger))

	// Start lifecycle manager (background job for tenant cleanup)
	var lifecycleManager *provisioning.LifecycleManager
	if cfg.LifecycleManagerEnabled {
		lifecycleManager = provisioning.NewLifecycleManager(provisioning.LifecycleManagerConfig{
			Service:  svc,
			Interval: cfg.LifecycleCheckInterval,
			Logger:   structuredLogger,
		})
		lifecycleManager.Start()
		defer lifecycleManager.Stop()
		logger.Printf("Lifecycle manager started (interval: %s)", cfg.LifecycleCheckInterval)
	}

	// Start SIGHUP reload handler for config file mode
	if configLoader != nil {
		sighupCh := make(chan os.Signal, 1)
		signal.Notify(sighupCh, syscall.SIGHUP)

		wg.Add(1)
		go func() {
			defer logging.RecoverPanic(structuredLogger, "sighup_handler", nil)
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case <-sighupCh:
					structuredLogger.Info().Msg("SIGHUP received, reloading config file")
					configReloadTotal.Inc()

					start := time.Now()
					if err := configLoader.Reload(); err != nil {
						configReloadFailures.Inc()
						configReloadDuration.Observe(time.Since(start).Seconds())
						structuredLogger.Error().Err(err).Msg("Config file reload failed, keeping previous config")
						continue
					}
					configReloadDuration.Observe(time.Since(start).Seconds())

					// Publish all event types to notify gRPC stream subscribers
					bus.Publish(eventbus.Event{Type: eventbus.KeysChanged})
					bus.Publish(eventbus.Event{Type: eventbus.TenantConfigChanged})
					bus.Publish(eventbus.Event{Type: eventbus.TopicsChanged})

					structuredLogger.Info().Msg("Config file reloaded successfully")
				}
			}
		}()
		logger.Printf("SIGHUP reload handler registered for config file mode")
	}

	// Start gRPC server in goroutine
	wg.Add(1)
	go func() {
		defer logging.RecoverPanic(structuredLogger, "grpc.Serve", nil)
		defer wg.Done()
		logger.Printf("Starting gRPC server on %s", grpcAddr)
		if err := grpcSrv.Serve(grpcListener); err != nil {
			logger.Fatalf("gRPC server error: %v", err)
		}
	}()

	// Start HTTP server in goroutine
	wg.Add(1)
	go func() {
		defer logging.RecoverPanic(structuredLogger, "http.ListenAndServe", nil)
		defer wg.Done()
		logger.Printf("Starting provisioning HTTP service on %s", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Println("Shutting down provisioning service...")

	// Cancel context to stop admin auth and other background goroutines
	cancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("Error during HTTP server shutdown: %v", err)
	}

	// Graceful stop gRPC server
	grpcSrv.GracefulStop()

	// Wait for background goroutines (admin auth cleanup, etc.)
	wg.Wait()

	logger.Println("Provisioning service gracefully shut down.")
}
