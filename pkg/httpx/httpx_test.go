package httpx_test

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/andytrue7/coinly/pkg/httpx"
	"github.com/andytrue7/coinly/pkg/jwtx"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteJSON(rec, http.StatusCreated, map[string]any{"ok": true})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rec.Body)
	}
	if body["ok"] != true {
		t.Errorf("body = %v", body)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteError(rec, http.StatusConflict, "email_taken", "email already registered")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	var body httpx.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "email_taken" || body.Error.Message != "email already registered" {
		t.Errorf("body = %+v", body)
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Email string `json:"email"`
	}

	tests := []struct {
		name    string
		body    string
		wantErr bool
		want    string
	}{
		{name: "ok", body: `{"email":"a@b.c"}`, want: "a@b.c"},
		{name: "empty body", body: ``, wantErr: true},
		{name: "malformed", body: `{"email":`, wantErr: true},
		{name: "unknown field", body: `{"email":"a@b.c","admin":true}`, wantErr: true},
		{name: "two documents", body: `{"email":"a@b.c"}{"email":"x"}`, wantErr: true},
		{name: "array not object", body: `["a@b.c"]`, wantErr: true},
		{name: "too large", body: `{"email":"` + strings.Repeat("a", 2000) + `"}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			var p payload
			err := httpx.DecodeJSON(httptest.NewRecorder(), req, &p, 1024)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DecodeJSON(%q) = %+v, want error", tc.body, p)
				}
				if !errors.Is(err, httpx.ErrBadRequest) {
					t.Errorf("err = %v, want ErrBadRequest", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeJSON: %v", err)
			}
			if p.Email != tc.want {
				t.Errorf("Email = %q, want %q", p.Email, tc.want)
			}
		})
	}
}

// --- auth middleware ----------------------------------------------------------

const (
	issuer   = "coinly-identity"
	audience = "coinly"
	subject  = "user-123"
)

var now = time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)

func newAuth(t *testing.T) (*jwtx.Signer, func(http.Handler) http.Handler) {
	t.Helper()
	priv, err := jwtx.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	signer := jwtx.NewSigner(priv, jwtx.SignerConfig{Issuer: issuer, Audience: audience})
	verifier := jwtx.NewVerifier(jwtx.NewStaticKeySet(jwtx.PublicJWK(pub)),
		jwtx.VerifierConfig{Issuer: issuer, Audience: audience})
	return signer, httpx.RequireAuth(verifier, func() time.Time { return now })
}

// echoSubject is a protected handler that reports the authenticated subject.
var echoSubject = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	sub, ok := httpx.Subject(r.Context())
	if !ok {
		http.Error(w, "no subject in context", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(sub))
})

func TestRequireAuth_Accepts(t *testing.T) {
	signer, mw := newAuth(t)
	token, _, err := signer.Sign(subject, now, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mw(echoSubject).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if rec.Body.String() != subject {
		t.Errorf("subject = %q, want %q", rec.Body.String(), subject)
	}
}

func TestRequireAuth_Rejects(t *testing.T) {
	signer, mw := newAuth(t)
	good, _, _ := signer.Sign(subject, now, 15*time.Minute)
	expired, _, _ := signer.Sign(subject, now.Add(-2*time.Hour), 15*time.Minute)

	otherPriv, _ := jwtx.GeneratePrivateKey()
	foreign, _, _ := jwtx.NewSigner(otherPriv, jwtx.SignerConfig{Issuer: issuer, Audience: audience}).
		Sign(subject, now, 15*time.Minute)

	tests := []struct {
		name   string
		header string
	}{
		{name: "missing header", header: ""},
		{name: "not bearer", header: "Basic " + good},
		{name: "bearer no token", header: "Bearer "},
		{name: "garbage token", header: "Bearer nope"},
		{name: "expired", header: "Bearer " + expired},
		{name: "foreign key", header: "Bearer " + foreign},
		{name: "alg none", header: "Bearer " + noneToken(t)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			mw(echoSubject).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
			if wa := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(wa, "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want Bearer challenge", wa)
			}
			var body httpx.ErrorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body not JSON: %s", rec.Body)
			}
			if body.Error.Code != "unauthorized" {
				t.Errorf("error code = %q, want unauthorized", body.Error.Code)
			}
		})
	}
}

func TestRequireAuth_SchemeCaseInsensitive(t *testing.T) {
	// RFC 6750 / RFC 9110: the auth scheme is case-insensitive.
	signer, mw := newAuth(t)
	token, _, _ := signer.Sign(subject, now, 15*time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()
	mw(echoSubject).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestSubject_Absent(t *testing.T) {
	if sub, ok := httpx.Subject(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok || sub != "" {
		t.Errorf("Subject(empty ctx) = %q, %v; want \"\", false", sub, ok)
	}
}

func noneToken(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": subject, "iss": issuer, "aud": audience,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
