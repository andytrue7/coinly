package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// fakeHasher is a trivially reversible "hash" so user tests don't pay for
// argon2 and can assert on what was stored.
type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) { return "fake:" + password, nil }

func (fakeHasher) Verify(password, hash string) (bool, error) {
	return hash == "fake:"+password, nil
}

var now = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func TestNewUser(t *testing.T) {
	u, err := domain.NewUser("Alice@Example.com", "s3cret-pass", fakeHasher{}, now)
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}

	if u.ID.IsZero() {
		t.Error("ID is zero, want generated")
	}
	if u.Email != "alice@example.com" {
		t.Errorf("Email = %q, want normalized %q", u.Email, "alice@example.com")
	}
	if u.PasswordHash != "fake:s3cret-pass" {
		t.Errorf("PasswordHash = %q, want hasher output", u.PasswordHash)
	}
	if u.Status != domain.UserStatusActive {
		t.Errorf("Status = %v, want active", u.Status)
	}
	if !u.CreatedAt.Equal(now) || !u.UpdatedAt.Equal(now) {
		t.Errorf("CreatedAt/UpdatedAt = %v/%v, want %v", u.CreatedAt, u.UpdatedAt, now)
	}
}

func TestNewUser_UniqueIDs(t *testing.T) {
	a, _ := domain.NewUser("a@example.com", "s3cret-pass", fakeHasher{}, now)
	b, _ := domain.NewUser("b@example.com", "s3cret-pass", fakeHasher{}, now)
	if a.ID == b.ID {
		t.Errorf("two users share ID %s", a.ID)
	}
}

func TestNewUser_Validation(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "bad email", email: "nope", password: "s3cret-pass", wantErr: domain.ErrInvalidEmail},
		{name: "empty password", email: "a@example.com", password: "", wantErr: domain.ErrWeakPassword},
		{name: "7 chars", email: "a@example.com", password: "1234567", wantErr: domain.ErrWeakPassword},
		{name: "too long", email: "a@example.com", password: strings.Repeat("x", 129), wantErr: domain.ErrPasswordTooLong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewUser(tc.email, tc.password, fakeHasher{}, now)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("NewUser err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// Boundaries: exactly 8 and exactly 128 are accepted.
	for _, pw := range []string{"12345678", strings.Repeat("x", 128)} {
		if _, err := domain.NewUser("a@example.com", pw, fakeHasher{}, now); err != nil {
			t.Errorf("NewUser(len %d password) err = %v, want nil", len(pw), err)
		}
	}
}

func TestNewUser_HasherError(t *testing.T) {
	boom := errors.New("kdf exploded")
	_, err := domain.NewUser("a@example.com", "s3cret-pass", errHasher{err: boom}, now)
	if !errors.Is(err, boom) {
		t.Errorf("NewUser err = %v, want wrapped %v", err, boom)
	}
}

type errHasher struct{ err error }

func (e errHasher) Hash(string) (string, error)         { return "", e.err }
func (e errHasher) Verify(string, string) (bool, error) { return false, e.err }

func TestUser_Authenticate(t *testing.T) {
	newUser := func(t *testing.T) *domain.User {
		t.Helper()
		u, err := domain.NewUser("a@example.com", "s3cret-pass", fakeHasher{}, now)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}

	t.Run("correct password", func(t *testing.T) {
		u := newUser(t)
		if err := u.Authenticate("s3cret-pass", fakeHasher{}); err != nil {
			t.Errorf("Authenticate = %v, want nil", err)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		u := newUser(t)
		err := u.Authenticate("wrong", fakeHasher{})
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("Authenticate = %v, want %v", err, domain.ErrInvalidCredentials)
		}
	})

	t.Run("suspended with correct password", func(t *testing.T) {
		u := newUser(t)
		u.Suspend(now.Add(time.Hour))
		err := u.Authenticate("s3cret-pass", fakeHasher{})
		if !errors.Is(err, domain.ErrUserSuspended) {
			t.Errorf("Authenticate = %v, want %v", err, domain.ErrUserSuspended)
		}
	})

	t.Run("suspended with wrong password does not reveal suspension", func(t *testing.T) {
		u := newUser(t)
		u.Suspend(now.Add(time.Hour))
		err := u.Authenticate("wrong", fakeHasher{})
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("Authenticate = %v, want %v", err, domain.ErrInvalidCredentials)
		}
	})

	t.Run("hasher error is surfaced, not treated as wrong password", func(t *testing.T) {
		u := newUser(t)
		boom := errors.New("kdf exploded")
		err := u.Authenticate("s3cret-pass", errHasher{err: boom})
		if !errors.Is(err, boom) {
			t.Errorf("Authenticate = %v, want wrapped %v", err, boom)
		}
		if errors.Is(err, domain.ErrInvalidCredentials) {
			t.Error("hasher failure must not be reported as invalid credentials")
		}
	})
}

func TestUser_Suspend(t *testing.T) {
	u, err := domain.NewUser("a@example.com", "s3cret-pass", fakeHasher{}, now)
	if err != nil {
		t.Fatal(err)
	}

	later := now.Add(time.Hour)
	u.Suspend(later)

	if u.Status != domain.UserStatusSuspended {
		t.Errorf("Status = %v, want suspended", u.Status)
	}
	if !u.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", u.UpdatedAt, later)
	}
	if !u.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt changed to %v", u.CreatedAt)
	}
}

func TestUserStatus_String(t *testing.T) {
	tests := map[domain.UserStatus]string{
		domain.UserStatusActive:    "active",
		domain.UserStatusSuspended: "suspended",
	}
	for s, want := range tests {
		if got := s.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", s, got, want)
		}
	}

	// Unknown values render diagnostically rather than as a valid name.
	if got := domain.UserStatus(0).String(); got == "active" || got == "suspended" || got == "" {
		t.Errorf("UserStatus(0).String() = %q, want a diagnostic form", got)
	}
}

func TestParseUserStatus(t *testing.T) {
	for _, s := range []domain.UserStatus{domain.UserStatusActive, domain.UserStatusSuspended} {
		got, err := domain.ParseUserStatus(s.String())
		if err != nil {
			t.Errorf("ParseUserStatus(%q): %v", s, err)
		}
		if got != s {
			t.Errorf("ParseUserStatus(%q) = %v, want %v", s, got, s)
		}
	}
	if _, err := domain.ParseUserStatus("banned"); err == nil {
		t.Error("ParseUserStatus(banned): err = nil, want error")
	}
}
