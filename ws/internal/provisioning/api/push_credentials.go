package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

// NOTE: httputil.WriteJSON errors are assigned to _ throughout this file.
// If WriteJSON fails, the client has disconnected and the HTTP response is
// already committed — there is no way to communicate a secondary error.

// validProviders is the set of allowed push credential providers.
var validProviders = map[string]bool{
	"fcm":   true,
	"apns":  true,
	"vapid": true,
}

// PushCredentialHandler handles push credential upload and deletion.
type PushCredentialHandler struct {
	credentialsRepo *repository.CredentialsRepository
	eventBus        *eventbus.Bus
	logger          zerolog.Logger
}

// NewPushCredentialHandler creates a PushCredentialHandler.
func NewPushCredentialHandler(
	credentialsRepo *repository.CredentialsRepository,
	eventBus *eventbus.Bus,
	logger zerolog.Logger,
) (*PushCredentialHandler, error) {
	if credentialsRepo == nil {
		return nil, errNilCredentialsRepo
	}
	if eventBus == nil {
		return nil, errNilEventBus
	}
	return &PushCredentialHandler{
		credentialsRepo: credentialsRepo,
		eventBus:        eventBus,
		logger:          logger.With().Str("component", "push_credential_handler").Logger(),
	}, nil
}

// uploadCredentialsRequest is the JSON body for POST /api/v1/push/credentials.
type uploadCredentialsRequest struct {
	TenantID       string `json:"tenant_id"`
	Provider       string `json:"provider"`
	CredentialData string `json:"credential_data"`
}

// uploadCredentialsResponse is the JSON body returned on successful upload.
type uploadCredentialsResponse struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	TenantID string `json:"tenant_id"`
}

// deleteCredentialsRequest is the JSON body for DELETE /api/v1/push/credentials.
type deleteCredentialsRequest struct {
	TenantID string `json:"tenant_id"`
	Provider string `json:"provider"`
}

// maxCredentialBodySize is the maximum request body size for credential operations (1MB).
const maxCredentialBodySize = 1 << 20

// HandleUploadCredentials handles POST /api/v1/push/credentials.
func (h *PushCredentialHandler) HandleUploadCredentials(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBodySize)
	var req uploadCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	if req.TenantID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_TENANT_ID", "tenant_id is required")
		return
	}

	if req.Provider == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_PROVIDER", "provider is required")
		return
	}

	if !validProviders[req.Provider] {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_PROVIDER", "provider must be one of: fcm, apns, vapid")
		return
	}

	if req.CredentialData == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_CREDENTIAL_DATA", "credential_data is required")
		return
	}

	// Validate credential_data format per provider
	if err := validateCredentialData(req.Provider, req.CredentialData); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_CREDENTIAL_DATA", err.Error())
		return
	}

	// Connectivity test (FR-011): FCM OAuth2 token exchange
	if req.Provider == "fcm" {
		if err := testFCMConnectivity(req.CredentialData); err != nil {
			h.logger.Warn().Err(err).Str("tenant_id", req.TenantID).Msg("FCM connectivity test failed")
			httputil.WriteError(w, http.StatusBadRequest, "FCM_CONNECTIVITY_FAILED", "FCM connectivity test failed: "+err.Error())
			return
		}
	}
	// APNs: skip in v1 (complex to test without real device token)
	if req.Provider == "apns" {
		h.logger.Warn().Str("tenant_id", req.TenantID).Msg("APNs connectivity test skipped in v1")
	}
	// VAPID: skip (no external service to test against)

	cred := &repository.PushCredential{
		TenantID:       req.TenantID,
		Provider:       req.Provider,
		CredentialData: req.CredentialData,
	}

	if err := h.credentialsRepo.Create(r.Context(), cred); err != nil {
		h.logger.Error().Err(err).
			Str("tenant_id", req.TenantID).
			Str("provider", req.Provider).
			Msg("Failed to create push credential")
		httputil.WriteError(w, http.StatusInternalServerError, "CREATE_FAILED", "Failed to create push credential")
		return
	}

	h.eventBus.Publish(eventbus.Event{Type: eventbus.PushConfigChanged})

	h.logger.Info().
		Str("tenant_id", req.TenantID).
		Str("provider", req.Provider).
		Int64("id", cred.ID).
		Msg("Push credential created")

	_ = httputil.WriteJSON(w, http.StatusCreated, uploadCredentialsResponse{
		ID:       cred.ID,
		Provider: cred.Provider,
		TenantID: cred.TenantID,
	})
}

// HandleDeleteCredentials handles DELETE /api/v1/push/credentials.
func (h *PushCredentialHandler) HandleDeleteCredentials(w http.ResponseWriter, r *http.Request) {
	var req deleteCredentialsRequest

	// Support both query params and JSON body
	if r.Body != nil && r.ContentLength > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBodySize)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
			return
		}
	}

	// Query params override body (if present)
	if v := r.URL.Query().Get("tenant_id"); v != "" {
		req.TenantID = v
	}
	if v := r.URL.Query().Get("provider"); v != "" {
		req.Provider = v
	}

	if req.TenantID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_TENANT_ID", "tenant_id is required")
		return
	}
	if req.Provider == "" {
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_PROVIDER", "provider is required")
		return
	}
	if !validProviders[req.Provider] {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_PROVIDER", "provider must be one of: fcm, apns, vapid")
		return
	}

	if err := h.credentialsRepo.Delete(r.Context(), req.TenantID, req.Provider); err != nil {
		if errors.Is(err, repository.ErrCredentialNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Push credential not found")
		} else {
			h.logger.Error().Err(err).
				Str("tenant_id", req.TenantID).
				Str("provider", req.Provider).
				Msg("Failed to delete push credential")
			httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete credential")
		}
		return
	}

	h.eventBus.Publish(eventbus.Event{Type: eventbus.PushConfigChanged})

	h.logger.Info().
		Str("tenant_id", req.TenantID).
		Str("provider", req.Provider).
		Msg("Push credential deleted")

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
