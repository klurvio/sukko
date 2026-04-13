package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
)

func TestConnect_InvalidURL(t *testing.T) {
	t.Parallel()

	_, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: "not-a-url",
		Logger:     zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestConnect_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Connect(ctx, ConnectConfig{
		GatewayURL: "ws://localhost:59999",
		Logger:     zerolog.Nop(),
	})
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestClient_CloseIdempotent(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, err := wsutil.ReadClientText(conn); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second close should be no-op, got: %v", err)
	}
}

func TestClient_Subscribe(t *testing.T) {
	t.Parallel()

	receivedCh := make(chan map[string]any, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()

		data, err := wsutil.ReadClientText(conn)
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		receivedCh <- msg
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if err := client.Subscribe([]string{"ch.1", "ch.2"}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case received := <-receivedCh:
		if received["type"] != "subscribe" {
			t.Errorf("type = %v, want subscribe", received["type"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive message")
	}
}

func TestClient_RefreshToken(t *testing.T) {
	t.Parallel()

	receivedCh := make(chan map[string]any, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()

		data, err := wsutil.ReadClientText(conn)
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		receivedCh <- msg
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if err := client.RefreshToken("eyJnew.token.here"); err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}

	select {
	case received := <-receivedCh:
		if received["type"] != "auth" {
			t.Errorf("type = %v, want auth", received["type"])
		}
		data, ok := received["data"].(map[string]any)
		if !ok {
			t.Fatal("data is not a map")
		}
		if data["token"] != "eyJnew.token.here" {
			t.Errorf("token = %v, want eyJnew.token.here", data["token"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive message")
	}
}

func TestClient_Publish(t *testing.T) {
	t.Parallel()

	receivedCh := make(chan map[string]any, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()

		data, err := wsutil.ReadClientText(conn)
		if err != nil {
			return
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		receivedCh <- msg
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if err := client.Publish("ch.1", json.RawMessage(`{"v":1}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case received := <-receivedCh:
		if received["type"] != "publish" {
			t.Errorf("type = %v, want publish", received["type"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive message")
	}
}

func TestClient_ReadLoop_OnMessage(t *testing.T) {
	t.Parallel()

	serverMsg := Message{
		Type:    "message",
		Channel: "test.ch",
		Data:    json.RawMessage(`{"x":1}`),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()

		payload, _ := json.Marshal(serverMsg)
		_ = wsutil.WriteServerText(conn, payload)

		// Keep connection open briefly to let ReadLoop process
		for {
			if _, err := wsutil.ReadClientText(conn); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	received := make(chan Message, 1)
	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
		OnMessage:  func(m Message) { received <- m },
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = client.ReadLoop(ctx) }()

	select {
	case msg := <-received:
		if msg.Type != "message" {
			t.Errorf("type = %q, want message", msg.Type)
		}
		if msg.Channel != "test.ch" {
			t.Errorf("channel = %q, want test.ch", msg.Channel)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	cancel()
	client.Close()
}

func TestClient_ReadLoop_CloseCode(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send close frame with code 1008 (Policy Violation)
		closeBody := ws.NewCloseFrameBody(ws.StatusPolicyViolation, "token revoked")
		frame := ws.NewCloseFrame(closeBody)
		if err := ws.WriteFrame(conn, frame); err != nil {
			return
		}
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	code, err := client.ReadLoop(context.Background())
	if err != nil {
		t.Fatalf("ReadLoop error: %v", err)
	}
	if code != ws.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (PolicyViolation)", code, ws.StatusPolicyViolation)
	}
}

func TestClient_ReadLoop_ContextCancel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := ws.HTTPUpgrader{}
		conn, _, _, err := upgrader.Upgrade(r, w)
		if err != nil {
			return
		}
		defer conn.Close()
		// Keep connection open — never send anything
		select {}
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:]
	client, err := Connect(context.Background(), ConnectConfig{
		GatewayURL: wsURL,
		Logger:     zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	var code ws.StatusCode
	var readErr error
	go func() {
		code, readErr = client.ReadLoop(ctx)
		close(done)
	}()

	// Cancel context — ReadLoop should return
	cancel()

	select {
	case <-done:
		if readErr != nil {
			t.Errorf("ReadLoop error: %v", readErr)
		}
		if code != 0 {
			t.Errorf("close code = %d, want 0 (context cancel)", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ReadLoop to return after context cancel")
	}
}
