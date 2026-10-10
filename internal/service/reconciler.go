package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/platform/metrics"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

// ReconcilerConfig controls reconciliation timing.
type ReconcilerConfig struct {
	Interval time.Duration // how often to run
	Window   time.Duration // how far back to compare each run
}

// DefaultReconcilerConfig returns production-sane defaults.
func DefaultReconcilerConfig() ReconcilerConfig {
	return ReconcilerConfig{
		Interval: 10 * time.Minute,
		Window:   time.Hour,
	}
}

// Reconciler periodically compares our ledger against each bank's view.
type Reconciler struct {
	db       *store.DB
	registry *adapter.Registry
	cfg      ReconcilerConfig
	metrics  *metrics.Metrics
	log      *slog.Logger
	now      func() time.Time
}

func NewReconciler(db *store.DB, registry *adapter.Registry, cfg ReconcilerConfig, m *metrics.Metrics, log *slog.Logger) *Reconciler {
	if log == nil {
		log = slog.Default()
	}
	if m == nil {
		m = metrics.NewNoop()
	}
	return &Reconciler{
		db:       db,
		registry: registry,
		cfg:      cfg,
		metrics:  m,
		log:      log,
		now:      time.Now,
	}
}

// Run blocks until ctx is cancelled, running the reconcile loop.
func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	r.ReconcileOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("reconciler stopping")
			return
		case <-ticker.C:
			r.ReconcileOnce(ctx)
		}
	}
}

// ReconcileOnce runs one reconciliation pass across all registered banks.
// Exported so tests and admin tools can trigger it on demand.
func (r *Reconciler) ReconcileOnce(ctx context.Context) {
	to := r.now().UTC()
	from := to.Add(-r.cfg.Window)

	ours, err := r.ourSettled(ctx, from, to)
	if err != nil {
		r.log.Error("reconciler: fetch our settled failed", "err", err)
		return
	}

	for _, bankID := range r.registry.List() {
		r.reconcileBank(ctx, bankID, from, to, ours)
	}
}

// ourSettled reads our settled transfers in the window.
func (r *Reconciler) ourSettled(ctx context.Context, from, to time.Time) (map[domain.TransferID]struct{}, error) {
	repo := store.NewTransferRepo(r.db)
	var ids []domain.TransferID
	err := r.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var innerErr error
		ids, innerErr = repo.SettledSince(ctx, tx, from, to)
		return innerErr
	})
	if err != nil {
		return nil, err
	}
	set := make(map[domain.TransferID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

// reconcileBank compares one bank's view against ours.
func (r *Reconciler) reconcileBank(
	ctx context.Context,
	bankID string,
	from, to time.Time,
	ours map[domain.TransferID]struct{},
) {
	a, err := r.registry.Get(bankID)
	if err != nil {
		r.log.Error("reconciler: bank adapter not found", "bank", bankID, "err", err)
		return
	}

	theirs, err := a.Reconcile(ctx, from, to)
	if err != nil {
		r.log.Error("reconciler: bank Reconcile failed", "bank", bankID, "err", err)
		return
	}

	theirSet := make(map[domain.TransferID]struct{}, len(theirs))
	for _, id := range theirs {
		theirSet[id] = struct{}{}
	}

	// We think it settled, bank disagrees.
	var missingAtBank []domain.TransferID
	for id := range ours {
		if _, ok := theirSet[id]; !ok {
			missingAtBank = append(missingAtBank, id)
		}
	}

	// Bank thinks it settled, we disagree.
	var missingHere []domain.TransferID
	for id := range theirSet {
		if _, ok := ours[id]; !ok {
			missingHere = append(missingHere, id)
		}
	}

	if len(missingAtBank) > 0 {
		r.metrics.ReconcileDrift.WithLabelValues(bankID, "missing_at_bank").Add(float64(len(missingAtBank)))
		r.log.Warn("reconciler: transfers settled here but missing at bank",
			"bank", bankID,
			"count", len(missingAtBank),
			"ids", missingAtBank,
		)
	}
	if len(missingHere) > 0 {
		r.metrics.ReconcileDrift.WithLabelValues(bankID, "missing_here").Add(float64(len(missingHere)))
		r.log.Warn("reconciler: transfers settled at bank but missing here",
			"bank", bankID,
			"count", len(missingHere),
			"ids", missingHere,
		)
	}

	r.metrics.ReconcileRuns.WithLabelValues(bankID).Inc()
}
