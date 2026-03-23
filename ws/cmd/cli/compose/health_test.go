package compose

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWaitForHealth_AllHealthy(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	targets := []HealthTarget{
		{Name: "svc-a", URL: srv.URL + "/health"},
		{Name: "svc-b", URL: srv.URL + "/health"},
	}

	var buf bytes.Buffer
	err := WaitForHealth(context.Background(), &buf, targets, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("svc-a: healthy")) {
		t.Error("expected svc-a healthy in output")
	}
	if !bytes.Contains(buf.Bytes(), []byte("svc-b: healthy")) {
		t.Error("expected svc-b healthy in output")
	}
}

func TestWaitForHealth_Timeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	targets := []HealthTarget{
		{Name: "unhealthy-svc", URL: srv.URL + "/health"},
	}

	var buf bytes.Buffer
	err := WaitForHealth(context.Background(), &buf, targets, 3*time.Second)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("unhealthy-svc")) {
		t.Errorf("error should mention unhealthy service, got: %v", err)
	}
}

func TestWaitForHealth_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	targets := []HealthTarget{
		{Name: "svc", URL: srv.URL + "/health"},
	}

	var buf bytes.Buffer
	err := WaitForHealth(ctx, &buf, targets, 30*time.Second)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestWaitForHealth_EmptyTargets(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := WaitForHealth(context.Background(), &buf, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error for empty targets: %v", err)
	}
}

func TestWaitForHealth_PartialHealth(t *testing.T) {
	t.Parallel()

	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthy.Close()

	targets := []HealthTarget{
		{Name: "good", URL: healthy.URL + "/health"},
		{Name: "bad", URL: unhealthy.URL + "/health"},
	}

	var buf bytes.Buffer
	err := WaitForHealth(context.Background(), &buf, targets, 3*time.Second)
	if err == nil {
		t.Fatal("expected timeout error when one service is unhealthy")
	}
	// The healthy service should have been reported
	if !bytes.Contains(buf.Bytes(), []byte("good: healthy")) {
		t.Error("expected healthy service to be reported")
	}
}

func TestWaitForHealth_BecomesHealthy(t *testing.T) {
	t.Parallel()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount <= 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	targets := []HealthTarget{
		{Name: "delayed", URL: srv.URL + "/health"},
	}

	var buf bytes.Buffer
	err := WaitForHealth(context.Background(), &buf, targets, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
