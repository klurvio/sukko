package api //nolint:revive // api is a common package name for HTTP handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/klurvio/sukko/internal/shared/kafka"
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
func NewHandler(svc *provisioning.Service, logger zerolog.Logger) *Handler {
	return &Handler{
		service: svc,
		logger:  logger,
	}
}

// writeServiceError writes an error response with the given status and code.
func (h *Handler) writeServiceError(w http.ResponseWriter, _ error, defaultStatus int, code, msg string) {
	httputil.WriteError(w, defaultStatus, code, msg)
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
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	resp, err := h.service.CreateTenant(r.Context(), req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", req.TenantID).Msg("Failed to create tenant")
		RecordTenantOperation("create", "error")
		h.writeServiceError(w, err, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	RecordTenantCreated()
	RecordTenantOperation("create", "success")
	_ = httputil.WriteJSON(w, http.StatusCreated, resp)
}

// GetTenant retrieves a tenant by ID.
func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	tenant, err := h.service.GetTenant(r.Context(), tenantID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, tenant)
}

// ListTenants returns a paginated list of tenants.
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	opts := parseListOptions(r)

	tenants, total, err := h.service.ListTenants(r.Context(), opts)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"tenants": tenants,
		"total":   total,
		"limit":   opts.Limit,
		"offset":  opts.Offset,
	})
}

// UpdateTenant updates tenant metadata.
func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	tenant, err := h.service.UpdateTenant(r.Context(), tenantID, req)
	if err != nil {
		h.writeServiceError(w, err, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, tenant)
}

// SuspendTenant suspends a tenant.
func (h *Handler) SuspendTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.SuspendTenant(r.Context(), tenantID); err != nil {
		RecordTenantOperation("suspend", "error")
		h.writeServiceError(w, err, http.StatusInternalServerError, "SUSPEND_FAILED", err.Error())
		return
	}

	RecordTenantOperation("suspend", "success")
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}

// ReactivateTenant reactivates a suspended tenant.
func (h *Handler) ReactivateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.ReactivateTenant(r.Context(), tenantID); err != nil {
		RecordTenantOperation("reactivate", "error")
		h.writeServiceError(w, err, http.StatusInternalServerError, "REACTIVATE_FAILED", err.Error())
		return
	}

	RecordTenantOperation("reactivate", "success")
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// DeprovisionTenant initiates tenant deletion.
func (h *Handler) DeprovisionTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.DeprovisionTenant(r.Context(), tenantID); err != nil {
		RecordTenantOperation("deprovision", "error")
		h.writeServiceError(w, err, http.StatusInternalServerError, "DEPROVISION_FAILED", err.Error())
		return
	}

	RecordTenantOperation("deprovision", "success")
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deprovisioning"})
}

// CreateKey registers a new public key.
func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	key, err := h.service.CreateKey(r.Context(), tenantID, req)
	if err != nil {
		h.writeServiceError(w, err, http.StatusInternalServerError, "CREATE_KEY_FAILED", err.Error())
		return
	}

	RecordKeyCreated()
	_ = httputil.WriteJSON(w, http.StatusCreated, key)
}

// ListKeys returns all keys for a tenant.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	keys, err := h.service.ListKeys(r.Context(), tenantID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_KEYS_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"keys": keys,
	})
}

// RevokeKey revokes a key.
func (h *Handler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := h.service.RevokeKey(r.Context(), tenantID, keyID); err != nil {
		h.writeServiceError(w, err, http.StatusInternalServerError, "REVOKE_KEY_FAILED", err.Error())
		return
	}

	RecordKeyRevoked()
	_ = httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// GetActiveKeys returns all active keys (for WS Gateway).
func (h *Handler) GetActiveKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.service.GetActiveKeys(r.Context())
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "GET_KEYS_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"keys": keys,
	})
}

// CreateTopics creates topics for a tenant.
func (h *Handler) CreateTopics(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	topics, err := h.service.CreateTopics(r.Context(), tenantID, req.Categories)
	if err != nil {
		h.writeServiceError(w, err, http.StatusInternalServerError, "CREATE_TOPICS_FAILED", err.Error())
		return
	}

	// Build full topic names at runtime
	namespace := h.service.TopicNamespace()
	topicNames := make([]string, len(topics))
	for i, t := range topics {
		topicNames[i] = kafka.BuildTopicName(namespace, tenantID, t.Category)
	}

	RecordTopicCreated(len(topics))
	_ = httputil.WriteJSON(w, http.StatusCreated, map[string]any{
		"topics": topicNames,
	})
}

// ListTopics returns all topics for a tenant.
func (h *Handler) ListTopics(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	topics, err := h.service.ListTopics(r.Context(), tenantID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "LIST_TOPICS_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"topics": topics,
	})
}

// GetQuota returns quotas for a tenant.
func (h *Handler) GetQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	quota, err := h.service.GetQuota(r.Context(), tenantID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "QUOTA_NOT_FOUND", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, quota)
}

// UpdateQuota updates quotas for a tenant.
func (h *Handler) UpdateQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.UpdateQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	quota, err := h.service.UpdateQuota(r.Context(), tenantID, req)
	if err != nil {
		h.writeServiceError(w, err, http.StatusInternalServerError, "UPDATE_QUOTA_FAILED", err.Error())
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
		httputil.WriteError(w, http.StatusInternalServerError, "GET_AUDIT_FAILED", err.Error())
		return
	}

	_ = httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"total":   total,
		"limit":   opts.Limit,
		"offset":  opts.Offset,
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
