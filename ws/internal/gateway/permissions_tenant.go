package gateway

import (
	"context"
	"errors"

	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/types"
)

// TenantPermissionChecker validates channel permissions using per-tenant rules.
// Falls back to default rules when tenant has no custom configuration.
type TenantPermissionChecker struct {
	provider      ChannelRulesProvider
	fallbackRules *types.ChannelRules
	logger        zerolog.Logger
}

// NewTenantPermissionChecker creates a permission checker with fallback rules.
func NewTenantPermissionChecker(provider ChannelRulesProvider, fallback *types.ChannelRules, logger zerolog.Logger) (*TenantPermissionChecker, error) {
	if provider == nil {
		return nil, errors.New("channel rules provider is required")
	}
	if fallback == nil {
		fallback = types.NewChannelRules()
	}
	return &TenantPermissionChecker{
		provider:      provider,
		fallbackRules: fallback,
		logger:        logger,
	}, nil
}

// CanSubscribe checks if the claims allow subscription to the channel.
func (pc *TenantPermissionChecker) CanSubscribe(ctx context.Context, claims *auth.Claims, channel string) bool {
	if claims == nil {
		return false
	}
	rules := pc.getRulesForTenant(ctx, claims.TenantID)

	// Compute allowed patterns for this user's groups (method on shared type)
	allowedPatterns := rules.ComputeAllowedPatterns(claims.Groups)

	// Check if channel matches any allowed pattern
	allowed := auth.MatchAnyWildcard(allowedPatterns, channel)

	// Record metric
	result := ChannelCheckDenied
	if allowed {
		result = ChannelCheckAllowed
	}
	RecordChannelAuthorization(claims.TenantID, result)

	if !allowed {
		pc.logger.Debug().
			Str("tenant_id", claims.TenantID).
			Str("channel", channel).
			Strs("groups", claims.Groups).
			Strs("allowed_patterns", allowedPatterns).
			Msg("Channel subscription denied")
	}

	return allowed
}

// FilterChannels filters a list of channels to only those the claims allow.
func (pc *TenantPermissionChecker) FilterChannels(ctx context.Context, claims *auth.Claims, channels []string) []string {
	if claims == nil {
		return nil
	}
	rules := pc.getRulesForTenant(ctx, claims.TenantID)
	allowedPatterns := rules.ComputeAllowedPatterns(claims.Groups)

	allowed := make([]string, 0, len(channels))
	for _, ch := range channels {
		if auth.MatchAnyWildcard(allowedPatterns, ch) {
			allowed = append(allowed, ch)
		}
	}

	return allowed
}

// getRulesForTenant returns channel rules for a tenant, falling back to defaults.
func (pc *TenantPermissionChecker) getRulesForTenant(ctx context.Context, tenantID string) *types.ChannelRules {
	rules, err := pc.provider.GetChannelRules(ctx, tenantID)
	if err != nil {
		if errors.Is(err, types.ErrChannelRulesNotFound) {
			// Expected: tenant has no custom rules, use fallback
			RecordChannelRulesLookup(tenantID, LookupSourceFallback)
		} else {
			// Unexpected error: log and use fallback (graceful degradation)
			pc.logger.Warn().
				Err(err).
				Str("tenant_id", tenantID).
				Msg("Failed to load channel rules, using fallback")
			RecordChannelRulesLookup(tenantID, LookupSourceErrorFallback)
		}
		return pc.fallbackRules
	}

	return rules
}

// DefaultChannelRules returns fallback rules from configuration.
func DefaultChannelRules(publicPatterns []string) *types.ChannelRules {
	return &types.ChannelRules{
		Public:        publicPatterns,
		GroupMappings: make(map[string][]string),
		Default:       []string{},
	}
}
