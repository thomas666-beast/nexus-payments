package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/store"
	"github.com/thomas666-beast/nexus-payments/internal/platform/metrics"
)

// Config controls retry behavior and timeouts.
type Config struct {
	MaxRetries      int
	RetryBackoff    time.Duration
	TransferTimeout time.Duration
}

// DefaultConfig returns production-sane defaults.
func DefaultConfig() Config {
	return Config{
		MaxRetries:      10,
		RetryBackoff:    5 * time.Millisecond,
		TransferTimeout: 10 * time.Second,
	}
}

type Service struct {
	db       *store.DB
	engine   *ledger.Engine
	registry *adapter.Registry
	cfg      Config
	now      func() time.Time
	log      *slog.Logger
	metrics  *metrics.Metrics
}

func New(db *store.DB, engine *ledger.Engine, registry *adapter.Registry, cfg Config, log *slog.Logger, m *metrics.Metrics) *Service {
	if log == nil {
		log = slog.Default()
	}
	if registry == nil {
		registry = adapter.NewRegistry()
	}
	if m == nil {
		m = metrics.NewNoop()
	}
	return &Service{
		db:       db,
		engine:   engine,
		registry: registry,
		cfg:      cfg,
		now:      time.Now,
		log:      log,
		metrics:  m,
	}
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	start := time.Now()
	defer func() {
		s.metrics.TransferDuration.Observe(time.Since(start).Seconds())
	}()

	if err := s.validate(req); err != nil {
		s.metrics.TransfersTotal.WithLabelValues("INVALID").Inc()
		s.metrics.TransferErrors.WithLabelValues("invalid_request").Inc()
		return ExecuteResult{}, err
	}

	if req.IdempotencyKey == "" {
		return s.executeAndRecord(ctx, req)
	}

	reqHash, err := hashRequest(req)
	if err != nil {
		s.metrics.TransfersTotal.WithLabelValues("FAILED").Inc()
		s.metrics.TransferErrors.WithLabelValues("hash").Inc()
		return ExecuteResult{}, fmt.Errorf("hash request: %w", err)
	}

	existing, acquired, err := s.acquireKey(ctx, req.IdempotencyKey, reqHash)
	if err != nil {
		s.metrics.TransfersTotal.WithLabelValues("FAILED").Inc()
		s.metrics.TransferErrors.WithLabelValues("acquire_key").Inc()
		return ExecuteResult{}, err
	}
	if !acquired {
		s.metrics.IdempotencyReplays.Inc()
		res, err := s.replayExisting(existing, reqHash)
		if err != nil {
			s.metrics.TransfersTotal.WithLabelValues("REPLAY").Inc()
		}
		return res, err
	}

	res, execErr := s.executeAndRecord(ctx, req)
	if execErr != nil {
		if recordErr := s.failKey(ctx, req.IdempotencyKey, execErr); recordErr != nil {
			s.log.Error("failed to record idempotency failure",
				"key", req.IdempotencyKey, "err", recordErr)
		}
		return ExecuteResult{}, execErr
	}

	if err := s.completeKey(ctx, req.IdempotencyKey, res); err != nil {
		s.log.Error("failed to record idempotency completion",
			"key", req.IdempotencyKey, "err", err)
	}
	return res, nil
}

// executeAndRecord wraps executeOnce with metrics + error classification.
func (s *Service) executeAndRecord(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	res, err := s.executeOnce(ctx, req)
	if err == nil {
		s.metrics.TransfersTotal.WithLabelValues("SETTLED").Inc()
		return res, nil
	}
	code := classifyError(err)
	s.metrics.TransfersTotal.WithLabelValues("FAILED").Inc()
	s.metrics.TransferErrors.WithLabelValues(code).Inc()
	return ExecuteResult{}, err
}

// classifyError returns a small set of canonical categories for metrics.
func classifyError(err error) string {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, ErrIdempotencyConflict):
		return "idempotency_conflict"
	case errors.Is(err, ErrIdempotencyInProgress):
		return "idempotency_in_progress"
	case errors.Is(err, ledger.ErrInsufficientFunds):
		return "insufficient_funds"
	case errors.Is(err, ledger.ErrAccountNotActive):
		return "account_not_active"
	case errors.Is(err, store.ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrRetriesExhausted):
		return "retries_exhausted"
	default:
		return "internal"
	}
}

func (s *Service) executeOnce(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.TransferTimeout)
	defer cancel()

	now := s.now().UTC()
	tx := domain.Transfer{
		ID:          domain.NewTransferID(),
		FromAccount: req.FromAccount,
		ToAccount:   req.ToAccount,
		Amount:      req.Amount,
		Status:      domain.TransferStatusInitiated,
		Reason:      req.Reason,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Phase 1: audit anchor, committed once before any retry.
	if err := s.insertTransfer(ctx, tx); err != nil {
		return ExecuteResult{}, fmt.Errorf("insert transfer: %w", err)
	}

	var lastErr error
	backoff := s.cfg.RetryBackoff

	for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Int63n(int64(backoff)))
			wait := backoff + jitter
			s.log.Warn("retrying transfer",
				"transfer_id", tx.ID,
				"attempt", attempt,
				"wait", wait,
				"err", lastErr,
			)
			select {
			case <-ctx.Done():
				return ExecuteResult{}, ctx.Err()
			case <-time.After(wait):
			}
			backoff *= 2
		}

		res, err := s.attempt(ctx, tx)
		if err == nil {
			return res, nil
		}
		if !store.IsRetryable(err) {
			return ExecuteResult{}, err
		}
		lastErr = err
	}

	return ExecuteResult{}, fmt.Errorf("%w: %v", ErrRetriesExhausted, lastErr)
}

// validate checks request shape before touching the database.
func (s *Service) validate(req ExecuteRequest) error {
	if req.FromAccount == "" || req.ToAccount == "" {
		return fmt.Errorf("%w: from and to accounts required", ErrInvalidRequest)
	}
	if req.FromAccount == req.ToAccount {
		return fmt.Errorf("%w: cannot transfer to self", ErrInvalidRequest)
	}
	if err := req.Amount.Currency.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if !req.Amount.IsPositive() {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidRequest)
	}
	return nil
}

// attempt runs one full attempt.
//
// Two-phase structure:
//  1. Persist the transfer row as INITIATED (own transaction).
//  2. Run the money movement in a second transaction.
//
// If step 2 fails at commit (e.g. cross-bank Commit fails), the transfer
// row survives step 1 and is marked FAILED by the caller.
func (s *Service) insertTransfer(ctx context.Context, tx domain.Transfer) error {
	transferRepo := store.NewTransferRepo(s.db)
	return s.db.WithTx(ctx, func(ctx context.Context, dbtx pgx.Tx) error {
		return transferRepo.Insert(ctx, dbtx, tx)
	})
}

// attempt runs one full attempt.
//
// Two-phase structure:
//  1. Persist the transfer row as INITIATED (own transaction).
//  2. Run the money movement in a second transaction.
//
// If step 2 fails at commit (e.g. cross-bank Commit fails), the transfer
// row survives step 1 and is marked FAILED by the caller.
func (s *Service) attempt(ctx context.Context, tx domain.Transfer) (ExecuteResult, error) {
	accountRepo := store.NewAccountRepo(s.db)
	transferRepo := store.NewTransferRepo(s.db)
	entryRepo := store.NewEntryRepo(s.db)

	var result ExecuteResult
	var businessErr error

	txErr := s.db.WithTx(ctx, func(ctx context.Context, dbtx pgx.Tx) error {
		first, second := orderAccountIDs(tx.FromAccount, tx.ToAccount)

		if err := accountRepo.LockAdvisory(ctx, dbtx, first); err != nil {
			return fmt.Errorf("advisory lock %s: %w", first, err)
		}
		if err := accountRepo.LockAdvisory(ctx, dbtx, second); err != nil {
			return fmt.Errorf("advisory lock %s: %w", second, err)
		}

		acctFirst, err := accountRepo.LockForUpdate(ctx, dbtx, first)
		if err != nil {
			businessErr = fmt.Errorf("lock %s: %w", first, err)
			return nil
		}
		acctSecond, err := accountRepo.LockForUpdate(ctx, dbtx, second)
		if err != nil {
			businessErr = fmt.Errorf("lock %s: %w", second, err)
			return nil
		}

		var fromAcct, toAcct domain.Account
		if tx.FromAccount == first {
			fromAcct, toAcct = acctFirst, acctSecond
		} else {
			fromAcct, toAcct = acctSecond, acctFirst
		}

		balance, err := entryRepo.Balance(ctx, dbtx, tx.FromAccount)
		if err != nil {
			return fmt.Errorf("balance: %w", err)
		}

		var reservation *adapter.Reservation
		if fromAcct.BankID != toAcct.BankID {
			res, err := s.reserveAtSourceBank(ctx, tx, fromAcct)
			if err != nil {
				businessErr = err
				return nil
			}
			reservation = &res
		}

		commitSucceeded := false
		defer func() {
			if reservation != nil && !commitSucceeded {
				rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.rollbackAtSourceBank(rbCtx, *reservation); err != nil {
					s.log.Error("rollback failed",
						"reservation_id", reservation.ID,
						"err", err)
				}
			}
		}()

		entries, err := s.engine.Post(tx, fromAcct, toAcct, balance, true)
		if err != nil {
			businessErr = err
			return nil
		}

		if err := entryRepo.Insert(ctx, dbtx, entries); err != nil {
			return fmt.Errorf("insert entries: %w", err)
		}

		if err := transferRepo.TransitionStatus(ctx, dbtx, tx.ID,
			domain.TransferStatusInitiated, domain.TransferStatusValidated, ""); err != nil {
			return fmt.Errorf("transition validated: %w", err)
		}
		if err := transferRepo.TransitionStatus(ctx, dbtx, tx.ID,
			domain.TransferStatusValidated, domain.TransferStatusSettled, ""); err != nil {
			return fmt.Errorf("transition settled: %w", err)
		}

		if reservation != nil {
			if err := s.commitAtSourceBank(ctx, *reservation); err != nil {
				return fmt.Errorf("commit reservation: %w", err)
			}
			commitSucceeded = true
		}

		result = ExecuteResult{
			Transfer: domain.Transfer{
				ID:          tx.ID,
				FromAccount: tx.FromAccount,
				ToAccount:   tx.ToAccount,
				Amount:      tx.Amount,
				Status:      domain.TransferStatusSettled,
				Reason:      tx.Reason,
				CreatedAt:   tx.CreatedAt,
				UpdatedAt:   s.now().UTC(),
			},
			Entries: entries,
		}
		return nil
	})

	if txErr != nil {
		// Money movement failed. The transfer row exists in INITIATED.
		// Mark it FAILED, then return the error.
		if mfErr := s.markFailed(ctx, tx.ID, txErr.Error()); mfErr != nil {
			s.log.Error("failed to mark transfer FAILED",
				"transfer_id", tx.ID, "err", mfErr)
		}
		return ExecuteResult{}, txErr
	}

	if businessErr != nil {
		if err := s.markFailed(ctx, tx.ID, businessErr.Error()); err != nil {
			s.log.Error("failed to mark transfer FAILED",
				"transfer_id", tx.ID, "err", err)
			return ExecuteResult{}, fmt.Errorf("%w (also failed to persist failure: %v)", businessErr, err)
		}
		return ExecuteResult{}, businessErr
	}

	return result, nil
}

// reserveAtSourceBank looks up the source bank's adapter and reserves funds.
func (s *Service) reserveAtSourceBank(
	ctx context.Context,
	tx domain.Transfer,
	fromAcct domain.Account,
) (adapter.Reservation, error) {
	a, err := s.registry.Get(fromAcct.BankID)
	if err != nil {
		return adapter.Reservation{}, fmt.Errorf("source bank adapter: %w", err)
	}
	res, err := a.Reserve(ctx, adapter.ReserveRequest{
		TransferID: tx.ID,
		AccountID:  fromAcct.ID,
		Amount:     tx.Amount,
		ExpiresAt:  s.now().UTC().Add(30 * time.Second),
	})
	if err != nil {
		return adapter.Reservation{}, fmt.Errorf("reserve at %s: %w", fromAcct.BankID, err)
	}
	return res, nil
}

func (s *Service) commitAtSourceBank(ctx context.Context, res adapter.Reservation) error {
	a, err := s.registry.Get(res.BankID)
	if err != nil {
		return err
	}
	return a.Commit(ctx, res)
}

func (s *Service) rollbackAtSourceBank(ctx context.Context, res adapter.Reservation) error {
	a, err := s.registry.Get(res.BankID)
	if err != nil {
		return err
	}
	return a.Rollback(ctx, res)
}

// markFailed transitions a transfer to FAILED.
func (s *Service) markFailed(ctx context.Context, id domain.TransferID, reason string) error {
	transferRepo := store.NewTransferRepo(s.db)
	return s.db.WithTx(ctx, func(ctx context.Context, dbtx pgx.Tx) error {
		err := transferRepo.TransitionStatus(ctx, dbtx, id,
			domain.TransferStatusInitiated, domain.TransferStatusFailed, reason)
		if err == nil {
			return nil
		}
		if !errors.Is(err, store.ErrConflict) {
			return err
		}
		return transferRepo.TransitionStatus(ctx, dbtx, id,
			domain.TransferStatusValidated, domain.TransferStatusFailed, reason)
	})
}

// orderAccountIDs returns the two IDs in ascending lexicographic order.
func orderAccountIDs(a, b domain.AccountID) (domain.AccountID, domain.AccountID) {
	if string(a) < string(b) {
		return a, b
	}
	return b, a
}

// ---------- Idempotency helpers ----------

func (s *Service) acquireKey(ctx context.Context, key, reqHash string) (store.IdempotencyRecord, bool, error) {
	repo := store.NewIdempotencyRepo(s.db)
	now := s.now().UTC()
	expires := now.Add(24 * time.Hour)

	var rec store.IdempotencyRecord
	var acquired bool
	err := s.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var innerErr error
		rec, acquired, innerErr = repo.TryAcquire(ctx, tx, key, reqHash, now, expires)
		return innerErr
	})
	if err != nil {
		return store.IdempotencyRecord{}, false, err
	}
	return rec, acquired, nil
}

func (s *Service) replayExisting(rec store.IdempotencyRecord, reqHash string) (ExecuteResult, error) {
	if rec.RequestHash != reqHash {
		return ExecuteResult{}, fmt.Errorf("%w: key=%s", ErrIdempotencyConflict, rec.Key)
	}
	switch rec.Status {
	case store.IdempotencyStatusInProgress:
		return ExecuteResult{}, fmt.Errorf("%w: key=%s", ErrIdempotencyInProgress, rec.Key)
	case store.IdempotencyStatusFailed:
		var se storedError
		if rec.ResponseBody != nil {
			_ = json.Unmarshal(rec.ResponseBody, &se)
		}
		if se.Message == "" {
			se.Message = "previous request failed"
		}
		return ExecuteResult{}, errors.New(se.Message)
	case store.IdempotencyStatusCompleted:
		return decodeResult(rec.ResponseBody)
	}
	return ExecuteResult{}, fmt.Errorf("unknown idempotency status: %s", rec.Status)
}

func (s *Service) completeKey(ctx context.Context, key string, res ExecuteResult) error {
	body, err := encodeResult(res)
	if err != nil {
		return err
	}
	repo := store.NewIdempotencyRepo(s.db)
	return s.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.Complete(ctx, tx, key, body, 200)
	})
}

func (s *Service) failKey(ctx context.Context, key string, execErr error) error {
	body, err := encodeError(execErr)
	if err != nil {
		return err
	}
	repo := store.NewIdempotencyRepo(s.db)
	return s.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return repo.Fail(ctx, tx, key, body, 400)
	})
}

// GetTransfer returns the current state of a transfer.
func (s *Service) GetTransfer(ctx context.Context, id domain.TransferID) (domain.Transfer, error) {
	transferRepo := store.NewTransferRepo(s.db)
	var out domain.Transfer
	err := s.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t, err := transferRepo.GetByID(ctx, tx, id)
		if err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

// GetBalance returns the derived balance for an account.
func (s *Service) GetBalance(ctx context.Context, id domain.AccountID) (domain.Money, error) {
	entryRepo := store.NewEntryRepo(s.db)
	var out domain.Money
	err := s.db.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		b, err := entryRepo.Balance(ctx, tx, id)
		if err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}
