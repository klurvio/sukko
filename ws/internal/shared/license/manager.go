package license

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Manager resolves and provides the current edition, limits, and feature gates.
//
// It is created once at startup with NewManager and provides two sets of accessors:
//
// Startup-resolved (for Config.Validate() — does not change mid-flight):
//   - Edition()
//   - Limits()
//
// Expiry-aware (for runtime gates — returns Community if license expired mid-flight):
//   - CurrentEdition()
//   - CurrentLimits()
//   - HasFeature()
//
// The expiry-aware methods check time.Now().Unix() > claims.Exp on every call.
// This is an int comparison with zero overhead, and ensures Docker Compose
// deployments detect license expiry without needing a restart.
type Manager struct {
	// Startup-resolved values (never change after construction).
	edition atomic.Value // Edition
	limits  atomic.Value // Limits

	// Claims from the license key (nil for Community with no key).
	// Stored even when expired — useful for debugging and Org() access.
	claims atomic.Value // *Claims

	logger zerolog.Logger
}

// NewManager creates a Manager from a license key string.
//
// Behavior:
//   - Empty key → Community edition with default Community limits
//   - Valid key → edition and limits from claims (with precedence overlay)
//   - Expired key → Community degradation (FR-036: never brick production)
//   - Invalid signature/format → error (fatal: corrupt or forged key)
func NewManager(licenseKey string, logger zerolog.Logger) (*Manager, error) {
	m := &Manager{
		logger: logger.With().Str("component", "license").Logger(),
	}

	// No license key → Community
	if licenseKey == "" {
		m.edition.Store(Community)
		m.limits.Store(DefaultLimits(Community))
		m.logger.Info().Str("edition", Community.String()).Msg("No license key, running as Community edition")
		return m, nil
	}

	// Parse and verify the license key
	claims, err := ParseAndVerify(licenseKey)

	// Handle expired license: degrade to Community (FR-036)
	if errors.Is(err, ErrLicenseExpired) {
		m.edition.Store(Community)
		m.limits.Store(DefaultLimits(Community))
		m.claims.Store(claims) // store expired claims for debugging/Org()

		m.logger.Warn().
			Str("edition", Community.String()).
			Str("original_edition", claims.Edition.String()).
			Str("org", claims.Org).
			Time("expired_at", time.Unix(claims.Exp, 0)).
			Str("upgrade_url", UpgradeURL).
			Msg("License expired, running as Community edition")
		return m, nil
	}

	// Handle invalid signature/format: fatal error
	if err != nil {
		return nil, fmt.Errorf("license validation: %w", err)
	}

	// Valid license — resolve edition and limits
	edition := claims.Edition.normalize()
	limits := resolveLimits(edition, claims.Limits)

	m.edition.Store(edition)
	m.limits.Store(limits)
	m.claims.Store(claims)

	m.logger.Info().
		Str("edition", edition.String()).
		Str("org", claims.Org).
		Time("expires_at", time.Unix(claims.Exp, 0)).
		Int("nodes", claims.Nodes).
		Msg("License validated")

	return m, nil
}

// Edition returns the startup-resolved edition. Used by Config.Validate().
// This value never changes after construction — even if the license expires mid-flight.
func (m *Manager) Edition() Edition {
	return m.edition.Load().(Edition)
}

// Limits returns the startup-resolved limits. Used by Config.Validate().
// This value never changes after construction.
func (m *Manager) Limits() Limits {
	return m.limits.Load().(Limits)
}

// CurrentEdition returns the expiry-aware edition.
// Returns Community if the license has expired since startup.
// Used by all runtime gates (provisioning API, LB health).
func (m *Manager) CurrentEdition() Edition {
	claims := m.loadClaims()
	if claims != nil && claims.IsExpired() {
		return Community
	}
	return m.edition.Load().(Edition)
}

// CurrentLimits returns the expiry-aware limits.
// Returns Community defaults if the license has expired since startup.
// Used by all runtime gates (provisioning API, LB health).
func (m *Manager) CurrentLimits() Limits {
	claims := m.loadClaims()
	if claims != nil && claims.IsExpired() {
		return DefaultLimits(Community)
	}
	return m.limits.Load().(Limits)
}

// HasFeature returns true if the current (expiry-aware) edition includes the feature.
func (m *Manager) HasFeature(feature Feature) bool {
	return EditionHasFeature(m.CurrentEdition(), feature)
}

// Claims returns the license claims, or nil if no license key was provided.
// Returns expired claims for degraded licenses (useful for debugging).
func (m *Manager) Claims() *Claims {
	return m.loadClaims()
}

// Org returns the licensee organization name, or empty string for Community.
func (m *Manager) Org() string {
	if c := m.loadClaims(); c != nil {
		return c.Org
	}
	return ""
}

// loadClaims safely loads claims from the atomic value.
func (m *Manager) loadClaims() *Claims {
	v := m.claims.Load()
	if v == nil {
		return nil
	}
	return v.(*Claims)
}

// resolveLimits applies the claims-override-defaults precedence rule.
// For each limit field: if the claims value is > 0, use it; otherwise use the default.
func resolveLimits(edition Edition, claimsLimits Limits) Limits {
	defaults := DefaultLimits(edition)

	if claimsLimits.MaxTenants > 0 {
		defaults.MaxTenants = claimsLimits.MaxTenants
	}
	if claimsLimits.MaxTotalConnections > 0 {
		defaults.MaxTotalConnections = claimsLimits.MaxTotalConnections
	}
	if claimsLimits.MaxShards > 0 {
		defaults.MaxShards = claimsLimits.MaxShards
	}
	if claimsLimits.MaxTopicsPerTenant > 0 {
		defaults.MaxTopicsPerTenant = claimsLimits.MaxTopicsPerTenant
	}
	if claimsLimits.MaxRoutingRulesPerTenant > 0 {
		defaults.MaxRoutingRulesPerTenant = claimsLimits.MaxRoutingRulesPerTenant
	}

	return defaults
}
