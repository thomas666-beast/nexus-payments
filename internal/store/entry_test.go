package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

func seedTransfer(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := context.Background()
	ar := store.NewAccountRepo(db)
	tr := store.NewTransferRepo(db)
	er := store.NewEntryRepo(db)

	now := time.Now().UTC()
	err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := ar.Insert(ctx, tx, domain.Account{
			ID: "a1", BankID: "b", ExternalRef: "e1", Currency: domain.USD,
			Holder: "A", Status: domain.AccountStatusActive, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		if err := ar.Insert(ctx, tx, domain.Account{
			ID: "a2", BankID: "b", ExternalRef: "e2", Currency: domain.USD,
			Holder: "B", Status: domain.AccountStatusActive, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		txfr := domain.Transfer{
			ID: "t1", FromAccount: "a1", ToAccount: "a2",
			Amount: domain.MustMoney(100, domain.USD), Status: domain.TransferStatusInitiated,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tr.Insert(ctx, tx, txfr); err != nil {
			return err
		}
		entries := []domain.LedgerEntry{
			{ID: "e1", TransferID: "t1", AccountID: "a1", Direction: domain.DirectionDebit, Amount: domain.MustMoney(-100, domain.USD), CreatedAt: now},
			{ID: "e2", TransferID: "t1", AccountID: "a2", Direction: domain.DirectionCredit, Amount: domain.MustMoney(100, domain.USD), CreatedAt: now},
		}
		return er.Insert(ctx, tx, entries)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestEntryRepo_AppendOnly_RejectsUpdate(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	db := &store.DB{Pool: pool}
	seedTransfer(t, db)

	err := db.WithTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE ledger_entries SET amount_minor = 999 WHERE id = 'e1'`)
		return err
	})
	if err == nil {
		t.Fatal("expected append-only trigger to reject UPDATE, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestEntryRepo_AppendOnly_RejectsDelete(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	db := &store.DB{Pool: pool}
	seedTransfer(t, db)

	err := db.WithTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM ledger_entries WHERE id = 'e1'`)
		return err
	})
	if err == nil {
		t.Fatal("expected append-only trigger to reject DELETE, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestEntryRepo_Balance(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	db := &store.DB{Pool: pool}
	seedTransfer(t, db)

	er := store.NewEntryRepo(db)
	ctx := context.Background()

	err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		from, err := er.Balance(ctx, tx, "a1")
		if err != nil {
			return err
		}
		to, err := er.Balance(ctx, tx, "a2")
		if err != nil {
			return err
		}
		if from.Amount != -100 {
			t.Fatalf("from balance want -100, got %d", from.Amount)
		}
		if to.Amount != 100 {
			t.Fatalf("to balance want 100, got %d", to.Amount)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
}
