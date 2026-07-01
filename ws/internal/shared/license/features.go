package license

// Feature represents a gated capability in the Sukko platform.
type Feature string

// FeatureStatus indicates the implementation state of a feature.
type FeatureStatus string

const (
	// StatusImplemented — feature is functional with EditionHasFeature gate check wired.
	StatusImplemented FeatureStatus = "implemented"

	// StatusFuture — feature is not yet implemented, gate reserved for future use.
	StatusFuture FeatureStatus = "future"
)

// FeaturePriority indicates implementation priority for unimplemented features.
// Lower number = higher priority. Implemented features use PriorityNone.
type FeaturePriority int

// FeaturePriority constants for implementation ordering.
const (
	PriorityNone   FeaturePriority = 0 // Implemented — no priority needed
	PriorityHigh   FeaturePriority = 2 // Next to implement
	PriorityMedium FeaturePriority = 3 // Planned
	PriorityLow    FeaturePriority = 4 // Future / nice-to-have
)

// FeatureInfo holds metadata about a gated feature.
type FeatureInfo struct {
	// Description is a human-readable summary of what the feature does.
	Description string

	// Status indicates whether the feature is implemented or future.
	Status FeatureStatus

	// Priority indicates implementation priority (0 = implemented, 2 = high, 4 = low).
	Priority FeaturePriority
}

// Feature Matrix — Single Source of Truth
//
// Every gated feature is listed here with its edition requirement and metadata.
// The docs site (sukko-docs) auto-generates the editions comparison page from
// this file via the extract-editions script. Update metadata when implementing.
const (
	// ── Pro Features ─────────────────────────────────────────────────────

	KafkaBackend              Feature = "MESSAGE_BACKEND=kafka"            // Implemented
	PerTenantChannelRules     Feature = "GATEWAY_PER_TENANT_CHANNEL_RULES" // Implemented
	PerTenantConnectionLimits Feature = "TENANT_CONNECTION_LIMIT_ENABLED"  // Implemented
	Alerting                  Feature = "ALERT_ENABLED"                    // Implemented
	ChannelTopicRouting       Feature = "CHANNEL_TOPIC_ROUTING"            // Implemented

	PerTenantConfigurableQuotas Feature = "per-tenant configurable quotas" // Implemented
	TenantLifecycleManager      Feature = "tenant lifecycle manager"       // Implemented
	ConnectionTracing           Feature = "connection tracing"             // Implemented

	SSETransport       Feature = "SSE transport"              // Implemented
	Analytics          Feature = "real-time analytics"        // Implemented
	AdminUI            Feature = "admin UI"                   // Implemented
	TokenRevocation    Feature = "token revocation"           // Implemented
	Webhooks           Feature = "webhook delivery"           // Implemented
	MessageHistory     Feature = "message history"            // Implemented
	LiveGapRecovery    Feature = "live gap recovery"          // Implemented
	ConnectionsAPI     Feature = "connections management API" // Implemented
	ChannelPatternsCEL Feature = "channel patterns (CEL)"     // Future
	DeltaCompression   Feature = "delta compression"          // Future

	// ── Enterprise Features ──────────────────────────────────────────────

	// Note: the AuditLogging gate covers only the audit-log query API (GET /audit-log). Audit record writes are unconditional — not gated.
	AuditLogging        Feature = "audit logging"                // Implemented
	PushNotifications   Feature = "push notifications"           // Implemented
	AnalyticsPush       Feature = "real-time analytics for push" // Implemented
	IPAllowlisting      Feature = "per-tenant IP allowlisting"   // Future
	PriorityRouting     Feature = "priority message routing"     // Future
	CustomQuotaPolicies Feature = "custom quota policies"        // Future
)

// featureEditions maps each feature to its minimum required edition.
// Features not in this map default to Community (always available).
var featureEditions = map[Feature]Edition{
	// Pro
	KafkaBackend:                Pro,
	PerTenantChannelRules:       Pro,
	PerTenantConnectionLimits:   Pro,
	Alerting:                    Pro,
	ChannelTopicRouting:         Pro,
	PerTenantConfigurableQuotas: Pro,
	TenantLifecycleManager:      Pro,
	ConnectionTracing:           Pro,
	SSETransport:                Pro,
	Analytics:                   Pro,
	AdminUI:                     Pro,
	TokenRevocation:             Pro,
	Webhooks:                    Pro,
	MessageHistory:              Pro,
	LiveGapRecovery:             Pro,
	ConnectionsAPI:              Pro,
	ChannelPatternsCEL:          Pro,
	DeltaCompression:            Pro,

	// Enterprise
	AuditLogging:        Enterprise,
	PushNotifications:   Enterprise,
	AnalyticsPush:       Enterprise,
	IPAllowlisting:      Enterprise,
	PriorityRouting:     Enterprise,
	CustomQuotaPolicies: Enterprise,
}

// featureMetadata holds implementation status and priority for each feature.
// This is the authoritative metadata — the docs site reads this via extraction.
var featureMetadata = map[Feature]FeatureInfo{
	// ── Implemented — Pro ────────────────────────────────────────────────

	KafkaBackend:                {Description: "Kafka/Redpanda message backend", Status: StatusImplemented, Priority: PriorityNone},
	PerTenantChannelRules:       {Description: "Per-tenant channel subscribe/publish rules", Status: StatusImplemented, Priority: PriorityNone},
	PerTenantConnectionLimits:   {Description: "Per-tenant WebSocket connection limits", Status: StatusImplemented, Priority: PriorityNone},
	Alerting:                    {Description: "AlertManager integration for Prometheus alerts", Status: StatusImplemented, Priority: PriorityNone},
	PerTenantConfigurableQuotas: {Description: "Per-tenant configurable resource quotas (topics, connections, rules)", Status: StatusImplemented, Priority: PriorityNone},
	TenantLifecycleManager:      {Description: "Tenant suspend/reactivate lifecycle management", Status: StatusImplemented, Priority: PriorityNone},
	ConnectionTracing:           {Description: "OpenTelemetry distributed tracing for connections", Status: StatusImplemented, Priority: PriorityNone},
	SSETransport:                {Description: "SSE transport + REST publish", Status: StatusImplemented, Priority: PriorityNone},
	ChannelTopicRouting:         {Description: "Per-tenant routing rules for channel-to-topic mapping, multi-topic fan-out, and header-based consumer routing", Status: StatusImplemented, Priority: PriorityNone},
	TokenRevocation:             {Description: "Revoke individual JWT tokens (not just keys)", Status: StatusImplemented, Priority: PriorityNone},
	MessageHistory:              {Description: "Queryable message history per channel", Status: StatusImplemented, Priority: PriorityNone},
	LiveGapRecovery:             {Description: "In-band gap detection and live replay on existing connections without reconnect", Status: StatusImplemented, Priority: PriorityNone},
	ConnectionsAPI:              {Description: "Inspect and force-disconnect live connections per tenant", Status: StatusImplemented, Priority: PriorityNone},
	AdminUI:                     {Description: "Web-based tenant management interface", Status: StatusImplemented, Priority: PriorityNone},
	Analytics:                   {Description: "Real-time per-tenant usage analytics (connections, messages)", Status: StatusImplemented, Priority: PriorityNone},
	Webhooks:                    {Description: "HTTP webhook delivery as alternative to WebSocket", Status: StatusImplemented, Priority: PriorityNone},

	// ── Implemented — Enterprise ──────────────────────────────────────────

	// Note: AuditLogging covers only the audit-log query API (GET /audit-log); writes are unconditional.
	AuditLogging:      {Description: "Audit trail of all provisioning API actions", Status: StatusImplemented, Priority: PriorityNone},
	PushNotifications: {Description: "Push notifications (Web Push + FCM + APNs)", Status: StatusImplemented, Priority: PriorityNone},
	AnalyticsPush:     {Description: "Real-time push delivery analytics (platform breakdown, failure reasons)", Status: StatusImplemented, Priority: PriorityNone},

	// ── Future — Pro ─────────────────────────────────────────────────────

	ChannelPatternsCEL: {Description: "CEL expressions for complex channel authorization", Status: StatusFuture, Priority: PriorityLow},
	DeltaCompression:   {Description: "Send only changed fields in high-frequency updates", Status: StatusFuture, Priority: PriorityLow},

	// ── Future — Enterprise ──────────────────────────────────────────────

	IPAllowlisting:      {Description: "Per-tenant IP allowlisting for connection filtering", Status: StatusFuture, Priority: PriorityLow},
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
