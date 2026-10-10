package domain

import (
	"errors"
	"fmt"
	"math"
)

// Money represents an amount in the smallest indivisible unit of a currency.
// For USD/EUR/RUB, that is cents/kopecks (10^-2). It is NEVER a float.
//
// Invariant: Amount is always in minor units. 1.00 USD == Money{Amount: 100}.
type Money struct {
	Amount   int64    // minor units, e.g. kopecks
	Currency Currency // ISO 4217 alpha-3 code
}

var (
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrOverflow         = errors.New("monetary amount overflow")
	ErrNegativeAmount   = errors.New("amount must not be negative")
	ErrAmountOutOfRange = errors.New("amount out of representable range")
)



// NewMoney constructs a Money value, rejecting invalid inputs.
func NewMoney(amount int64, currency Currency) (Money, error) {
	if err := currency.Validate(); err != nil {
		return Money{}, err
	}
	if amount == math.MinInt64 {
		return Money{}, ErrAmountOutOfRange
	}
	return Money{Amount: amount, Currency: currency}, nil
}

// MustMoney is a test/seed helper. Panics on invalid input.
// Do not use in production paths.
func MustMoney(amount int64, currency Currency) Money {
	m, err := NewMoney(amount, currency)
	if err != nil {
		panic(err)
	}
	return m
}

// Add returns m + other. Currencies must match. Overflow is detected.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	sum := m.Amount + other.Amount
	// Overflow check: if signs of operands are the same but sign of result differs.
	if (m.Amount > 0 && other.Amount > 0 && sum < 0) ||
		(m.Amount < 0 && other.Amount < 0 && sum > 0) {
		return Money{}, ErrOverflow
	}
	return Money{Amount: sum, Currency: m.Currency}, nil
}

// Sub returns m - other. Currencies must match.
func (m Money) Sub(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	diff := m.Amount - other.Amount
	if (m.Amount > 0 && other.Amount < 0 && diff < 0) ||
		(m.Amount < 0 && other.Amount > 0 && diff > 0) {
		return Money{}, ErrOverflow
	}
	return Money{Amount: diff, Currency: m.Currency}, nil
}

// Neg returns -m.
func (m Money) Neg() Money { return Money{Amount: -m.Amount, Currency: m.Currency} }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.Amount == 0 }

// IsPositive reports whether the amount is strictly greater than zero.
func (m Money) IsPositive() bool { return m.Amount > 0 }

// IsNegative reports whether the amount is strictly less than zero.
func (m Money) IsNegative() bool { return m.Amount < 0 }

// String renders Money for logs. Do NOT use for display formatting.
func (m Money) String() string {
	sign := ""
	if m.Amount < 0 {
		sign = "-"
	}
	a := abs(m.Amount)
	return fmt.Sprintf("%s %s%d.%02d", m.Currency, sign, a/100, a%100)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
