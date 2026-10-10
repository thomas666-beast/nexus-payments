package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TxFunc is the work performed inside a transaction. Return nil to commit;
// return any error to roll back.
type TxFunc func(ctx context.Context, tx pgx.Tx) error

// WithTx runs fn inside a ReadCommitted transaction. Serializable isolation
// is deliberately NOT used: concurrent transfers on the same account would
// abort each other constantly under SSI. Correctness is enforced instead by
// explicit row locks (SELECT ... FOR UPDATE) acquired in a deterministic
// order — see Service.attempt.
func (db *DB) WithTx(ctx context.Context, fn TxFunc) error {
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
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
		return err
	}
	return nil
}

// IsRetryable reports whether an error is a transient failure worth retrying.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 40001 serialization_failure
		// 40P01 deadlock_detected
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}
