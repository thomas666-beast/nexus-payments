package rest_test

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/store"
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
		fmt.Fprintf(os.Stderr, "container: %v\n", err)
		os.Exit(1)
	}
	defer c.Terminate(ctx)

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432")
	testDSN = fmt.Sprintf("postgres://nexus:nexus@%s:%s/nexus?sslmode=disable", host, port.Port())

	wd, _ := os.Getwd()
	mig, err := migrate.New("file://"+filepath.Join(wd, "..", "..", "..", "migrations"), testDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	if err := mig.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintf(os.Stderr, "migrate up: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE ledger_entries, transfers, accounts, idempotency_keys RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func ensureSystemAccount(t *testing.T, db *store.DB) {
	t.Helper()
	now := time.Now().UTC()
	err := db.WithTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return store.NewAccountRepo(db).Insert(ctx, tx, domain.Account{
			ID:          "system-genesis",
			BankID:      "system",
			ExternalRef: "system-genesis",
			Currency:    domain.USD,
			Holder:      "System Genesis",
			Status:      domain.AccountStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	})
	if err != nil {
		t.Fatalf("ensure system account: %v", err)
	}
}

func seedAccount(t *testing.T, db *store.DB, id domain.AccountID, bankID string, cur domain.Currency) {
	t.Helper()
	now := time.Now().UTC()
	err := db.WithTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return store.NewAccountRepo(db).Insert(ctx, tx, domain.Account{
			ID:          id,
			BankID:      bankID,
			ExternalRef: string(id),
			Currency:    cur,
			Holder:      string(id),
			Status:      domain.AccountStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	})
	if err != nil {
		t.Fatalf("seed account %s: %v", id, err)
	}
}

func fundAccount(t *testing.T, pool *pgxpool.Pool, id domain.AccountID, amount int64, cur domain.Currency) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	genesisTransferID := "genesis-transfer-" + string(id)
	_, err := pool.Exec(ctx, `
		INSERT INTO transfers (id, from_account, to_account, amount_minor, currency, status, reason, created_at, updated_at)
		VALUES ($1, 'system-genesis', $2, $3, $4, 'SETTLED', 'genesis', $5, $5)
		ON CONFLICT (id) DO NOTHING
	`, genesisTransferID, id, amount, string(cur), now)
	if err != nil {
		t.Fatalf("genesis transfer for %s: %v", id, err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount_minor, currency, created_at)
		VALUES ($1, $2, $3, 'CREDIT', $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, "genesis-entry-"+string(id), genesisTransferID, id, amount, string(cur), now)
	if err != nil {
		t.Fatalf("genesis entry for %s: %v", id, err)
	}
}
