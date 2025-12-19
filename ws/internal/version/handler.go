package version

import (
	"encoding/json"
	"net/http"
)

// Handler returns an http.HandlerFunc for the /version endpoint.
// serviceName is embedded in the response (e.g., "ws-server", "auth-service").
func Handler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Get(serviceName))
	}
}
