package types

import (
	"fmt"
	"regexp"
	"time"
)

// ChannelRules represents per-tenant channel access rules.
// Used by provisioning (storage) and gateway (authorization).
type ChannelRules struct {
	// Public contains channel patterns accessible to all authenticated users of this tenant.
	Public []string `json:"public"`

	// GroupMappings maps IdP group names to allowed channel patterns.
	// Example: {"traders": ["*.trade", "*.liquidity"], "premium": ["*.realtime"]}
	GroupMappings map[string][]string `json:"group_mappings"`

	// Default contains channel patterns allowed when no groups match.
	// Used as a fallback for users without any mapped groups.
	Default []string `json:"default,omitempty"`
}

// TenantChannelRules represents stored channel rules for a tenant.
type TenantChannelRules struct {
	// TenantID is the tenant these rules belong to.
	TenantID string `json:"tenant_id"`

	// Rules contains the channel access rules.
	Rules ChannelRules `json:"rules"`

	// CreatedAt is when the rules were created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the rules were last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// channelPatternRegex validates channel patterns.
// Allows: alphanumeric, dots, hyphens, underscores, and wildcards (*)
var channelPatternRegex = regexp.MustCompile(`^[a-zA-Z0-9.*_-]{1,256}$`)

// Validate validates channel rules. Defense in depth.
func (r *ChannelRules) Validate() error {
	// Validate public patterns count
	if len(r.Public) > MaxPublicPatterns {
		return fmt.Errorf("%w: got %d, max %d", ErrTooManyPublicPatterns, len(r.Public), MaxPublicPatterns)
	}

	// Validate public patterns
	for _, pattern := range r.Public {
		if !IsValidChannelPattern(pattern) {
			return fmt.Errorf("%w: %q", ErrInvalidChannelPattern, pattern)
		}
	}

	// Validate group mappings count
	if len(r.GroupMappings) > MaxGroupsPerMapping {
		return fmt.Errorf("%w: got %d, max %d", ErrTooManyGroups, len(r.GroupMappings), MaxGroupsPerMapping)
	}

	// Validate group mappings
	for group, patterns := range r.GroupMappings {
		if group == "" {
			return ErrEmptyGroupName
		}
		if len(group) > MaxGroupNameLength {
			return fmt.Errorf("%w: %q (max %d)", ErrGroupNameTooLong, group, MaxGroupNameLength)
		}
		if len(patterns) > MaxPatternsPerGroup {
			return fmt.Errorf("%w: group %q has %d patterns, max %d", ErrTooManyPatterns, group, len(patterns), MaxPatternsPerGroup)
		}
		for _, pattern := range patterns {
			if !IsValidChannelPattern(pattern) {
				return fmt.Errorf("%w for group %q: %q", ErrInvalidChannelPattern, group, pattern)
			}
		}
	}

	// Validate default patterns
	for _, pattern := range r.Default {
		if !IsValidChannelPattern(pattern) {
			return fmt.Errorf("%w in default: %q", ErrInvalidChannelPattern, pattern)
		}
	}

	return nil
}

// ComputeAllowedPatterns returns all channel patterns a user can access based on their groups.
func (r *ChannelRules) ComputeAllowedPatterns(groups []string) []string {
	// Pre-allocate with estimated capacity
	capacity := len(r.Public)
	for _, g := range groups {
		if patterns, ok := r.GroupMappings[g]; ok {
			capacity += len(patterns)
		}
	}
	allowed := make([]string, 0, capacity)

	// Add public channels (always allowed)
	allowed = append(allowed, r.Public...)

	// Add channels for each group
	matched := false
	for _, group := range groups {
		if patterns, ok := r.GroupMappings[group]; ok {
			allowed = append(allowed, patterns...)
			matched = true
		}
	}

	// Add default if no groups matched
	if !matched && len(r.Default) > 0 {
		allowed = append(allowed, r.Default...)
	}

	return deduplicate(allowed)
}

// deduplicate removes duplicate strings from a slice while preserving order.
func deduplicate(s []string) []string {
	if len(s) <= 1 {
		return s
	}
	seen := make(map[string]struct{}, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// IsValidChannelPattern checks if a pattern is valid.
// Exported for use in tests and other packages.
func IsValidChannelPattern(pattern string) bool {
	if pattern == "" || len(pattern) > MaxChannelPatternLength {
		return false
	}
	return channelPatternRegex.MatchString(pattern)
}

// NewChannelRules creates a new ChannelRules with initialized maps.
func NewChannelRules() *ChannelRules {
	return &ChannelRules{
		Public:        []string{},
		GroupMappings: make(map[string][]string),
		Default:       []string{},
	}
}
