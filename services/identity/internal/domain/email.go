package domain

import (
	"fmt"
	"net/mail"
	"strings"
)

// maxEmailLength is the RFC 5321 limit on a forward-path address.
const maxEmailLength = 254

// NormalizeEmail trims and lower-cases an email address and validates it
// as a bare addr-spec (no display name). The normalized form is what gets
// stored and compared, so "Alice@Example.com" and "alice@example.com" are
// the same account.
//
// Lower-casing the local part is technically stricter than RFC 5321 (which
// leaves local-part case to the receiving host), but every mainstream
// provider treats it case-insensitively and it avoids duplicate accounts.
func NormalizeEmail(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || len(s) > maxEmailLength {
		return "", ErrInvalidEmail
	}

	addr, err := mail.ParseAddress(s)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidEmail, err)
	}
	// ParseAddress accepts "Name <addr>"; only a bare address is allowed.
	if addr.Address != s {
		return "", ErrInvalidEmail
	}
	return s, nil
}
