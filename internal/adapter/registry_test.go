package adapter_test

import (
	"errors"
	"testing"

	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/adapter/mock"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

func TestRegistry_Get(t *testing.T) {
	r := adapter.NewRegistry()
	b := mock.NewBank("bank-a", nil)
	r.Register(b)

	got, err := r.Get("bank-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BankID() != "bank-a" {
		t.Fatalf("bank id want bank-a, got %s", got.BankID())
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := adapter.NewRegistry()
	_, err := r.Get("missing")
	if !errors.Is(err, adapter.ErrUnknownBank) {
		t.Fatalf("want ErrUnknownBank, got %v", err)
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	r := adapter.NewRegistry()
	r.Register(mock.NewBank("bank-a", nil))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.Register(mock.NewBank("bank-a", nil))
}

func TestMockBank_ReserveCommit(t *testing.T) {
	bal := map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(1000, domain.USD),
	}
	b := mock.NewBank("bank-a", bal)

	res, err := b.Reserve(nil, adapter.ReserveRequest{
		TransferID: "t1",
		AccountID:  "alice",
		Amount:     domain.MustMoney(100, domain.USD),
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if b.Balance("alice").Amount != 900 {
		t.Fatalf("balance after reserve want 900, got %d", b.Balance("alice").Amount)
	}
	if b.Reservations() != 1 {
		t.Fatalf("reservations want 1, got %d", b.Reservations())
	}

	if err := b.Commit(nil, res); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if b.Reservations() != 0 {
		t.Fatalf("reservations after commit want 0, got %d", b.Reservations())
	}
	if b.Balance("alice").Amount != 900 {
		t.Fatalf("balance after commit want 900, got %d", b.Balance("alice").Amount)
	}
}

func TestMockBank_ReserveRollback(t *testing.T) {
	bal := map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(1000, domain.USD),
	}
	b := mock.NewBank("bank-a", bal)

	res, err := b.Reserve(nil, adapter.ReserveRequest{
		TransferID: "t1",
		AccountID:  "alice",
		Amount:     domain.MustMoney(100, domain.USD),
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	if err := b.Rollback(nil, res); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if b.Balance("alice").Amount != 1000 {
		t.Fatalf("balance after rollback want 1000, got %d", b.Balance("alice").Amount)
	}
	if b.Reservations() != 0 {
		t.Fatalf("reservations want 0, got %d", b.Reservations())
	}
}

func TestMockBank_InsufficientBankBalance(t *testing.T) {
	bal := map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(50, domain.USD),
	}
	b := mock.NewBank("bank-a", bal)

	_, err := b.Reserve(nil, adapter.ReserveRequest{
		TransferID: "t1",
		AccountID:  "alice",
		Amount:     domain.MustMoney(100, domain.USD),
	})
	if !errors.Is(err, adapter.ErrBankRejected) {
		t.Fatalf("want ErrBankRejected, got %v", err)
	}
}
