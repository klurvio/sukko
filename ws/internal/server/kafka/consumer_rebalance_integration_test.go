//go:build integration

package kafka

import (
	"context"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TestRebalance_CommitOnLeaveGroup verifies that OnPartitionsRevoked commits
// marked offsets to the broker when partitions are revoked via LeaveGroup.
//
// Test flow:
//  1. Start Redpanda container.
//  2. Produce 10 records to a test topic.
//  3. Start a Consumer with a BroadcastFunc that signals a channel after each broadcast.
//  4. Block until ≥ 1 record is broadcast (consumer is actively processing).
//  5. Trigger OnPartitionsRevoked via client.LeaveGroup().
//  6. Query committed offsets via kadm.FetchOffsetsForTopics.
//  7. Assert committed offset > 0.
func TestRebalance_CommitOnLeaveGroup(t *testing.T) {
	// Docker availability guard — skip cleanly rather than failing at container creation
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is not available — skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start Redpanda container
	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.1.1")
	if err != nil {
		t.Skipf("failed to start Redpanda container (Docker required): %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	broker, err := container.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("get broker address: %v", err)
	}

	const (
		topicName  = "sukko.test.trade"
		groupID    = "test-rebalance-group"
		numRecords = 10
	)

	// Create the topic before producing
	adminClient, err := kgo.NewClient(kgo.SeedBrokers(broker))
	if err != nil {
		t.Fatalf("create admin kgo client: %v", err)
	}
	adm := kadm.NewClient(adminClient)
	if _, err := adm.CreateTopics(ctx, 1, 1, nil, topicName); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	adminClient.Close()

	// Produce 10 records to the topic
	producer, err := kgo.NewClient(kgo.SeedBrokers(broker))
	if err != nil {
		t.Fatalf("create producer: %v", err)
	}
	for i := 0; i < numRecords; i++ {
		if err := producer.ProduceSync(ctx, &kgo.Record{
			Topic: topicName,
			Key:   []byte("BTC.trade"),
			Value: []byte(`{"price":"50000"}`),
		}).FirstErr(); err != nil {
			t.Fatalf("produce record %d: %v", i, err)
		}
	}
	producer.Close()

	// Signal channel to track broadcast delivery
	broadcastCount := atomic.Int32{}
	broadcastSignal := make(chan struct{}, numRecords)
	broadcastFn := func(_ string, _ []byte, _ string, _ int32, _ int64) {
		broadcastCount.Add(1)
		select {
		case broadcastSignal <- struct{}{}:
		default:
		}
	}

	// Create Consumer
	logger := zerolog.Nop()
	reg := prometheus.NewRegistry()
	guard := &mockResourceGuardFixed{allowKafka: true}

	consumer, err := NewConsumer(ConsumerConfig{
		Brokers:               []string{broker},
		ConsumerGroup:         groupID,
		Topics:                []string{topicName},
		Logger:                &logger,
		Broadcast:             broadcastFn,
		ResourceGuard:         guard,
		ConsumerType:          ConsumerTypeKindShared,
		CommitOnRevokeTimeout: 10 * time.Second,
		AutoCommitInterval:    1 * time.Second,
		Registerer:            reg,
	})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	if err := consumer.Start(); err != nil {
		t.Fatalf("consumer.Start: %v", err)
	}

	// Wait for at least 1 record to be broadcast
	select {
	case <-broadcastSignal:
		// At least 1 record processed
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for first broadcast — consumer may not be connected")
	}

	// Trigger partition revoke via LeaveGroup — fires OnPartitionsRevoked synchronously
	consumer.client.LeaveGroup()

	// Give a moment for the commit to propagate to the broker
	time.Sleep(500 * time.Millisecond)

	// Create a fresh admin client to query committed offsets
	queryClient, err := kgo.NewClient(kgo.SeedBrokers(broker))
	if err != nil {
		t.Fatalf("create query client: %v", err)
	}
	defer queryClient.Close()

	adm3 := kadm.NewClient(queryClient)
	offsets, err := adm3.FetchOffsetsForTopics(ctx, groupID, map[string][]int32{topicName: nil})
	if err != nil {
		t.Fatalf("FetchOffsetsForTopics: %v", err)
	}

	// Assert that at least one partition has a committed offset > 0
	var maxCommitted int64
	offsets.Each(func(o kadm.OffsetResponse) {
		if o.Err == nil && o.Offset.At > maxCommitted {
			maxCommitted = o.Offset.At
		}
	})

	if maxCommitted <= 0 {
		t.Errorf("expected committed offset > 0, got %d (OnPartitionsRevoked may not have committed)", maxCommitted)
	}

	t.Logf("broadcast count: %d, committed offset: %d", broadcastCount.Load(), maxCommitted)

	_ = consumer.Stop()
}
