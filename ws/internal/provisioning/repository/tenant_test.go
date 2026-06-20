package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	"github.com/klurvio/sukko/internal/shared/testutil"
)

func TestTenantRepository_CreateAndGet(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	repo := repository.NewTenantRepository(pool)
	ctx := context.Background()

	tenant := &provisioning.Tenant{
		ID:           "test-tenant",
		Slug:         "test-tenant",
		Name:         "Test Tenant",
		Status:       provisioning.StatusActive,
		ConsumerType: provisioning.ConsumerShared,
		Metadata:     provisioning.Metadata{"env": "test"},
	}

	if err := repo.Create(ctx, tenant); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetBySlug(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("GetBySlug() error = %v", err)
	}
	if got.Name != "Test Tenant" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Tenant")
	}
	if got.Status != provisioning.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, provisioning.StatusActive)
	}
}

func TestTenantRepository_CreateDuplicate(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	repo := repository.NewTenantRepository(pool)
	ctx := context.Background()

	tenant := &provisioning.Tenant{
		ID:           "dup-tenant",
		Slug:         "dup-tenant",
		Name:         "Dup Tenant",
		Status:       provisioning.StatusActive,
		ConsumerType: provisioning.ConsumerShared,
	}

	if err := repo.Create(ctx, tenant); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	err := repo.Create(ctx, tenant)
	if err == nil {
		t.Fatal("second Create() should return error for duplicate ID")
	}
}

func TestTenantRepository_GetNotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	repo := repository.NewTenantRepository(pool)
	ctx := context.Background()

	_, err := repo.GetBySlug(ctx, "nonexistent")
	if err == nil {
		t.Fatal("GetBySlug() should return error for nonexistent tenant")
	}
}

func TestTenantRepository_CountActive(t *testing.T) {
	t.Parallel()
	pool := testutil.NewTestPool(t)
	repo := repository.NewTenantRepository(pool)
	ctx := context.Background()

	// Empty DB: should return 0, not error.
	n, err := repo.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() on empty DB error = %v", err)
	}
	if n != 0 {
		t.Errorf("CountActive() on empty DB = %d, want 0", n)
	}

	newTenant := func(slug string) *provisioning.Tenant {
		return &provisioning.Tenant{
			ID:           uuid.NewString(),
			Slug:         slug,
			Name:         slug,
			Status:       provisioning.StatusActive,
			ConsumerType: provisioning.ConsumerShared,
			Metadata:     provisioning.Metadata{},
		}
	}

	// Create 2 active tenants.
	for _, slug := range []string{"cnt-active-1", "cnt-active-2"} {
		if err := repo.Create(ctx, newTenant(slug)); err != nil {
			t.Fatalf("Create(%s) error = %v", slug, err)
		}
	}
	// Create 1 suspended tenant (must not be counted).
	susp := newTenant("cnt-susp-1")
	if err := repo.Create(ctx, susp); err != nil {
		t.Fatalf("Create(suspended) error = %v", err)
	}
	if err := repo.UpdateStatus(ctx, susp.ID, provisioning.StatusSuspended); err != nil {
		t.Fatalf("UpdateStatus(suspended) error = %v", err)
	}

	n, err = repo.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() error = %v", err)
	}
	if n != 2 {
		t.Errorf("CountActive() = %d, want 2 (active only, suspended excluded)", n)
	}
}
