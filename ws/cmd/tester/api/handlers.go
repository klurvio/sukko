package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/klurvio/sukko/cmd/tester/runner"
	"github.com/klurvio/sukko/internal/shared/httputil"
	"github.com/rs/zerolog"
)

const testIDLength = 8

const maxRequestBodySize = 1 << 20 // 1MB

type handlers struct {
	runner    *runner.Runner
	authToken string
	logger    zerolog.Logger
}

func (h *handlers) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authToken == "" {
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		expected := "Bearer " + h.authToken
		if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			httputil.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing authorization token")
			return
		}
		next(w, r)
	}
}

type startTestRequest struct {
	Type        string `json:"type"`
	Connections int    `json:"connections,omitempty"`
	Duration    string `json:"duration,omitempty"`
	PublishRate int    `json:"publish_rate,omitempty"`
	RampRate    int    `json:"ramp_rate,omitempty"`
	Suite       string `json:"suite,omitempty"`
}

func (h *handlers) startTest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req startTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Debug().Err(err).Msg("invalid request body")
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	if req.Type == "" {
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_TYPE", "test type is required")
		return
	}

	switch runner.TestType(req.Type) {
	case runner.TestSmoke, runner.TestLoad, runner.TestStress, runner.TestSoak, runner.TestValidate:
		// valid
	default:
		httputil.WriteError(w, http.StatusBadRequest, "INVALID_TYPE", fmt.Sprintf("unknown test type: %q", req.Type))
		return
	}

	id, err := uuid.NewRandom()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to generate test ID")
		return
	}
	testID := id.String()[:testIDLength]

	cfg := runner.TestConfig{
		Type:        runner.TestType(req.Type),
		Connections: req.Connections,
		Duration:    req.Duration,
		PublishRate: req.PublishRate,
		RampRate:    req.RampRate,
		Suite:       req.Suite,
	}

	run, err := h.runner.Start(testID, cfg)
	if err != nil {
		status := http.StatusInternalServerError
		code := "TEST_ERROR"
		msg := "failed to start test"
		if errors.Is(err, runner.ErrTestAlreadyExists) {
			status = http.StatusConflict
			code = "CONFLICT"
			msg = "a test with this ID already exists"
		}
		httputil.WriteError(w, status, code, msg)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     run.ID,
		"status": run.Status,
		"config": run.Config,
	})
}

func (h *handlers) stopTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.runner.Stop(id); err != nil {
		if errors.Is(err, runner.ErrTestNotFound) {
			httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("test %q not found", id))
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to stop test")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (h *handlers) getTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.runner.Get(id)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("test %q not found", id))
		return
	}

	status, report := run.StatusSnapshot()
	resp := map[string]any{
		"id":     run.ID,
		"status": status,
		"config": run.Config,
	}
	if report != nil {
		resp["report"] = report
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handlers) streamMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.runner.Get(id)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("test %q not found", id))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.WriteError(w, http.StatusInternalServerError, "STREAMING_UNSUPPORTED", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			snapshot := run.Collector.Snapshot()
			data, _ := json.Marshal(snapshot) //nolint:errcheck // MetricsSnapshot fields are all JSON-serializable primitives
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()

			// Stop streaming when test completes
			status, report := run.StatusSnapshot()
			if status == runner.StatusComplete || status == runner.StatusFailed || status == runner.StatusStopped {
				if report != nil {
					finalData, _ := json.Marshal(report) //nolint:errcheck // Report fields are all JSON-serializable primitives
					if _, err := fmt.Fprintf(w, "event: complete\ndata: %s\n\n", finalData); err != nil {
						return
					}
					flusher.Flush()
				}
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	_ = httputil.WriteJSON(w, status, data) // best-effort: client may have disconnected
}
