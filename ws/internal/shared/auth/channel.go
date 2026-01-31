// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Toniq-Labs/odin-ws/internal/shared/protocol"
)

// ChannelMapper handles tenant-implicit channel name mapping.
// Clients use simple channel names (e.g., "BTC.trade"), and the mapper
// converts them to internal tenant-prefixed channels (e.g., "acme.BTC.trade").
//
// This follows industry standards (Pusher, Ably) where tenant isolation
// is derived from authentication, not embedded in channel names.
//
// Example:
//
//	Client subscribes: "BTC.trade"
//	Server internal:   "acme.BTC.trade"  (tenant from JWT)
//	Kafka topic:       "main.acme.trade" (env + tenant + category)
type ChannelMapper struct {
	config ChannelConfig
}

// ChannelConfig configures channel name mapping behavior.
type ChannelConfig struct {
	// Separator between channel parts (default: ".")
	Separator string `yaml:"separator" json:"separator"`

	// TenantImplicit when true, tenant is derived from JWT, not channel name.
	// Client sends "BTC.trade", server maps to "{tenant}.BTC.trade"
	TenantImplicit bool `yaml:"tenant_implicit" json:"tenant_implicit"`
}

// DefaultChannelConfig returns sensible defaults for channel mapping.
func DefaultChannelConfig() ChannelConfig {
	return ChannelConfig{
		Separator:      ".",
		TenantImplicit: true,
	}
}

// NewChannelMapper creates a channel mapper with the given configuration.
func NewChannelMapper(config ChannelConfig) *ChannelMapper {
	if config.Separator == "" {
		config.Separator = "."
	}
	return &ChannelMapper{config: config}
}

// MapToInternal converts a client channel to internal tenant-prefixed format.
// The tenant is extracted from the JWT claims.
//
// Example:
//
//	client: "BTC.trade", tenant: "acme" → "acme.BTC.trade"
//	client: "user123.balances", tenant: "acme" → "acme.user123.balances"
func (m *ChannelMapper) MapToInternal(claims *Claims, clientChannel string) string {
	if claims == nil || claims.TenantID == "" {
		// No tenant - return as-is (single-tenant mode or auth disabled)
		return clientChannel
	}

	if !m.config.TenantImplicit {
		// Tenant explicit in channel - no mapping needed
		return clientChannel
	}

	// Prefix with tenant
	return claims.TenantID + m.config.Separator + clientChannel
}

// MapToClient converts an internal tenant-prefixed channel back to client format.
// Strips the tenant prefix so clients see simple channel names.
//
// Example:
//
//	internal: "acme.BTC.trade" → client: "BTC.trade"
//	internal: "acme.user123.balances" → client: "user123.balances"
func (m *ChannelMapper) MapToClient(internalChannel string) string {
	if !m.config.TenantImplicit {
		return internalChannel
	}

	// Find first separator and strip tenant prefix
	idx := strings.Index(internalChannel, m.config.Separator)
	if idx == -1 {
		return internalChannel
	}

	return internalChannel[idx+len(m.config.Separator):]
}

// MapToClientWithTenant converts internal channel to client format, returning tenant.
// Useful when you need both the client channel and the tenant it belongs to.
//
// Example:
//
//	internal: "acme.BTC.trade" → client: "BTC.trade", tenant: "acme"
func (m *ChannelMapper) MapToClientWithTenant(internalChannel string) (clientChannel, tenant string) {
	if !m.config.TenantImplicit {
		return internalChannel, ""
	}

	idx := strings.Index(internalChannel, m.config.Separator)
	if idx == -1 {
		return internalChannel, ""
	}

	return internalChannel[idx+len(m.config.Separator):], internalChannel[:idx]
}

// ExtractTenant extracts the tenant from an internal channel name.
// Returns empty string if no tenant can be extracted.
//
// Example:
//
//	"acme.BTC.trade" → "acme"
//	"BTC.trade" → "" (no tenant prefix)
func (m *ChannelMapper) ExtractTenant(internalChannel string) string {
	if !m.config.TenantImplicit {
		return ""
	}

	idx := strings.Index(internalChannel, m.config.Separator)
	if idx == -1 {
		return ""
	}

	return internalChannel[:idx]
}

// ValidateChannelAccess checks if claims allow access to the internal channel.
// Returns true if:
// - Auth is disabled (no tenant in claims)
// - Channel belongs to the claims' tenant
// - Claims have cross-tenant role (e.g., admin)
func (m *ChannelMapper) ValidateChannelAccess(claims *Claims, internalChannel string, crossTenantRoles []string) bool {
	if claims == nil || claims.TenantID == "" {
		// No tenant enforcement - allow (auth disabled mode)
		return true
	}

	channelTenant := m.ExtractTenant(internalChannel)
	if channelTenant == "" {
		// No tenant in channel - allow (shared channel)
		return true
	}

	// Check if channel belongs to claims' tenant
	if channelTenant == claims.TenantID {
		return true
	}

	// Check for cross-tenant roles
	return slices.ContainsFunc(crossTenantRoles, claims.HasRole)
}

// ChannelParts represents the parsed components of a channel name.
type ChannelParts struct {
	Tenant   string   // Tenant ID (from internal format)
	Parts    []string // All parts after tenant
	Original string   // Original channel string
}

// ParseChannel parses an internal channel into its components.
//
// Example:
//
//	"acme.BTC.trade" → {Tenant: "acme", Parts: ["BTC", "trade"]}
//	"acme.user123.balances" → {Tenant: "acme", Parts: ["user123", "balances"]}
func (m *ChannelMapper) ParseChannel(internalChannel string) *ChannelParts {
	parts := strings.Split(internalChannel, m.config.Separator)

	result := &ChannelParts{
		Original: internalChannel,
	}

	if m.config.TenantImplicit && len(parts) > 0 {
		result.Tenant = parts[0]
		if len(parts) > 1 {
			result.Parts = parts[1:]
		}
	} else {
		result.Parts = parts
	}

	return result
}

// BuildInternalChannel constructs an internal channel from tenant and parts.
//
// Example:
//
//	tenant: "acme", parts: ["BTC", "trade"] → "acme.BTC.trade"
func (m *ChannelMapper) BuildInternalChannel(tenant string, parts ...string) string {
	if tenant == "" || !m.config.TenantImplicit {
		return strings.Join(parts, m.config.Separator)
	}

	allParts := make([]string, 0, len(parts)+1)
	allParts = append(allParts, tenant)
	allParts = append(allParts, parts...)

	return strings.Join(allParts, m.config.Separator)
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
