package jwtx_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/andytrue7/coinly/pkg/jwtx"
)

const (
	issuer   = "coinly-identity"
	audience = "coinly"
	subject  = "0192a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	ttl      = 15 * time.Minute
)

var now = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

type fixture struct {
	signer   *jwtx.Signer
	verifier *jwtx.Verifier
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
}

// setup returns a signer and a verifier that trusts exactly the signer's key.
func setup(t *testing.T) fixture {
	t.Helper()
	pub, priv := newKey(t)
	signer := jwtx.NewSigner(priv, jwtx.SignerConfig{Issuer: issuer, Audience: audience})
	keys := jwtx.NewStaticKeySet(jwtx.PublicJWK(pub))
	verifier := jwtx.NewVerifier(keys, jwtx.VerifierConfig{Issuer: issuer, Audience: audience})
	return fixture{signer: signer, verifier: verifier, pub: pub, priv: priv}
}

func TestSignAndVerify(t *testing.T) {
	f := setup(t)
	signer, verifier, pub := f.signer, f.verifier, f.pub

	token, exp, err := signer.Sign(subject, now, ttl)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if want := now.Add(ttl); !exp.Equal(want) {
		t.Errorf("exp = %v, want %v", exp, want)
	}
	if parts := strings.Split(token, "."); len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3: %q", len(parts), token)
	}

	claims, err := verifier.Verify(token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != subject {
		t.Errorf("Subject = %q, want %q", claims.Subject, subject)
	}
	if claims.Issuer != issuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, issuer)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != audience {
		t.Errorf("Audience = %v, want [%q]", claims.Audience, audience)
	}
	if claims.ID == "" {
		t.Error("ID (jti) is empty; tokens must be individually identifiable")
	}
	if claims.IssuedAt == nil || !claims.IssuedAt.Equal(now) {
		t.Errorf("IssuedAt = %v, want %v", claims.IssuedAt, now)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt = %v, want %v", claims.ExpiresAt, exp)
	}

	// Header carries the key id and the only algorithm we ever use.
	hdr := decodeSegment(t, strings.Split(token, ".")[0])
	if hdr["alg"] != "EdDSA" {
		t.Errorf("alg = %v, want EdDSA", hdr["alg"])
	}
	if hdr["kid"] != jwtx.Thumbprint(pub) {
		t.Errorf("kid = %v, want thumbprint %q", hdr["kid"], jwtx.Thumbprint(pub))
	}
}

func TestSign_UniqueJTI(t *testing.T) {
	f := setup(t)
	a, _, _ := f.signer.Sign(subject, now, ttl)
	b, _, _ := f.signer.Sign(subject, now, ttl)
	ca, _ := f.verifier.Verify(a, now)
	cb, _ := f.verifier.Verify(b, now)
	if ca.ID == cb.ID {
		t.Errorf("two tokens share jti %q", ca.ID)
	}
}

func TestSign_Validation(t *testing.T) {
	signer := setup(t).signer
	if _, _, err := signer.Sign("", now, ttl); err == nil {
		t.Error("Sign(empty subject) err = nil, want error")
	}
	if _, _, err := signer.Sign(subject, now, 0); err == nil {
		t.Error("Sign(zero ttl) err = nil, want error")
	}
}

func TestVerify_Rejects(t *testing.T) {
	f := setup(t)
	signer, verifier, pub, priv := f.signer, f.verifier, f.pub, f.priv
	good, _, err := signer.Sign(subject, now, ttl)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv := newKey(t)

	tests := []struct {
		name  string
		token string
		at    time.Time
		want  error
	}{
		{
			name:  "expired",
			token: good,
			at:    now.Add(ttl + time.Minute),
			want:  jwtx.ErrExpired,
		},
		{
			name:  "signed by untrusted key",
			token: mustSign(t, jwtx.NewSigner(otherPriv, jwtx.SignerConfig{Issuer: issuer, Audience: audience})),
			at:    now,
			want:  jwtx.ErrUnknownKey,
		},
		{
			name:  "wrong issuer",
			token: mustSign(t, jwtx.NewSigner(priv, jwtx.SignerConfig{Issuer: "evil", Audience: audience})),
			at:    now,
			want:  jwtx.ErrInvalidToken,
		},
		{
			name:  "wrong audience",
			token: mustSign(t, jwtx.NewSigner(priv, jwtx.SignerConfig{Issuer: issuer, Audience: "other-app"})),
			at:    now,
			want:  jwtx.ErrInvalidToken,
		},
		{
			name:  "tampered payload",
			token: tamper(t, good),
			at:    now,
			want:  jwtx.ErrInvalidToken,
		},
		{
			name:  "alg none",
			token: unsigned(t),
			at:    now,
			want:  jwtx.ErrInvalidToken,
		},
		{
			name:  "HS256 with public key as secret (algorithm confusion)",
			token: hmacConfusion(t, pub),
			at:    now,
			want:  jwtx.ErrInvalidToken,
		},
		{
			name:  "missing kid",
			token: withoutKid(t, priv),
			at:    now,
			want:  jwtx.ErrInvalidToken,
		},
		{name: "garbage", token: "not.a.jwt", at: now, want: jwtx.ErrInvalidToken},
		{name: "empty", token: "", at: now, want: jwtx.ErrInvalidToken},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := verifier.Verify(tc.token, tc.at)
			if err == nil {
				t.Fatalf("Verify = %+v, want error %v", claims, tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("Verify err = %v, want %v", err, tc.want)
			}
			// Every rejection, whatever the cause, is also ErrInvalidToken
			// so callers can treat them uniformly.
			if !errors.Is(err, jwtx.ErrInvalidToken) {
				t.Errorf("Verify err = %v does not wrap ErrInvalidToken", err)
			}
		})
	}
}

func TestVerify_Leeway(t *testing.T) {
	f := setup(t)
	signer, verifier := f.signer, f.verifier
	token, exp, _ := signer.Sign(subject, now, ttl)

	// A few seconds of clock skew must not break a token that just expired
	// or was just issued by a slightly-ahead issuer.
	if _, err := verifier.Verify(token, exp.Add(10*time.Second)); err != nil {
		t.Errorf("Verify 10s after exp: %v, want nil (leeway)", err)
	}
	if _, err := verifier.Verify(token, now.Add(-10*time.Second)); err != nil {
		t.Errorf("Verify 10s before iat: %v, want nil (leeway)", err)
	}
	if _, err := verifier.Verify(token, exp.Add(2*time.Minute)); !errors.Is(err, jwtx.ErrExpired) {
		t.Errorf("Verify 2m after exp: %v, want %v", err, jwtx.ErrExpired)
	}
}

func TestVerify_KeyRotation(t *testing.T) {
	// Verifier trusts two keys; tokens from either verify, tokens from a
	// third don't.
	pubA, privA := newKey(t)
	pubB, privB := newKey(t)
	_, privC := newKey(t)

	keys := jwtx.NewStaticKeySet(jwtx.PublicJWK(pubA), jwtx.PublicJWK(pubB))
	verifier := jwtx.NewVerifier(keys, jwtx.VerifierConfig{Issuer: issuer, Audience: audience})
	cfg := jwtx.SignerConfig{Issuer: issuer, Audience: audience}

	for name, priv := range map[string]ed25519.PrivateKey{"A": privA, "B": privB} {
		if _, err := verifier.Verify(mustSign(t, jwtx.NewSigner(priv, cfg)), now); err != nil {
			t.Errorf("token from key %s: %v", name, err)
		}
	}
	if _, err := verifier.Verify(mustSign(t, jwtx.NewSigner(privC, cfg)), now); !errors.Is(err, jwtx.ErrUnknownKey) {
		t.Errorf("token from key C: err = %v, want %v", err, jwtx.ErrUnknownKey)
	}
}

// --- JWKS ---------------------------------------------------------------------

func TestPublicJWK(t *testing.T) {
	pub, _ := newKey(t)
	jwk := jwtx.PublicJWK(pub)

	if jwk.Kty != "OKP" || jwk.Crv != "Ed25519" || jwk.Alg != "EdDSA" || jwk.Use != "sig" {
		t.Errorf("JWK = %+v, want OKP/Ed25519/EdDSA/sig", jwk)
	}
	x, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		t.Fatalf("X is not base64url: %v", err)
	}
	if !ed25519.PublicKey(x).Equal(pub) {
		t.Error("X does not decode to the public key")
	}
	if jwk.Kid != jwtx.Thumbprint(pub) {
		t.Errorf("Kid = %q, want thumbprint %q", jwk.Kid, jwtx.Thumbprint(pub))
	}

	got, err := jwk.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if !got.Equal(pub) {
		t.Error("PublicKey() round-trip mismatch")
	}
}

func TestJWK_PublicKey_Rejects(t *testing.T) {
	pub, _ := newKey(t)
	good := jwtx.PublicJWK(pub)

	bad := []jwtx.JWK{
		func() jwtx.JWK { j := good; j.Kty = "RSA"; return j }(),
		func() jwtx.JWK { j := good; j.Crv = "X25519"; return j }(),
		func() jwtx.JWK { j := good; j.X = "!!!"; return j }(),
		func() jwtx.JWK { j := good; j.X = "AAAA"; return j }(), // wrong length
	}
	for i, j := range bad {
		if _, err := j.PublicKey(); err == nil {
			t.Errorf("bad[%d] %+v: PublicKey err = nil, want error", i, j)
		}
	}
}

func TestThumbprint(t *testing.T) {
	// RFC 7638: SHA-256 over the canonical JSON {"crv","kty","x"}.
	pub, _ := newKey(t)
	a := jwtx.Thumbprint(pub)
	b := jwtx.Thumbprint(pub)
	if a != b {
		t.Errorf("Thumbprint not deterministic: %q vs %q", a, b)
	}
	if _, err := base64.RawURLEncoding.DecodeString(a); err != nil || len(a) != 43 {
		t.Errorf("Thumbprint %q is not a 43-char base64url SHA-256", a)
	}
	other, _ := newKey(t)
	if jwtx.Thumbprint(other) == a {
		t.Error("distinct keys share a thumbprint")
	}
}

func TestJWKS_JSON(t *testing.T) {
	pub, _ := newKey(t)
	set := jwtx.JWKS{Keys: []jwtx.JWK{jwtx.PublicJWK(pub)}}

	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	keys, ok := generic["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("JSON = %s, want a \"keys\" array of 1", raw)
	}
	k := keys[0].(map[string]any)
	for _, field := range []string{"kty", "crv", "kid", "use", "alg", "x"} {
		if _, ok := k[field]; !ok {
			t.Errorf("JWK JSON missing %q: %s", field, raw)
		}
	}
	if _, leaked := k["d"]; leaked {
		t.Error("JWK JSON contains a private component")
	}

	var back jwtx.JWKS
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	got, err := jwtx.NewStaticKeySet(back.Keys...).PublicKey(jwtx.Thumbprint(pub))
	if err != nil {
		t.Fatalf("key set from unmarshalled JWKS: %v", err)
	}
	if !got.Equal(pub) {
		t.Error("JWKS JSON round-trip lost the key")
	}
}

func TestStaticKeySet_UnknownKid(t *testing.T) {
	pub, _ := newKey(t)
	keys := jwtx.NewStaticKeySet(jwtx.PublicJWK(pub))
	if _, err := keys.PublicKey("nope"); !errors.Is(err, jwtx.ErrUnknownKey) {
		t.Errorf("PublicKey(unknown) err = %v, want %v", err, jwtx.ErrUnknownKey)
	}
}

func TestStaticKeySet_SkipsMalformed(t *testing.T) {
	// One bad entry in a JWKS must not poison the good ones.
	pub, _ := newKey(t)
	bad := jwtx.JWK{Kty: "RSA", Kid: "bad"}
	keys := jwtx.NewStaticKeySet(bad, jwtx.PublicJWK(pub))

	if _, err := keys.PublicKey(jwtx.Thumbprint(pub)); err != nil {
		t.Errorf("good key unavailable: %v", err)
	}
	if _, err := keys.PublicKey("bad"); !errors.Is(err, jwtx.ErrUnknownKey) {
		t.Errorf("malformed key was registered: err = %v", err)
	}
}

func TestGeneratePrivateKey(t *testing.T) {
	a, err := jwtx.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := jwtx.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != ed25519.PrivateKeySize {
		t.Errorf("key is %d bytes, want %d", len(a), ed25519.PrivateKeySize)
	}
	if a.Equal(b) {
		t.Error("two generated keys are identical")
	}
}

// --- PEM --------------------------------------------------------------------------

func TestPrivateKeyPEM_RoundTrip(t *testing.T) {
	_, priv := newKey(t)

	pemBytes, err := jwtx.MarshalPrivateKeyPEM(priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pemBytes), "-----BEGIN PRIVATE KEY-----") {
		t.Errorf("PEM does not start with a PKCS#8 header: %q", pemBytes)
	}

	back, err := jwtx.ParsePrivateKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}
	if !back.Equal(priv) {
		t.Error("PEM round-trip mismatch")
	}
}

func TestParsePrivateKeyPEM_Rejects(t *testing.T) {
	for name, in := range map[string]string{
		"empty":         "",
		"not pem":       "hello",
		"wrong block":   "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n",
		"garbage pkcs8": "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n",
	} {
		if _, err := jwtx.ParsePrivateKeyPEM([]byte(in)); err == nil {
			t.Errorf("%s: err = nil, want error", name)
		}
	}
}

// --- helpers -------------------------------------------------------------------------

func mustSign(t *testing.T, s *jwtx.Signer) string {
	t.Helper()
	tok, _, err := s.Sign(subject, now, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func decodeSegment(t *testing.T, seg string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func encodeSegment(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// tamper swaps the subject in the payload without re-signing.
func tamper(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	payload := decodeSegment(t, parts[1])
	payload["sub"] = "attacker"
	parts[1] = encodeSegment(payload)
	return strings.Join(parts, ".")
}

func unsigned(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": subject, "iss": issuer, "aud": audience,
		"iat": now.Unix(), "exp": now.Add(ttl).Unix(),
	})
	s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hmacConfusion(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": subject, "iss": issuer, "aud": audience,
		"iat": now.Unix(), "exp": now.Add(ttl).Unix(),
	})
	tok.Header["kid"] = jwtx.Thumbprint(pub)
	s, err := tok.SignedString([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func withoutKid(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub": subject, "iss": issuer, "aud": audience,
		"iat": now.Unix(), "exp": now.Add(ttl).Unix(),
	})
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
