package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
)

func main() {
	cfg, err := ParseConfig()
	if err != nil {
		log := zerolog.New(os.Stderr).With().Timestamp().Logger()
		log.Fatal().Err(err).Msg("failed to parse config")
	}

	log := setupLogger(cfg.LogLevel)

	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	log.Info().
		Str("namespace", cfg.KafkaNamespace).
		Str("timing_mode", string(cfg.TimingMode)).
		Dur("duration", cfg.Duration).
		Msg("wspublisher starting")

	publisher, err := NewPublisher(cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create publisher")
	}
	defer publisher.Close()

	runner := NewRunner(cfg, publisher, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("received signal, shutting down")
		cancel()
	}()

	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error().Err(err).Msg("runner error")
		os.Exit(1)
	}

	log.Info().Msg("wspublisher stopped")
}

func setupLogger(level string) zerolog.Logger {
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}

	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	return zerolog.New(output).
		With().
		Timestamp().
		Str("service", "wspublisher").
		Logger().
		Level(lvl)
}
