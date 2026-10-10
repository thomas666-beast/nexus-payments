package domain

import "errors"

// Currency is an ISO 4217 alpha-3 code.
type Currency string

const (
	USD Currency = "USD"
	EUR Currency = "EUR"
	RUB Currency = "RUB"
	KZT Currency = "KZT"
	BYN Currency = "BYN"
	AMD Currency = "AMD"
)

var ErrInvalidCurrency = errors.New("invalid currency code")

// supported is the explicit allowlist. Anything not here is rejected.
// This is deliberate: implicit support for a currency is a compliance risk.
var supported = map[Currency]struct{}{
	USD: {}, EUR: {}, RUB: {}, KZT: {}, BYN: {}, AMD: {},
}

// Validate returns nil if the currency is in the allowlist.
func (c Currency) Validate() error {
	if _, ok := supported[c]; !ok {
		return ErrInvalidCurrency
	}
	return nil
}

// String implements fmt.Stringer.
func (c Currency) String() string { return string(c) }
