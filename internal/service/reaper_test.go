package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

func TestReaper_DeletesStaleInProgress(t *testing.T) {
	pool := newPool(t)
	truncate(t, pool)
	db := &store.DB{Pool: pool}
	ctx := context.Background()

	// Insert a stale IN_PROGRESS row directly (created 10 minutes ago).
	_, err := pool.Exec(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, status, created_at, expires_at)
		VALUES ('stale-key', 'hash', 'IN_PROGRESS', NOW() - INTERVAL '10 minutes', NOW() + INTERVAL '1 hour')
	`)
	if err != nil {
		t.Fatalf("insert stale: %v", err)
	}

	// Insert a fresh IN_PROGRESS row (created now).
	_, err = pool.Exec(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, status, created_at, expires_at)
		VALUES ('fresh-key', 'hash', 'IN_PROGRESS', NOW(), NOW() + INTERVAL '1 hour')
	`)
	if err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	// Run reaper once with a short TTL.
	cfg := service.ReaperConfig{Interval: time.Second, InProgressTTL: 5 * time.Minute}
	r := service.NewReaper(db, cfg, nil)

	// Trigger one reap by running with a cancelled-after-one-tick context.
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	// Run will call reapOnce immediately, then wait on ticker.
	go r.Run(shortCtx)
	time.Sleep(50 * time.Millisecond) // give it a moment
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Stale should be gone.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM idempotency_keys WHERE key = 'stale-key'`,
	).Scan(&count); err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale key should be deleted, got %d", count)
	}

	// Fresh should remain.
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM idempotency_keys WHERE key = 'fresh-key'`,
	).Scan(&count); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if count != 1 {
		t.Fatalf("fresh key should remain, got %d", count)
	}
}

// silence unused imports if the compiler complains during editing
var _ = pgx.ErrNoRows
