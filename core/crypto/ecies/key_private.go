package ecies

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
)

// PrivateKey represents an ECIES private key that wraps an ECDH private key.
// It provides methods for key encapsulation and shared secret derivation.
type PrivateKey struct {
	publicKey *PublicKey
	ecdhKey   *ecdh.PrivateKey
}

// Public returns the public key corresponding to this private key.
func (priv *PrivateKey) Public() *PublicKey {
	return priv.publicKey
}

// Bytes returns the private key scalar as a fixed-length big-endian byte slice.
// Returns nil if the key has been destroyed.
func (priv *PrivateKey) Bytes() []byte {
	if priv.ecdhKey == nil {
		return nil
	}
	return priv.ecdhKey.Bytes()
}

// Hex returns the private key in hexadecimal encoding.
func (priv *PrivateKey) Hex() string {
	return hex.EncodeToString(priv.Bytes())
}

// Encapsulate performs ECDH key encapsulation using the recipient's public key.
// It derives a shared secret that can be safely used as an encryption key.
// This uses the modern crypto/ecdh package for constant-time operations.
func (priv *PrivateKey) Encapsulate(recipientPublicKey *PublicKey) ([]byte, error) {
	if recipientPublicKey == nil {
		return nil, ErrPublicKeyEmpty
	}

	// Perform ECDH using constant-time implementation
	sharedSecret, err := priv.ecdhKey.ECDH(recipientPublicKey.ecdhKey)
	if err != nil {
		return nil, ErrKeyDerivationFailed
	}

	// Derive encryption key using KDF
	return deriveKey(priv.publicKey.Bytes(false), sharedSecret)
}

// ECDH derives a shared secret using Elliptic Curve Diffie-Hellman.
// The returned value should NOT be used directly as an encryption key.
// Use Encapsulate() instead for proper key derivation.
//
// This method is provided for compatibility and low-level operations.
func (priv *PrivateKey) ECDH(publicKey *PublicKey) ([]byte, error) {
	if publicKey == nil {
		return nil, ErrPublicKeyEmpty
	}

	sharedSecret, err := priv.ecdhKey.ECDH(publicKey.ecdhKey)
	if err != nil {
		return nil, err
	}

	return sharedSecret, nil
}

// Equals compares two private keys using constant-time comparison
// to resist timing attacks.
func (priv *PrivateKey) Equals(other *PrivateKey) bool {
	if priv == nil || other == nil {
		return priv == other
	}
	if priv.ecdhKey == nil || other.ecdhKey == nil {
		return priv.ecdhKey == other.ecdhKey
	}
	return subtle.ConstantTimeCompare(priv.ecdhKey.Bytes(), other.ecdhKey.Bytes()) == 1
}

// Destroy clears the private key reference, allowing the GC to reclaim the memory.
// After calling this method, the private key should not be used.
func (priv *PrivateKey) Destroy() {
	// crypto/ecdh does not expose a zeroing API; removing the reference
	// ensures GC can collect the memory.
	priv.ecdhKey = nil
}

// ImportECDSA converts an ECDSA private key to an ECIES private key.
// This allows interoperability with standard ECDSA key generation.
func ImportECDSA(ecdsaKey *ecdsa.PrivateKey) (*PrivateKey, error) {
	if ecdsaKey == nil {
		return nil, ErrPrivateKeyEmpty
	}

	publicKey, err := ImportECDSAPublic(&ecdsaKey.PublicKey)
	if err != nil {
		return nil, err
	}

	// Use the standard-library helper to convert to ECDH format (Go 1.20+).
	// This avoids direct access to the deprecated D field.
	ecdhKey, err := ecdsaKey.ECDH()
	if err != nil {
		return nil, ErrInvalidPrivateKey
	}

	return &PrivateKey{
		publicKey: publicKey,
		ecdhKey:   ecdhKey,
	}, nil
}

// GenerateKey generates a new ECIES key pair using the P-256 curve.
func GenerateKey() (*PrivateKey, error) {
	curve := ecdh.P256()
	ecdhKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	// Extract public key
	ecdhPubKey := ecdhKey.PublicKey()
	publicKey := &PublicKey{
		ecdhKey: ecdhPubKey,
	}

	// Sanity-check the encoded length so callers can rely on Bytes() shape.
	if len(ecdhPubKey.Bytes()) != PublicKeyBytes {
		return nil, ErrInvalidPublicKey
	}

	return &PrivateKey{
		publicKey: publicKey,
		ecdhKey:   ecdhKey,
	}, nil
}
