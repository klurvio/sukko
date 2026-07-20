package runner

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/klurvio/sukko/cmd/tester/metrics"
)

// TestClassifyRoutingRules pins the per-edition routing-rules classification, including the
// CROSSED negatives that the two DISTINCT rejections make possible: the routing-rules API is
// feature-gated on Community (403 EDITION_LIMIT) but count-limited on paid editions (400
// TOO_MANY_ROUTING_RULES). Discriminating on status+code — never message substrings — must
// keep a count rejection from being read as a feature gate and vice-versa.
func TestClassifyRoutingRules(t *testing.T) {
	t.Parallel()

	const (
		editionLimitBody = `HTTP 403: {"code":"EDITION_LIMIT","message":"This feature requires pro edition or higher"}`
		tooManyBody      = `HTTP 400: {"code":"TOO_MANY_ROUTING_RULES","message":"Too many routing rules"}`
		otherForbidden   = `HTTP 403: {"code":"FORBIDDEN","message":"nope"}`
	)

	tests := []struct {
		name       string
		edition    string
		statusCode int
		errText    string
		wantName   string
		wantStatus string
	}{
		{
			name:       "community feature gate 403 EDITION_LIMIT passes",
			edition:    "community",
			statusCode: http.StatusForbidden,
			errText:    editionLimitBody,
			wantName:   "routing rules feature gate",
			wantStatus: metrics.CheckStatusPass,
		},
		{
			name:       "community CROSSED: 400 TOO_MANY_ROUTING_RULES is NOT the feature gate",
			edition:    "community",
			statusCode: http.StatusBadRequest,
			errText:    tooManyBody,
			wantName:   "routing rules feature gate",
			wantStatus: metrics.CheckStatusFail,
		},
		{
			name:       "community 403 without EDITION_LIMIT fails",
			edition:    "community",
			statusCode: http.StatusForbidden,
			errText:    otherForbidden,
			wantName:   "routing rules feature gate",
			wantStatus: metrics.CheckStatusFail,
		},
		{
			name:       "community unexpected 200 (gate regressed) fails",
			edition:    "community",
			statusCode: http.StatusOK,
			errText:    "",
			wantName:   "routing rules feature gate",
			wantStatus: metrics.CheckStatusFail,
		},
		{
			name:       "pro count limit 400 TOO_MANY_ROUTING_RULES passes",
			edition:    "pro",
			statusCode: http.StatusBadRequest,
			errText:    tooManyBody,
			wantName:   "routing rules limit rejection",
			wantStatus: metrics.CheckStatusPass,
		},
		{
			name:       "pro CROSSED: feature-gate 403 EDITION_LIMIT is NOT a count-limit pass",
			edition:    "pro",
			statusCode: http.StatusForbidden,
			errText:    editionLimitBody,
			wantName:   "routing rules limit rejection",
			wantStatus: metrics.CheckStatusFail,
		},
		{
			name:       "pro over-count unexpectedly accepted (200) fails",
			edition:    "pro",
			statusCode: http.StatusOK,
			errText:    "",
			wantName:   "routing rules limit rejection",
			wantStatus: metrics.CheckStatusFail,
		},
		{
			name:       "enterprise paid-branch count limit 400 passes",
			edition:    "enterprise",
			statusCode: http.StatusBadRequest,
			errText:    tooManyBody,
			wantName:   "routing rules limit rejection",
			wantStatus: metrics.CheckStatusPass,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyRoutingRules(tc.edition, tc.statusCode, tc.errText)
			if got.Name != tc.wantName {
				t.Errorf("classifyRoutingRules(%q, %d) name = %q, want %q", tc.edition, tc.statusCode, got.Name, tc.wantName)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("classifyRoutingRules(%q, %d) status = %q, want %q (result: %+v)", tc.edition, tc.statusCode, got.Status, tc.wantStatus, got)
			}
		})
	}
}

// TestEditionTestTenantSlugsAreValid guards against a regression where the test-tenant slug
// prefix produces slugs that provisioning rejects. Tenant slugs must match
// ^[a-z][a-z0-9-]{2,62}$ (see internal/provisioning/types.go); an underscore-based prefix
// yielded an invalid slug that surfaced as HTTP 500 CREATE_FAILED and failed the edition-limits
// grid cell. The uuid.New().String()[:8] suffix is always 8 lowercase-hex chars (no hyphen).
func TestEditionTestTenantSlugsAreValid(t *testing.T) {
	t.Parallel()
	// Mirror provisioning's tenantSlugPattern.
	slugRe := regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)
	const sampleUUID8 = "550e8400" // representative uuid.New().String()[:8]
	for _, slug := range []string{
		editionTestTenantPrefix + sampleUUID8,             // shared/headroom test tenants
		editionTestTenantPrefix + "reject-" + sampleUUID8, // boundary-rejection tenant
	} {
		if !slugRe.MatchString(slug) {
			t.Errorf("test-tenant slug %q is not a valid tenant slug (must match %s)", slug, slugRe)
		}
	}
}

// TestConnLimitRejectionCheckName pins the exported check-name constant that the e2e skip
// allow-list depends on. taskfiles/e2e/battery_guard_test.sh greps this exact value out of
// edition_limits.go and asserts taskfiles/e2e.yml declares edition-limits:<value> (FR-016);
// this test is the Go half of that cross-file binding — renaming the constant here forces a
// deliberate, test-breaking change that the bash half will then flag if e2e.yml lags.
func TestConnLimitRejectionCheckName(t *testing.T) {
	t.Parallel()
	if ConnLimitRejectionCheckName != "connection limit rejection" {
		t.Fatalf("ConnLimitRejectionCheckName = %q; the e2e ALLOWED_SKIPS entry (edition-limits:connection limit rejection) and battery_guard_test.sh must be updated in lockstep", ConnLimitRejectionCheckName)
	}
}
