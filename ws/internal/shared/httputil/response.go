package httputil

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		return fmt.Errorf("encode JSON response: %w", err)
	}
	return nil
}

// ErrorResponse represents a standard API error response.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes a JSON error response with the given status code.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Code: code, Message: message}) // best-effort: client may have disconnected
}

// WriteHealthOK writes a standard health check response.
func WriteHealthOK(w http.ResponseWriter, serviceName string) {
	_ = WriteJSON(w, http.StatusOK, map[string]string{ // best-effort: client may have disconnected
		"status":  "ok",
		"service": serviceName,
	})
}
