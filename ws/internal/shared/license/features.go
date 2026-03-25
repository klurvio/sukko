package license

// Feature represents a gated capability in the Sukko platform.
type Feature string

// Feature constants — each maps to a minimum required edition via featureEditions.
const (
	// Pro features
	KafkaBackend              Feature = "MESSAGE_BACKEND=kafka"
	NATSJetStreamBackend      Feature = "MESSAGE_BACKEND=nats"
	PostgresDatabase          Feature = "DATABASE_DRIVER=postgres"
	SSETransport              Feature = "SSE transport"
	PerTenantChannelRules     Feature = "GATEWAY_PER_TENANT_CHANNEL_RULES"
	PerTenantConnectionLimits Feature = "TENANT_CONNECTION_LIMIT_ENABLED"
	PerTenantConfigurableQuotas Feature = "per-tenant configurable quotas"
	TenantLifecycleManager    Feature = "tenant lifecycle manager"
	Alerting                  Feature = "ALERT_ENABLED"
	Analytics                 Feature = "real-time analytics"
	ConnectionTracing         Feature = "connection tracing"
	AdminUI                   Feature = "admin UI"
	TokenRevocation           Feature = "token revocation"
	Webhooks                  Feature = "webhook delivery"
	MessageHistory            Feature = "message history"
	ChannelPatternsCEL        Feature = "channel patterns (CEL)"
	DeltaCompression          Feature = "delta compression"

	// Enterprise features
	WebPushTransport     Feature = "Web Push transport"
	AdminUISSO           Feature = "admin UI SSO/OIDC"
	IPAllowlisting       Feature = "per-tenant IP allowlisting"
	AuditLogging         Feature = "audit logging"
	E2EEncryption        Feature = "end-to-end encryption"
	PriorityRouting      Feature = "priority message routing"
	CustomQuotaPolicies  Feature = "custom quota policies"
)

// featureEditions maps each feature to its minimum required edition.
// Features not in this map default to Community (always available).
var featureEditions = map[Feature]Edition{
	// Pro
	KafkaBackend:              Pro,
	NATSJetStreamBackend:      Pro,
	PostgresDatabase:          Pro,
	SSETransport:              Pro,
	PerTenantChannelRules:     Pro,
	PerTenantConnectionLimits: Pro,
	PerTenantConfigurableQuotas: Pro,
	TenantLifecycleManager:    Pro,
	Alerting:                  Pro,
	Analytics:                 Pro,
	ConnectionTracing:         Pro,
	AdminUI:                   Pro,
	TokenRevocation:           Pro,
	Webhooks:                  Pro,
	MessageHistory:            Pro,
	ChannelPatternsCEL:        Pro,
	DeltaCompression:          Pro,

	// Enterprise
	WebPushTransport:    Enterprise,
	AdminUISSO:          Enterprise,
	IPAllowlisting:      Enterprise,
	AuditLogging:        Enterprise,
	E2EEncryption:       Enterprise,
	PriorityRouting:     Enterprise,
	CustomQuotaPolicies: Enterprise,
}

// RequiredEdition returns the minimum edition needed for the given feature.
// Returns Community if the feature is not gated (always available).
func RequiredEdition(feature Feature) Edition {
	if edition, ok := featureEditions[feature]; ok {
		return edition
	}
	return Community
}

// EditionHasFeature returns true if the given edition includes the feature.
// This is a pure function with no Manager dependency — suitable for use in
// Config.Validate() with a startup-resolved edition value.
func EditionHasFeature(edition Edition, feature Feature) bool {
	return edition.IsAtLeast(RequiredEdition(feature))
}
