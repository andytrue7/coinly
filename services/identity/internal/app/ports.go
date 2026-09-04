// Package app holds the identity service's use cases. It depends on the
// domain and on the outbound port interfaces declared here; adapters
// (Postgres, JWT signing, HTTP) implement those ports and are wired in by
// cmd. Nothing in this package imports an adapter (enforced by depguard).
package app

import (
	"context"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// UserRepository persists users. Implementations return
// domain.ErrEmailTaken from Create on a normalized-email collision and
// domain.ErrUserNotFound from lookups that miss.
type UserRepository interface {
	Create(ctx context.Context, u *domain.User) error
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	FindByID(ctx context.Context, id ids.ID) (*domain.User, error)
	Update(ctx context.Context, u *domain.User) error
}

// RefreshTokenRepository persists refresh tokens, keyed by their hash.
// Lookups that miss return domain.ErrRefreshTokenNotFound.
type RefreshTokenRepository interface {
	Create(ctx context.Context, t *domain.RefreshToken) error
	FindByHash(ctx context.Context, hash string) (*domain.RefreshToken, error)
	Update(ctx context.Context, t *domain.RefreshToken) error
	// RevokeAllForUser revokes every unrevoked token belonging to userID
	// as of now. Used when refresh-token reuse indicates a leaked token.
	RevokeAllForUser(ctx context.Context, userID ids.ID, now time.Time) error
}

// AccessTokenIssuer mints short-lived access tokens (JWTs) for a user.
// The issuer owns the access-token lifetime, so it reports the expiry.
type AccessTokenIssuer interface {
	Issue(user *domain.User, now time.Time) (token string, expiresAt time.Time, err error)
}

// Clock abstracts time.Now so use cases are deterministic under test.
type Clock interface {
	Now() time.Time
}
