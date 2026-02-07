package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// Publisher handles sending messages to Kafka.
type Publisher struct {
	client *kgo.Client
	log    zerolog.Logger
	mu     sync.Mutex
	closed bool
}

// NewPublisher creates a new Kafka publisher.
func NewPublisher(cfg *Config, log zerolog.Logger) (*Publisher, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.GetBrokers()...),
		kgo.ProducerBatchCompression(kgo.Lz4Compression()),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	}

	// Configure SASL if enabled
	if cfg.KafkaSASLMechanism != "" {
		mechanism := strings.ToLower(cfg.KafkaSASLMechanism)
		var auth scram.Auth
		auth.User = cfg.KafkaSASLUsername
		auth.Pass = cfg.KafkaSASLPassword

		switch mechanism {
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(auth.AsSha256Mechanism()))
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(auth.AsSha512Mechanism()))
		default:
			return nil, fmt.Errorf("unsupported SASL mechanism: %s", mechanism)
		}
	}

	// Configure TLS if enabled
	if cfg.KafkaTLSEnabled {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
		}))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(context.Background()); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	log.Info().
		Strs("brokers", cfg.GetBrokers()).
		Str("namespace", cfg.KafkaNamespace).
		Bool("sasl", cfg.KafkaSASLMechanism != "").
		Bool("tls", cfg.KafkaTLSEnabled).
		Msg("connected to Kafka")

	return &Publisher{client: client, log: log}, nil
}

// Publish sends a message to Kafka.
func (p *Publisher) Publish(ctx context.Context, msg *Message) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.Unlock()

	record := &kgo.Record{
		Topic: msg.Topic,
		Key:   []byte(msg.Key),
		Value: msg.Payload,
	}

	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("failed to produce message: %w", err)
	}

	return nil
}

// Close shuts down the publisher.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	ctx := context.Background()
	if err := p.client.Flush(ctx); err != nil {
		p.log.Warn().Err(err).Msg("error flushing messages on close")
	}

	p.client.Close()
	p.log.Info().Msg("publisher closed")
	return nil
}
