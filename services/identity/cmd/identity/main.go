// Command identity runs the Coinly identity service: registration, login,
// JWT issuance and the JWKS endpoint. This file is wiring only — every
// decision lives in the domain, app or adapter packages it composes.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/andytrue7/coinly/pkg/httpx"
	"github.com/andytrue7/coinly/pkg/jwtx"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/in/httpapi"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/out/postgres"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/out/token"
	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/config"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

func main() {
	if err := run(); err != nil {
		slog.Error("identity exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(log)

	// --- outbound adapters -----------------------------------------------------
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := postgres.Connect(connectCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		if err := postgres.Migrate(connectCtx, pool); err != nil {
			return err
		}
		log.Info("migrations applied")
	}

	priv, err := loadSigningKey(cfg, log)
	if err != nil {
		return err
	}
	issuer := token.NewIssuer(priv, token.Config{
		Issuer:         cfg.JWTIssuer,
		Audience:       cfg.JWTAudience,
		AccessTokenTTL: cfg.AccessTokenTTL,
	})
	verifier := jwtx.NewVerifier(jwtx.NewStaticKeySet(issuer.JWKS().Keys...),
		jwtx.VerifierConfig{Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience})

	// --- application -------------------------------------------------------------
	auth := app.NewAuthService(app.AuthDeps{
		Users:  postgres.NewUserRepo(pool),
		Tokens: postgres.NewRefreshTokenRepo(pool),
		Hasher: domain.NewArgon2Hasher(domain.DefaultArgon2Params()),
		Issuer: issuer,
		Clock:  systemClock{},
	}, app.AuthConfig{RefreshTokenTTL: cfg.RefreshTokenTTL})

	// --- inbound adapter -----------------------------------------------------------
	root := http.NewServeMux()
	root.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "not_ready", "database unreachable")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	root.Handle("/", httpapi.NewHandler(httpapi.Deps{
		Auth:     auth,
		JWKS:     issuer.JWKS(),
		Verifier: verifier,
		Clock:    systemClock{},
		Logger:   log,
	}))

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("identity listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// loadSigningKey reads the configured PEM key, or generates an ephemeral
// one in dev mode (with a loud warning: every token dies on restart).
func loadSigningKey(cfg config.Config, log *slog.Logger) (ed25519.PrivateKey, error) {
	if cfg.JWTDevEphemeralKey {
		log.Warn("IDENTITY_JWT_DEV_EPHEMERAL_KEY set: using a throwaway signing key; tokens will not survive a restart")
		return jwtx.GeneratePrivateKey()
	}
	pemBytes, err := os.ReadFile(cfg.JWTPrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	return jwtx.ParsePrivateKeyPEM(pemBytes)
}

// systemClock is the production app.Clock. UTC so timestamps are uniform
// whether they come fresh from the clock or back from Postgres.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
