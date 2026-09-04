package httpapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andytrue7/coinly/pkg/httpx"
	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/pkg/jwtx"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/in/httpapi"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/out/token"
	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/app/apptest"
)

const (
	email    = "alice@example.com"
	password = "s3cret-pass"
)

var t0 = time.Date(2026, 9, 4, 14, 0, 0, 0, time.UTC)

type env struct {
	h      http.Handler
	clock  *apptest.Clock
	users  *apptest.UserRepo
	tokens *apptest.TokenRepo
	issuer *token.Issuer
	signer *jwtx.Signer // same key as issuer, for minting odd tokens
}

// newEnv wires the real AuthService, a real Ed25519 issuer and the real
// verifier over in-memory repos, so tests exercise actual JWTs end to end.
func newEnv(t *testing.T) *env {
	t.Helper()
	priv, err := jwtx.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := token.Config{Issuer: "coinly-identity", Audience: "coinly", AccessTokenTTL: 15 * time.Minute}
	issuer := token.NewIssuer(priv, cfg)

	e := &env{
		clock:  apptest.NewClock(t0),
		users:  apptest.NewUserRepo(),
		tokens: apptest.NewTokenRepo(),
		issuer: issuer,
		signer: jwtx.NewSigner(priv, jwtx.SignerConfig{Issuer: cfg.Issuer, Audience: cfg.Audience}),
	}
	auth := app.NewAuthService(app.AuthDeps{
		Users: e.users, Tokens: e.tokens, Hasher: &apptest.Hasher{}, Issuer: issuer, Clock: e.clock,
	}, app.AuthConfig{RefreshTokenTTL: 30 * 24 * time.Hour})

	verifier := jwtx.NewVerifier(jwtx.NewStaticKeySet(issuer.JWKS().Keys...),
		jwtx.VerifierConfig{Issuer: cfg.Issuer, Audience: cfg.Audience})

	e.h = httpapi.NewHandler(httpapi.Deps{
		Auth:     auth,
		JWKS:     issuer.JWKS(),
		Verifier: verifier,
		Clock:    e.clock,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return e
}

func (e *env) do(t *testing.T, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if s, ok := body.(string); ok {
		r = strings.NewReader(s)
	} else if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("body is not JSON (%v): %s", err, rec.Body)
	}
	return v
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[httpx.ErrorBody](t, rec).Error.Code
}

func (e *env) register(t *testing.T) httpapi.AuthResponse {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/v1/auth/register", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d: %s", rec.Code, rec.Body)
	}
	return decode[httpapi.AuthResponse](t, rec)
}

// --- register --------------------------------------------------------------------

func TestRegister(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodPost, "/v1/auth/register", map[string]string{"email": " Alice@Example.com ", "password": password})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	res := decode[httpapi.AuthResponse](t, rec)

	if res.User.Email != email {
		t.Errorf("user.email = %q, want normalized %q", res.User.Email, email)
	}
	if res.User.ID == "" || res.User.Status != "active" || res.User.CreatedAt.IsZero() {
		t.Errorf("user = %+v; want id, active status, created_at", res.User)
	}
	if res.Tokens.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", res.Tokens.TokenType)
	}
	if res.Tokens.AccessToken == "" || res.Tokens.RefreshToken == "" {
		t.Errorf("tokens = %+v; want both tokens", res.Tokens)
	}
	if want := t0.Add(15 * time.Minute); !res.Tokens.AccessTokenExpiresAt.Equal(want) {
		t.Errorf("access_token_expires_at = %v, want %v", res.Tokens.AccessTokenExpiresAt, want)
	}
	if want := t0.Add(30 * 24 * time.Hour); !res.Tokens.RefreshTokenExpiresAt.Equal(want) {
		t.Errorf("refresh_token_expires_at = %v, want %v", res.Tokens.RefreshTokenExpiresAt, want)
	}

	// Never leak credential material.
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("response mentions password: %s", rec.Body)
	}
}

func TestRegister_Errors(t *testing.T) {
	e := newEnv(t)
	e.register(t)

	tests := []struct {
		name     string
		body     any
		wantCode int
		wantErr  string
	}{
		{name: "duplicate email", body: map[string]string{"email": "ALICE@example.com", "password": password}, wantCode: 409, wantErr: "email_taken"},
		{name: "invalid email", body: map[string]string{"email": "nope", "password": password}, wantCode: 400, wantErr: "invalid_email"},
		{name: "weak password", body: map[string]string{"email": "bob@example.com", "password": "short"}, wantCode: 400, wantErr: "weak_password"},
		{name: "password too long", body: map[string]string{"email": "bob@example.com", "password": strings.Repeat("x", 129)}, wantCode: 400, wantErr: "password_too_long"},
		{name: "malformed json", body: `{"email":`, wantCode: 400, wantErr: "bad_request"},
		{name: "unknown field", body: `{"email":"bob@example.com","password":"s3cret-pass","admin":true}`, wantCode: 400, wantErr: "bad_request"},
		{name: "empty body", body: ``, wantCode: 400, wantErr: "bad_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.do(t, http.MethodPost, "/v1/auth/register", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errCode(t, rec); got != tc.wantErr {
				t.Errorf("error.code = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

// --- login -----------------------------------------------------------------------

func TestLogin(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)
	e.clock.Advance(time.Hour)

	rec := e.do(t, http.MethodPost, "/v1/auth/login", map[string]string{"email": "ALICE@example.com", "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	res := decode[httpapi.AuthResponse](t, rec)
	if res.User.ID != reg.User.ID {
		t.Errorf("user.id = %q, want %q", res.User.ID, reg.User.ID)
	}
	if res.Tokens.RefreshToken == reg.Tokens.RefreshToken {
		t.Error("login reused the registration refresh token")
	}
}

func TestLogin_Errors(t *testing.T) {
	e := newEnv(t)
	e.register(t)

	tests := []struct {
		name     string
		body     map[string]string
		wantCode int
		wantErr  string
	}{
		{name: "wrong password", body: map[string]string{"email": email, "password": "wrong"}, wantCode: 401, wantErr: "invalid_credentials"},
		{name: "unknown email", body: map[string]string{"email": "nobody@example.com", "password": password}, wantCode: 401, wantErr: "invalid_credentials"},
		{name: "malformed email", body: map[string]string{"email": "nope", "password": password}, wantCode: 401, wantErr: "invalid_credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.do(t, http.MethodPost, "/v1/auth/login", tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errCode(t, rec); got != tc.wantErr {
				t.Errorf("error.code = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestLogin_Suspended(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)
	suspend(t, e, reg.User.ID)

	rec := e.do(t, http.MethodPost, "/v1/auth/login", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body)
	}
	if got := errCode(t, rec); got != "user_suspended" {
		t.Errorf("error.code = %q, want user_suspended", got)
	}
}

// --- refresh ---------------------------------------------------------------------

func TestRefresh(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)
	e.clock.Advance(10 * time.Minute)

	rec := e.do(t, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": reg.Tokens.RefreshToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	res := decode[httpapi.RefreshResponse](t, rec)
	if res.Tokens.RefreshToken == "" || res.Tokens.RefreshToken == reg.Tokens.RefreshToken {
		t.Errorf("refresh_token = %q; want a rotated token", res.Tokens.RefreshToken)
	}
	if res.Tokens.AccessToken == reg.Tokens.AccessToken {
		t.Error("access token was not reissued")
	}

	// The new access token authenticates.
	me := e.do(t, http.MethodGet, "/v1/users/me", nil, "Authorization", "Bearer "+res.Tokens.AccessToken)
	if me.Code != http.StatusOK {
		t.Errorf("/me with refreshed token: status = %d: %s", me.Code, me.Body)
	}

	// The old refresh token is dead.
	rec = e.do(t, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": reg.Tokens.RefreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("replayed refresh: status = %d, want 401: %s", rec.Code, rec.Body)
	}
	if got := errCode(t, rec); got != "invalid_refresh_token" {
		t.Errorf("error.code = %q, want invalid_refresh_token", got)
	}
}

func TestRefresh_Errors(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)

	t.Run("unknown token", func(t *testing.T) {
		rec := e.do(t, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": "never-issued"})
		if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "invalid_refresh_token" {
			t.Errorf("status = %d body = %s; want 401 invalid_refresh_token", rec.Code, rec.Body)
		}
	})
	t.Run("missing token", func(t *testing.T) {
		rec := e.do(t, http.MethodPost, "/v1/auth/refresh", map[string]string{})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
	})
	t.Run("expired", func(t *testing.T) {
		e.clock.Advance(31 * 24 * time.Hour)
		rec := e.do(t, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": reg.Tokens.RefreshToken})
		if rec.Code != http.StatusUnauthorized || errCode(t, rec) != "invalid_refresh_token" {
			t.Errorf("status = %d body = %s; want 401 invalid_refresh_token", rec.Code, rec.Body)
		}
	})
}

// --- logout ----------------------------------------------------------------------

func TestLogout(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)

	rec := e.do(t, http.MethodPost, "/v1/auth/logout", map[string]string{"refresh_token": reg.Tokens.RefreshToken})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 with body: %s", rec.Body)
	}

	// Idempotent.
	rec = e.do(t, http.MethodPost, "/v1/auth/logout", map[string]string{"refresh_token": reg.Tokens.RefreshToken})
	if rec.Code != http.StatusNoContent {
		t.Errorf("second logout: status = %d, want 204", rec.Code)
	}
	rec = e.do(t, http.MethodPost, "/v1/auth/logout", map[string]string{"refresh_token": "never-issued"})
	if rec.Code != http.StatusNoContent {
		t.Errorf("logout unknown: status = %d, want 204", rec.Code)
	}
}

// --- me ---------------------------------------------------------------------------

func TestMe(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)

	rec := e.do(t, http.MethodGet, "/v1/users/me", nil, "Authorization", "Bearer "+reg.Tokens.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	res := decode[httpapi.UserResponse](t, rec)
	if res.ID != reg.User.ID || res.Email != email || res.Status != "active" {
		t.Errorf("me = %+v, want registered user", res)
	}
}

func TestMe_Unauthorized(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)

	tests := []struct {
		name string
		hdr  string
		at   time.Duration
	}{
		{name: "no header"},
		{name: "garbage", hdr: "Bearer nope"},
		{name: "expired", hdr: "Bearer " + reg.Tokens.AccessToken, at: time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e.clock.Advance(tc.at)
			defer e.clock.Advance(-tc.at)
			var rec *httptest.ResponseRecorder
			if tc.hdr == "" {
				rec = e.do(t, http.MethodGet, "/v1/users/me", nil)
			} else {
				rec = e.do(t, http.MethodGet, "/v1/users/me", nil, "Authorization", tc.hdr)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestMe_DeletedUser(t *testing.T) {
	// Valid token, but the user is gone: 401, not 404 — there's nothing
	// to "find", the credential simply no longer maps to anyone.
	e := newEnv(t)
	reg := e.register(t)
	id, err := ids.Parse(reg.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.users.Delete(id)

	rec := e.do(t, http.MethodGet, "/v1/users/me", nil, "Authorization", "Bearer "+reg.Tokens.AccessToken)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
	}
}

func TestMe_NonUUIDSubject(t *testing.T) {
	// A validly signed token whose subject isn't a user ID is still not a
	// usable credential.
	e := newEnv(t)
	tok, _, err := e.signer.Sign("not-a-uuid", t0, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rec := e.do(t, http.MethodGet, "/v1/users/me", nil, "Authorization", "Bearer "+tok)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
	}
}

func TestLogout_Errors(t *testing.T) {
	e := newEnv(t)
	reg := e.register(t)

	if rec := e.do(t, http.MethodPost, "/v1/auth/logout", `{"refresh_token":`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed: status = %d, want 400", rec.Code)
	}

	e.tokens.Failing = errors.New("db down")
	rec := e.do(t, http.MethodPost, "/v1/auth/logout", map[string]string{"refresh_token": reg.Tokens.RefreshToken})
	if rec.Code != http.StatusInternalServerError || errCode(t, rec) != "internal" {
		t.Errorf("repo failure: status = %d body = %s; want 500 internal", rec.Code, rec.Body)
	}
}

// --- jwks / health ----------------------------------------------------------------

func TestJWKS(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/.well-known/jwks.json", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("Cache-Control = %q, want a max-age", cc)
	}
	set := decode[jwtx.JWKS](t, rec)
	if len(set.Keys) != 1 || set.Keys[0].Kid != e.issuer.JWKS().Keys[0].Kid {
		t.Errorf("jwks = %+v, want the issuer's key", set)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t, http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// --- infrastructure errors ----------------------------------------------------------

func TestInternalError_IsOpaque(t *testing.T) {
	e := newEnv(t)
	e.users.Failing = errors.New("pq: connection refused to 10.0.0.7:5432")

	rec := e.do(t, http.MethodPost, "/v1/auth/register", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body)
	}
	if got := errCode(t, rec); got != "internal" {
		t.Errorf("error.code = %q, want internal", got)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Errorf("internal details leaked: %s", rec.Body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t, http.MethodGet, "/v1/auth/register", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET register: status = %d, want 405", rec.Code)
	}
	if rec := e.do(t, http.MethodGet, "/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown route: status = %d, want 404", rec.Code)
	}
}

// suspend flips the stored user to suspended.
func suspend(t *testing.T, e *env, id string) {
	t.Helper()
	uid, err := ids.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	u, err := e.users.FindByID(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	u.Suspend(e.clock.Now())
	if err := e.users.Update(t.Context(), u); err != nil {
		t.Fatal(err)
	}
}
