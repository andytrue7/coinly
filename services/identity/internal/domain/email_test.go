package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "plain", in: "alice@example.com", want: "alice@example.com"},
		{name: "lowercased", in: "Alice@Example.COM", want: "alice@example.com"},
		{name: "trimmed", in: "  alice@example.com \n", want: "alice@example.com"},
		{name: "plus tag kept", in: "alice+wallet@example.com", want: "alice+wallet@example.com"},
		{name: "subdomain", in: "a.b@mail.example.co.uk", want: "a.b@mail.example.co.uk"},

		{name: "empty", in: "", wantErr: domain.ErrInvalidEmail},
		{name: "whitespace only", in: "   ", wantErr: domain.ErrInvalidEmail},
		{name: "no at", in: "alice.example.com", wantErr: domain.ErrInvalidEmail},
		{name: "no local part", in: "@example.com", wantErr: domain.ErrInvalidEmail},
		{name: "no domain", in: "alice@", wantErr: domain.ErrInvalidEmail},
		{name: "display name form rejected", in: "Alice <alice@example.com>", wantErr: domain.ErrInvalidEmail},
		{name: "internal space", in: "ali ce@example.com", wantErr: domain.ErrInvalidEmail},
		{name: "too long", in: strings.Repeat("a", 250) + "@example.com", wantErr: domain.ErrInvalidEmail},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.NormalizeEmail(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NormalizeEmail(%q) err = %v, want %v", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeEmail(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
