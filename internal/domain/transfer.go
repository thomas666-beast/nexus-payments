package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// TransferID is a stable, opaque identifier.
type TransferID string

func NewTransferID() TransferID { return TransferID(uuid.NewString()) }

// TransferStatus is the lifecycle of a transfer.
//
//	INITIATED ─► VALIDATED ─► SETTLED
//	    │            │
//	    └────────────┴──────► FAILED
//	                         (terminal)
type TransferStatus string

const (
	TransferStatusInitiated TransferStatus = "INITIATED"
	TransferStatusValidated TransferStatus = "VALIDATED"
	TransferStatusSettled   TransferStatus = "SETTLED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// allowedTransitions encodes the legal state machine edges.
// Anything not in this map is illegal.
var allowedTransitions = map[TransferStatus]map[TransferStatus]struct{}{
	TransferStatusInitiated: {
		TransferStatusValidated: {},
		TransferStatusFailed:    {},
	},
	TransferStatusValidated: {
		TransferStatusSettled: {},
		TransferStatusFailed:  {},
	},
	TransferStatusSettled: {}, // terminal
	TransferStatusFailed:  {}, // terminal
}

// CanTransitionTo reports whether from → to is a legal transition.
func CanTransitionTo(from, to TransferStatus) bool {
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = targets[to]
	return ok
}

// IsTerminal reports whether no further transitions are possible.
func (s TransferStatus) IsTerminal() bool {
	return s == TransferStatusSettled || s == TransferStatusFailed
}

// Transfer represents a single movement of money between two accounts.
// It is a pure domain object. Persistence lives in internal/store.
type Transfer struct {
	ID          TransferID
	FromAccount AccountID
	ToAccount   AccountID
	Amount      Money
	Status      TransferStatus
	Reason      string    // failure reason when Status == FAILED
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate checks structural invariants.
func (t Transfer) Validate() error {
	if t.ID == "" {
		return errors.New("transfer id required")
	}
	if t.FromAccount == "" || t.ToAccount == "" {
		return errors.New("from and to accounts required")
	}
	if t.FromAccount == t.ToAccount {
		return errors.New("cannot transfer to self")
	}
	if err := t.Amount.Currency.Validate(); err != nil {
		return err
	}
	if !t.Amount.IsPositive() {
		return errors.New("amount must be positive")
	}
	switch t.Status {
	case TransferStatusInitiated, TransferStatusValidated,
		TransferStatusSettled, TransferStatusFailed:
	default:
		return errors.New("invalid transfer status")
	}
	return nil
}
