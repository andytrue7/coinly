package domain_test

import (
	"strings"
	"testing"

	"github.com/andytrue7/coinly/services/identity/internal/domain"
)

// testArgon2Params are deliberately cheap so the suite stays fast; the
// production defaults are checked separately in TestDefaultArgon2Params.
var testArgon2Params = domain.Argon2Params{
	Memory:      8 * 1024, // KiB
	Iterations:  1,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

func TestArgon2Hasher_HashAndVerify(t *testing.T) {
	h := domain.NewArgon2Hasher(testArgon2Params)

	hash, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("hash %q is not PHC argon2id format", hash)
	}
	if strings.Contains(hash, "correct horse") {
		t.Errorf("hash %q leaks the plaintext", hash)
	}

	ok, err := h.Verify("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("Verify(correct): %v", err)
	}
	if !ok {
		t.Error("Verify(correct) = false, want true")
	}

	ok, err = h.Verify("correct horse battery stapl", hash)
	if err != nil {
		t.Fatalf("Verify(wrong): %v", err)
	}
	if ok {
		t.Error("Verify(wrong) = true, want false")
	}
}

func TestArgon2Hasher_UniqueSaltPerHash(t *testing.T) {
	h := domain.NewArgon2Hasher(testArgon2Params)

	a, err := h.Hash("same password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash("same password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("two hashes of the same password are identical (%q): salt not random", a)
	}
}

func TestArgon2Hasher_ParamsEncodedInHash(t *testing.T) {
	// A hash must carry its own cost parameters so old hashes keep
	// verifying after the defaults are raised.
	h := domain.NewArgon2Hasher(testArgon2Params)
	hash, err := h.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}

	stronger := domain.NewArgon2Hasher(domain.Argon2Params{
		Memory: 16 * 1024, Iterations: 2, Parallelism: 2, SaltLength: 16, KeyLength: 32,
	})
	ok, err := stronger.Verify("pw", hash)
	if err != nil {
		t.Fatalf("Verify with different params: %v", err)
	}
	if !ok {
		t.Error("hash produced with old params fails to verify under new params")
	}
}

func TestArgon2Hasher_VerifyMalformed(t *testing.T) {
	h := domain.NewArgon2Hasher(testArgon2Params)

	malformed := []string{
		"",
		"not-a-hash",
		"$bcrypt$whatever",
		"$argon2id$v=19$m=8192,t=1,p=1$notbase64!!$abc",
		"$argon2id$v=19$m=8192,t=1,p=1$c2FsdA",         // missing key segment
		"$argon2id$v=18$m=8192,t=1,p=1$c2FsdA$c2FsdA",  // wrong version
		"$argon2id$v=19$m=lots,t=1,p=1$c2FsdA$c2FsdA",  // unparsable params
		"$argon2id$v=19$m=8192,t=1,p=1$$c2FsdA",        // empty salt
		"$argon2id$v=19$m=8192,t=1,p=1$c2FsdA$",        // empty key
		"$argon2id$v=19$m=8192,t=1,p=1$c2FsdA$c2Fs!dA", // bad key base64
	}
	for _, in := range malformed {
		ok, err := h.Verify("pw", in)
		if err == nil {
			t.Errorf("Verify(%q): err = nil, want error", in)
		}
		if ok {
			t.Errorf("Verify(%q): ok = true, want false", in)
		}
	}
}

func TestDefaultArgon2Params(t *testing.T) {
	// Floor from the OWASP Password Storage Cheat Sheet for argon2id:
	// m=19 MiB, t=2, p=1. Defaults must be at least that strong.
	p := domain.DefaultArgon2Params()
	if p.Memory < 19*1024 {
		t.Errorf("Memory = %d KiB, want >= %d", p.Memory, 19*1024)
	}
	if p.Iterations < 2 {
		t.Errorf("Iterations = %d, want >= 2", p.Iterations)
	}
	if p.Parallelism < 1 {
		t.Errorf("Parallelism = %d, want >= 1", p.Parallelism)
	}
	if p.SaltLength < 16 {
		t.Errorf("SaltLength = %d, want >= 16", p.SaltLength)
	}
	if p.KeyLength < 32 {
		t.Errorf("KeyLength = %d, want >= 32", p.KeyLength)
	}
}
