package virtualbank

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrInsufficient    = errors.New("insufficient balance")
	ErrCurrencyMatch   = errors.New("currency mismatch")
	ErrReservationGone = errors.New("reservation not found or already finalized")
)

// Service is the bank's internal ledger engine. It is intentionally
// independent from nexus-payments — a real bank would have its own stack.
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

// Reservation is the bank's record of a held amount.
type Reservation struct {
	ID         string
	TransferID string
	AccountID  string
	Amount     int64
	Currency   string
	ExpiresAt  time.Time
}

// CreateAccount creates a bank-side account with the given opening balance.
func (s *Service) CreateAccount(ctx context.Context, accountID, currency string, opening int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bank_accounts (id, currency, balance_minor, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, accountID, currency, opening)
	return err
}

// Reserve holds funds from an account and returns a reservation.
func (s *Service) Reserve(ctx context.Context, transferID, accountID string, amount int64, currency string, expiresAt time.Time) (Reservation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback(ctx)

	// Lock account row.
	var balance int64
	var acctCurrency string
	err = tx.QueryRow(ctx, `
		SELECT balance_minor, currency FROM bank_accounts WHERE id = $1 FOR UPDATE
	`, accountID).Scan(&balance, &acctCurrency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reservation{}, ErrAccountNotFound
		}
		return Reservation{}, err
	}
	if acctCurrency != currency {
		return Reservation{}, ErrCurrencyMatch
	}
	if balance < amount {
		return Reservation{}, ErrInsufficient
	}

	// Debit immediately; Commit keeps it debited, Rollback credits it back.
	if _, err := tx.Exec(ctx, `
		UPDATE bank_accounts SET balance_minor = balance_minor - $1, updated_at = NOW() WHERE id = $2
	`, amount, accountID); err != nil {
		return Reservation{}, err
	}

	res := Reservation{
		ID:         uuid.NewString(),
		TransferID: transferID,
		AccountID:  accountID,
		Amount:     amount,
		Currency:   currency,
		ExpiresAt:  expiresAt,
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO bank_reservations (id, transfer_id, account_id, amount_minor, currency, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'HELD', NOW(), $6)
	`, res.ID, res.TransferID, res.AccountID, res.Amount, res.Currency, res.ExpiresAt); err != nil {
		return Reservation{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, err
	}
	return res, nil
}

// Commit finalizes a reservation: funds stay debited, transfer recorded.
func (s *Service) Commit(ctx context.Context, reservationID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status, transferID, accountID, currency string
	var amount int64
	err = tx.QueryRow(ctx, `
		SELECT status, transfer_id, account_id, amount_minor, currency
		FROM bank_reservations WHERE id = $1 FOR UPDATE
	`, reservationID).Scan(&status, &transferID, &accountID, &amount, &currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrReservationGone
		}
		return err
	}
	if status == "COMMITTED" {
		return nil // idempotent
	}
	if status != "HELD" {
		return ErrReservationGone
	}

	if _, err := tx.Exec(ctx, `
		UPDATE bank_reservations SET status = 'COMMITTED' WHERE id = $1
	`, reservationID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO bank_settled_transfers (transfer_id, account_id, amount_minor, currency, settled_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (transfer_id) DO NOTHING
	`, transferID, accountID, amount, currency); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Rollback releases a reservation: funds return to the account.
func (s *Service) Rollback(ctx context.Context, reservationID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status, accountID string
	var amount int64
	err = tx.QueryRow(ctx, `
		SELECT status, account_id, amount_minor
		FROM bank_reservations WHERE id = $1 FOR UPDATE
	`, reservationID).Scan(&status, &accountID, &amount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // already gone or never existed — idempotent
		}
		return err
	}
	if status != "HELD" {
		return nil // already finalized — idempotent
	}

	if _, err := tx.Exec(ctx, `
		UPDATE bank_accounts SET balance_minor = balance_minor + $1, updated_at = NOW() WHERE id = $2
	`, amount, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE bank_reservations SET status = 'ROLLED_BACK' WHERE id = $1
	`, reservationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Reconcile returns settled transfer IDs in the window.
func (s *Service) Reconcile(ctx context.Context, from, to time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT transfer_id FROM bank_settled_transfers
		WHERE settled_at >= $1 AND settled_at < $2
		ORDER BY settled_at
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Balance returns the current balance of an account.
func (s *Service) Balance(ctx context.Context, accountID string) (int64, string, error) {
	var balance int64
	var currency string
	err := s.pool.QueryRow(ctx, `
		SELECT balance_minor, currency FROM bank_accounts WHERE id = $1
	`, accountID).Scan(&balance, &currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", ErrAccountNotFound
		}
		return 0, "", err
	}
	return balance, currency, nil
}

var _ = fmt.Sprintf
