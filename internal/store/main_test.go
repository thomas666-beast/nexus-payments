package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testDSN string

func TestMain(m *testing.M) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "nexus",
			"POSTGRES_PASSWORD": "nexus",
			"POSTGRES_DB":       "nexus",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start container: %v\n", err)
		os.Exit(1)
	}
	defer c.Terminate(ctx)

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432")
	testDSN = fmt.Sprintf("postgres://nexus:nexus@%s:%s/nexus?sslmode=disable", host, port.Port())

	if err := runMigrations(testDSN); err != nil {
		fmt.Fprintf(os.Stderr, "migrations: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func runMigrations(dsn string) error {
	wd, _ := os.Getwd()
	migrationsPath := filepath.Join(wd, "..", "..", "migrations")
	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return err
	}
	return m.Up()
}

// newTestDB returns a fresh pool connected to the shared test container.
func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// truncateAll clears tables between tests. FKs require cascade-friendly order.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// ledger_entries is append-only (trigger blocks DELETE), so we use
	// TRUNCATE which is DDL and bypasses row triggers.
	_, err := pool.Exec(ctx, `
		TRUNCATE ledger_entries, transfers, accounts, idempotency_keys RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
