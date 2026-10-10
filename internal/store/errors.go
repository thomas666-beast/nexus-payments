package store

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrLedgerImmutable   = errors.New("ledger entries are immutable")
)
