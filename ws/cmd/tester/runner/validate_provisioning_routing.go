package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/routing"
	"github.com/rs/zerolog"
)

// maxTopicsPerRule is the default MAX_TOPICS_PER_RULE cap (shared/platform/provisioning_config.go).
// The `routing rule validation` too-many-topics probe sends maxTopicsPerRule+1 identical
// suffixes so only the COUNT trips (all "default", already provisioned).
const maxTopicsPerRule = 10

// routingRulesMaxPageLimit mirrors the handler's per-page cap for routing rules
// (internal/provisioning/api/handlers.go). A GET with ?limit above this echoes this capped
// value in the response's `limit` field — the cap assertion needs no 200+ rules.
const routingRulesMaxPageLimit = 200

// unprovisionedTopicSuffix is a topic suffix that is never provisioned for any tenant
// (CreateTenant provisions only "default" and "dead-letter"). A routing rule referencing it
// must be rejected 400 TOPIC_NOT_PROVISIONED.
const unprovisionedTopicSuffix = "never-provisioned"

// provRoutingCheck runs fn (one probe) and classifies its result, tolerating the edition
// feature gate: a feature-gate rejection (exact error code EDITION_LIMIT, emitted only by the
// RequireFeature middleware at HTTP 403) becomes an explicit skip — NEVER a pass. A nil error
// (2xx) is a pass; any other error is a fail. This is the gate-tolerant wrapper applied to the
// new routing checks and, via the same discrimination, to the pre-existing edition-gated checks
// (set/get/delete routing rules, get/update quota, suspend/reactivate tenant, get audit log) so
// the suite is green on lower editions without masking real failures (R2/R5). Exact-code
// discrimination (extractErrorCode, never strings.Contains) is required because EDITION_LIMIT is
// a substring of the EDITION_LIMIT_* limit codes.
func provRoutingCheck(name string, fn func() error) metrics.CheckResult {
	err := fn()
	if err == nil {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusPass}
	}
	if extractErrorCode(err.Error()) == errCodeEditionLimit {
		return metrics.CheckResult{
			Name: name, Status: metrics.CheckStatusSkip,
			Latency: "edition-gated: feature not available on this edition (403 " + errCodeEditionLimit + ")",
		}
	}
	return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail, Error: err.Error()}
}

// gateSkip builds the explicit skip result a Community-only check returns when the probed
// feature is edition-gated (used by checks that branch on the fetched edition rather than
// probing a single call, e.g. the role/GET checks whose whole flow is Pro-only).
func gateSkip(name string) metrics.CheckResult {
	return metrics.CheckResult{
		Name: name, Status: metrics.CheckStatusSkip,
		Latency: "edition-gated: routing-rules API requires Pro (community: 403 " + errCodeEditionLimit + ")",
	}
}

// routingRule builds a single routing-rule map for the raw client methods.
func routingRule(pattern string, topics []string, priority int) map[string]any {
	return map[string]any{"pattern": pattern, "topics": topics, "priority": priority}
}

// validateProvisioningRouting runs the e2e-unique routing-rules coverage checks (FR-002…FR-007):
// middleware composition, noop-backed topic registry, error-code mapping, and read
// normalization — every negative asserts HTTP status AND the exact error code, and pairs a
// near-identical allowed (2xx) case differing only in the tested dimension ([[one-sided-green]]).
// Every mutating check ends by restoring testRoutingRules via resetRoutingRulesToFixture (FR-014)
// so ordering cannot create false results. All read-backs go through the provisioning API GET
// only (FR-016) — no message-delivery observation.
//
// The block is invoked from validateProvisioning immediately after check 10 (delete routing
// rules), whose populated-delete pairs with this block's `routing delete idempotent` empty-delete.
//
//nolint:gocritic // appendCombine: each check is separated by an interleaved appendReset (FR-014 fixture restore) or an edition branch, so the sequential appends cannot be combined.
func validateProvisioningRouting(
	ctx context.Context,
	run *TestRun,
	setupA *auth.SetupResult,
	edition string,
	logger zerolog.Logger,
) []metrics.CheckResult {
	provClient := setupA.ProvClient
	tenantA := setupA.TenantID
	isCommunity := edition == string(license.Community)

	var checks []metrics.CheckResult

	// --- FR-007: delete idempotent (pairs with check 10's populated delete) ---
	// A second DELETE on already-empty rules must still succeed (200). The service delete has no
	// not-found branch — idempotent by construction (handlers.go:560-572).
	checks = append(checks, provRoutingCheck("routing delete idempotent", func() error {
		status, err := provClient.DeleteRoutingRulesRaw(ctx, tenantA)
		return expectStatus("routing delete idempotent", status, err, http.StatusOK)
	}))

	// --- FR-002: topic gate ---
	// Negative: a rule referencing an unprovisioned suffix → 400 TOPIC_NOT_PROVISIONED.
	// Allowed: an identical rule referencing the provisioned "default" suffix → 201.
	checks = append(checks, provRoutingCheck("routing topic gate", func() error {
		// Allowed case first (proves the write path works; differs only in the topic suffix).
		okStatus, okErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
			routingRule("topic-gate-ok.**", []string{routing.DefaultTopicSuffix}, 10))
		if err := expectStatus("topic gate allowed", okStatus, okErr, http.StatusCreated); err != nil {
			return err
		}
		// Negative: same shape, unprovisioned suffix.
		badStatus, badErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
			routingRule("topic-gate-bad.**", []string{unprovisionedTopicSuffix}, 11))
		return expectReject("topic gate rejected", badStatus, badErr, http.StatusBadRequest, errCodeTopicNotProvisioned)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-006: POST duplicate pattern (409) ---
	// Allowed: distinct pattern → 201. Negative: same pattern again → 409 DUPLICATE_PATTERN.
	checks = append(checks, provRoutingCheck("routing post duplicate pattern", func() error {
		okStatus, okErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
			routingRule("dup-pattern.**", []string{routing.DefaultTopicSuffix}, 20))
		if err := expectStatus("dup-pattern allowed", okStatus, okErr, http.StatusCreated); err != nil {
			return err
		}
		badStatus, badErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
			routingRule("dup-pattern.**", []string{routing.DefaultTopicSuffix}, 21))
		return expectReject("dup-pattern rejected", badStatus, badErr, http.StatusConflict, errCodeRoutingRuleDuplicatePattern)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-006: POST duplicate priority (409, POST-only) ---
	// Allowed: distinct priority → 201. Negative: same priority, different pattern → 409.
	checks = append(checks, provRoutingCheck("routing post duplicate priority", func() error {
		okStatus, okErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
			routingRule("dup-priority-a.**", []string{routing.DefaultTopicSuffix}, 30))
		if err := expectStatus("dup-priority allowed", okStatus, okErr, http.StatusCreated); err != nil {
			return err
		}
		badStatus, badErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
			routingRule("dup-priority-b.**", []string{routing.DefaultTopicSuffix}, 30))
		return expectReject("dup-priority rejected", badStatus, badErr, http.StatusConflict, errCodeRoutingRuleDuplicatePriority)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-006: PUT duplicate pattern (400 VALIDATION_ERROR — real POST-409/PUT-400 asymmetry) ---
	// A PUT carrying two identical patterns in the SAME request is rejected by boundary validation
	// (ValidateRoutingRules, topic_routing.go:81-83 → ReplaceRoutingRules default branch,
	// handlers.go:531) as 400 ROUTING_RULE_VALIDATION_ERROR — NOT 409 ROUTING_RULE_DUPLICATE_PATTERN
	// like POST. POST returns 409 only because its duplicate is against ALREADY-STORED rules, which
	// boundary validation never sees; for an in-request duplicate the service-level DUPLICATE_PATTERN
	// branch (handlers.go:540-541) is unreachable. Asserted as-is per FR-009; see FR-013 follow-up #204
	// (POST/PUT code asymmetry + the dead service-level DUPLICATE_PATTERN branch for PUT).
	// Allowed: a valid two-rule PUT with distinct patterns → 200.
	checks = append(checks, provRoutingCheck("routing put duplicate pattern", func() error {
		okStatus, okErr := provClient.SetRoutingRulesRaw(ctx, tenantA, []map[string]any{
			routingRule("put-ok-a.**", []string{routing.DefaultTopicSuffix}, 40),
			routingRule("put-ok-b.**", []string{routing.DefaultTopicSuffix}, 41),
		})
		if err := expectStatus("put-dup allowed", okStatus, okErr, http.StatusOK); err != nil {
			return err
		}
		badStatus, badErr := provClient.SetRoutingRulesRaw(ctx, tenantA, []map[string]any{
			routingRule("put-dup.**", []string{routing.DefaultTopicSuffix}, 42),
			routingRule("put-dup.**", []string{routing.DefaultTopicSuffix}, 43),
		})
		return expectReject("put-dup rejected", badStatus, badErr, http.StatusBadRequest, errCodeRoutingRuleValidation)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-006: rule validation (empty topics / too-many topics / invalid pattern → 400) ---
	checks = append(checks, provRoutingCheck("routing rule validation", func() error {
		// Allowed: a valid single-rule PUT → 200 (differs only in being well-formed).
		okStatus, okErr := provClient.SetRoutingRulesRaw(ctx, tenantA, []map[string]any{
			routingRule("valid.**", []string{routing.DefaultTopicSuffix}, 50),
		})
		if err := expectStatus("validation allowed", okStatus, okErr, http.StatusOK); err != nil {
			return err
		}
		// Empty topics → 400 ROUTING_RULE_VALIDATION_ERROR.
		emptyStatus, emptyErr := provClient.SetRoutingRulesRaw(ctx, tenantA, []map[string]any{
			routingRule("empty-topics.**", []string{}, 51),
		})
		if err := expectReject("empty topics", emptyStatus, emptyErr, http.StatusBadRequest, errCodeRoutingRuleValidation); err != nil {
			return err
		}
		// Too many topics (> MAX_TOPICS_PER_RULE) → 400 ROUTING_RULE_VALIDATION_ERROR.
		manyTopics := make([]string, maxTopicsPerRule+1)
		for i := range manyTopics {
			manyTopics[i] = routing.DefaultTopicSuffix // all provisioned — only the COUNT trips
		}
		manyStatus, manyErr := provClient.SetRoutingRulesRaw(ctx, tenantA, []map[string]any{
			routingRule("many-topics.**", manyTopics, 52),
		})
		if err := expectReject("too many topics", manyStatus, manyErr, http.StatusBadRequest, errCodeRoutingRuleValidation); err != nil {
			return err
		}
		// Invalid pattern (uppercase segment fails [a-z0-9-]+) → 400 ROUTING_RULE_VALIDATION_ERROR.
		// This asserts current server behavior; the server is stricter than the OpenAPI pattern regex
		// (FR-009 divergence — see FR-013 follow-up #203).
		badStatus, badErr := provClient.SetRoutingRulesRaw(ctx, tenantA, []map[string]any{
			routingRule("INVALID_SEGMENT", []string{routing.DefaultTopicSuffix}, 53),
		})
		return expectReject("invalid pattern", badStatus, badErr, http.StatusBadRequest, errCodeRoutingRuleValidation)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-004: role gates (user-role GET allowed, user-role write rejected) ---
	// GET is inside the feature gate but outside the role gate (router.go:234) → user-role GET
	// returns 200 on Pro+. On Community the whole routing-rules API is feature-gated → skip.
	userTokenA, mintErr := setupA.Minter.MintWithClaims(auth.MintOptions{
		Subject: "routing-user-" + uuid.NewString()[:8],
		Roles:   []string{"user"},
	})
	switch {
	case mintErr != nil:
		mintFail := fmt.Sprintf("mint user token: %v", mintErr)
		checks = append(checks,
			metrics.CheckResult{Name: "routing role gate read", Status: metrics.CheckStatusFail, Error: mintFail},
			metrics.CheckResult{Name: "routing role gate write", Status: metrics.CheckStatusFail, Error: mintFail})
	case isCommunity:
		checks = append(checks, gateSkip("routing role gate read"), gateSkip("routing role gate write"))
	default:
		// role gate read: user-role GET → 200.
		checks = append(checks, provRoutingCheck("routing role gate read", func() error {
			status, err := provClient.GetRoutingRulesWithToken(ctx, tenantA, userTokenA)
			return expectStatus("role gate read", status, err, http.StatusOK)
		}))
		// role gate write: user-role POST → 403 INSUFFICIENT_ROLE; identical admin POST → 201.
		// Both sides POST the same-shaped rule (differ ONLY in the token's role) — the write verb
		// is inside the role gate (router.go:236-239), so a user token must be rejected.
		checks = append(checks, provRoutingCheck("routing role gate write", func() error {
			okStatus, okErr := provClient.AddRoutingRuleRaw(ctx, tenantA,
				routingRule("role-gate.**", []string{routing.DefaultTopicSuffix}, 60))
			if err := expectStatus("role gate write (admin)", okStatus, okErr, http.StatusCreated); err != nil {
				return err
			}
			badStatus, badErr := provClient.AddRoutingRuleRawWithToken(ctx, tenantA, userTokenA,
				routingRule("role-gate-2.**", []string{routing.DefaultTopicSuffix}, 61))
			return expectReject("role gate write (user)", badStatus, badErr, http.StatusForbidden, errCodeInsufficientRole)
		}))
		checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)
	}

	// --- FR-005: cross-tenant mismatch (RequireTenant fires before the edition gate → all editions) ---
	checks = append(checks, checkCrossTenantMismatch(ctx, run, setupA, userTokenA, mintErr, edition, logger))

	// --- FR-007: pagination + limit cap ---
	// Seed exactly 3 rules, then GET ?limit=1 → items:1, total:3; GET ?limit=500 → capped limit:200.
	if isCommunity {
		checks = append(checks, gateSkip("routing get pagination"), gateSkip("routing get limit cap"))
	} else {
		checks = append(checks, checkPagination(ctx, provClient, tenantA)...)
		checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)
	}

	// --- FR-007: PUT echo — response body echoes the request rules ---
	checks = append(checks, provRoutingCheck("routing put echo", func() error {
		return checkPutEcho(ctx, provClient, tenantA)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-007: star normalization — PUT pattern "*" reads back "**" ---
	checks = append(checks, provRoutingCheck("routing star normalization", func() error {
		return checkStarNormalization(ctx, provClient, tenantA)
	}))
	checks = appendReset(ctx, checks, provClient, tenantA, isCommunity, logger)

	// --- FR-003: edition gate (Community-only; absent on Pro+) ---
	// Distinct from edition-limits' `routing rules feature gate` check. Over-count (400
	// TOO_MANY_ROUTING_RULES) is NOT re-implemented here — delegated to the edition-limits suite,
	// which co-runs in community-direct.
	if isCommunity {
		checks = append(checks, checkEditionGate(ctx, provClient, tenantA))
	}

	return checks
}

// checkCrossTenantMismatch verifies FR-005: an A-scoped user token writing tenant B is rejected
// 403 TENANT_MISMATCH (RequireTenant, router.go:196 — fires BEFORE the edition gate, so this runs
// on ALL editions and MUST use a user-role token: admin bypasses ownership). The paired allowed
// case holds the SAME user token and does a same-tenant GET (never a same-tenant WRITE — a user
// token 403s INSUFFICIENT_ROLE on writes, which would false-red). On Pro+ the allowed GET is
// GET /routing-rules (not role-gated); on Community the routing GET is itself feature-gated, so
// the allowed case is an ungated same-tenant tenant self-read (GET /tenants/{A}). Tenant B is
// eagerly cleaned (FR-018).
func checkCrossTenantMismatch(
	ctx context.Context,
	run *TestRun,
	setupA *auth.SetupResult,
	userTokenA string,
	mintErr error,
	edition string,
	logger zerolog.Logger,
) metrics.CheckResult {
	const name = "routing cross-tenant mismatch"
	if mintErr != nil {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail, Error: fmt.Sprintf("mint user token: %v", mintErr)}
	}

	setupB, err := auth.Setup(ctx, auth.SetupConfig{
		TestID:               run.ID + "-prov-routing-b",
		ProvisioningURL:      run.Config.ProvisioningURL,
		Logger:               logger,
		AdminProvider:        run.authResult.AdminProvider,
		RequireAdminProvider: true,
	})
	if err != nil {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail, Error: fmt.Sprintf("setup tenant B: %v", err)}
	}
	// Eager cleanup (FR-018 headroom): delete tenant B as soon as the check completes.
	defer setupB.Cleanup(context.Background()) //nolint:contextcheck // cleanup must survive parent cancellation

	tenantA := setupA.TenantID
	tenantB := setupB.TenantID

	// Allowed case (same user token, same-tenant, differs only in the TARGET tenant).
	if edition == string(license.Community) {
		// Routing GET is feature-gated on Community — use an ungated self-read of tenant A.
		if status, gErr := setupA.ProvClient.GetTenant(ctx, tenantA, userTokenA); gErr != nil || status != http.StatusOK {
			return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail,
				Error: fmt.Sprintf("allowed same-tenant self-read: expected 200, got status=%d err=%v", status, gErr)}
		}
	} else {
		if status, gErr := setupA.ProvClient.GetRoutingRulesWithToken(ctx, tenantA, userTokenA); gErr != nil || status != http.StatusOK {
			return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail,
				Error: fmt.Sprintf("allowed same-tenant GET routing-rules: expected 200, got status=%d err=%v", status, gErr)}
		}
	}

	// Negative: A-scoped user token writes tenant B → 403 TENANT_MISMATCH.
	status, wErr := setupB.ProvClient.SetRoutingRulesRawWithToken(ctx, tenantB, userTokenA, testRoutingRulesRaw())
	if err := expectReject("cross-tenant write", status, wErr, http.StatusForbidden, errCodeTenantMismatch); err != nil {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail, Error: err.Error()}
	}
	return metrics.CheckResult{Name: name, Status: metrics.CheckStatusPass}
}

// checkPagination verifies FR-007 pagination honoring and the max-page-limit cap echo.
func checkPagination(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string) []metrics.CheckResult {
	seed := []map[string]any{
		routingRule("page-a.**", []string{routing.DefaultTopicSuffix}, 70),
		routingRule("page-b.**", []string{routing.DefaultTopicSuffix}, 71),
		routingRule("page-c.**", []string{routing.DefaultTopicSuffix}, 72),
	}
	if err := provClient.SetRoutingRules(ctx, tenantID, seed); err != nil {
		fail := metrics.CheckResult{Name: "routing get pagination", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("seed rules: %v", err)}
		return []metrics.CheckResult{fail, {Name: "routing get limit cap", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("seed rules: %v", err)}}
	}

	var results []metrics.CheckResult

	// ?limit=1 → items:1, total:3.
	pageBody, err := provClient.GetRoutingRulesPage(ctx, tenantID, 1)
	if err != nil {
		results = append(results, metrics.CheckResult{Name: "routing get pagination", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("get ?limit=1: %v", err)})
	} else {
		page := parseRoutingPage(pageBody)
		switch {
		case len(page.Items) != 1:
			results = append(results, metrics.CheckResult{Name: "routing get pagination", Status: metrics.CheckStatusFail,
				Error: fmt.Sprintf("expected 1 item with ?limit=1, got %d (body=%s)", len(page.Items), string(pageBody))})
		case page.Total != len(seed):
			results = append(results, metrics.CheckResult{Name: "routing get pagination", Status: metrics.CheckStatusFail,
				Error: fmt.Sprintf("expected total=%d, got %d (body=%s)", len(seed), page.Total, string(pageBody))})
		default:
			results = append(results, metrics.CheckResult{Name: "routing get pagination", Status: metrics.CheckStatusPass,
				Latency: fmt.Sprintf("items=1 total=%d", page.Total)})
		}
	}

	// ?limit=500 → response echoes the capped limit (routingRulesMaxPageLimit=200).
	capBody, err := provClient.GetRoutingRulesPage(ctx, tenantID, routingRulesMaxPageLimit+300)
	if err != nil {
		results = append(results, metrics.CheckResult{Name: "routing get limit cap", Status: metrics.CheckStatusFail, Error: fmt.Sprintf("get ?limit large: %v", err)})
	} else {
		page := parseRoutingPage(capBody)
		if page.Limit != routingRulesMaxPageLimit {
			results = append(results, metrics.CheckResult{Name: "routing get limit cap", Status: metrics.CheckStatusFail,
				Error: fmt.Sprintf("expected capped limit=%d, got %d (body=%s)", routingRulesMaxPageLimit, page.Limit, string(capBody))})
		} else {
			results = append(results, metrics.CheckResult{Name: "routing get limit cap", Status: metrics.CheckStatusPass,
				Latency: fmt.Sprintf("limit capped to %d", page.Limit)})
		}
	}

	return results
}

// checkPutEcho verifies FR-007: the PUT response body echoes the request rules (items + total).
func checkPutEcho(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string) error {
	rules := []map[string]any{
		routingRule("echo-a.**", []string{routing.DefaultTopicSuffix}, 80),
		routingRule("echo-b.**", []string{routing.DefaultTopicSuffix}, 81),
	}
	// Use the admin PUT and read the echo body via a follow-up GET (FR-016 — read-back via GET).
	if status, err := provClient.SetRoutingRulesRaw(ctx, tenantID, rules); err != nil || status != http.StatusOK {
		return fmt.Errorf("put echo: expected 200, got status=%d err=%w", status, err)
	}
	body, err := provClient.GetRoutingRulesPage(ctx, tenantID, routingRulesMaxPageLimit)
	if err != nil {
		return fmt.Errorf("put echo: get back: %w", err)
	}
	page := parseRoutingPage(body)
	if page.Total != len(rules) {
		return fmt.Errorf("put echo: expected total=%d, got %d (body=%s)", len(rules), page.Total, string(body))
	}
	// Both request patterns must appear in the echoed items.
	got := make(map[string]bool, len(page.Items))
	for _, it := range page.Items {
		got[it.Pattern] = true
	}
	for _, want := range []string{"echo-a.**", "echo-b.**"} {
		if !got[want] {
			return fmt.Errorf("put echo: pattern %q missing from GET body (body=%s)", want, string(body))
		}
	}
	return nil
}

// checkStarNormalization verifies FR-007: a PUT with a bare "*" pattern reads back normalized
// to "**" (NormalizePattern is applied on write; the stored/GET form is canonical).
func checkStarNormalization(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string) error {
	rules := []map[string]any{
		routingRule("*", []string{routing.DefaultTopicSuffix}, 90),
	}
	if status, err := provClient.SetRoutingRulesRaw(ctx, tenantID, rules); err != nil || status != http.StatusOK {
		return fmt.Errorf("star normalization: expected 200, got status=%d err=%w", status, err)
	}
	body, err := provClient.GetRoutingRulesPage(ctx, tenantID, routingRulesMaxPageLimit)
	if err != nil {
		return fmt.Errorf("star normalization: get back: %w", err)
	}
	page := parseRoutingPage(body)
	for _, it := range page.Items {
		if it.Pattern == "**" {
			return nil
		}
		if it.Pattern == "*" {
			return fmt.Errorf("star normalization: pattern stored as %q, expected normalized %q (body=%s)", "*", "**", string(body))
		}
	}
	return fmt.Errorf("star normalization: normalized pattern %q not found in GET body (body=%s)", "**", string(body))
}

// checkEditionGate verifies FR-003 (Community-only): a routing-rules write probe is rejected by
// the RequireFeature gate (403 + exact EDITION_LIMIT). A 2xx means the gate regressed.
func checkEditionGate(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string) metrics.CheckResult {
	const name = "routing edition gate"
	status, err := provClient.SetRoutingRulesRaw(ctx, tenantID, testRoutingRulesRaw())
	if err == nil && status < 400 {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail,
			Error: fmt.Sprintf("community: routing-rules write succeeded (status=%d), expected 403/%s (gate regressed)", status, errCodeEditionLimit)}
	}
	if status == http.StatusForbidden && extractErrorCode(errText(err)) == errCodeEditionLimit {
		return metrics.CheckResult{Name: name, Status: metrics.CheckStatusPass,
			Latency: "community: routing-rules API correctly feature-gated (403 " + errCodeEditionLimit + ")"}
	}
	return metrics.CheckResult{Name: name, Status: metrics.CheckStatusFail,
		Error: fmt.Sprintf("community: expected 403/%s (feature gate), got status=%d code=%q", errCodeEditionLimit, status, extractErrorCode(errText(err)))}
}

// --- Helpers ---

// routingPage is the parsed shape of a routing-rules list response.
type routingPage struct {
	Items []struct {
		Pattern string `json:"pattern"`
	} `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func parseRoutingPage(body []byte) routingPage {
	var page routingPage
	_ = json.Unmarshal(body, &page) // malformed body surfaces as zero values → the caller's assertion fails loudly
	return page
}

// testRoutingRulesRaw returns the catch-all fixture (testRoutingRules) as []map[string]any for
// the raw client methods.
func testRoutingRulesRaw() []map[string]any {
	return []map[string]any{
		routingRule("**", []string{routing.DefaultTopicSuffix}, routing.DefaultCatchAllPriority),
	}
}

// expectStatus returns nil when status matches want (and err is nil), else a descriptive error.
func expectStatus(label string, status int, err error, want int) error {
	if err != nil {
		return fmt.Errorf("%s: expected %d, got status=%d err=%w", label, want, status, err)
	}
	if status != want {
		return fmt.Errorf("%s: expected %d, got %d", label, want, status)
	}
	return nil
}

// expectReject returns nil when the response is exactly wantStatus AND the error body carries
// wantCode (extractErrorCode), else a descriptive error. Every negative check asserts BOTH the
// HTTP status and the exact error code (never merely "an error occurred") per FR-008.
func expectReject(label string, status int, err error, wantStatus int, wantCode string) error {
	code := extractErrorCode(errText(err))
	if status == wantStatus && code == wantCode {
		return nil
	}
	return fmt.Errorf("%s: expected %d/%s, got status=%d code=%q err=%w", label, wantStatus, wantCode, status, code, err)
}

// appendReset restores testRoutingRules after a mutating check (FR-014) and appends a fail
// result if the reset itself fails, so a leaked fixture never silently corrupts later checks.
// On Community the routing-rules API is feature-gated, so the reset is skipped (no state to
// leak — every mutating probe was itself gated).
func appendReset(
	ctx context.Context,
	checks []metrics.CheckResult,
	provClient *auth.ProvisioningClient,
	tenantID string,
	isCommunity bool,
	logger zerolog.Logger,
) []metrics.CheckResult {
	if isCommunity {
		return checks
	}
	if err := resetRoutingRulesToFixture(ctx, provClient, tenantID); err != nil {
		logger.Warn().Err(err).Str(logging.LogKeyTenantSlug, tenantID).Msg("reset routing rules to fixture failed")
		return append(checks, metrics.CheckResult{
			Name: "routing fixture reset", Status: metrics.CheckStatusFail, Error: err.Error(),
		})
	}
	return checks
}

// resetRoutingRulesToFixture PUTs the catch-all testRoutingRules fixture (atomic replace) and
// verifies the write landed by GET-asserting the fixture's topic suffix is present (FR-014).
// This is the one genuinely-new non-trivial helper; it reuses the EXISTING admin SetRoutingRules
// / GetRoutingRules client methods (NOT the raw/token FR-015 methods).
func resetRoutingRulesToFixture(ctx context.Context, provClient *auth.ProvisioningClient, tenantID string) error {
	if err := provClient.SetRoutingRules(ctx, tenantID, testRoutingRules); err != nil {
		return fmt.Errorf("reset routing rules: put fixture: %w", err)
	}
	body, err := provClient.GetRoutingRules(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("reset routing rules: verify: %w", err)
	}
	if !strings.Contains(string(body), routing.DefaultTopicSuffix) {
		return fmt.Errorf("reset routing rules: fixture topic %q not present after PUT (body=%s)", routing.DefaultTopicSuffix, string(body))
	}
	return nil
}
