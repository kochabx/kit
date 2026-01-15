package ecies

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/subtle"
	"encoding/hex"
	"math/big"

	"github.com/kochabx/kit/core/crypto/ecies/internal"
)

// PublicKey represents an ECIES public key that wraps an ECDH public key.
// It provides methods for key decapsulation and point encoding.
type PublicKey struct {
	ecdhKey *ecdh.PublicKey
	// Keep ECDSA-compatible fields for serialization
	curve elliptic.Curve
	x, y  *big.Int
}

// Bytes returns the public key in encoded format.
// If compressed is true, returns 33 bytes (0x02/0x03 + X).
// If compressed is false, returns 65 bytes (0x04 + X + Y).
func (pub *PublicKey) Bytes(compressed bool) []byte {
	if pub.ecdhKey != nil {
		// Use the efficient ECDH encoding
		pubBytes := pub.ecdhKey.Bytes()
		if !compressed {
			return pubBytes // Already uncompressed format
		}
		// For compressed, derive from coordinates
	}

	// Fallback to coordinate-based encoding
	xBytes := internal.ZeroPad(pub.x.Bytes(), CurvePointSize)

	if compressed {
		if pub.y.Bit(0) == 0 {
			return append([]byte{CompressedEvenTag}, xBytes...)
		}
		return append([]byte{CompressedOddTag}, xBytes...)
	}

	yBytes := internal.ZeroPad(pub.y.Bytes(), CurvePointSize)
	return bytes.Join([][]byte{{UncompressedPointTag}, xBytes, yBytes}, nil)
}

// Hex returns the public key in hexadecimal encoding.
func (pub *PublicKey) Hex(compressed bool) string {
	return hex.EncodeToString(pub.Bytes(compressed))
}

// Decapsulate performs ECDH key decapsulation using the sender's ephemeral public key.
// It derives the same shared secret that the sender derived, which can be used for decryption.
func (pub *PublicKey) Decapsulate(senderPrivateKey *PrivateKey) ([]byte, error) {
	if senderPrivateKey == nil {
		return nil, ErrPrivateKeyEmpty
	}

	// Perform ECDH
	sharedSecret, err := senderPrivateKey.ecdhKey.ECDH(pub.ecdhKey)
	if err != nil {
		return nil, ErrKeyDerivationFailed
	}

	// Derive encryption key using KDF
	return deriveKey(pub.Bytes(false), sharedSecret)
}

// Equals compares two public keys using constant-time comparison
// to resist timing attacks.
func (pub *PublicKey) Equals(other *PublicKey) bool {
	if pub == nil || other == nil {
		return pub == other
	}
	if pub.x == nil || pub.y == nil || other.x == nil || other.y == nil {
		return false
	}

	eqX := subtle.ConstantTimeCompare(pub.x.Bytes(), other.x.Bytes()) == 1
	eqY := subtle.ConstantTimeCompare(pub.y.Bytes(), other.y.Bytes()) == 1
	return eqX && eqY
}

// ImportECDSAPublic converts an ECDSA public key to an ECIES public key.
func ImportECDSAPublic(ecdsaKey *ecdsa.PublicKey) (*PublicKey, error) {
	if ecdsaKey == nil {
		return nil, ErrPublicKeyEmpty
	}

	// Validate that the point is on the curve
	if !ecdsaKey.Curve.IsOnCurve(ecdsaKey.X, ecdsaKey.Y) {
		return nil, ErrKeyNotOnCurve
	}

	// Encode to uncompressed format for ECDH
	xBytes := internal.ZeroPad(ecdsaKey.X.Bytes(), CurvePointSize)
	yBytes := internal.ZeroPad(ecdsaKey.Y.Bytes(), CurvePointSize)
	pubBytes := append([]byte{UncompressedPointTag}, append(xBytes, yBytes...)...)

	// Create ECDH public key
	curve := ecdh.P256()
	ecdhKey, err := curve.NewPublicKey(pubBytes)
	if err != nil {
		return nil, ErrInvalidPublicKey
	}

	return &PublicKey{
		ecdhKey: ecdhKey,
		curve:   ecdsaKey.Curve,
		x:       new(big.Int).Set(ecdsaKey.X),
		y:       new(big.Int).Set(ecdsaKey.Y),
	}, nil
}
