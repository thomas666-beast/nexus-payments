package domain

import (
	"errors"
	"testing"
	"math"
)

func TestMoney_Add(t *testing.T) {
	a := MustMoney(100, USD) // $1.00
	b := MustMoney(250, USD) // $2.50
	got, err := a.Add(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount != 350 {
		t.Fatalf("want 350, got %d", got.Amount)
	}
}

func TestMoney_Add_CurrencyMismatch(t *testing.T) {
	_, err := MustMoney(100, USD).Add(MustMoney(100, EUR))
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("want ErrCurrencyMismatch, got %v", err)
	}
}

func TestMoney_Add_Overflow(t *testing.T) {
	a := Money{Amount: 1<<63 - 1, Currency: USD}
	b := Money{Amount: 1, Currency: USD}
	_, err := a.Add(b)
	if !errors.Is(err, ErrOverflow) {
		t.Fatalf("want ErrOverflow, got %v", err)
	}
}

func TestMoney_Sub(t *testing.T) {
	a := MustMoney(500, USD)
	b := MustMoney(200, USD)
	got, err := a.Sub(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Amount != 300 {
		t.Fatalf("want 300, got %d", got.Amount)
	}
}

func TestMoney_String(t *testing.T) {
	if got := MustMoney(1234, USD).String(); got != "USD 12.34" {
		t.Fatalf("want USD 12.34, got %s", got)
	}
	if got := MustMoney(-5, USD).String(); got != "USD -0.05" {
		t.Fatalf("want USD -0.05, got %s", got)
	}
}

func TestMoney_String_PositiveAndNegative(t *testing.T) {
	cases := []struct {
		amount int64
		want   string
	}{
		{1234, "USD 12.34"},
		{-1234, "USD -12.34"},
		{-5, "USD -0.05"},
		{0, "USD 0.00"},
		{5, "USD 0.05"},
		{-100, "USD -1.00"},
		{100, "USD 1.00"},
	}
	for _, tc := range cases {
		got := MustMoney(tc.amount, USD).String()
		if got != tc.want {
			t.Errorf("MustMoney(%d).String() = %q, want %q", tc.amount, got, tc.want)
		}
	}
}

func TestNewMoney_RejectsMinInt64(t *testing.T) {
	_, err := NewMoney(math.MinInt64, USD)
	if !errors.Is(err, ErrAmountOutOfRange) {
		t.Fatalf("want ErrAmountOutOfRange, got %v", err)
	}
}
