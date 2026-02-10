package main

import (
	"testing"
	"time"
)

func TestPoissonScheduler_NextDelay(t *testing.T) {
	s := NewPoissonScheduler(100) // 100 events/sec = avg 10ms delay

	var totalDelay time.Duration
	samples := 10000
	for range samples {
		delay := s.NextDelay()
		if delay < 0 {
			t.Errorf("got negative delay: %v", delay)
		}
		totalDelay += delay
	}

	// Average delay should be around 1/lambda = 10ms
	avgDelay := totalDelay / time.Duration(samples)
	expectedAvg := 10 * time.Millisecond

	// Allow 20% tolerance
	if avgDelay < expectedAvg*8/10 || avgDelay > expectedAvg*12/10 {
		t.Errorf("average delay = %v, expected ~%v", avgDelay, expectedAvg)
	}
}

func TestPoissonScheduler_Distribution(t *testing.T) {
	s := NewPoissonScheduler(1000)

	// Verify delays vary
	delays := make(map[time.Duration]int)
	for range 100 {
		delays[s.NextDelay()]++
	}

	if len(delays) < 50 {
		t.Errorf("expected varied delays, got only %d unique values", len(delays))
	}
}

func TestPoissonScheduler_Name(t *testing.T) {
	s := NewPoissonScheduler(100)
	if s.Name() != "poisson" {
		t.Errorf("expected name 'poisson', got %s", s.Name())
	}
}

func TestUniformScheduler_NextDelay(t *testing.T) {
	min := 10 * time.Millisecond
	max := 100 * time.Millisecond
	s := NewUniformScheduler(min, max)

	for range 1000 {
		delay := s.NextDelay()
		if delay < min {
			t.Errorf("delay %v < min %v", delay, min)
		}
		if delay > max {
			t.Errorf("delay %v > max %v", delay, max)
		}
	}
}

func TestUniformScheduler_Distribution(t *testing.T) {
	min := time.Millisecond
	max := 10 * time.Millisecond
	s := NewUniformScheduler(min, max)

	var total time.Duration
	samples := 10000
	for range samples {
		total += s.NextDelay()
	}

	// Average should be (min + max) / 2 = 5.5ms
	avg := total / time.Duration(samples)
	expected := (min + max) / 2

	// Allow 10% tolerance
	if avg < expected*9/10 || avg > expected*11/10 {
		t.Errorf("average delay = %v, expected ~%v", avg, expected)
	}
}

func TestUniformScheduler_Name(t *testing.T) {
	s := NewUniformScheduler(time.Millisecond, time.Second)
	if s.Name() != "uniform" {
		t.Errorf("expected name 'uniform', got %s", s.Name())
	}
}

func TestBurstScheduler_Pattern(t *testing.T) {
	burstCount := 5
	burstPause := 100 * time.Millisecond
	s := NewBurstScheduler(burstCount, burstPause)

	// First burst: 4 short delays then 1 pause
	for i := range burstCount {
		delay := s.NextDelay()
		if i < burstCount-1 {
			if delay > 10*time.Millisecond {
				t.Errorf("expected short delay at position %d, got %v", i, delay)
			}
		} else {
			if delay != burstPause {
				t.Errorf("expected pause %v, got %v", burstPause, delay)
			}
		}
	}

	// Next burst starts over
	delay := s.NextDelay()
	if delay > 10*time.Millisecond {
		t.Errorf("expected short delay for new burst, got %v", delay)
	}
}

func TestBurstScheduler_Name(t *testing.T) {
	s := NewBurstScheduler(10, time.Second)
	if s.Name() != "burst" {
		t.Errorf("expected name 'burst', got %s", s.Name())
	}
}

func TestNewScheduler(t *testing.T) {
	tests := []struct {
		mode         TimingMode
		expectedName string
	}{
		{TimingModePoisson, "poisson"},
		{TimingModeUniform, "uniform"},
		{TimingModeBurst, "burst"},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			cfg := &Config{
				TimingMode:    tt.mode,
				PoissonLambda: 100,
				MinInterval:   10 * time.Millisecond,
				MaxInterval:   100 * time.Millisecond,
				BurstCount:    10,
				BurstPause:    time.Second,
			}
			s := NewScheduler(cfg)
			if s.Name() != tt.expectedName {
				t.Errorf("expected scheduler name %q, got %q", tt.expectedName, s.Name())
			}
		})
	}
}
