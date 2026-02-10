package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Phase represents the current test phase
type Phase string

const (
	PhaseRamping    Phase = "ramping"
	PhaseSustaining Phase = "sustaining"
	PhaseCompleted  Phase = "completed"
)

// Stats collects test metrics using atomic operations
type Stats struct {
	// Connection metrics
	ActiveConnections atomic.Int64
	TotalCreated      atomic.Int64
	FailedConnections atomic.Int64

	// Message metrics
	MessagesReceived atomic.Int64

	// Subscription metrics
	SubscriptionsSent      atomic.Int64
	SubscriptionsConfirmed atomic.Int64
	SubscriptionsFailed    atomic.Int64

	// Timing
	StartTime        time.Time
	RampCompleteTime time.Time
	phase            atomic.Value // Phase

	// Error tracking
	ErrorCounts sync.Map // map[string]*atomic.Int64

	// Per-channel message counts
	ChannelCounts sync.Map // map[string]*atomic.Int64
}

// NewStats creates a new Stats instance
func NewStats() *Stats {
	s := &Stats{
		StartTime: time.Now(),
	}
	s.phase.Store(PhaseRamping)
	return s
}

// SetPhase sets the current phase
func (s *Stats) SetPhase(phase Phase) {
	s.phase.Store(phase)
	if phase == PhaseSustaining {
		s.RampCompleteTime = time.Now()
	}
}

// GetPhase returns the current phase
func (s *Stats) GetPhase() Phase {
	return s.phase.Load().(Phase)
}

// RecordError increments the error count for the given error type
func (s *Stats) RecordError(errType string) {
	actual, _ := s.ErrorCounts.LoadOrStore(errType, &atomic.Int64{})
	actual.(*atomic.Int64).Add(1)
}

// RecordChannelMessage increments the message count for the given channel
func (s *Stats) RecordChannelMessage(channel string) {
	actual, _ := s.ChannelCounts.LoadOrStore(channel, &atomic.Int64{})
	actual.(*atomic.Int64).Add(1)
	s.MessagesReceived.Add(1)
}

// GetErrorCounts returns a snapshot of error counts
func (s *Stats) GetErrorCounts() map[string]int64 {
	counts := make(map[string]int64)
	s.ErrorCounts.Range(func(key, value any) bool {
		counts[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})
	return counts
}

// GetChannelCounts returns a snapshot of per-channel message counts
func (s *Stats) GetChannelCounts() map[string]int64 {
	counts := make(map[string]int64)
	s.ChannelCounts.Range(func(key, value any) bool {
		counts[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})
	return counts
}

// GetMessageRate returns the messages per second rate
func (s *Stats) GetMessageRate() float64 {
	elapsed := time.Since(s.StartTime).Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(s.MessagesReceived.Load()) / elapsed
}

// GetSubscriptionsPending returns the number of pending subscriptions
func (s *Stats) GetSubscriptionsPending() int64 {
	return s.SubscriptionsSent.Load() - s.SubscriptionsConfirmed.Load() - s.SubscriptionsFailed.Load()
}

// LogReport logs a stats report using zerolog
func (s *Stats) LogReport(logger zerolog.Logger) {
	elapsed := time.Since(s.StartTime)

	logger.Info().
		Str("phase", string(s.GetPhase())).
		Dur("elapsed", elapsed).
		Int64("active_connections", s.ActiveConnections.Load()).
		Int64("total_created", s.TotalCreated.Load()).
		Int64("failed_connections", s.FailedConnections.Load()).
		Int64("messages_received", s.MessagesReceived.Load()).
		Float64("message_rate", s.GetMessageRate()).
		Int64("subscriptions_sent", s.SubscriptionsSent.Load()).
		Int64("subscriptions_confirmed", s.SubscriptionsConfirmed.Load()).
		Int64("subscriptions_failed", s.SubscriptionsFailed.Load()).
		Int64("subscriptions_pending", s.GetSubscriptionsPending()).
		Msg("Stats report")
}

// LogFinalReport logs the final stats report
func (s *Stats) LogFinalReport(logger zerolog.Logger) {
	totalElapsed := time.Since(s.StartTime)
	var sustainElapsed time.Duration
	if !s.RampCompleteTime.IsZero() {
		sustainElapsed = time.Since(s.RampCompleteTime)
	}

	// Build error counts dict
	errCounts := s.GetErrorCounts()
	channelCounts := s.GetChannelCounts()

	event := logger.Info().
		Str("phase", "completed").
		Dur("total_elapsed", totalElapsed).
		Dur("sustain_elapsed", sustainElapsed).
		Int64("total_connections_created", s.TotalCreated.Load()).
		Int64("failed_connections", s.FailedConnections.Load()).
		Int64("total_messages_received", s.MessagesReceived.Load()).
		Float64("avg_message_rate", s.GetMessageRate()).
		Int64("subscriptions_sent", s.SubscriptionsSent.Load()).
		Int64("subscriptions_confirmed", s.SubscriptionsConfirmed.Load()).
		Int64("subscriptions_failed", s.SubscriptionsFailed.Load())

	if len(errCounts) > 0 {
		event = event.Interface("error_counts", errCounts)
	}
	if len(channelCounts) > 0 {
		event = event.Interface("channel_counts", channelCounts)
	}

	event.Msg("Final report")
}
