// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Toniq-Labs/odin-ws/internal/shared/protocol"
)

// extractChannelTenant extracts the tenant prefix from an internal channel name.
// Returns empty string if no separator is found.
//
// Example:
//
//	extractChannelTenant("acme.BTC.trade", ".") → "acme"
//	extractChannelTenant("channel", ".") → ""
func extractChannelTenant(channel, separator string) string {
	idx := strings.Index(channel, separator)
	if idx == -1 {
		return ""
	}
	return channel[:idx]
}

// IsSharedChannel checks if a channel pattern indicates a shared (cross-tenant) channel.
// Shared channels don't have tenant prefixes and are accessible by all tenants.
//
// Example patterns for shared channels:
//   - "system.*"
//   - "broadcast.*"
func IsSharedChannel(channel string, sharedPatterns []string) bool {
	return MatchAnyWildcard(sharedPatterns, channel)
}

// ValidateInternalChannel validates internal channel format.
// Internal channels must have at least 3 dot-separated parts: {tenant}.{identifier}.{category}
// Parts cannot be empty.
//
// Examples:
//   - "acme.BTC.trade" → valid (tenant=acme, identifier=BTC, category=trade)
//   - "acme.user123.balances" → valid
//   - "BTC.trade" → invalid (only 2 parts, not tenant-prefixed)
func ValidateInternalChannel(channel string) error {
	if channel == "" {
		return errors.New("channel cannot be empty")
	}

	parts := strings.Split(channel, ".")
	if len(parts) < protocol.MinInternalChannelParts {
		return fmt.Errorf("channel must have at least %d parts: tenant.identifier.category (got %d)",
			protocol.MinInternalChannelParts, len(parts))
	}

	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("channel part %d cannot be empty", i)
		}
	}

	return nil
}

// IsValidInternalChannel checks if a channel has valid internal format.
// This is a convenience wrapper around ValidateInternalChannel for boolean checks.
func IsValidInternalChannel(channel string) bool {
	return ValidateInternalChannel(channel) == nil
}

// ParseInternalChannel extracts tenant and category from an internal channel.
// The tenant is the first part, category is the last part.
//
// Example:
//
//	"acme.BTC.trade" → tenant: "acme", category: "trade"
//	"acme.user123.private.balances" → tenant: "acme", category: "balances"
func ParseInternalChannel(channel string) (tenant, category string, err error) {
	if err := ValidateInternalChannel(channel); err != nil {
		return "", "", err
	}

	parts := strings.Split(channel, ".")
	return parts[0], parts[len(parts)-1], nil
}
