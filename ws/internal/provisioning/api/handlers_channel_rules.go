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

// GetChannelRules retrieves channel rules for a tenant.
// GET /tenants/{tenantID}/channel-rules
func (h *Handler) GetChannelRules(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	rules, err := h.service.GetChannelRules(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, types.ErrChannelRulesNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Channel rules not configured for this tenant")
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, rules)
}

// SetChannelRules creates or updates channel rules for a tenant.
// PUT /tenants/{tenantID}/channel-rules
func (h *Handler) SetChannelRules(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.SetChannelRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Build rules from request
	rules := &types.ChannelRules{
		Public:        req.Public,
		GroupMappings: req.GroupMappings,
		Default:       req.Default,
	}

	// Initialize nil slices/maps to empty values
	if rules.Public == nil {
		rules.Public = []string{}
	}
	if rules.GroupMappings == nil {
		rules.GroupMappings = make(map[string][]string)
	}
	if rules.Default == nil {
		rules.Default = []string{}
	}

	// Validate rules
	if err := rules.Validate(); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return
	}

	// Set via service (upsert)
	if err := h.service.SetChannelRules(r.Context(), tenantID, rules); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to set channel rules")
		httputil.WriteError(w, http.StatusInternalServerError, "SET_FAILED", err.Error())
		return
	}

	h.logger.Info().
		Str("tenant_id", tenantID).
		Int("public_patterns", len(rules.Public)).
		Int("group_mappings", len(rules.GroupMappings)).
		Msg("Channel rules set")

	_ = httputil.WriteJSON(w, http.StatusOK, rules)
}

// DeleteChannelRules deletes channel rules for a tenant.
// DELETE /tenants/{tenantID}/channel-rules
func (h *Handler) DeleteChannelRules(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.DeleteChannelRules(r.Context(), tenantID); err != nil {
		if errors.Is(err, types.ErrChannelRulesNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Channel rules not configured for this tenant")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to delete channel rules")
		httputil.WriteError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	h.logger.Info().
		Str("tenant_id", tenantID).
		Msg("Channel rules deleted")

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TestAccess tests what channel patterns a set of groups would have access to.
// POST /tenants/{tenantID}/test-access
func (h *Handler) TestAccess(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.TestAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Get channel rules
	rules, err := h.service.GetChannelRules(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, types.ErrChannelRulesNotFound) {
			// No rules configured - return empty patterns
			_ = httputil.WriteJSON(w, http.StatusOK, provisioning.TestAccessResponse{
				AllowedPatterns: []string{},
			})
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}

	// Compute allowed patterns for the given groups
	allowedPatterns := rules.Rules.ComputeAllowedPatterns(req.Groups)

	_ = httputil.WriteJSON(w, http.StatusOK, provisioning.TestAccessResponse{
		AllowedPatterns: allowedPatterns,
	})
}
