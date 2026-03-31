package publisher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSPublisher publishes messages to NATS JetStream.
// Channel names are used directly as NATS subjects (no topic resolution needed).
type NATSPublisher struct {
	conn      *nats.Conn
	js        jetstream.JetStream
	closeOnce sync.Once
}

// NewNATSPublisher connects to NATS and creates a JetStream context.
func NewNATSPublisher(urls string) (*NATSPublisher, error) {
	servers := splitNATSURLs(urls)
	if len(servers) == 0 {
		return nil, errors.New("nats publisher: no URLs provided")
	}

	conn, err := nats.Connect(strings.Join(servers, ","))
	if err != nil {
		return nil, fmt.Errorf("nats publisher: connect: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nats publisher: create jetstream: %w", err)
	}

	return &NATSPublisher{
		conn: conn,
		js:   js,
	}, nil
}

// Publish sends a message to the channel as a NATS JetStream subject.
// In Sukko's JetStream backend, the channel IS the NATS subject directly.
func (p *NATSPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	_, err := p.js.Publish(ctx, channel, payload)
	if err != nil {
		return fmt.Errorf("nats publish to %s: %w", channel, err)
	}
	return nil
}

// Close flushes pending messages and closes the NATS connection. Safe to call multiple times.
func (p *NATSPublisher) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if err := p.conn.Drain(); err != nil {
			// Drain failed — force close
			p.conn.Close()
			closeErr = fmt.Errorf("nats drain: %w", err)
		}
	})
	return closeErr
}

func splitNATSURLs(urls string) []string {
	var result []string
	for u := range strings.SplitSeq(urls, ",") {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// Ensure NATSPublisher implements Publisher.
var _ Publisher = (*NATSPublisher)(nil)
