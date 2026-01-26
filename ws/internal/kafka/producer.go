// Package kafka provides Kafka/Redpanda integration for the WebSocket server.
// This file contains the Producer for publishing client messages to Kafka.
package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// TopicClientEvents is the base topic name for client-generated events.
// All client publishes go to this single ingress topic (industry standard pattern).
// Full topic name: odin.{namespace}.client-events
const TopicClientEvents = "client-events"

// ProducerStats holds metrics for the producer.
type ProducerStats struct {
	MessagesPublished int64 // Successfully published messages
	MessagesFailed    int64 // Failed publish attempts
}

// ProducerConfig holds configuration for creating a Kafka producer.
type ProducerConfig struct {
	Brokers        []string        // Kafka/Redpanda broker addresses (required)
	TopicNamespace string          // Topic namespace: "local", "dev", "staging", "main" (required)
	ClientID       string          // Client ID for Kafka connection (optional, defaults to "odin-ws-producer-{hostname}")
	Logger         *zerolog.Logger // Structured logger (optional)

	// Security configuration for managed Kafka/Redpanda services
	SASL *SASLConfig // SASL authentication (nil = no auth, for local dev)
	TLS  *TLSConfig  // TLS encryption (nil = no TLS, for local dev)

	// Producer tuning (optional - sensible defaults provided)
	BatchMaxBytes   int32         // Max batch size in bytes (default: 1MB)
	MaxBufferedRecs int           // Max buffered records (default: 10000)
	RecordRetries   int           // Number of retries (default: 8)
	ProduceTimeout  time.Duration // Timeout for produce operations (default: 10s)
}

// Producer wraps franz-go client for publishing to Kafka/Redpanda.
// It provides a simple interface for WebSocket clients to publish messages.
//
// Thread Safety: All public methods are safe for concurrent use.
//
// Lifecycle: Create with NewProducer, use Publish to send messages, Close when done.
type Producer struct {
	client         *kgo.Client
	topicNamespace string
	topic          string // Pre-computed full topic name
	logger         *zerolog.Logger
	stats          ProducerStats
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
	closed         bool
}

// NewProducer creates a new Kafka producer with the provided configuration.
// It validates required fields and initializes the franz-go client with
// optimal settings for client message publishing.
//
// Returns an error if any required field is missing or client creation fails.
func NewProducer(cfg ProducerConfig) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("at least one broker is required")
	}
	if cfg.TopicNamespace == "" {
		return nil, errors.New("topic namespace is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Set defaults
	// Auto-generate client ID with hostname for per-instance observability
	// Example: "odin-ws-producer-odin-ws-7f8d9c4b5-abc123" in Kubernetes
	clientID := cfg.ClientID
	if clientID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		clientID = "odin-ws-producer-" + hostname
	}
	batchMaxBytes := cfg.BatchMaxBytes
	if batchMaxBytes == 0 {
		batchMaxBytes = 1024 * 1024 // 1MB default
	}
	maxBufferedRecs := cfg.MaxBufferedRecs
	if maxBufferedRecs == 0 {
		maxBufferedRecs = 10000
	}
	recordRetries := cfg.RecordRetries
	if recordRetries == 0 {
		recordRetries = 8
	}

	// Build client options
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(clientID),
		kgo.ProducerBatchMaxBytes(batchMaxBytes),
		kgo.MaxBufferedRecords(maxBufferedRecs),
		kgo.RecordRetries(recordRetries),
		kgo.RetryBackoffFn(func(attempt int) time.Duration {
			// Exponential backoff: 100ms, 200ms, 400ms, 800ms, ...
			return time.Duration(100*(1<<attempt)) * time.Millisecond
		}),
	}

	// Add SASL authentication if configured
	if cfg.SASL != nil {
		mechanism := scram.Auth{
			User: cfg.SASL.Username,
			Pass: cfg.SASL.Password,
		}

		switch cfg.SASL.Mechanism {
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(mechanism.AsSha256Mechanism()))
			if cfg.Logger != nil {
				cfg.Logger.Info().
					Str("mechanism", "SCRAM-SHA-256").
					Str("username", cfg.SASL.Username).
					Msg("Kafka producer SASL authentication enabled")
			}
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(mechanism.AsSha512Mechanism()))
			if cfg.Logger != nil {
				cfg.Logger.Info().
					Str("mechanism", "SCRAM-SHA-512").
					Str("username", cfg.SASL.Username).
					Msg("Kafka producer SASL authentication enabled")
			}
		default:
			cancel()
			return nil, fmt.Errorf("unsupported SASL mechanism: %s (use scram-sha-256 or scram-sha-512)", cfg.SASL.Mechanism)
		}
	}

	// Add TLS encryption if configured
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, //nolint:gosec // Controlled by configuration for dev/testing environments
			MinVersion:         tls.VersionTLS12,
		}

		// Load CA certificate if provided
		if cfg.TLS.CAPath != "" {
			caCert, err := os.ReadFile(cfg.TLS.CAPath)
			if err != nil {
				cancel()
				return nil, fmt.Errorf("failed to read CA certificate from %s: %w", cfg.TLS.CAPath, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				cancel()
				return nil, fmt.Errorf("failed to parse CA certificate from %s", cfg.TLS.CAPath)
			}
			tlsCfg.RootCAs = caCertPool
		}

		opts = append(opts, kgo.DialTLSConfig(tlsCfg))
		if cfg.Logger != nil {
			cfg.Logger.Info().
				Bool("insecure_skip_verify", cfg.TLS.InsecureSkipVerify).
				Str("ca_path", cfg.TLS.CAPath).
				Msg("Kafka producer TLS encryption enabled")
		}
	}

	// Create franz-go client
	client, err := kgo.NewClient(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create kafka producer client: %w", err)
	}

	// Pre-compute the full topic name
	topic := GetTopic(cfg.TopicNamespace, TopicClientEvents)

	producer := &Producer{
		client:         client,
		topicNamespace: NormalizeEnv(cfg.TopicNamespace),
		topic:          topic,
		logger:         cfg.Logger,
		ctx:            ctx,
		cancel:         cancel,
	}

	if cfg.Logger != nil {
		cfg.Logger.Info().
			Strs("brokers", cfg.Brokers).
			Str("topic", topic).
			Str("client_id", clientID).
			Msg("Kafka producer initialized")
	}

	return producer, nil
}

// Publish sends a client message to Kafka.
//
// The message is published to the client-events topic with:
//   - Key: channel string (for partitioning - same channel = same partition = ordering)
//   - Value: raw JSON from client (no parsing/validation)
//   - Headers: metadata (client_id, source, timestamp)
//
// This follows the industry standard pattern of using a single ingress topic
// for client-generated events, with the channel as the partition key.
func (p *Producer) Publish(ctx context.Context, clientID int64, channel string, data []byte) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return errors.New("producer is closed")
	}
	topic := p.topic
	p.mu.RUnlock()

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(channel),
		Value: data,
		Headers: []kgo.RecordHeader{
			{Key: "client_id", Value: []byte(strconv.FormatInt(clientID, 10))},
			{Key: "source", Value: []byte("ws-server")},
			{Key: "timestamp", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))},
		},
	}

	// Use synchronous produce for reliability (client expects ack)
	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		atomic.AddInt64(&p.stats.MessagesFailed, 1)
		if p.logger != nil {
			p.logger.Error().
				Err(err).
				Int64("client_id", clientID).
				Str("channel", channel).
				Str("topic", topic).
				Msg("Failed to publish message to Kafka")
		}
		return fmt.Errorf("kafka produce failed: %w", err)
	}

	atomic.AddInt64(&p.stats.MessagesPublished, 1)

	if p.logger != nil {
		p.logger.Debug().
			Int64("client_id", clientID).
			Str("channel", channel).
			Str("topic", topic).
			Int("data_size", len(data)).
			Msg("Published message to Kafka")
	}

	return nil
}

// Stats returns a copy of the current producer statistics.
func (p *Producer) Stats() ProducerStats {
	return ProducerStats{
		MessagesPublished: atomic.LoadInt64(&p.stats.MessagesPublished),
		MessagesFailed:    atomic.LoadInt64(&p.stats.MessagesFailed),
	}
}

// Topic returns the full topic name this producer publishes to.
func (p *Producer) Topic() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.topic
}

// Close gracefully shuts down the producer.
// It flushes any buffered messages and closes the Kafka client.
func (p *Producer) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	p.cancel()
	p.client.Close()

	if p.logger != nil {
		stats := p.Stats()
		p.logger.Info().
			Int64("messages_published", stats.MessagesPublished).
			Int64("messages_failed", stats.MessagesFailed).
			Msg("Kafka producer closed")
	}

	return nil
}
