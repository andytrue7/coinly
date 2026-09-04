package token_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/andytrue7/coinly/pkg/ids"
	"github.com/andytrue7/coinly/pkg/jwtx"
	"github.com/andytrue7/coinly/services/identity/internal/adapters/out/token"
	"github.com/andytrue7/coinly/services/identity/internal/app"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

var now = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

func TestIssuer_Issue(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := token.Config{Issuer: "coinly-identity", Audience: "coinly", AccessTokenTTL: 15 * time.Minute}
	issuer := token.NewIssuer(priv, cfg)

	user := &domain.User{ID: ids.New(), Email: "alice@example.com", Status: domain.UserStatusActive}
	tok, exp, err := issuer.Issue(user, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if want := now.Add(cfg.AccessTokenTTL); !exp.Equal(want) {
		t.Errorf("exp = %v, want %v", exp, want)
	}

	// Verifiable with the public half via the shared verifier and JWKS.
	verifier := jwtx.NewVerifier(jwtx.NewStaticKeySet(issuer.JWKS().Keys...),
		jwtx.VerifierConfig{Issuer: cfg.Issuer, Audience: cfg.Audience})
	claims, err := verifier.Verify(tok, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != user.ID.String() {
		t.Errorf("Subject = %q, want user id %q", claims.Subject, user.ID)
	}
	if got := issuer.JWKS().Keys[0].Kid; got != jwtx.Thumbprint(pub) {
		t.Errorf("JWKS kid = %q, want %q", got, jwtx.Thumbprint(pub))
	}
	if len(issuer.JWKS().Keys) != 1 {
		t.Errorf("JWKS has %d keys, want 1", len(issuer.JWKS().Keys))
	}
}

func TestIssuer_DefaultTTL(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	issuer := token.NewIssuer(priv, token.Config{Issuer: "i", Audience: "a"})

	_, exp, err := issuer.Issue(&domain.User{ID: ids.New()}, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(token.DefaultAccessTokenTTL); !exp.Equal(want) {
		t.Errorf("exp = %v, want default %v", exp, want)
	}
}

// Compile-time check that the adapter satisfies the app port.
var _ app.AccessTokenIssuer = (*token.Issuer)(nil)
