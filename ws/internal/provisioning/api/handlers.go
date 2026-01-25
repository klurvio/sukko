package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/provisioning"
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

// Health returns basic health status.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready returns readiness status (database connectivity).
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Ready(r.Context()); err != nil {
		h.logger.Error().Err(err).Msg("Readiness check failed")
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "Database not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Metrics returns Prometheus metrics.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	promhttp.Handler().ServeHTTP(w, r)
}

// CreateTenant creates a new tenant.
func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var req provisioning.CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	resp, err := h.service.CreateTenant(r.Context(), req)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", req.TenantID).Msg("Failed to create tenant")
		RecordTenantOperation("create", "error")
		writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	RecordTenantCreated()
	RecordTenantOperation("create", "success")
	writeJSON(w, http.StatusCreated, resp)
}

// GetTenant retrieves a tenant by ID.
func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	tenant, err := h.service.GetTenant(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tenant)
}

// ListTenants returns a paginated list of tenants.
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	opts := parseListOptions(r)

	tenants, total, err := h.service.ListTenants(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
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
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	tenant, err := h.service.UpdateTenant(r.Context(), tenantID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tenant)
}

// SuspendTenant suspends a tenant.
func (h *Handler) SuspendTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.SuspendTenant(r.Context(), tenantID); err != nil {
		RecordTenantOperation("suspend", "error")
		writeError(w, http.StatusInternalServerError, "SUSPEND_FAILED", err.Error())
		return
	}

	RecordTenantOperation("suspend", "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}

// ReactivateTenant reactivates a suspended tenant.
func (h *Handler) ReactivateTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.ReactivateTenant(r.Context(), tenantID); err != nil {
		RecordTenantOperation("reactivate", "error")
		writeError(w, http.StatusInternalServerError, "REACTIVATE_FAILED", err.Error())
		return
	}

	RecordTenantOperation("reactivate", "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// DeprovisionTenant initiates tenant deletion.
func (h *Handler) DeprovisionTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	if err := h.service.DeprovisionTenant(r.Context(), tenantID); err != nil {
		RecordTenantOperation("deprovision", "error")
		writeError(w, http.StatusInternalServerError, "DEPROVISION_FAILED", err.Error())
		return
	}

	RecordTenantOperation("deprovision", "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deprovisioning"})
}

// CreateKey registers a new public key.
func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	key, err := h.service.CreateKey(r.Context(), tenantID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_KEY_FAILED", err.Error())
		return
	}

	RecordKeyCreated()
	writeJSON(w, http.StatusCreated, key)
}

// ListKeys returns all keys for a tenant.
func (h *Handler) ListKeys(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	keys, err := h.service.ListKeys(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_KEYS_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"keys": keys,
	})
}

// RevokeKey revokes a key.
func (h *Handler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	keyID := chi.URLParam(r, "keyID")

	if err := h.service.RevokeKey(r.Context(), tenantID, keyID); err != nil {
		writeError(w, http.StatusInternalServerError, "REVOKE_KEY_FAILED", err.Error())
		return
	}

	RecordKeyRevoked()
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// GetActiveKeys returns all active keys (for WS Gateway).
func (h *Handler) GetActiveKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.service.GetActiveKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GET_KEYS_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"keys": keys,
	})
}

// CreateTopics creates topics for a tenant.
func (h *Handler) CreateTopics(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.CreateTopicsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	topics, err := h.service.CreateTopics(r.Context(), tenantID, req.Categories)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CREATE_TOPICS_FAILED", err.Error())
		return
	}

	topicNames := make([]string, len(topics))
	for i, t := range topics {
		topicNames[i] = t.TopicName
	}

	RecordTopicCreated(len(topics))
	writeJSON(w, http.StatusCreated, map[string]any{
		"topics": topicNames,
	})
}

// ListTopics returns all topics for a tenant.
func (h *Handler) ListTopics(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	topics, err := h.service.ListTopics(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_TOPICS_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"topics": topics,
	})
}

// GetQuota returns quotas for a tenant.
func (h *Handler) GetQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	quota, err := h.service.GetQuota(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusNotFound, "QUOTA_NOT_FOUND", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, quota)
}

// UpdateQuota updates quotas for a tenant.
func (h *Handler) UpdateQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")

	var req provisioning.UpdateQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}

	quota, err := h.service.UpdateQuota(r.Context(), tenantID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "UPDATE_QUOTA_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, quota)
}

// GetAuditLog returns audit entries for a tenant.
func (h *Handler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	opts := parseListOptions(r)

	entries, total, err := h.service.GetAuditLog(r.Context(), tenantID, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GET_AUDIT_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"total":   total,
		"limit":   opts.Limit,
		"offset":  opts.Offset,
	})
}

// parseListOptions extracts pagination options from query params.
func parseListOptions(r *http.Request) provisioning.ListOptions {
	opts := provisioning.ListOptions{
		Limit:  50,
		Offset: 0,
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 100 {
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

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// APIError represents an error response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{Code: code, Message: message})
}
