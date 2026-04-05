package license

// Feature represents a gated capability in the Sukko platform.
type Feature string

// FeatureStatus indicates the implementation state of a feature.
type FeatureStatus string

const (
	// StatusImplemented — feature is functional with EditionHasFeature gate check wired.
	StatusImplemented FeatureStatus = "implemented"

	// StatusUngated — feature is functional but missing EditionHasFeature gate check (bug).
	StatusUngated FeatureStatus = "ungated"

	// StatusFuture — feature is not yet implemented, gate reserved for future use.
	StatusFuture FeatureStatus = "future"
)

// FeaturePriority indicates implementation priority for unimplemented features.
// Lower number = higher priority. Implemented features use PriorityNone.
type FeaturePriority int

// FeaturePriority constants for implementation ordering.
const (
	PriorityNone     FeaturePriority = 0 // Implemented — no priority needed
	PriorityCritical FeaturePriority = 1 // Must fix (ungated features)
	PriorityHigh     FeaturePriority = 2 // Next to implement
	PriorityMedium   FeaturePriority = 3 // Planned
	PriorityLow      FeaturePriority = 4 // Future / nice-to-have
)

// FeatureInfo holds metadata about a gated feature.
type FeatureInfo struct {
	// Description is a human-readable summary of what the feature does.
	Description string

	// Status indicates whether the feature is implemented, ungated, or future.
	Status FeatureStatus

	// Priority indicates implementation priority (0 = implemented, 1 = critical, 4 = low).
	Priority FeaturePriority
}

// Feature Matrix — Single Source of Truth
//
// Every gated feature is listed here with its edition requirement and metadata.
// The docs site (sukko-docs) auto-generates the editions comparison page from
// this file via the extract-editions script. Update metadata when implementing.
const (
	// ── Pro Features ─────────────────────────────────────────────────────

	KafkaBackend              Feature = "MESSAGE_BACKEND=kafka"
	NATSJetStreamBackend      Feature = "MESSAGE_BACKEND=nats"
	PostgresDatabase          Feature = "DATABASE_DRIVER=postgres"
	PerTenantChannelRules     Feature = "GATEWAY_PER_TENANT_CHANNEL_RULES"
	PerTenantConnectionLimits Feature = "TENANT_CONNECTION_LIMIT_ENABLED"
	Alerting                  Feature = "ALERT_ENABLED"

	PerTenantConfigurableQuotas Feature = "per-tenant configurable quotas"
	TenantLifecycleManager      Feature = "tenant lifecycle manager"
	ConnectionTracing           Feature = "connection tracing"

	SSETransport       Feature = "SSE transport"
	Analytics          Feature = "real-time analytics"
	AdminUI            Feature = "admin UI"
	TokenRevocation    Feature = "token revocation"
	Webhooks           Feature = "webhook delivery"
	MessageHistory     Feature = "message history"
	ChannelPatternsCEL Feature = "channel patterns (CEL)"
	DeltaCompression   Feature = "delta compression"

	// ── Enterprise Features ──────────────────────────────────────────────

	AuditLogging        Feature = "audit logging"
	WebPushTransport    Feature = "Web Push transport" // Implemented
	IPAllowlisting      Feature = "per-tenant IP allowlisting"
	E2EEncryption       Feature = "end-to-end encryption"
	PriorityRouting     Feature = "priority message routing"
	CustomQuotaPolicies Feature = "custom quota policies"
)

// featureEditions maps each feature to its minimum required edition.
// Features not in this map default to Community (always available).
var featureEditions = map[Feature]Edition{
	// Pro
	KafkaBackend:                Pro,
	NATSJetStreamBackend:        Pro,
	PostgresDatabase:            Pro,
	PerTenantChannelRules:       Pro,
	PerTenantConnectionLimits:   Pro,
	Alerting:                    Pro,
	PerTenantConfigurableQuotas: Pro,
	TenantLifecycleManager:      Pro,
	ConnectionTracing:           Pro,
	SSETransport:                Pro,
	Analytics:                   Pro,
	AdminUI:                     Pro,
	TokenRevocation:             Pro,
	Webhooks:                    Pro,
	MessageHistory:              Pro,
	ChannelPatternsCEL:          Pro,
	DeltaCompression:            Pro,

	// Enterprise
	AuditLogging:        Enterprise,
	WebPushTransport:    Enterprise,
	IPAllowlisting:      Enterprise,
	E2EEncryption:       Enterprise,
	PriorityRouting:     Enterprise,
	CustomQuotaPolicies: Enterprise,
}

// featureMetadata holds implementation status and priority for each feature.
// This is the authoritative metadata — the docs site reads this via extraction.
var featureMetadata = map[Feature]FeatureInfo{
	// ── Implemented (gated) ──────────────────────────────────────────────

	KafkaBackend:              {Description: "Kafka/Redpanda message backend", Status: StatusImplemented, Priority: PriorityNone},
	NATSJetStreamBackend:      {Description: "NATS JetStream message backend", Status: StatusImplemented, Priority: PriorityNone},
	PostgresDatabase:          {Description: "PostgreSQL for provisioning (instead of SQLite)", Status: StatusImplemented, Priority: PriorityNone},
	PerTenantChannelRules:     {Description: "Per-tenant channel subscribe/publish rules", Status: StatusImplemented, Priority: PriorityNone},
	PerTenantConnectionLimits: {Description: "Per-tenant WebSocket connection limits", Status: StatusImplemented, Priority: PriorityNone},
	Alerting:                  {Description: "AlertManager integration for Prometheus alerts", Status: StatusImplemented, Priority: PriorityNone},
	WebPushTransport:          {Description: "Push notifications (Web Push + FCM + APNs)", Status: StatusImplemented, Priority: PriorityNone},

	// ── Implemented (ungated) — need EditionHasFeature checks ────────────

	PerTenantConfigurableQuotas: {Description: "Per-tenant configurable resource quotas (topics, connections, rules)", Status: StatusImplemented, Priority: PriorityNone},
	TenantLifecycleManager:      {Description: "Tenant suspend/reactivate lifecycle management", Status: StatusImplemented, Priority: PriorityNone},
	ConnectionTracing:           {Description: "OpenTelemetry distributed tracing for connections", Status: StatusImplemented, Priority: PriorityNone},
	AuditLogging:                {Description: "Audit trail of all provisioning API actions", Status: StatusImplemented, Priority: PriorityNone},

	// ── Future — Pro ─────────────────────────────────────────────────────

	TokenRevocation:    {Description: "Revoke individual JWT tokens (not just keys)", Status: StatusFuture, Priority: PriorityHigh},
	MessageHistory:     {Description: "Queryable message history per channel", Status: StatusFuture, Priority: PriorityHigh},
	AdminUI:            {Description: "Web-based tenant management interface", Status: StatusFuture, Priority: PriorityMedium},
	Analytics:          {Description: "Real-time usage dashboards and metrics", Status: StatusFuture, Priority: PriorityMedium},
	Webhooks:           {Description: "HTTP webhook delivery as alternative to WebSocket", Status: StatusFuture, Priority: PriorityMedium},
	ChannelPatternsCEL: {Description: "CEL expressions for complex channel authorization", Status: StatusFuture, Priority: PriorityLow},
	SSETransport:       {Description: "SSE transport + REST publish", Status: StatusImplemented, Priority: PriorityNone},
	DeltaCompression:   {Description: "Send only changed fields in high-frequency updates", Status: StatusFuture, Priority: PriorityLow},

	// ── Future — Enterprise ──────────────────────────────────────────────

	IPAllowlisting:      {Description: "Per-tenant IP allowlisting for connection filtering", Status: StatusFuture, Priority: PriorityLow},
	E2EEncryption:       {Description: "End-to-end encrypted message delivery", Status: StatusFuture, Priority: PriorityLow},
	PriorityRouting:     {Description: "Priority-based message delivery under load", Status: StatusFuture, Priority: PriorityLow},
	CustomQuotaPolicies: {Description: "Tenant-specific quota rules beyond simple limits", Status: StatusFuture, Priority: PriorityLow},
}

// GetFeatureInfo returns metadata for a feature. Returns zero value if not found.
func GetFeatureInfo(feature Feature) FeatureInfo {
	return featureMetadata[feature]
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
