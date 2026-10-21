package evidence

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Default key file names inside a key directory.
const (
	PrivateKeyFile = "capledger.key.pem"
	PublicKeyFile  = "capledger.pub.pem"
)

// KeyID returns the first 16 hex characters of SHA-256 over the public key.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// GenerateKeyPair writes a new Ed25519 key pair into dir. It refuses to
// overwrite existing key files.
func GenerateKeyPair(dir string) (privPath, pubPath, keyID string, err error) {
	privPath = filepath.Join(dir, PrivateKeyFile)
	pubPath = filepath.Join(dir, PublicKeyFile)
	for _, p := range []string{privPath, pubPath} {
		if _, statErr := os.Stat(p); statErr == nil {
			return "", "", "", fmt.Errorf("%s already exists; refusing to overwrite a key", p)
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", "", err
	}
	pubDer, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDer}), 0o644); err != nil {
		return "", "", "", err
	}
	return privPath, pubPath, KeyID(pub), nil
}

// LoadPrivateKey reads a PKCS#8 PEM Ed25519 private key.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s: not a PEM private key", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New(path + ": not an Ed25519 key")
	}
	return priv, nil
}

// LoadPublicKey reads a PKIX PEM Ed25519 public key.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("%s: not a PEM public key", path)
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New(path + ": not an Ed25519 key")
	}
	return pub, nil
}

// LoadPublicKeys reads several public keys and indexes them by key id.
func LoadPublicKeys(paths []string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey, len(paths))
	for _, p := range paths {
		pub, err := LoadPublicKey(p)
		if err != nil {
			return nil, err
		}
		keys[KeyID(pub)] = pub
	}
	return keys, nil
}
