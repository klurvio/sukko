package main

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestStats_RecordSuccess(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	s.RecordSuccess("local.odin.trade", "odin.BTC.trade", 100)
	s.RecordSuccess("local.odin.trade", "odin.BTC.trade", 150)
	s.RecordSuccess("local.odin.liquidity", "odin.ETH.liquidity", 200)

	if s.messagesPublished.Load() != 3 {
		t.Errorf("expected 3 messages, got %d", s.messagesPublished.Load())
	}
	if s.bytesPublished.Load() != 450 {
		t.Errorf("expected 450 bytes, got %d", s.bytesPublished.Load())
	}
	if s.messagesFailed.Load() != 0 {
		t.Errorf("expected 0 failures, got %d", s.messagesFailed.Load())
	}
}

func TestStats_RecordFailure(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	s.RecordFailure()
	s.RecordFailure()
	s.RecordSuccess("topic", "channel", 100)
	s.RecordFailure()

	if s.messagesFailed.Load() != 3 {
		t.Errorf("expected 3 failures, got %d", s.messagesFailed.Load())
	}
	if s.messagesPublished.Load() != 1 {
		t.Errorf("expected 1 message, got %d", s.messagesPublished.Load())
	}
}

func TestStats_TopicCounts(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	s.RecordSuccess("topic1", "ch1", 100)
	s.RecordSuccess("topic1", "ch2", 100)
	s.RecordSuccess("topic2", "ch3", 100)
	s.RecordSuccess("topic1", "ch1", 100)

	// Verify via topicCounts map
	var topic1Count, topic2Count int64
	s.topicCounts.Range(func(k, v any) bool {
		count := v.(*atomic.Int64).Load()
		switch k.(string) {
		case "topic1":
			topic1Count = count
		case "topic2":
			topic2Count = count
		}
		return true
	})

	if topic1Count != 3 {
		t.Errorf("expected topic1 count 3, got %d", topic1Count)
	}
	if topic2Count != 1 {
		t.Errorf("expected topic2 count 1, got %d", topic2Count)
	}
}

func TestStats_ChannelCounts(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	s.RecordSuccess("topic", "ch1", 100)
	s.RecordSuccess("topic", "ch1", 100)
	s.RecordSuccess("topic", "ch2", 100)

	var ch1Count, ch2Count int64
	s.channelCounts.Range(func(k, v any) bool {
		count := v.(*atomic.Int64).Load()
		switch k.(string) {
		case "ch1":
			ch1Count = count
		case "ch2":
			ch2Count = count
		}
		return true
	})

	if ch1Count != 2 {
		t.Errorf("expected ch1 count 2, got %d", ch1Count)
	}
	if ch2Count != 1 {
		t.Errorf("expected ch2 count 1, got %d", ch2Count)
	}
}

func TestStats_Rate(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	for range 100 {
		s.RecordSuccess("topic", "channel", 100)
	}

	time.Sleep(100 * time.Millisecond)

	rate := s.Rate()
	if rate <= 0 {
		t.Errorf("expected positive rate, got %f", rate)
	}
	if rate > 100000 {
		t.Errorf("rate seems too high: %f", rate)
	}
}

func TestStats_Duration(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	time.Sleep(50 * time.Millisecond)

	duration := s.Duration()
	if duration < 50*time.Millisecond {
		t.Errorf("expected duration >= 50ms, got %v", duration)
	}
}

func TestStats_ConcurrentAccess(t *testing.T) {
	log := zerolog.New(os.Stdout).Level(zerolog.Disabled)
	s := NewStats(log)

	done := make(chan bool)
	workers := 10
	iterations := 1000

	for range workers {
		go func() {
			for j := range iterations {
				s.RecordSuccess("topic", "channel", 100)
				if j%3 == 0 {
					s.RecordFailure()
				}
			}
			done <- true
		}()
	}

	for range workers {
		<-done
	}

	expected := int64(workers * iterations)
	if s.messagesPublished.Load() != expected {
		t.Errorf("expected %d published, got %d", expected, s.messagesPublished.Load())
	}
}
