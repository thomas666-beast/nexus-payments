package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

type TransferRepo struct {
	db *DB
}

func NewTransferRepo(db *DB) *TransferRepo { return &TransferRepo{db: db} }

// Insert creates a new transfer row (status INITIATED).
func (r *TransferRepo) Insert(ctx context.Context, tx pgx.Tx, t domain.Transfer) error {
	if err := t.Validate(); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO transfers (id, from_account, to_account, amount_minor, currency, status, reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		t.ID, t.FromAccount, t.ToAccount, t.Amount.Amount, string(t.Amount.Currency),
		string(t.Status), t.Reason, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: transfer %s", ErrConflict, t.ID)
		}
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

// GetByID fetches a transfer.
func (r *TransferRepo) GetByID(ctx context.Context, tx pgx.Tx, id domain.TransferID) (domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, from_account, to_account, amount_minor, currency, status, reason, created_at, updated_at
		FROM transfers WHERE id = $1
	`, id)
	return scanTransfer(row)
}

// TransitionStatus moves a transfer from one status to another.
// It enforces the state machine at the database level via a WHERE clause.
func (r *TransferRepo) TransitionStatus(
	ctx context.Context,
	tx pgx.Tx,
	id domain.TransferID,
	from, to domain.TransferStatus,
	reason string,
) error {
	if !domain.CanTransitionTo(from, to) {
		return fmt.Errorf("illegal transition %s -> %s", from, to)
	}
	ct, err := tx.Exec(ctx, `
		UPDATE transfers
		SET status = $1, reason = $2, updated_at = $3
		WHERE id = $4 AND status = $5
	`, string(to), reason, time.Now().UTC(), id, string(from))
	if err != nil {
		return fmt.Errorf("transition transfer: %w", err)
	}
	if ct.RowsAffected() == 0 {
		// Either the transfer doesn't exist, or it wasn't in the expected
		// from-state. Both are conflicts.
		return fmt.Errorf("%w: transfer %s not in state %s", ErrConflict, id, from)
	}
	return nil
}

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer
	var currency, status string
	err := row.Scan(&t.ID, &t.FromAccount, &t.ToAccount, &t.Amount.Amount, &currency, &status, &t.Reason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Transfer{}, ErrNotFound
		}
		return domain.Transfer{}, fmt.Errorf("scan transfer: %w", err)
	}
	t.Amount.Currency = domain.Currency(currency)
	t.Status = domain.TransferStatus(status)
	return t, nil
}
