package mock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

var errInjected = errors.New("injected failure")

// Bank is a fully in-memory implementation of adapter.Adapter.
type Bank struct {
	id string

	mu           sync.Mutex
	balances     map[domain.AccountID]domain.Money
	reservations map[string]adapter.Reservation
	settled      map[domain.TransferID]bool

	nextReserveErr error
	nextCommitErr  error
}

// NewBank creates an in-memory bank with the given ID and starting balances.
func NewBank(id string, balances map[domain.AccountID]domain.Money) *Bank {
	cp := make(map[domain.AccountID]domain.Money, len(balances))
	for k, v := range balances {
		cp[k] = v
	}
	return &Bank{
		id:           id,
		balances:     cp,
		reservations: make(map[string]adapter.Reservation),
		settled:      make(map[domain.TransferID]bool),
	}
}

func (b *Bank) BankID() string { return b.id }

func (b *Bank) Reserve(ctx context.Context, req adapter.ReserveRequest) (adapter.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return adapter.Reservation{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.nextReserveErr != nil {
		err := b.nextReserveErr
		b.nextReserveErr = nil
		return adapter.Reservation{}, err
	}

	bal, ok := b.balances[req.AccountID]
	if !ok {
		return adapter.Reservation{}, fmt.Errorf("%w: unknown account %s", adapter.ErrBankRejected, req.AccountID)
	}
	if bal.Currency != req.Amount.Currency {
		return adapter.Reservation{}, fmt.Errorf("%w: currency mismatch", adapter.ErrBankRejected)
	}
	if bal.Amount < req.Amount.Amount {
		return adapter.Reservation{}, fmt.Errorf("%w: insufficient bank balance", adapter.ErrBankRejected)
	}

	b.balances[req.AccountID] = domain.Money{
		Amount:   bal.Amount - req.Amount.Amount,
		Currency: bal.Currency,
	}

	res := adapter.Reservation{
		ID:         uuid.NewString(),
		BankID:     b.id,
		TransferID: req.TransferID,
		AccountID:  req.AccountID,
		Amount:     req.Amount,
		ExpiresAt:  req.ExpiresAt,
	}
	b.reservations[res.ID] = res
	return res, nil
}

func (b *Bank) Commit(ctx context.Context, res adapter.Reservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.nextCommitErr != nil {
		err := b.nextCommitErr
		b.nextCommitErr = nil
		return err
	}

	if _, ok := b.reservations[res.ID]; !ok {
		if b.settled[res.TransferID] {
			return nil
		}
		return fmt.Errorf("%w: %s", adapter.ErrReservationStale, res.ID)
	}
	delete(b.reservations, res.ID)
	b.settled[res.TransferID] = true
	return nil
}

func (b *Bank) Rollback(ctx context.Context, res adapter.Reservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	stored, ok := b.reservations[res.ID]
	if !ok {
		return nil
	}

	bal, ok := b.balances[stored.AccountID]
	if !ok {
		return fmt.Errorf("rollback: account %s vanished", stored.AccountID)
	}
	b.balances[stored.AccountID] = domain.Money{
		Amount:   bal.Amount + stored.Amount.Amount,
		Currency: bal.Currency,
	}
	delete(b.reservations, res.ID)
	return nil
}

func (b *Bank) Reconcile(ctx context.Context, from, to time.Time) ([]domain.TransferID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	ids := make([]domain.TransferID, 0, len(b.settled))
	for id := range b.settled {
		ids = append(ids, id)
	}
	return ids, nil
}

// Balance exposes the current bank-side balance. Test-only helper.
func (b *Bank) Balance(account domain.AccountID) domain.Money {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.balances[account]
}

// Reservations returns the count of currently held reservations. Test-only.
func (b *Bank) Reservations() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.reservations)
}

// FailNextReserve makes the next Reserve call return the given error.
func (b *Bank) FailNextReserve(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextReserveErr = err
}

// FailNextCommit makes the next Commit call return the given error.
func (b *Bank) FailNextCommit(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextCommitErr = err
}
