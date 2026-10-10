package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/adapter/breaker"
	"github.com/thomas666-beast/nexus-payments/internal/adapter/mock"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

type brokenBank struct {
	inner       *mock.Bank
	failAfter   int64
	reserveHits int64
}

func (b *brokenBank) BankID() string { return b.inner.BankID() }

func (b *brokenBank) Reserve(ctx context.Context, req adapter.ReserveRequest) (adapter.Reservation, error) {
	n := atomic.AddInt64(&b.reserveHits, 1)
	if b.failAfter > 0 && n > b.failAfter {
		return adapter.Reservation{}, adapter.ErrBankUnavailable
	}
	return b.inner.Reserve(ctx, req)
}

func (b *brokenBank) Commit(ctx context.Context, res adapter.Reservation) error {
	return b.inner.Commit(ctx, res)
}

func (b *brokenBank) Rollback(ctx context.Context, res adapter.Reservation) error {
	return b.inner.Rollback(ctx, res)
}

func (b *brokenBank) Reconcile(ctx context.Context, from, to time.Time) ([]domain.TransferID, error) {
	return b.inner.Reconcile(ctx, from, to)
}

func TestChaos_EndToEnd(t *testing.T) {
	ctx := context.Background()

	// --- Setup ---
	pool := newPool(t)
	truncate(t, pool)
	db := &store.DB{Pool: pool}
	ensureSystemAccount(t, db)
	seedAccountWithBank(t, db, "alice", "bank-a", domain.USD)
	seedAccountWithBank(t, db, "bob", "bank-b", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	innerBankA := mock.NewBank("bank-a", map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(100000, domain.USD),
	})
	bb := &brokenBank{inner: innerBankA, failAfter: 5}
	settings := breaker.DefaultSettings()
	settings.Threshold = 3
	settings.Timeout = 100 * time.Millisecond
	wrappedA := breaker.New(bb, settings)

	bankB := mock.NewBank("bank-b", map[domain.AccountID]domain.Money{
		"bob": domain.MustMoney(100000, domain.USD),
	})

	reg := adapter.NewRegistry()
	reg.Register(wrappedA)
	reg.Register(bankB)

	svc := service.New(db, ledger.New(nil), reg, service.DefaultConfig(), nil, nil)

	// --- Phase 1 ---
	const N = 30
	var wg sync.WaitGroup
	wg.Add(N)
	results := make(chan error, N)

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Execute(ctx, service.ExecuteRequest{
				FromAccount: "alice",
				ToAccount:   "bob",
				Amount:      domain.MustMoney(10, domain.USD),
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var settled, insufficient, circuitOpen, bankUnavailable, other int
	for err := range results {
		switch {
		case err == nil:
			settled++
		case errors.Is(err, ledger.ErrInsufficientFunds):
			insufficient++
		case errors.Is(err, breaker.ErrCircuitOpen):
			circuitOpen++
		case errors.Is(err, adapter.ErrBankUnavailable):
			bankUnavailable++
		default:
			other++
			t.Logf("other error: %v", err)
		}
	}

	t.Logf("phase 1: settled=%d insufficient=%d circuitOpen=%d bankUnavailable=%d other=%d",
		settled, insufficient, circuitOpen, bankUnavailable, other)

	if settled == 0 {
		t.Fatal("expected at least some transfers to settle")
	}
	if circuitOpen == 0 && bankUnavailable == 0 {
		t.Fatal("expected chaos: breaker should have opened or bank should have failed")
	}

	// --- Phase 2 ---
	var aliceBalance, bobBalance int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_minor), 0) FROM ledger_entries WHERE account_id = 'alice'`,
	).Scan(&aliceBalance); err != nil {
		t.Fatalf("alice balance: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_minor), 0) FROM ledger_entries WHERE account_id = 'bob'`,
	).Scan(&bobBalance); err != nil {
		t.Fatalf("bob balance: %v", err)
	}

	expectedTransferred := int64(settled) * 10
	if aliceBalance != 1000-expectedTransferred {
		t.Fatalf("alice balance: want %d, got %d", 1000-expectedTransferred, aliceBalance)
	}
	if bobBalance != expectedTransferred {
		t.Fatalf("bob balance: want %d, got %d", expectedTransferred, bobBalance)
	}

	var ledgerSum int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_minor), 0) FROM ledger_entries WHERE transfer_id NOT LIKE 'genesis%'`,
	).Scan(&ledgerSum); err != nil {
		t.Fatalf("ledger sum: %v", err)
	}
	if ledgerSum != 0 {
		t.Fatalf("LEDGER IMBALANCE: sum=%d, want 0", ledgerSum)
	}

	// --- Phase 3 ---
	key := "chaos-key-1"
	_, err1 := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount:         domain.MustMoney(10, domain.USD),
		IdempotencyKey: key,
	})
	_, err2 := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount:         domain.MustMoney(10, domain.USD),
		IdempotencyKey: key,
	})
	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("idempotency mismatch: err1=%v err2=%v", err1, err2)
	}
	if err1 != nil && err2 != nil && err1.Error() != err2.Error() {
		t.Fatalf("idempotent errors differ: %v vs %v", err1, err2)
	}

	// --- Phase 4 ---
	t.Logf("phase 4: breaker state = %s", wrappedA.State())
}
