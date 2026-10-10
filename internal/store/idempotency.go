package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// IdempotencyStatus is the lifecycle of a stored request.
type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// IdempotencyRecord is a stored request/response pair.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	Status         IdempotencyStatus
	ResponseBody   []byte // nil while IN_PROGRESS
	ResponseStatus *int   // nil while IN_PROGRESS
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

type IdempotencyRepo struct {
	db *DB
}

func NewIdempotencyRepo(db *DB) *IdempotencyRepo { return &IdempotencyRepo{db: db} }

// TryAcquire attempts to insert a new IN_PROGRESS record for the key.
//
// Returns:
//   - (record, false, nil) if the key already existed. Caller must inspect
//     record.Status to decide what to do.
//   - (zero, true, nil) if acquisition succeeded. Caller now owns the key.
//   - (zero, false, err) on infrastructure error.
//
// The caller must eventually call Complete or Fail to release the key.
func (r *IdempotencyRepo) TryAcquire(
	ctx context.Context,
	tx pgx.Tx,
	key, requestHash string,
	now, expiresAt time.Time,
) (IdempotencyRecord, bool, error) {
	// Attempt insert-first. On conflict, SELECT the existing row.
	ct, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, status, created_at, expires_at)
		VALUES ($1, $2, 'IN_PROGRESS', $3, $4)
		ON CONFLICT (key) DO NOTHING
	`, key, requestHash, now, expiresAt)
	if err != nil {
		return IdempotencyRecord{}, false, fmt.Errorf("acquire idempotency: %w", err)
	}
	if ct.RowsAffected() == 1 {
		// We own the key.
		return IdempotencyRecord{
			Key:         key,
			RequestHash: requestHash,
			Status:      IdempotencyStatusInProgress,
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		}, true, nil
	}

	// Key already exists — fetch it.
	rec, err := r.get(ctx, tx, key)
	if err != nil {
		return IdempotencyRecord{}, false, err
	}
	return rec, false, nil
}

// get fetches a record by key, ignoring expiry (callers handle staleness).
func (r *IdempotencyRepo) get(ctx context.Context, tx pgx.Tx, key string) (IdempotencyRecord, error) {
	row := tx.QueryRow(ctx, `
		SELECT key, request_hash, status, response_body, response_status, created_at, expires_at
		FROM idempotency_keys WHERE key = $1
	`, key)
	var rec IdempotencyRecord
	var status string
	err := row.Scan(&rec.Key, &rec.RequestHash, &status, &rec.ResponseBody, &rec.ResponseStatus, &rec.CreatedAt, &rec.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdempotencyRecord{}, ErrNotFound
		}
		return IdempotencyRecord{}, fmt.Errorf("scan idempotency: %w", err)
	}
	rec.Status = IdempotencyStatus(status)
	return rec, nil
}

// Complete marks a record as COMPLETED with the stored response.
func (r *IdempotencyRepo) Complete(
	ctx context.Context,
	tx pgx.Tx,
	key string,
	responseBody []byte,
	responseStatus int,
) error {
	ct, err := tx.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'COMPLETED', response_body = $1, response_status = $2
		WHERE key = $3 AND status = 'IN_PROGRESS'
	`, responseBody, responseStatus, key)
	if err != nil {
		return fmt.Errorf("complete idempotency: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w: key %s not in IN_PROGRESS", ErrConflict, key)
	}
	return nil
}

// Fail marks a record as FAILED with the stored error response.
func (r *IdempotencyRepo) Fail(
	ctx context.Context,
	tx pgx.Tx,
	key string,
	responseBody []byte,
	responseStatus int,
) error {
	ct, err := tx.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'FAILED', response_body = $1, response_status = $2
		WHERE key = $3 AND status = 'IN_PROGRESS'
	`, responseBody, responseStatus, key)
	if err != nil {
		return fmt.Errorf("fail idempotency: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w: key %s not in IN_PROGRESS", ErrConflict, key)
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

// DeleteStaleInProgress removes IN_PROGRESS records older than olderThan.
// Returns the number of rows deleted.
func (r *IdempotencyRepo) DeleteStaleInProgress(ctx context.Context, tx pgx.Tx, olderThan time.Duration) (int64, error) {
	ct, err := tx.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE status = 'IN_PROGRESS' AND created_at < NOW() - $1::interval
	`, fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("delete stale in-progress: %w", err)
	}
	return ct.RowsAffected(), nil
}
