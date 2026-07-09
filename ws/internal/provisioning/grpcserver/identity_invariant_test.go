package grpcserver

import (
	"testing"

	"github.com/google/uuid"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/revocation"
)

// TestIdentityFieldInvariant locks the #161 identity-unification rule: a proto
// field named tenant_uuid MUST carry a parseable UUID, and a field named
// tenant_slug MUST carry a non-UUID routing label. It exercises each provisioning
// producer with a UUID-shaped ID and a deliberately non-parseable slug, so a
// future edit that maps tenant.ID / tenant.Slug into the wrong field is caught
// (the compiler cannot catch a string→string field swap). It also asserts
// value-flow: the exact input value reaches the renamed output field.
func TestIdentityFieldInvariant(t *testing.T) {
	t.Parallel()
	const (
		tenantUUID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
		tenantSlug = "acme-corp" // intentionally NOT a UUID
	)

	t.Run("convertKeys emits tenant_uuid as a UUID", func(t *testing.T) {
		t.Parallel()
		out := convertKeys([]*provisioning.TenantKey{
			{KeyID: "k1", TenantID: tenantUUID, Algorithm: "ES256", PublicKey: "pem"},
		})
		if len(out) != 1 {
			t.Fatalf("convertKeys returned %d, want 1", len(out))
		}
		if got := out[0].GetTenantUuid(); got != tenantUUID {
			t.Errorf("value-flow: KeyInfo.tenant_uuid = %q, want %q", got, tenantUUID)
		}
		if _, err := uuid.Parse(out[0].GetTenantUuid()); err != nil {
			t.Errorf("KeyInfo.tenant_uuid %q must parse as a UUID: %v", out[0].GetTenantUuid(), err)
		}
	})

	t.Run("convertAPIKeys emits tenant_uuid as a UUID", func(t *testing.T) {
		t.Parallel()
		out := convertAPIKeys([]*provisioning.APIKey{
			{KeyID: "pk_live_x", TenantID: tenantUUID, Name: "prod", IsActive: true},
		})
		if len(out) != 1 {
			t.Fatalf("convertAPIKeys returned %d, want 1", len(out))
		}
		if got := out[0].GetTenantUuid(); got != tenantUUID {
			t.Errorf("value-flow: APIKeyInfo.tenant_uuid = %q, want %q", got, tenantUUID)
		}
		if _, err := uuid.Parse(out[0].GetTenantUuid()); err != nil {
			t.Errorf("APIKeyInfo.tenant_uuid %q must parse as a UUID: %v", out[0].GetTenantUuid(), err)
		}
	})

	t.Run("convertRevocationEntries emits tenant_slug (never a UUID)", func(t *testing.T) {
		t.Parallel()
		out := convertRevocationEntries([]*revocation.Entry{
			{TenantID: tenantSlug, Type: "token", JTI: "j1", ExpiresAt: 1},
		})
		if len(out) != 1 {
			t.Fatalf("convertRevocationEntries returned %d, want 1", len(out))
		}
		if got := out[0].GetTenantSlug(); got != tenantSlug {
			t.Errorf("value-flow: TokenRevocation.tenant_slug = %q, want %q", got, tenantSlug)
		}
		if _, err := uuid.Parse(out[0].GetTenantSlug()); err == nil {
			t.Errorf("TokenRevocation.tenant_slug %q must NOT parse as a UUID (it is the routing label)", out[0].GetTenantSlug())
		}
	})
}
