package version

import (
	"net/http"

	"github.com/klurvio/sukko/internal/shared/httputil"
)

// Handler returns an http.HandlerFunc for the /version endpoint.
// serviceName is embedded in the response (e.g., "ws-server", "ws-gateway").
// edition is the current Sukko edition (e.g., "community", "pro", "enterprise").
func Handler(serviceName, edition string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = httputil.WriteJSON(w, http.StatusOK, Get(serviceName, edition))
	}
}
