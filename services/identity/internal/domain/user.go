// Package domain holds the identity service's pure business model: users,
// credentials and refresh tokens, plus the rules that govern them. It
// imports nothing from the app or adapter layers (enforced by depguard).
package domain

import (
	"fmt"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
)

// UserStatus is the lifecycle state of a user account.
type UserStatus int

// User lifecycle states. The zero value is deliberately invalid so an
// uninitialized status can't pass as active.
const (
	UserStatusActive UserStatus = iota + 1
	UserStatusSuspended
)

// String renders the status as its stable storage/API name.
func (s UserStatus) String() string {
	switch s {
	case UserStatusActive:
		return "active"
	case UserStatusSuspended:
		return "suspended"
	default:
		return fmt.Sprintf("UserStatus(%d)", int(s))
	}
}

// ParseUserStatus is the inverse of String, for reading persisted values.
func ParseUserStatus(s string) (UserStatus, error) {
	switch s {
	case "active":
		return UserStatusActive, nil
	case "suspended":
		return UserStatusSuspended, nil
	default:
		return 0, fmt.Errorf("unknown user status %q", s)
	}
}

// User is a registered account. PasswordHash never leaves the service
// boundary (see the identity/v1 proto, which deliberately omits it).
type User struct {
	ID           ids.ID
	Email        string
	PasswordHash string
	Status       UserStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewUser validates and normalizes the email, enforces the password
// policy, hashes the password and returns an active user. now is injected
// rather than read from the clock so the domain stays deterministic.
func NewUser(email, password string, hasher PasswordHasher, now time.Time) (*User, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	hash, err := hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	return &User{
		ID:           ids.New(),
		Email:        normalized,
		PasswordHash: hash,
		Status:       UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// Authenticate checks password against the stored hash and then the
// account status. The password is checked first so that a caller who
// doesn't know the password learns nothing about whether the account is
// suspended.
func (u *User) Authenticate(password string, hasher PasswordHasher) error {
	ok, err := hasher.Verify(password, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return ErrInvalidCredentials
	}
	if u.Status == UserStatusSuspended {
		return ErrUserSuspended
	}
	return nil
}

// Suspend blocks the account from authenticating.
func (u *User) Suspend(now time.Time) {
	u.Status = UserStatusSuspended
	u.UpdatedAt = now
}
