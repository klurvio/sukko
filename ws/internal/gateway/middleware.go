package gateway

import (
	"fmt"
	"net/http"

	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/klurvio/sukko/internal/shared/license"
)

// RequireFeature returns middleware that gates a handler behind an edition feature check.
// On Community Edition, returns 403 EDITION_LIMIT. Pro/Enterprise pass through.
// Used on http.ServeMux — no chi migration required (FR-028).
func RequireFeature(mgr *license.Manager, feature license.Feature) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if mgr != nil && !mgr.HasFeature(feature) {
				httputil.WriteError(w, http.StatusForbidden, "EDITION_LIMIT",
					fmt.Sprintf("Feature %q requires Pro edition or higher", feature))
				return
			}
			next(w, r)
		}
	}
}
