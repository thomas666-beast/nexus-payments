package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func activeAccount(id domain.AccountID, cur domain.Currency) domain.Account {
	return domain.Account{
		ID:          id,
		BankID:      "bank-test",
		ExternalRef: "ext-" + string(id),
		Currency:    cur,
		Holder:      "Test Holder",
		Status:      domain.AccountStatusActive,
		CreatedAt:   time.Unix(0, 0),
		UpdatedAt:   time.Unix(0, 0),
	}
}

func newTransfer(from, to domain.AccountID, amount int64, cur domain.Currency) domain.Transfer {
	return domain.Transfer{
		ID:          domain.NewTransferID(),
		FromAccount: from,
		ToAccount:   to,
		Amount:      domain.MustMoney(amount, cur),
		Status:      domain.TransferStatusInitiated,
		CreatedAt:   time.Unix(0, 0),
		UpdatedAt:   time.Unix(0, 0),
	}
}

func TestPost_HappyPath(t *testing.T) {
	from := activeAccount("acc-from", domain.USD)
	to := activeAccount("acc-to", domain.USD)
	tx := newTransfer(from.ID, to.ID, 1000, domain.USD)
	eng := New(fixedClock(time.Unix(1000, 0)))

	entries, err := eng.Post(tx, from, to, domain.MustMoney(5000, domain.USD), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Direction != domain.DirectionDebit {
		t.Fatalf("first entry must be DEBIT, got %s", entries[0].Direction)
	}
	if entries[0].Amount.Amount != -1000 {
		t.Fatalf("debit amount want -1000, got %d", entries[0].Amount.Amount)
	}
	if entries[1].Direction != domain.DirectionCredit {
		t.Fatalf("second entry must be CREDIT, got %s", entries[1].Direction)
	}
	if entries[1].Amount.Amount != 1000 {
		t.Fatalf("credit amount want 1000, got %d", entries[1].Amount.Amount)
	}
	if err := AssertBalanced(entries); err != nil {
		t.Fatalf("entries not balanced: %v", err)
	}
}

func TestPost_InsufficientFunds(t *testing.T) {
	from := activeAccount("acc-from", domain.USD)
	to := activeAccount("acc-to", domain.USD)
	tx := newTransfer(from.ID, to.ID, 1000, domain.USD)
	eng := New(fixedClock(time.Unix(1000, 0)))

	_, err := eng.Post(tx, from, to, domain.MustMoney(999, domain.USD), true)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
}

func TestPost_FrozenFromAccount(t *testing.T) {
	from := activeAccount("acc-from", domain.USD)
	from.Status = domain.AccountStatusFrozen
	to := activeAccount("acc-to", domain.USD)
	tx := newTransfer(from.ID, to.ID, 100, domain.USD)
	eng := New(nil)

	_, err := eng.Post(tx, from, to, domain.MustMoney(1000, domain.USD), true)
	if !errors.Is(err, ErrAccountNotActive) {
		t.Fatalf("want ErrAccountNotActive, got %v", err)
	}
}

func TestPost_CurrencyMismatch(t *testing.T) {
	from := activeAccount("acc-from", domain.USD)
	to := activeAccount("acc-to", domain.EUR)
	tx := newTransfer(from.ID, to.ID, 100, domain.USD)
	eng := New(nil)

	_, err := eng.Post(tx, from, to, domain.MustMoney(1000, domain.USD), true)
	if err == nil {
		t.Fatal("expected currency mismatch error, got nil")
	}
}

func TestPost_ZeroAmountRejected(t *testing.T) {
	from := activeAccount("acc-from", domain.USD)
	to := activeAccount("acc-to", domain.USD)
	tx := newTransfer(from.ID, to.ID, 100, domain.USD)
	tx.Amount = domain.MustMoney(0, domain.USD)
	eng := New(nil)

	_, err := eng.Post(tx, from, to, domain.MustMoney(1000, domain.USD), true)
	if !errors.Is(err, ErrTransferNotValid) {
		t.Fatalf("want ErrTransferNotValid, got %v", err)
	}
}

func TestPost_NoFundsCheck(t *testing.T) {
	// requiresFunds=false: a zero-balance account can still send.
	from := activeAccount("acc-from", domain.USD)
	to := activeAccount("acc-to", domain.USD)
	tx := newTransfer(from.ID, to.ID, 100, domain.USD)
	eng := New(nil)

	_, err := eng.Post(tx, from, to, domain.MustMoney(0, domain.USD), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssertBalanced_DetectsUnbalanced(t *testing.T) {
	entries := []domain.LedgerEntry{
		{
			ID: "e1", TransferID: "t1", AccountID: "a1",
			Direction: domain.DirectionDebit,
			Amount:    domain.MustMoney(-100, domain.USD),
			CreatedAt: time.Now(),
		},
		{
			ID: "e2", TransferID: "t1", AccountID: "a2",
			Direction: domain.DirectionCredit,
			Amount:    domain.MustMoney(99, domain.USD), // off by one on purpose
			CreatedAt: time.Now(),
		},
	}
	if err := AssertBalanced(entries); !errors.Is(err, ErrTransferNotBalanced) {
		t.Fatalf("want ErrTransferNotBalanced, got %v", err)
	}
}

func TestBalance_DerivesFromEntries(t *testing.T) {
	account := domain.AccountID("acc-1")
	entries := []domain.LedgerEntry{
		{ID: "e1", TransferID: "t1", AccountID: account, Direction: domain.DirectionCredit, Amount: domain.MustMoney(500, domain.USD)},
		{ID: "e2", TransferID: "t2", AccountID: account, Direction: domain.DirectionDebit, Amount: domain.MustMoney(-200, domain.USD)},
		{ID: "e3", TransferID: "t3", AccountID: account, Direction: domain.DirectionCredit, Amount: domain.MustMoney(100, domain.USD)},
		{ID: "e4", TransferID: "t4", AccountID: "other", Direction: domain.DirectionDebit, Amount: domain.MustMoney(-9999, domain.USD)},
	}
	got, err := Balance(account, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount != 400 {
		t.Fatalf("want 400, got %d", got.Amount)
	}
}
