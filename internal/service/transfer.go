package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/store"
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

// Service orchestrates transfers.
type Service struct {
	db     *store.DB
	engine *ledger.Engine
	cfg    Config
	now    func() time.Time
	log    *slog.Logger
}

// New constructs a Service. Pass nil for logger to get a discard logger.
func New(db *store.DB, engine *ledger.Engine, cfg Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		db:     db,
		engine: engine,
		cfg:    cfg,
		now:    time.Now,
		log:    log,
	}
}

// Execute runs a transfer end-to-end. It is safe for concurrent use.
func (s *Service) executeOnce(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	if err := s.validate(req); err != nil {
		return ExecuteResult{}, err
	}

	// Fast path: no idempotency key → run without coordination.
	if req.IdempotencyKey == "" {
		return s.executeOnce(ctx, req)
	}

	// Idempotent path.
	reqHash, err := hashRequest(req)
	if err != nil {
		return ExecuteResult{}, fmt.Errorf("hash request: %w", err)
	}

	// Phase 1: try to acquire the key.
	existing, acquired, err := s.acquireKey(ctx, req.IdempotencyKey, reqHash)
	if err != nil {
		return ExecuteResult{}, err
	}
	if !acquired {
		return s.replayExisting(existing, reqHash)
	}

	// Phase 2: we own the key. Run the transfer, then complete or fail.
	res, execErr := s.executeOnce(ctx, req)
	if execErr != nil {
		// Record the failure. If recording fails, log and still return the
		// business error — the caller's request did fail.
		if recordErr := s.failKey(ctx, req.IdempotencyKey, execErr); recordErr != nil {
			s.log.Error("failed to record idempotency failure",
				"key", req.IdempotencyKey, "err", recordErr)
		}
		return ExecuteResult{}, execErr
	}

	if err := s.completeKey(ctx, req.IdempotencyKey, res); err != nil {
		s.log.Error("failed to record idempotency completion",
			"key", req.IdempotencyKey, "err", err)
		// Transfer succeeded. Return it; the key will expire.
	}
	return res, nil
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

// attempt runs one full attempt inside a single transaction.
//
// Order of operations:
//  1. Advisory-lock both accounts in deterministic order.
//  2. Row-lock both accounts (FOR UPDATE) in the same order.
//  3. Read the from-account balance (safe: advisory lock held).
//  4. Insert the transfer row.
//  5. Validate via the ledger engine.
//  6. Insert ledger entries.
//  7. Transition INITIATED -> VALIDATED -> SETTLED.
func (s *Service) attempt(ctx context.Context, tx domain.Transfer) (ExecuteResult, error) {
	accountRepo := store.NewAccountRepo(s.db)
	transferRepo := store.NewTransferRepo(s.db)
	entryRepo := store.NewEntryRepo(s.db)

	var result ExecuteResult
	var businessErr error

	txErr := s.db.WithTx(ctx, func(ctx context.Context, dbtx pgx.Tx) error {
		first, second := orderAccountIDs(tx.FromAccount, tx.ToAccount)

		// 1. Advisory locks — the authoritative serializer for the
		// "read balance, then insert entries" pattern.
		if err := accountRepo.LockAdvisory(ctx, dbtx, first); err != nil {
			return fmt.Errorf("advisory lock %s: %w", first, err)
		}
		if err := accountRepo.LockAdvisory(ctx, dbtx, second); err != nil {
			return fmt.Errorf("advisory lock %s: %w", second, err)
		}

		// 2. Row-level locks on the accounts themselves.
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

		// 3. Read from-account balance. Safe: advisory + row locks held.
		balance, err := entryRepo.Balance(ctx, dbtx, tx.FromAccount)
		if err != nil {
			return fmt.Errorf("balance: %w", err)
		}

		// 4. Insert the transfer row.
		if err := transferRepo.Insert(ctx, dbtx, tx); err != nil {
			return fmt.Errorf("insert transfer: %w", err)
		}

		// 5. Validate and build entries.
		entries, err := s.engine.Post(tx, fromAcct, toAcct, balance, true)
		if err != nil {
			businessErr = err
			return nil
		}

		// 6. Persist entries.
		if err := entryRepo.Insert(ctx, dbtx, entries); err != nil {
			return fmt.Errorf("insert entries: %w", err)
		}

		// 7. Advance state.
		if err := transferRepo.TransitionStatus(ctx, dbtx, tx.ID,
			domain.TransferStatusInitiated, domain.TransferStatusValidated, ""); err != nil {
			return fmt.Errorf("transition validated: %w", err)
		}
		if err := transferRepo.TransitionStatus(ctx, dbtx, tx.ID,
			domain.TransferStatusValidated, domain.TransferStatusSettled, ""); err != nil {
			return fmt.Errorf("transition settled: %w", err)
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

// markFailed transitions a transfer to FAILED. Runs outside the main
// attempt transaction so a business failure still leaves an audit record.
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
// Deterministic ordering prevents deadlocks under concurrent transfers.
func orderAccountIDs(a, b domain.AccountID) (domain.AccountID, domain.AccountID) {
	if string(a) < string(b) {
		return a, b
	}
	return b, a
}

// acquireKey tries to take ownership of the idempotency key.
// Returns (existingRecord, false, nil) if the key was already held,
// or (newRecord, true, nil) if we now own it.
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

// replayExisting returns the stored result for an existing key, or an
// appropriate error if the request hash differs or the previous call is
// still in progress.
func (s *Service) replayExisting(rec store.IdempotencyRecord, reqHash string) (ExecuteResult, error) {
	if rec.RequestHash != reqHash {
		return ExecuteResult{}, fmt.Errorf("%w: key=%s", ErrIdempotencyConflict, rec.Key)
	}
	switch rec.Status {
	case store.IdempotencyStatusInProgress:
		return ExecuteResult{}, fmt.Errorf("%w: key=%s", ErrIdempotencyInProgress, rec.Key)
	case store.IdempotencyStatusFailed:
		var se storedError
		_ = json.Unmarshal(rec.ResponseBody, &se)
		return ExecuteResult{}, errors.New(se.Message)
	case store.IdempotencyStatusCompleted:
		return s.decodeResult(rec.ResponseBody)
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

func (s *Service) decodeResult(body []byte) (ExecuteResult, error) {
	var sr storedResult
	if err := json.Unmarshal(body, &sr); err != nil {
		return ExecuteResult{}, fmt.Errorf("decode stored result: %w", err)
	}
	res := ExecuteResult{
		Transfer: domain.Transfer{
			ID:          sr.TransferID,
			FromAccount: sr.FromAccount,
			ToAccount:   sr.ToAccount,
			Amount:      domain.Money{Amount: sr.AmountMinor, Currency: sr.Currency},
			Status:      domain.TransferStatus(sr.Status),
		},
	}
	for _, id := range sr.EntryIDs {
		res.Entries = append(res.Entries, domain.LedgerEntry{ID: id})
	}
	return res, nil
}
