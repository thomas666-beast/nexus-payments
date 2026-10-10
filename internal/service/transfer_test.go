package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

func setup(t *testing.T) (*service.Service, *store.DB, *pgxpool.Pool) {
	t.Helper()
	pool := newPool(t)
	truncate(t, pool)
	db := &store.DB{Pool: pool}
	ensureSystemAccount(t, db)
	eng := ledger.New(nil)
	svc := service.New(db, eng, service.DefaultConfig(), nil)
	return svc, db, pool
}

// seedActiveAccount inserts an ACTIVE account with zero starting balance.
func seedActiveAccount(t *testing.T, db *store.DB, id domain.AccountID, cur domain.Currency) {
	t.Helper()
	now := time.Now().UTC()
	err := db.WithTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return store.NewAccountRepo(db).Insert(ctx, tx, domain.Account{
			ID:          id,
			BankID:      "bank-test",
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

// ensureSystemAccount creates a synthetic genesis account used only by test
// fixtures. Real transfers never reference it.
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
	if err != nil && !errors.Is(err, store.ErrConflict) {
		t.Fatalf("ensure system account: %v", err)
	}
}

// fundAccount seeds a starting balance by inserting a synthetic transfer and
// a matching CREDIT ledger entry. This is test setup, not a real transfer —
// it deliberately bypasses the service to inject history.
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

func TestExecute_HappyPath(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	res, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount: domain.MustMoney(250, domain.USD),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Transfer.Status != domain.TransferStatusSettled {
		t.Fatalf("status want SETTLED, got %s", res.Transfer.Status)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(res.Entries))
	}
	if err := ledger.AssertBalanced(res.Entries); err != nil {
		t.Fatalf("entries not balanced: %v", err)
	}
}

func TestExecute_InsufficientFunds(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 100, domain.USD)

	_, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount: domain.MustMoney(500, domain.USD),
	})
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM transfers WHERE from_account = 'alice' AND reason <> 'genesis' ORDER BY created_at DESC LIMIT 1`,
	).Scan(&status); err != nil {
		t.Fatalf("query transfer: %v", err)
	}
	if status != "FAILED" {
		t.Fatalf("want FAILED persisted, got %s", status)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ledger_entries WHERE account_id = 'bob'`,
	).Scan(&count); err != nil {
		t.Fatalf("count bob entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected zero entries for failed transfer, got %d", count)
	}
}

func TestExecute_FrozenFromAccount(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	if err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return store.NewAccountRepo(db).UpdateStatus(ctx, tx, "alice", domain.AccountStatusFrozen)
	}); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	_, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount: domain.MustMoney(100, domain.USD),
	})
	if !errors.Is(err, ledger.ErrAccountNotActive) {
		t.Fatalf("want ErrAccountNotActive, got %v", err)
	}
}

func TestExecute_SelfTransferRejected(t *testing.T) {
	svc, _, _ := setup(t)
	_, err := svc.Execute(context.Background(), service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "alice",
		Amount: domain.MustMoney(100, domain.USD),
	})
	if !errors.Is(err, service.ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestExecute_ConcurrentTransfers_NoDoubleSpend(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	seedActiveAccount(t, db, "carol", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	const N = 20
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		to := domain.AccountID("bob")
		if i%2 == 0 {
			to = "carol"
		}
		go func(to domain.AccountID) {
			_, err := svc.Execute(ctx, service.ExecuteRequest{
				FromAccount: "alice", ToAccount: to,
				Amount: domain.MustMoney(100, domain.USD),
			})
			errs <- err
		}(to)
	}

	var success, failed, exhausted int
	for i := 0; i < N; i++ {
		err := <-errs
		switch {
		case err == nil:
			success++
		case errors.Is(err, ledger.ErrInsufficientFunds):
			failed++
		case errors.Is(err, service.ErrRetriesExhausted):
			exhausted++
		default:
			t.Fatalf("unexpected error type: %v", err)
		}
	}

	// Invariant: money spent from alice must never exceed the starting balance.
	// The number of successes can vary (10 to 20 depending on scheduling and
	// retry outcomes), but the sum of debited amounts must be ≤ 1000.
	if success+failed+exhausted != N {
		t.Fatalf("accounting mismatch: success=%d failed=%d exhausted=%d total=%d",
			success, failed, exhausted, N)
	}

	// Check the ledger directly: total money debited from alice must be ≤ 1000.
	var totalDebited int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(-SUM(amount_minor), 0) FROM ledger_entries
		WHERE account_id = 'alice' AND direction = 'DEBIT'
	`).Scan(&totalDebited); err != nil {
		t.Fatalf("sum debits: %v", err)
	}
	if totalDebited > 1000 {
		t.Fatalf("LEDGER INVARIANT VIOLATED: total debited %d exceeds balance 1000", totalDebited)
	}
	if totalDebited != int64(success)*100 {
		t.Fatalf("debited total (%d) != success count * 100 (%d)", totalDebited, success*100)
	}

	// Settled transfers from alice must equal successes.
	var settled int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transfers WHERE status = 'SETTLED' AND from_account = 'alice'`,
	).Scan(&settled); err != nil {
		t.Fatalf("count settled: %v", err)
	}
	if settled != success {
		t.Fatalf("success count (%d) != settled transfers from alice in db (%d)", success, settled)
	}
}
