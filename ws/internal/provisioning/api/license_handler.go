package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/klurvio/sukko/internal/provisioning/eventbus"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/klurvio/sukko/internal/shared/license"
)

// Prometheus metrics for license reload.
var (
	licenseReloadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "provisioning_license_reload_total",
		Help: "Total license reload attempts by result",
	}, []string{"result"})

	licenseEditionGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "provisioning_license_edition",
		Help: "Current license edition (1 = active for the labeled edition)",
	}, []string{"edition"})
)

// License reload result labels.
const (
	reloadSuccess          = "success"
	reloadInvalidSignature = "invalid_signature"
	reloadExpired          = "expired"
	reloadReplay           = "replay_detected"
	reloadInvalidFormat    = "invalid_format"
	reloadInternalError    = "internal_error"
)

// LicenseHandler handles POST /api/v1/license for license hot-reload.
// No auth required — the license key's Ed25519 signature is the authentication.
type LicenseHandler struct {
	manager       *license.Manager
	licenseRepo   *repository.LicenseStateRepository
	eventBus      *eventbus.Bus
	logger        zerolog.Logger
	currentKeyMu  sync.Mutex // guards currentKey read/write
	currentKey    string     // raw key string for WatchLicense snapshot
	rateLimiter   *ipRateLimiter
}

// ipRateLimiter provides per-IP rate limiting for the license endpoint.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
}

func newIPRateLimiter(r rate.Limit, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     r,
		burst:    burst,
	}
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	limiter, ok := l.limiters[ip]
	if !ok {
		limiter = rate.NewLimiter(l.rate, l.burst)
		l.limiters[ip] = limiter
	}
	l.mu.Unlock()
	return limiter.Allow()
}

// NewLicenseHandler creates a LicenseHandler.
func NewLicenseHandler(
	manager *license.Manager,
	licenseRepo *repository.LicenseStateRepository,
	eventBus *eventbus.Bus,
	logger zerolog.Logger,
) *LicenseHandler {
	return &LicenseHandler{
		manager:     manager,
		licenseRepo: licenseRepo,
		eventBus:    eventBus,
		logger:      logger.With().Str("component", "license_handler").Logger(),
		rateLimiter: newIPRateLimiter(rate.Every(6*time.Second), 3), // ~10 req/min with burst of 3
	}
}

// SetCurrentKey sets the raw license key string for WatchLicense snapshot access.
// Called on startup (after loading from DB/env) and after each successful reload.
func (h *LicenseHandler) SetCurrentKey(key string) {
	h.currentKeyMu.Lock()
	h.currentKey = key
	h.currentKeyMu.Unlock()
}

// CurrentKey returns the raw license key string for WatchLicense snapshot.
func (h *LicenseHandler) CurrentKey() string {
	h.currentKeyMu.Lock()
	defer h.currentKeyMu.Unlock()
	return h.currentKey
}

// licenseReloadRequest is the JSON body for POST /api/v1/license.
type licenseReloadRequest struct {
	Key string `json:"key"`
}

// licenseReloadResponse is the JSON response on successful reload.
type licenseReloadResponse struct {
	Edition   string `json:"edition"`
	Org       string `json:"org"`
	ExpiresAt string `json:"expires_at"`
	Status    string `json:"status"`
}

// HandleReload handles POST /api/v1/license.
// No auth — Ed25519 signature on the key is the authentication.
// Rate-limited per IP. Replay-protected via iat.
func (h *LicenseHandler) HandleReload(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	clientIP := httputil.GetClientIP(r)
	if !h.rateLimiter.allow(clientIP) {
		licenseReloadTotal.WithLabelValues("rate_limited").Inc()
		httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many license reload requests")
		return
	}

	// Parse request body (limit to 64KB — a license key is ~200 bytes)
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req licenseReloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		licenseReloadTotal.WithLabelValues(reloadInvalidFormat).Inc()
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if req.Key == "" {
		licenseReloadTotal.WithLabelValues(reloadInvalidFormat).Inc()
		httputil.WriteError(w, http.StatusBadRequest, "MISSING_KEY", "key field is required")
		return
	}

	// Reload Manager (validates Ed25519 signature, checks iat replay, atomic swap)
	if err := h.manager.Reload(req.Key); err != nil {
		h.handleReloadError(w, err, clientIP)
		return
	}

	// After successful Reload(), claims are guaranteed non-nil and non-expired
	// (ParseAndVerify validated signature + expiry).
	claims := h.manager.Claims()
	expiresAt := time.Unix(claims.Exp, 0)
	edition := claims.Edition.String()
	org := claims.Org

	// Persist to DB (encrypted)
	if err := h.licenseRepo.Upsert(r.Context(), req.Key, edition, org, &expiresAt); err != nil {
		// Reload succeeded but persistence failed — log error, still return success
		// The key is active in memory; persistence will be retried on next reload
		h.logger.Error().Err(err).Msg("license reload succeeded but DB persistence failed")
	}

	// Update current key for WatchLicense snapshot
	h.SetCurrentKey(req.Key)

	// Publish event for WatchLicense stream
	h.eventBus.Publish(eventbus.Event{Type: eventbus.LicenseChanged})

	// Update Prometheus gauge
	licenseEditionGauge.Reset()
	licenseEditionGauge.WithLabelValues(edition).Set(1)

	licenseReloadTotal.WithLabelValues(reloadSuccess).Inc()

	h.logger.Info().
		Str("edition", edition).
		Str("org", org).
		Str("client_ip", clientIP).
		Msg("license reloaded via API")

	_ = httputil.WriteJSON(w, http.StatusOK, licenseReloadResponse{
		Edition:   edition,
		Org:       org,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		Status:    "reloaded",
	})
}

// handleReloadError classifies the reload error and returns the appropriate HTTP response.
func (h *LicenseHandler) handleReloadError(w http.ResponseWriter, err error, clientIP string) {
	switch {
	case errors.Is(err, license.ErrReplayDetected):
		licenseReloadTotal.WithLabelValues(reloadReplay).Inc()
		h.logger.Warn().Err(err).Str("client_ip", clientIP).Msg("license replay detected")
		httputil.WriteError(w, http.StatusConflict, "REPLAY_DETECTED", "key iat is not newer than current — replay rejected")

	case errors.Is(err, license.ErrLicenseExpired):
		licenseReloadTotal.WithLabelValues(reloadExpired).Inc()
		h.logger.Warn().Err(err).Str("client_ip", clientIP).Msg("expired license key pushed")
		httputil.WriteError(w, http.StatusBadRequest, "KEY_EXPIRED", "pushed key is already expired")

	case errors.Is(err, license.ErrLicenseInvalidSignature):
		licenseReloadTotal.WithLabelValues(reloadInvalidSignature).Inc()
		h.logger.Warn().Err(err).Str("client_ip", clientIP).Msg("invalid license signature")
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_SIGNATURE", "Ed25519 signature verification failed")

	case errors.Is(err, license.ErrLicenseInvalidFormat):
		licenseReloadTotal.WithLabelValues(reloadInvalidFormat).Inc()
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_FORMAT", "license key format invalid")

	default:
		licenseReloadTotal.WithLabelValues(reloadInternalError).Inc()
		h.logger.Error().Err(err).Str("client_ip", clientIP).Msg("license reload failed")
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL", "license reload failed")
	}
}
