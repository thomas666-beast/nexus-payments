package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

// AccountRepo reads and writes accounts.
type AccountRepo struct {
	db *DB
}

func NewAccountRepo(db *DB) *AccountRepo { return &AccountRepo{db: db} }

// Insert creates a new account. Returns ErrConflict on unique violation.
func (r *AccountRepo) Insert(ctx context.Context, tx pgx.Tx, a domain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, bank_id, external_ref, currency, holder, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		a.ID, a.BankID, a.ExternalRef, string(a.Currency), a.Holder,
		string(a.Status), a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: account %s", ErrConflict, a.ID)
		}
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
}

// GetByID fetches an account. Returns ErrNotFound if absent.
func (r *AccountRepo) GetByID(ctx context.Context, tx pgx.Tx, id domain.AccountID) (domain.Account, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, bank_id, external_ref, currency, holder, status, created_at, updated_at
		FROM accounts WHERE id = $1
	`, id)
	return scanAccount(row)
}

// GetByBankRef fetches by the bank's external reference.
func (r *AccountRepo) GetByBankRef(ctx context.Context, tx pgx.Tx, bankID, externalRef string) (domain.Account, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, bank_id, external_ref, currency, holder, status, created_at, updated_at
		FROM accounts WHERE bank_id = $1 AND external_ref = $2
	`, bankID, externalRef)
	return scanAccount(row)
}

// LockForUpdate locks the account row until the transaction ends.
func (r *AccountRepo) LockForUpdate(ctx context.Context, tx pgx.Tx, id domain.AccountID) (domain.Account, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, bank_id, external_ref, currency, holder, status, created_at, updated_at
		FROM accounts WHERE id = $1 FOR UPDATE
	`, id)
	return scanAccount(row)
}

// LockAdvisory takes a transaction-scoped advisory lock on the account ID.
//
// This is the authoritative serializer for the "read balance, then insert
// ledger entries" pattern. A row-level FOR UPDATE on accounts is insufficient
// because inserting new ledger_entries rows isn't covered by any existing
// accounts row lock. The advisory lock guarantees that no two transactions
// can concurrently read the same account's balance and then write entries
// for it.
//
// The lock is released automatically when the transaction commits or rolls back.
func (r *AccountRepo) LockAdvisory(ctx context.Context, tx pgx.Tx, id domain.AccountID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, string(id))
	if err != nil {
		return fmt.Errorf("advisory lock %s: %w", id, err)
	}
	return nil
}

// UpdateStatus changes the account status and bumps updated_at.
func (r *AccountRepo) UpdateStatus(ctx context.Context, tx pgx.Tx, id domain.AccountID, status domain.AccountStatus) error {
	ct, err := tx.Exec(ctx, `
		UPDATE accounts SET status = $1, updated_at = $2 WHERE id = $3
	`, string(status), time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update account status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanAccount(row pgx.Row) (domain.Account, error) {
	var a domain.Account
	var currency, status string
	err := row.Scan(&a.ID, &a.BankID, &a.ExternalRef, &currency, &a.Holder, &status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Account{}, ErrNotFound
		}
		return domain.Account{}, fmt.Errorf("scan account: %w", err)
	}
	a.Currency = domain.Currency(currency)
	a.Status = domain.AccountStatus(status)
	return a, nil
}
