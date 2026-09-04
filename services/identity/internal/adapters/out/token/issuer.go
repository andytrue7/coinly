// Package token adapts pkg/jwtx to the app.AccessTokenIssuer port and
// exposes the JWKS the REST adapter publishes.
package token

import (
	"crypto/ed25519"
	"time"

	"github.com/andytrue7/coinly/pkg/jwtx"
	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// DefaultAccessTokenTTL is used when Config.AccessTokenTTL is zero.
const DefaultAccessTokenTTL = 15 * time.Minute

// Config tunes the issuer.
type Config struct {
	Issuer         string
	Audience       string
	AccessTokenTTL time.Duration
}

// Issuer mints access tokens for users.
type Issuer struct {
	signer *jwtx.Signer
	jwks   jwtx.JWKS
	ttl    time.Duration
}

// NewIssuer returns an Issuer signing with priv.
func NewIssuer(priv ed25519.PrivateKey, cfg Config) *Issuer {
	ttl := cfg.AccessTokenTTL
	if ttl <= 0 {
		ttl = DefaultAccessTokenTTL
	}
	pub, _ := priv.Public().(ed25519.PublicKey)
	return &Issuer{
		signer: jwtx.NewSigner(priv, jwtx.SignerConfig{Issuer: cfg.Issuer, Audience: cfg.Audience}),
		jwks:   jwtx.JWKS{Keys: []jwtx.JWK{jwtx.PublicJWK(pub)}},
		ttl:    ttl,
	}
}

// Issue implements app.AccessTokenIssuer: the token's subject is the
// user's ID.
func (i *Issuer) Issue(user *domain.User, now time.Time) (string, time.Time, error) {
	return i.signer.Sign(user.ID.String(), now, i.ttl)
}

// JWKS returns the public key set that verifies this issuer's tokens.
func (i *Issuer) JWKS() jwtx.JWKS {
	return i.jwks
}
