package app_test

import (
	"time"

	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/app/apptest"
)

// harness wires an AuthService to the in-memory fakes from apptest.
type harness struct {
	svc    *app.AuthService
	clock  *apptest.Clock
	hasher *apptest.Hasher
	issuer *apptest.Issuer
	users  *apptest.UserRepo
	tokens *apptest.TokenRepo
}

const (
	accessTTL  = 15 * time.Minute
	refreshTTL = 30 * 24 * time.Hour
)

var t0 = time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)

func newHarness() *harness {
	h := &harness{
		clock:  apptest.NewClock(t0),
		hasher: &apptest.Hasher{},
		issuer: &apptest.Issuer{TTL: accessTTL},
		users:  apptest.NewUserRepo(),
		tokens: apptest.NewTokenRepo(),
	}
	h.svc = app.NewAuthService(app.AuthDeps{
		Users:  h.users,
		Tokens: h.tokens,
		Hasher: h.hasher,
		Issuer: h.issuer,
		Clock:  h.clock,
	}, app.AuthConfig{RefreshTokenTTL: refreshTTL})
	return h
}
