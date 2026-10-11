package service

import (
	"errors"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

type ExecuteRequest struct {
	FromAccount    domain.AccountID
	ToAccount      domain.AccountID
	Amount         domain.Money
	Reason         string
	IdempotencyKey string
}

type ExecuteResult struct {
	Transfer domain.Transfer
	Entries  []domain.LedgerEntry
}

var (
	ErrInvalidRequest         = errors.New("invalid transfer request")
	ErrRetriesExhausted       = errors.New("retries exhausted")
	ErrSerializationFailure   = errors.New("serialization failure")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrIdempotencyInProgress  = errors.New("idempotency key is currently in progress")
	ErrIdempotencyKeyRequired = errors.New("idempotency key required for this operation")
)
