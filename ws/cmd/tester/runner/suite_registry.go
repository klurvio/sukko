package runner

// SuiteInfo describes a validation suite for the capabilities endpoint.
type SuiteInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SuiteRegistry maps suite names to their metadata.
// Used by the capabilities endpoint and the validate runner.
var SuiteRegistry = map[string]SuiteInfo{
	"auth":             {Name: "auth", Description: "JWT authentication validation (valid, expired, wrong kid, wrong tenant, revoked, missing)"},
	"channels":         {Name: "channels", Description: "Channel subscribe/unsubscribe operations"},
	"ordering":         {Name: "ordering", Description: "Message FIFO ordering verification"},
	"reconnect":        {Name: "reconnect", Description: "Disconnect/reconnect session recovery"},
	"ratelimit":        {Name: "ratelimit", Description: "Rate limit enforcement detection"},
	"edition-limits":   {Name: "edition-limits", Description: "Edition boundary limit testing"},
	"pubsub":           {Name: "pubsub", Description: "Pub-sub delivery with channel scoping (public, user, group)"},
	"tenant-isolation": {Name: "tenant-isolation", Description: "Cross-tenant message isolation"},
	"provisioning":     {Name: "provisioning", Description: "Provisioning API validation"},
	"sse":              {Name: "sse", Description: "SSE transport receive and auth validation"},
	"rest-publish":     {Name: "rest-publish", Description: "REST publish endpoint delivery and input validation"},
	"push":             {Name: "push", Description: "Push notification subscription and pipeline validation"},
	"license-reload":     {Name: "license-reload", Description: "License hot-reload propagation, gating, and connection survival"},
	"token-revocation":  {Name: "token-revocation", Description: "Token revocation force-disconnect and rejection validation"},
}
