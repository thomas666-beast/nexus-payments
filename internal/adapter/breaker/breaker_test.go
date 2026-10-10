package breaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/adapter/breaker"
	"github.com/thomas666-beast/nexus-payments/internal/adapter/mock"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

var testErr = errors.New("simulated bank failure")

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	bank := mock.NewBank("bank-a", map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(1000, domain.USD),
	})
	s := breaker.DefaultSettings()
	s.Threshold = 3
	s.Timeout = 200 * time.Millisecond
	wrapped := breaker.New(bank, s)

	ctx := context.Background()
	req := adapter.ReserveRequest{
		TransferID: "t1",
		AccountID:  "alice",
		Amount:     domain.MustMoney(100, domain.USD),
	}

	// Cause 3 failures by making the bank reject.
	for i := 0; i < 3; i++ {
		bank.FailNextReserve(testErr)
		_, err := wrapped.Reserve(ctx, req)
		if err == nil {
			t.Fatalf("call %d: expected error", i)
		}
	}

	// 4th call should be blocked by the open breaker.
	_, err := wrapped.Reserve(ctx, req)
	if !errors.Is(err, breaker.ErrCircuitOpen) {
		t.Fatalf("want ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_RecoversAfterCooldown(t *testing.T) {
	bank := mock.NewBank("bank-a", map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(1000, domain.USD),
	})
	s := breaker.DefaultSettings()
	s.Threshold = 2
	s.Timeout = 100 * time.Millisecond
	wrapped := breaker.New(bank, s)

	ctx := context.Background()
	req := adapter.ReserveRequest{
		TransferID: "t1",
		AccountID:  "alice",
		Amount:     domain.MustMoney(100, domain.USD),
	}

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		bank.FailNextReserve(testErr)
		_, _ = wrapped.Reserve(ctx, req)
	}
	if _, err := wrapped.Reserve(ctx, req); !errors.Is(err, breaker.ErrCircuitOpen) {
		t.Fatalf("want open, got %v", err)
	}

	// Wait for cooldown.
	time.Sleep(150 * time.Millisecond)

	// Next call is the HALF_OPEN probe. Bank succeeds → breaker closes.
	if _, err := wrapped.Reserve(ctx, req); err != nil {
		t.Fatalf("probe should succeed, got %v", err)
	}

	// Subsequent calls should pass.
	if _, err := wrapped.Reserve(ctx, req); err != nil {
		t.Fatalf("closed breaker should allow, got %v", err)
	}
}

func TestBreaker_RollbackBypassesBreaker(t *testing.T) {
	bank := mock.NewBank("bank-a", map[domain.AccountID]domain.Money{
		"alice": domain.MustMoney(1000, domain.USD),
	})
	s := breaker.DefaultSettings()
	s.Threshold = 1
	s.Timeout = time.Hour
	wrapped := breaker.New(bank, s)

	ctx := context.Background()

	// First reserve succeeds — grab a reservation.
	res, err := wrapped.Reserve(ctx, adapter.ReserveRequest{
		TransferID: "t1",
		AccountID:  "alice",
		Amount:     domain.MustMoney(100, domain.USD),
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	// Trip the breaker via a Commit failure.
	bank.FailNextCommit(testErr)
	_ = wrapped.Commit(ctx, res)

	// Breaker is now open.
	if _, err := wrapped.Reserve(ctx, adapter.ReserveRequest{
		TransferID: "t2",
		AccountID:  "alice",
		Amount:     domain.MustMoney(50, domain.USD),
	}); !errors.Is(err, breaker.ErrCircuitOpen) {
		t.Fatalf("want open, got %v", err)
	}

	// Rollback must still work despite open breaker.
	if err := wrapped.Rollback(ctx, res); err != nil {
		t.Fatalf("rollback should bypass breaker, got %v", err)
	}

	// Balance restored.
	if got := bank.Balance("alice").Amount; got != 1000 {
		t.Fatalf("balance want 1000, got %d", got)
	}
}
