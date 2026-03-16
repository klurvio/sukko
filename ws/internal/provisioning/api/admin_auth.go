package api

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"

	"github.com/Toniq-Labs/odin-ws/internal/shared/auth"
	"github.com/Toniq-Labs/odin-ws/internal/shared/httputil"
	"github.com/Toniq-Labs/odin-ws/internal/shared/logging"
	pkgmetrics "github.com/Toniq-Labs/odin-ws/internal/shared/metrics"
)

// AdminAuthConfig holds rate limiting configuration for admin token authentication.
type AdminAuthConfig struct {
	// FailureThreshold is the number of failures before rate limiting kicks in.
	FailureThreshold int
	// BlockDuration is how long to block after exceeding the threshold.
	BlockDuration time.Duration
	// CleanupInterval is how often expired entries are swept.
	CleanupInterval time.Duration
	// CleanupMaxAge is the max age for entries before cleanup.
	CleanupMaxAge time.Duration
}

// Prometheus metrics for admin token auth.
var (
	adminAuthTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "provisioning_admin_auth_total",
		Help: "Total admin token authentication attempts",
	}, []string{"result"})

	adminAuthRateLimitedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "provisioning_admin_auth_rate_limited_total",
		Help: "Total admin auth requests rate limited",
	})
)

// ipFailure tracks auth failures for a single IP.
// Instances are immutable once stored in the sync.Map — updates
// store a new *ipFailure rather than mutating fields in place.
type ipFailure struct {
	count   int
	resetAt time.Time
}

// AdminAuth provides admin token authentication middleware with
// per-IP rate limiting for failed auth attempts.
type AdminAuth struct {
	adminToken []byte
	config     AdminAuthConfig
	failures   sync.Map // map[string]*ipFailure
	logger     zerolog.Logger
	cancel     context.CancelFunc
	wg         *sync.WaitGroup
}

// NewAdminAuth creates a new AdminAuth middleware. If adminToken is empty,
// the middleware is a no-op passthrough. The cleanup goroutine follows
// Constitution VII lifecycle: wg.Add before go, RecoverPanic first defer,
// wg.Done second defer.
func NewAdminAuth(ctx context.Context, wg *sync.WaitGroup, adminToken string, authCfg AdminAuthConfig, logger zerolog.Logger) *AdminAuth {
	cleanupCtx, cancel := context.WithCancel(ctx)

	aa := &AdminAuth{
		adminToken: []byte(adminToken),
		config:     authCfg,
		logger:     logger.With().Str("component", "admin_auth").Logger(),
		cancel:     cancel,
		wg:         wg,
	}

	// Start cleanup goroutine only if admin auth is enabled
	if adminToken != "" {
		wg.Go(func() {
			defer logging.RecoverPanic(aa.logger, "admin_auth_cleanup", nil)

			ticker := time.NewTicker(authCfg.CleanupInterval)
			defer ticker.Stop()

			for {
				select {
				case <-cleanupCtx.Done():
					return
				case <-ticker.C:
					aa.cleanupFailures()
				}
			}
		})
	}

	return aa
}

// Middleware returns an HTTP middleware that checks for admin token authentication.
// If the admin token is empty, the middleware is a no-op.
// If the token matches, it sets admin claims in context and calls next.
// If the token doesn't match, it falls through to the next middleware (does NOT reject).
func (aa *AdminAuth) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If no admin token configured, skip entirely
			if len(aa.adminToken) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Extract Bearer token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				next.ServeHTTP(w, r)
				return
			}

			scheme, tokenString, found := strings.Cut(authHeader, " ")
			if !found || !strings.EqualFold(scheme, "Bearer") {
				next.ServeHTTP(w, r)
				return
			}

			// Check per-IP rate limiting
			clientIP := extractIP(r)
			if aa.isRateLimited(clientIP) {
				adminAuthRateLimitedTotal.Inc()
				httputil.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many authentication failures")
				return
			}

			// Constant-time comparison to prevent timing attacks
			tokenBytes := []byte(tokenString)
			if subtle.ConstantTimeCompare(tokenBytes, aa.adminToken) == 1 {
				// Token matches — set admin claims in context
				adminClaims := &auth.Claims{
					Roles: []string{"admin", "system"},
				}
				ctx := auth.WithClaims(r.Context(), adminClaims)
				ctx = auth.WithActor(ctx, "admin", "admin", clientIP)

				adminAuthTotal.WithLabelValues(pkgmetrics.AuthStatusSuccess).Inc()

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Token didn't match — record failure and fall through to JWT auth
			aa.recordFailure(clientIP)
			adminAuthTotal.WithLabelValues(adminAuthResultFallthrough).Inc()

			next.ServeHTTP(w, r)
		})
	}
}

// Close cancels the cleanup goroutine.
func (aa *AdminAuth) Close() {
	aa.cancel()
}

// isRateLimited checks if an IP has exceeded the failure threshold.
func (aa *AdminAuth) isRateLimited(ip string) bool {
	v, ok := aa.failures.Load(ip)
	if !ok {
		return false
	}

	f := v.(*ipFailure)
	now := time.Now()

	// If past the reset time, the entry has expired
	if now.After(f.resetAt) {
		aa.failures.Delete(ip)
		return false
	}

	return f.count >= aa.config.FailureThreshold
}

// recordFailure tracks an auth failure for an IP.
// Uses immutable snapshots: reads the current value, then stores a new
// *ipFailure with the updated count. This eliminates data races because
// sync.Map operations are synchronized and we never mutate stored values.
func (aa *AdminAuth) recordFailure(ip string) {
	now := time.Now()
	resetAt := now.Add(aa.config.BlockDuration)
	fresh := &ipFailure{count: 1, resetAt: resetAt}

	v, loaded := aa.failures.LoadOrStore(ip, fresh)
	if !loaded {
		return // new entry stored
	}

	f := v.(*ipFailure)
	if now.After(f.resetAt) {
		// Expired — replace with fresh entry
		aa.failures.Store(ip, fresh)
		return
	}

	// Store a new immutable snapshot with incremented count
	aa.failures.Store(ip, &ipFailure{
		count:   f.count + 1,
		resetAt: resetAt,
	})
}

// cleanupFailures removes expired rate limit entries to prevent unbounded memory growth.
func (aa *AdminAuth) cleanupFailures() {
	cutoff := time.Now().Add(-aa.config.CleanupMaxAge)
	removed := 0

	aa.failures.Range(func(key, value any) bool {
		f := value.(*ipFailure)
		if f.resetAt.Before(cutoff) {
			aa.failures.Delete(key)
			removed++
		}
		return true
	})

	if removed > 0 {
		aa.logger.Debug().Int("removed", removed).Msg("cleaned up expired rate limit entries")
	}
}

// extractIP extracts the client IP from the request, stripping the port.
func extractIP(r *http.Request) string {
	// Use X-Real-IP if available (set by RealIP middleware)
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
