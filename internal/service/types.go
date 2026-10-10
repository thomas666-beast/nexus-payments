package service

import (
	"errors"
	// "time"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

// ExecuteRequest is the input to Service.Execute.
//
// Note: only FromAccount, ToAccount, and Amount are required.
// IdempotencyKey is optional for now — Task 7 wires it to the HTTP layer.
type ExecuteRequest struct {
	FromAccount domain.AccountID
	ToAccount   domain.AccountID
	Amount      domain.Money
	Reason      string // free-form note, stored on the transfer
}

// ExecuteResult is the outcome of a successful Execute.
type ExecuteResult struct {
	Transfer domain.Transfer
	Entries  []domain.LedgerEntry
}

var (
	ErrInvalidRequest       = errors.New("invalid transfer request")
	ErrRetriesExhausted     = errors.New("retries exhausted")
	ErrSerializationFailure = errors.New("serialization failure")
)
