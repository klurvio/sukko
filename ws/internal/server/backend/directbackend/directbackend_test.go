package directbackend

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/server/backend"
	"github.com/Toniq-Labs/odin-ws/internal/server/broadcast"
)

// Compile-time interface check.
var _ backend.MessageBackend = (*DirectBackend)(nil)

// mockBus implements broadcast.Bus and records Publish calls for assertions.
type mockBus struct {
	mu       sync.Mutex
	messages []*broadcast.Message
}

func (m *mockBus) Publish(msg *broadcast.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

func (m *mockBus) Subscribe() <-chan *broadcast.Message { return make(chan *broadcast.Message) }
func (m *mockBus) Run()                                {}
func (m *mockBus) Shutdown()                            {}
func (m *mockBus) ShutdownWithContext(context.Context)   {}
func (m *mockBus) IsHealthy() bool                      { return true }
func (m *mockBus) GetMetrics() broadcast.Metrics        { return broadcast.Metrics{} }

func (m *mockBus) published() []*broadcast.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*broadcast.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bus     broadcast.Bus
		wantErr bool
	}{
		{
			name:    "nil bus returns error",
			bus:     nil,
			wantErr: true,
		},
		{
			name:    "valid bus succeeds",
			bus:     &mockBus{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := New(tt.bus, zerolog.Nop())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if db != nil {
					t.Fatal("expected nil DirectBackend when error is returned")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if db == nil {
				t.Fatal("expected non-nil DirectBackend")
			}
		})
	}
}

func TestStart(t *testing.T) {
	t.Parallel()

	db, err := New(&mockBus{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := db.Start(context.Background()); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
}

func TestPublish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		clientID int64
		channel  string
		data     []byte
	}{
		{
			name:     "simple message",
			clientID: 1,
			channel:  "BTC.trade",
			data:     []byte(`{"price":"50000"}`),
		},
		{
			name:     "empty payload",
			clientID: 2,
			channel:  "ETH.ticker",
			data:     []byte{},
		},
		{
			name:     "nil payload",
			clientID: 3,
			channel:  "XRP.depth",
			data:     nil,
		},
		{
			name:     "large client ID",
			clientID: 9999999,
			channel:  "DOGE.trade",
			data:     []byte(`{"amount":"1000000"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bus := &mockBus{}
			db, err := New(bus, zerolog.Nop())
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if err := db.Publish(context.Background(), tt.clientID, tt.channel, tt.data); err != nil {
				t.Fatalf("Publish returned unexpected error: %v", err)
			}

			msgs := bus.published()
			if len(msgs) != 1 {
				t.Fatalf("expected 1 published message, got %d", len(msgs))
			}

			msg := msgs[0]
			if msg.Subject != tt.channel {
				t.Errorf("Subject: got %q, want %q", msg.Subject, tt.channel)
			}
			if string(msg.Payload) != string(tt.data) {
				t.Errorf("Payload: got %q, want %q", msg.Payload, tt.data)
			}
		})
	}
}

func TestPublish_EmptyChannel(t *testing.T) {
	t.Parallel()

	db, err := New(&mockBus{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = db.Publish(context.Background(), 1, "", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty channel, got nil")
	}
	if !errors.Is(err, backend.ErrPublishFailed) {
		t.Errorf("error = %v, want wrapping %v", err, backend.ErrPublishFailed)
	}
}

func TestReplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  backend.ReplayRequest
	}{
		{
			name: "empty request",
			req:  backend.ReplayRequest{},
		},
		{
			name: "request with positions",
			req: backend.ReplayRequest{
				Positions:     map[string]int64{"topic-a": 42},
				MaxMessages:   100,
				Subscriptions: []string{"BTC.trade"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := New(&mockBus{}, zerolog.Nop())
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			msgs, err := db.Replay(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Replay returned unexpected error: %v", err)
			}
			if msgs != nil {
				t.Fatalf("expected nil messages, got %v", msgs)
			}
		})
	}
}

func TestIsHealthy(t *testing.T) {
	t.Parallel()

	db, err := New(&mockBus{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !db.IsHealthy() {
		t.Fatal("IsHealthy: expected true, got false")
	}
}

func TestShutdown(t *testing.T) {
	t.Parallel()

	db, err := New(&mockBus{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := db.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned unexpected error: %v", err)
	}
}
