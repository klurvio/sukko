package authsvc

import (
	"time"

	"github.com/adred-codev/odin-ws/internal/auth"
)

// Service is the core auth service that handles token issuance.
type Service struct {
	config   *Config
	jwt      *auth.JWTValidator
	tokenExp time.Duration
	logger   *Logger // optional, for production logging
}

// New creates a new auth service with the given JWT secret.
// Config must be loaded separately via LoadConfig and set via SetConfig.
func New(jwtSecret string) *Service {
	return &Service{
		jwt:      auth.NewJWTValidator(jwtSecret),
		tokenExp: 24 * time.Hour,
	}
}

// SetConfig sets the tenant configuration.
func (s *Service) SetConfig(cfg *Config) {
	s.config = cfg
}

// SetTokenExpiry sets the token expiry duration.
func (s *Service) SetTokenExpiry(d time.Duration) {
	s.tokenExp = d
}

// SetLogger sets the logger for the service.
// If not set, logging is disabled (silent operation).
func (s *Service) SetLogger(l *Logger) {
	s.logger = l
}

// Logger returns the service logger (may be nil).
func (s *Service) Logger() *Logger {
	return s.logger
}

// IssueToken validates app credentials and issues a JWT token.
// Returns the token response or an error message and status code.
func (s *Service) IssueToken(appID, appSecret string) (*TokenResponse, string, int) {
	if s.config == nil {
		return nil, "Service not configured", 500
	}

	app := s.config.FindApp(appID)
	if app == nil {
		return nil, "Invalid credentials", 401
	}

	if app.Secret() != appSecret {
		return nil, "Invalid credentials", 401
	}

	token, expiresAt, err := s.jwt.IssueTokenWithTenant(app.ID, app.TenantID, s.tokenExp)
	if err != nil {
		return nil, "Failed to issue token", 500
	}

	return &TokenResponse{
		Token:     token,
		ExpiresAt: expiresAt.Unix(),
		TenantID:  app.TenantID,
	}, "", 200
}
