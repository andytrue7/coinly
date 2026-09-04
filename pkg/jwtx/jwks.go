package jwtx

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// JWK is an Ed25519 public key in JSON Web Key form (RFC 7517 + RFC 8037,
// "OKP" key type). Only public components exist here by construction.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	X   string `json:"x"`
}

// JWKS is the document served at /.well-known/jwks.json.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// PublicJWK renders pub as a signing JWK keyed by its thumbprint.
func PublicJWK(pub ed25519.PublicKey) JWK {
	return JWK{
		Kty: "OKP",
		Crv: "Ed25519",
		Kid: Thumbprint(pub),
		Use: "sig",
		Alg: "EdDSA",
		X:   base64.RawURLEncoding.EncodeToString(pub),
	}
}

// PublicKey decodes the JWK back into an Ed25519 public key.
func (k JWK) PublicKey() (ed25519.PublicKey, error) {
	if k.Kty != "OKP" || k.Crv != "Ed25519" {
		return nil, fmt.Errorf("jwtx: unsupported JWK %s/%s", k.Kty, k.Crv)
	}
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("jwtx: decode JWK x: %w", err)
	}
	if len(x) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("jwtx: JWK x is %d bytes, want %d", len(x), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(x), nil
}

// Thumbprint computes the RFC 7638 JWK thumbprint of pub: base64url of
// SHA-256 over the required members in lexicographic order with no
// whitespace. It is stable across implementations, so a key rotated into
// a JWKS by any tool gets the same kid.
func Thumbprint(pub ed25519.PublicKey) string {
	canonical := `{"crv":"Ed25519","kty":"OKP","x":"` + base64.RawURLEncoding.EncodeToString(pub) + `"}`
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// StaticKeySet is a fixed, in-memory KeyProvider.
type StaticKeySet struct {
	keys map[string]ed25519.PublicKey
}

// NewStaticKeySet builds a key set from JWKs; malformed entries are
// skipped rather than failing the whole set, so one bad key in a JWKS
// can't take down verification for the good ones.
func NewStaticKeySet(jwks ...JWK) *StaticKeySet {
	s := &StaticKeySet{keys: make(map[string]ed25519.PublicKey, len(jwks))}
	for _, k := range jwks {
		pub, err := k.PublicKey()
		if err != nil {
			continue
		}
		s.keys[k.Kid] = pub
	}
	return s
}

// PublicKey implements KeyProvider.
func (s *StaticKeySet) PublicKey(kid string) (ed25519.PublicKey, error) {
	pub, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKey, kid)
	}
	return pub, nil
}

var _ KeyProvider = (*StaticKeySet)(nil)

// errNotEd25519 is returned when a parsed key is some other type.
var errNotEd25519 = errors.New("jwtx: key is not Ed25519")
