package version

import (
	"encoding/json"
	"net/http"
)

// Handler returns an http.HandlerFunc for the /version endpoint.
// serviceName is embedded in the response (e.g., "ws-server", "auth-service").
func Handler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(Get(serviceName)); err != nil {
			http.Error(w, "failed to encode version", http.StatusInternalServerError)
		}
	}
}
