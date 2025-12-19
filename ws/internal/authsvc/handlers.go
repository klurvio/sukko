package authsvc

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/adred-codev/odin-ws/internal/version"
)

// Handler returns the HTTP handler for the auth service.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/token", s.handleIssueToken)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /version", version.Handler("auth-service"))
	return mux
}

// handleIssueToken handles POST /auth/token requests.
func (s *Service) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req TokenRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if s.logger != nil {
			s.logger.LogInvalidRequest(r, "invalid request body")
		}
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.AppID == "" || req.AppSecret == "" {
		if s.logger != nil {
			s.logger.LogInvalidRequest(r, "missing app_id or app_secret")
		}
		writeError(w, "app_id and app_secret are required", http.StatusBadRequest)
		return
	}

	resp, errMsg, statusCode := s.IssueToken(req.AppID, req.AppSecret)
	if errMsg != "" {
		if s.logger != nil {
			s.logger.LogAuthFailure(r, req.AppID, errMsg)
		}
		writeError(w, errMsg, statusCode)
		return
	}

	// Log successful authentication
	if s.logger != nil {
		s.logger.LogAuthSuccess(r, resp.TenantID, req.AppID, time.Unix(resp.ExpiresAt, 0), time.Since(start))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleHealth handles GET /health requests.
func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{
		Status:  "ok",
		Version: version.Get("auth-service"),
	})
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
