package store_test

import (
	"context"
	"errors"
	"testing"
	// "time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

func TestTransferRepo_TransitionStatus(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	db := &store.DB{Pool: pool}
	seedTransfer(t, db)
	ctx := context.Background()
	repo := store.NewTransferRepo(db)

	// INITIATED -> VALIDATED
	if err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.TransitionStatus(ctx, tx, "t1", domain.TransferStatusInitiated, domain.TransferStatusValidated, "")
	}); err != nil {
		t.Fatalf("initiated->validated: %v", err)
	}

	// VALIDATED -> SETTLED
	if err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.TransitionStatus(ctx, tx, "t1", domain.TransferStatusValidated, domain.TransferStatusSettled, "")
	}); err != nil {
		t.Fatalf("validated->settled: %v", err)
	}

	// SETTLED -> anything is illegal
	err := db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.TransitionStatus(ctx, tx, "t1", domain.TransferStatusSettled, domain.TransferStatusFailed, "x")
	})
	if err == nil {
		t.Fatal("expected illegal transition error, got nil")
	}
}

func TestTransferRepo_ConcurrentTransition_OnlyOneWins(t *testing.T) {
	pool := newTestDB(t)
	truncateAll(t, pool)
	db := &store.DB{Pool: pool}
	seedTransfer(t, db)
	ctx := context.Background()
	repo := store.NewTransferRepo(db)

	const N = 5
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			errs <- db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return repo.TransitionStatus(ctx, tx, "t1",
					domain.TransferStatusInitiated, domain.TransferStatusValidated, "")
			})
		}()
	}

	var success, conflict int
	for i := 0; i < N; i++ {
		err := <-errs
		switch {
		case err == nil:
			success++
		case errors.Is(err, store.ErrConflict):
			conflict++
		default:
			// serialization retry is acceptable here — count as conflict
			conflict++
		}
	}
	if success != 1 {
		t.Fatalf("want exactly 1 success, got %d (conflicts=%d)", success, conflict)
	}
}