package publisher

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

type Generator struct {
	sequence atomic.Int64
}

func NewGenerator() *Generator {
	return &Generator{}
}

type TestMessage struct {
	Sequence  int64  `json:"seq"`
	Timestamp int64  `json:"ts"`
	Payload   string `json:"payload"`
}

func (g *Generator) Next(channel string) (json.RawMessage, error) {
	seq := g.sequence.Add(1)
	msg := TestMessage{
		Sequence:  seq,
		Timestamp: time.Now().UnixMicro(),
		Payload:   fmt.Sprintf("test-message-%d", seq),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal test message: %w", err)
	}
	return data, nil
}

func (g *Generator) Reset() {
	g.sequence.Store(0)
}
