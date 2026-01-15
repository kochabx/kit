package ecies

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kochabx/kit/core/stag"
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
	if err := stag.ApplyDefaults(option); err != nil {
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

// SavePrivateKey saves a private key to a file in PEM format.
func SavePrivateKey(privateKey *PrivateKey, path string) error {
	if privateKey == nil {
		return ErrPrivateKeyEmpty
	}

	// Convert to ECDSA format for serialization
	tempKey := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     privateKey.publicKey.x,
			Y:     privateKey.publicKey.y,
		},
		D: privateKey.d,
	}

	privateKeyBytes, err := x509.MarshalECPrivateKey(tempKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Create PEM block
	privateKeyBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	// Write to file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}
	defer file.Close()

	if err := pem.Encode(file, privateKeyBlock); err != nil {
		return fmt.Errorf("failed to encode PEM: %w", err)
	}

	return nil
}

// SavePublicKey saves a public key to a file in PEM format.
func SavePublicKey(publicKey *PublicKey, path string) error {
	if publicKey == nil {
		return ErrPublicKeyEmpty
	}

	// Convert to ECDSA format for serialization
	tempKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     publicKey.x,
		Y:     publicKey.y,
	}

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(tempKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}

	// Create PEM block
	publicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	// Write to file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}
	defer file.Close()

	if err := pem.Encode(file, publicKeyBlock); err != nil {
		return fmt.Errorf("failed to encode PEM: %w", err)
	}

	return nil
}

// LoadPrivateKey loads an ECDSA private key from a PEM file.
func LoadPrivateKey(path string) (*PrivateKey, error) {
	privateKeyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}

	block, _ := pem.Decode(privateKeyBytes)
	if block == nil {
		return nil, ErrInvalidPEMBlock
	}

	ecdsaKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return ImportECDSA(ecdsaKey)
}

// LoadPublicKey loads an ECDSA public key from a PEM file.
func LoadPublicKey(path string) (*PublicKey, error) {
	publicKeyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyFileRead, err)
	}

	block, _ := pem.Decode(publicKeyBytes)
	if block == nil {
		return nil, ErrInvalidPEMBlock
	}

	publicKeyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	ecdsaKey, ok := publicKeyInterface.(*ecdsa.PublicKey)
	if !ok {
		return nil, ErrInvalidPublicKey
	}

	return ImportECDSAPublic(ecdsaKey)
}
