package api

import (
	"encoding/json"
	"net/http"
	"strconv"
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

// Revocation type and Prometheus result label constants (Constitution §I).
const (
	revTypeUser    = "user"
	revTypeToken   = "token"
	revTypeUnknown = "unknown" // used in type label position when type is not yet known (early-exit paths)

	revResultInvalidRequest = "invalid_request"
	revResultRateLimited    = "rate_limited"
	revResultError          = "error"
	revResultSuccess        = "success"

	revocationRateLimitInterval = 2 * time.Second
	revocationRateLimitBurst    = 5
)

// revocationRequest is the JSON body for POST /api/v1/tenants/{tenantSlug}/tokens/revoke.
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
		limiter:               newIPRateLimiter(rate.Every(revocationRateLimitInterval), revocationRateLimitBurst),
		maxRevocationLifetime: maxLifetime,
	}
}

// HandleRevoke processes POST /api/v1/tenants/{tenantSlug}/tokens/revoke.
func (h *RevocationHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	clientIP := httputil.GetClientIP(r)
	if !h.limiter.allow(clientIP) {
		revocationTotal.WithLabelValues(revTypeUnknown, revResultRateLimited).Inc()
		w.Header().Set("Retry-After", strconv.Itoa(int(revocationRateLimitInterval.Seconds())))
		httputil.WriteError(w, http.StatusTooManyRequests, errCodeRateLimited, "too many revocation requests")
		return
	}

	// Parse request
	var req revocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		revocationTotal.WithLabelValues(revTypeUnknown, revResultInvalidRequest).Inc()
		httputil.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid JSON body")
		return
	}

	// Validate: exactly one of sub or jti
	hasSub := req.Sub != ""
	hasJTI := req.JTI != ""
	if !hasSub && !hasJTI {
		revocationTotal.WithLabelValues(revTypeUnknown, revResultInvalidRequest).Inc()
		httputil.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "one of 'sub' or 'jti' is required")
		return
	}
	if hasSub && hasJTI {
		revocationTotal.WithLabelValues(revTypeUnknown, revResultInvalidRequest).Inc()
		httputil.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "provide either 'sub' or 'jti', not both")
		return
	}

	// Determine type and tenant (from URL path via RequireTenant middleware)
	tenantSlug := chi.URLParam(r, "tenantSlug")
	revType := revTypeUser
	if hasJTI {
		revType = revTypeToken
	}

	// Compute expiry — client may supply exp to shorten the lifetime, but cannot exceed maxRevocationLifetime.
	now := time.Now()
	maxExp := now.Add(h.maxRevocationLifetime).Unix()
	expiresAt := maxExp
	if req.Exp != nil && *req.Exp > now.Unix() && *req.Exp < maxExp {
		expiresAt = *req.Exp
	}

	// Store revocation
	entry := revocation.Entry{
		TenantID:  tenantSlug,
		Type:      revType,
		Sub:       req.Sub,
		JTI:       req.JTI,
		RevokedAt: now.Unix(),
		ExpiresAt: expiresAt,
	}
	// Defensive: unreachable with the in-memory store (all conditions pre-validated above);
	// required when the store is migrated to a persistent backend.
	if err := h.store.Revoke(entry); err != nil {
		revocationTotal.WithLabelValues(revType, revResultError).Inc()
		h.logger.Error().Err(err).Str("type", revType).Str("tenant_id", tenantSlug).Msg("revocation store error")
		httputil.WriteError(w, http.StatusInternalServerError, errCodeInternal, "failed to store revocation")
		return
	}

	// Publish event for gRPC stream
	h.eventBus.Publish(eventbus.Event{Type: eventbus.TokenRevocationsChanged})

	revocationTotal.WithLabelValues(revType, revResultSuccess).Inc()

	logEvt := h.logger.Info().Str("type", revType).Str("tenant_id", tenantSlug).Str("client_ip", clientIP)
	if revType == revTypeUser {
		logEvt = logEvt.Str("sub", req.Sub)
	} else {
		logEvt = logEvt.Str("jti", req.JTI)
	}
	logEvt.Msg("token revoked")

	_ = httputil.WriteJSON(w, http.StatusOK, revocationResponse{
		Status:    statusRevoked,
		Type:      revType,
		TenantID:  tenantSlug,
		ExpiresAt: time.Unix(expiresAt, 0).UTC().Format(time.RFC3339),
	})
}
