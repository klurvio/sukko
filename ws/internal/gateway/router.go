package gateway

import (
	"encoding/json"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// PodMetrics holds connection metrics for a single ws-server pod.
type PodMetrics struct {
	Connections int64
	MaxConns    int64
	LastUpdate  time.Time
}

// LeastConnRouter maintains connection metrics from all ws-server pods
// and selects the least-loaded pod for new connections.
type LeastConnRouter struct {
	pods     map[string]*PodMetrics // podIP -> metrics
	mu       sync.RWMutex
	nc       *nats.Conn
	sub      *nats.Subscription
	logger   zerolog.Logger
	staleTTL time.Duration // Consider pod stale after this duration

	// Metrics for observability
	updatesReceived atomic.Uint64
	podSelections   atomic.Uint64
	fallbackCount   atomic.Uint64
}

// metricsUpdate is the JSON structure published by ws-server LoadBalancer.
type metricsUpdate struct {
	PodIP       string `json:"pod_ip"`
	Connections int64  `json:"connections"`
	MaxConns    int64  `json:"max"`
	Timestamp   int64  `json:"ts"`
}

// NewLeastConnRouter creates a new router that subscribes to pod metrics via NATS.
func NewLeastConnRouter(nc *nats.Conn, logger zerolog.Logger) *LeastConnRouter {
	return &LeastConnRouter{
		pods:     make(map[string]*PodMetrics),
		nc:       nc,
		logger:   logger.With().Str("component", "least_conn_router").Logger(),
		staleTTL: 30 * time.Second,
	}
}

// Start subscribes to the pod metrics subject and begins receiving updates.
func (r *LeastConnRouter) Start() error {
	sub, err := r.nc.Subscribe("odin.lb.metrics", func(msg *nats.Msg) {
		var update metricsUpdate
		if err := json.Unmarshal(msg.Data, &update); err != nil {
			r.logger.Error().
				Err(err).
				Str("data", string(msg.Data)).
				Msg("Failed to unmarshal pod metrics")
			return
		}

		r.mu.Lock()
		r.pods[update.PodIP] = &PodMetrics{
			Connections: update.Connections,
			MaxConns:    update.MaxConns,
			LastUpdate:  time.Now(),
		}
		r.mu.Unlock()

		r.updatesReceived.Add(1)

		r.logger.Debug().
			Str("pod_ip", update.PodIP).
			Int64("connections", update.Connections).
			Int64("max", update.MaxConns).
			Msg("Updated pod metrics")
	})

	if err != nil {
		return err
	}

	r.sub = sub

	r.logger.Info().
		Str("subject", "odin.lb.metrics").
		Dur("stale_ttl", r.staleTTL).
		Msg("LeastConnRouter started")

	return nil
}

// Stop unsubscribes from NATS and cleans up.
func (r *LeastConnRouter) Stop() {
	if r.sub != nil {
		if err := r.sub.Drain(); err != nil {
			r.logger.Error().Err(err).Msg("Failed to drain subscription")
		}
	}
	r.logger.Info().Msg("LeastConnRouter stopped")
}

// SelectPod returns the IP of the pod with the fewest connections.
// Returns empty string if no suitable pod is available (fallback to K8s Service).
func (r *LeastConnRouter) SelectPod() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var selected string
	var minConns int64 = math.MaxInt64

	now := time.Now()
	for ip, m := range r.pods {
		// Skip stale pods (might be dead or unhealthy)
		if now.Sub(m.LastUpdate) > r.staleTTL {
			continue
		}

		// Skip pods at capacity
		if m.Connections >= m.MaxConns {
			continue
		}

		// Select pod with minimum connections
		if m.Connections < minConns {
			minConns = m.Connections
			selected = ip
		}
	}

	if selected != "" {
		r.podSelections.Add(1)
	} else {
		r.fallbackCount.Add(1)
	}

	return selected
}

// GetMetrics returns router statistics for observability.
func (r *LeastConnRouter) GetMetrics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	podStats := make([]map[string]interface{}, 0, len(r.pods))
	now := time.Now()

	for ip, m := range r.pods {
		isStale := now.Sub(m.LastUpdate) > r.staleTTL
		podStats = append(podStats, map[string]interface{}{
			"pod_ip":      ip,
			"connections": m.Connections,
			"max":         m.MaxConns,
			"stale":       isStale,
			"last_update": m.LastUpdate.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"pods":             podStats,
		"updates_received": r.updatesReceived.Load(),
		"pod_selections":   r.podSelections.Load(),
		"fallback_count":   r.fallbackCount.Load(),
	}
}

// ActivePodCount returns the number of non-stale pods.
func (r *LeastConnRouter) ActivePodCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	now := time.Now()
	for _, m := range r.pods {
		if now.Sub(m.LastUpdate) <= r.staleTTL {
			count++
		}
	}
	return count
}
