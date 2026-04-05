package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	pushv1 "github.com/klurvio/sukko/gen/proto/sukko/push/v1"
	"github.com/klurvio/sukko/internal/shared/auth"
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
//  3. Validate each channel has tenant prefix via auth.ValidateChannelTenant
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

	// 5. Validate channels non-empty and tenant prefix
	if len(req.Channels) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"at least one channel is required")
		return
	}
	for _, ch := range req.Channels {
		if !auth.ValidateChannelTenant(ch, authRes.TenantID) {
			httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
				"channel "+ch+" does not match tenant prefix "+authRes.TenantID)
			return
		}
	}

	// 6. Forward to push service
	if gw.pushClient == nil {
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

	// 4. Return success
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]bool{
		"success": resp.GetSuccess(),
	})
}
