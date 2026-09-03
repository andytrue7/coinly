package domain

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password length policy. The minimum follows NIST SP 800-63B; the
// maximum bounds the work a single request can force the KDF to do.
const (
	minPasswordLength = 8
	maxPasswordLength = 128
)

// PasswordHasher turns a plaintext password into a storable hash and
// verifies a plaintext against a stored hash. It is an interface so tests
// can substitute a cheap fake; production uses Argon2Hasher.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify reports whether password matches hash. A false result with a
	// nil error means "wrong password"; a non-nil error means the hash
	// could not be checked at all (malformed, unsupported algorithm).
	Verify(password, hash string) (bool, error)
}

// Argon2Params are the argon2id cost parameters. Memory is in KiB.
type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params returns the production cost parameters: the OWASP
// Password Storage Cheat Sheet's first recommended argon2id configuration
// (m=19 MiB, t=2, p=1), which targets roughly 50 ms per hash on commodity
// server hardware.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// Argon2Hasher implements PasswordHasher with argon2id, producing and
// consuming the PHC string format:
//
//	$argon2id$v=19$m=<KiB>,t=<iters>,p=<par>$<salt b64>$<key b64>
//
// The parameters are embedded in each hash, so hashes minted under older,
// weaker defaults keep verifying after the defaults are raised.
type Argon2Hasher struct {
	params Argon2Params
}

// NewArgon2Hasher returns a hasher that mints hashes with the given
// parameters. Verification always uses the parameters stored in the hash.
func NewArgon2Hasher(p Argon2Params) *Argon2Hasher {
	return &Argon2Hasher{params: p}
}

// Hash derives an argon2id key for password under a fresh random salt.
func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2: generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt,
		h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyLength)

	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.Memory, h.params.Iterations, h.params.Parallelism,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify re-derives the key using the parameters and salt encoded in hash
// and compares it to the stored key in constant time.
func (h *Argon2Hasher) Verify(password, hash string) (bool, error) {
	params, salt, key, err := decodeArgon2Hash(hash)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt,
		params.Iterations, params.Memory, params.Parallelism, params.KeyLength)

	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

var errMalformedHash = errors.New("argon2: malformed hash")

func decodeArgon2Hash(hash string) (Argon2Params, []byte, []byte, error) {
	// Leading "$" yields an empty first element.
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, fmt.Errorf("%w: unsupported version %q", errMalformedHash, parts[2])
	}

	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Argon2Params{}, nil, nil, fmt.Errorf("%w: bad params %q", errMalformedHash, parts[3])
	}

	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, fmt.Errorf("%w: bad salt: %w", errMalformedHash, err)
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, fmt.Errorf("%w: bad key: %w", errMalformedHash, err)
	}
	if len(salt) == 0 || len(key) == 0 {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	p.SaltLength = uint32(len(salt)) //nolint:gosec // bounded by the decoded hash string
	p.KeyLength = uint32(len(key))   //nolint:gosec // bounded by the decoded hash string

	return p, salt, key, nil
}

func validatePassword(password string) error {
	switch {
	case len(password) < minPasswordLength:
		return ErrWeakPassword
	case len(password) > maxPasswordLength:
		return ErrPasswordTooLong
	}
	return nil
}
