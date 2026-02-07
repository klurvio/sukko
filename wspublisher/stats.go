package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Stats tracks publishing metrics.
type Stats struct {
	messagesPublished atomic.Int64
	messagesFailed    atomic.Int64
	bytesPublished    atomic.Int64
	startTime         time.Time
	topicCounts       sync.Map
	channelCounts     sync.Map
	log               zerolog.Logger
}

func NewStats(log zerolog.Logger) *Stats {
	return &Stats{startTime: time.Now(), log: log}
}

func (s *Stats) RecordSuccess(topic, channel string, bytes int) {
	s.messagesPublished.Add(1)
	s.bytesPublished.Add(int64(bytes))
	s.increment(&s.topicCounts, topic)
	s.increment(&s.channelCounts, channel)
}

func (s *Stats) RecordFailure() {
	s.messagesFailed.Add(1)
}

func (s *Stats) increment(m *sync.Map, key string) {
	counter, _ := m.LoadOrStore(key, &atomic.Int64{})
	counter.(*atomic.Int64).Add(1)
}

func (s *Stats) Duration() time.Duration {
	return time.Since(s.startTime)
}

func (s *Stats) Rate() float64 {
	elapsed := s.Duration().Seconds()
	if elapsed == 0 {
		return 0
	}
	return float64(s.messagesPublished.Load()) / elapsed
}

func (s *Stats) LogSummary() {
	s.log.Info().
		Int64("published", s.messagesPublished.Load()).
		Int64("failed", s.messagesFailed.Load()).
		Int64("bytes", s.bytesPublished.Load()).
		Dur("duration", s.Duration()).
		Float64("rate_per_sec", s.Rate()).
		Msg("stats")
}

func (s *Stats) LogFinal() {
	s.log.Info().
		Int64("published", s.messagesPublished.Load()).
		Int64("failed", s.messagesFailed.Load()).
		Int64("bytes", s.bytesPublished.Load()).
		Dur("duration", s.Duration()).
		Float64("rate_per_sec", s.Rate()).
		Msg("final stats")

	// Log per-topic breakdown
	topics := make(map[string]int64)
	s.topicCounts.Range(func(k, v interface{}) bool {
		topics[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	if len(topics) > 0 {
		s.log.Info().Interface("topics", topics).Msg("per-topic breakdown")
	}
}
