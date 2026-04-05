package gateway

import (
	"errors"
	"net/http"

	pushv1 "github.com/klurvio/sukko/gen/proto/sukko/push/v1"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

// HandlePushVAPIDKey returns the tenant's VAPID public key for Web Push subscriptions.
//
// Flow:
//  1. Authenticate via shared authenticateRequest()
//  2. Forward to push service via gRPC GetVAPIDKey
//  3. Return {"public_key": "..."}
func (gw *Gateway) HandlePushVAPIDKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Authenticate
	authRes, authErr := gw.authenticateRequest(ctx, r)
	if authErr != nil {
		status := http.StatusUnauthorized
		code := "UNAUTHORIZED"
		if errors.Is(authErr, ErrTenantMismatch) {
			status = http.StatusForbidden
			code = "FORBIDDEN"
		}
		httputil.WriteError(w, status, code, authErr.Error())
		return
	}

	// 2. Forward to push service
	if gw.pushClient == nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"push service connection not available")
		return
	}

	resp, err := gw.pushClient.GetVAPIDKey(ctx, &pushv1.GetVAPIDKeyRequest{
		TenantId: authRes.TenantID,
	})
	if err != nil {
		gw.logger.Error().Err(err).
			Str("tenant_id", authRes.TenantID).
			Msg("Push GetVAPIDKey failed")
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to retrieve VAPID key")
		return
	}

	// 3. Return public key
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"public_key": resp.GetPublicKey(),
	})
}
