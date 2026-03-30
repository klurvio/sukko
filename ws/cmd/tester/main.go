package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/klurvio/sukko/cmd/tester/api"
	"github.com/klurvio/sukko/cmd/tester/runner"
	"github.com/klurvio/sukko/internal/shared/logging"
)

const (
	httpReadHeaderTimeout = 10 * time.Second
	httpIdleTimeout       = 120 * time.Second
	shutdownTimeout       = 10 * time.Second
)

func main() {
	// Bootstrap logger for config parsing errors; replaced after config is loaded.
	bootLogger := logging.BootstrapLogger("sukko-tester")

	if err := run(); err != nil {
		bootLogger.Fatal().Err(err).Msg("fatal error")
	}
}

func run() error {
	var cfg TesterConfig
	if err := env.Parse(&cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	logger := logging.NewLogger(logging.LoggerConfig{
		Level:       logging.LogLevel(cfg.LogLevel),
		Format:      logging.LogFormat(cfg.LogFormat),
		ServiceName: "sukko-tester",
	})

	r := runner.New(runner.Config{
		GatewayURL:       cfg.GatewayURL,
		ProvisioningURL:  cfg.ProvisioningURL,
		Token:            cfg.AuthToken,
		MessageBackend:   cfg.MessageBackend,
		KafkaBrokers:     cfg.KafkaBrokers,
		JWTLifetime:      cfg.JWTLifetime,
		JWTRefreshBefore: cfg.JWTRefreshBefore,
		KeyExpiry:        cfg.KeyExpiry,
	}, logger)

	handler := api.NewRouter(r, cfg.AuthToken, logger)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Go(func() {
		defer logging.RecoverPanic(logger, "http-server", nil)
		logger.Info().Int("port", cfg.Port).Msg("tester API listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	})

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("shutting down")
	case err := <-errCh:
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	wg.Wait()
	r.StopAll()
	r.Wait()
	logger.Info().Msg("shutdown complete")
	return nil
}
