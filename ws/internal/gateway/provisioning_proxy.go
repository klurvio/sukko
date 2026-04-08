package gateway

import (
	"fmt"
	"net/http"
	nethttputil "net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/httputil"
)

// Prometheus metrics for the provisioning proxy.
var (
	proxyRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_provisioning_proxy_requests_total",
		Help: "Total requests proxied to provisioning service",
	}, []string{"method", "status"})

	proxyDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gateway_provisioning_proxy_duration_seconds",
		Help:    "Latency of proxied requests to provisioning service",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})
)

// ProvisioningProxy is an HTTP reverse proxy that forwards unmatched requests
// to the provisioning service. It implements http.Handler.
type ProvisioningProxy struct {
	proxy  *nethttputil.ReverseProxy
	target *url.URL
	logger zerolog.Logger
}

// NewProvisioningProxy creates a reverse proxy targeting the provisioning service.
// The targetURL must be a valid http:// or https:// URL.
func NewProvisioningProxy(targetURL string, logger zerolog.Logger) (*ProvisioningProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse provisioning proxy target URL: %w", err)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("provisioning proxy target URL scheme must be http or https, got %q", target.Scheme)
	}

	p := &ProvisioningProxy{
		target: target,
		logger: logger.With().Str("component", "provisioning_proxy").Logger(),
	}

	p.proxy = &nethttputil.ReverseProxy{
		Director:     p.director,
		ErrorHandler: p.errorHandler,
	}

	return p, nil
}

// ServeHTTP proxies the request to provisioning and records metrics.
func (p *ProvisioningProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Wrap ResponseWriter to capture status code for metrics
	rw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

	p.proxy.ServeHTTP(rw, r)

	elapsed := time.Since(start)
	status := strconv.Itoa(rw.statusCode)

	proxyRequestsTotal.WithLabelValues(r.Method, status).Inc()
	proxyDuration.WithLabelValues(r.Method).Observe(elapsed.Seconds())

	p.logger.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Int("status", rw.statusCode).
		Dur("duration", elapsed).
		Msg("proxied request to provisioning")
}

// director rewrites the request URL to target provisioning and sets forwarding headers.
func (p *ProvisioningProxy) director(req *http.Request) {
	originalHost := req.Host // capture before overwrite for X-Forwarded-Host

	req.URL.Scheme = p.target.Scheme
	req.URL.Host = p.target.Host
	req.Host = p.target.Host

	// Preserve client IP for provisioning's rate limiting and audit logging
	clientIP := httputil.GetClientIP(req)
	if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
		req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		req.Header.Set("X-Forwarded-For", clientIP)
	}
	req.Header.Set("X-Forwarded-Host", originalHost)
}

// errorHandler is called when the proxy cannot reach provisioning.
func (p *ProvisioningProxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	p.logger.Warn().Err(err).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("provisioning proxy error")

	httputil.WriteError(w, http.StatusBadGateway, "BAD_GATEWAY", "provisioning service unavailable")
}

// statusResponseWriter wraps http.ResponseWriter to capture the status code.
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *statusResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b) //nolint:wrapcheck // ResponseWriter wrapper — must preserve the interface contract
}
