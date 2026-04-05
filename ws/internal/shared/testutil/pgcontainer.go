package testutil

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewTestPool creates a PostgreSQL container and returns a pgxpool.Pool.
// Migrations from shared/migrations/postgres/ are applied programmatically.
// Container is auto-cleaned when the test ends.
func NewTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("sukko_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Apply migrations programmatically
	applyMigrations(ctx, t, pool)

	return pool
}

func applyMigrations(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	// Find migrations directory — walk up from test working dir
	migrationsDir := findMigrationsDir(t)

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	// Sort and apply each .sql file
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		sql, err := os.ReadFile(filepath.Join(migrationsDir, f)) //nolint:gosec // G304: test helper reads known migration SQL files from repo, not user input
		if err != nil {
			t.Fatalf("read migration %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", f, err)
		}
	}
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	// Try relative paths from common test locations
	candidates := []string{
		"../../shared/migrations/postgres",          // from provisioning/repository/
		"../../../shared/migrations/postgres",       // from push/repository/
		"../../internal/shared/migrations/postgres", // from cmd/
		"../internal/shared/migrations/postgres",    // from ws/
		"internal/shared/migrations/postgres",       // from ws root
		"../migrations/postgres",                    // from shared/testutil/
		"../shared/migrations/postgres",             // from internal/
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	t.Fatal("cannot find shared/migrations/postgres directory")
	return ""
}
