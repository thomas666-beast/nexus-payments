package ledger

import (
	"errors"
	"fmt"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

var (
	ErrTransferNotBalanced = errors.New("transfer entries do not sum to zero")
	ErrEmptyJournalEntry   = errors.New("journal entry is empty")
	ErrAccountNotActive    = errors.New("account is not active")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrTransferNotValid    = errors.New("transfer not valid")
)

// Clock is injectable so tests can freeze time.
type Clock func() time.Time

// Engine is the pure double-entry ledger. It knows nothing about storage,
// networking, or retries. It only knows how to turn a validated Transfer
// into a balanced set of LedgerEntries.
type Engine struct {
	now Clock
}

// New creates an Engine with the given clock. Pass nil for the system clock.
func New(now Clock) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{now: now}
}

// Post takes a validated transfer and produces the balanced journal entries.
//
// Caller responsibilities (checked here, but enforced upstream in Task 6):
//   - transfer.Validate() must pass
//   - transfer.Status must be INITIATED
//   - from account must be able to debit
//   - to account must be able to credit
//   - from account must have sufficient funds (if requiresFunds is true)
//
// The returned slice always has exactly two entries, and their Amounts sum
// to zero. This is guaranteed by construction; the balance check at the end
// is a belt-and-braces assertion, not a fix-up.
func (e *Engine) Post(
	tx domain.Transfer,
	from domain.Account,
	to domain.Account,
	fromBalance domain.Money,
	requiresFunds bool,
) ([]domain.LedgerEntry, error) {
	if err := tx.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransferNotValid, err)
	}
	if err := from.Validate(); err != nil {
		return nil, fmt.Errorf("from account: %w", err)
	}
	if err := to.Validate(); err != nil {
		return nil, fmt.Errorf("to account: %w", err)
	}
	if from.Currency != tx.Amount.Currency || to.Currency != tx.Amount.Currency {
		return nil, fmt.Errorf("currency mismatch: transfer=%s from=%s to=%s",
			tx.Amount.Currency, from.Currency, to.Currency)
	}
	if !from.CanDebit() {
		return nil, fmt.Errorf("%w: from=%s status=%s", ErrAccountNotActive, from.ID, from.Status)
	}
	if !to.CanCredit() {
		return nil, fmt.Errorf("%w: to=%s status=%s", ErrAccountNotActive, to.ID, to.Status)
	}

	// Sufficient funds check: balance + credit >= debit amount.
	// fromBalance is the current balance of the from account; it must be
	// greater than or equal to tx.Amount for the transfer to proceed.
	if requiresFunds {
		available := fromBalance
		if available.Currency != tx.Amount.Currency {
			return nil, fmt.Errorf("balance currency mismatch: %s vs %s",
				available.Currency, tx.Amount.Currency)
		}
		if available.Amount < tx.Amount.Amount {
			return nil, fmt.Errorf("%w: have %s, need %s",
				ErrInsufficientFunds, available, tx.Amount)
		}
	}

	now := e.now()

	debit := domain.LedgerEntry{
		ID:         domain.NewEntryID(),
		TransferID: tx.ID,
		AccountID:  from.ID,
		Direction:  domain.DirectionDebit,
		Amount:     tx.Amount.Neg(), // negative
		CreatedAt:  now,
	}
	credit := domain.LedgerEntry{
		ID:         domain.NewEntryID(),
		TransferID: tx.ID,
		AccountID:  to.ID,
		Direction:  domain.DirectionCredit,
		Amount:     tx.Amount, // positive
		CreatedAt:  now,
	}

	if err := debit.Validate(); err != nil {
		return nil, fmt.Errorf("debit entry: %w", err)
	}
	if err := credit.Validate(); err != nil {
		return nil, fmt.Errorf("credit entry: %w", err)
	}

	entries := []domain.LedgerEntry{debit, credit}
	if err := AssertBalanced(entries); err != nil {
		// Should be impossible. If this ever fires, it's a bug in this file.
		return nil, fmt.Errorf("internal error: %w", err)
	}
	return entries, nil
}

// AssertBalanced verifies that the sum of all entry amounts is zero
// and that all entries share the same currency.
func AssertBalanced(entries []domain.LedgerEntry) error {
	if len(entries) == 0 {
		return ErrEmptyJournalEntry
	}
	var sum domain.Money
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if i == 0 {
			sum = domain.Money{Amount: 0, Currency: e.Amount.Currency}
		}
		next, err := sum.Add(e.Amount)
		if err != nil {
			return err
		}
		sum = next
	}
	if !sum.IsZero() {
		return fmt.Errorf("%w: sum=%s", ErrTransferNotBalanced, sum)
	}
	return nil
}

// Balance computes an account balance from a set of entries.
// It returns an error if entries span more than one currency.
func Balance(account domain.AccountID, entries []domain.LedgerEntry) (domain.Money, error) {
	var balance domain.Money
	first := true
	for i, e := range entries {
		if e.AccountID != account {
			continue
		}
		if first {
			balance = domain.Money{Amount: 0, Currency: e.Amount.Currency}
			first = false
		}
		next, err := balance.Add(e.Amount)
		if err != nil {
			return domain.Money{}, fmt.Errorf("entry %d: %w", i, err)
		}
		balance = next
	}
	if first {
		return domain.Money{}, errors.New("no entries for account")
	}
	return balance, nil
}
