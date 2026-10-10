package domain

import (
	"testing"

	"pgregory.net/rapid"
)

// Property 1: Add is commutative.
func TestProperty_AddCommutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genMoney(t)
		b := genMoney(t, a.Currency)
		ab, errA := a.Add(b)
		ba, errB := b.Add(a)
		if errA != nil || errB != nil {
			return // overflow path, both should fail identically; covered separately
		}
		if ab != ba {
			t.Fatalf("a+b != b+a: %v vs %v", ab, ba)
		}
	})
}

// Property 2: Add is associative.
func TestProperty_AddAssociative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genMoney(t)
		b := genMoney(t, a.Currency)
		c := genMoney(t, a.Currency)
		ab, err1 := a.Add(b)
		if err1 != nil {
			return
		}
		abc1, err2 := ab.Add(c)
		bc, err3 := b.Add(c)
		if err3 != nil {
			return
		}
		abc2, err4 := a.Add(bc)
		if err2 != nil || err4 != nil {
			return
		}
		if abc1 != abc2 {
			t.Fatalf("(a+b)+c != a+(b+c): %v vs %v", abc1, abc2)
		}
	})
}

// Property 3: Add then Sub with same value is identity.
func TestProperty_AddThenSubIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genMoney(t)
		b := genMoney(t, a.Currency)
		sum, err := a.Add(b)
		if err != nil {
			return
		}
		back, err := sum.Sub(b)
		if err != nil {
			return
		}
		if back != a {
			t.Fatalf("(a+b)-b != a: got %v want %v", back, a)
		}
	})
}

// Property 4: Neg is an involution and preserves currency.
func TestProperty_NegInvolution(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genMoney(t)
		if a.Neg().Neg() != a {
			t.Fatalf("(-a).-a != a: %v", a)
		}
	})
}

// genMoney generates a Money with random amount in a safe range and the given currency.
func genMoney(t *rapid.T, currencies ...Currency) Money {
	var cur Currency
	if len(currencies) > 0 {
		cur = currencies[0]
	} else {
		cur = rapid.SampledFrom([]Currency{USD, EUR, RUB}).Draw(t, "currency")
	}
	// Keep amounts small enough that arithmetic won't overflow in these tests.
	amt := rapid.Int64Range(-1_000_000, 1_000_000).Draw(t, "amount")
	return Money{Amount: amt, Currency: cur}
}
