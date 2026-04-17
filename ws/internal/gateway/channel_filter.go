package gateway

import (
	"context"
	"slices"
	"strings"

	"github.com/klurvio/sukko/internal/shared/auth"
	"github.com/klurvio/sukko/internal/shared/protocol"
)

// filterSubscribeChannels validates and filters channels for subscribe-like operations.
// Used by SSE and push subscribe. Replicates the WebSocket subscribe permission flow.
//
// Steps:
//  1. Filter out channels failing tenant prefix validation
//  2. Filter out channels with fewer than MinInternalChannelParts
//  3. Strip tenant prefix from surviving channels
//  4. Apply permission filtering (tenantPermChecker → global permissions → pass all)
//  5. Re-prefix allowed channels with tenantID
//
// When auth is disabled (both checkers nil), all valid channels pass through.
func (gw *Gateway) filterSubscribeChannels(
	ctx context.Context,
	channels []string,
	tenantID string,
	claims *auth.Claims,
) []string {
	// Auth disabled — no checkers, skip all validation (FR-011)
	if gw.permissions == nil && gw.tenantPermChecker == nil {
		return channels
	}

	// 1 + 2. Tenant prefix and channel format validation
	valid := make([]string, 0, len(channels))
	for _, ch := range channels {
		switch {
		case !auth.ValidateChannelTenant(ch, tenantID):
			RecordChannelCheck(ChannelCheckDenied)
			gw.logger.Warn().
				Str("channel", ch).
				Str("tenant_id", tenantID).
				Str("reason", "wrong_tenant_prefix").
				Msg("channel denied")
		case strings.Count(ch, ".")+1 < protocol.MinInternalChannelParts:
			RecordChannelCheck(ChannelCheckDenied)
			gw.logger.Warn().
				Str("channel", ch).
				Str("tenant_id", tenantID).
				Str("reason", "invalid_format").
				Msg("channel denied")
		default:
			valid = append(valid, ch)
		}
	}

	// 3. Strip tenant prefix for pattern matching
	tenantPrefix := tenantID + "."
	stripped := make([]string, len(valid))
	for i, ch := range valid {
		stripped[i] = strings.TrimPrefix(ch, tenantPrefix)
	}

	// 4. Permission filtering (per-tenant → global)
	var allowedStripped []string
	switch {
	case gw.tenantPermChecker != nil:
		allowedStripped = gw.tenantPermChecker.FilterChannels(ctx, claims, stripped)
	case gw.permissions != nil:
		allowedStripped = gw.permissions.FilterChannels(claims, stripped)
	default:
		// Auth disabled — no permission checker, all valid channels pass through
		allowedStripped = stripped
	}

	// 5. Re-prefix allowed channels and record metrics
	allowed := make([]string, 0, len(allowedStripped))
	for _, s := range allowedStripped {
		allowed = append(allowed, tenantPrefix+s)
	}

	// Record metrics for permission-denied channels
	for _, ch := range valid {
		if !slices.Contains(allowed, ch) {
			RecordChannelCheck(ChannelCheckDenied)
			gw.logger.Warn().
				Str("channel", ch).
				Str("tenant_id", tenantID).
				Str("reason", "permission_denied").
				Msg("channel denied")
		} else {
			RecordChannelCheck(ChannelCheckAllowed)
		}
	}

	return allowed
}
