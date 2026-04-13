package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/revocation"
	"github.com/klurvio/sukko/internal/shared/httputil"
)

var revocationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "provisioning_token_revocations_total",
	Help: "Total token revocations by type and result",
}, []string{"type", "result"})

// revocationRequest is the JSON body for POST /api/v1/tenants/{tenantID}/tokens/revoke.
type revocationRequest struct {
	Sub string `json:"sub,omitempty"`
	JTI string `json:"jti,omitempty"`
	Exp *int64 `json:"exp,omitempty"` // Optional: explicit expiry for auto-prune
}

// revocationResponse is returned on successful revocation.
type revocationResponse struct {
	Status    string `json:"status"`
	Type      string `json:"type"`
	TenantID  string `json:"tenant_id"`
	ExpiresAt string `json:"expires_at"`
}

// RevocationHandler handles token revocation requests.
type RevocationHandler struct {
	store                 *revocation.Store
	eventBus              *eventbus.Bus
	logger                zerolog.Logger
	limiter               *ipRateLimiter
	maxRevocationLifetime time.Duration
}

// NewRevocationHandler creates a handler for the token revocation endpoint.
func NewRevocationHandler(store *revocation.Store, bus *eventbus.Bus, maxLifetime time.Duration, logger zerolog.Logger) *RevocationHandler {
	return &RevocationHandler{
		store:                 store,
		eventBus:              bus,
		logger:                logger.With().Str("handler", "revocation").Logger(),
		limiter:               newIPRateLimiter(rate.Every(2*time.Second), 5), // 30 req/min with burst of 5
		maxRevocationLifetime: maxLifetime,
	}
}

// HandleRevoke processes POST /api/v1/tenants/{tenantID}/tokens/revoke.
func (h *RevocationHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	clientIP := httputil.GetClientIP(r)
	if !h.limiter.allow(clientIP) {
		revocationTotal.WithLabelValues("unknown", "rate_limited").Inc()
		httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many revocation requests")
		return
	}

	// Parse request
	var req revocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		revocationTotal.WithLabelValues("unknown", "invalid_request").Inc()
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate: exactly one of sub or jti
	hasSub := req.Sub != ""
	hasJTI := req.JTI != ""
	if !hasSub && !hasJTI {
		revocationTotal.WithLabelValues("unknown", "invalid_request").Inc()
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "one of 'sub' or 'jti' is required")
		return
	}
	if hasSub && hasJTI {
		revocationTotal.WithLabelValues("unknown", "invalid_request").Inc()
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "provide either 'sub' or 'jti', not both")
		return
	}

	// Determine type and tenant (from URL path via RequireTenant middleware)
	tenantID := chi.URLParam(r, "tenantID")
	revType := "user"
	if hasJTI {
		revType = "token"
	}

	// Compute expiry
	now := time.Now()
	expiresAt := now.Add(h.maxRevocationLifetime).Unix()
	if req.Exp != nil && *req.Exp > now.Unix() {
		expiresAt = *req.Exp
	}

	// Store revocation
	entry := revocation.Entry{
		TenantID:  tenantID,
		Type:      revType,
		Sub:       req.Sub,
		JTI:       req.JTI,
		RevokedAt: now.Unix(),
		ExpiresAt: expiresAt,
	}
	if err := h.store.Revoke(entry); err != nil {
		revocationTotal.WithLabelValues(revType, "error").Inc()
		h.logger.Error().Err(err).Str("type", revType).Str("tenant_id", tenantID).Msg("revocation store error")
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to store revocation")
		return
	}

	// Publish event for gRPC stream
	h.eventBus.Publish(eventbus.Event{Type: eventbus.TokenRevocationsChanged})

	revocationTotal.WithLabelValues(revType, "success").Inc()

	h.logger.Info().
		Str("type", revType).
		Str("tenant_id", tenantID).
		Str("sub", req.Sub).
		Str("jti", req.JTI).
		Str("client_ip", clientIP).
		Msg("token revoked")

	_ = httputil.WriteJSON(w, http.StatusOK, revocationResponse{
		Status:    "revoked",
		Type:      revType,
		TenantID:  tenantID,
		ExpiresAt: time.Unix(expiresAt, 0).UTC().Format(time.RFC3339),
	})
}
