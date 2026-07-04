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

	kafkashared "github.com/klurvio/sukko/internal/shared/kafka"
)

// testerSourceValue identifies records produced by the tester in the source header.
const testerSourceValue = "sukko-tester"

// KafkaPublisher produces messages to Kafka/Redpanda topics using franz-go.
// Uses the same wire format as ws-server's producer: record key = tenant-qualified
// channel, value = JSON payload, headers = [x-sukko-channel, source, timestamp].
type KafkaPublisher struct {
	client    *kgo.Client
	resolver  *TopicResolver
	closeOnce sync.Once
}

// NewKafkaPublisher connects to Kafka brokers and returns a publisher.
// The resolver handles channel → topic name resolution.
func NewKafkaPublisher(brokers string, resolver *TopicResolver, sasl *kafkashared.SASLConfig, tlsCfg *kafkashared.TLSConfig) (*KafkaPublisher, error) {
	seeds := splitBrokers(brokers)
	if len(seeds) == 0 {
		return nil, errors.New("kafka publisher: no brokers provided")
	}

	opts, err := kafkashared.BuildKgoOpts(seeds, sasl, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("kafka publisher: build opts: %w", err)
	}
	opts = append(opts, kgo.ProducerBatchCompression(kgo.NoCompression()))

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("kafka publisher: create client: %w", err)
	}

	return &KafkaPublisher{
		client:   client,
		resolver: resolver,
	}, nil
}

// Publish produces a message to the resolved Kafka topic.
// Wire format matches ws-server's producer: key = tenant-qualified channel (Community
// key-based routing fallback), value = payload, headers = [x-sukko-channel (required
// for Pro/Enterprise channel-header routing), source, timestamp]. The channel MUST be
// tenant-qualified ("<tenant>.<channel>") — the consumer requires the header's tenant
// prefix to match the topic's tenant segment.
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
			{Key: kafkashared.HeaderChannel, Value: []byte(channel)},
			{Key: kafkashared.HeaderSource, Value: []byte(testerSourceValue)},
			{Key: kafkashared.HeaderTimestamp, Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
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
