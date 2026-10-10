package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// IdempotencyRecord is a stored request/response pair.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	ResponseBody   []byte
	ResponseStatus int
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

type IdempotencyRepo struct {
	db *DB
}

func NewIdempotencyRepo(db *DB) *IdempotencyRepo { return &IdempotencyRepo{db: db} }

// Get fetches a record. Returns ErrNotFound if absent or expired.
func (r *IdempotencyRepo) Get(ctx context.Context, tx pgx.Tx, key string) (IdempotencyRecord, error) {
	row := tx.QueryRow(ctx, `
		SELECT key, request_hash, response_body, response_status, created_at, expires_at
		FROM idempotency_keys WHERE key = $1 AND expires_at > NOW()
	`, key)
	var rec IdempotencyRecord
	err := row.Scan(&rec.Key, &rec.RequestHash, &rec.ResponseBody, &rec.ResponseStatus, &rec.CreatedAt, &rec.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdempotencyRecord{}, ErrNotFound
		}
		return IdempotencyRecord{}, fmt.Errorf("scan idempotency: %w", err)
	}
	return rec, nil
}

// Put stores a record. On conflict, does nothing (caller must Get first).
func (r *IdempotencyRepo) Put(ctx context.Context, tx pgx.Tx, rec IdempotencyRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, response_body, response_status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (key) DO NOTHING
	`, rec.Key, rec.RequestHash, rec.ResponseBody, rec.ResponseStatus, rec.CreatedAt, rec.ExpiresAt)
	if err != nil {
		return fmt.Errorf("put idempotency: %w", err)
	}
	return nil
}

// DeleteExpired removes expired records. Call periodically.
func (r *IdempotencyRepo) DeleteExpired(ctx context.Context, tx pgx.Tx) (int64, error) {
	ct, err := tx.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return ct.RowsAffected(), nil
}
