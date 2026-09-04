package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// RefreshTokenRepo implements app.RefreshTokenRepository.
type RefreshTokenRepo struct {
	pool *pgxpool.Pool
}

// NewRefreshTokenRepo returns a RefreshTokenRepo backed by pool.
func NewRefreshTokenRepo(pool *pgxpool.Pool) *RefreshTokenRepo {
	return &RefreshTokenRepo{pool: pool}
}

const (
	tokenColumns = `id, user_id, token_hash, created_at, expires_at, revoked_at` //nolint:gosec // column list, not a credential

	insertToken = `INSERT INTO refresh_tokens (` + tokenColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6)`

	selectTokenByHash = `SELECT ` + tokenColumns + ` FROM refresh_tokens WHERE token_hash = $1`

	updateToken = `UPDATE refresh_tokens SET revoked_at = $2 WHERE id = $1`

	// Only touches live tokens so an earlier revocation timestamp is never
	// overwritten — matching domain.RefreshToken.Revoke's semantics.
	revokeAllForUser = `UPDATE refresh_tokens SET revoked_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL`
)

// Create inserts t.
func (r *RefreshTokenRepo) Create(ctx context.Context, t *domain.RefreshToken) error {
	_, err := r.pool.Exec(ctx, insertToken,
		[16]byte(t.ID), [16]byte(t.UserID), t.TokenHash, t.CreatedAt, t.ExpiresAt, t.RevokedAt)
	if err != nil {
		return fmt.Errorf("postgres: insert refresh token: %w", err)
	}
	return nil
}

// FindByHash looks a token up by the hash of its secret.
func (r *RefreshTokenRepo) FindByHash(ctx context.Context, hash string) (*domain.RefreshToken, error) {
	rows, err := r.pool.Query(ctx, selectTokenByHash, hash)
	if err != nil {
		return nil, fmt.Errorf("postgres: query refresh token: %w", err)
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanToken)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRefreshTokenNotFound
		}
		return nil, fmt.Errorf("postgres: scan refresh token: %w", err)
	}
	return t, nil
}

// Update persists t's revocation state. A missing row is
// domain.ErrRefreshTokenNotFound.
func (r *RefreshTokenRepo) Update(ctx context.Context, t *domain.RefreshToken) error {
	tag, err := r.pool.Exec(ctx, updateToken, [16]byte(t.ID), t.RevokedAt)
	if err != nil {
		return fmt.Errorf("postgres: update refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRefreshTokenNotFound
	}
	return nil
}

// RevokeAllForUser revokes every live token belonging to userID as of now.
func (r *RefreshTokenRepo) RevokeAllForUser(ctx context.Context, userID ids.ID, now time.Time) error {
	if _, err := r.pool.Exec(ctx, revokeAllForUser, [16]byte(userID), now); err != nil {
		return fmt.Errorf("postgres: revoke refresh tokens: %w", err)
	}
	return nil
}

func scanToken(row pgx.CollectableRow) (*domain.RefreshToken, error) {
	var (
		id, userID [16]byte
		t          domain.RefreshToken
	)
	if err := row.Scan(&id, &userID, &t.TokenHash, &t.CreatedAt, &t.ExpiresAt, &t.RevokedAt); err != nil {
		return nil, err
	}
	t.ID = ids.ID(id)
	t.UserID = ids.ID(userID)
	t.CreatedAt = t.CreatedAt.UTC()
	t.ExpiresAt = t.ExpiresAt.UTC()
	t.RevokedAt = utcPtr(t.RevokedAt)
	return &t, nil
}
