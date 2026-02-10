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
	// Parse configuration
	config, err := ParseConfig()
	if err != nil {
		// Use stderr for config errors since logger isn't set up yet
		os.Stderr.WriteString("Configuration error: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Setup logger
	zerolog.SetGlobalLevel(config.GetLogLevel())
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).
		With().
		Timestamp().
		Logger()

	// Log configuration
	logger.Info().
		Str("url", config.WSURL).
		Int("target_connections", config.TargetConnections).
		Int("ramp_rate", config.RampRate).
		Dur("duration", config.SustainDuration).
		Strs("channels", config.Channels).
		Str("mode", config.SubscriptionMode).
		Int("channels_per_client", config.ChannelsPerClient).
		Msg("Starting load test")

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	// Create and run load runner
	runner := NewLoadRunner(config, logger)
	if err := runner.Run(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Error().Err(err).Msg("Load test failed")
			os.Exit(1)
		}
	}

	logger.Info().Msg("Load test completed")
}
