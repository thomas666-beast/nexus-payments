package ledger

import (
	"testing"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"pgregory.net/rapid"
)

// Property 1: For ANY positive amount and same-currency accounts,
// Post produces entries that sum to zero.
func TestProperty_PostAlwaysBalances(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amount := rapid.Int64Range(1, 1_000_000_000).Draw(t, "amount")
		cur := rapid.SampledFrom([]domain.Currency{domain.USD, domain.EUR, domain.RUB}).Draw(t, "currency")

		from := activeAccount("acc-from", cur)
		to := activeAccount("acc-to", cur)
		tx := newTransfer(from.ID, to.ID, amount, cur)

		eng := New(nil)
		entries, err := eng.Post(tx, from, to, domain.MustMoney(amount, cur), true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := AssertBalanced(entries); err != nil {
			t.Fatalf("entries not balanced for amount=%d cur=%s: %v", amount, cur, err)
		}
	})
}

// Property 2: Credit amount always equals the transfer amount; debit is its negation.
func TestProperty_PostPreservesMagnitude(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amount := rapid.Int64Range(1, 1_000_000_000).Draw(t, "amount")
		cur := rapid.SampledFrom([]domain.Currency{domain.USD, domain.EUR}).Draw(t, "currency")

		from := activeAccount("acc-from", cur)
		to := activeAccount("acc-to", cur)
		tx := newTransfer(from.ID, to.ID, amount, cur)

		eng := New(nil)
		entries, err := eng.Post(tx, from, to, domain.MustMoney(amount, cur), true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entries[0].Amount.Amount != -amount {
			t.Fatalf("debit magnitude mismatch: want %d, got %d", -amount, entries[0].Amount.Amount)
		}
		if entries[1].Amount.Amount != amount {
			t.Fatalf("credit magnitude mismatch: want %d, got %d", amount, entries[1].Amount.Amount)
		}
	})
}

// Property 3: Balance after one transfer reflects the credit exactly.
func TestProperty_BalanceAfterTransfer(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amount := rapid.Int64Range(1, 1_000_000).Draw(t, "amount")
		cur := domain.USD

		from := activeAccount("acc-from", cur)
		to := activeAccount("acc-to", cur)
		tx := newTransfer(from.ID, to.ID, amount, cur)

		eng := New(nil)
		entries, err := eng.Post(tx, from, to, domain.MustMoney(amount, cur), true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		toBal, err := Balance(to.ID, entries)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if toBal.Amount != amount {
			t.Fatalf("to balance want %d, got %d", amount, toBal.Amount)
		}
		fromBal, err := Balance(from.ID, entries)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fromBal.Amount != -amount {
			t.Fatalf("from balance want %d, got %d", -amount, fromBal.Amount)
		}
	})
}

// Property 4: Post never produces entries with mismatched currencies.
func TestProperty_PostEntriesShareCurrency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		amount := rapid.Int64Range(1, 1_000_000).Draw(t, "amount")
		cur := rapid.SampledFrom([]domain.Currency{domain.USD, domain.EUR, domain.RUB}).Draw(t, "currency")

		from := activeAccount("acc-from", cur)
		to := activeAccount("acc-to", cur)
		tx := newTransfer(from.ID, to.ID, amount, cur)

		eng := New(nil)
		entries, err := eng.Post(tx, from, to, domain.MustMoney(amount, cur), true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i, e := range entries {
			if e.Amount.Currency != cur {
				t.Fatalf("entry %d currency want %s, got %s", i, cur, e.Amount.Currency)
			}
		}
	})
}
