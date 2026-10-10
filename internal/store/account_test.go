package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

func TestAccountRepo_InsertAndGet(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	ctx := context.Background()
	db := &store.DB{Pool: pool}
	repo := store.NewAccountRepo(db)

	acct := domain.Account{
		ID:          "acc-1",
		BankID:      "bank-a",
		ExternalRef: "ext-1",
		Currency:    domain.USD,
		Holder:      "Alice",
		Status:      domain.AccountStatusActive,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.Insert(ctx, tx, acct)
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		got, err := repo.GetByID(ctx, tx, "acc-1")
		if err != nil {
			return err
		}
		if got.Holder != "Alice" {
			t.Fatalf("holder want Alice, got %s", got.Holder)
		}
		if got.Currency != domain.USD {
			t.Fatalf("currency want USD, got %s", got.Currency)
		}
		return nil
	}); err != nil {
		t.Fatalf("get: %v", err)
	}
}

func TestAccountRepo_DuplicateBankRefFails(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	ctx := context.Background()
	db := &store.DB{Pool: pool}
	repo := store.NewAccountRepo(db)

	a := domain.Account{
		ID: "acc-1", BankID: "bank-a", ExternalRef: "ext-1",
		Currency: domain.USD, Holder: "Alice", Status: domain.AccountStatusActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	b := a
	b.ID = "acc-2" // different ID, same bank+external_ref

	if err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.Insert(ctx, tx, a)
	}); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.Insert(ctx, tx, b)
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestAccountRepo_NotFound(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	ctx := context.Background()
	db := &store.DB{Pool: pool}
	repo := store.NewAccountRepo(db)

	err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := repo.GetByID(ctx, tx, "missing")
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
