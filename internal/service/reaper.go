package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

// ReaperConfig controls reaper intervals and thresholds.
type ReaperConfig struct {
	Interval      time.Duration // how often to run
	InProgressTTL time.Duration // how old an IN_PROGRESS record must be to delete
	ExpiredTTL    time.Duration // unused; expires_at handles this in DeleteExpired
}

// DefaultReaperConfig returns production-sane defaults.
func DefaultReaperConfig() ReaperConfig {
	return ReaperConfig{
		Interval:      30 * time.Second,
		InProgressTTL: 5 * time.Minute,
	}
}

// Reaper periodically cleans up stale idempotency records.
type Reaper struct {
	db  *store.DB
	cfg ReaperConfig
	log *slog.Logger
}

func NewReaper(db *store.DB, cfg ReaperConfig, log *slog.Logger) *Reaper {
	if log == nil {
		log = slog.Default()
	}
	return &Reaper{db: db, cfg: cfg, log: log}
}

// Run blocks until ctx is cancelled, running the reap loop.
func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	// Run once immediately.
	r.reapOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("reaper stopping")
			return
		case <-ticker.C:
			r.reapOnce(ctx)
		}
	}
}

func (r *Reaper) reapOnce(ctx context.Context) {
	repo := store.NewIdempotencyRepo(r.db)

	// Delete stale IN_PROGRESS.
	var inProgressDeleted int64
	err := r.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var innerErr error
		inProgressDeleted, innerErr = repo.DeleteStaleInProgress(ctx, tx, r.cfg.InProgressTTL)
		return innerErr
	})
	if err != nil {
		r.log.Error("reaper: delete stale in-progress failed", "err", err)
		return
	}

	// Delete expired.
	var expiredDeleted int64
	err = r.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var innerErr error
		expiredDeleted, innerErr = repo.DeleteExpired(ctx, tx)
		return innerErr
	})
	if err != nil {
		r.log.Error("reaper: delete expired failed", "err", err)
		return
	}

	if inProgressDeleted > 0 || expiredDeleted > 0 {
		r.log.Info("reaper: cleaned up",
			"in_progress", inProgressDeleted,
			"expired", expiredDeleted)
	}
}
