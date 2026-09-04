//go:build integration

package postgres_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/andytrue7/coinly/services/identity/internal/adapters/out/postgres"
)

// pool is shared by every test in the package; each test starts from an
// empty schema via truncate.
var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("identity"),
		tcpostgres.WithUsername("identity"),
		tcpostgres.WithPassword("identity"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
	}

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}
	pool, err = postgres.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	code := m.Run()

	pool.Close()
	if err := ctr.Terminate(ctx); err != nil {
		log.Printf("terminate container: %v", err)
	}
	os.Exit(code)
}

func truncate(t *testing.T) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, refresh_tokens CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// now is a fixed instant at Postgres' microsecond precision so round-trips
// compare equal.
var now = time.Date(2026, 9, 4, 10, 30, 0, 123456000, time.UTC)

func TestMigrate_Idempotent(t *testing.T) {
	// TestMain already migrated once; a second run must be a clean no-op.
	if err := postgres.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('users', 'refresh_tokens')`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("tables present = %d, want 2", n)
	}
}

func TestConnect_BadDSN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := postgres.Connect(ctx, "postgres://nobody:nothing@127.0.0.1:1/none?sslmode=disable"); err == nil {
		t.Error("Connect(bad dsn) err = nil, want error")
	}
}
