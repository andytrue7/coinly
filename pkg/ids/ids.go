// Package ids generates and parses the UUIDv7 identifiers used as primary
// keys across Coinly services (see ADR 0003). It wraps a single vetted
// library so every service shares one generator rather than picking its
// own.
package ids

import (
	"fmt"

	"github.com/google/uuid"
)

// ID is a UUIDv7 identifier. The zero value is the nil UUID and is never a
// valid entity ID; use IsZero to detect it.
type ID uuid.UUID

// New returns a fresh UUIDv7. IDs generated close together in time sort
// close together, which keeps append-heavy primary-key indexes compact.
func New() ID {
	// uuid.NewV7 only fails if the OS random source does, which is treated
	// as unrecoverable everywhere else in Go's crypto stack too.
	return ID(uuid.Must(uuid.NewV7()))
}

// Parse decodes the canonical hyphenated textual form of an ID. It accepts
// upper- or lower-case hex.
func Parse(s string) (ID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return ID{}, fmt.Errorf("ids: parse %q: %w", s, err)
	}
	return ID(u), nil
}

// String renders the ID in canonical lowercase hyphenated form.
func (id ID) String() string {
	return uuid.UUID(id).String()
}

// IsZero reports whether the ID is the nil UUID, i.e. unset.
func (id ID) IsZero() bool {
	return id == ID{}
}
