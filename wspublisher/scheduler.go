package main

import (
	"math"
	"math/rand"
	"time"
)

// Scheduler generates delays between message publishes.
type Scheduler interface {
	NextDelay() time.Duration
	Name() string
}

// PoissonScheduler generates delays following a Poisson process.
type PoissonScheduler struct {
	lambda float64
	rng    *rand.Rand
}

func NewPoissonScheduler(lambda float64) *PoissonScheduler {
	return &PoissonScheduler{
		lambda: lambda,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *PoissonScheduler) NextDelay() time.Duration {
	u := s.rng.Float64()
	if u == 0 {
		u = 1e-10
	}
	seconds := -math.Log(u) / s.lambda
	return time.Duration(seconds * float64(time.Second))
}

func (s *PoissonScheduler) Name() string { return "poisson" }

// UniformScheduler generates random delays uniformly between min and max.
type UniformScheduler struct {
	min, max time.Duration
	rng      *rand.Rand
}

func NewUniformScheduler(min, max time.Duration) *UniformScheduler {
	return &UniformScheduler{
		min: min,
		max: max,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *UniformScheduler) NextDelay() time.Duration {
	diff := s.max - s.min
	return s.min + time.Duration(s.rng.Int63n(int64(diff)+1))
}

func (s *UniformScheduler) Name() string { return "uniform" }

// BurstScheduler sends messages in bursts.
type BurstScheduler struct {
	burstCount     int
	burstPause     time.Duration
	inBurstDelay   time.Duration
	currentInBurst int
}

func NewBurstScheduler(burstCount int, burstPause time.Duration) *BurstScheduler {
	return &BurstScheduler{
		burstCount:   burstCount,
		burstPause:   burstPause,
		inBurstDelay: time.Millisecond,
	}
}

func (s *BurstScheduler) NextDelay() time.Duration {
	s.currentInBurst++
	if s.currentInBurst >= s.burstCount {
		s.currentInBurst = 0
		return s.burstPause
	}
	return s.inBurstDelay
}

func (s *BurstScheduler) Name() string { return "burst" }

// NewScheduler creates a scheduler based on configuration.
func NewScheduler(cfg *Config) Scheduler {
	switch cfg.TimingMode {
	case TimingModePoisson:
		return NewPoissonScheduler(cfg.PoissonLambda)
	case TimingModeUniform:
		return NewUniformScheduler(cfg.MinInterval, cfg.MaxInterval)
	case TimingModeBurst:
		return NewBurstScheduler(cfg.BurstCount, cfg.BurstPause)
	default:
		return NewPoissonScheduler(100)
	}
}
