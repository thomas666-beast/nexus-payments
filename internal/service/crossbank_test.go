package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/service"
)

func TestCrossBank_ReserveCommit(t *testing.T) {
	svc, db, pool, banks := setupWithBanks(t, []string{"bank-a", "bank-b"}, 1000)
	ctx := context.Background()

	seedAccountWithBank(t, db, "alice", "bank-a", domain.USD)
	seedAccountWithBank(t, db, "bob", "bank-b", domain.USD)
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

	if got := banks["bank-a"].Balance("alice").Amount; got != 750 {
		t.Fatalf("bank-a alice balance want 750, got %d", got)
	}
	if n := banks["bank-a"].Reservations(); n != 0 {
		t.Fatalf("bank-a reservations want 0, got %d", n)
	}
}

func TestCrossBank_ReserveFails_RollsBackCleanly(t *testing.T) {
	svc, db, pool, banks := setupWithBanks(t, []string{"bank-a", "bank-b"}, 1000)
	ctx := context.Background()

	seedAccountWithBank(t, db, "alice", "bank-a", domain.USD)
	seedAccountWithBank(t, db, "bob", "bank-b", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	banks["bank-a"].FailNextReserve(adapter.ErrBankUnavailable)

	_, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount: domain.MustMoney(250, domain.USD),
	})
	if !errors.Is(err, adapter.ErrBankUnavailable) {
		t.Fatalf("want ErrBankUnavailable, got %v", err)
	}

	if got := banks["bank-a"].Balance("alice").Amount; got != 1000 {
		t.Fatalf("bank-a alice balance want 1000, got %d", got)
	}
	if n := banks["bank-a"].Reservations(); n != 0 {
		t.Fatalf("bank-a reservations want 0, got %d", n)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM transfers WHERE from_account = 'alice' AND reason <> 'genesis' ORDER BY created_at DESC LIMIT 1`,
	).Scan(&status); err != nil {
		t.Fatalf("query transfer: %v", err)
	}
	if status != "FAILED" {
		t.Fatalf("want FAILED, got %s", status)
	}
}

func TestCrossBank_CommitFails_ReservationRolledBack(t *testing.T) {
	svc, db, pool, banks := setupWithBanks(t, []string{"bank-a", "bank-b"}, 1000)
	ctx := context.Background()

	seedAccountWithBank(t, db, "alice", "bank-a", domain.USD)
	seedAccountWithBank(t, db, "bob", "bank-b", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	banks["bank-a"].FailNextCommit(adapter.ErrBankUnavailable)

	_, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount: domain.MustMoney(250, domain.USD),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if got := banks["bank-a"].Balance("alice").Amount; got != 1000 {
		t.Fatalf("bank-a alice balance want 1000, got %d", got)
	}
	if n := banks["bank-a"].Reservations(); n != 0 {
		t.Fatalf("bank-a reservations want 0, got %d", n)
	}
}

func TestSameBank_SkipsAdapter(t *testing.T) {
	svc, db, pool, banks := setupWithBanks(t, []string{"bank-a"}, 1000)
	ctx := context.Background()

	seedAccountWithBank(t, db, "alice", "bank-a", domain.USD)
	seedAccountWithBank(t, db, "bob", "bank-a", domain.USD)
	fundAccount(t, pool, "alice", 1000, domain.USD)

	_, err := svc.Execute(ctx, service.ExecuteRequest{
		FromAccount: "alice", ToAccount: "bob",
		Amount: domain.MustMoney(250, domain.USD),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Same-bank: adapter never called, so bank-a's mock balance is untouched.
	if got := banks["bank-a"].Balance("alice").Amount; got != 1000 {
		t.Fatalf("bank-a alice balance want 1000 (adapter skipped), got %d", got)
	}
}
