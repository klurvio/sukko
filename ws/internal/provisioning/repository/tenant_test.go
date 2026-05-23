package repository_test

import (
	"context"
	"testing"

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
