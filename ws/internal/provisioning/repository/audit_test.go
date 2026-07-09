package repository_test

import (
	"context"
	"testing"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	"github.com/klurvio/sukko/internal/shared/testutil"
)

// TestAuditRepository_Log_TenantIDColumnIsUUID documents and enforces the provisioning_audit
// tenant_id UUID-column contract that motivates #166: audit entries MUST carry the tenant
// UUID, not a slug. A slug is not valid UUID syntax and is rejected by Postgres — which is
// exactly why the connections force-disconnect handlers must pass the stashed tenant UUID
// (not claims.TenantID, a slug) to AuditLog.
func TestAuditRepository_Log_TenantIDColumnIsUUID(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	repo := repository.NewAuditRepository(pool)
	ctx := context.Background()

	// A tenant UUID (no FK on provisioning_audit.tenant_id, so any valid UUID persists).
	tenantUUID := mustCreateTenant(t, pool, "audit-"+uniqueSuffix(t))

	if err := repo.Log(ctx, &provisioning.AuditEntry{
		TenantID:  tenantUUID,
		Action:    "test.action",
		Actor:     "tester",
		ActorType: provisioning.ActorTypeSystem,
	}); err != nil {
		t.Fatalf("log with tenant UUID: unexpected error: %v", err)
	}

	// A slug (non-UUID string) must be rejected by the UUID column.
	err := repo.Log(ctx, &provisioning.AuditEntry{
		TenantID:  "acme-slug",
		Action:    "test.action",
		Actor:     "tester",
		ActorType: provisioning.ActorTypeSystem,
	})
	if err == nil {
		t.Fatal("logging a slug into the UUID tenant_id column must fail, got nil error")
	}
}
