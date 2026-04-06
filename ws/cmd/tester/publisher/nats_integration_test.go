//go:build integration

package publisher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
)

func TestNATSPublisher_PublishAndFetch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start NATS container with JetStream
	container, err := tcnats.Run(ctx, "nats:2.10", tcnats.WithArgument("--js"))
	if err != nil {
		t.Skipf("Docker required for integration tests: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("get NATS URL: %v", err)
	}

	// Create JetStream stream covering test subjects
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect to NATS: %v", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("create jetstream: %v", err)
	}

	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "TEST",
		Subjects: []string{"test.>"},
	})
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	// Create NATSPublisher
	pub, err := NewNATSPublisher(url)
	if err != nil {
		t.Fatalf("create nats publisher: %v", err)
	}
	defer pub.Close() //nolint:errcheck

	// Publish a test message
	gen := NewGenerator()
	channel := "test.BTC.trade"
	data, err := gen.Next(channel)
	if err != nil {
		t.Fatalf("generate message: %v", err)
	}

	if err := pub.Publish(ctx, channel, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Fetch and verify
	cons, err := js.CreateConsumer(ctx, "TEST", jetstream.ConsumerConfig{
		FilterSubject: "test.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}

	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	var received int
	for msg := range msgs.Messages() {
		received++

		// Verify subject matches channel
		if msg.Subject() != channel {
			t.Errorf("subject = %q, want %q", msg.Subject(), channel)
		}

		// Verify payload deserializes as TestMessage
		var testMsg TestMessage
		if err := json.Unmarshal(msg.Data(), &testMsg); err != nil {
			t.Fatalf("unmarshal message: %v", err)
		}
		if testMsg.Sequence != 1 {
			t.Errorf("sequence = %d, want 1", testMsg.Sequence)
		}
		if testMsg.Timestamp <= 0 {
			t.Error("timestamp should be positive")
		}

		_ = msg.Ack()
	}

	if received == 0 {
		t.Fatal("expected at least 1 message, got 0")
	}
}

func TestNATSPublisher_ConnectionFailure(t *testing.T) {
	t.Parallel()

	_, err := NewNATSPublisher("nats://localhost:19999")
	if err == nil {
		t.Error("expected error connecting to unreachable NATS server")
	}
}
