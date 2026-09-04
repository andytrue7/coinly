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

// UserRepo implements app.UserRepository.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo returns a UserRepo backed by pool.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

const (
	userColumns = `id, email, password_hash, status, created_at, updated_at`

	insertUser = `INSERT INTO users (` + userColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6)`

	selectUserByEmail = `SELECT ` + userColumns + ` FROM users WHERE email = $1`
	selectUserByID    = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	updateUser = `UPDATE users
		SET email = $2, password_hash = $3, status = $4, updated_at = $5
		WHERE id = $1`
)

// Create inserts u. A collision on the (case-insensitive) email index is
// reported as domain.ErrEmailTaken.
func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	_, err := r.pool.Exec(ctx, insertUser,
		[16]byte(u.ID), u.Email, u.PasswordHash, u.Status.String(), u.CreatedAt, u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "users_email_key") {
			return domain.ErrEmailTaken
		}
		return fmt.Errorf("postgres: insert user: %w", err)
	}
	return nil
}

// FindByEmail looks a user up by normalized email.
func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.queryOne(ctx, selectUserByEmail, email)
}

// FindByID looks a user up by primary key.
func (r *UserRepo) FindByID(ctx context.Context, id ids.ID) (*domain.User, error) {
	return r.queryOne(ctx, selectUserByID, [16]byte(id))
}

// Update persists every mutable field of u. A missing row is
// domain.ErrUserNotFound.
func (r *UserRepo) Update(ctx context.Context, u *domain.User) error {
	tag, err := r.pool.Exec(ctx, updateUser,
		[16]byte(u.ID), u.Email, u.PasswordHash, u.Status.String(), u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "users_email_key") {
			return domain.ErrEmailTaken
		}
		return fmt.Errorf("postgres: update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepo) queryOne(ctx context.Context, sql string, arg any) (*domain.User, error) {
	rows, err := r.pool.Query(ctx, sql, arg)
	if err != nil {
		return nil, fmt.Errorf("postgres: query user: %w", err)
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("postgres: scan user: %w", err)
	}
	return u, nil
}

func scanUser(row pgx.CollectableRow) (*domain.User, error) {
	var (
		id     [16]byte
		status string
		u      domain.User
	)
	if err := row.Scan(&id, &u.Email, &u.PasswordHash, &status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	st, err := domain.ParseUserStatus(status)
	if err != nil {
		return nil, fmt.Errorf("user %x: %w", id, err)
	}
	u.ID = ids.ID(id)
	u.Status = st
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	return &u, nil
}

// utcPtr normalizes a nullable timestamp to UTC.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}
