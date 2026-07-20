package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/cmd/tester/restpublish"
	testersse "github.com/klurvio/sukko/cmd/tester/sse"
	testerws "github.com/klurvio/sukko/cmd/tester/ws"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/rs/zerolog"
)

// editionTestTenantPrefix is the recognizable prefix for test tenants (FR-006). It MUST be a
// valid tenant slug prefix — slugs match ^[a-z][a-z0-9-]{2,62}$ (lowercase, start with a
// letter, hyphens only). An underscore-based prefix produces an invalid slug that provisioning
// rejects (surfacing as HTTP 500 CREATE_FAILED, since ValidateSlug's error is not mapped to 400).
const editionTestTenantPrefix = "edition-test-"

// editionHTTPTimeout is the HTTP client timeout for /edition and provisioning API calls.
const editionHTTPTimeout = 10 * time.Second

// Provisioning API error codes, mirrored here because they are unexported in
// internal/provisioning/api. The tester matches them in the response body (embedded in
// the error text by SetRoutingRulesRaw's readError) to discriminate the two DISTINCT
// routing-rules rejections: the feature gate (403 EDITION_LIMIT, from the RequireFeature
// middleware) versus the count limit (400 TOO_MANY_ROUTING_RULES, from ReplaceRoutingRules).
const (
	errCodeEditionLimit        = "EDITION_LIMIT"          // feature/resource gate (403)
	errCodeTooManyRoutingRules = "TOO_MANY_ROUTING_RULES" // routing-rule count limit (400)
)

// ConnLimitRejectionCheckName is the check name emitted by the connection-boundary
// sub-check. When the license headroom exceeds maxTestConnections the sub-check honestly
// reports a skip under this name; the e2e skip allow-list (taskfiles/e2e.yml) declares it
// as edition-limits:<this value>. Exported and asserted by a bash grep + a Go test so the
// cross-file coupling has a binding it cannot silently drift from (FR-016).
const ConnLimitRejectionCheckName = "connection limit rejection"

// editionInfo holds parsed data from provisioning and gateway /edition endpoints.
type editionInfo struct {
	Edition string
	Limits  struct {
		MaxTenants               int `json:"max_tenants"`
		MaxTotalConnections      int `json:"max_total_connections"`
		MaxShards                int `json:"max_shards"`
		MaxTopicsPerTenant       int `json:"max_topics_per_tenant"`
		MaxRoutingRulesPerTenant int `json:"max_routing_rules_per_tenant"`
	}
	Usage struct {
		Tenants     *int `json:"tenants"`
		Connections *int `json:"connections"`
		Shards      *int `json:"shards"`
	}
}

// editionResponse mirrors the JSON from GET /edition.
type editionResponse struct {
	Edition string `json:"edition"`
	Limits  struct {
		MaxTenants               int `json:"max_tenants"`
		MaxTotalConnections      int `json:"max_total_connections"`
		MaxShards                int `json:"max_shards"`
		MaxTopicsPerTenant       int `json:"max_topics_per_tenant"`
		MaxRoutingRulesPerTenant int `json:"max_routing_rules_per_tenant"`
	} `json:"limits"`
	Usage *struct {
		Tenants     *int `json:"tenants"`
		Connections *int `json:"connections"`
		Shards      *int `json:"shards"`
	} `json:"usage"`
}

// validateEditionLimits runs the edition-limits boundary test suite.
func validateEditionLimits(ctx context.Context, run *TestRun, logger zerolog.Logger) ([]metrics.CheckResult, error) {
	provURL := run.Config.ProvisioningURL
	gwURL := run.Config.GatewayURL
	provClient := run.authResult.ProvClient // authenticated via admin keypair JWT

	// Fetch edition info from provisioning (tenants, limits)
	provInfo, err := fetchEditionFrom(ctx, provURL)
	if err != nil {
		return nil, fmt.Errorf("fetch provisioning /edition: %w", err)
	}

	// Merge: start with provisioning data
	info := &editionInfo{}
	info.Edition = provInfo.Edition
	info.Limits = provInfo.Limits
	if provInfo.Usage != nil {
		info.Usage.Tenants = provInfo.Usage.Tenants
	}

	// Fetch gateway /edition for connections + shards (best-effort)
	gwInfo, err := fetchEditionFrom(ctx, httpURL(gwURL))
	if err != nil {
		logger.Debug().Err(err).Msg("Gateway /edition unreachable — connection/shard data unavailable")
	} else if gwInfo.Usage != nil {
		info.Usage.Connections = gwInfo.Usage.Connections
		info.Usage.Shards = gwInfo.Usage.Shards
	}

	// Feature gate checks (run on all editions including Enterprise)
	checks := make([]metrics.CheckResult, 0, 12) //nolint:mnd // pre-allocate for ~12 checks across 4 dimensions + 2 feature gates

	// Feature gate: SSE Transport (Pro+ only)
	sseCheck := checkSSEFeatureGate(ctx, gwURL, run.authResult.TokenFunc(0), info.Edition, logger)
	checks = append(checks, sseCheck)

	// Feature gate: REST Publish (Pro+ only, same gate as SSE)
	restCheck := checkRESTPublishFeatureGate(ctx, gwURL, run.authResult.TokenFunc(0), info.Edition)
	checks = append(checks, restCheck)

	// If all limits are 0 (Enterprise/unlimited), skip numeric boundary tests
	if isUnlimited(info) {
		checks = append(checks, metrics.CheckResult{
			Name: "edition", Status: "pass", Latency: "unlimited — no boundaries to test",
		})
		return checks, nil
	}

	// Create shared test tenant (used by routing rules check)
	sharedTenantID := editionTestTenantPrefix + uuid.New().String()[:8]
	if err := provClient.CreateTenant(ctx, sharedTenantID, "Edition boundary test tenant"); err != nil {
		return nil, fmt.Errorf("create shared test tenant: %w", err)
	}
	defer func() { //nolint:contextcheck // intentional: use background context so cleanup survives parent cancellation
		cleanupCtx, cancel := context.WithTimeout(context.Background(), editionHTTPTimeout)
		defer cancel()
		if delErr := provClient.DeleteTenant(cleanupCtx, sharedTenantID); delErr != nil {
			logger.Warn().Err(delErr).Str(logging.LogKeyTenantSlug, sharedTenantID).Msg("Failed to clean up shared test tenant")
		}
	}()

	// Tenant limit boundary
	tenantChecks := checkTenantLimit(ctx, provClient, info, logger)
	checks = append(checks, tenantChecks...)

	// Routing rules limit boundary (uses shared tenant)
	rulesChecks := checkRoutingRulesLimit(ctx, provClient, sharedTenantID, info, logger)
	checks = append(checks, rulesChecks...)

	// Connection limit boundary
	connChecks := checkConnectionLimit(ctx, gwURL, run.authResult.TokenFunc, info, logger)
	checks = append(checks, connChecks...)

	// Shard count verification (read-only)
	shardChecks := checkShardLimit(info)
	checks = append(checks, shardChecks...)

	return checks, nil
}

// --- Boundary checks ---

func checkTenantLimit(ctx context.Context, provClient *auth.ProvisioningClient, info *editionInfo, logger zerolog.Logger) []metrics.CheckResult {
	maxTenants := info.Limits.MaxTenants
	if maxTenants == 0 {
		return []metrics.CheckResult{{Name: "tenant limit", Status: "pass", Latency: "unlimited"}}
	}

	currentTenants := 0
	if info.Usage.Tenants != nil {
		currentTenants = *info.Usage.Tenants
	}

	// Account for the shared tenant already created by validateEditionLimits
	headroom := max(maxTenants-currentTenants-1, 0)

	var created []string
	defer func() { //nolint:contextcheck // intentional: use background context so cleanup survives parent cancellation
		cleanupCtx, cancel := context.WithTimeout(context.Background(), editionHTTPTimeout)
		defer cancel()
		for _, id := range created {
			if err := provClient.DeleteTenant(cleanupCtx, id); err != nil {
				logger.Warn().Err(err).Str(logging.LogKeyTenantSlug, id).Msg("Failed to clean up test tenant")
			}
		}
	}()

	var checks []metrics.CheckResult

	// Fill headroom
	if headroom > 0 {
		allCreated := true
		for range headroom {
			tenantID := editionTestTenantPrefix + uuid.New().String()[:8]
			if err := provClient.CreateTenant(ctx, tenantID, "Edition boundary test tenant"); err != nil {
				allCreated = false
				checks = append(checks, metrics.CheckResult{
					Name: "tenant creation within limit", Status: "fail",
					Error: fmt.Sprintf("failed to create tenant %d/%d: %v", len(created)+1, headroom, err),
				})
				break
			}
			created = append(created, tenantID)
		}
		if allCreated {
			checks = append(checks, metrics.CheckResult{
				Name: "tenant creation within limit", Status: "pass",
				Latency: fmt.Sprintf("created %d test tenants", headroom),
			})
		}
	}

	// Attempt one more — expect rejection
	rejectID := editionTestTenantPrefix + "reject-" + uuid.New().String()[:8]
	err := provClient.CreateTenant(ctx, rejectID, "Edition rejection test")
	if err != nil {
		if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), errCodeEditionLimit) {
			checks = append(checks, metrics.CheckResult{
				Name: "tenant limit rejection", Status: "pass",
				Latency: fmt.Sprintf("correctly rejected at %d/%d", maxTenants, maxTenants),
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name: "tenant limit rejection", Status: "fail",
				Error: fmt.Sprintf("expected 403/EDITION_LIMIT, got: %v", err),
			})
		}
	} else {
		// Unexpected success — clean up
		created = append(created, rejectID)
		checks = append(checks, metrics.CheckResult{
			Name: "tenant limit rejection", Status: "fail",
			Error: fmt.Sprintf("tenant created beyond limit (%d), expected rejection", maxTenants),
		})
	}

	return checks
}

func checkRoutingRulesLimit(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string, info *editionInfo, logger zerolog.Logger) []metrics.CheckResult {
	maxRules := info.Limits.MaxRoutingRulesPerTenant
	if maxRules == 0 {
		return []metrics.CheckResult{{Name: "routing rules limit", Status: metrics.CheckStatusPass, Latency: "unlimited"}}
	}

	defer func() {
		// Clean up rules on shared tenant (not-found is expected when the API is feature-gated).
		if err := provClient.DeleteRoutingRules(ctx, tenantID); err != nil {
			logger.Debug().Err(err).Str(logging.LogKeyTenantSlug, tenantID).
				Msg("Routing rules cleanup (not-found is expected)")
		}
	}()

	// Community: the routing-rules API is Pro-gated wholesale (ChannelTopicRouting), so every
	// request is rejected by the RequireFeature middleware (403 EDITION_LIMIT) before the count
	// validator runs. There is no reachable "within limit" success — asserting 200 here (the
	// pre-fix behavior) always failed on Community. Assert the feature gate instead.
	if info.Edition == string(license.Community) {
		status, err := setTestRoutingRulesViaClient(ctx, provClient, tenantID, 1)
		return []metrics.CheckResult{classifyRoutingRules(info.Edition, status, errText(err))}
	}

	// Editions WITH the feature: the count boundary is real — maxRules accepted (200),
	// maxRules+1 rejected by the count validator (400 TOO_MANY_ROUTING_RULES).
	var checks []metrics.CheckResult

	status, err := setTestRoutingRulesViaClient(ctx, provClient, tenantID, maxRules)
	if err == nil && status == http.StatusOK {
		checks = append(checks, metrics.CheckResult{
			Name: "routing rules within limit", Status: metrics.CheckStatusPass,
			Latency: fmt.Sprintf("set %d rules", maxRules),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "routing rules within limit", Status: metrics.CheckStatusFail,
			Error: fmt.Sprintf("expected 200 for %d rules, got status=%d err=%v", maxRules, status, err),
		})
	}

	status, err = setTestRoutingRulesViaClient(ctx, provClient, tenantID, maxRules+1)
	checks = append(checks, classifyRoutingRules(info.Edition, status, errText(err)))

	return checks
}

// classifyRoutingRules classifies a routing-rules PUT response for the given edition,
// discriminating the two DISTINCT rejections on HTTP status + error code (never message
// substrings, which are not a stable contract). Community's routing-rules API is Pro-gated,
// so the rejection it must observe is the feature gate (403 EDITION_LIMIT); editions that
// have the feature reach ReplaceRoutingRules, whose count validator rejects an over-count
// request with 400 TOO_MANY_ROUTING_RULES. The status+code split is what keeps a count
// rejection from being read as a feature gate, and a feature-gate 403 on a paid edition from
// passing as a count-limit assertion. errText carries the response body (embedded by
// SetRoutingRulesRaw's readError), which contains the `code` field.
func classifyRoutingRules(edition string, statusCode int, errBody string) metrics.CheckResult {
	if edition == string(license.Community) {
		if statusCode == http.StatusForbidden && strings.Contains(errBody, errCodeEditionLimit) {
			return metrics.CheckResult{
				Name: "routing rules feature gate", Status: metrics.CheckStatusPass,
				Latency: "community: routing-rules API correctly feature-gated (403 " + errCodeEditionLimit + ")",
			}
		}
		return metrics.CheckResult{
			Name: "routing rules feature gate", Status: metrics.CheckStatusFail,
			Error: fmt.Sprintf("community: expected 403/%s (feature gate), got status=%d err=%q", errCodeEditionLimit, statusCode, errBody),
		}
	}

	if statusCode == http.StatusBadRequest && strings.Contains(errBody, errCodeTooManyRoutingRules) {
		return metrics.CheckResult{
			Name: "routing rules limit rejection", Status: metrics.CheckStatusPass,
			Latency: fmt.Sprintf("%s: count limit correctly enforced (400 %s)", edition, errCodeTooManyRoutingRules),
		}
	}
	return metrics.CheckResult{
		Name: "routing rules limit rejection", Status: metrics.CheckStatusFail,
		Error: fmt.Sprintf("%s: expected 400/%s (count limit), got status=%d err=%q", edition, errCodeTooManyRoutingRules, statusCode, errBody),
	}
}

// errText returns the error's message, or "" for nil — feeds the routing-rules classifier
// the response body that SetRoutingRulesRaw embeds in its error.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

const maxTestConnections = 100 // cap to avoid resource exhaustion

func checkConnectionLimit(ctx context.Context, gwURL string, tokenFunc func(int) string, info *editionInfo, logger zerolog.Logger) []metrics.CheckResult {
	maxConns := info.Limits.MaxTotalConnections
	if maxConns == 0 {
		return []metrics.CheckResult{{Name: "connection limit", Status: "pass", Latency: "unlimited"}}
	}

	currentConns := 0
	if info.Usage.Connections != nil {
		currentConns = *info.Usage.Connections
	}

	headroom := max(min(maxConns-currentConns, maxTestConnections), 0)

	pool := testerws.NewPool(logger)
	defer pool.Drain()

	var checks []metrics.CheckResult

	if headroom > 0 {
		err := pool.RampUp(ctx, testerws.PoolConfig{
			GatewayURL: gwURL,
			TokenFunc:  tokenFunc,
		}, headroom, 50) // 50 connections/sec ramp rate
		if err != nil {
			checks = append(checks, metrics.CheckResult{
				Name: "connections within limit", Status: "fail",
				Error: fmt.Sprintf("ramp-up failed at headroom %d: %v", headroom, err),
			})
		} else {
			checks = append(checks, metrics.CheckResult{
				Name: "connections within limit", Status: "pass",
				Latency: fmt.Sprintf("opened %d connections", headroom),
			})
		}
	}

	// If headroom was capped, note partial test
	if maxConns-currentConns > maxTestConnections {
		checks = append(checks, metrics.CheckResult{
			Name: ConnLimitRejectionCheckName, Status: metrics.CheckStatusSkip,
			Latency: fmt.Sprintf("partial check — headroom %d too large (capped at %d)", maxConns-currentConns, maxTestConnections),
		})
		return checks
	}

	// Attempt one more connection — expect rejection
	extraClient, err := testerws.Connect(ctx, testerws.ConnectConfig{
		GatewayURL: gwURL,
		Token:      tokenFunc(headroom),
		Logger:     logger,
	})
	if err != nil {
		checks = append(checks, metrics.CheckResult{
			Name: ConnLimitRejectionCheckName, Status: metrics.CheckStatusPass,
			Latency: fmt.Sprintf("correctly rejected at %d connections", maxConns),
		})
	} else {
		_ = extraClient.Close() // clean up unexpected connection
		checks = append(checks, metrics.CheckResult{
			Name: ConnLimitRejectionCheckName, Status: metrics.CheckStatusFail,
			Error: fmt.Sprintf("connection accepted beyond limit (%d)", maxConns),
		})
	}

	return checks
}

func checkShardLimit(info *editionInfo) []metrics.CheckResult {
	maxShards := info.Limits.MaxShards
	if maxShards == 0 {
		return []metrics.CheckResult{{Name: "shard count", Status: "pass", Latency: "unlimited"}}
	}

	if info.Usage.Shards == nil {
		return []metrics.CheckResult{{
			Name: "shard count", Status: "skip",
			Latency: "shard count not available from gateway /edition",
		}}
	}

	shards := *info.Usage.Shards
	if shards <= maxShards {
		return []metrics.CheckResult{{
			Name: "shard count within limit", Status: "pass",
			Latency: fmt.Sprintf("%d/%d shards", shards, maxShards),
		}}
	}

	return []metrics.CheckResult{{
		Name: "shard count within limit", Status: "fail",
		Error: fmt.Sprintf("shard count %d exceeds limit %d", shards, maxShards),
	}}
}

func checkSSEFeatureGate(ctx context.Context, gwURL, token, edition string, logger zerolog.Logger) metrics.CheckResult {
	client, statusCode, err := testersse.Connect(ctx, testersse.ConnectConfig{
		GatewayURL: httpURL(gwURL),
		Channels:   []string{"test.gate"},
		Token:      token,
		Logger:     logger,
	})

	if edition == string(license.Community) {
		// Expect 403 EDITION_LIMIT
		if statusCode == http.StatusForbidden {
			return metrics.CheckResult{Name: "sse feature gate", Status: metrics.CheckStatusPass, Latency: "community: correctly blocked (403)"}
		}
		if client != nil {
			_ = client.Close()
			return metrics.CheckResult{Name: "sse feature gate", Status: metrics.CheckStatusFail, Error: "expected 403, got 200 (connection succeeded)"}
		}
		errMsg := fmt.Sprintf("expected 403, got %d", statusCode)
		if err != nil {
			errMsg = fmt.Sprintf("expected 403: %v", err)
		}
		return metrics.CheckResult{Name: "sse feature gate", Status: metrics.CheckStatusFail, Error: errMsg}
	}

	// Pro/Enterprise — expect success
	if err != nil {
		return metrics.CheckResult{Name: "sse feature gate", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("expected 200: %v", err)}
	}
	_ = client.Close()
	return metrics.CheckResult{Name: "sse feature gate", Status: metrics.CheckStatusPass, Latency: edition + ": accessible (200)"}
}

func checkRESTPublishFeatureGate(ctx context.Context, gwURL, token, edition string) metrics.CheckResult {
	client := restpublish.NewClient(httpURL(gwURL))
	body := []byte(`{"channel":"test.gate","data":{}}`)
	statusCode, _, err := client.PublishRaw(ctx, body, restpublish.AuthConfig{Token: token}, "application/json")

	if err != nil {
		return metrics.CheckResult{Name: "rest publish feature gate", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("request failed: %v", err)}
	}

	if edition == string(license.Community) {
		if statusCode == http.StatusForbidden {
			return metrics.CheckResult{Name: "rest publish feature gate", Status: metrics.CheckStatusPass, Latency: "community: correctly blocked (403)"}
		}
		return metrics.CheckResult{Name: "rest publish feature gate", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("expected 403, got %d", statusCode)}
	}

	// Pro/Enterprise — expect success
	if statusCode == http.StatusOK {
		return metrics.CheckResult{Name: "rest publish feature gate", Status: metrics.CheckStatusPass, Latency: edition + ": accessible (200)"}
	}
	return metrics.CheckResult{Name: "rest publish feature gate", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("expected 200, got %d", statusCode)}
}

func isUnlimited(info *editionInfo) bool {
	l := info.Limits
	return l.MaxTenants == 0 && l.MaxTotalConnections == 0 && l.MaxShards == 0 &&
		l.MaxTopicsPerTenant == 0 && l.MaxRoutingRulesPerTenant == 0
}

// --- HTTP helpers ---

func fetchEditionFrom(ctx context.Context, baseURL string) (*editionResponse, error) {
	client := &http.Client{Timeout: editionHTTPTimeout}
	url := baseURL + "/edition"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result editionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// setTestRoutingRulesViaClient sets routing rules using the ProvisioningClient for auth,
// returning the HTTP status code for boundary limit testing (needs 200 vs 403 distinction).
func setTestRoutingRulesViaClient(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string, count int) (int, error) {
	rules := make([]map[string]any, 0, count)
	for i := range count {
		rules = append(rules, map[string]any{
			"pattern":  fmt.Sprintf("test.%d.**", i),
			"topics":   []string{fmt.Sprintf("test%d", i)},
			"priority": i + 1,
		})
	}

	status, err := provClient.SetRoutingRulesRaw(ctx, tenantID, rules)
	if err != nil {
		return status, fmt.Errorf("set test routing rules: %w", err)
	}
	return status, nil
}
