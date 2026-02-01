package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
	"github.com/Toniq-Labs/odin-ws/internal/shared/httputil"
	"github.com/Toniq-Labs/odin-ws/internal/shared/types"
)

// CreateOIDCConfig creates OIDC configuration for a tenant.
// POST /tenants/{tenantID}/oidc
func (h *Handler) CreateOIDCConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateOIDCConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Build config from request
	config := &types.TenantOIDCConfig{
		TenantID:  tenantID,
		IssuerURL: req.IssuerURL,
		JWKSURL:   req.JWKSURL,
		Audience:  req.Audience,
		Enabled:   true,
	}

	// Validate config
	if err := config.Validate(); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return
	}

	// Create via service
	if err := h.service.CreateOIDCConfig(r.Context(), config); err != nil {
		if errors.Is(err, types.ErrIssuerAlreadyExists) {
			httputil.WriteError(w, http.StatusConflict, "ISSUER_EXISTS", "Issuer URL is already registered to another tenant")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to create OIDC config")
		httputil.WriteError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	h.logger.Info().
		Str("tenant_id", tenantID).
		Str("issuer_url", config.IssuerURL).
		Msg("OIDC config created")

	httputil.WriteJSON(w, http.StatusCreated, config)
}

// GetOIDCConfig retrieves OIDC configuration for a tenant.
// GET /tenants/{tenantID}/oidc
func (h *Handler) GetOIDCConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	config, err := h.service.GetOIDCConfig(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, types.ErrOIDCNotConfigured) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "OIDC not configured for this tenant")
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, config)
}

// UpdateOIDCConfig updates OIDC configuration for a tenant.
// PUT /tenants/{tenantID}/oidc
func (h *Handler) UpdateOIDCConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.UpdateOIDCConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Get existing config
	existing, err := h.service.GetOIDCConfig(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, types.ErrOIDCNotConfigured) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "OIDC not configured for this tenant")
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}

	// Apply updates
	if req.IssuerURL != nil {
		existing.IssuerURL = *req.IssuerURL
	}
	if req.JWKSURL != nil {
		existing.JWKSURL = *req.JWKSURL
	}
	if req.Audience != nil {
		existing.Audience = *req.Audience
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	// Validate updated config
	if err := existing.Validate(); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return
	}

	// Update via service
	if err := h.service.UpdateOIDCConfig(r.Context(), existing); err != nil {
		if errors.Is(err, types.ErrIssuerAlreadyExists) {
			httputil.WriteError(w, http.StatusConflict, "ISSUER_EXISTS", "Issuer URL is already registered to another tenant")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to update OIDC config")
		httputil.WriteError(w, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	h.logger.Info().
		Str("tenant_id", tenantID).
		Str("issuer_url", existing.IssuerURL).
		Bool("enabled", existing.Enabled).
		Msg("OIDC config updated")

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteOIDCConfig deletes OIDC configuration for a tenant.
// DELETE /tenants/{tenantID}/oidc
func (h *Handler) DeleteOIDCConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.DeleteOIDCConfig(r.Context(), tenantID); err != nil {
		if errors.Is(err, types.ErrOIDCNotConfigured) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "OIDC not configured for this tenant")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to delete OIDC config")
		httputil.WriteError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	h.logger.Info().
		Str("tenant_id", tenantID).
		Msg("OIDC config deleted")

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
