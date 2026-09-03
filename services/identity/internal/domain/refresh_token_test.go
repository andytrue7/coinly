package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

const ttl = 30 * 24 * time.Hour

func TestNewRefreshToken(t *testing.T) {
	userID := ids.New()

	tok, secret, err := domain.NewRefreshToken(userID, now, ttl)
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}

	if tok.ID.IsZero() {
		t.Error("ID is zero, want generated")
	}
	if tok.UserID != userID {
		t.Errorf("UserID = %v, want %v", tok.UserID, userID)
	}
	if secret == "" {
		t.Fatal("secret is empty")
	}
	if len(secret) < 40 {
		t.Errorf("secret %q is only %d chars; want >= 40 (256 bits base64url)", secret, len(secret))
	}
	if tok.TokenHash == "" || tok.TokenHash == secret {
		t.Errorf("TokenHash = %q; must be a hash of the secret, not empty or the secret itself", tok.TokenHash)
	}
	if got := domain.HashRefreshToken(secret); got != tok.TokenHash {
		t.Errorf("HashRefreshToken(secret) = %q, want stored hash %q", got, tok.TokenHash)
	}
	if !tok.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", tok.CreatedAt, now)
	}
	if want := now.Add(ttl); !tok.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", tok.ExpiresAt, want)
	}
	if tok.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", tok.RevokedAt)
	}
}

func TestNewRefreshToken_UniqueSecrets(t *testing.T) {
	userID := ids.New()
	seen := make(map[string]struct{})
	for range 1000 {
		_, secret, err := domain.NewRefreshToken(userID, now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[secret]; dup {
			t.Fatalf("duplicate refresh token secret %q", secret)
		}
		seen[secret] = struct{}{}
	}
}

func TestNewRefreshToken_InvalidTTL(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if _, _, err := domain.NewRefreshToken(ids.New(), now, d); err == nil {
			t.Errorf("NewRefreshToken(ttl=%v): err = nil, want error", d)
		}
	}
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
	first := domain.HashRefreshToken("abc")
	again := domain.HashRefreshToken("abc")
	other := domain.HashRefreshToken("abd")

	if first != again {
		t.Errorf("HashRefreshToken is not deterministic: %q vs %q", first, again)
	}
	if first == other {
		t.Error("HashRefreshToken collides on nearby inputs")
	}
}

func TestRefreshToken_Validate(t *testing.T) {
	fresh := func(t *testing.T) *domain.RefreshToken {
		t.Helper()
		tok, _, err := domain.NewRefreshToken(ids.New(), now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	t.Run("valid", func(t *testing.T) {
		if err := fresh(t).Validate(now.Add(time.Hour)); err != nil {
			t.Errorf("Validate = %v, want nil", err)
		}
	})

	t.Run("valid just before expiry", func(t *testing.T) {
		if err := fresh(t).Validate(now.Add(ttl - time.Nanosecond)); err != nil {
			t.Errorf("Validate = %v, want nil", err)
		}
	})

	t.Run("expired at exactly ExpiresAt", func(t *testing.T) {
		err := fresh(t).Validate(now.Add(ttl))
		if !errors.Is(err, domain.ErrRefreshTokenExpired) {
			t.Errorf("Validate = %v, want %v", err, domain.ErrRefreshTokenExpired)
		}
	})

	t.Run("revoked", func(t *testing.T) {
		tok := fresh(t)
		tok.Revoke(now.Add(time.Minute))
		err := tok.Validate(now.Add(time.Hour))
		if !errors.Is(err, domain.ErrRefreshTokenRevoked) {
			t.Errorf("Validate = %v, want %v", err, domain.ErrRefreshTokenRevoked)
		}
	})

	t.Run("revoked and expired reports revoked", func(t *testing.T) {
		tok := fresh(t)
		tok.Revoke(now.Add(time.Minute))
		err := tok.Validate(now.Add(2 * ttl))
		if !errors.Is(err, domain.ErrRefreshTokenRevoked) {
			t.Errorf("Validate = %v, want %v", err, domain.ErrRefreshTokenRevoked)
		}
	})
}

func TestRefreshToken_Revoke(t *testing.T) {
	tok, _, err := domain.NewRefreshToken(ids.New(), now, ttl)
	if err != nil {
		t.Fatal(err)
	}

	first := now.Add(time.Minute)
	tok.Revoke(first)
	if tok.RevokedAt == nil || !tok.RevokedAt.Equal(first) {
		t.Fatalf("RevokedAt = %v, want %v", tok.RevokedAt, first)
	}

	// Revoking again must not move the original revocation time.
	tok.Revoke(now.Add(time.Hour))
	if !tok.RevokedAt.Equal(first) {
		t.Errorf("second Revoke moved RevokedAt to %v, want unchanged %v", tok.RevokedAt, first)
	}
}
