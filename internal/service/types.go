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
	FromAccount    domain.AccountID
	ToAccount      domain.AccountID
	Amount         domain.Money
	Reason         string
	IdempotencyKey string // optional; empty means no idempotency
}

// ExecuteResult is the outcome of a successful Execute.
type ExecuteResult struct {
	Transfer domain.Transfer     `json:"transfer"`
	Entries  []domain.LedgerEntry `json:"entries"`
}

var (
	ErrInvalidRequest         = errors.New("invalid transfer request")
	ErrRetriesExhausted       = errors.New("retries exhausted")
	ErrSerializationFailure   = errors.New("serialization failure")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrIdempotencyInProgress  = errors.New("idempotency key is currently in progress")
	ErrIdempotencyKeyRequired = errors.New("idempotency key required for this operation")
)
