package jwtx

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

const pemBlockType = "PRIVATE KEY" // PKCS#8

// GeneratePrivateKey creates a fresh Ed25519 signing key. Intended for
// dev/test bootstrapping; production keys come from configuration.
func GeneratePrivateKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("jwtx: generate key: %w", err)
	}
	return priv, nil
}

// MarshalPrivateKeyPEM encodes priv as a PKCS#8 "PRIVATE KEY" PEM block,
// the format `openssl genpkey -algorithm ed25519` produces.
func MarshalPrivateKeyPEM(priv ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("jwtx: marshal pkcs8: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: pemBlockType, Bytes: der}), nil
}

// ParsePrivateKeyPEM decodes a PKCS#8 PEM Ed25519 private key.
func ParsePrivateKeyPEM(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("jwtx: no PEM block found")
	}
	if block.Type != pemBlockType {
		return nil, fmt.Errorf("jwtx: unexpected PEM block %q, want %q", block.Type, pemBlockType)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("jwtx: parse pkcs8: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errNotEd25519
	}
	return priv, nil
}
