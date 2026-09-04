//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/out/postgres"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

const ttl = 30 * 24 * time.Hour

// seedUser inserts a user so refresh tokens have a valid FK target.
func seedUser(t *testing.T, email string) *domain.User {
	t.Helper()
	u := newUser(email)
	if err := postgres.NewUserRepo(pool).Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func mintToken(t *testing.T, repo *postgres.RefreshTokenRepo, userID ids.ID) (*domain.RefreshToken, string) {
	t.Helper()
	tok, secret, err := domain.NewRefreshToken(userID, now, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), tok); err != nil {
		t.Fatalf("Create token: %v", err)
	}
	return tok, secret
}

func assertTokenEqual(t *testing.T, got, want *domain.RefreshToken) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.UserID != want.UserID {
		t.Errorf("UserID = %v, want %v", got.UserID, want.UserID)
	}
	if got.TokenHash != want.TokenHash {
		t.Errorf("TokenHash = %q, want %q", got.TokenHash, want.TokenHash)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
	switch {
	case got.RevokedAt == nil && want.RevokedAt == nil:
	case got.RevokedAt == nil || want.RevokedAt == nil:
		t.Errorf("RevokedAt = %v, want %v", got.RevokedAt, want.RevokedAt)
	case !got.RevokedAt.Equal(*want.RevokedAt):
		t.Errorf("RevokedAt = %v, want %v", *got.RevokedAt, *want.RevokedAt)
	}
}

func TestRefreshTokenRepo_CreateAndFind(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)
	u := seedUser(t, "alice@example.com")

	tok, secret := mintToken(t, repo, u.ID)

	got, err := repo.FindByHash(ctx, domain.HashRefreshToken(secret))
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	assertTokenEqual(t, got, tok)
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", got.RevokedAt)
	}
}

func TestRefreshTokenRepo_NotFound(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)

	if _, err := repo.FindByHash(ctx, domain.HashRefreshToken("never-issued")); !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Errorf("FindByHash err = %v, want %v", err, domain.ErrRefreshTokenNotFound)
	}

	ghost, _, _ := domain.NewRefreshToken(ids.New(), now, ttl)
	if err := repo.Update(ctx, ghost); !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Errorf("Update(missing) err = %v, want %v", err, domain.ErrRefreshTokenNotFound)
	}
}

func TestRefreshTokenRepo_DuplicateHash(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)
	u := seedUser(t, "alice@example.com")

	tok, _ := mintToken(t, repo, u.ID)
	dup := *tok
	dup.ID = ids.New()
	if err := repo.Create(ctx, &dup); err == nil {
		t.Error("Create(duplicate hash) err = nil, want error")
	}
}

func TestRefreshTokenRepo_UnknownUser(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)

	tok, _, _ := domain.NewRefreshToken(ids.New(), now, ttl)
	if err := repo.Create(ctx, tok); err == nil {
		t.Error("Create(token for nonexistent user) err = nil, want FK violation")
	}
}

func TestRefreshTokenRepo_UpdateRevokes(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)
	u := seedUser(t, "alice@example.com")

	tok, secret := mintToken(t, repo, u.ID)
	tok.Revoke(now.Add(time.Minute))
	if err := repo.Update(ctx, tok); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByHash(ctx, domain.HashRefreshToken(secret))
	if err != nil {
		t.Fatal(err)
	}
	assertTokenEqual(t, got, tok)
	if err := got.Validate(now.Add(time.Hour)); !errors.Is(err, domain.ErrRefreshTokenRevoked) {
		t.Errorf("Validate after revoke = %v, want %v", err, domain.ErrRefreshTokenRevoked)
	}
}

func TestRefreshTokenRepo_RevokeAllForUser(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)
	alice := seedUser(t, "alice@example.com")
	bob := seedUser(t, "bob@example.com")

	_, aliceLive1 := mintToken(t, repo, alice.ID)
	_, aliceLive2 := mintToken(t, repo, alice.ID)
	earlier, aliceOld := mintToken(t, repo, alice.ID)
	_, bobLive := mintToken(t, repo, bob.ID)

	// One of Alice's tokens was already revoked earlier; that timestamp
	// must survive.
	firstRevocation := now.Add(-time.Hour)
	earlier.Revoke(firstRevocation)
	if err := repo.Update(ctx, earlier); err != nil {
		t.Fatal(err)
	}

	at := now.Add(time.Minute)
	if err := repo.RevokeAllForUser(ctx, alice.ID, at); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	for _, secret := range []string{aliceLive1, aliceLive2} {
		got, err := repo.FindByHash(ctx, domain.HashRefreshToken(secret))
		if err != nil {
			t.Fatal(err)
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(at) {
			t.Errorf("alice token RevokedAt = %v, want %v", got.RevokedAt, at)
		}
	}

	old, err := repo.FindByHash(ctx, domain.HashRefreshToken(aliceOld))
	if err != nil {
		t.Fatal(err)
	}
	if old.RevokedAt == nil || !old.RevokedAt.Equal(firstRevocation) {
		t.Errorf("previously revoked token RevokedAt = %v, want unchanged %v", old.RevokedAt, firstRevocation)
	}

	bobTok, err := repo.FindByHash(ctx, domain.HashRefreshToken(bobLive))
	if err != nil {
		t.Fatal(err)
	}
	if bobTok.RevokedAt != nil {
		t.Errorf("bob's token was revoked (%v); RevokeAllForUser must be scoped to one user", bobTok.RevokedAt)
	}
}

func TestRefreshTokenRepo_RevokeAllForUser_NoTokens(t *testing.T) {
	truncate(t)
	repo := postgres.NewRefreshTokenRepo(pool)

	if err := repo.RevokeAllForUser(context.Background(), ids.New(), now); err != nil {
		t.Errorf("RevokeAllForUser(no tokens) err = %v, want nil", err)
	}
}

func TestRefreshTokenRepo_CascadeOnUserDelete(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewRefreshTokenRepo(pool)
	u := seedUser(t, "alice@example.com")
	_, secret := mintToken(t, repo, u.ID)

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, [16]byte(u.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByHash(ctx, domain.HashRefreshToken(secret)); !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Errorf("token survived user delete: err = %v, want %v", err, domain.ErrRefreshTokenNotFound)
	}
}
