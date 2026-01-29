// Provisioning service entry point for tenant management.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver

	"github.com/Toniq-Labs/odin-ws/internal/auth"
	"github.com/Toniq-Labs/odin-ws/internal/monitoring"
	"github.com/Toniq-Labs/odin-ws/internal/platform"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/api"
	provkafka "github.com/Toniq-Labs/odin-ws/internal/provisioning/kafka"
	"github.com/Toniq-Labs/odin-ws/internal/provisioning/repository"
	"github.com/Toniq-Labs/odin-ws/internal/types"
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

	// Initialize structured logger
	structuredLogger := monitoring.NewLogger(monitoring.LoggerConfig{
		Level:       types.LogLevel(cfg.LogLevel),
		Format:      types.LogFormat(cfg.LogFormat),
		ServiceName: "provisioning-service",
	})

	// Connect to PostgreSQL
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("Failed to open database connection: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Configure connection pool
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)

	// Verify database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		logger.Fatalf("Failed to ping database: %v", err)
	}
	logger.Printf("Connected to database")

	// Initialize repositories
	tenantRepo := repository.NewPostgresTenantRepository(db)
	keyRepo := repository.NewPostgresKeyRepository(db)
	topicRepo := repository.NewPostgresTopicRepository(db)
	quotaRepo := repository.NewPostgresQuotaRepository(db)
	auditRepo := repository.NewPostgresAuditRepository(db)

	// Initialize Kafka admin
	var kafkaAdmin provisioning.KafkaAdmin
	if cfg.KafkaBrokers != "" {
		// Parse brokers (comma-separated)
		brokers := strings.Split(cfg.KafkaBrokers, ",")
		for i := range brokers {
			brokers[i] = strings.TrimSpace(brokers[i])
		}

		// Build Kafka admin config
		adminCfg := provkafka.AdminConfig{
			Brokers: brokers,
			Timeout: cfg.KafkaAdminTimeout,
			Logger:  structuredLogger,
		}

		// Add SASL authentication if enabled
		if cfg.KafkaSASLEnabled {
			adminCfg.SASL = &provkafka.SASLConfig{
				Mechanism: cfg.KafkaSASLMechanism,
				Username:  cfg.KafkaSASLUsername,
				Password:  cfg.KafkaSASLPassword,
			}
		}

		// Add TLS encryption if enabled
		if cfg.KafkaTLSEnabled {
			adminCfg.TLS = &provkafka.TLSConfig{
				Enabled:            true,
				InsecureSkipVerify: cfg.KafkaTLSInsecure,
				CAPath:             cfg.KafkaTLSCAPath,
			}
		}

		// Create real Kafka admin
		admin, err := provkafka.NewAdmin(adminCfg)
		if err != nil {
			logger.Printf("Warning: Failed to connect to Kafka, using noop admin: %v", err)
			kafkaAdmin = provisioning.NewNoopKafkaAdmin()
		} else {
			kafkaAdmin = admin
			logger.Printf("Kafka admin connected to %v", brokers)
			// Ensure cleanup on shutdown
			defer admin.Close()
		}
	} else {
		kafkaAdmin = provisioning.NewNoopKafkaAdmin()
		logger.Printf("Kafka admin disabled (no brokers configured)")
	}

	// Initialize provisioning service
	svc := provisioning.NewService(provisioning.ServiceConfig{
		TenantStore:          tenantRepo,
		KeyStore:             keyRepo,
		TopicStore:           topicRepo,
		QuotaStore:           quotaRepo,
		AuditStore:           auditRepo,
		KafkaAdmin:           kafkaAdmin,
		TopicNamespace:       cfg.TopicNamespace,
		DefaultPartitions:    cfg.DefaultPartitions,
		DefaultRetentionMs:   cfg.DefaultRetentionMs,
		MaxTopicsPerTenant:   cfg.MaxTopicsPerTenant,
		DeprovisionGraceDays: cfg.DeprovisionGraceDays,
		Logger:               structuredLogger,
	})

	// Set up authentication if enabled
	var validator *auth.MultiTenantValidator
	var oidcCloser *auth.OIDCKeyfuncResult
	if cfg.AuthEnabled {
		// Create key registry from existing database connection
		keyRegistry, err := auth.NewPostgresKeyRegistry(auth.PostgresKeyRegistryConfig{
			DB:              db,
			RefreshInterval: 1 * time.Minute,
			QueryTimeout:    5 * time.Second,
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

	// Initialize HTTP router
	router := api.NewRouter(api.RouterConfig{
		Service:            svc,
		Logger:             structuredLogger,
		RateLimit:          cfg.APIRateLimitPerMinute,
		AuthEnabled:        cfg.AuthEnabled,
		Validator:          validator,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		CORSMaxAge:         cfg.CORSMaxAge,
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: cfg.HTTPWriteTimeout,
		IdleTimeout:  cfg.HTTPIdleTimeout,
	}

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

	// Start server in goroutine
	go func() {
		defer monitoring.RecoverPanic(structuredLogger, "http.ListenAndServe", nil)
		logger.Printf("Starting provisioning service on %s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Println("Shutting down provisioning service...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("Error during server shutdown: %v", err)
	}

	logger.Println("Provisioning service gracefully shut down.")
}
