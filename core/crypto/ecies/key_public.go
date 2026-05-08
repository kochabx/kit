package ecies

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/subtle"
	"encoding/hex"
)

// PublicKey represents an ECIES public key that wraps an ECDH public key.
// It provides methods for key decapsulation and point encoding.
type PublicKey struct {
	ecdhKey *ecdh.PublicKey
}

// Bytes returns the public key in encoded format.
// If compressed is true, returns 33 bytes (0x02/0x03 + X).
// If compressed is false, returns 65 bytes (0x04 + X + Y).
func (pub *PublicKey) Bytes(compressed bool) []byte {
	if pub == nil || pub.ecdhKey == nil {
		return nil
	}
	// (*ecdh.PublicKey).Bytes returns 0x04 || X(32) || Y(32) for NIST curves.
	raw := pub.ecdhKey.Bytes()
	if !compressed {
		return raw
	}
	tag := byte(CompressedEvenTag)
	if raw[len(raw)-1]&1 != 0 {
		tag = CompressedOddTag
	}
	out := make([]byte, 1+CurvePointSize)
	out[0] = tag
	copy(out[1:], raw[1:1+CurvePointSize])
	return out
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
	if pub.ecdhKey == nil || other.ecdhKey == nil {
		return pub.ecdhKey == other.ecdhKey
	}
	return subtle.ConstantTimeCompare(pub.ecdhKey.Bytes(), other.ecdhKey.Bytes()) == 1
}

// ImportECDSAPublic converts an ECDSA public key to an ECIES public key.
func ImportECDSAPublic(ecdsaKey *ecdsa.PublicKey) (*PublicKey, error) {
	if ecdsaKey == nil {
		return nil, ErrPublicKeyEmpty
	}

	// (*ecdsa.PublicKey).ECDH performs the on-curve check and avoids the
	// deprecated low-level access to the X/Y/Curve fields (Go 1.20+).
	ecdhKey, err := ecdsaKey.ECDH()
	if err != nil {
		return nil, ErrInvalidPublicKey
	}

	return &PublicKey{ecdhKey: ecdhKey}, nil
}
