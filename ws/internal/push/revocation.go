package push

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/klurvio/sukko/internal/shared/provapi"
)

var pushRegistrationsRevokedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "push_registrations_revoked_total",
	Help: "Push registrations deleted due to token revocation",
}, []string{"type"})

// HandleRevocation processes a token revocation event from the gRPC stream.
// Deletes matching device registrations from the database.
func (s *Service) HandleRevocation(entry provapi.RevocationEntry) {
	ctx := context.Background()

	switch entry.Type {
	case "token":
		count, err := s.repo.DeleteByJTI(ctx, entry.TenantID, entry.JTI)
		if err != nil {
			s.logger.Error().Err(err).
				Str("jti", entry.JTI).
				Str("tenant_id", entry.TenantID).
				Msg("push revocation: delete by jti failed")
			return
		}
		if count > 0 {
			pushRegistrationsRevokedTotal.WithLabelValues("token").Add(float64(count))
			s.logger.Info().
				Str("jti", entry.JTI).
				Str("tenant_id", entry.TenantID).
				Int("deleted", count).
				Str("revocation_type", "token").
				Msg("push registrations deleted: token revoked")
		}

	case "user":
		count, err := s.repo.DeleteBySub(ctx, entry.TenantID, entry.Sub, entry.RevokedAt)
		if err != nil {
			s.logger.Error().Err(err).
				Str("sub", entry.Sub).
				Str("tenant_id", entry.TenantID).
				Msg("push revocation: delete by sub failed")
			return
		}
		if count > 0 {
			pushRegistrationsRevokedTotal.WithLabelValues("user").Add(float64(count))
			s.logger.Info().
				Str("sub", entry.Sub).
				Str("tenant_id", entry.TenantID).
				Int("deleted", count).
				Str("revocation_type", "user").
				Msg("push registrations deleted: user revoked")
		}
	}
}
