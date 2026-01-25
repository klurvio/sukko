package gateway

import (
	"slices"
	"strings"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
)

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
func (pc *PermissionChecker) CanSubscribe(claims *auth.Claims, channel string) bool {
	// Check public patterns first (any authenticated user)
	if pc.matchesPublic(channel) {
		return true
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
	for _, pattern := range pc.publicPatterns {
		if matchPattern(pattern, channel) {
			return true
		}
	}
	return false
}

// extractUserPrincipal extracts the principal from a user-scoped channel.
// For pattern "balances.{principal}" and channel "balances.abc123", returns "abc123".
// Returns empty string if no user-scoped pattern matches.
func (pc *PermissionChecker) extractUserPrincipal(channel string) string {
	for _, pattern := range pc.userScopedPatterns {
		if principal := extractPlaceholder(pattern, channel, "{principal}"); principal != "" {
			return principal
		}
	}
	return ""
}

// extractGroupID extracts the group_id from a group-scoped channel.
// For pattern "community.{group_id}" and channel "community.crypto-traders", returns "crypto-traders".
// Returns empty string if no group-scoped pattern matches.
func (pc *PermissionChecker) extractGroupID(channel string) string {
	for _, pattern := range pc.groupScopedPatterns {
		if groupID := extractPlaceholder(pattern, channel, "{group_id}"); groupID != "" {
			return groupID
		}
	}
	return ""
}

// containsGroup checks if the groups slice contains the target group.
func (pc *PermissionChecker) containsGroup(groups []string, target string) bool {
	return slices.Contains(groups, target)
}

// matchPattern matches a pattern against a channel.
// Supports * as a wildcard that matches any sequence of characters.
// Examples:
//   - "*.trade" matches "BTC.trade", "ETH.trade"
//   - "odin.*" matches "odin.trades", "odin.liquidity"
//   - "*" matches anything
func matchPattern(pattern, channel string) bool {
	// Handle exact match
	if pattern == channel {
		return true
	}

	// Handle simple wildcard patterns
	if pattern == "*" {
		return true
	}

	// Handle prefix wildcard: *.suffix
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(channel, suffix)
	}

	// Handle suffix wildcard: prefix.*
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(channel, prefix)
	}

	// Handle middle wildcard: prefix*suffix
	if idx := strings.Index(pattern, "*"); idx >= 0 {
		prefix := pattern[:idx]
		suffix := pattern[idx+1:]
		return strings.HasPrefix(channel, prefix) && strings.HasSuffix(channel, suffix)
	}

	return false
}

// extractPlaceholder extracts a value from a channel based on a pattern with a placeholder.
// For pattern "balances.{principal}" and channel "balances.abc123", returns "abc123".
// Returns empty string if the pattern doesn't match or placeholder not found.
func extractPlaceholder(pattern, channel, placeholder string) string {
	// Find the placeholder position in the pattern
	placeholderIdx := strings.Index(pattern, placeholder)
	if placeholderIdx < 0 {
		return ""
	}

	// Get prefix and suffix around the placeholder
	prefix := pattern[:placeholderIdx]
	suffix := pattern[placeholderIdx+len(placeholder):]

	// Check if channel has the required prefix and suffix
	if !strings.HasPrefix(channel, prefix) {
		return ""
	}
	if suffix != "" && !strings.HasSuffix(channel, suffix) {
		return ""
	}

	// Extract the value between prefix and suffix
	value := channel[len(prefix):]
	if suffix != "" {
		value = value[:len(value)-len(suffix)]
	}

	// Ensure we extracted something
	if value == "" {
		return ""
	}

	return value
}
