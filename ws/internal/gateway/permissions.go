package gateway

import (
	"slices"

	"github.com/klurvio/sukko/internal/shared/auth"
)

// Note: This package uses auth.MatchWildcard for pattern matching and
// auth.MatchPattern for placeholder extraction from channel patterns.

// PermissionChecker validates channel subscription permissions based on JWT claims.
// It supports three channel categories:
// - Public: Any authenticated user can subscribe (e.g., *.trade, *.liquidity)
// - User-scoped: JWT.sub must match the principal in the channel (e.g., balances.{principal})
// - Group-scoped: JWT.groups must contain the group_id in the channel (e.g., community.{group_id})
type PermissionChecker struct {
	publicPatterns      []string
	userScopedPatterns  []string
	groupScopedPatterns []string
}

// NewPermissionChecker creates a new permission checker with the given patterns.
func NewPermissionChecker(publicPatterns, userScopedPatterns, groupScopedPatterns []string) *PermissionChecker {
	return &PermissionChecker{
		publicPatterns:      publicPatterns,
		userScopedPatterns:  userScopedPatterns,
		groupScopedPatterns: groupScopedPatterns,
	}
}

// CanSubscribe checks if the given claims allow subscription to the channel.
// Returns true if the subscription is allowed, false otherwise.
// When claims is nil (API-key-only connections), only public channels are allowed.
func (pc *PermissionChecker) CanSubscribe(claims *auth.Claims, channel string) bool {
	// Check public patterns first (any authenticated user or API-key-only)
	if pc.matchesPublic(channel) {
		return true
	}

	// Non-public channels require JWT claims — API-key-only connections (nil claims) are denied.
	if claims == nil {
		return false
	}

	// Check user-scoped patterns (JWT.sub must match principal)
	if principal := pc.extractUserPrincipal(channel); principal != "" {
		return claims.Subject == principal
	}

	// Check group-scoped patterns (JWT.groups must contain group_id)
	if groupID := pc.extractGroupID(channel); groupID != "" {
		return pc.containsGroup(claims.Groups, groupID)
	}

	// No matching pattern - deny by default
	return false
}

// FilterChannels filters a list of channels to only those the claims allow.
// Returns the list of allowed channels.
func (pc *PermissionChecker) FilterChannels(claims *auth.Claims, channels []string) []string {
	allowed := make([]string, 0, len(channels))
	for _, ch := range channels {
		if pc.CanSubscribe(claims, ch) {
			allowed = append(allowed, ch)
		}
	}
	return allowed
}

// matchesPublic checks if the channel matches any public pattern.
// Patterns support wildcards: *.trade matches BTC.trade, ETH.trade, etc.
func (pc *PermissionChecker) matchesPublic(channel string) bool {
	return auth.MatchAnyWildcard(pc.publicPatterns, channel)
}

// extractUserPrincipal extracts the principal from a user-scoped channel.
// For pattern "balances.{principal}" and channel "balances.abc123", returns "abc123".
// Returns empty string if no user-scoped pattern matches.
func (pc *PermissionChecker) extractUserPrincipal(channel string) string {
	for _, pattern := range pc.userScopedPatterns {
		result := auth.MatchPattern(pattern, channel)
		if result.Matched {
			if principal, ok := result.Captures["principal"]; ok && principal != "" {
				return principal
			}
		}
	}
	return ""
}

// extractGroupID extracts the group_id from a group-scoped channel.
// For pattern "community.{group_id}" and channel "community.crypto-traders", returns "crypto-traders".
// Returns empty string if no group-scoped pattern matches.
func (pc *PermissionChecker) extractGroupID(channel string) string {
	for _, pattern := range pc.groupScopedPatterns {
		result := auth.MatchPattern(pattern, channel)
		if result.Matched {
			if groupID, ok := result.Captures["group_id"]; ok && groupID != "" {
				return groupID
			}
		}
	}
	return ""
}

// containsGroup checks if the groups slice contains the target group.
func (pc *PermissionChecker) containsGroup(groups []string, target string) bool {
	return slices.Contains(groups, target)
}
