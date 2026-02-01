package types

import "errors"

// Sentinel errors for OIDC configuration.
var (
	// ErrIssuerNotFound indicates the OIDC issuer is not registered to any tenant.
	ErrIssuerNotFound = errors.New("issuer not found")

	// ErrIssuerAlreadyExists indicates the issuer is already registered to another tenant.
	ErrIssuerAlreadyExists = errors.New("issuer already registered to another tenant")

	// ErrOIDCNotConfigured indicates OIDC is not configured for the tenant.
	ErrOIDCNotConfigured = errors.New("OIDC not configured for tenant")

	// ErrIssuerURLRequired indicates the issuer URL is required but missing.
	ErrIssuerURLRequired = errors.New("issuer URL is required")

	// ErrInvalidIssuerURL indicates the issuer URL is malformed or invalid.
	ErrInvalidIssuerURL = errors.New("invalid issuer URL")

	// ErrInvalidJWKSURL indicates the JWKS URL is malformed or invalid.
	ErrInvalidJWKSURL = errors.New("invalid JWKS URL")

	// ErrIssuerURLTooLong indicates the issuer URL exceeds the maximum length.
	ErrIssuerURLTooLong = errors.New("issuer URL too long")

	// ErrAudienceTooLong indicates the audience exceeds the maximum length.
	ErrAudienceTooLong = errors.New("audience too long")
)

// Sentinel errors for channel rules.
var (
	// ErrChannelRulesNotFound indicates channel rules are not configured for the tenant.
	ErrChannelRulesNotFound = errors.New("channel rules not found")

	// ErrInvalidChannelPattern indicates a channel pattern is malformed.
	ErrInvalidChannelPattern = errors.New("invalid channel pattern")

	// ErrEmptyGroupName indicates a group name is empty.
	ErrEmptyGroupName = errors.New("group name cannot be empty")

	// ErrGroupNameTooLong indicates a group name exceeds the maximum length.
	ErrGroupNameTooLong = errors.New("group name too long")

	// ErrTooManyGroups indicates the channel rules have too many group mappings.
	ErrTooManyGroups = errors.New("too many group mappings")

	// ErrTooManyPatterns indicates a group has too many channel patterns.
	ErrTooManyPatterns = errors.New("too many patterns for group")

	// ErrTooManyPublicPatterns indicates there are too many public channel patterns.
	ErrTooManyPublicPatterns = errors.New("too many public patterns")
)
