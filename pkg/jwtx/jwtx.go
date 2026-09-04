// Package jwtx issues and verifies Coinly's access tokens: EdDSA
// (Ed25519) JWTs with RFC 7638 thumbprint key IDs, published as a JWKS so
// every service can verify locally without calling identity.
//
// Only EdDSA is ever accepted. Tokens using any other algorithm — "none",
// HMAC with the public key as secret, RSA — are rejected before any
// signature check, which closes the classic algorithm-confusion attacks.
package jwtx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors. Every verification failure wraps ErrInvalidToken;
// ErrExpired and ErrUnknownKey additionally identify the two causes a
// caller may want to distinguish (e.g. to refresh, or to refetch a JWKS).
var (
	ErrInvalidToken = errors.New("jwtx: invalid token")
	ErrExpired      = fmt.Errorf("%w: expired", ErrInvalidToken)
	ErrUnknownKey   = fmt.Errorf("%w: unknown key id", ErrInvalidToken)
)

// DefaultLeeway tolerates small clock skew between issuer and verifier.
const DefaultLeeway = 30 * time.Second

// Claims are the registered claims Coinly tokens carry. Subject is the
// user ID.
type Claims struct {
	jwt.RegisteredClaims
}

// SignerConfig identifies the issuer and intended audience.
type SignerConfig struct {
	Issuer   string
	Audience string
}

// Signer mints tokens with one Ed25519 private key.
type Signer struct {
	priv ed25519.PrivateKey
	kid  string
	cfg  SignerConfig
}

// NewSigner returns a Signer for priv; the token's kid is the thumbprint
// of the matching public key.
func NewSigner(priv ed25519.PrivateKey, cfg SignerConfig) *Signer {
	pub, _ := priv.Public().(ed25519.PublicKey)
	return &Signer{priv: priv, kid: Thumbprint(pub), cfg: cfg}
}

// Sign issues a token for subject valid from now for ttl. It returns the
// compact serialization and the expiry.
func (s *Signer) Sign(subject string, now time.Time, ttl time.Duration) (string, time.Time, error) {
	if subject == "" {
		return "", time.Time{}, errors.New("jwtx: empty subject")
	}
	if ttl <= 0 {
		return "", time.Time{}, errors.New("jwtx: ttl must be positive")
	}

	jti, err := randomID()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := now.Add(ttl)

	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    s.cfg.Issuer,
		Subject:   subject,
		Audience:  jwt.ClaimStrings{s.cfg.Audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
		ID:        jti,
	}}

	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = s.kid

	signed, err := tok.SignedString(s.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwtx: sign: %w", err)
	}
	return signed, exp, nil
}

// KeyProvider resolves a key id to the public key that should verify it.
// StaticKeySet is the in-memory implementation; a JWKS-fetching cache is
// another.
type KeyProvider interface {
	PublicKey(kid string) (ed25519.PublicKey, error)
}

// VerifierConfig pins the issuer and audience a token must carry. Leeway
// zero selects DefaultLeeway.
type VerifierConfig struct {
	Issuer   string
	Audience string
	Leeway   time.Duration
}

// Verifier checks tokens against a KeyProvider.
type Verifier struct {
	keys KeyProvider
	cfg  VerifierConfig
}

// NewVerifier returns a Verifier trusting keys.
func NewVerifier(keys KeyProvider, cfg VerifierConfig) *Verifier {
	if cfg.Leeway <= 0 {
		cfg.Leeway = DefaultLeeway
	}
	return &Verifier{keys: keys, cfg: cfg}
}

// Verify parses and validates token as of now, returning its claims.
func (v *Verifier) Verify(token string, now time.Time) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, v.keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(v.cfg.Leeway),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return nil, classify(err)
	}
	return &claims, nil
}

func (v *Verifier) keyFunc(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("%w: missing kid", ErrInvalidToken)
	}
	return v.keys.PublicKey(kid)
}

// classify maps library errors onto the package sentinels.
func classify(err error) error {
	switch {
	case errors.Is(err, ErrInvalidToken):
		// Already one of ours (from keyFunc), possibly ErrUnknownKey.
		return err
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("%w: %w", ErrExpired, err)
	default:
		return fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("jwtx: generate jti: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
