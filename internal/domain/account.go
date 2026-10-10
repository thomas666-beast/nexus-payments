package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidAccount = errors.New("invalid account")

// AccountID is a stable, opaque identifier. Never reuse.
type AccountID string

// NewAccountID returns a fresh ID.
func NewAccountID() AccountID { return AccountID(uuid.NewString()) }

// Account is a virtual bank account in the nexus ledger.
// Balances are NOT stored here — they are derived from ledger entries.
type Account struct {
	ID           AccountID
	BankID       string    // which participating bank owns this account
	ExternalRef  string    // the bank's own identifier for the customer
	Currency     Currency
	Holder       string    // display name for now; KYC comes later
	Status       AccountStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AccountStatus is the lifecycle of an account.
type AccountStatus string

const (
	AccountStatusActive  AccountStatus = "ACTIVE"
	AccountStatusFrozen  AccountStatus = "FROZEN"
	AccountStatusClosed  AccountStatus = "CLOSED"
)

// Validate checks structural invariants. Does not check business rules.
func (a Account) Validate() error {
	if a.ID == "" {
		return errors.New("account id required")
	}
	if a.BankID == "" {
		return errors.New("bank id required")
	}
	if a.ExternalRef == "" {
		return errors.New("external ref required")
	}
	if err := a.Currency.Validate(); err != nil {
		return err
	}
	if a.Holder == "" {
		return errors.New("holder required")
	}
	switch a.Status {
	case AccountStatusActive, AccountStatusFrozen, AccountStatusClosed:
	default:
		return errors.New("invalid account status")
	}
	return nil
}

// CanDebit reports whether funds may leave this account.
func (a Account) CanDebit() bool { return a.Status == AccountStatusActive }

// CanCredit reports whether funds may enter this account.
func (a Account) CanCredit() bool { return a.Status == AccountStatusActive }
