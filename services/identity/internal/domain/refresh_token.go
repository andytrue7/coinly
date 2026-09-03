package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
)

// refreshSecretBytes is the entropy of a refresh token secret (256 bits).
const refreshSecretBytes = 32

// RefreshToken is a long-lived, revocable credential used to mint new
// access tokens. Only a hash of the secret is stored: a database leak must
// not yield usable tokens. The plaintext secret exists exactly once, in
// the return value of NewRefreshToken, and is handed to the client.
type RefreshToken struct {
	ID        ids.ID
	UserID    ids.ID
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// NewRefreshToken mints a token for userID valid for ttl from now. It
// returns the persistable token and, separately, the plaintext secret to
// give to the client.
func NewRefreshToken(userID ids.ID, now time.Time, ttl time.Duration) (*RefreshToken, string, error) {
	if ttl <= 0 {
		return nil, "", errors.New("refresh token ttl must be positive")
	}

	raw := make([]byte, refreshSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate refresh token: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)

	return &RefreshToken{
		ID:        ids.New(),
		UserID:    userID,
		TokenHash: HashRefreshToken(secret),
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}, secret, nil
}

// HashRefreshToken maps a plaintext secret to its stored form. SHA-256 is
// enough here (unlike passwords) because the secret is 256 random bits and
// cannot be brute-forced by dictionary.
func HashRefreshToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Validate reports whether the token may still be used at now. Revocation
// is checked before expiry so a revoked token is always reported as such.
func (t *RefreshToken) Validate(now time.Time) error {
	if t.RevokedAt != nil {
		return ErrRefreshTokenRevoked
	}
	if !now.Before(t.ExpiresAt) {
		return ErrRefreshTokenExpired
	}
	return nil
}

// Revoke marks the token unusable. Repeated calls keep the first
// revocation time.
func (t *RefreshToken) Revoke(now time.Time) {
	if t.RevokedAt != nil {
		return
	}
	t.RevokedAt = &now
}
