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

func newUser(email string) *domain.User {
	return &domain.User{ //nolint:gosec // fixture hash of a throwaway password, not a credential
		ID:           ids.New(),
		Email:        email,
		PasswordHash: "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA",
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func assertUserEqual(t *testing.T, got, want *domain.User) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Email != want.Email {
		t.Errorf("Email = %q, want %q", got.Email, want.Email)
	}
	if got.PasswordHash != want.PasswordHash {
		t.Errorf("PasswordHash = %q, want %q", got.PasswordHash, want.PasswordHash)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %v, want %v", got.Status, want.Status)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
}

func TestUserRepo_CreateAndFind(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	u := newUser("alice@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byEmail, err := repo.FindByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	assertUserEqual(t, byEmail, u)

	byID, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	assertUserEqual(t, byID, u)

	if loc := byID.CreatedAt.Location(); loc != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", loc)
	}
}

func TestUserRepo_NotFound(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	if _, err := repo.FindByEmail(ctx, "nobody@example.com"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("FindByEmail err = %v, want %v", err, domain.ErrUserNotFound)
	}
	if _, err := repo.FindByID(ctx, ids.New()); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("FindByID err = %v, want %v", err, domain.ErrUserNotFound)
	}
	if err := repo.Update(ctx, newUser("ghost@example.com")); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("Update(missing) err = %v, want %v", err, domain.ErrUserNotFound)
	}
}

func TestUserRepo_DuplicateEmail(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	if err := repo.Create(ctx, newUser("alice@example.com")); err != nil {
		t.Fatal(err)
	}

	// Exact duplicate.
	if err := repo.Create(ctx, newUser("alice@example.com")); !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("Create(dup) err = %v, want %v", err, domain.ErrEmailTaken)
	}
	// The domain normalizes case, but the DB must guard against it too
	// (citext) in case a row ever arrives via another path.
	if err := repo.Create(ctx, newUser("ALICE@example.com")); !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("Create(case-variant dup) err = %v, want %v", err, domain.ErrEmailTaken)
	}
	// And the case-insensitive lookup finds the original.
	if _, err := repo.FindByEmail(ctx, "Alice@Example.COM"); err != nil {
		t.Errorf("FindByEmail(case variant): %v", err)
	}
}

func TestUserRepo_DuplicateID(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	u := newUser("alice@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	dup := newUser("bob@example.com")
	dup.ID = u.ID

	err := repo.Create(ctx, dup)
	if err == nil {
		t.Fatal("Create(duplicate id) err = nil, want error")
	}
	if errors.Is(err, domain.ErrEmailTaken) {
		t.Error("a PK collision must not be reported as ErrEmailTaken")
	}
}

func TestUserRepo_Update(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	u := newUser("alice@example.com")
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	u.Suspend(now.Add(time.Hour))
	u.PasswordHash = "$argon2id$v=19$m=8,t=1,p=1$bmV3$bmV3"
	if err := repo.Update(ctx, u); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertUserEqual(t, got, u)
	if got.Status != domain.UserStatusSuspended {
		t.Errorf("Status after Update = %v, want suspended", got.Status)
	}
}

func TestUserRepo_UpdateEmailCollision(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	alice := newUser("alice@example.com")
	bob := newUser("bob@example.com")
	for _, u := range []*domain.User{alice, bob} {
		if err := repo.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	bob.Email = "alice@example.com"
	if err := repo.Update(ctx, bob); !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("Update(email collision) err = %v, want %v", err, domain.ErrEmailTaken)
	}
}

func TestUserRepo_RejectsUnknownStatus(t *testing.T) {
	// The CHECK constraint is the schema's guard against a status the
	// domain doesn't know; a row like that could never be read back.
	truncate(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, status, created_at, updated_at)
		 VALUES ($1, 'x@example.com', 'h', 'banned', $2, $2)`, [16]byte(ids.New()), now)
	if err == nil {
		t.Error("insert with unknown status succeeded, want CHECK violation")
	}
}
