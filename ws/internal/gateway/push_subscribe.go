package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	pushv1 "github.com/klurvio/sukko/gen/proto/sukko/push/v1"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

// validPushPlatforms defines the allowed push notification platforms.
var validPushPlatforms = map[string]bool{
	"web":     true,
	"android": true,
	"ios":     true,
}

// pushSubscribeRequest is the JSON body format for POST /api/v1/push/subscribe.
type pushSubscribeRequest struct {
	Platform   string   `json:"platform"`
	Token      string   `json:"token,omitempty"`       // FCM/APNs token (android/ios)
	Endpoint   string   `json:"endpoint,omitempty"`    // Web Push endpoint URL (web)
	P256dhKey  string   `json:"p256dh_key,omitempty"`  // Web Push ECDH public key (web)
	AuthSecret string   `json:"auth_secret,omitempty"` // Web Push auth secret (web)
	Channels   []string `json:"channels"`
}

// pushUnsubscribeRequest is the JSON body format for DELETE /api/v1/push/subscribe.
type pushUnsubscribeRequest struct {
	DeviceID int64 `json:"device_id"`
}

// HandlePushSubscribe handles push notification subscription requests.
//
// Flow:
//  1. Authenticate via shared authenticateRequest()
//  2. Parse and validate JSON body (platform, platform-specific fields, channels)
//  3. Validate and filter channels via filterSubscribeChannels (tenant prefix, format, permissions)
//  4. Forward to push service via gRPC RegisterDevice
//  5. Return {"device_id": N}
func (gw *Gateway) HandlePushSubscribe(w http.ResponseWriter, r *http.Request) {
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

	// 2. Parse JSON body with size limit
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(gw.config.MaxPublishSize)+1))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "failed to read request body")
		return
	}
	if len(body) > gw.config.MaxPublishSize {
		httputil.WriteError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE",
			"request body exceeds maximum size")
		return
	}

	var req pushSubscribeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// 3. Validate platform
	if !validPushPlatforms[req.Platform] {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"platform must be one of: web, android, ios")
		return
	}

	// 4. Validate platform-specific fields
	switch req.Platform {
	case "web":
		if req.Endpoint == "" || req.P256dhKey == "" || req.AuthSecret == "" {
			httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
				"web platform requires endpoint, p256dh_key, and auth_secret")
			return
		}
	case "android", "ios":
		if req.Token == "" {
			httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
				"android and ios platforms require token")
			return
		}
	}

	// 5. Validate channels non-empty
	if len(req.Channels) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"at least one channel is required")
		return
	}

	// 5a. Permission filtering — tenant prefix, format, and subscribe permissions (FR-009)
	// API-key-only allowed for public channels (nil claims → public patterns only)
	allowed := gw.filterSubscribeChannels(ctx, req.Channels, authRes.TenantID, authRes.Claims)
	if len(allowed) != len(req.Channels) {
		// Build list of denied channels for error message
		deniedSet := make(map[string]bool, len(req.Channels))
		for _, ch := range req.Channels {
			deniedSet[ch] = true
		}
		for _, ch := range allowed {
			delete(deniedSet, ch)
		}
		denied := make([]string, 0, len(deniedSet))
		for ch := range deniedSet {
			denied = append(denied, ch)
		}
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"channels denied by permission rules: "+strings.Join(denied, ", "))
		return
	}
	req.Channels = allowed

	// 6. Forward to push service
	if gw.pushClient == nil {
		// LOG-012: Push service unavailable
		gw.logger.Warn().Str("handler", "subscribe").Str("tenant_id", authRes.TenantID).
			Msg("push service unavailable")
		httputil.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"push service connection not available")
		return
	}

	resp, err := gw.pushClient.RegisterDevice(ctx, &pushv1.RegisterDeviceRequest{
		TenantId:   authRes.TenantID,
		Principal:  authRes.Principal,
		Platform:   req.Platform,
		Token:      req.Token,
		Endpoint:   req.Endpoint,
		P256DhKey:  req.P256dhKey,
		AuthSecret: req.AuthSecret,
		Channels:   req.Channels,
	})
	if err != nil {
		gw.logger.Error().Err(err).
			Str("tenant_id", authRes.TenantID).
			Str("platform", req.Platform).
			Msg("Push RegisterDevice failed")
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to register push subscription")
		return
	}

	// LOG-013: Push device registered
	gw.logger.Info().Str("tenant_id", authRes.TenantID).Str("platform", req.Platform).
		Int64("device_id", resp.GetDeviceId()).Msg("push device registered")

	// 7. Return device_id
	_ = httputil.WriteJSON(w, http.StatusCreated, map[string]int64{
		"device_id": resp.GetDeviceId(),
	})
}

// HandlePushUnsubscribe handles push notification unsubscription requests.
//
// Flow:
//  1. Authenticate via shared authenticateRequest()
//  2. Parse JSON body with device_id
//  3. Forward to push service via gRPC UnregisterDevice
//  4. Return {"success": true}
func (gw *Gateway) HandlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
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

	// 1a. Block API-key-only — JWT required for unsubscribe (FR-010)
	// Skipped when AUTH_MODE=disabled (FR-011)
	if gw.config.AuthRequired() && authRes.APIKeyOnly {
		httputil.WriteError(w, http.StatusForbidden, "FORBIDDEN",
			"push unsubscribe requires JWT authentication")
		return
	}

	// 2. Parse JSON body
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(gw.config.MaxPublishSize)+1))
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "failed to read request body")
		return
	}

	var req pushUnsubscribeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	if req.DeviceID == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "device_id is required")
		return
	}

	// 3. Forward to push service
	if gw.pushClient == nil {
		// LOG-012: Push service unavailable
		gw.logger.Warn().Str("handler", "unsubscribe").Str("tenant_id", authRes.TenantID).
			Msg("push service unavailable")
		httputil.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"push service connection not available")
		return
	}

	resp, err := gw.pushClient.UnregisterDevice(ctx, &pushv1.UnregisterDeviceRequest{
		TenantId: authRes.TenantID,
		DeviceId: req.DeviceID,
	})
	if err != nil {
		gw.logger.Error().Err(err).
			Str("tenant_id", authRes.TenantID).
			Int64("device_id", req.DeviceID).
			Msg("Push UnregisterDevice failed")
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to unregister push subscription")
		return
	}

	// LOG-014: Push device unregistered
	gw.logger.Info().Str("tenant_id", authRes.TenantID).Int64("device_id", req.DeviceID).
		Msg("push device unregistered")

	// 4. Return success
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]bool{
		"success": resp.GetSuccess(),
	})
}
