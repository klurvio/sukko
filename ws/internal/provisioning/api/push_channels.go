package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

// NOTE: httputil.WriteJSON errors are assigned to _ throughout this file.
// If WriteJSON fails, the client has disconnected and the HTTP response is
// already committed — there is no way to communicate a secondary error.

// Sentinel errors for push channel handler construction.
var (
	errNilChannelConfigRepo = errors.New("push channel handler: channel config repository is required")
	errNilChannelEventBus   = errors.New("push channel handler: event bus is required")
)

// validUrgencies is the set of allowed urgency values per Web Push spec.
var validUrgencies = map[string]bool{
	"very-low": true,
	"low":      true,
	"normal":   true,
	"high":     true,
}

// PushChannelHandler handles push channel configuration CRUD.
type PushChannelHandler struct {
	channelConfigRepo *repository.ChannelConfigRepository
	eventBus          *eventbus.Bus
	logger            zerolog.Logger
}

// NewPushChannelHandler creates a PushChannelHandler.
func NewPushChannelHandler(
	channelConfigRepo *repository.ChannelConfigRepository,
	eventBus *eventbus.Bus,
	logger zerolog.Logger,
) (*PushChannelHandler, error) {
	if channelConfigRepo == nil {
		return nil, errNilChannelConfigRepo
	}
	if eventBus == nil {
		return nil, errNilChannelEventBus
	}
	return &PushChannelHandler{
		channelConfigRepo: channelConfigRepo,
		eventBus:          eventBus,
		logger:            logger.With().Str("component", "push_channel_handler").Logger(),
	}, nil
}

// createChannelConfigRequest is the JSON body for POST /api/v1/push/channels.
type createChannelConfigRequest struct {
	TenantID       string   `json:"tenant_id"`
	Patterns       []string `json:"patterns"`
	DefaultTTL     int      `json:"default_ttl"`
	DefaultUrgency string   `json:"default_urgency"`
}

// channelConfigResponse is the JSON response for channel config operations.
type channelConfigResponse struct {
	TenantID       string   `json:"tenant_id"`
	Patterns       []string `json:"patterns"`
	DefaultTTL     int      `json:"default_ttl"`
	DefaultUrgency string   `json:"default_urgency"`
}

// deleteChannelConfigRequest is the JSON body for DELETE /api/v1/push/channels.
type deleteChannelConfigRequest struct {
	TenantID string `json:"tenant_id"`
}

// maxChannelConfigBodySize is the maximum request body size for channel config operations (1MB).
const maxChannelConfigBodySize = 1 << 20

// HandleCreateChannelConfig handles POST /api/v1/push/channels.
func (h *PushChannelHandler) HandleCreateChannelConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxChannelConfigBodySize)
	var req createChannelConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	if req.TenantID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_TENANT_ID", "tenant_id is required")
		return
	}

	if len(req.Patterns) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_PATTERNS", "patterns must be non-empty")
		return
	}

	// Validate tenant prefix on each pattern
	for _, pattern := range req.Patterns {
		if !auth.ValidateChannelTenant(pattern, req.TenantID) {
			httputil.WriteError(w, http.StatusBadRequest, "INVALID_PATTERN",
				fmt.Sprintf("pattern %q must start with tenant prefix %q", pattern, req.TenantID+"."))
			return
		}
	}

	// Validate urgency if provided
	if req.DefaultUrgency != "" && !validUrgencies[req.DefaultUrgency] {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_URGENCY",
			"default_urgency must be one of: very-low, low, normal, high")
		return
	}

	// Apply defaults
	if req.DefaultUrgency == "" {
		req.DefaultUrgency = "normal"
	}
	if req.DefaultTTL == 0 {
		req.DefaultTTL = defaultPushTTL
	}

	config := &repository.PushChannelConfig{
		TenantID:       req.TenantID,
		Patterns:       req.Patterns,
		DefaultTTL:     req.DefaultTTL,
		DefaultUrgency: req.DefaultUrgency,
	}

	if err := h.channelConfigRepo.Upsert(r.Context(), config); err != nil {
		h.logger.Error().Err(err).
			Str("tenant_id", req.TenantID).
			Msg("Failed to upsert push channel config")
		httputil.WriteError(w, http.StatusInternalServerError, "UPSERT_FAILED", "Failed to save push channel config")
		return
	}

	h.eventBus.Publish(eventbus.Event{Type: eventbus.PushConfigChanged})

	h.logger.Info().
		Str("tenant_id", req.TenantID).
		Int("patterns", len(req.Patterns)).
		Int("default_ttl", req.DefaultTTL).
		Str("default_urgency", req.DefaultUrgency).
		Msg("Push channel config created/updated")

	_ = httputil.WriteJSON(w, http.StatusCreated, channelConfigResponse{
		TenantID:       config.TenantID,
		Patterns:       config.Patterns,
		DefaultTTL:     config.DefaultTTL,
		DefaultUrgency: config.DefaultUrgency,
	})
}

// HandleGetChannelConfig handles GET /api/v1/push/channels.
func (h *PushChannelHandler) HandleGetChannelConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_TENANT_ID", "tenant_id query parameter is required")
		return
	}

	config, err := h.channelConfigRepo.Get(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, repository.ErrChannelConfigNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Push channel config not found for tenant")
		} else {
			h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to get push channel config")
			httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve channel config")
		}
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, channelConfigResponse{
		TenantID:       config.TenantID,
		Patterns:       config.Patterns,
		DefaultTTL:     config.DefaultTTL,
		DefaultUrgency: config.DefaultUrgency,
	})
}

// HandleDeleteChannelConfig handles DELETE /api/v1/push/channels.
func (h *PushChannelHandler) HandleDeleteChannelConfig(w http.ResponseWriter, r *http.Request) {
	var req deleteChannelConfigRequest

	// Support both query params and JSON body
	if r.Body != nil && r.ContentLength > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxChannelConfigBodySize)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
			return
		}
	}

	if v := r.URL.Query().Get("tenant_id"); v != "" {
		req.TenantID = v
	}

	if req.TenantID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_TENANT_ID", "tenant_id is required")
		return
	}

	if err := h.channelConfigRepo.Delete(r.Context(), req.TenantID); err != nil {
		if errors.Is(err, repository.ErrChannelConfigNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Push channel config not found for tenant")
		} else {
			h.logger.Error().Err(err).Str("tenant_id", req.TenantID).Msg("Failed to delete push channel config")
			httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete channel config")
		}
		return
	}

	h.eventBus.Publish(eventbus.Event{Type: eventbus.PushConfigChanged})

	h.logger.Info().Str("tenant_id", req.TenantID).Msg("Push channel config deleted")

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// defaultPushTTL is the default TTL for push messages (28 days in seconds).
const defaultPushTTL = 2419200
