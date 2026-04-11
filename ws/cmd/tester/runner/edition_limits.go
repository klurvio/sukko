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
	"github.com/rs/zerolog"
)

// editionTestTenantPrefix is the recognizable prefix for test tenants (FR-006).
const editionTestTenantPrefix = "_sukko_test_edition_"

// editionHTTPTimeout is the HTTP client timeout for /edition and provisioning API calls.
const editionHTTPTimeout = 10 * time.Second

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
			logger.Warn().Err(delErr).Str("tenant_id", sharedTenantID).Msg("Failed to clean up shared test tenant")
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
				logger.Warn().Err(err).Str("tenant_id", id).Msg("Failed to clean up test tenant")
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
	rejectID := editionTestTenantPrefix + "reject_" + uuid.New().String()[:8]
	err := provClient.CreateTenant(ctx, rejectID, "Edition rejection test")
	if err != nil {
		if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "EDITION_LIMIT") {
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
		return []metrics.CheckResult{{Name: "routing rules limit", Status: "pass", Latency: "unlimited"}}
	}

	defer func() {
		// Clean up rules on shared tenant
		if err := provClient.DeleteRoutingRules(ctx, tenantID); err != nil {
			logger.Debug().Err(err).Str("tenant_id", tenantID).
				Msg("Routing rules cleanup (not-found is expected)")
		}
	}()

	var checks []metrics.CheckResult

	// Set rules at limit — use provClient which has admin JWT auth
	statusCode, err := setTestRoutingRulesViaClient(ctx, provClient, tenantID, maxRules)
	if err != nil {
		return []metrics.CheckResult{{
			Name: "routing rules within limit", Status: "fail",
			Error: fmt.Sprintf("failed to set %d rules: %v", maxRules, err),
		}}
	}
	if statusCode == http.StatusOK {
		checks = append(checks, metrics.CheckResult{
			Name: "routing rules within limit", Status: "pass",
			Latency: fmt.Sprintf("set %d rules", maxRules),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "routing rules within limit", Status: "fail",
			Error: fmt.Sprintf("expected 200, got %d", statusCode),
		})
	}

	// Attempt limit+1 — expect rejection
	statusCode, err = setTestRoutingRulesViaClient(ctx, provClient, tenantID, maxRules+1)
	rejected := statusCode == http.StatusForbidden || (err != nil && strings.Contains(err.Error(), "403"))
	if rejected {
		checks = append(checks, metrics.CheckResult{
			Name: "routing rules limit rejection", Status: "pass",
			Latency: fmt.Sprintf("correctly rejected %d rules (max %d)", maxRules+1, maxRules),
		})
	} else {
		checks = append(checks, metrics.CheckResult{
			Name: "routing rules limit rejection", Status: "fail",
			Error: fmt.Sprintf("expected 403, got status=%d err=%v", statusCode, err),
		})
	}

	return checks
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
			Name: "connection limit rejection", Status: "skip",
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
			Name: "connection limit rejection", Status: "pass",
			Latency: fmt.Sprintf("correctly rejected at %d connections", maxConns),
		})
	} else {
		_ = extraClient.Close() // clean up unexpected connection
		checks = append(checks, metrics.CheckResult{
			Name: "connection limit rejection", Status: "fail",
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

	if edition == "community" {
		// Expect 403 EDITION_LIMIT
		if statusCode == http.StatusForbidden {
			return metrics.CheckResult{Name: "sse feature gate", Status: "pass", Latency: "community: correctly blocked (403)"}
		}
		if client != nil {
			_ = client.Close()
			return metrics.CheckResult{Name: "sse feature gate", Status: "fail", Error: "expected 403, got 200 (connection succeeded)"}
		}
		errMsg := fmt.Sprintf("expected 403, got %d", statusCode)
		if err != nil {
			errMsg = fmt.Sprintf("expected 403: %v", err)
		}
		return metrics.CheckResult{Name: "sse feature gate", Status: "fail", Error: errMsg}
	}

	// Pro/Enterprise — expect success
	if err != nil {
		return metrics.CheckResult{Name: "sse feature gate", Status: "fail", Error: fmt.Sprintf("expected 200: %v", err)}
	}
	_ = client.Close()
	return metrics.CheckResult{Name: "sse feature gate", Status: "pass", Latency: edition + ": accessible (200)"}
}

func checkRESTPublishFeatureGate(ctx context.Context, gwURL, token, edition string) metrics.CheckResult {
	client := restpublish.NewClient(httpURL(gwURL))
	body := []byte(`{"channel":"test.gate","data":{}}`)
	statusCode, _, err := client.PublishRaw(ctx, body, restpublish.AuthConfig{Token: token}, "application/json")

	if err != nil {
		return metrics.CheckResult{Name: "rest publish feature gate", Status: "fail", Error: fmt.Sprintf("request failed: %v", err)}
	}

	if edition == "community" {
		if statusCode == http.StatusForbidden {
			return metrics.CheckResult{Name: "rest publish feature gate", Status: "pass", Latency: "community: correctly blocked (403)"}
		}
		return metrics.CheckResult{Name: "rest publish feature gate", Status: "fail", Error: fmt.Sprintf("expected 403, got %d", statusCode)}
	}

	// Pro/Enterprise — expect success
	if statusCode == http.StatusOK {
		return metrics.CheckResult{Name: "rest publish feature gate", Status: "pass", Latency: edition + ": accessible (200)"}
	}
	return metrics.CheckResult{Name: "rest publish feature gate", Status: "fail", Error: fmt.Sprintf("expected 200, got %d", statusCode)}
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
			"pattern":      fmt.Sprintf("test.%d.*", i),
			"topic_suffix": fmt.Sprintf("test%d", i),
		})
	}

	status, err := provClient.SetRoutingRulesRaw(ctx, tenantID, rules)
	if err != nil {
		return status, fmt.Errorf("set test routing rules: %w", err)
	}
	return status, nil
}
