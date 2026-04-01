package publisher

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// KafkaPublisher produces messages to Kafka/Redpanda topics using franz-go.
// Uses the same wire format as ws-server's producer: record key = channel,
// value = JSON payload, headers include source and timestamp.
type KafkaPublisher struct {
	client    *kgo.Client
	resolver  *TopicResolver
	closeOnce sync.Once
}

// NewKafkaPublisher connects to Kafka brokers and returns a publisher.
// The resolver handles channel → topic name resolution.
func NewKafkaPublisher(brokers string, resolver *TopicResolver) (*KafkaPublisher, error) {
	seeds := splitBrokers(brokers)
	if len(seeds) == 0 {
		return nil, errors.New("kafka publisher: no brokers provided")
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ProducerBatchCompression(kgo.NoCompression()),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka publisher: create client: %w", err)
	}

	return &KafkaPublisher{
		client:   client,
		resolver: resolver,
	}, nil
}

// Publish produces a message to the resolved Kafka topic.
// Wire format matches ws-server: key=channel, value=payload, headers=[source, timestamp].
func (p *KafkaPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	topic, err := p.resolver.Resolve(channel)
	if err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(channel),
		Value: payload,
		Headers: []kgo.RecordHeader{
			{Key: "source", Value: []byte("sukko-tester")},
			{Key: "timestamp", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
		},
	}

	// Synchronous produce — wait for ack
	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("kafka publish to %s: %w", topic, err)
	}

	return nil
}

// Close closes the Kafka client connection. Safe to call multiple times.
func (p *KafkaPublisher) Close() error {
	p.closeOnce.Do(func() {
		p.client.Close()
	})
	return nil
}

func splitBrokers(brokers string) []string {
	var result []string
	for b := range strings.SplitSeq(brokers, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// Ensure KafkaPublisher implements Publisher.
var _ Publisher = (*KafkaPublisher)(nil)
