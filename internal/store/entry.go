package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

type EntryRepo struct {
	db *DB
}

func NewEntryRepo(db *DB) *EntryRepo { return &EntryRepo{db: db} }

// Insert writes entries. There is deliberately no Update or Delete —
// the database trigger would reject them anyway, but we don't even
// expose the methods, so callers can't write the code.
func (r *EntryRepo) Insert(ctx context.Context, tx pgx.Tx, entries []domain.LedgerEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("no entries to insert")
	}
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
	}
	batch := &pgx.Batch{}
	for _, e := range entries {
		batch.Queue(`
			INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount_minor, currency, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, e.ID, e.TransferID, e.AccountID, string(e.Direction), e.Amount.Amount, string(e.Amount.Currency), e.CreatedAt)
	}
	br := tx.SendBatch(ctx, batch)
	defer br.Close()
	for i := range entries {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert entry %d: %w", i, err)
		}
	}
	return nil
}

// ListByTransfer returns all entries for a transfer, ordered deterministically.
func (r *EntryRepo) ListByTransfer(ctx context.Context, tx pgx.Tx, transferID domain.TransferID) ([]domain.LedgerEntry, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, transfer_id, account_id, direction, amount_minor, currency, created_at
		FROM ledger_entries WHERE transfer_id = $1
		ORDER BY direction DESC, id
	`, transferID)
	if err != nil {
		return nil, fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	var out []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var currency, direction string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.AccountID, &direction, &e.Amount.Amount, &currency, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		e.Direction = domain.Direction(direction)
		e.Amount.Currency = domain.Currency(currency)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Balance computes the account balance as SUM(amount_minor).
// Returns a Money value in the account's currency.
func (r *EntryRepo) Balance(ctx context.Context, tx pgx.Tx, accountID domain.AccountID) (domain.Money, error) {
	row := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_minor), 0), COALESCE(MAX(currency), '')
		FROM ledger_entries WHERE account_id = $1
	`, accountID)
	var amount int64
	var currency string
	if err := row.Scan(&amount, &currency); err != nil {
		return domain.Money{}, fmt.Errorf("balance: %w", err)
	}
	if currency == "" {
		// No entries: look up the account's currency for a zero balance.
		var acctCurrency string
		if err := tx.QueryRow(ctx, `SELECT currency FROM accounts WHERE id = $1`, accountID).Scan(&acctCurrency); err != nil {
			return domain.Money{}, fmt.Errorf("balance: account currency: %w", err)
		}
		currency = acctCurrency
	}
	return domain.Money{Amount: amount, Currency: domain.Currency(currency)}, nil
}
