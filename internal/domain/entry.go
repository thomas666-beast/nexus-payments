package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// EntryID is a stable, opaque identifier for a single ledger entry.
type EntryID string

func NewEntryID() EntryID { return EntryID(uuid.NewString()) }

// Direction describes the effect of an entry on an account's balance.
//
//	DEBIT  -> negative amount (balance decreases)
//	CREDIT -> positive amount (balance increases)
type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

func (d Direction) Validate() error {
	switch d {
	case DirectionDebit, DirectionCredit:
		return nil
	default:
		return ErrInvalidDirection
	}
}

var ErrInvalidDirection = errors.New("invalid direction")

// LedgerEntry is one half of a double-entry transaction.
// It is immutable once written. Corrections are made by writing a
// compensating entry, never by updating or deleting.
type LedgerEntry struct {
	ID         EntryID
	TransferID TransferID    // groups the two halves of a single transfer
	AccountID  AccountID
	Direction  Direction
	Amount     Money         // signed: negative for DEBIT, positive for CREDIT
	CreatedAt  time.Time
}

// Validate checks structural invariants of a single entry.
// It does NOT check that the parent transfer balances — that is the
// ledger engine's job (see engine.go).
func (e LedgerEntry) Validate() error {
	if e.ID == "" {
		return errors.New("entry id required")
	}
	if e.TransferID == "" {
		return errors.New("transfer id required")
	}
	if e.AccountID == "" {
		return errors.New("account id required")
	}
	if err := e.Direction.Validate(); err != nil {
		return err
	}
	if err := e.Amount.Currency.Validate(); err != nil {
		return err
	}
	if e.Amount.IsZero() {
		return errors.New("entry amount must be non-zero")
	}
	switch e.Direction {
	case DirectionDebit:
		if !e.Amount.IsNegative() {
			return errors.New("DEBIT entry must have negative amount")
		}
	case DirectionCredit:
		if !e.Amount.IsPositive() {
			return errors.New("CREDIT entry must have positive amount")
		}
	}
	return nil
}
