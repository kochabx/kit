package ecies

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"math/big"

	"github.com/kochabx/kit/core/crypto/ecies/internal"
)

// PrivateKey represents an ECIES private key that wraps an ECDH private key.
// It provides methods for key encapsulation and shared secret derivation.
type PrivateKey struct {
	publicKey *PublicKey
	ecdhKey   *ecdh.PrivateKey
	d         *big.Int // Keep for compatibility and serialization
}

// Public returns the public key corresponding to this private key.
func (priv *PrivateKey) Public() *PublicKey {
	return priv.publicKey
}

// Bytes returns the private key as a byte slice (big-endian representation of D).
func (priv *PrivateKey) Bytes() []byte {
	if priv.d == nil {
		return nil
	}
	return priv.d.Bytes()
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
	if priv.d == nil || other.d == nil {
		return priv.d == other.d
	}
	return subtle.ConstantTimeCompare(priv.d.Bytes(), other.d.Bytes()) == 1
}

// Destroy securely clears the private key material from memory.
// After calling this method, the private key should not be used.
func (priv *PrivateKey) Destroy() {
	if priv.d != nil {
		priv.d.SetInt64(0)
		priv.d = nil
	}
	// Note: crypto/ecdh doesn't provide a way to zero the key material
	// but setting to nil allows GC to reclaim it
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

	// Convert to ECDH format
	keyBytes := internal.ZeroPad(ecdsaKey.D.Bytes(), CurvePointSize)
	curve := ecdh.P256()
	ecdhKey, err := curve.NewPrivateKey(keyBytes)
	if err != nil {
		return nil, ErrInvalidPrivateKey
	}

	return &PrivateKey{
		publicKey: publicKey,
		ecdhKey:   ecdhKey,
		d:         new(big.Int).Set(ecdsaKey.D),
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

	// Parse public key bytes to extract X, Y for compatibility
	pubBytes := ecdhPubKey.Bytes()
	if len(pubBytes) != PublicKeyBytes {
		return nil, ErrInvalidPublicKey
	}

	publicKey.curve = elliptic.P256()
	publicKey.x = new(big.Int).SetBytes(pubBytes[1:33])
	publicKey.y = new(big.Int).SetBytes(pubBytes[33:65])

	// Extract private key scalar
	privBytes := ecdhKey.Bytes()
	d := new(big.Int).SetBytes(privBytes)

	return &PrivateKey{
		publicKey: publicKey,
		ecdhKey:   ecdhKey,
		d:         d,
	}, nil
}
