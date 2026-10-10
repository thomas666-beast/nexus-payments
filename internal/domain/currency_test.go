package domain

import (
	"errors"
	"testing"
)

func TestCurrency_Validate(t *testing.T) {
	for _, c := range []Currency{USD, EUR, RUB, KZT, BYN, AMD} {
		if err := c.Validate(); err != nil {
			t.Fatalf("%s should be valid: %v", c, err)
		}
	}
}

func TestCurrency_Validate_RejectsUnknown(t *testing.T) {
	if err := Currency("XYZ").Validate(); !errors.Is(err, ErrInvalidCurrency) {
		t.Fatalf("want ErrInvalidCurrency, got %v", err)
	}
}
