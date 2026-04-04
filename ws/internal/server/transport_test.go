package server

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	serverv1 "github.com/klurvio/sukko/gen/proto/sukko/server/v1"
	"google.golang.org/grpc/metadata"
)

// mockConn implements net.Conn for testing WebSocketTransport.
type mockConn struct {
	written    []byte
	writeErr   error
	closeErr   error
	closed     bool
	remoteAddr net.Addr
}

func (m *mockConn) Read(b []byte) (n int, err error) { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.written = append(m.written, b...)
	return len(b), nil
}
func (m *mockConn) Close() error {
	m.closed = true
	return m.closeErr
}
func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return m.remoteAddr }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func newMockConn() *mockConn {
	return &mockConn{
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345},
	}
}

func TestWebSocketTransport_Send(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		msg       OutgoingMsg
		writeErr  error
		wantBytes int
		wantErr   bool
	}{
		{
			name:      "raw message",
			msg:       RawMsg([]byte(`{"type":"ack"}`)),
			wantBytes: 14,
		},
		{
			name:      "nil message",
			msg:       OutgoingMsg{}, // zero value — Bytes() returns nil
			wantBytes: 0,
		},
		{
			name:     "write error on flush",
			msg:      RawMsg([]byte(`test`)),
			writeErr: errors.New("connection reset"),
			wantErr:  true,
			// wsutil writes to bufio.Writer (succeeds in buffer),
			// error surfaces on Flush() — bytes were "written" to buffer
			wantBytes: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn := newMockConn()
			conn.writeErr = tt.writeErr
			transport := NewWebSocketTransport(conn)

			n, err := transport.Send(tt.msg)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if n != tt.wantBytes {
				t.Errorf("bytes = %d, want %d", n, tt.wantBytes)
			}
		})
	}
}

func TestWebSocketTransport_SendBatch(t *testing.T) {
	t.Parallel()

	conn := newMockConn()
	transport := NewWebSocketTransport(conn)

	msgs := []OutgoingMsg{
		RawMsg([]byte(`{"type":"msg1"}`)),
		{}, // nil — should be skipped
		RawMsg([]byte(`{"type":"msg2"}`)),
	}

	n, err := transport.SendBatch(msgs)
	if err != nil {
		t.Fatalf("SendBatch error: %v", err)
	}

	// Two messages written (nil skipped)
	wantBytes := len(`{"type":"msg1"}`) + len(`{"type":"msg2"}`)
	if n != wantBytes {
		t.Errorf("bytes = %d, want %d", n, wantBytes)
	}
}

func TestWebSocketTransport_SendBatch_WriteError(t *testing.T) {
	t.Parallel()

	conn := newMockConn()
	conn.writeErr = errors.New("broken pipe")
	transport := NewWebSocketTransport(conn)

	msgs := []OutgoingMsg{
		RawMsg([]byte(`test`)),
	}

	_, err := transport.SendBatch(msgs)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestWebSocketTransport_Close(t *testing.T) {
	t.Parallel()

	conn := newMockConn()
	transport := NewWebSocketTransport(conn)

	err := transport.Close()
	if err != nil {
		t.Errorf("Close error: %v", err)
	}
	if !conn.closed {
		t.Error("expected conn to be closed")
	}
}

func TestWebSocketTransport_Close_NilConn(t *testing.T) {
	t.Parallel()

	transport := &WebSocketTransport{conn: nil}
	err := transport.Close()
	if err != nil {
		t.Errorf("Close on nil conn should return nil, got: %v", err)
	}
}

func TestWebSocketTransport_Type(t *testing.T) {
	t.Parallel()

	conn := newMockConn()
	transport := NewWebSocketTransport(conn)

	if transport.Type() != TransportWebSocket {
		t.Errorf("Type() = %q, want %q", transport.Type(), TransportWebSocket)
	}
}

func TestWebSocketTransport_RemoteAddr(t *testing.T) {
	t.Parallel()

	conn := newMockConn()
	transport := NewWebSocketTransport(conn)

	addr := transport.RemoteAddr()
	if addr == "" {
		t.Error("RemoteAddr() should not be empty")
	}
}

func TestWebSocketTransport_RemoteAddr_NilConn(t *testing.T) {
	t.Parallel()

	transport := &WebSocketTransport{conn: nil}
	if transport.RemoteAddr() != "" {
		t.Error("RemoteAddr() on nil conn should be empty")
	}
}

func TestTransportType_Labels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tt      TransportType
		wantStr string
	}{
		{"websocket", TransportWebSocket, "ws"},
		{"grpc_stream", TransportGRPCStream, "sse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.tt) != tt.wantStr {
				t.Errorf("TransportType = %q, want %q", string(tt.tt), tt.wantStr)
			}
		})
	}
}

func TestClient_TransportType(t *testing.T) {
	t.Parallel()

	t.Run("with transport", func(t *testing.T) {
		t.Parallel()
		conn := newMockConn()
		client := &Client{transport: NewWebSocketTransport(conn)}
		if client.TransportType() != TransportWebSocket {
			t.Errorf("TransportType() = %q, want %q", client.TransportType(), TransportWebSocket)
		}
	})

	t.Run("nil transport", func(t *testing.T) {
		t.Parallel()
		client := &Client{transport: nil}
		if client.TransportType() != "" {
			t.Errorf("TransportType() on nil transport should be empty, got %q", client.TransportType())
		}
	})
}

// =============================================================================
// GRPCStreamTransport Tests
// =============================================================================

// mockSubscribeServer implements RealtimeService_SubscribeServer for testing.
type mockSubscribeServer struct {
	mu      sync.Mutex
	sent    []*serverv1.SubscribeResponse
	sendErr error
}

func (m *mockSubscribeServer) Send(resp *serverv1.SubscribeResponse) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	m.sent = append(m.sent, resp)
	m.mu.Unlock()
	return nil
}

// Implement the grpc.ServerStreamingServer[SubscribeResponse] interface stubs.
func (m *mockSubscribeServer) SendHeader(_ metadata.MD) error { return nil }
func (m *mockSubscribeServer) SetHeader(_ metadata.MD) error  { return nil }
func (m *mockSubscribeServer) SetTrailer(_ metadata.MD)       {}
func (m *mockSubscribeServer) Context() context.Context       { return context.Background() }
func (m *mockSubscribeServer) SendMsg(_ any) error            { return nil }
func (m *mockSubscribeServer) RecvMsg(_ any) error            { return nil }

func TestGRPCStreamTransport_Send(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		msg       OutgoingMsg
		sendErr   error
		wantBytes int
		wantErr   bool
		wantSent  int
	}{
		{
			name:      "raw message",
			msg:       RawMsg([]byte(`{"type":"ack"}`)),
			wantBytes: 14,
			wantSent:  1,
		},
		{
			name:      "nil message",
			msg:       OutgoingMsg{},
			wantBytes: 0,
			wantSent:  0,
		},
		{
			name:     "send error",
			msg:      RawMsg([]byte(`test`)),
			sendErr:  errors.New("stream closed"),
			wantErr:  true,
			wantSent: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &mockSubscribeServer{sendErr: tt.sendErr}
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			transport := NewGRPCStreamTransport(mock, cancel, "192.168.1.1:12345")

			n, err := transport.Send(tt.msg)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if n != tt.wantBytes {
				t.Errorf("bytes = %d, want %d", n, tt.wantBytes)
			}
			if len(mock.sent) != tt.wantSent {
				t.Errorf("sent count = %d, want %d", len(mock.sent), tt.wantSent)
			}
		})
	}
}

func TestGRPCStreamTransport_Send_SequenceAndPayload(t *testing.T) {
	t.Parallel()

	mock := &mockSubscribeServer{}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := NewGRPCStreamTransport(mock, cancel, "10.0.0.1")

	// Create an OutgoingMsg with a sequence number (simulating broadcast message)
	msg := OutgoingMsg{
		raw: []byte(`{"type":"message","seq":42,"channel":"test"}`),
		seq: 42,
	}

	n, err := transport.Send(msg)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if n != len(msg.raw) {
		t.Errorf("bytes = %d, want %d", n, len(msg.raw))
	}
	if len(mock.sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(mock.sent))
	}
	if mock.sent[0].GetSequence() != 42 {
		t.Errorf("sequence = %d, want 42", mock.sent[0].GetSequence())
	}
	if !bytes.Equal(mock.sent[0].GetPayload(), msg.raw) {
		t.Errorf("payload = %q, want %q", mock.sent[0].GetPayload(), msg.raw)
	}
}

func TestGRPCStreamTransport_SendBatch(t *testing.T) {
	t.Parallel()

	mock := &mockSubscribeServer{}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := NewGRPCStreamTransport(mock, cancel, "10.0.0.1")

	msgs := []OutgoingMsg{
		RawMsg([]byte(`msg1`)),
		{}, // nil — skipped
		RawMsg([]byte(`msg2`)),
	}

	n, err := transport.SendBatch(msgs)
	if err != nil {
		t.Fatalf("SendBatch error: %v", err)
	}
	wantBytes := len("msg1") + len("msg2")
	if n != wantBytes {
		t.Errorf("bytes = %d, want %d", n, wantBytes)
	}
	if len(mock.sent) != 2 {
		t.Errorf("sent count = %d, want 2", len(mock.sent))
	}
}

func TestGRPCStreamTransport_Close(t *testing.T) {
	t.Parallel()

	canceled := false
	transport := NewGRPCStreamTransport(
		&mockSubscribeServer{},
		func() { canceled = true },
		"10.0.0.1",
	)

	err := transport.Close()
	if err != nil {
		t.Errorf("Close error: %v", err)
	}
	if !canceled {
		t.Error("expected cancel to be called")
	}
}

func TestGRPCStreamTransport_Type(t *testing.T) {
	t.Parallel()

	transport := &GRPCStreamTransport{}
	if transport.Type() != TransportGRPCStream {
		t.Errorf("Type() = %q, want %q", transport.Type(), TransportGRPCStream)
	}
}

func TestGRPCStreamTransport_RemoteAddr(t *testing.T) {
	t.Parallel()

	transport := NewGRPCStreamTransport(&mockSubscribeServer{}, func() {}, "192.168.1.100:8080")
	if transport.RemoteAddr() != "192.168.1.100:8080" {
		t.Errorf("RemoteAddr() = %q, want %q", transport.RemoteAddr(), "192.168.1.100:8080")
	}
}

func TestGRPCStreamTransport_NoOps(t *testing.T) {
	t.Parallel()

	transport := &GRPCStreamTransport{}

	if err := transport.WritePing(); err != nil {
		t.Errorf("WritePing should be no-op, got: %v", err)
	}
	if err := transport.WritePong([]byte("test")); err != nil {
		t.Errorf("WritePong should be no-op, got: %v", err)
	}
	if err := transport.SetWriteDeadline(time.Now()); err != nil {
		t.Errorf("SetWriteDeadline should be no-op, got: %v", err)
	}
}
