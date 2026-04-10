package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/klurvio/sukko/internal/shared/license"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
)

// NOTE: httputil.WriteJSON errors are assigned to _ throughout this file.
// If WriteJSON fails, the client has disconnected and the HTTP response is
// already committed — there is no way to communicate a secondary error.

// Pagination defaults for list endpoints.
const (
	defaultPageLimit = 50  // Default number of items per page
	maxPageLimit     = 100 // Maximum allowed items per page
)

// Handler provides HTTP handlers for provisioning operations.
type Handler struct {
	service *provisioning.Service
	logger  zerolog.Logger
}

// NewHandler creates a new Handler.
func NewHandler(svc *provisioning.Service, logger zerolog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("handler: service is required")
	}
	return &Handler{
		service: svc,
		logger:  logger,
	}, nil
}

// writeServiceError writes an error response, mapping known sentinel errors to appropriate HTTP status codes.
func (h *Handler) writeServiceError(w http.ResponseWriter, err error, code, msg string) {
	switch {
	case errors.Is(err, provisioning.ErrTenantAlreadyExists):
		httputil.WriteError(w, http.StatusConflict, "TENANT_ALREADY_EXISTS", "Tenant already exists")
	case errors.Is(err, provisioning.ErrTenantNotFound):
		httputil.WriteError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "Tenant not found")
	case errors.Is(err, provisioning.ErrTenantDeleted):
		httputil.WriteError(w, http.StatusConflict, "TENANT_DELETED", "Cannot modify deleted tenant")
	case errors.Is(err, provisioning.ErrTenantNotActive):
		httputil.WriteError(w, http.StatusConflict, "TENANT_NOT_ACTIVE", "Tenant is not active")
	case errors.Is(err, provisioning.ErrKeyNotOwnedByTenant):
		httputil.WriteError(w, http.StatusForbidden, "KEY_NOT_OWNED", "Key does not belong to tenant")
	case errors.Is(err, provisioning.ErrAPIKeyNotFound):
		httputil.WriteError(w, http.StatusNotFound, "API_KEY_NOT_FOUND", "API key not found")
	case errors.Is(err, provisioning.ErrAPIKeyNotOwnedByTenant):
		httputil.WriteError(w, http.StatusForbidden, "API_KEY_NOT_OWNED", "API key does not belong to tenant")
	case errors.Is(err, provisioning.ErrQuotaNotFound):
		httputil.WriteError(w, http.StatusNotFound, "QUOTA_NOT_FOUND", "Quota not found")
	case errors.Is(err, provisioning.ErrKeyNotFound):
		httputil.WriteError(w, http.StatusNotFound, "KEY_NOT_FOUND", "Key not found")
	case errors.Is(err, provisioning.ErrInvalidQuota):
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_QUOTA", err.Error())
	case errors.Is(err, provisioning.ErrChannelRulesNotConfigured),
		errors.Is(err, provisioning.ErrRoutingRulesNotConfigured):
		httputil.WriteError(w, http.StatusNotImplemented, "FEATURE_NOT_CONFIGURED", "Feature store not configured")
	case errors.Is(err, provisioning.ErrTooManyRoutingRules):
		httputil.WriteError(w, http.StatusBadRequest, "TOO_MANY_ROUTING_RULES", "Too many routing rules")
	case isEditionError(err):
		writeEditionError(w, err)
	default:
		httputil.WriteError(w, http.StatusInternalServerError, code, msg)
	}
}

// isEditionError returns true if the error is a license edition limit or feature error.
func isEditionError(err error) bool {
	var limitErr *license.EditionLimitError
	var featureErr *license.EditionFeatureError
	return errors.As(err, &limitErr) || errors.As(err, &featureErr)
}

// writeEditionError writes an HTTP 403 response for edition limit/feature errors.
func writeEditionError(w http.ResponseWriter, err error) {
	var limitErr *license.EditionLimitError
	if errors.As(err, &limitErr) {
		httputil.WriteError(w, http.StatusForbidden, limitErr.Code(), limitErr.Error())
		return
	}
	var featureErr *license.EditionFeatureError
	if errors.As(err, &featureErr) {
		httputil.WriteError(w, http.StatusForbidden, featureErr.Code(), featureErr.Error())
		return
	}
}

// Health returns basic health status.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready returns readiness status (database connectivity).
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Ready(r.Context()); err != nil {
		h.logger.Error().Err(err).Msg("Readiness check failed")
		httputil.WriteError(w, http.StatusServiceUnavailable, "NOT_READY", "Database not available")
		return
	}
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Metrics returns Prometheus metrics.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}

// CreateTenant creates a new tenant.
func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req provisioning.CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Str("handler", "CreateTenant").Str("remote_addr", r.RemoteAddr).Msg("request body parse failed") // LOG-023
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	resp, err := h.service.CreateTenant(r.Context(), req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", req.TenantID).Msg("Failed to create tenant")
		RecordTenantOperation("create", pkgmetrics.ResultError)
		h.writeServiceError(w, err, "CREATE_FAILED", "Failed to create tenant")
		return
	}

	RecordTenantCreated()
	RecordTenantOperation("create", pkgmetrics.ResultSuccess)
	h.logger.Info().Str("tenant_id", req.TenantID).Str("operation", "create").Msg("tenant create succeeded") // LOG-017
	_ = httputil.WriteJSON(w, http.StatusCreated, resp)
}

// GetTenant retrieves a tenant by ID.
func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	tenant, err := h.service.GetTenant(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, provisioning.ErrTenantNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to get tenant")
		httputil.WriteError(w, http.StatusInternalServerError, "GET_TENANT_FAILED", "Failed to get tenant")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, tenant)
}

// ListTenants returns a paginated list of tenants.
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	opts := parseListOptions(r)

	tenants, total, err := h.service.ListTenants(r.Context(), opts)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to list tenants")
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_FAILED", "Failed to list tenants")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  tenants,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

// UpdateTenant updates tenant metadata.
func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Str("handler", "UpdateTenant").Str("remote_addr", r.RemoteAddr).Msg("request body parse failed") // LOG-023
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	tenant, err := h.service.UpdateTenant(r.Context(), tenantID, req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to update tenant")
		h.writeServiceError(w, err, "UPDATE_FAILED", "Failed to update tenant")
		return
	}

	h.logger.Info().Str("tenant_id", tenantID).Str("operation", "update").Msg("tenant update succeeded") // LOG-017
	_ = httputil.WriteJSON(w, http.StatusOK, tenant)
}

// SuspendTenant suspends a tenant.
func (h *Handler) SuspendTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.SuspendTenant(r.Context(), tenantID); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to suspend tenant")
		RecordTenantOperation("suspend", pkgmetrics.ResultError)
		h.writeServiceError(w, err, "SUSPEND_FAILED", "Failed to suspend tenant")
		return
	}

	RecordTenantOperation("suspend", pkgmetrics.ResultSuccess)
	h.logger.Info().Str("tenant_id", tenantID).Str("operation", "suspend").Msg("tenant suspend succeeded") // LOG-017
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}

// ReactivateTenant reactivates a suspended tenant.
func (h *Handler) ReactivateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.ReactivateTenant(r.Context(), tenantID); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to reactivate tenant")
		RecordTenantOperation("reactivate", pkgmetrics.ResultError)
		h.writeServiceError(w, err, "REACTIVATE_FAILED", "Failed to reactivate tenant")
		return
	}

	RecordTenantOperation("reactivate", pkgmetrics.ResultSuccess)
	h.logger.Info().Str("tenant_id", tenantID).Str("operation", "reactivate").Msg("tenant reactivate succeeded") // LOG-017
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// DeprovisionTenant initiates tenant deletion. With ?force=true, deletes immediately
// (no grace period, no reactivation). Without force, uses the standard grace period.
func (h *Handler) DeprovisionTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	force := r.URL.Query().Get("force") == "true"

	if err := h.service.DeprovisionTenant(r.Context(), tenantID, force); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Bool("force", force).Msg("Failed to deprovision tenant")
		RecordTenantOperation("deprovision", pkgmetrics.ResultError)
		h.writeServiceError(w, err, "DEPROVISION_FAILED", "Failed to deprovision tenant")
		return
	}

	status := "deprovisioning"
	if force {
		status = "deleted"
	}
	RecordTenantOperation("deprovision", pkgmetrics.ResultSuccess)
	h.logger.Info().Str("tenant_id", tenantID).Str("operation", "deprovision").Msg("tenant deprovision succeeded") // LOG-017
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": status}) // WriteJSON error = broken client connection; nothing actionable
}

// CreateKey registers a new public key.
func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Str("handler", "CreateKey").Str("remote_addr", r.RemoteAddr).Msg("request body parse failed") // LOG-023
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	key, err := h.service.CreateKey(r.Context(), tenantID, req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to create key")
		h.writeServiceError(w, err, "CREATE_KEY_FAILED", "Failed to create key")
		return
	}

	RecordKeyCreated()
	h.logger.Info().Str("tenant_id", tenantID).Str("key_id", req.KeyID).Str("operation", "create").Msg("key create succeeded") // LOG-018
	_ = httputil.WriteJSON(w, http.StatusCreated, key)
}

// ListKeys returns keys for a tenant with pagination.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	opts := parseListOptions(r)

	keys, total, err := h.service.ListKeys(r.Context(), tenantID, opts)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to list keys")
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_KEYS_FAILED", "Failed to list keys")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  keys,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

// RevokeKey revokes a key.
func (h *Handler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := h.service.RevokeKey(r.Context(), tenantID, keyID); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Str("key_id", keyID).Msg("Failed to revoke key")
		h.writeServiceError(w, err, "REVOKE_KEY_FAILED", "Failed to revoke key")
		return
	}

	RecordKeyRevoked()
	h.logger.Info().Str("tenant_id", tenantID).Str("key_id", keyID).Str("operation", "revoke").Msg("key revoke succeeded") // LOG-018
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// GetActiveKeys returns all active keys (for WS Gateway).
func (h *Handler) GetActiveKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.service.GetActiveKeys(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get active keys")
		httputil.WriteError(w, http.StatusInternalServerError, "GET_KEYS_FAILED", "Failed to get active keys")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"keys": keys,
	})
}

// GetRoutingRules returns routing rules for a tenant.
func (h *Handler) GetRoutingRules(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	rules, err := h.service.GetRoutingRules(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, provisioning.ErrRoutingRulesNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "No routing rules configured")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to get routing rules")
		httputil.WriteError(w, http.StatusInternalServerError, "GET_ROUTING_RULES_FAILED", "Failed to retrieve routing rules")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"rules": rules,
	})
}

// SetRoutingRules creates or updates routing rules for a tenant.
func (h *Handler) SetRoutingRules(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.SetRoutingRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Str("handler", "SetRoutingRules").Str("remote_addr", r.RemoteAddr).Msg("request body parse failed") // LOG-023
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	// Boundary validation (defense in depth)
	if err := provisioning.ValidateRoutingRules(req.Rules); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return
	}

	if err := h.service.SetRoutingRules(r.Context(), tenantID, req.Rules); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to set routing rules")
		h.writeServiceError(w, err, "SET_ROUTING_RULES_FAILED", "Failed to set routing rules")
		return
	}

	RecordRoutingRulesSet()
	h.logger.Info().Str("tenant_id", tenantID).Int("rule_count", len(req.Rules)).Msg("routing rules updated") // LOG-019

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"rules": req.Rules,
	})
}

// DeleteRoutingRules deletes routing rules for a tenant.
func (h *Handler) DeleteRoutingRules(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.DeleteRoutingRules(r.Context(), tenantID); err != nil {
		if errors.Is(err, provisioning.ErrRoutingRulesNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", "No routing rules configured")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to delete routing rules")
		httputil.WriteError(w, http.StatusInternalServerError, "DELETE_ROUTING_RULES_FAILED", "Failed to delete routing rules")
		return
	}

	RecordRoutingRulesDeleted()
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetQuota returns quotas for a tenant.
func (h *Handler) GetQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	quota, err := h.service.GetQuota(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, provisioning.ErrQuotaNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "QUOTA_NOT_FOUND", "Quota not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to get quota")
		httputil.WriteError(w, http.StatusInternalServerError, "GET_QUOTA_FAILED", "Failed to get quota")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, quota)
}

// UpdateQuota updates quotas for a tenant.
func (h *Handler) UpdateQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.UpdateQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Str("handler", "UpdateQuota").Str("remote_addr", r.RemoteAddr).Msg("request body parse failed") // LOG-023
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	quota, err := h.service.UpdateQuota(r.Context(), tenantID, req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to update quota")
		h.writeServiceError(w, err, "UPDATE_QUOTA_FAILED", "Failed to update quota")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, quota)
}

// GetAuditLog returns audit entries for a tenant.
func (h *Handler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	opts := parseListOptions(r)

	entries, total, err := h.service.GetAuditLog(r.Context(), tenantID, opts)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to get audit log")
		httputil.WriteError(w, http.StatusInternalServerError, "GET_AUDIT_FAILED", "Failed to get audit log")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  entries,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

// CreateAPIKey creates a new API key for a tenant.
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Str("handler", "CreateAPIKey").Str("remote_addr", r.RemoteAddr).Msg("request body parse failed") // LOG-023
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	key, err := h.service.CreateAPIKey(r.Context(), tenantID, req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to create API key")
		h.writeServiceError(w, err, "CREATE_API_KEY_FAILED", "Failed to create API key")
		return
	}

	RecordAPIKeyCreated()
	h.logger.Info().Str("tenant_id", tenantID).Str("key_id", key.KeyID).Str("operation", "create").Msg("key create succeeded") // LOG-018
	_ = httputil.WriteJSON(w, http.StatusCreated, key)
}

// ListAPIKeys returns API keys for a tenant with pagination.
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	opts := parseListOptions(r)

	keys, total, err := h.service.ListAPIKeys(r.Context(), tenantID, opts)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Msg("Failed to list API keys")
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_API_KEYS_FAILED", "Failed to list API keys")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  keys,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

// RevokeAPIKey revokes an API key.
func (h *Handler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := h.service.RevokeAPIKey(r.Context(), tenantID, keyID); err != nil {
		h.logger.Error().Err(err).Str("tenant_id", tenantID).Str("key_id", keyID).Msg("Failed to revoke API key")
		h.writeServiceError(w, err, "REVOKE_API_KEY_FAILED", "Failed to revoke API key")
		return
	}

	RecordAPIKeyRevoked()
	h.logger.Info().Str("tenant_id", tenantID).Str("key_id", keyID).Str("operation", "revoke").Msg("key revoke succeeded") // LOG-018
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// GetActiveAPIKeys returns all active API keys (for gateway).
func (h *Handler) GetActiveAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.service.GetActiveAPIKeys(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get active API keys")
		httputil.WriteError(w, http.StatusInternalServerError, "GET_API_KEYS_FAILED", "Failed to get active API keys")
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"keys": keys,
	})
}

// parseListOptions extracts pagination options from query params.
func parseListOptions(r *http.Request) provisioning.ListOptions {
	opts := provisioning.ListOptions{
		Limit:  defaultPageLimit,
		Offset: 0,
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= maxPageLimit {
			opts.Limit = l
		}
	}

	if offset := r.URL.Query().Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil && o >= 0 {
			opts.Offset = o
		}
	}

	if status := r.URL.Query().Get("status"); status != "" {
		s := provisioning.TenantStatus(status)
		if s.IsValid() {
			opts.Status = &s
		}
	}

	return opts
}
