package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/service"
)

func TestIdempotency_SameKeySameBody_ReturnsSameResult(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	req := service.ExecuteRequest{
		FromAccount:    "alice",
		ToAccount:      "bob",
		Amount:         domain.MustMoney(100, domain.USD),
		IdempotencyKey: "test-key-1",
	}

	res1, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	res2, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if res1.Transfer.ID != res2.Transfer.ID {
		t.Fatalf("transfer IDs differ: %s vs %s", res1.Transfer.ID, res2.Transfer.ID)
	}

	// Only one transfer in DB from alice (excluding genesis).
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transfers WHERE from_account = 'alice' AND reason <> 'genesis'`,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 transfer, got %d", count)
	}
}

func TestIdempotency_SameKeyDifferentBody_Conflict(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	_, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount:    "alice",
		ToAccount:      "bob",
		Amount:         domain.MustMoney(100, domain.USD),
		IdempotencyKey: "test-key-2",
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, err = svc.Execute(ctx, service.ExecuteRequest{
		FromAccount:    "alice",
		ToAccount:      "bob",
		Amount:         domain.MustMoney(200, domain.USD), // different amount
		IdempotencyKey: "test-key-2",
	})
	if !errors.Is(err, service.ErrIdempotencyConflict) {
		t.Fatalf("want ErrIdempotencyConflict, got %v", err)
	}
}

func TestIdempotency_FailedTransferIsReplayed(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 100, domain.USD)

	req := service.ExecuteRequest{
		FromAccount:    "alice",
		ToAccount:      "bob",
		Amount:         domain.MustMoney(500, domain.USD), // too much
		IdempotencyKey: "test-key-3",
	}

	_, err1 := svc.Execute(ctx, req)
	if err1 == nil {
		t.Fatal("first call should fail")
	}

	_, err2 := svc.Execute(ctx, req)
	if err2 == nil {
		t.Fatal("second call should also fail")
	}

	// Both should have the same error message.
	if err1.Error() != err2.Error() {
		t.Fatalf("error messages differ:\n  first:  %v\n  second: %v", err1, err2)
	}
}

func TestIdempotency_ConcurrentSameKey_OnlyOneExecutes(t *testing.T) {
	svc, db, pool := setup(t)
	ctx := context.Background()

	seedActiveAccount(t, db, "alice", domain.USD)
	seedActiveAccount(t, db, "bob", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	req := service.ExecuteRequest{
		FromAccount:    "alice",
		ToAccount:      "bob",
		Amount:         domain.MustMoney(100, domain.USD),
		IdempotencyKey: "concurrent-key",
	}

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	results := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Execute(ctx, req)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes, inProgress int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, service.ErrIdempotencyInProgress):
			inProgress++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if successes < 1 {
		t.Fatalf("want at least 1 success, got %d", successes)
	}

	// Exactly one transfer must exist.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transfers WHERE from_account = 'alice' AND reason <> 'genesis'`,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 transfer, got %d", count)
	}
	t.Logf("successes=%d in-progress=%d", successes, inProgress)
}
