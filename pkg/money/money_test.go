package money_test

import (
	"errors"
	"testing"

	"github.com/andytrue7/coinly/pkg/money"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		amount   int64
		currency string
		wantErr  error
	}{
		{name: "valid EUR", amount: 1050, currency: "EUR"},
		{name: "valid JPY zero-exponent currency", amount: 1050, currency: "JPY"},
		{name: "valid BHD three-exponent currency", amount: 1050, currency: "BHD"},
		{name: "valid negative amount", amount: -500, currency: "USD"},
		{name: "valid zero amount", amount: 0, currency: "USD"},
		{name: "unknown currency", amount: 100, currency: "XYZ", wantErr: money.ErrUnknownCurrency},
		{name: "empty currency", amount: 100, currency: "", wantErr: money.ErrUnknownCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := money.New(tt.amount, tt.currency)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("New(%d, %q) error = %v, want %v", tt.amount, tt.currency, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%d, %q) unexpected error: %v", tt.amount, tt.currency, err)
			}
			if m.Amount() != tt.amount {
				t.Errorf("Amount() = %d, want %d", m.Amount(), tt.amount)
			}
			if m.Currency() != tt.currency {
				t.Errorf("Currency() = %q, want %q", m.Currency(), tt.currency)
			}
		})
	}
}

func TestAdd(t *testing.T) {
	tests := []struct {
		name      string
		a, b      int64
		currency  string
		bCurrency string // defaults to currency if empty
		want      int64
		wantErr   error
	}{
		{name: "positive + positive", a: 100, b: 250, currency: "EUR", want: 350},
		{name: "positive + negative", a: 500, b: -200, currency: "EUR", want: 300},
		{name: "results in zero", a: 500, b: -500, currency: "EUR", want: 0},
		{name: "currency mismatch", a: 100, b: 100, currency: "EUR", bCurrency: "USD", wantErr: money.ErrCurrencyMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bCurrency := tt.bCurrency
			if bCurrency == "" {
				bCurrency = tt.currency
			}
			a, err := money.New(tt.a, tt.currency)
			if err != nil {
				t.Fatalf("New(a) unexpected error: %v", err)
			}
			b, err := money.New(tt.b, bCurrency)
			if err != nil {
				t.Fatalf("New(b) unexpected error: %v", err)
			}

			got, err := a.Add(b)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Add() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Add() unexpected error: %v", err)
			}
			if got.Amount() != tt.want {
				t.Errorf("Add() amount = %d, want %d", got.Amount(), tt.want)
			}
			if got.Currency() != tt.currency {
				t.Errorf("Add() currency = %q, want %q", got.Currency(), tt.currency)
			}
		})
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		name      string
		a, b      int64
		currency  string
		bCurrency string
		want      int64
		wantErr   error
	}{
		{name: "positive - smaller positive", a: 500, b: 200, currency: "EUR", want: 300},
		{name: "positive - larger positive goes negative", a: 200, b: 500, currency: "EUR", want: -300},
		{name: "results in zero", a: 500, b: 500, currency: "EUR", want: 0},
		{name: "currency mismatch", a: 100, b: 100, currency: "EUR", bCurrency: "USD", wantErr: money.ErrCurrencyMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bCurrency := tt.bCurrency
			if bCurrency == "" {
				bCurrency = tt.currency
			}
			a, err := money.New(tt.a, tt.currency)
			if err != nil {
				t.Fatalf("New(a) unexpected error: %v", err)
			}
			b, err := money.New(tt.b, bCurrency)
			if err != nil {
				t.Fatalf("New(b) unexpected error: %v", err)
			}

			got, err := a.Sub(b)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Sub() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Sub() unexpected error: %v", err)
			}
			if got.Amount() != tt.want {
				t.Errorf("Sub() amount = %d, want %d", got.Amount(), tt.want)
			}
		})
	}
}

func TestNeg(t *testing.T) {
	tests := []struct {
		name   string
		amount int64
		want   int64
	}{
		{name: "positive becomes negative", amount: 500, want: -500},
		{name: "negative becomes positive", amount: -500, want: 500},
		{name: "zero stays zero", amount: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := money.New(tt.amount, "EUR")
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}
			got := m.Neg()
			if got.Amount() != tt.want {
				t.Errorf("Neg() amount = %d, want %d", got.Amount(), tt.want)
			}
			if got.Currency() != "EUR" {
				t.Errorf("Neg() currency = %q, want %q", got.Currency(), "EUR")
			}
		})
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		n       int
		want    []int64
		wantErr error
	}{
		{name: "evenly divisible", amount: 300, n: 3, want: []int64{100, 100, 100}},
		{name: "remainder distributed to first parts", amount: 100, n: 3, want: []int64{34, 33, 33}},
		{name: "remainder equals one minor unit", amount: 10, n: 3, want: []int64{4, 3, 3}},
		{name: "negative amount remainder", amount: -100, n: 3, want: []int64{-34, -33, -33}},
		{name: "single recipient", amount: 500, n: 1, want: []int64{500}},
		{name: "more recipients than minor units", amount: 2, n: 5, want: []int64{1, 1, 0, 0, 0}},
		{name: "zero recipients", amount: 100, n: 0, wantErr: money.ErrInvalidSplit},
		{name: "negative recipients", amount: 100, n: -1, wantErr: money.ErrInvalidSplit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := money.New(tt.amount, "EUR")
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}

			parts, err := m.Split(tt.n)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Split() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Split() unexpected error: %v", err)
			}

			if len(parts) != len(tt.want) {
				t.Fatalf("Split() returned %d parts, want %d", len(parts), len(tt.want))
			}

			var sum int64
			for i, p := range parts {
				if p.Currency() != "EUR" {
					t.Errorf("part %d currency = %q, want %q", i, p.Currency(), "EUR")
				}
				if p.Amount() != tt.want[i] {
					t.Errorf("part %d amount = %d, want %d", i, p.Amount(), tt.want[i])
				}
				sum += p.Amount()
			}
			if sum != tt.amount {
				t.Errorf("Split() parts sum to %d, want %d (no minor unit should be lost or created)", sum, tt.amount)
			}
		})
	}
}

func TestExponent(t *testing.T) {
	tests := []struct {
		currency string
		want     int
		wantErr  error
	}{
		{currency: "EUR", want: 2},
		{currency: "USD", want: 2},
		{currency: "JPY", want: 0},
		{currency: "BHD", want: 3},
		{currency: "XYZ", wantErr: money.ErrUnknownCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.currency, func(t *testing.T) {
			got, err := money.Exponent(tt.currency)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Exponent(%q) error = %v, want %v", tt.currency, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Exponent(%q) unexpected error: %v", tt.currency, err)
			}
			if got != tt.want {
				t.Errorf("Exponent(%q) = %d, want %d", tt.currency, got, tt.want)
			}
		})
	}
}
