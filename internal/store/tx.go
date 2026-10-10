package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TxFunc is the work performed inside a transaction. Return nil to commit;
// return any error to roll back.
type TxFunc func(ctx context.Context, tx pgx.Tx) error

// WithTx runs fn inside a serializable transaction. Uses the strongest
// isolation level because money demands it.
//
// Serializable isolation means Postgres will abort the transaction if it
// can't guarantee serializability. Callers may retry on 40001.
func (db *DB) WithTx(ctx context.Context, fn TxFunc) error {
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.Serializable,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// IsRetryable reports whether an error is a transient serialization failure.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	// pgx exposes SQLSTATE via pgconn.PgError.
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		// 40001 serialization_failure
		// 40P01 deadlock_detected
		return pgErr.SQLState() == "40001" || pgErr.SQLState() == "40P01"
	}
	return false
}
