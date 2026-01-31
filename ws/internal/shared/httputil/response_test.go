package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       any
		wantStatus int
		wantBody   string
	}{
		{
			name:       "simple object",
			status:     http.StatusOK,
			data:       map[string]string{"key": "value"},
			wantStatus: http.StatusOK,
			wantBody:   `{"key":"value"}`,
		},
		{
			name:       "status created",
			status:     http.StatusCreated,
			data:       map[string]int{"id": 123},
			wantStatus: http.StatusCreated,
			wantBody:   `{"id":123}`,
		},
		{
			name:       "array data",
			status:     http.StatusOK,
			data:       []string{"a", "b", "c"},
			wantStatus: http.StatusOK,
			wantBody:   `["a","b","c"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := WriteJSON(w, tt.status, tt.data)
			if err != nil {
				t.Fatalf("WriteJSON() error = %v", err)
			}

			if w.Code != tt.wantStatus {
				t.Errorf("WriteJSON() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("WriteJSON() Content-Type = %q, want %q", ct, "application/json")
			}

			// Parse both as JSON to compare (handles whitespace differences)
			var got, want any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &want); err != nil {
				t.Fatalf("Failed to parse want: %v", err)
			}

			gotBytes, _ := json.Marshal(got)
			wantBytes, _ := json.Marshal(want)
			if string(gotBytes) != string(wantBytes) {
				t.Errorf("WriteJSON() body = %s, want %s", gotBytes, wantBytes)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		code        string
		message     string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "bad request error",
			status:      http.StatusBadRequest,
			code:        "INVALID_INPUT",
			message:     "Invalid input provided",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "INVALID_INPUT",
			wantMessage: "Invalid input provided",
		},
		{
			name:        "not found error",
			status:      http.StatusNotFound,
			code:        "NOT_FOUND",
			message:     "Resource not found",
			wantStatus:  http.StatusNotFound,
			wantCode:    "NOT_FOUND",
			wantMessage: "Resource not found",
		},
		{
			name:        "internal server error",
			status:      http.StatusInternalServerError,
			code:        "INTERNAL_ERROR",
			message:     "Something went wrong",
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "INTERNAL_ERROR",
			wantMessage: "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, tt.status, tt.code, tt.message)

			if w.Code != tt.wantStatus {
				t.Errorf("WriteError() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("WriteError() Content-Type = %q, want %q", ct, "application/json")
			}

			var resp ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			if resp.Code != tt.wantCode {
				t.Errorf("WriteError() code = %q, want %q", resp.Code, tt.wantCode)
			}
			if resp.Message != tt.wantMessage {
				t.Errorf("WriteError() message = %q, want %q", resp.Message, tt.wantMessage)
			}
		})
	}
}

func TestWriteHealthOK(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
	}{
		{
			name:        "gateway service",
			serviceName: "ws-gateway",
		},
		{
			name:        "server service",
			serviceName: "ws-server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteHealthOK(w, tt.serviceName)

			if w.Code != http.StatusOK {
				t.Errorf("WriteHealthOK() status = %d, want %d", w.Code, http.StatusOK)
			}

			var resp map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			if resp["status"] != "ok" {
				t.Errorf("WriteHealthOK() status = %q, want %q", resp["status"], "ok")
			}
			if resp["service"] != tt.serviceName {
				t.Errorf("WriteHealthOK() service = %q, want %q", resp["service"], tt.serviceName)
			}
		})
	}
}
