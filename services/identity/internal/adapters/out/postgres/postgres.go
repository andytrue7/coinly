// Package postgres implements the identity service's outbound persistence
// ports (app.UserRepository, app.RefreshTokenRepository) on PostgreSQL via
// pgx, and owns the service's schema migrations.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect opens a connection pool for dsn and verifies it with a ping.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies all pending embedded goose migrations. It is idempotent:
// running it against an up-to-date schema is a no-op.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("postgres: migrations fs: %w", err)
	}

	// goose speaks database/sql; borrow the pool through pgx's stdlib shim
	// so migrations share the service's connection settings.
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close() //nolint:errcheck // closing the shim doesn't close the pool

	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		return fmt.Errorf("postgres: migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
}

// uniqueViolation is the SQLSTATE for a unique-constraint failure.
const uniqueViolation = "23505"

// isUniqueViolation reports whether err is a unique-constraint violation,
// optionally on the named constraint (empty matches any).
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}
