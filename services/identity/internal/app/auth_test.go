package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

const (
	email    = "alice@example.com"
	password = "s3cret-pass"
)

func mustRegister(t *testing.T, h *harness) app.AuthResult {
	t.Helper()
	res, err := h.svc.Register(context.Background(), email, password)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return res
}

func assertTokenPair(t *testing.T, tp app.TokenPair, now time.Time) {
	t.Helper()
	if tp.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if tp.RefreshToken == "" {
		t.Error("RefreshToken is empty")
	}
	if want := now.Add(accessTTL); !tp.AccessExpiresAt.Equal(want) {
		t.Errorf("AccessExpiresAt = %v, want %v", tp.AccessExpiresAt, want)
	}
	if want := now.Add(refreshTTL); !tp.RefreshExpiresAt.Equal(want) {
		t.Errorf("RefreshExpiresAt = %v, want %v", tp.RefreshExpiresAt, want)
	}
}

// --- Register -------------------------------------------------------------------

func TestRegister(t *testing.T) {
	h := newHarness()

	res, err := h.svc.Register(context.Background(), "  Alice@Example.com ", password)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if res.User == nil {
		t.Fatal("User is nil")
	}
	if res.User.Email != email {
		t.Errorf("User.Email = %q, want normalized %q", res.User.Email, email)
	}
	if res.User.Status != domain.UserStatusActive {
		t.Errorf("User.Status = %v, want active", res.User.Status)
	}
	assertTokenPair(t, res.Tokens, t0)

	// Persisted.
	stored, err := h.users.FindByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
	if stored.ID != res.User.ID {
		t.Errorf("stored ID %v != returned ID %v", stored.ID, res.User.ID)
	}
	if stored.PasswordHash == password || !strings.HasPrefix(stored.PasswordHash, "fake:") {
		t.Errorf("stored PasswordHash = %q, want hasher output", stored.PasswordHash)
	}

	// A refresh token was persisted for the user.
	if n := h.tokens.activeCount(res.User.ID, t0); n != 1 {
		t.Errorf("active refresh tokens = %d, want 1", n)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	h := newHarness()
	mustRegister(t, h)

	_, err := h.svc.Register(context.Background(), "ALICE@example.com", "another-pass")
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("Register duplicate err = %v, want %v", err, domain.ErrEmailTaken)
	}
}

func TestRegister_Validation(t *testing.T) {
	h := newHarness()

	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "bad email", email: "nope", password: password, wantErr: domain.ErrInvalidEmail},
		{name: "weak password", email: email, password: "short", wantErr: domain.ErrWeakPassword},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.svc.Register(context.Background(), tc.email, tc.password)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Register err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// Nothing was persisted on the failed attempts.
	if _, err := h.users.FindByEmail(context.Background(), email); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("user persisted after failed validation: err = %v", err)
	}
}

func TestRegister_RepoError(t *testing.T) {
	h := newHarness()
	boom := errors.New("db down")
	h.users.failing = boom

	_, err := h.svc.Register(context.Background(), email, password)
	if !errors.Is(err, boom) {
		t.Errorf("Register err = %v, want wrapped %v", err, boom)
	}
}

func TestRegister_IssuerError(t *testing.T) {
	h := newHarness()
	boom := errors.New("no signing key")
	h.issuer.err = boom

	_, err := h.svc.Register(context.Background(), email, password)
	if !errors.Is(err, boom) {
		t.Errorf("Register err = %v, want wrapped %v", err, boom)
	}
}

// --- Login ------------------------------------------------------------------------

func TestLogin(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	h.clock.Advance(time.Hour)

	res, err := h.svc.Login(context.Background(), "Alice@EXAMPLE.com", password)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if res.User.ID != reg.User.ID {
		t.Errorf("Login returned user %v, want %v", res.User.ID, reg.User.ID)
	}
	assertTokenPair(t, res.Tokens, t0.Add(time.Hour))

	if res.Tokens.RefreshToken == reg.Tokens.RefreshToken {
		t.Error("Login reused the registration refresh token; each login must mint its own")
	}
	// Both sessions remain valid: logging in elsewhere doesn't log out here.
	if n := h.tokens.activeCount(reg.User.ID, h.clock.Now()); n != 2 {
		t.Errorf("active refresh tokens = %d, want 2", n)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h := newHarness()
	mustRegister(t, h)

	_, err := h.svc.Login(context.Background(), email, "wrong-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("Login err = %v, want %v", err, domain.ErrInvalidCredentials)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	h := newHarness()

	before := h.hasher.calls()
	_, err := h.svc.Login(context.Background(), "nobody@example.com", password)

	// Same error as a wrong password: don't reveal which emails exist.
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("Login err = %v, want %v", err, domain.ErrInvalidCredentials)
	}
	if errors.Is(err, domain.ErrUserNotFound) {
		t.Error("Login must not leak ErrUserNotFound")
	}
	// And the KDF still ran, so timing doesn't reveal it either.
	if h.hasher.calls() == before {
		t.Error("hasher was not invoked for unknown email; timing side-channel")
	}
}

func TestLogin_InvalidEmailFormat(t *testing.T) {
	h := newHarness()

	_, err := h.svc.Login(context.Background(), "not an email", password)
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("Login err = %v, want %v", err, domain.ErrInvalidCredentials)
	}
}

func TestLogin_Suspended(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)

	u, _ := h.users.FindByID(context.Background(), reg.User.ID)
	u.Suspend(h.clock.Now())
	if err := h.users.Update(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	_, err := h.svc.Login(context.Background(), email, password)
	if !errors.Is(err, domain.ErrUserSuspended) {
		t.Errorf("Login err = %v, want %v", err, domain.ErrUserSuspended)
	}
}

func TestLogin_RepoError(t *testing.T) {
	h := newHarness()
	boom := errors.New("db down")
	h.users.failing = boom

	_, err := h.svc.Login(context.Background(), email, password)
	if !errors.Is(err, boom) {
		t.Errorf("Login err = %v, want wrapped %v", err, boom)
	}
	if errors.Is(err, domain.ErrInvalidCredentials) {
		t.Error("infrastructure failure must not be reported as bad credentials")
	}
}

// --- Refresh --------------------------------------------------------------------

func TestRefresh_Rotates(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	h.clock.Advance(10 * time.Minute)

	tp, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	assertTokenPair(t, tp, h.clock.Now())

	if tp.RefreshToken == reg.Tokens.RefreshToken {
		t.Fatal("Refresh returned the same refresh token; must rotate")
	}
	if tp.AccessToken == reg.Tokens.AccessToken {
		t.Error("Refresh returned the same access token")
	}

	// Old token is dead, new one is the only live session.
	if n := h.tokens.activeCount(reg.User.ID, h.clock.Now()); n != 1 {
		t.Errorf("active refresh tokens = %d, want 1 (old revoked, new created)", n)
	}

	// The new token works (checked before replaying the old one, since a
	// replay is reuse and revokes the whole family — see the reuse test).
	if _, err := h.svc.Refresh(context.Background(), tp.RefreshToken); err != nil {
		t.Errorf("Refresh with rotated token: %v", err)
	}
	if _, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken); !errors.Is(err, domain.ErrRefreshTokenRevoked) {
		t.Errorf("second use of rotated token err = %v, want %v", err, domain.ErrRefreshTokenRevoked)
	}
}

func TestRefresh_ReuseRevokesAllSessions(t *testing.T) {
	// Replaying an already-rotated token means the token leaked (client
	// and attacker both hold it). Kill every session for that user.
	h := newHarness()
	reg := mustRegister(t, h)
	other, err := h.svc.Login(context.Background(), email, password)
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if n := h.tokens.activeCount(reg.User.ID, h.clock.Now()); n != 2 {
		t.Fatalf("precondition: active tokens = %d, want 2", n)
	}

	// Replay of the consumed token.
	_, err = h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if !errors.Is(err, domain.ErrRefreshTokenRevoked) {
		t.Fatalf("replay err = %v, want %v", err, domain.ErrRefreshTokenRevoked)
	}

	if n := h.tokens.activeCount(reg.User.ID, h.clock.Now()); n != 0 {
		t.Errorf("active tokens after reuse = %d, want 0 (all sessions revoked)", n)
	}
	for name, tok := range map[string]string{"rotated": rotated.RefreshToken, "other login": other.Tokens.RefreshToken} {
		if _, err := h.svc.Refresh(context.Background(), tok); !errors.Is(err, domain.ErrRefreshTokenRevoked) {
			t.Errorf("%s token after reuse: err = %v, want %v", name, err, domain.ErrRefreshTokenRevoked)
		}
	}
}

func TestRefresh_Expired(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	h.clock.Advance(refreshTTL)

	_, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if !errors.Is(err, domain.ErrRefreshTokenExpired) {
		t.Errorf("Refresh err = %v, want %v", err, domain.ErrRefreshTokenExpired)
	}
	// Expiry is not reuse; the other sessions must survive.
	if n := h.tokens.activeCount(reg.User.ID, t0); n != 1 {
		t.Errorf("token was revoked on expiry; active at t0 = %d, want 1", n)
	}
}

func TestRefresh_Unknown(t *testing.T) {
	h := newHarness()
	mustRegister(t, h)

	for _, tok := range []string{"", "garbage", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		_, err := h.svc.Refresh(context.Background(), tok)
		if !errors.Is(err, domain.ErrRefreshTokenNotFound) {
			t.Errorf("Refresh(%q) err = %v, want %v", tok, err, domain.ErrRefreshTokenNotFound)
		}
	}
}

func TestRefresh_SuspendedUser(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)

	u, _ := h.users.FindByID(context.Background(), reg.User.ID)
	u.Suspend(h.clock.Now())
	if err := h.users.Update(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	_, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if !errors.Is(err, domain.ErrUserSuspended) {
		t.Errorf("Refresh err = %v, want %v", err, domain.ErrUserSuspended)
	}
}

func TestRefresh_RepoError(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	boom := errors.New("db down")
	h.tokens.failing = boom

	_, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if !errors.Is(err, boom) {
		t.Errorf("Refresh err = %v, want wrapped %v", err, boom)
	}
}

// --- Logout ---------------------------------------------------------------------

func TestLogout(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	other, err := h.svc.Login(context.Background(), email, password)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.svc.Logout(context.Background(), reg.Tokens.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	// Logout is per-session, not per-user: the other session is intact.
	if n := h.tokens.activeCount(reg.User.ID, h.clock.Now()); n != 1 {
		t.Errorf("active refresh tokens after Logout = %d, want 1", n)
	}
	if _, err := h.svc.Refresh(context.Background(), other.Tokens.RefreshToken); err != nil {
		t.Errorf("other session broken by Logout: %v", err)
	}
	// The logged-out session is dead.
	if _, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken); !errors.Is(err, domain.ErrRefreshTokenRevoked) {
		t.Errorf("Refresh after Logout err = %v, want %v", err, domain.ErrRefreshTokenRevoked)
	}
}

func TestLogout_ReplayAfterLogoutIsReuse(t *testing.T) {
	// Once a session is logged out nobody legitimate holds its token, so a
	// later presentation is treated exactly like rotated-token reuse.
	h := newHarness()
	reg := mustRegister(t, h)
	other, err := h.svc.Login(context.Background(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Logout(context.Background(), reg.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken); !errors.Is(err, domain.ErrRefreshTokenRevoked) {
		t.Fatalf("replay err = %v, want %v", err, domain.ErrRefreshTokenRevoked)
	}
	if _, err := h.svc.Refresh(context.Background(), other.Tokens.RefreshToken); !errors.Is(err, domain.ErrRefreshTokenRevoked) {
		t.Errorf("other session after replay: err = %v, want %v", err, domain.ErrRefreshTokenRevoked)
	}
}

func TestLogout_Idempotent(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)

	if err := h.svc.Logout(context.Background(), reg.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Logout(context.Background(), reg.Tokens.RefreshToken); err != nil {
		t.Errorf("second Logout err = %v, want nil", err)
	}
	if err := h.svc.Logout(context.Background(), "never-issued"); err != nil {
		t.Errorf("Logout(unknown) err = %v, want nil", err)
	}
}

func TestLogout_RepoError(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	boom := errors.New("db down")
	h.tokens.failing = boom

	if err := h.svc.Logout(context.Background(), reg.Tokens.RefreshToken); !errors.Is(err, boom) {
		t.Errorf("Logout err = %v, want wrapped %v", err, boom)
	}
}

// --- GetUser --------------------------------------------------------------------

func TestGetUser(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)

	u, err := h.svc.GetUser(context.Background(), reg.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Email != email {
		t.Errorf("Email = %q, want %q", u.Email, email)
	}

	if _, err := h.svc.GetUser(context.Background(), ids.New()); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("GetUser(unknown) err = %v, want %v", err, domain.ErrUserNotFound)
	}
}

func TestGetUser_RepoError(t *testing.T) {
	h := newHarness()
	boom := errors.New("db down")
	h.users.failing = boom

	if _, err := h.svc.GetUser(context.Background(), ids.New()); !errors.Is(err, boom) {
		t.Errorf("GetUser err = %v, want wrapped %v", err, boom)
	}
}

// --- Infrastructure failures mid-flow ---------------------------------------------

func TestRegister_TokenRepoError(t *testing.T) {
	h := newHarness()
	boom := errors.New("db down")
	h.tokens.failing = boom

	_, err := h.svc.Register(context.Background(), email, password)
	if !errors.Is(err, boom) {
		t.Errorf("Register err = %v, want wrapped %v", err, boom)
	}
}

func TestRefresh_UserRepoError(t *testing.T) {
	h := newHarness()
	reg := mustRegister(t, h)
	boom := errors.New("db down")
	h.users.failing = boom

	_, err := h.svc.Refresh(context.Background(), reg.Tokens.RefreshToken)
	if !errors.Is(err, boom) {
		t.Errorf("Refresh err = %v, want wrapped %v", err, boom)
	}
}

// --- Config -----------------------------------------------------------------------

func TestNewAuthService_DefaultsRefreshTTL(t *testing.T) {
	h := newHarness()
	svc := app.NewAuthService(app.AuthDeps{
		Users: h.users, Tokens: h.tokens, Hasher: h.hasher, Issuer: h.issuer, Clock: h.clock,
	}, app.AuthConfig{})

	res, err := svc.Register(context.Background(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Tokens.RefreshExpiresAt.After(t0) {
		t.Errorf("zero RefreshTokenTTL produced non-positive lifetime: expires %v", res.Tokens.RefreshExpiresAt)
	}
}
