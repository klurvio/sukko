package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestGetCapabilities_NATSAbsent(t *testing.T) {
	t.Parallel()

	handler, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var caps Capabilities
	if err := json.NewDecoder(w.Body).Decode(&caps); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// (a) Backends must not contain "nats"
	for _, b := range caps.Backends {
		if strings.EqualFold(b, "nats") {
			t.Errorf("Backends contains %q — NATS must not be listed as a valid backend", b)
		}
	}

	// (b) Backends must contain "direct" and "kafka"
	has := func(s string) bool {
		return slices.Contains(caps.Backends, s)
	}
	if !has("direct") {
		t.Error("Backends missing \"direct\"")
	}
	if !has("kafka") {
		t.Error("Backends missing \"kafka\"")
	}

	// (c) message_backend_urls Description must not contain "NATS" (case-insensitive)
	for _, f := range caps.ContextFields {
		if f.Name == "message_backend_urls" {
			if strings.Contains(strings.ToUpper(f.Description), "NATS") {
				t.Errorf("message_backend_urls Description %q contains NATS reference", f.Description)
			}
		}
	}
}
