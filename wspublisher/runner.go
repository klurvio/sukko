package main

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Runner orchestrates the publishing loop.
type Runner struct {
	cfg       *Config
	publisher *Publisher
	generator *Generator
	scheduler Scheduler
	stats     *Stats
	log       zerolog.Logger
}

func NewRunner(cfg *Config, publisher *Publisher, log zerolog.Logger) *Runner {
	return &Runner{
		cfg:       cfg,
		publisher: publisher,
		generator: NewGenerator(cfg),
		scheduler: NewScheduler(cfg),
		stats:     NewStats(log),
		log:       log,
	}
}

// Run starts the publishing loop.
func (r *Runner) Run(ctx context.Context) error {
	r.log.Info().
		Str("timing_mode", r.scheduler.Name()).
		Strs("channels", r.generator.Channels()).
		Str("namespace", r.cfg.KafkaNamespace).
		Dur("duration", r.cfg.Duration).
		Msg("starting publisher")

	// Apply duration limit if set
	if r.cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.cfg.Duration)
		defer cancel()
	}

	// Start stats logger
	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-statsTicker.C:
				r.stats.LogSummary()
			}
		}
	}()

	// Main publishing loop
	for {
		select {
		case <-ctx.Done():
			r.log.Info().Msg("stopping publisher")
			r.stats.LogFinal()
			return ctx.Err()
		default:
			if err := r.publishOne(ctx); err != nil {
				if ctx.Err() != nil {
					r.stats.LogFinal()
					return ctx.Err()
				}
				r.log.Warn().Err(err).Msg("publish failed")
				r.stats.RecordFailure()
			}

			delay := r.scheduler.NextDelay()
			if delay > 0 {
				select {
				case <-ctx.Done():
					r.stats.LogFinal()
					return ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
}

func (r *Runner) publishOne(ctx context.Context) error {
	msg, err := r.generator.NextMessage()
	if err != nil {
		return err
	}

	if err := r.publisher.Publish(ctx, msg); err != nil {
		return err
	}

	r.stats.RecordSuccess(msg.Topic, msg.Key, len(msg.Payload))

	r.log.Debug().
		Str("topic", msg.Topic).
		Str("key", msg.Key).
		Int("bytes", len(msg.Payload)).
		Msg("published")

	return nil
}
