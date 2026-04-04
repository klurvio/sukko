package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	serverv1 "github.com/klurvio/sukko/gen/proto/sukko/server/v1"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

// publishRequest is the JSON body format for POST /api/v1/publish (FR-011).
type publishRequest struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// HandlePublish handles REST publish requests — stateless message publishing (FR-010).
//
// Flow:
//  1. Validate Content-Type and body size
//  2. Parse JSON body
//  3. Authenticate via shared authenticateRequest()
//  4. Check channel publish permissions
//  5. Rate limit via PublishRateLimiter
//  6. Call gRPC Publish() on ws-server
//  7. Return 200 {"status":"accepted","channel":"..."}
func (gw *Gateway) HandlePublish(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	ctx := r.Context()

	// 1. Validate Content-Type
	if ct := r.Header.Get("Content-Type"); ct != "" && ct != "application/json" {
		RecordRestPublish("invalid_content_type", time.Since(startTime))
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Content-Type must be application/json")
		return
	}

	// Limit body size to prevent abuse (same as GATEWAY_MAX_PUBLISH_SIZE)
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(gw.config.MaxPublishSize)+1))
	if err != nil {
		RecordRestPublish("read_error", time.Since(startTime))
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "failed to read request body")
		return
	}
	if len(body) > gw.config.MaxPublishSize {
		RecordRestPublish("body_too_large", time.Since(startTime))
		httputil.WriteError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE",
			"request body exceeds maximum publish size")
		return
	}

	// 2. Parse JSON body
	var req publishRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RecordRestPublish("invalid_json", time.Since(startTime))
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	if req.Channel == "" {
		RecordRestPublish("missing_channel", time.Since(startTime))
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "channel is required")
		return
	}
	if len(req.Data) == 0 {
		RecordRestPublish("missing_data", time.Since(startTime))
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "data is required")
		return
	}

	// 3. Authenticate
	authRes, authErr := gw.authenticateRequest(ctx, r)
	if authErr != nil {
		status := http.StatusUnauthorized
		code := "UNAUTHORIZED"
		if errors.Is(authErr, ErrTenantMismatch) {
			status = http.StatusForbidden
			code = "FORBIDDEN"
		}
		RecordRestPublish("auth_failed", time.Since(startTime))
		httputil.WriteError(w, status, code, authErr.Error())
		return
	}

	// 4. Check channel publish permissions
	// TODO: Apply publish permission checking when integrating with existing permission checker
	// For now, all authenticated users can publish

	// 5. Rate limit
	if gw.publishRateLimiter != nil {
		clientIP := httputil.GetClientIP(r)
		if !gw.publishRateLimiter.Allow(authRes.TenantID, clientIP) {
			RecordRestPublish("rate_limited", time.Since(startTime))
			httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED",
				"publish rate limit exceeded")
			return
		}
	}

	// 6. Call gRPC Publish() on ws-server
	if gw.serverClient == nil {
		RecordRestPublish("unavailable", time.Since(startTime))
		httputil.WriteError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"ws-server connection not available")
		return
	}

	resp, err := gw.serverClient.Client().Publish(ctx, &serverv1.PublishRequest{
		TenantId:  authRes.TenantID,
		Channel:   req.Channel,
		Data:      req.Data,
		Principal: authRes.Principal,
	})
	if err != nil {
		gw.logger.Error().Err(err).
			Str("channel", req.Channel).
			Str("tenant_id", authRes.TenantID).
			Msg("gRPC Publish failed")
		RecordRestPublish("error", time.Since(startTime))
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to publish message")
		return
	}

	// 7. Return success (FR-015)
	RecordRestPublish("success", time.Since(startTime))
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  resp.GetStatus(),
		"channel": resp.GetChannel(),
	})
}
