package ecies

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kochabx/kit/core/defaults"
)

// PEM block types used by this package.
//
// Private keys are written as PKCS#8 (RFC 5958) and public keys as
// SubjectPublicKeyInfo (RFC 5280). These are the modern, language-neutral
// formats that the Go standard library can serialize directly from
// *ecdh.PrivateKey / *ecdh.PublicKey without going through *ecdsa.* shims.
const (
	pemTypePrivateKey = "PRIVATE KEY"
	pemTypePublicKey  = "PUBLIC KEY"
)

// KeyOption contains options for key generation and file I/O.
type KeyOption struct {
	Dirpath            string `json:"dirpath" default:"."`
	PrivateKeyFilename string `json:"private_key_filename" default:"private.pem"`
	PublicKeyFilename  string `json:"public_key_filename" default:"public.pem"`
}

// WithDirpath sets the directory path for key file operations.
func WithDirpath(dirpath string) func(*KeyOption) {
	return func(o *KeyOption) {
		o.Dirpath = dirpath
	}
}

// WithPrivateKeyFilename sets the filename for the private key.
func WithPrivateKeyFilename(filename string) func(*KeyOption) {
	return func(o *KeyOption) {
		o.PrivateKeyFilename = filename
	}
}

// WithPublicKeyFilename sets the filename for the public key.
func WithPublicKeyFilename(filename string) func(*KeyOption) {
	return func(o *KeyOption) {
		o.PublicKeyFilename = filename
	}
}

// GenerateKeyPair generates a new ECDSA key pair and saves it to the specified directory.
// The keys are saved in PEM format for easy interoperability.
func GenerateKeyPair(opts ...func(*KeyOption)) error {
	option := &KeyOption{}

	// Apply default tags
	if err := defaults.Apply(option); err != nil {
		return fmt.Errorf("failed to apply defaults: %w", err)
	}

	// Apply user options
	for _, opt := range opts {
		opt(option)
	}

	// Generate new key pair
	privateKey, err := GenerateKey()
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}
	defer privateKey.Destroy()

	// Save private key
	privateKeyPath := filepath.Join(option.Dirpath, option.PrivateKeyFilename)
	if err := SavePrivateKey(privateKey, privateKeyPath); err != nil {
		return fmt.Errorf("failed to save private key: %w", err)
	}

	// Save public key
	publicKeyPath := filepath.Join(option.Dirpath, option.PublicKeyFilename)
	if err := SavePublicKey(privateKey.Public(), publicKeyPath); err != nil {
		return fmt.Errorf("failed to save public key: %w", err)
	}

	return nil
}

// SavePrivateKey saves a private key to a file in PKCS#8 PEM format.
func SavePrivateKey(privateKey *PrivateKey, path string) error {
	if privateKey == nil || privateKey.ecdhKey == nil {
		return ErrPrivateKeyEmpty
	}

	der, err := x509.MarshalPKCS8PrivateKey(privateKey.ecdhKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}
	defer file.Close()

	if err := pem.Encode(file, &pem.Block{Type: pemTypePrivateKey, Bytes: der}); err != nil {
		return fmt.Errorf("failed to encode PEM: %w", err)
	}
	return nil
}

// SavePublicKey saves a public key to a file in SubjectPublicKeyInfo PEM format.
func SavePublicKey(publicKey *PublicKey, path string) error {
	if publicKey == nil || publicKey.ecdhKey == nil {
		return ErrPublicKeyEmpty
	}

	der, err := x509.MarshalPKIXPublicKey(publicKey.ecdhKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}
	defer file.Close()

	if err := pem.Encode(file, &pem.Block{Type: pemTypePublicKey, Bytes: der}); err != nil {
		return fmt.Errorf("failed to encode PEM: %w", err)
	}
	return nil
}

// LoadPrivateKey loads a PKCS#8-encoded ECDH private key from a PEM file.
func LoadPrivateKey(path string) (*PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, ErrInvalidPEMBlock
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// crypto/x509 returns *ecdsa.PrivateKey for NIST curves and
	// *ecdh.PrivateKey only for X25519. Normalise to the ECDH form.
	var ecdhKey *ecdh.PrivateKey
	switch k := parsed.(type) {
	case *ecdh.PrivateKey:
		ecdhKey = k
	case *ecdsa.PrivateKey:
		ecdhKey, err = k.ECDH()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
		}
	default:
		return nil, ErrInvalidPrivateKey
	}
	if ecdhKey.Curve() != ecdh.P256() {
		return nil, ErrInvalidPrivateKey
	}

	return &PrivateKey{
		publicKey: &PublicKey{ecdhKey: ecdhKey.PublicKey()},
		ecdhKey:   ecdhKey,
	}, nil
}

// LoadPublicKey loads a SubjectPublicKeyInfo-encoded ECDH public key from a PEM file.
func LoadPublicKey(path string) (*PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, ErrInvalidPEMBlock
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	var ecdhKey *ecdh.PublicKey
	switch k := parsed.(type) {
	case *ecdh.PublicKey:
		ecdhKey = k
	case *ecdsa.PublicKey:
		ecdhKey, err = k.ECDH()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPublicKey, err)
		}
	default:
		return nil, ErrInvalidPublicKey
	}
	if ecdhKey.Curve() != ecdh.P256() {
		return nil, ErrInvalidPublicKey
	}

	return &PublicKey{ecdhKey: ecdhKey}, nil
}
