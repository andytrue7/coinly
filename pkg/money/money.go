// Package money represents monetary amounts as exact integer minor units
// (e.g. cents), never as floating-point, so that ledger arithmetic never
// drifts. See docs/adr/0002-money-as-int64-minor-units.md.
package money

import "errors"

// ErrUnknownCurrency is returned when a currency code has no entry in the
// exponent table.
var ErrUnknownCurrency = errors.New("money: unknown currency")

// ErrCurrencyMismatch is returned by Add/Sub when the two operands are in
// different currencies.
var ErrCurrencyMismatch = errors.New("money: currency mismatch")

// ErrInvalidSplit is returned by Split when asked for a non-positive number
// of parts.
var ErrInvalidSplit = errors.New("money: split count must be positive")

// exponents maps an ISO 4217 currency code to the number of minor-unit
// decimal places it has. Most currencies use 2 (cents); a few use 0 (no
// subunit) or 3. Add an entry here to support a new currency — no
// conversion logic changes.
var exponents = map[string]int{
	"EUR": 2,
	"USD": 2,
	"GBP": 2,
	"JPY": 0,
	"BHD": 3,
}

// Exponent returns the number of minor-unit decimal places for currency,
// or ErrUnknownCurrency if it isn't in the exponent table.
func Exponent(currency string) (int, error) {
	exp, ok := exponents[currency]
	if !ok {
		return 0, ErrUnknownCurrency
	}
	return exp, nil
}

// Money is an exact monetary amount: amount always counts the currency's
// minor units (e.g. EUR amount 1050 = €10.50; JPY amount 1050 = ¥1050).
// The zero value is not a valid Money; construct one with New.
type Money struct {
	amount   int64
	currency string
}

// New constructs a Money value. It returns ErrUnknownCurrency if currency
// has no entry in the exponent table.
func New(amount int64, currency string) (Money, error) {
	if _, err := Exponent(currency); err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: currency}, nil
}

// Amount returns the amount in minor units.
func (m Money) Amount() int64 { return m.amount }

// Currency returns the ISO 4217 currency code.
func (m Money) Currency() string { return m.currency }

// Add returns m + other. It returns ErrCurrencyMismatch if the two values
// are in different currencies.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

// Sub returns m - other. It returns ErrCurrencyMismatch if the two values
// are in different currencies.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount - other.amount, currency: m.currency}, nil
}

// Neg returns -m.
func (m Money) Neg() Money {
	return Money{amount: -m.amount, currency: m.currency}
}

// Split divides m into n parts of the same currency whose amounts sum
// exactly back to m.Amount() — no minor unit is created or lost to
// rounding. Any remainder from the integer division is distributed one
// minor unit at a time to the first parts. It returns ErrInvalidSplit if n
// is not positive.
func (m Money) Split(n int) ([]Money, error) {
	if n <= 0 {
		return nil, ErrInvalidSplit
	}

	base := m.amount / int64(n)
	rem := m.amount % int64(n)

	sign := int64(1)
	if rem < 0 {
		sign = -1
		rem = -rem
	}

	parts := make([]Money, n)
	for i := 0; i < n; i++ {
		amt := base
		if int64(i) < rem {
			amt += sign
		}
		parts[i] = Money{amount: amt, currency: m.currency}
	}
	return parts, nil
}
