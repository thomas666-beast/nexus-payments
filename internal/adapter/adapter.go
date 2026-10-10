package adapter

import (
	"context"
	"errors"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

var (
	ErrBankUnavailable  = errors.New("bank adapter unavailable")
	ErrBankRejected     = errors.New("bank rejected reservation")
	ErrReservationStale = errors.New("reservation not found or already settled")
	ErrUnknownBank      = errors.New("unknown bank id")
)

// Reservation is proof that a bank is holding funds for a transfer.
// It is opaque to us — only the bank that issued it can act on it.
type Reservation struct {
	ID        string
	BankID    string
	TransferID domain.TransferID
	AccountID domain.AccountID
	Amount    domain.Money
	ExpiresAt time.Time
}

// Adapter is the contract every participating bank implements.
//
// Implementations must be safe for concurrent use.
//
// All methods must:
//   - respect ctx cancellation
//   - be idempotent when called with the same request ID
//   - return errors wrapping one of the sentinel errors above when applicable
type Adapter interface {
	// BankID returns the identifier this adapter serves.
	BankID() string

	// Reserve asks the bank to hold funds on the account for this transfer.
	// On success, returns a Reservation the caller must eventually Commit
	// or Rollback.
	Reserve(ctx context.Context, req ReserveRequest) (Reservation, error)

	// Commit confirms that the transfer is final. After Commit, the
	// reservation is gone and the funds have moved.
	Commit(ctx context.Context, res Reservation) error

	// Rollback releases a reservation. Called when the transfer fails
	// after Reserve but before Commit. Safe to call on an already-released
	// reservation (returns nil).
	Rollback(ctx context.Context, res Reservation) error

	// Reconcile returns the bank's settled transfer IDs in the given time
	// window. Used by the reconciliation loop (Task 12) to detect drift.
	Reconcile(ctx context.Context, from, to time.Time) ([]domain.TransferID, error)
}

// ReserveRequest is the input to Reserve.
type ReserveRequest struct {
	TransferID domain.TransferID
	AccountID  domain.AccountID
	Amount     domain.Money
	// ExpiresAt is the deadline for the reservation. If the caller doesn't
	// Commit or Rollback before this, the bank may auto-release the funds.
	ExpiresAt time.Time
}
