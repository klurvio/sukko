package adminui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorstStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		statuses []string
		want     string
	}{
		{"all ok", []string{"ok", "ok", "ok"}, "ok"},
		{"one unconfigured", []string{"ok", "unconfigured", "ok"}, "unconfigured"},
		{"one degraded", []string{"ok", "degraded", "ok"}, "degraded"},
		{"one unknown", []string{"ok", "unknown", "ok"}, "unknown"},
		{"unknown beats degraded", []string{"degraded", "unknown"}, "unknown"},
		{"unknown beats unconfigured", []string{"unconfigured", "unknown"}, "unknown"},
		{"degraded beats unconfigured", []string{"unconfigured", "degraded"}, "degraded"},
		{"single ok", []string{"ok"}, "ok"},
		{"single unknown", []string{"unknown"}, "unknown"},
		{"empty", []string{}, "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := worstStatus(tt.statuses...)
			if got != tt.want {
				t.Errorf("worstStatus(%v) = %q, want %q", tt.statuses, got, tt.want)
			}
		})
	}
}

func TestProbeHealth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		serverFunc func(w http.ResponseWriter, r *http.Request)
		useEmpty   bool
		want       string
	}{
		{
			name: "2xx → ok",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			want: "ok",
		},
		{
			name: "503 → degraded",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			want: "degraded",
		},
		{
			name: "404 → degraded",
			serverFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			want: "degraded",
		},
		{
			name:     "empty url → unconfigured",
			useEmpty: true,
			want:     "unconfigured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var url string
			if !tt.useEmpty {
				srv := httptest.NewServer(http.HandlerFunc(tt.serverFunc))
				defer srv.Close()
				url = srv.URL
			}
			got := probeHealth(t.Context(), url)
			if got != tt.want {
				t.Errorf("probeHealth(%q) = %q, want %q", url, got, tt.want)
			}
		})
	}
}

func TestProbeHealth_Unreachable(t *testing.T) {
	t.Parallel()
	// Use a server that is immediately closed to simulate unreachable upstream.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	got := probeHealth(t.Context(), url)
	if got != "unknown" {
		t.Errorf("probeHealth(closed server) = %q, want %q", got, "unknown")
	}
}
