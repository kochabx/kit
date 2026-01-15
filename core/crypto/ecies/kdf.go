package ecies

import (
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// deriveKey derives an encryption key from the shared secret using HKDF-SHA256.
// This implements the key derivation function (KDF) for ECIES.
//
// The derivation includes the public key bytes as additional context to ensure
// unique keys for each encryption operation.
func deriveKey(publicKeyBytes []byte, sharedSecret []byte) ([]byte, error) {
	// Use HKDF (HMAC-based Key Derivation Function) with SHA-256
	// This provides proper key stretching and domain separation
	kdfReader := hkdf.New(sha256.New, sharedSecret, nil, publicKeyBytes)

	// Derive a 256-bit (32-byte) symmetric key for AES-256
	key := make([]byte, AESKeySize)
	if _, err := io.ReadFull(kdfReader, key); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyDerivationFailed, err)
	}

	return key, nil
}
