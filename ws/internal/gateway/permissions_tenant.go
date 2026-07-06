package gateway

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/provapi"
	"github.com/klurvio/sukko/internal/shared/types"
)

// principalPlaceholder is substituted with the JWT subject at match time.
// It is the only placeholder token supported in channel rule patterns
// (validated by types.ChannelRules.ValidateWithPlaceholders).
const principalPlaceholder = "{principal}"

// TenantPermissionChecker validates channel permissions using per-tenant rules
// streamed from provisioning. It is the sole channel-authorization source:
// a tenant with no rules configured is denied everything (fail closed), and
// unknown rules (initial snapshot not yet applied, or stream down with no
// cache) also deny — the distinction only drives health/degraded signals,
// never the authorization outcome.
type TenantPermissionChecker struct {
	provider ChannelRulesProvider
	logger   zerolog.Logger
}

// NewTenantPermissionChecker creates the checker. The provider is required —
// there is no fallback rule set (provisioning-only authorization).
func NewTenantPermissionChecker(provider ChannelRulesProvider, logger zerolog.Logger) (*TenantPermissionChecker, error) {
	if provider == nil {
		return nil, errors.New("channel rules provider is required")
	}
	return &TenantPermissionChecker{
		provider: provider,
		logger:   logger,
	}, nil
}

// CanSubscribe checks if the connection may subscribe to the (tenant-stripped)
// channel. Nil claims (API-key-only connections) are checked against the
// tenant's public patterns only.
func (pc *TenantPermissionChecker) CanSubscribe(ctx context.Context, tenantID string, claims *auth.Claims, channel string) bool {
	rules := pc.getRulesForTenant(ctx, tenantID)
	allowed := auth.MatchAnyWildcard(pc.subscribePatterns(rules, claims), channel)

	result := ChannelCheckDenied
	if allowed {
		result = ChannelCheckAllowed
	}
	RecordChannelAuthorization(result)

	if !allowed {
		pc.logger.Debug().
			Str("tenant_id", tenantID).
			Str("channel", channel).
			Msg("Channel subscription denied")
	}
	return allowed
}

// FilterChannels filters (tenant-stripped) channels to those the connection
// may subscribe to. Nil claims are checked against public patterns only.
func (pc *TenantPermissionChecker) FilterChannels(ctx context.Context, tenantID string, claims *auth.Claims, channels []string) []string {
	rules := pc.getRulesForTenant(ctx, tenantID)
	patterns := pc.subscribePatterns(rules, claims)

	allowed := make([]string, 0, len(channels))
	for _, ch := range channels {
		if auth.MatchAnyWildcard(patterns, ch) {
			allowed = append(allowed, ch)
		}
	}
	return allowed
}

// CanPublish checks if the connection may publish to the (tenant-stripped)
// channel. Publish always requires JWT claims — API-key-only connections are
// read-only and denied regardless of rules.
func (pc *TenantPermissionChecker) CanPublish(ctx context.Context, tenantID string, claims *auth.Claims, channel string) bool {
	if claims == nil {
		return false
	}
	rules := pc.getRulesForTenant(ctx, tenantID)
	patterns := substitutePrincipal(rules.ComputeAllowedPublishPatterns(claims.Groups), claims)
	allowed := auth.MatchAnyWildcard(patterns, channel)

	result := ChannelCheckDenied
	if allowed {
		result = ChannelCheckAllowed
	}
	RecordChannelAuthorization(result)

	if !allowed {
		pc.logger.Debug().
			Str("tenant_id", tenantID).
			Str("channel", channel).
			Msg("Channel publish denied")
	}
	return allowed
}

// subscribePatterns computes the subscribe-side patterns for the connection,
// with {principal} substituted. Nil claims → public patterns only.
func (pc *TenantPermissionChecker) subscribePatterns(rules *types.ChannelRules, claims *auth.Claims) []string {
	if claims == nil {
		return substitutePrincipal(rules.Public, nil)
	}
	return substitutePrincipal(rules.ComputeAllowedPatterns(claims.Groups), claims)
}

// substitutePrincipal replaces the {principal} placeholder in each pattern
// with the JWT subject. With nil claims (or an empty subject), patterns
// containing {principal} are dropped — they can never legitimately match.
func substitutePrincipal(patterns []string, claims *auth.Claims) []string {
	subject := ""
	if claims != nil {
		subject = claims.Subject
	}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if strings.Contains(p, principalPlaceholder) {
			if subject == "" {
				continue
			}
			p = strings.ReplaceAll(p, principalPlaceholder, subject)
		}
		out = append(out, p)
	}
	return out
}

// getRulesForTenant returns the tenant's channel rules, or empty rules
// (deny-all) when none exist. There is no fallback rule set. The
// none-vs-unknown distinction affects only observability:
//   - none configured (snapshot applied, tenant absent): expected, healthy.
//   - unknown (initial snapshot not applied, or stream down): fail closed,
//     surfaced as degraded via lookup metrics and warn logs; readiness
//     separately gates on SnapshotReceived (see Gateway.streamStatus).
func (pc *TenantPermissionChecker) getRulesForTenant(ctx context.Context, tenantID string) *types.ChannelRules {
	rules, err := pc.provider.GetChannelRules(ctx, tenantID)
	if err == nil {
		return rules
	}

	rulesUnknown := !pc.provider.SnapshotReceived() || pc.provider.State() == provapi.StreamStateDisconnected
	switch {
	case errors.Is(err, types.ErrChannelRulesNotFound) && !rulesUnknown:
		// Expected: snapshot applied and this tenant has no rules → deny-all.
		RecordChannelRulesLookup(LookupSourceFallback)
	default:
		// Rules unknown (cold start or stream down without cache) or an
		// unexpected provider error: fail closed and surface degradation.
		pc.logger.Warn().
			Err(err).
			Str("tenant_id", tenantID).
			Bool("snapshot_received", pc.provider.SnapshotReceived()).
			Msg("Channel rules unavailable, denying (fail closed)")
		RecordChannelRulesLookup(LookupSourceErrorFallback)
	}
	return types.NewChannelRules()
}
